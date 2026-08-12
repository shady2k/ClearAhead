package worldstore

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/chunk"
	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/seedmap"
	"github.com/shady2k/ClearAhead/server/internal/terrain"
	"github.com/shady2k/ClearAhead/server/internal/track"
)

func openStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "world.db"))
	if err != nil {
		t.Fatalf("открытие: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// testRule — правило подробности тестового региона: то же, что у фикстуры
// seedmap. Регион без правила хранилище не принимает — про его чанки нельзя
// было бы сказать, каким охватом они порождены.
var testRule = chunk.Rule{Level0RadiusM: 512, MaxLevel: 4}

func putRegion(t *testing.T, s *Store) {
	t.Helper()
	if err := s.PutRegion(Region{ID: "ST_A", Frame: `{"datum":"WGS84"}`, Epoch: 1, Rule: testRule}); err != nil {
		t.Fatalf("регион: %v", err)
	}
}

// Правило подробности переживает запись и чтение. Проверяется отдельно от
// прочих полей затем, что по нему клиент выбирает, какой уровень спрашивать:
// потерянное при чтении, оно стало бы «правилом неизвестно» и остановило бы
// сервер на старте.
func TestRegionRuleSurvivesRoundTrip(t *testing.T) {
	s := openStore(t)
	putRegion(t, s)
	got, ok, err := s.GetRegion("ST_A")
	if err != nil || !ok {
		t.Fatalf("чтение региона: ok=%v, err=%v", ok, err)
	}
	if got.Rule != testRule {
		t.Fatalf("правило %+v, записано %+v", got.Rule, testRule)
	}
	// Регион без правила — отказ, а не запись с нулями: молча записанный ноль
	// стал бы миром радиусом ноль, неотличимым от исправного.
	if err := s.PutRegion(Region{ID: "ST_B", Frame: "{}", Epoch: 1}); err == nil {
		t.Fatal("регион без правила подробности записан — отказа не было")
	}
}

func newField(t *testing.T) *terrain.Field {
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

func putChunk(t *testing.T, s *Store, f *terrain.Field, a chunk.Address) {
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
func TestSharedRowAgreesThroughStore(t *testing.T) {
	s := openStore(t)
	putRegion(t, s)
	f := newField(t)

	left := chunk.Address{Region: "ST_A", Level: 0, CX: 0, CZ: 0}
	right := chunk.Address{Region: "ST_A", Level: 0, CX: 1, CZ: 0}
	below := chunk.Address{Region: "ST_A", Level: 0, CX: 0, CZ: 1}
	for _, a := range []chunk.Address{left, right, below} {
		putChunk(t, s, f, a)
	}

	leftChunk, ok, err := s.GetChunk(left)
	if err != nil || !ok {
		t.Fatalf("левый чанк: ok=%v err=%v", ok, err)
	}
	rightChunk, _, _ := s.GetChunk(right)
	belowChunk, _, _ := s.GetChunk(below)

	for j := range chunk.Samples {
		a := leftChunk.Heights[chunk.Index(chunk.Samples-1, j)]
		b := rightChunk.Heights[chunk.Index(0, j)]
		if a != b {
			t.Fatalf("столбец %d: левый %d см, правый %d см", j, a, b)
		}
	}
	for i := range chunk.Samples {
		a := leftChunk.Heights[chunk.Index(i, chunk.Samples-1)]
		b := belowChunk.Heights[chunk.Index(i, 0)]
		if a != b {
			t.Fatalf("ряд %d: верхний %d см, нижний %d см", i, a, b)
		}
	}
}

// Разреженность — свойство хранилища, а не сбой: спросивший пустоту получает
// «здесь ничего нет», а не отказ.
func TestMissingChunkIsNotAnError(t *testing.T) {
	s := openStore(t)
	putRegion(t, s)

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
func TestBlobSurvivesRoundTrip(t *testing.T) {
	s := openStore(t)
	putRegion(t, s)

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
func TestHashFollowsBody(t *testing.T) {
	s := openStore(t)
	putRegion(t, s)
	a := chunk.Address{Region: "ST_A", Level: 0, CX: 0, CZ: 0}

	h := make([]int16, chunk.Samples*chunk.Samples)
	put := func(base int64) string {
		t.Helper()
		if err := s.PutChunk(Chunk{Address: a, Revision: 1, BaseZmm: base, Heights: h}); err != nil {
			t.Fatalf("запись: %v", err)
		}
		c, _, _ := s.GetChunk(a)
		return c.Hash
	}

	first := put(140000)
	if put(140000) != first {
		t.Fatal("хеш изменился без изменения тела")
	}
	h[42] = 7
	if put(140000) == first {
		t.Fatal("хеш не изменился при изменении отсчёта")
	}
	h[42] = 0
	if put(999000) == first {
		t.Fatal("хеш не изменился при смене опорной высоты")
	}
}

// Ссылка чанка на несуществующий регион не должна проходить: без включённых
// внешних ключей SQLite пропустил бы её молча.
func TestChunkWithoutRegionIsRejected(t *testing.T) {
	s := openStore(t)
	h := make([]int16, chunk.Samples*chunk.Samples)
	err := s.PutChunk(Chunk{
		Address: chunk.Address{Region: "НЕТ_ТАКОГО", Level: 0},
		Heights: h,
	})
	if err == nil {
		t.Fatal("чанк без региона записан")
	}
}

// TestConcurrentWritersDoNotCollide — база выдерживает столько писателей,
// сколько к серверу пришло запросов.
//
// # Почему это появилось только теперь
//
// До порождения чанков по требованию писал ОДИН поток на старте, и умолчаний
// SQLite хватало с запасом. Как только чанк считается по запросу, писателей
// становится столько же, сколько одновременных запросов, и первый же залп
// клиента упирался в `database is locked (SQLITE_BUSY)`: замер на этой самой
// проверке до настройки соединения давал 63 отказа из 512.
//
// # Что именно проверяется
//
// Не «нет гонок» — это дело -race, — а то, что настройки доехали до КАЖДОГО
// соединения пула. database/sql открывает соединения по мере надобности, и
// PRAGMA, выполненная разово через Exec, достаётся ровно одному из них;
// остальные приходят с умолчаниями. Одна горутина такого не покажет никогда:
// ей хватает первого соединения.
func TestConcurrentWritersDoNotCollide(t *testing.T) {
	s := openStore(t)
	putRegion(t, s)
	h := make([]int16, chunk.Samples*chunk.Samples)

	const writers, each = 32, 8
	errs := make(chan error, writers*each*2)
	var wg sync.WaitGroup
	for w := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := range each {
				a := chunk.Address{Region: "ST_A", Level: 0, CX: w, CZ: n}
				if err := s.PutChunk(Chunk{Address: a, Revision: 1, BaseZmm: 1000, Heights: h}); err != nil {
					errs <- err
					return
				}
				if _, ok, err := s.GetChunk(a); err != nil {
					errs <- err
					return
				} else if !ok {
					errs <- fmt.Errorf("чанк %v записан и не прочитан", a)
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)

	failed := 0
	for err := range errs {
		if failed == 0 {
			t.Errorf("первый отказ: %v", err)
		}
		failed++
	}
	if failed != 0 {
		t.Fatalf("отказов %d из %d записей", failed, writers*each)
	}

	// Внешние ключи обязаны действовать на ВСЕХ соединениях, а не только на
	// первом: пул к этой минуте уже разросся, и запись мимо региона, прошедшая
	// на одном из соединений, завела бы чанк-сироту.
	var wg2 sync.WaitGroup
	slipped := make(chan struct{}, writers)
	for range writers {
		wg2.Add(1)
		go func() {
			defer wg2.Done()
			err := s.PutChunk(Chunk{Address: chunk.Address{Region: "НЕТ_ТАКОГО", Level: 0}, Heights: h})
			if err == nil {
				slipped <- struct{}{}
			}
		}()
	}
	wg2.Wait()
	close(slipped)
	if n := len(slipped); n != 0 {
		t.Fatalf("чанк без региона прошёл %d раз: внешние ключи достались не каждому соединению", n)
	}
}
