package track

import (
	"math"
	"strings"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
)

// twoEdges — N1 --E1-- N2 --E2-- N3, прямая 100 + прямая 50.
// N2.P1 — стык: им пользуются оба ребра, и именно там проверяется замыкание.
const twoEdges = `{
  "format_version": 1, "map_id": "T", "map_revision": 1,
  "anchors": { "N1.P1": { "x": 0, "y": 0, "z": 10, "heading": 0 } },
  "topology": {
    "nodes": [
      { "id": "N1", "ports": [ { "id": "P1", "purpose": "map_boundary" } ] },
      { "id": "N2", "ports": [ { "id": "P1" } ] },
      { "id": "N3", "ports": [ { "id": "P1", "purpose": "buffer_stop" } ] }
    ],
    "turnouts": [], "trackside": [],
    "edges": [
      { "id": "E1", "from": "N1.P1", "to": "N2.P1" },
      { "id": "E2", "from": "N2.P1", "to": "N3.P1" }
    ]
  },
  "geometry": { "turnouts": {}, "edges": {
    "E1": { "horizontal": [ { "kind": "straight", "length": 100.0 } ] },
    "E2": { "horizontal": [ { "kind": "straight", "length": 50.0 } ] }
  } }
}`

func loadMap(t *testing.T, doc string) *mapfmt.Map {
	t.Helper()
	m, err := mapfmt.Decode(strings.NewReader(doc))
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if err := mapfmt.Validate(m); err != nil {
		t.Fatalf("валидация: %v", err)
	}
	return m
}

func TestPropagateChain(t *testing.T) {
	poses, els, err := Propagate(loadMap(t, twoEdges))
	if err != nil {
		t.Fatalf("распространение: %v", err)
	}
	// Конец E1 в порту N2.P1: 100 м по X. Heading смотрит внутрь E1, то есть назад.
	p := poses[Incidence{Port: "N2.P1", Element: "E1"}]
	if math.Abs(p.Plan.X-100) > 1e-6 || math.Abs(p.Plan.Y) > 1e-6 {
		t.Fatalf("N2.P1 в (%v, %v), ожидалось (100, 0)", p.Plan.X, p.Plan.Y)
	}
	if math.Abs(math.Abs(p.Plan.Heading)-math.Pi) > 1e-9 {
		t.Fatalf("heading N2.P1 = %v, ожидалось ±π", p.Plan.Heading)
	}
	if math.Abs(p.Z-10) > 1e-9 {
		t.Fatalf("z N2.P1 = %v, ожидалось 10 (профиля нет)", p.Z)
	}
	if len(els) != 2 {
		t.Fatalf("элементов %d, ожидалось 2", len(els))
	}
}

func TestPropagateRejectsUnanchored(t *testing.T) {
	doc := strings.Replace(twoEdges, `"anchors": { "N1.P1": { "x": 0, "y": 0, "z": 10, "heading": 0 } }`,
		`"anchors": {}`, 1)
	m, err := mapfmt.Decode(strings.NewReader(doc))
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if err := mapfmt.Validate(m); err == nil {
		if _, _, err := Propagate(m); err == nil {
			t.Fatal("ожидался отказ: компонента без якоря")
		}
	}
}

// ringWith строит кольцо из четырёх дуг на π/2: замкнутую окружность радиуса
// 50 м, у которой у последней дуги радиус подменён на lastRadius.
//
// Углы не трогаются, поэтому направление сходится точно при любом радиусе, а
// расходится только положение — так зонд бьёт ровно в допуск по положению и
// ничего не смешивает. Ошибка замыкания при подмене ΔR равна ΔR·√2.
func ringWith(lastRadius string) string {
	return `{
	  "format_version": 1, "map_id": "C", "map_revision": 1,
	  "anchors": { "N1.P1": { "element": "E1", "x": 0, "y": 0, "z": 0, "heading": 0 } },
	  "topology": {
	    "nodes": [
	      { "id": "N1", "ports": [ { "id": "P1" } ] },
	      { "id": "N2", "ports": [ { "id": "P1" } ] },
	      { "id": "N3", "ports": [ { "id": "P1" } ] },
	      { "id": "N4", "ports": [ { "id": "P1" } ] }
	    ],
	    "turnouts": [], "trackside": [],
	    "edges": [
	      { "id": "E1", "from": "N1.P1", "to": "N2.P1" },
	      { "id": "E2", "from": "N2.P1", "to": "N3.P1" },
	      { "id": "E3", "from": "N3.P1", "to": "N4.P1" },
	      { "id": "E4", "from": "N4.P1", "to": "N1.P1" }
	    ]
	  },
	  "geometry": { "turnouts": {}, "edges": {
	    "E1": { "horizontal": [ { "kind": "arc", "radius": 50.0, "angle": 1.5707963267948966 } ] },
	    "E2": { "horizontal": [ { "kind": "arc", "radius": 50.0, "angle": 1.5707963267948966 } ] },
	    "E3": { "horizontal": [ { "kind": "arc", "radius": 50.0, "angle": 1.5707963267948966 } ] },
	    "E4": { "horizontal": [ { "kind": "arc", "radius": ` + lastRadius + `, "angle": 1.5707963267948966 } ] }
	  } }
	}`
}

// TestPropagateClosingCycle — положительный случай: кольцо, которое сходится.
//
// Без него тест на невязку бесполезен: проверка, которая отвергает всё подряд,
// тоже «ловит расхождение».
func TestPropagateClosingCycle(t *testing.T) {
	if _, _, err := Propagate(loadMap(t, ringWith("50.0"))); err != nil {
		t.Fatalf("замкнутое кольцо должно приниматься, получен отказ: %v", err)
	}
}

// TestPropagateClosureWithinTolerance — кольцо с невязкой 0,7 мм принимается.
//
// Вместе с TestPropagateClosureMismatch это зонд по обе стороны границы: без
// него допуск мог бы быть нулевым, и проверка отвергала бы любую честную карту.
// ΔR = 0,5 мм даёт невязку 0,5·√2 ≈ 0,71 мм — под допуском 1 мм.
func TestPropagateClosureWithinTolerance(t *testing.T) {
	if _, _, err := Propagate(loadMap(t, ringWith("50.0005"))); err != nil {
		t.Fatalf("невязка 0,71 мм под допуском 1 мм должна приниматься, получен отказ: %v", err)
	}
}

// TestPropagateClosureMismatch — кольцо с невязкой 7 мм отвергается.
//
// Первая редакция этого теста строила «треугольник» из трёх прямых одного
// направления. Он не смыкался вовсе — расхождение было в сотни метров, то есть
// тест доказывал лишь, что проверка отвергает заведомый мусор, и о допуске в
// 1 мм не говорил ничего. Найдено воркером при реализации.
//
// ΔR = 5 мм даёт невязку 5·√2 ≈ 7,07 мм — семь допусков, а не триста тысяч.
func TestPropagateClosureMismatch(t *testing.T) {
	_, _, err := Propagate(loadMap(t, ringWith("50.005")))
	if err == nil {
		t.Fatal("ожидался отказ по невязке замыкания")
	}
	if !strings.Contains(err.Error(), "невязк") {
		t.Fatalf("ошибка не про невязку: %v", err)
	}
	// Число в сообщении должно быть миллиметрами того же порядка: иначе зонд
	// снова бьёт мимо границы, а мы этого не заметим.
	if !strings.Contains(err.Error(), "7.0") {
		t.Fatalf("в сообщении ожидалась невязка около 7 мм, получено: %v", err)
	}
}
