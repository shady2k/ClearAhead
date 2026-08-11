package seedmap

import (
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/track"
)

// ГЛАВНЫЙ ТЕСТ ФАБРИКИ. Всё, что она порождает без опций, обязано проходить
// валидацию целиком. Фикстура, переставшая быть валидной, обесценивает каждый
// тест, который её берёт, и падать это должно здесь — а не в чужом падении, где
// причину придётся искать.
func TestFactoryProducesValidMaps(t *testing.T) {
	cases := map[string]*mapfmt.Map{
		"перегон":                          Line(),
		"перегон с рельефом":               Line(WithTerrain()),
		"перегон без решётки":              Line(WithoutConstruction()),
		"станция":                          Station(),
		"станция с рельефом":               Station(WithTerrain()),
		"станция без решётки":              Station(WithoutConstruction()),
		"перегон с мостом":                 Line(WithTerrain(), WithCarryingStructure("bridge", "MOST", LineEdgeID, 50, 150)),
		"перегон с тоннелем":               Line(WithTerrain(), WithCarryingStructure("tunnel", "TONNEL", LineEdgeID, 0, 80)),
		"станция иначе названная":          Station(WithID("DRUGAYA"), WithRevision(7)),
		"кольцо":                           Ring(RingRadiusM),
		"перегон из двух рёбер":            Corridor(),
		"перегон из двух рёбер с рельефом": Corridor(WithTerrain()),
	}
	for name, m := range cases {
		if err := mapfmt.Validate(m); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// Карта фабрики должна не только проходить валидацию, но и компилироваться:
// замыкание циклов и распространение поз — отдельный класс требований, и
// фикстура, ломающаяся на них, бесполезна для тестов компилятора.
func TestFactoryMapsCompile(t *testing.T) {
	for name, m := range map[string]*mapfmt.Map{
		"перегон": Line(), "станция": Station(),
		"кольцо": Ring(RingRadiusM), "перегон из двух рёбер": Corridor(),
	} {
		cn, rg, err := track.Compile(m)
		if err != nil {
			t.Fatalf("%s: компиляция: %v", name, err)
		}
		if len(cn.Elements) == 0 || len(rg.Elements) == 0 {
			t.Fatalf("%s: пусто после компиляции", name)
		}
	}
}

// У станции обязаны быть устройства и проходы: если фабрика выродится в набор
// прямых, тесты топологии перестанут что-либо проверять, оставаясь зелёными.
func TestStationCarriesDevices(t *testing.T) {
	cn, _, err := track.Compile(Station())
	if err != nil {
		t.Fatalf("компиляция: %v", err)
	}
	if len(cn.Devices) != 2 {
		t.Fatalf("устройств %d, ожидалось 2", len(cn.Devices))
	}
	for id, d := range cn.Devices {
		if len(d.Traversals) != 2 {
			t.Fatalf("%s: переходов %d", id, len(d.Traversals))
		}
	}
	// Кривая на главном пути — не украшение: без неё не проверяются ни дуги,
	// ни пересечения осей, ни крестовины.
	if len(cn.Elements) < 7 {
		t.Fatalf("элементов %d: станция выродилась", len(cn.Elements))
	}
}

// Опция, ломающая карту, обязана её ломать — иначе тесты валидатора, которые
// на неё опираются, проходили бы вхолостую.
func TestCorruptionActuallyBreaks(t *testing.T) {
	m := Station(Mutate(func(m *mapfmt.Map) { m.MapID = "ПЛОХОЙ:ID" }))
	if err := mapfmt.Validate(m); err == nil {
		t.Fatal("испорченная карта прошла валидацию")
	}
}

// Кольцо обязано СХОДИТЬСЯ при штатном радиусе и расходиться при изменённом —
// иначе проверка невязки замыкания холостая с обеих сторон.
func TestRingClosesAndDiverges(t *testing.T) {
	if _, _, err := track.Propagate(Ring(RingRadiusM)); err != nil {
		t.Fatalf("замкнутое кольцо отвергнуто: %v", err)
	}
	// ΔR = 5 мм даёт невязку 5·√2 ≈ 7 мм — заведомо больше допуска 1 мм.
	if _, _, err := track.Propagate(Ring(RingRadiusM + 0.005)); err == nil {
		t.Fatal("кольцо с невязкой 7 мм принято")
	}
}

// У перегона из двух рёбер обязан быть обычный порт, где сходятся ДВА ребра:
// в этом весь смысл фикстуры, и вырождение её в один элемент оставило бы
// ветку переноса позы через такой порт без покрытия незаметно.
func TestTwoEdgeLineHasPlainJoint(t *testing.T) {
	m := Corridor()
	ends := 0
	for _, e := range m.Topology.Edges {
		if e.From == CorridorJoint || e.To == CorridorJoint {
			ends++
		}
	}
	if ends != 2 {
		t.Fatalf("в стыке %s сходится %d концов, ожидалось 2", CorridorJoint, ends)
	}
}

// Заготовка новой карты отличается от перегона СМЫСЛОМ концов: западный —
// граница карты, оттуда придёт перегон; восточный — тупиковый упор. Стереть
// это различие легко и незаметно.
func TestBlankCarriesEndPurposes(t *testing.T) {
	m := Blank()
	purposes := map[string]string{}
	for _, n := range m.Topology.Nodes {
		for _, p := range n.Ports {
			purposes[n.ID+"."+p.ID] = p.Purpose
		}
	}
	if purposes[BlankWest+".P1"] != "map_boundary" {
		t.Fatalf("западный конец: %q, ожидалась граница карты", purposes[BlankWest+".P1"])
	}
	if purposes[BlankEast+".P1"] != "buffer_stop" {
		t.Fatalf("восточный конец: %q, ожидался тупиковый упор", purposes[BlankEast+".P1"])
	}
}
