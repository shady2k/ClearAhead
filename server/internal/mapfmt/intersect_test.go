package mapfmt_test

import (
	"math"
	"strings"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/seedmap"
)

// Пересечение осей в плане: две оси имеют общую точку, не объяснённую
// топологией. Карты строятся twoComponents — два ребра без общего порта, каждое
// со своим якорем, — и вся разница между случаями в геометрии и позе второго.

// TestAxesIntersectInTheMiddle — заведомое пересечение в середине: E1 идёт на
// восток по y=0, E2 — на север из (50,-50). Общая точка (50, 0) не объяснена
// топологией: у пары нет общего порта.
func TestAxesIntersectInTheMiddle(t *testing.T) {
	m := twoComponents(
		mapfmt.Anchor{},
		[]mapfmt.HPrim{straight(100)},
		mapfmt.Anchor{X: 50, Y: -50, Heading: math.Pi / 2},
		[]mapfmt.HPrim{straight(100)},
	)
	rejects(t, m, seedmap.LineEdgeID+" x "+secondEdge+" в (50.0, 0.0)")
}

// TestAxisEndsAtAnotherAxisMiddle — конец пути, упирающийся в середину другого
// без топологической связи: E2 длиной 50 от (50,-50) на север кончается в
// (50, 0), на середине E1.
func TestAxisEndsAtAnotherAxisMiddle(t *testing.T) {
	m := twoComponents(
		mapfmt.Anchor{},
		[]mapfmt.HPrim{straight(100)},
		mapfmt.Anchor{X: 50, Y: -50, Heading: math.Pi / 2},
		[]mapfmt.HPrim{straight(50)},
	)
	rejects(t, m, seedmap.LineEdgeID+" x "+secondEdge)
}

// TestAxesTouchWithoutCrossing — касание вне разрешённого порта. E1 идёт на
// север по x=0. E2 — дуга радиуса 100 с центром (0,100) от 120° до 60° (по
// часовой); её вершина (0,200) касается середины E1.
func TestAxesTouchWithoutCrossing(t *testing.T) {
	m := twoComponents(
		mapfmt.Anchor{Heading: math.Pi / 2},
		[]mapfmt.HPrim{straight(300)},
		// Начало дуги: центр (0,100) плюс радиус под 120°, курс — касательная.
		mapfmt.Anchor{X: -50, Y: 100 + 100*math.Sqrt(3)/2, Heading: math.Pi / 6},
		[]mapfmt.HPrim{arc(100, -math.Pi/3)},
	)
	rejects(t, m, seedmap.LineEdgeID+" x "+secondEdge+" в (0.0, 200.0)")
}

// TestAxesOverlap — коллинеарное наложение: E2 лежит на линии E1 с 30-го по
// 100-й метр. Общая ось записана дважды.
func TestAxesOverlap(t *testing.T) {
	m := twoComponents(
		mapfmt.Anchor{},
		[]mapfmt.HPrim{straight(100)},
		mapfmt.Anchor{X: 30},
		[]mapfmt.HPrim{straight(100)},
	)
	rejects(t, m, "налагаются")
}

// TestAxisSelfIntersects — цепочка одного элемента возвращается по себе:
// прямая на восток до (100, 0), дуга на π вниз до (100, -100) и дуга на π по
// той же окружности обратно. Конец цепочки приходит ровно в (100, 0) — в стык
// прямой и первой дуги, который построением не объяснён. Второе ребро отставлено
// далеко: предмет теста — сама цепочка.
func TestAxisSelfIntersects(t *testing.T) {
	m := twoComponents(
		mapfmt.Anchor{},
		[]mapfmt.HPrim{straight(100), arc(50, -math.Pi), arc(50, -math.Pi)},
		mapfmt.Anchor{X: 500},
		[]mapfmt.HPrim{straight(100)},
	)
	// Радиус 50 м ниже нормы профиля, но отказ обязан прийти раньше и по
	// геометрии: модуль норм зовётся последним.
	text := refusal(t, m)
	if !strings.Contains(text, seedmap.LineEdgeID+" самопересекается в (100.0, 0.0)") {
		t.Fatalf("ожидался отказ по самопересечению E1 в (100,0), получено: %s", text)
	}
}

// TestBothIntersectionPointsAreReported — прямая пересекает полную окружность
// дважды: в (50, 0) и (250, 0). Обе точки обязаны попасть в отчёт: назвать одну
// значит послать автора карты чинить полдефекта.
func TestBothIntersectionPointsAreReported(t *testing.T) {
	m := twoComponents(
		mapfmt.Anchor{},
		[]mapfmt.HPrim{straight(300)},
		mapfmt.Anchor{X: 150, Y: -100},
		[]mapfmt.HPrim{arc(100, 2*math.Pi)},
	)
	text := refusal(t, m)
	for _, point := range []string{"в (50.0, 0.0)", "в (250.0, 0.0)"} {
		if !strings.Contains(text, point) {
			t.Fatalf("ожидалась точка %q, получено: %s", point, text)
		}
	}
}

// TestJointInSharedPortIsAllowed — два коллинеарных ребра, стыкующихся концами в
// общем порту: общая точка объяснена топологией и разрешена.
func TestJointInSharedPortIsAllowed(t *testing.T) {
	m := seedmap.Line(seedmap.WithoutConstruction(), seedmap.Mutate(func(m *mapfmt.Map) {
		end := m.Topology.Edges[0].To
		m.Topology.Nodes = append(m.Topology.Nodes,
			mapfmt.Node{ID: tID03, Name: "NC", Ports: []mapfmt.Port{{ID: "P1", Purpose: "buffer_stop"}}})
		m.Topology.Edges = append(m.Topology.Edges, mapfmt.Edge{ID: secondEdge, Name: "E2", Kind: mapfmt.KindRail, From: end, To: tID03 + ".P1"})
		m.Geometry.Edges[seedmap.LineEdgeID] = mapfmt.Alignments{Horizontal: []mapfmt.HPrim{straight(100)}}
		m.Geometry.Edges[secondEdge] = mapfmt.Alignments{Horizontal: []mapfmt.HPrim{straight(100)}}
	}))
	accepts(t, m)
}

// TestParallelTracksAreAllowed — два ребра между одними портами, оси которых
// не совпадают: параллельный путь законен, это не наложение.
func TestParallelTracksAreAllowed(t *testing.T) {
	m := seedmap.Line(seedmap.WithoutConstruction(), seedmap.Mutate(func(m *mapfmt.Map) {
		first := m.Topology.Edges[0]
		m.Topology.Edges = append(m.Topology.Edges,
			mapfmt.Edge{ID: secondEdge, Name: "E2", Kind: mapfmt.KindRail, From: first.From, To: first.To})
		m.Geometry.Edges[seedmap.LineEdgeID] = mapfmt.Alignments{Horizontal: []mapfmt.HPrim{straight(100)}}
		m.Geometry.Edges[secondEdge] = mapfmt.Alignments{Horizontal: []mapfmt.HPrim{arc(200, 0.5)}}
		// В порту сходятся два элемента: якорь обязан назвать тот, ВНУТРЬ
		// которого смотрит курс, иначе «направление порта» не определено.
		m.Anchors = map[string]mapfmt.Anchor{
			first.From: {Element: seedmap.LineEdgeID},
		}
	}))
	accepts(t, m)
}
