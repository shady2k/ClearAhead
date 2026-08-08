package track

import (
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/units"
)

func TestCompileFlatLengths(t *testing.T) {
	ct, rg, err := Compile(loadMap(t, twoEdges))
	if err != nil {
		t.Fatalf("компиляция: %v", err)
	}
	e1 := ct.Elements["E1"]
	if e1.LengthS != 100*units.Meter || e1.LengthU != 100*units.Meter {
		t.Fatalf("E1: u=%s s=%s, ожидалось по 100m", e1.LengthU, e1.LengthS)
	}
	if len(rg.Elements) != 2 {
		t.Fatalf("в RenderGeometry %d элементов, ожидалось 2", len(rg.Elements))
	}
	if rg.Elements[0].ID != "E1" {
		t.Fatalf("порядок элементов не детерминирован: первый %s", rg.Elements[0].ID)
	}
}

// TestCompileRoundingRule закрепляет правило спеки §3: длина элемента — сумма
// индивидуально округлённых длин примитивов, а не округление суммы.
//
// Три отрезка по 0.0000005 м (полмикрометра). Каждый округляется вверх до 1 мкм
// (половины от нуля), сумма — 3 мкм. Округление математической суммы дало бы
// 1.5 мкм → 2 мкм. Разница видна и это ровно то место, где два компилятора
// разошлись бы.
func TestCompileRoundingRule(t *testing.T) {
	const doc = `{
	  "format_version": 1, "map_id": "R", "map_revision": 1,
	  "anchors": { "N1.P1": { "x": 0, "y": 0, "z": 0, "heading": 0 } },
	  "topology": {
	    "nodes": [
	      { "id": "N1", "ports": [ { "id": "P1", "purpose": "map_boundary" } ] },
	      { "id": "N2", "ports": [ { "id": "P1", "purpose": "buffer_stop" } ] }
	    ],
	    "turnouts": [], "trackside": [],
	    "edges": [ { "id": "E1", "from": "N1.P1", "to": "N2.P1" } ]
	  },
	  "geometry": { "turnouts": {}, "edges": { "E1": { "horizontal": [
	    { "kind": "straight", "length": 0.0000005 },
	    { "kind": "straight", "length": 0.0000005 },
	    { "kind": "straight", "length": 0.0000005 }
	  ] } } }
	}`
	ct, _, err := Compile(loadMap(t, doc))
	if err != nil {
		t.Fatalf("компиляция: %v", err)
	}
	if got := ct.Elements["E1"].LengthU; got != 3*units.Micrometer {
		t.Fatalf("длина %d мкм, ожидалось 3: правило округления не сумма округлённых", int64(got))
	}
}

func TestCompileDeterministic(t *testing.T) {
	a1, b1, err := Compile(loadMap(t, twoEdges))
	if err != nil {
		t.Fatalf("компиляция 1: %v", err)
	}
	a2, b2, err := Compile(loadMap(t, twoEdges))
	if err != nil {
		t.Fatalf("компиляция 2: %v", err)
	}
	if a1.Elements["E1"].LengthS != a2.Elements["E1"].LengthS {
		t.Fatal("длина зависит от запуска")
	}
	if b1.Elements[0].Start != b2.Elements[0].Start {
		t.Fatal("стартовая поза зависит от запуска")
	}
}
