package worldstore

import (
	"path/filepath"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/chunk"
	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/seedmap"
	"github.com/shady2k/ClearAhead/server/internal/terrain"
	"github.com/shady2k/ClearAhead/server/internal/track"
)

func открыть(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "world.db"))
	if err != nil {
		t.Fatalf("открытие: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func регион(t *testing.T, s *Store) {
	t.Helper()
	if err := s.PutRegion(Region{ID: "ST_A", Frame: `{"datum":"WGS84"}`, Epoch: 1}); err != nil {
		t.Fatalf("регион: %v", err)
	}
}

func рельеф(t *testing.T) *terrain.Field {
	t.Helper()
	m := seedmap.Station(seedmap.WithTerrain())
	if err := mapfmt.Validate(m); err != nil {
		t.Fatalf("фикстура невалидна: %v", err)
	}
	_, els, err := track.Propagate(m)
	if err != nil {
		t.Fatalf("распространение: %v", err)
	}
	field, err := terrain.New(m, els)
	if err != nil {
		t.Fatalf("рельеф: %v", err)
	}
	return field
}

func положитьЧанк(t *testing.T, s *Store, f *terrain.Field, a chunk.Address) {
	t.Helper()
	h, err := f.ChunkHeights(a)
	if err != nil {
		t.Fatalf("отсчёты чанка: %v", err)
	}
	err = s.PutChunk(Chunk{
		Address:  a,
		Revision: 1,
		BaseZmm:  int64(f.BaseZ() * 1000),
		Heights:  h,
	})
	if err != nil {
		t.Fatalf("запись чанка: %v", err)
	}
}

// СКВОЗНАЯ ПРОВЕРКА КОНТРАКТА. Два соседних чанка порождаются и пишутся
// ПОРОЗНЬ, а на стыке обязаны сойтись бит в бит: последний столбец левого — это
// нулевой столбец правого. Без этого между чанками остаётся щель в один
// интервал, и в неё видно небо.
//
// Проверка идёт через базу намеренно: она ловит не только арифметику сетки, но
// и кодирование блоба — порядок байт и порядок обхода.
func TestОбщийРядСходитсяЧерезБазу(t *testing.T) {
	s := открыть(t)
	регион(t, s)
	f := рельеф(t)

	левый := chunk.Address{Region: "ST_A", Level: 0, CX: 0, CZ: 0}
	правый := chunk.Address{Region: "ST_A", Level: 0, CX: 1, CZ: 0}
	нижний := chunk.Address{Region: "ST_A", Level: 0, CX: 0, CZ: 1}
	for _, a := range []chunk.Address{левый, правый, нижний} {
		положитьЧанк(t, s, f, a)
	}

	л, ok, err := s.GetChunk(левый)
	if err != nil || !ok {
		t.Fatalf("левый чанк: ok=%v err=%v", ok, err)
	}
	п, _, _ := s.GetChunk(правый)
	н, _, _ := s.GetChunk(нижний)

	for j := range chunk.Samples {
		a := л.Heights[chunk.Index(chunk.Samples-1, j)]
		b := п.Heights[chunk.Index(0, j)]
		if a != b {
			t.Fatalf("столбец %d: левый %d см, правый %d см", j, a, b)
		}
	}
	for i := range chunk.Samples {
		a := л.Heights[chunk.Index(i, chunk.Samples-1)]
		b := н.Heights[chunk.Index(i, 0)]
		if a != b {
			t.Fatalf("ряд %d: верхний %d см, нижний %d см", i, a, b)
		}
	}
}

// Разреженность — свойство хранилища, а не сбой: спросивший пустоту получает
// «здесь ничего нет», а не отказ.
func TestОтсутствующийЧанкНеОшибка(t *testing.T) {
	s := открыть(t)
	регион(t, s)

	c, ok, err := s.GetChunk(chunk.Address{Region: "ST_A", Level: 0, CX: 9999, CZ: -9999})
	if err != nil {
		t.Fatalf("отсутствующий чанк вернул ошибку: %v", err)
	}
	if ok {
		t.Fatalf("отсутствующий чанк нашёлся: %+v", c)
	}
}

// Блоб переживает запись и чтение без потерь, включая отрицательные отсчёты:
// int16 знаковый, и ошибка в знаке дала бы выемки высотой в 655 метров.
func TestБлобПереживаетКруг(t *testing.T) {
	s := открыть(t)
	регион(t, s)

	h := make([]int16, chunk.Samples*chunk.Samples)
	for i := range h {
		h[i] = int16(i%1000) - 500
	}
	a := chunk.Address{Region: "ST_A", Level: 3, CX: -7, CZ: 11}
	if err := s.PutChunk(Chunk{Address: a, Revision: 2, BaseZmm: 140000, Heights: h}); err != nil {
		t.Fatalf("запись: %v", err)
	}
	got, ok, err := s.GetChunk(a)
	if err != nil || !ok {
		t.Fatalf("чтение: ok=%v err=%v", ok, err)
	}
	for i := range h {
		if got.Heights[i] != h[i] {
			t.Fatalf("отсчёт %d: %d вместо %d", i, got.Heights[i], h[i])
		}
	}
	if got.Revision != 2 || got.BaseZmm != 140000 {
		t.Fatalf("ревизия %d, база %d мм", got.Revision, got.BaseZmm)
	}
	if len(got.Hash) != 64 {
		t.Fatalf("хеш длиной %d, ожидался sha256 в hex", len(got.Hash))
	}
}

// Хеш служит ETag'ом, значит обязан меняться вместе с телом и не меняться без
// него.
func TestХешСледуетЗаТелом(t *testing.T) {
	s := открыть(t)
	регион(t, s)
	a := chunk.Address{Region: "ST_A", Level: 0, CX: 0, CZ: 0}

	h := make([]int16, chunk.Samples*chunk.Samples)
	положить := func(base int64) string {
		t.Helper()
		if err := s.PutChunk(Chunk{Address: a, Revision: 1, BaseZmm: base, Heights: h}); err != nil {
			t.Fatalf("запись: %v", err)
		}
		c, _, _ := s.GetChunk(a)
		return c.Hash
	}

	первый := положить(140000)
	if положить(140000) != первый {
		t.Fatal("хеш изменился без изменения тела")
	}
	h[42] = 7
	if положить(140000) == первый {
		t.Fatal("хеш не изменился при изменении отсчёта")
	}
	h[42] = 0
	if положить(999000) == первый {
		t.Fatal("хеш не изменился при смене опорной высоты")
	}
}

// Ссылка чанка на несуществующий регион не должна проходить: без включённых
// внешних ключей SQLite пропустил бы её молча.
func TestЧанкБезРегионаОтвергается(t *testing.T) {
	s := открыть(t)
	h := make([]int16, chunk.Samples*chunk.Samples)
	err := s.PutChunk(Chunk{
		Address: chunk.Address{Region: "НЕТ_ТАКОГО", Level: 0},
		Heights: h,
	})
	if err == nil {
		t.Fatal("чанк без региона записан")
	}
}
