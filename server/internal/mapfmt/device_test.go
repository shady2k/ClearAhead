package mapfmt

import "testing"

func swFixture() Turnout {
	return Turnout{
		ID:    "ST_A_SW_1",
		Kind:  KindRail,
		Hand:  "right",
		Ports: TurnoutPorts{Common: "C", Straight: "S", Diverging: "D"},
	}
}

// Проходы стрелки — единственное место, где записано, что от чего идёт. До
// этого набор был выписан руками в трёх местах (intersect, propagate, compile),
// и любое устройство с другим числом портов потребовало бы править все три.
func TestTurnoutPassages(t *testing.T) {
	ps := swFixture().Passages()
	if len(ps) != 2 {
		t.Fatalf("проходов %d, у обыкновенной стрелки два", len(ps))
	}

	want := []Passage{
		{ID: "ST_A_SW_1:straight", From: "ST_A_SW_1.C", To: "ST_A_SW_1.S", Branch: "straight"},
		{ID: "ST_A_SW_1:diverging", From: "ST_A_SW_1.C", To: "ST_A_SW_1.D", Branch: "diverging"},
	}
	for i, w := range want {
		if ps[i] != w {
			t.Fatalf("проход %d: %+v, ожидался %+v", i, ps[i], w)
		}
	}

	// Оба прохода начинаются в общем порту — инвариант стрелки (спека §10.3).
	if ps[0].From != ps[1].From {
		t.Fatalf("проходы начинаются в разных портах: %s и %s", ps[0].From, ps[1].From)
	}
}

// Порядок проходов входит в хеш компиляции, поэтому он часть контракта.
func TestPassageOrderIsStable(t *testing.T) {
	a := swFixture().Passages()
	b := swFixture().Passages()
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("порядок проходов не воспроизводится на позиции %d", i)
		}
	}
	if a[0].Branch != "straight" {
		t.Fatalf("первым идёт %q, ожидался прямой проход", a[0].Branch)
	}
}

// ElementEnds обязан покрыть и рёбра, и проходы: он заменил две одинаковые
// рукописные таблицы, и пропуск любой половины сломал бы распространение поз.
func TestElementEndsCoversEdgesAndPassages(t *testing.T) {
	m := &Map{
		Topology: Topology{
			Edges:    []Edge{{ID: "E1", Kind: KindRail, From: "N1.P1", To: "ST_A_SW_1.C"}},
			Turnouts: []Turnout{swFixture()},
		},
	}
	ends := m.ElementEnds()
	if len(ends) != 3 {
		t.Fatalf("элементов %d, ожидалось три: ребро и два прохода", len(ends))
	}
	if got := ends["E1"]; got != [2]string{"N1.P1", "ST_A_SW_1.C"} {
		t.Fatalf("концы ребра %v", got)
	}
	if got := ends["ST_A_SW_1:diverging"]; got != [2]string{"ST_A_SW_1.C", "ST_A_SW_1.D"} {
		t.Fatalf("концы бокового прохода %v", got)
	}
}
