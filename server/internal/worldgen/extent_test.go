package worldgen

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/chunk"
	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/seedmap"
	"github.com/shady2k/ClearAhead/server/internal/worldstore"
)

// stationWithExtent — та же станция с другим охватом. Различается ровно одно
// число, поэтому разница в числе чанков — цена охвата, а не другой карты.
func stationWithExtent(radiusM float64, levels int) *mapfmt.Map {
	return seedmap.Station(seedmap.WithTerrain(), seedmap.Mutate(func(m *mapfmt.Map) {
		m.Terrain.Extent = mapfmt.Extent{Level0RadiusM: radiusM, Levels: levels}
	}))
}

func emptyStore(t *testing.T) *worldstore.Store {
	t.Helper()
	s, err := worldstore.Open(filepath.Join(t.TempDir(), "world.db"))
	if err != nil {
		t.Fatalf("база: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// ОХВАТ РЕШАЕТ КАРТА — то, ради чего числа и уехали из кода.
//
// Замер, а не рассуждение: две карты одной станции, различающиеся одним блоком,
// дают разное число чанков и разный последний уровень. Равенство здесь означало
// бы, что правило карты до порождения не доехало.
func TestExtentFromMapDecidesWorldSize(t *testing.T) {
	big, _, err := Bootstrap(emptyStore(t), stationWithExtent(512, 5), 1, "{}")
	if err != nil {
		t.Fatalf("большой мир: %v", err)
	}
	small, _, err := Bootstrap(emptyStore(t), stationWithExtent(256, 3), 1, "{}")
	if err != nil {
		t.Fatalf("малый мир: %v", err)
	}
	t.Logf("охват 8192 м: чанков %d, %.2f МБ, по уровням %v",
		big.TotalChunks, float64(big.TotalBytes)/1e6, big.ByLevel)
	t.Logf("охват 1024 м: чанков %d, %.2f МБ, по уровням %v",
		small.TotalChunks, float64(small.TotalBytes)/1e6, small.ByLevel)

	if !(small.TotalChunks < big.TotalChunks) {
		t.Fatalf("малый мир дал %d чанков, большой %d — охват карты до порождения не доехал",
			small.TotalChunks, big.TotalChunks)
	}
	// Уровней ровно столько, сколько названо картой: лишний уровень означал бы,
	// что обход считает по своему потолку, а не по правилу.
	if _, ok := small.ByLevel[3]; ok {
		t.Fatalf("у карты три уровня, а порождён и четвёртый: %v", small.ByLevel)
	}
	if big.ByLevel[4] == 0 {
		t.Fatalf("у карты пять уровней, а последний пуст: %v", big.ByLevel)
	}
}

// БАЗА, ЗАСЕЯННАЯ ДРУГИМ ПРАВИЛОМ, — ОТКАЗ, А НЕ ТИХАЯ ВЫДАЧА.
//
// Это и есть та ошибка, ради которой правило записано у региона: карту на диске
// подменили, а чанки в базе остались прежними. Без отказа сервер поднялся бы,
// манифест назвал бы новый охват, а база отвечала бы клетками старого — и
// расхождение искали бы глазами на снимке.
func TestBootstrapRefusesBaseSeededByAnotherRule(t *testing.T) {
	s := emptyStore(t)
	if _, seeded, err := Bootstrap(s, stationWithExtent(512, 5), 1, "{}"); err != nil || !seeded {
		t.Fatalf("первый засев: seeded=%v, err=%v", seeded, err)
	}
	// Тот же охват — тот же мир: бутстрап идемпотентен и молчит.
	if _, seeded, err := Bootstrap(s, stationWithExtent(512, 5), 1, "{}"); err != nil || seeded {
		t.Fatalf("повтор с тем же охватом: seeded=%v, err=%v", seeded, err)
	}

	_, seeded, err := Bootstrap(s, stationWithExtent(256, 3), 1, "{}")
	if err == nil {
		t.Fatal("карта с другим охватом принята на чужой базе — выдавались бы чужие чанки")
	}
	if seeded {
		t.Fatal("отказ сопровождался засевом")
	}
	// Отказ обязан называть оба охвата и способ починки: без них владелец видит
	// «что-то не сошлось» и идёт удалять базу наугад.
	for _, want := range []string{"512", "256", "-reseed"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("в отказе нет %q: %v", want, err)
		}
	}
}

// База ПРЕЖНЕЙ СБОРКИ — та, где охват был константой сервера и в регион не
// писался, — тоже отказ, и с другим текстом: сверять там не с чем.
//
// Проверяется функцией, а не живой базой, по единственной причине: регион без
// правила через PutRegion больше не записать, и подделать такую базу можно
// только своим SQL — то есть вторым местом, знающим форму таблицы регионов.
func TestUnknownStoredRuleIsRefused(t *testing.T) {
	err := sameRule("ST_A", chunk.Rule{}, chunk.Rule{Level0RadiusM: 512, MaxLevel: 4})
	if err == nil {
		t.Fatal("регион без записанного правила принят")
	}
	for _, want := range []string{"не записано правило", "-reseed"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("в отказе нет %q: %v", want, err)
		}
	}
}

// Generate на регионе с чужим правилом не пишет ни одного чанка: инвариант базы
// держится отказом, а не порядком вызовов.
func TestGenerateRefusesForeignRule(t *testing.T) {
	s := emptyStore(t)
	if err := s.PutRegion(worldstore.Region{
		ID: "ST_A", Frame: "{}", Epoch: 1,
		Rule: chunk.Rule{Level0RadiusM: 512, MaxLevel: 4},
	}); err != nil {
		t.Fatalf("регион: %v", err)
	}
	if _, err := Generate(s, stationWithExtent(256, 3), "ST_A", 1); err == nil {
		t.Fatal("порождение по чужому правилу не отвергнуто")
	}
	for level := 0; level <= chunk.MaxLevelLimit; level++ {
		if n, err := s.CountChunks("ST_A", level); err != nil {
			t.Fatal(err)
		} else if n != 0 {
			t.Fatalf("уровень %d: записано %d чанков после отказа", level, n)
		}
	}
}
