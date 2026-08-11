package worldgen

import (
	"path/filepath"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/chunk"
	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/seedmap"
	"github.com/shady2k/ClearAhead/server/internal/worldstore"
)

func карта(t *testing.T) *mapfmt.Map {
	t.Helper()
	return seedmap.Station(seedmap.WithTerrain())
}

func база(t *testing.T) *worldstore.Store {
	t.Helper()
	s, err := worldstore.Open(filepath.Join(t.TempDir(), "world.db"))
	if err != nil {
		t.Fatalf("база: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	if err := s.PutRegion(worldstore.Region{ID: "ST_A", Frame: "{}", Epoch: 1}); err != nil {
		t.Fatalf("регион: %v", err)
	}
	return s
}

func TestКонвейерПорождаетЧанки(t *testing.T) {
	s := база(t)
	rep, err := Generate(s, карта(t), "ST_A", 1)
	if err != nil {
		t.Fatalf("порождение: %v", err)
	}
	if rep.TotalChunks == 0 {
		t.Fatal("не порождено ни одного чанка")
	}
	t.Logf("чанков всего %d, байт %d (%.1f МБ)", rep.TotalChunks, rep.TotalBytes, float64(rep.TotalBytes)/1e6)
	for l := 0; l <= chunk.MaxLevel; l++ {
		t.Logf("  уровень %d: %d", l, rep.ByLevel[l])
	}

	// Подробность обязана быть у пути: без чанков нулевого уровня земляные
	// работы просто не видны.
	if rep.ByLevel[0] == 0 {
		t.Fatal("ни одного чанка нулевого уровня")
	}
	// И обязана падать с удалением: если все чанки нулевые, правило не
	// работает и хранилище перестало быть разреженным.
	дальние := 0
	for l := 1; l <= chunk.MaxLevel; l++ {
		дальние += rep.ByLevel[l]
	}
	if дальние == 0 {
		t.Fatal("ни одного чанка грубее нулевого уровня — правило подробности не сработало")
	}

	// Записанное читается.
	n, err := s.CountChunks("ST_A", 0)
	if err != nil {
		t.Fatalf("счёт: %v", err)
	}
	if n != rep.ByLevel[0] {
		t.Fatalf("в базе %d чанков нулевого уровня, отчёт говорит %d", n, rep.ByLevel[0])
	}
}

// Вдали от пути не хранится ничего: разреженность есть свойство хранилища.
func TestВдалиОтПутиЧанковНет(t *testing.T) {
	s := база(t)
	if _, err := Generate(s, карта(t), "ST_A", 1); err != nil {
		t.Fatalf("порождение: %v", err)
	}
	// Сто километров от станции — заведомо за пределами последнего уровня.
	далеко := chunk.Address{Region: "ST_A", Level: 0, CX: 400000 / int(chunk.SideM(0)), CZ: 0}
	if _, ok, err := s.GetChunk(далеко); err != nil {
		t.Fatalf("чтение: %v", err)
	} else if ok {
		t.Fatal("вдали от пути чанк всё-таки записан")
	}
}

// ИНВАРИАНТ ВХОДА: в базу попадает только то, что прошло полный путь. Карта, не
// проходящая валидацию, не должна оставить в базе ни одной записи.
func TestНевалиднаяКартаНеПишется(t *testing.T) {
	s := база(t)
	m := карта(t)
	m.MapID = "СЛОМАННЫЙ:ID" // разделитель в идентификаторе запрещён

	if _, err := Generate(s, m, "ST_A", 1); err == nil {
		t.Fatal("невалидная карта принята")
	}
	n, err := s.CountChunks("ST_A", 0)
	if err != nil {
		t.Fatalf("счёт: %v", err)
	}
	if n != 0 {
		t.Fatalf("после отказа в базе осталось %d чанков", n)
	}
}

// Регион должен существовать заранее: его геопривязку и происхождение конвейер
// придумать не вправе.
func TestБезРегионаОтказ(t *testing.T) {
	s, err := worldstore.Open(filepath.Join(t.TempDir(), "world.db"))
	if err != nil {
		t.Fatalf("база: %v", err)
	}
	defer s.Close()
	if _, err := Generate(s, карта(t), "ST_A", 1); err == nil {
		t.Fatal("порождение прошло без заведённого региона")
	}
}

// Бутстрап заполняет пустую базу и НЕ трогает заполненную: перезапись
// существующего мира затравкой уничтожила бы правки редактора.
func TestБутстрапИдемпотентен(t *testing.T) {
	s, err := worldstore.Open(filepath.Join(t.TempDir(), "world.db"))
	if err != nil {
		t.Fatalf("база: %v", err)
	}
	defer s.Close()

	m := seedmap.Station(seedmap.WithTerrain())

	rep, сделан, err := Bootstrap(s, m, 1)
	if err != nil {
		t.Fatalf("первый бутстрап: %v", err)
	}
	if !сделан {
		t.Fatal("первый бутстрап ничего не сделал на пустой базе")
	}
	if rep.TotalChunks == 0 {
		t.Fatal("бутстрап не породил чанков")
	}
	было, _ := s.CountChunks(m.MapID, 0)

	_, сделан2, err := Bootstrap(s, m, 2)
	if err != nil {
		t.Fatalf("второй бутстрап: %v", err)
	}
	if сделан2 {
		t.Fatal("второй бутстрап перезаписал заполненную базу")
	}
	стало, _ := s.CountChunks(m.MapID, 0)
	if было != стало {
		t.Fatalf("число чанков изменилось: было %d, стало %d", было, стало)
	}
}
