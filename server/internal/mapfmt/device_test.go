package mapfmt

import "testing"

func swFixture() Turnout {
	return Turnout{
		ID:    uIDSW,
		Name:  "SW",
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
		{ID: uIDSW + ":straight", From: uIDSW + ".C", To: uIDSW + ".S", Branch: "straight"},
		{ID: uIDSW + ":diverging", From: uIDSW + ".C", To: uIDSW + ".D", Branch: "diverging"},
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
			Edges:    []Edge{{ID: uIDE1, Name: "E1", Kind: KindRail, From: uIDN1 + ".P1", To: uIDSW + ".C"}},
			Turnouts: []Turnout{swFixture()},
		},
	}
	ends := m.ElementEnds()
	if len(ends) != 3 {
		t.Fatalf("элементов %d, ожидалось три: ребро и два прохода", len(ends))
	}
	if got := ends[uIDE1]; got != [2]string{uIDN1 + ".P1", uIDSW + ".C"} {
		t.Fatalf("концы ребра %v", got)
	}
	if got := ends[uIDSW+":diverging"]; got != [2]string{uIDSW + ".C", uIDSW + ".D"} {
		t.Fatalf("концы бокового прохода %v", got)
	}
}
