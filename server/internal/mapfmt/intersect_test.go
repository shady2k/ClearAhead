package mapfmt_test

import (
	"math"
	"strings"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/seedmap"
)

// Пересечение осей в плане: две оси имеют общую точку, не объяснённую
// топологией. Карты строятся двеКомпоненты — два ребра без общего порта, каждое
// со своим якорем, — и вся разница между случаями в геометрии и позе второго.

// TestОсиПересекаютсяПосередине — заведомое пересечение в середине: E1 идёт на
// восток по y=0, E2 — на север из (50,-50). Общая точка (50, 0) не объяснена
// топологией: у пары нет общего порта.
func TestОсиПересекаютсяПосередине(t *testing.T) {
	m := двеКомпоненты(
		mapfmt.Anchor{},
		[]mapfmt.HPrim{прямая(100)},
		mapfmt.Anchor{X: 50, Y: -50, Heading: math.Pi / 2},
		[]mapfmt.HPrim{прямая(100)},
	)
	отвергает(t, m, "E1 x E2 в (50.0, 0.0)")
}

// TestОсьУпираетсяВЧужуюСередину — конец пути, упирающийся в середину другого
// без топологической связи: E2 длиной 50 от (50,-50) на север кончается в
// (50, 0), на середине E1.
func TestОсьУпираетсяВЧужуюСередину(t *testing.T) {
	m := двеКомпоненты(
		mapfmt.Anchor{},
		[]mapfmt.HPrim{прямая(100)},
		mapfmt.Anchor{X: 50, Y: -50, Heading: math.Pi / 2},
		[]mapfmt.HPrim{прямая(50)},
	)
	отвергает(t, m, "E1 x E2")
}

// TestОсиКасаютсяБезПересечения — касание вне разрешённого порта. E1 идёт на
// север по x=0. E2 — дуга радиуса 100 с центром (0,100) от 120° до 60° (по
// часовой); её вершина (0,200) касается середины E1.
func TestОсиКасаютсяБезПересечения(t *testing.T) {
	m := двеКомпоненты(
		mapfmt.Anchor{Heading: math.Pi / 2},
		[]mapfmt.HPrim{прямая(300)},
		// Начало дуги: центр (0,100) плюс радиус под 120°, курс — касательная.
		mapfmt.Anchor{X: -50, Y: 100 + 100*math.Sqrt(3)/2, Heading: math.Pi / 6},
		[]mapfmt.HPrim{дуга(100, -math.Pi/3)},
	)
	отвергает(t, m, "E1 x E2 в (0.0, 200.0)")
}

// TestОсиНалагаются — коллинеарное наложение: E2 лежит на линии E1 с 30-го по
// 100-й метр. Общая ось записана дважды.
func TestОсиНалагаются(t *testing.T) {
	m := двеКомпоненты(
		mapfmt.Anchor{},
		[]mapfmt.HPrim{прямая(100)},
		mapfmt.Anchor{X: 30},
		[]mapfmt.HPrim{прямая(100)},
	)
	отвергает(t, m, "налагаются")
}

// TestОсьСамопересекается — цепочка одного элемента возвращается по себе:
// прямая на восток до (100, 0), дуга на π вниз до (100, -100) и дуга на π по
// той же окружности обратно. Конец цепочки приходит ровно в (100, 0) — в стык
// прямой и первой дуги, который построением не объяснён. Второе ребро отставлено
// далеко: предмет теста — сама цепочка.
func TestОсьСамопересекается(t *testing.T) {
	m := двеКомпоненты(
		mapfmt.Anchor{},
		[]mapfmt.HPrim{прямая(100), дуга(50, -math.Pi), дуга(50, -math.Pi)},
		mapfmt.Anchor{X: 500},
		[]mapfmt.HPrim{прямая(100)},
	)
	// Радиус 50 м ниже нормы профиля, но отказ обязан прийти раньше и по
	// геометрии: модуль норм зовётся последним.
	текст := отказ(t, m)
	if !strings.Contains(текст, seedmap.LineEdgeID+" самопересекается в (100.0, 0.0)") {
		t.Fatalf("ожидался отказ по самопересечению E1 в (100,0), получено: %s", текст)
	}
}

// TestДвеТочкиПересеченияПопадаютВОтчёт — прямая пересекает полную окружность
// дважды: в (50, 0) и (250, 0). Обе точки обязаны попасть в отчёт: назвать одну
// значит послать автора карты чинить полдефекта.
func TestДвеТочкиПересеченияПопадаютВОтчёт(t *testing.T) {
	m := двеКомпоненты(
		mapfmt.Anchor{},
		[]mapfmt.HPrim{прямая(300)},
		mapfmt.Anchor{X: 150, Y: -100},
		[]mapfmt.HPrim{дуга(100, 2*math.Pi)},
	)
	текст := отказ(t, m)
	for _, точка := range []string{"в (50.0, 0.0)", "в (250.0, 0.0)"} {
		if !strings.Contains(текст, точка) {
			t.Fatalf("ожидалась точка %q, получено: %s", точка, текст)
		}
	}
}

// TestСтыкВОбщемПортуРазрешён — два коллинеарных ребра, стыкующихся концами в
// общем порту: общая точка объяснена топологией и разрешена.
func TestСтыкВОбщемПортуРазрешён(t *testing.T) {
	m := seedmap.Line(seedmap.WithoutConstruction(), seedmap.Mutate(func(m *mapfmt.Map) {
		конец := m.Topology.Edges[0].To
		m.Topology.Nodes = append(m.Topology.Nodes,
			mapfmt.Node{ID: "NC", Ports: []mapfmt.Port{{ID: "P1", Purpose: "buffer_stop"}}})
		m.Topology.Edges = append(m.Topology.Edges, mapfmt.Edge{ID: второеРебро, From: конец, To: "NC.P1"})
		m.Geometry.Edges[seedmap.LineEdgeID] = mapfmt.Alignments{Horizontal: []mapfmt.HPrim{прямая(100)}}
		m.Geometry.Edges[второеРебро] = mapfmt.Alignments{Horizontal: []mapfmt.HPrim{прямая(100)}}
	}))
	принимает(t, m)
}

// TestПараллельныеПутиРазрешены — два ребра между одними портами, оси которых
// не совпадают: параллельный путь законен, это не наложение.
func TestПараллельныеПутиРазрешены(t *testing.T) {
	m := seedmap.Line(seedmap.WithoutConstruction(), seedmap.Mutate(func(m *mapfmt.Map) {
		первое := m.Topology.Edges[0]
		m.Topology.Edges = append(m.Topology.Edges,
			mapfmt.Edge{ID: второеРебро, From: первое.From, To: первое.To})
		m.Geometry.Edges[seedmap.LineEdgeID] = mapfmt.Alignments{Horizontal: []mapfmt.HPrim{прямая(100)}}
		m.Geometry.Edges[второеРебро] = mapfmt.Alignments{Horizontal: []mapfmt.HPrim{дуга(200, 0.5)}}
		// В порту сходятся два элемента: якорь обязан назвать тот, ВНУТРЬ
		// которого смотрит курс, иначе «направление порта» не определено.
		m.Anchors = map[string]mapfmt.Anchor{
			первое.From: {Element: seedmap.LineEdgeID},
		}
	}))
	принимает(t, m)
}
