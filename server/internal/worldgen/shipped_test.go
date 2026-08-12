package worldgen

import (
	"path/filepath"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/worldstore"
)

// Полный путь входа на боевой карте: файл -> разбор -> валидация -> компиляция
// -> развёртка -> база.
//
// # Зачем отдельно от теста в mapfmt
//
// Тот проверяет, что файл ЧИТАЕТСЯ и осмыслен; этот — что прочитанное доезжает
// до базы. Между ними лежит всё, чего mapfmt не видит: распространение поз,
// рельеф, выбор клеток, кодирование чанков. Карта, валидная по форме и не
// доходящая до базы, — состояние, в котором сервер стартует и отдаёт пустой мир.
//
// # Чисел покрытия здесь нет намеренно
//
// Правило выбора уровня чанка правится (ClearAhead-cue), и вписанное сюда число
// чанков сделало бы этот тест сторожем чужого решения. Проверяется свойство:
// чанки появились и регион заведён.
func TestShippedMapReachesTheDatabase(t *testing.T) {
	path := filepath.Join("..", "..", mapfmt.ShippedMapPath)
	m, err := mapfmt.DecodeFile(path)
	if err != nil {
		t.Fatalf("разбор боевой карты: %v", err)
	}

	s, err := worldstore.Open(filepath.Join(t.TempDir(), "world.db"))
	if err != nil {
		t.Fatalf("база: %v", err)
	}
	defer s.Close()

	prov := `{"source":"` + mapfmt.ShippedMapPath + `","kind":"authored"}`
	rep, seeded, err := Bootstrap(s, m, 1, prov)
	if err != nil {
		t.Fatalf("бутстрап боевой карты: %v", err)
	}
	if !seeded {
		t.Fatal("бутстрап не тронул пустую базу")
	}
	if rep.TotalChunks == 0 {
		t.Fatal("боевая карта не породила ни одного чанка")
	}

	r, ok, err := s.GetRegion(m.MapID)
	if err != nil || !ok {
		t.Fatalf("региона %s нет в базе: ok=%v err=%v", m.MapID, ok, err)
	}
	// Происхождение доехало ТЕМ, ЧТО ПЕРЕДАЛИ. Проверяется потому, что до
	// перехода на файл оно было зашито в бутстрап строкой "seedmap": на карте,
	// прочитанной с диска, такая строка стала бы враньём о том, откуда данные.
	if r.Provenance != prov {
		t.Fatalf("провенанс региона %q, передавали %q", r.Provenance, prov)
	}
}
