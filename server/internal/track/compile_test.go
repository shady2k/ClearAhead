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
	  "format_version": 2, "map_id": "R", "map_revision": 1,
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
		    { "kind": "vertical_curve", "length": 60.0, "end_slope_permille": 20.0 },
		    { "kind": "grade", "length": 10.0, "slope_permille": 20.0 },
		    { "kind": "vertical_curve", "length": 10.0, "end_slope_permille": 0.0 }
		  ] }`, 1)
	doc = strings.Replace(doc, `"trackside": [],`,
		`"trackside": [ { "id": "TSP", "kind": "platform", "side": "right",
		  "offset": 1.75, "width": 3.0,
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
	if ts.Offset != 1.75 || ts.Width != 3.0 {
		t.Fatalf("размеры платформы (%v, %v) — ожидались (1.75, 3.0) из карты", ts.Offset, ts.Width)
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

// TestCompileFrogOptional — марка крестовины необязательна и в карте, и в
// проводе (спека §7): роль ветви с опущенной маркой уходит клиенту, крестовина
// строится из особенности frog (§5), марка показывается подписью, если есть.
func TestCompileFrogOptional(t *testing.T) {
	doc := strings.Replace(oneTurnout("to"), `"frog": "1/9",`, ``, 1)
	m, err := mapfmt.Decode(strings.NewReader(doc))
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	_, rg, err := Compile(m)
	if err != nil {
		t.Fatalf("стрелка без марки обязана компилироваться: %v", err)
	}
	for _, e := range rg.Elements {
		if e.ID != "SW1:straight" && e.ID != "SW1:diverging" {
			continue
		}
		if e.Role == nil {
			t.Fatalf("%s: роль не назначена", e.ID)
		}
		if e.Role.Frog != "" {
			t.Fatalf("%s: марка %q должна была остаться опущенной", e.ID, e.Role.Frog)
		}
	}
}

// constructionTrackMap — oneTurnout("to") плюс блок construction: один тип,
// по run'у на каждое ребро. Проверяет перенос рецепта в провод.
const constructionTrackMap = `{
  "format_version": 2,
  "map_id": "T2",
  "map_revision": 1,
  "anchors": { "NW.P1": { "x": 0, "y": 0, "z": 0, "heading": 0 } },
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
      { "id": "EA", "from": "NW.P1", "to": "SW1.C" },
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
  },
  "construction": {
    "default_type": "TRACK_MAIN",
    "types": [ {
      "id": "TRACK_MAIN",
      "gauge": 1.435,
      "sleeper": { "pitch": 0.6, "length": 2.5, "width": 0.28 },
      "ballast": { "half_width": 1.75 }
    } ],
    "runs": [
      { "id": "RUN_B", "coordinate": "u", "phase": 0.0,
        "spans": [ { "element": "ES", "from": 0, "to": 200, "direction": "forward" } ] },
      { "id": "RUN_A", "coordinate": "u", "phase": 0.1,
        "spans": [ { "element": "EA", "from": 0, "to": 100, "direction": "forward" } ] },
      { "id": "RUN_C", "coordinate": "u", "phase": 0.2,
        "spans": [ { "element": "ED", "from": 0, "to": 200, "direction": "forward" } ] }
    ]
  }
}`

// TestCompileConstructionWire — типы и run'ы уезжают в провод: умолчание
// разрешено компилятором (в проводе у каждого run явный type), run'ы
// отсортированы по id, спаны — в авторском порядке, пустые массивы карты без
// блока — «[]», а не null.
func TestCompileConstructionWire(t *testing.T) {
	_, rg, err := Compile(loadMap(t, constructionTrackMap))
	if err != nil {
		t.Fatalf("компиляция: %v", err)
	}
	if len(rg.TrackTypes) != 1 || rg.TrackTypes[0].ID != "TRACK_MAIN" {
		t.Fatalf("типы в проводе %+v", rg.TrackTypes)
	}
	tt := rg.TrackTypes[0]
	if tt.Gauge != 1.435 || tt.Sleeper.Pitch != 0.6 || tt.Sleeper.Length != 2.5 ||
		tt.Sleeper.Width != 0.28 || tt.Ballast.HalfWidth != 1.75 {
		t.Fatalf("тип в проводе %+v, ожидались числа из карты", tt)
	}
	if rg.PlacementAlgorithm != PlacementAlgorithm {
		t.Fatalf("placement_algorithm %q, ожидалось %q", rg.PlacementAlgorithm, PlacementAlgorithm)
	}
	if len(rg.ConstructionRuns) != 3 {
		t.Fatalf("run'ов в проводе %d, ожидалось 3", len(rg.ConstructionRuns))
	}
	// Сортировка по id, несмотря на авторский порядок в карте.
	if rg.ConstructionRuns[0].ID != "RUN_A" || rg.ConstructionRuns[2].ID != "RUN_C" {
		t.Fatalf("run'ы не отсортированы по id: %s, %s, %s",
			rg.ConstructionRuns[0].ID, rg.ConstructionRuns[1].ID, rg.ConstructionRuns[2].ID)
	}
	for _, r := range rg.ConstructionRuns {
		if r.Type != "TRACK_MAIN" {
			t.Fatalf("run %s: type %q в проводе неявный", r.ID, r.Type)
		}
		if r.Coordinate != "u" || len(r.Spans) != 1 || r.Spans[0].Direction != "forward" {
			t.Fatalf("run %s: %+v", r.ID, r)
		}
	}
	// Карта без блока: массивы пустые, но не null (форма контракта).
	_, rg2, err := Compile(loadMap(t, twoEdges))
	if err != nil {
		t.Fatalf("компиляция: %v", err)
	}
	if rg2.TrackTypes == nil || rg2.ConstructionRuns == nil || rg2.Features == nil {
		t.Fatal("массивы рецепта обязаны быть пустыми, а не null")
	}
	if len(rg2.TrackTypes) != 0 || len(rg2.ConstructionRuns) != 0 || len(rg2.Features) != 0 {
		t.Fatalf("карта без construction дала рецепт: %d типов, %d run'ов, %d особенностей",
			len(rg2.TrackTypes), len(rg2.ConstructionRuns), len(rg2.Features))
	}
}
