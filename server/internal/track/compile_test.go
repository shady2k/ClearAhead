package track

import (
	"strings"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
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

// turnoutWithTrackside — oneTurnout("to") плюс платформа на ребре EA.
//
// Профиль EA начинается и кончается нулевым уклоном (замыкание с якорем и с
// проходами стрелки), а в середине уходит в 60‰: пространственная координата s
// расходится с u, поэтому тест видит, что спаны клиента взяты в u из карты, а
// симуляции — в s.
func turnoutWithTrackside(t *testing.T) string {
	t.Helper()
	doc := strings.Replace(oneTurnout("to"),
		`"EA": { "horizontal": [ { "kind": "straight", "length": 100.0 } ] }`,
		`"EA": { "horizontal": [ { "kind": "straight", "length": 100.0 } ],
		  "vertical": [
		    { "kind": "grade", "length": 20.0, "slope_permille": 0.0 },
		    { "kind": "vertical_curve", "length": 60.0, "end_slope_permille": 60.0 },
		    { "kind": "grade", "length": 10.0, "slope_permille": 60.0 },
		    { "kind": "vertical_curve", "length": 10.0, "end_slope_permille": 0.0 }
		  ] }`, 1)
	doc = strings.Replace(doc, `"trackside": [],`,
		`"trackside": [ { "id": "TSP", "kind": "platform", "side": "right",
		  "span": [ { "element": "EA", "from": 10.0, "to": 90.0 } ] } ],`, 1)
	return doc
}
func TestCompileRenderRole(t *testing.T) {
	_, rg, err := Compile(loadMap(t, turnoutWithTrackside(t)))
	if err != nil {
		t.Fatalf("компиляция: %v", err)
	}
	byID := make(map[string]RenderElement, len(rg.Elements))
	for _, e := range rg.Elements {
		byID[e.ID] = e
	}
	for _, tc := range []struct {
		id     string
		branch string
	}{
		{"SW1:straight", "straight"},
		{"SW1:diverging", "diverging"},
	} {
		e := byID[tc.id]
		if e.Role == nil {
			t.Fatalf("%s: роль не назначена", tc.id)
		}
		if e.Role.Turnout != "SW1" || e.Role.Branch != tc.branch ||
			e.Role.Hand != "right" || e.Role.Frog != "1/9" {
			t.Fatalf("%s: роль %+v, ожидалась ветвь %s стрелки SW1 right 1/9",
				tc.id, e.Role, tc.branch)
		}
	}
	for _, id := range []string{"EA", "ES", "ED"} {
		if e := byID[id]; e.Role != nil {
			t.Fatalf("обычный путь %s получил роль %+v", id, e.Role)
		}
	}
}

// TestCompileTracksideSpansInU — спаны клиента в координате u, ровно как в
// карте; симуляционный спан в s и на уклоне длиннее. Конвертировать обратно
// из s нельзя: для плоской станции они совпадают, в общем случае нет.
func TestCompileTracksideSpansInU(t *testing.T) {
	ct, rg, err := Compile(loadMap(t, turnoutWithTrackside(t)))
	if err != nil {
		t.Fatalf("компиляция: %v", err)
	}
	if len(rg.Trackside) != 1 {
		t.Fatalf("в RenderGeometry %d путевых объектов, ожидался 1", len(rg.Trackside))
	}
	ts := rg.Trackside[0]
	if ts.ID != "TSP" || ts.Kind != "platform" || ts.Side != "right" {
		t.Fatalf("объект %+v, ожидался TSP platform right", ts)
	}
	if len(ts.Spans) != 1 {
		t.Fatalf("у %s %d спанов, ожидался 1", ts.ID, len(ts.Spans))
	}
	sp := ts.Spans[0]
	if sp.Element != "EA" || sp.FromM != 10.0 || sp.ToM != 90.0 {
		t.Fatalf("спан клиента (%s, %v, %v) — ожидались значения u из карты (EA, 10, 90)",
			sp.Element, sp.FromM, sp.ToM)
	}
	ss := ct.Trackside["TSP"]
	if len(ss) != 1 {
		t.Fatalf("у CompiledTrack %d спанов, ожидался 1", len(ss))
	}
	// Начало в плоском участке: s == u. Конец на уклоне: s > u.
	if ss[0].FromS.Meters() != 10.0 || ss[0].ToS.Meters() <= 90.0 {
		t.Fatalf("симуляционный спан (%s, %v, %v) — начало в s==u, конец обязан превышать 90",
			ss[0].Element, ss[0].FromS.Meters(), ss[0].ToS.Meters())
	}
}

// TestCompileRequiresFrog — роль ветви в RenderGeometry обязана нести марку
// крестовины. Валидатор карты frog тоже требует (mapfmt), но Compile можно
// позвать и минуя Validate — и тогда роль без марки не должна уйти клиенту.
func TestCompileRequiresFrog(t *testing.T) {
	doc := strings.Replace(oneTurnout("to"), `"frog": "1/9",`, ``, 1)
	m, err := mapfmt.Decode(strings.NewReader(doc))
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if _, _, err := Compile(m); err == nil {
		t.Fatal("стрелка без frog скомпилировалась, а роль обязана нести марку крестовины")
	} else if !strings.Contains(err.Error(), "frog") {
		t.Fatalf("ошибка не про frog: %v", err)
	}
}
