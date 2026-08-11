package track

import (
	"math"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/seedmap"
)

// reversedApproach разворачивает запись подхода к общему порту SW1: было
// ребро, приходящее в C, стало ребро, выходящее из C.
//
// Якорь НЕ меняется. Поза якоря смотрит внутрь своего элемента, а не «по ходу
// движения»: на конце To она указывает назад в ребро, на конце From — вперёд. В
// обоих вариантах это одно и то же направление, поэтому физическая станция одна
// и та же, записанная с разных концов, и геометрия обязана совпасть.
func reversedApproach() seedmap.Option {
	return seedmap.Mutate(func(m *mapfmt.Map) {
		for i, e := range m.Topology.Edges {
			if e.ID == seedmap.StationApproach {
				m.Topology.Edges[i] = mapfmt.Edge{ID: e.ID, Kind: e.Kind, From: e.To, To: e.From}
			}
		}
	})
}

// TestTurnoutBothOrientations — обе ориентации внешнего ребра у общего порта
// законны и обязаны давать одну геометрию. Именно этой проверки не было, и
// потому дефект ориентации дожил до карты станции.
func TestTurnoutBothOrientations(t *testing.T) {
	posesTo, _, err := Propagate(seedmap.Station())
	if err != nil {
		t.Fatalf("ребро приходит в C: %v", err)
	}
	posesFrom, _, err := Propagate(valid(t, seedmap.Station(reversedApproach())))
	if err != nil {
		t.Fatalf("ребро выходит из C: %v", err)
	}
	for _, inc := range []Incidence{
		{Port: seedmap.StationSW1 + ".S", Element: seedmap.StationSW1 + mapfmt.PassageStraight},
		{Port: seedmap.StationSW1 + ".D", Element: seedmap.StationSW1 + mapfmt.PassageDiverging},
		{Port: "N_STOP_MAIN.P1", Element: seedmap.StationMain},
		{Port: seedmap.StationSW2 + ".C", Element: seedmap.StationCross},
	} {
		a, okA := posesTo[inc]
		b, okB := posesFrom[inc]
		// Опечатка в имени конца дала бы две нулевые позы и тест, проходящий
		// вхолостую.
		if !okA || !okB {
			t.Fatalf("%s: позы нет (прямая запись %v, перевёрнутая %v)", inc, okA, okB)
		}
		if math.Hypot(a.Plan.X-b.Plan.X, a.Plan.Y-b.Plan.Y) > 1e-6 {
			t.Fatalf("%s: (%.4f, %.4f) против (%.4f, %.4f) — ориентация внешнего ребра меняет геометрию",
				inc, a.Plan.X, a.Plan.Y, b.Plan.X, b.Plan.Y)
		}
	}
}

// TestTurnoutCommonPortDirections — прямой и отклонённый проходы в общем порту
// смотрят в ОДНУ сторону, а внешнее ребро — в противоположную.
func TestTurnoutCommonPortDirections(t *testing.T) {
	poses, _, err := Propagate(seedmap.Station())
	if err != nil {
		t.Fatalf("распространение: %v", err)
	}
	common := seedmap.StationSW1 + ".C"
	s := poses[Incidence{Port: common, Element: seedmap.StationSW1 + mapfmt.PassageStraight}]
	d := poses[Incidence{Port: common, Element: seedmap.StationSW1 + mapfmt.PassageDiverging}]
	e := poses[Incidence{Port: common, Element: seedmap.StationApproach}]
	if math.Abs(s.Plan.Heading-d.Plan.Heading) > 1e-9 {
		t.Fatalf("проходы в общем порту смотрят врозь: %.6f против %.6f", s.Plan.Heading, d.Plan.Heading)
	}
	diff := math.Abs(math.Abs(s.Plan.Heading-e.Plan.Heading) - math.Pi)
	if diff > 1e-9 {
		t.Fatalf("внешнее ребро не противоположно проходам: %.6f против %.6f", e.Plan.Heading, s.Plan.Heading)
	}
}
