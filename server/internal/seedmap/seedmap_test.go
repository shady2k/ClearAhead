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
func TestФабрикаПорождаетВалидныеКарты(t *testing.T) {
	случаи := map[string]*mapfmt.Map{
		"перегон":                 Line(),
		"перегон с рельефом":      Line(WithTerrain()),
		"перегон без решётки":     Line(WithoutConstruction()),
		"станция":                 Station(),
		"станция с рельефом":      Station(WithTerrain()),
		"станция без решётки":     Station(WithoutConstruction()),
		"перегон с мостом":        Line(WithTerrain(), WithStructure("bridge", "MOST", LineEdgeID, 50, 150)),
		"перегон с тоннелем":      Line(WithTerrain(), WithStructure("tunnel", "TONNEL", LineEdgeID, 0, 80)),
		"станция иначе названная": Station(WithID("DRUGAYA"), WithRevision(7)),
	}
	for имя, m := range случаи {
		if err := mapfmt.Validate(m); err != nil {
			t.Errorf("%s: %v", имя, err)
		}
	}
}

// Карта фабрики должна не только проходить валидацию, но и компилироваться:
// замыкание циклов и распространение поз — отдельный класс требований, и
// фикстура, ломающаяся на них, бесполезна для тестов компилятора.
func TestКартыФабрикиКомпилируются(t *testing.T) {
	for имя, m := range map[string]*mapfmt.Map{"перегон": Line(), "станция": Station()} {
		ct, rg, err := track.Compile(m)
		if err != nil {
			t.Fatalf("%s: компиляция: %v", имя, err)
		}
		if len(ct.Elements) == 0 || len(rg.Elements) == 0 {
			t.Fatalf("%s: пусто после компиляции", имя)
		}
	}
}

// У станции обязаны быть устройства и проходы: если фабрика выродится в набор
// прямых, тесты топологии перестанут что-либо проверять, оставаясь зелёными.
func TestСтанцияНесётУстройства(t *testing.T) {
	ct, _, err := track.Compile(Station())
	if err != nil {
		t.Fatalf("компиляция: %v", err)
	}
	if len(ct.Devices) != 2 {
		t.Fatalf("устройств %d, ожидалось 2", len(ct.Devices))
	}
	for id, d := range ct.Devices {
		if len(d.Traversals) != 2 {
			t.Fatalf("%s: переходов %d", id, len(d.Traversals))
		}
	}
	// Кривая на главном пути — не украшение: без неё не проверяются ни дуги,
	// ни пересечения осей, ни крестовины.
	if len(ct.Elements) < 7 {
		t.Fatalf("элементов %d: станция выродилась", len(ct.Elements))
	}
}

// Опция, ломающая карту, обязана её ломать — иначе тесты валидатора, которые
// на неё опираются, проходили бы вхолостую.
func TestПорчаДействительноЛомает(t *testing.T) {
	m := Station(Mutate(func(m *mapfmt.Map) { m.MapID = "ПЛОХОЙ:ID" }))
	if err := mapfmt.Validate(m); err == nil {
		t.Fatal("испорченная карта прошла валидацию")
	}
}
