package track

import (
	"fmt"
	"math"
	"testing"
)

// oneTurnout строит карту с одной стрелкой. Внешнее ребро у общего порта
// ориентировано по параметру: "to" — приходит в C (западный узор), "from" —
// выходит из C (восточный узор).
//
// Геометрия в обоих случаях одна и та же физически, поэтому и позы портов S и D
// обязаны совпасть. Именно этой проверки не было, и потому дефект ориентации
// дожил до карты станции.
func oneTurnout(dir string) string {
	approach := `{ "id": "EA", "from": "NW.P1", "to": "SW1.C" }`
	anchor := `"anchors": { "NW.P1": { "x": 0, "y": 0, "z": 0, "heading": 0 } }`
	if dir == "from" {
		approach = `{ "id": "EA", "from": "SW1.C", "to": "NW.P1" }`
		// Якорь НЕ меняется. Поза якоря смотрит внутрь своего элемента, а не
		// «по ходу движения»: на конце To она указывает назад в ребро, на конце
		// From — вперёд. В обоих вариантах это одно и то же направление, поэтому
		// физическая станция одна и та же, записанная с разных концов.
	}
	return fmt.Sprintf(`{
	  "format_version": 2, "map_id": "T1", "map_revision": 1,
	  %s,
	  "topology": {
	    "nodes": [
	      { "id": "NW", "ports": [ { "id": "P1", "purpose": "map_boundary" } ] },
	      { "id": "NS", "ports": [ { "id": "P1", "purpose": "buffer_stop" } ] },
	      { "id": "ND", "ports": [ { "id": "P1", "purpose": "buffer_stop" } ] }
	    ],
	    "turnouts": [
	      { "id": "SW1", "hand": "right", "frog": "1/9",
	        "ports": { "common": "C", "straight": "S", "diverging": "D" } }
	    ],
	    "trackside": [],
	    "edges": [
	      %s,
	      { "id": "ES", "from": "SW1.S", "to": "NS.P1" },
	      { "id": "ED", "from": "SW1.D", "to": "ND.P1" }
	    ]
	  },
	  "geometry": { "turnouts": {
	      "SW1": {
	        "straight":  { "horizontal": [ { "kind": "straight", "length": 33.5 } ] },
	        "diverging": { "horizontal": [ { "kind": "arc", "radius": 300.0, "angle": -0.1107 } ] }
	      }
	    },
	    "edges": {
	      "EA": { "horizontal": [ { "kind": "straight", "length": 100.0 } ] },
	      "ES": { "horizontal": [ { "kind": "straight", "length": 200.0 } ] },
	      "ED": { "horizontal": [ { "kind": "straight", "length": 200.0 } ] }
	    }
	  }
	}`, anchor, approach)
}

// TestTurnoutBothOrientations — обе ориентации внешнего ребра у общего порта
// законны и обязаны давать одну геометрию.
func TestTurnoutBothOrientations(t *testing.T) {
	posesTo, _, err := Propagate(loadMap(t, oneTurnout("to")))
	if err != nil {
		t.Fatalf("ребро приходит в C: %v", err)
	}
	posesFrom, _, err := Propagate(loadMap(t, oneTurnout("from")))
	if err != nil {
		t.Fatalf("ребро выходит из C: %v", err)
	}
	for _, inc := range []Incidence{
		{Port: "SW1.S", Element: "SW1:straight"},
		{Port: "SW1.D", Element: "SW1:diverging"},
		{Port: "NS.P1", Element: "ES"},
		{Port: "ND.P1", Element: "ED"},
	} {
		a, b := posesTo[inc], posesFrom[inc]
		if math.Hypot(a.Plan.X-b.Plan.X, a.Plan.Y-b.Plan.Y) > 1e-6 {
			t.Fatalf("%s: (%.4f, %.4f) против (%.4f, %.4f) — ориентация внешнего ребра меняет геометрию",
				inc, a.Plan.X, a.Plan.Y, b.Plan.X, b.Plan.Y)
		}
	}
}

// TestTurnoutCommonPortDirections — прямой и отклонённый проходы в общем порту
// смотрят в ОДНУ сторону, а внешнее ребро — в противоположную.
func TestTurnoutCommonPortDirections(t *testing.T) {
	poses, _, err := Propagate(loadMap(t, oneTurnout("to")))
	if err != nil {
		t.Fatalf("распространение: %v", err)
	}
	s := poses[Incidence{Port: "SW1.C", Element: "SW1:straight"}]
	d := poses[Incidence{Port: "SW1.C", Element: "SW1:diverging"}]
	e := poses[Incidence{Port: "SW1.C", Element: "EA"}]
	if math.Abs(s.Plan.Heading-d.Plan.Heading) > 1e-9 {
		t.Fatalf("проходы в общем порту смотрят врозь: %.6f против %.6f", s.Plan.Heading, d.Plan.Heading)
	}
	diff := math.Abs(math.Abs(s.Plan.Heading-e.Plan.Heading) - math.Pi)
	if diff > 1e-9 {
		t.Fatalf("внешнее ребро не противоположно проходам: %.6f против %.6f", e.Plan.Heading, s.Plan.Heading)
	}
}
