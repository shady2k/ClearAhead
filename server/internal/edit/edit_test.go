package edit

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/geom"
	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/netloc"
	"github.com/shady2k/ClearAhead/server/internal/seedmap"
	"github.com/shady2k/ClearAhead/server/internal/units"
)

// ---- Помощники ----

// testBaseMap — маленькая законная станция-цепочка с решёткой: три ребра,
// якорь на западном конце, оба свободных конца назначены. Три ребра стыкуются
// в обычных портах — одна физическая решётка, один run в порядке прохождения.
func testBaseMap() *mapfmt.Map {
	return &mapfmt.Map{
		FormatVersion: 2,
		MapID:         "T",
		MapRevision:   1,
		Anchors: map[string]mapfmt.Anchor{
			"N_B.P1": {X: 0, Y: 0, Z: 0, Heading: 0},
		},
		Topology: mapfmt.Topology{
			Nodes: []mapfmt.Node{
				{ID: "N_B", Ports: []mapfmt.Port{{ID: "P1", Purpose: "map_boundary"}}},
				{ID: "N1", Ports: []mapfmt.Port{{ID: "P1"}}},
				{ID: "N2", Ports: []mapfmt.Port{{ID: "P1"}}},
				{ID: "N_END", Ports: []mapfmt.Port{{ID: "P1", Purpose: "buffer_stop"}}},
			},
			Edges: []mapfmt.Edge{
				{ID: "E0", Kind: mapfmt.KindRail, From: "N_B.P1", To: "N1.P1"},
				{ID: "E1", Kind: mapfmt.KindRail, From: "N1.P1", To: "N2.P1"},
				{ID: "E2", Kind: mapfmt.KindRail, From: "N2.P1", To: "N_END.P1"},
			},
		},
		Geometry: mapfmt.Geometry{
			Edges: map[string]mapfmt.Alignments{
				"E0": {Horizontal: []mapfmt.HPrim{{Kind: "straight", Length: 100}}},
				"E1": {Horizontal: []mapfmt.HPrim{{Kind: "straight", Length: 100}}},
				"E2": {Horizontal: []mapfmt.HPrim{{Kind: "straight", Length: 100}}},
			},
		},
		Construction: &mapfmt.Construction{
			DefaultType: "TRACK_MAIN_1520",
			Types: []mapfmt.TrackType{{
				ID:      "TRACK_MAIN_1520",
				Gauge:   1.520,
				Rail:    mapfmt.TrackRail{Height: 0.18, HeadWidth: 0.075},
				Sleeper: mapfmt.TrackSleeper{Pitch: 0.6, Length: 2.5, Width: 0.28, Height: 0.20},
				Ballast: mapfmt.TrackBallast{HalfWidth: 1.75, Depth: 0.30, CribDepth: 0.10, SideSlope: 1.5},
			}},
			Runs: []mapfmt.ConstructionRun{
				{ID: "RUN_E0_E1_E2", Coordinate: "u", Phase: 0, Spans: []netloc.IntervalU{
					{Element: "E0", From: 0, To: 100, Direction: "forward"},
					{Element: "E1", From: 0, To: 100, Direction: "forward"},
					{Element: "E2", From: 0, To: 100, Direction: "forward"},
				}},
			},
		},
	}
}

func newStore(t *testing.T, m *mapfmt.Map) *Store {
	t.Helper()
	st, err := NewStore(m)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return st
}

func mustChain(t *testing.T, lens ...float64) geom.Chain {
	t.Helper()
	var c geom.Chain
	for _, l := range lens {
		d, err := units.MetersToDistance(l)
		if err != nil {
			t.Fatal(err)
		}
		p, err := geom.Straight(d)
		if err != nil {
			t.Fatal(err)
		}
		c = append(c, p)
	}
	return c
}

// rightTurnout — геометрия правой стрелки как в fixture_station: прямой проход
// 33.5 м, отклонённый — дуга R=300, −0.1107 рад.
func rightTurnout(t *testing.T) (straight, diverging geom.Chain) {
	t.Helper()
	d, err := units.MetersToDistance(33.5)
	if err != nil {
		t.Fatal(err)
	}
	s, err := geom.Straight(d)
	if err != nil {
		t.Fatal(err)
	}
	a, err := geom.Arc(300, -0.1107)
	if err != nil {
		t.Fatal(err)
	}
	return geom.Chain{s}, geom.Chain{a}
}

func toAlignments(t *testing.T, c geom.Chain) mapfmt.Alignments {
	t.Helper()
	al, err := chainToAlignments(c)
	if err != nil {
		t.Fatal(err)
	}
	return al
}

func jsonString(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return string(b)
}

func assertJSONEqual(t *testing.T, a, b any, what string) {
	t.Helper()
	if jsonString(t, a) != jsonString(t, b) {
		t.Fatalf("%s разошлись:\n got:  %s\n want: %s", what, jsonString(t, b), jsonString(t, a))
	}
}

// assertValid — карта обязана пройти валидатор целиком.
func assertValid(t *testing.T, m *mapfmt.Map, what string) {
	t.Helper()
	if err := mapfmt.Validate(m); err != nil {
		t.Fatalf("%s не проходит валидацию: %v", what, err)
	}
}

// mapDiff — фактические изменения между старой и новой картой: удалённые
// элементы (рёбра и стрелки), удалённые сооружения, порты, получившие
// упор. Независимая проверка предпросмотра.
func mapDiff(old, new mapfmt.Map) (removed, removedStructures, capped []string) {
	for _, e := range old.Topology.Edges {
		found := false
		for _, ne := range new.Topology.Edges {
			if ne.ID == e.ID {
				found = true
			}
		}
		if !found {
			removed = append(removed, e.ID)
		}
	}
	for _, t := range old.Topology.Turnouts {
		found := false
		for _, nt := range new.Topology.Turnouts {
			if nt.ID == t.ID {
				found = true
			}
		}
		if !found {
			removed = append(removed, t.ID)
		}
	}
	for _, st := range old.Topology.Structures {
		found := false
		for _, nst := range new.Topology.Structures {
			if nst.ID == st.ID {
				found = true
			}
		}
		if !found {
			removedStructures = append(removedStructures, st.ID)
		}
	}
	purposeOf := func(m mapfmt.Map, port string) string {
		for _, n := range m.Topology.Nodes {
			for _, p := range n.Ports {
				if n.ID+"."+p.ID == port {
					return p.Purpose
				}
			}
		}
		return ""
	}
	// Порты, у которых упор появился (было пусто — стало buffer_stop).
	for _, n := range old.Topology.Nodes {
		for _, p := range n.Ports {
			port := n.ID + "." + p.ID
			if p.Purpose == "" && purposeOf(new, port) == "buffer_stop" {
				capped = append(capped, port)
			}
		}
	}
	sort.Strings(removed)
	sort.Strings(removedStructures)
	sort.Strings(capped)
	return removed, removedStructures, capped
}

func assertCascade(t *testing.T, prev *ErasePreview, wantRemoved, wantStructures, wantCapped []string) {
	t.Helper()
	assertJSONEqual(t, wantRemoved, prev.RemovedElements, "каскад: удалённые элементы")
	assertJSONEqual(t, wantStructures, prev.RemovedStructures, "каскад: порванные сооружения")
	assertJSONEqual(t, wantCapped, prev.CappedPorts, "каскад: закрытые упором концы")
}

func hasEdge(t *testing.T, m *mapfmt.Map, id string) bool {
	t.Helper()
	for _, e := range m.Topology.Edges {
		if e.ID == id {
			return true
		}
	}
	return false
}

func hasTurnout(t *testing.T, m *mapfmt.Map, id string) bool {
	t.Helper()
	for _, s := range m.Topology.Turnouts {
		if s.ID == id {
			return true
		}
	}
	return false
}

// runsCoverAllEdges — каждое ребро покрыто ровно одним спаном целиком.
func runsCoverAllEdges(m *mapfmt.Map) error {
	byEdge := map[string][]netloc.IntervalU{}
	for _, r := range m.Construction.Runs {
		for _, sp := range r.Spans {
			byEdge[sp.Element] = append(byEdge[sp.Element], sp)
		}
	}
	for _, e := range m.Topology.Edges {
		spans, ok := byEdge[e.ID]
		if !ok {
			return fmt.Errorf("ребро %s не покрыто", e.ID)
		}
		if len(spans) != 1 {
			return fmt.Errorf("ребро %s покрыто %d спанами", e.ID, len(spans))
		}
		u, err := alignmentsLengthU(m.Geometry.Edges[e.ID])
		if err != nil {
			return err
		}
		sp := spans[0]
		if sp.From != 0 || sp.To != u.Meters() {
			return fmt.Errorf("ребро %s: спад [%v, %v], ожидается [0, %v]", e.ID, sp.From, sp.To, u.Meters())
		}
	}
	return nil
}

// ---- Критерий приёмки: последовательность правок, каждая валидна ----

func TestSequenceOfEditsValidates(t *testing.T) {
	st := newStore(t, testBaseMap())
	rev := st.Revision()

	// 1. Продлить путь от тупика.
	ext, err := st.Apply(Intent{Op: OpExtend, Extend: ExtendIntent{
		Port:  "N_END.P1",
		Chain: mustChain(t, 50),
	}})
	if err != nil {
		t.Fatalf("extend: %v", err)
	}
	rev++
	if ext.Revision != rev {
		t.Fatalf("ревизия: %d, ожидается %d", ext.Revision, rev)
	}
	assertValid(t, &ext.Map, "после extend")

	// 2. Ответвиться от середины E1.
	s, d := rightTurnout(t)
	br, err := st.Apply(Intent{Op: OpBranch, Branch: BranchIntent{
		Edge:      "E1",
		AtU:       50,
		Hand:      "right",
		Straight:  s,
		Diverging: d,
		Branch:    mustChain(t, 40),
	}})
	if err != nil {
		t.Fatalf("branch: %v", err)
	}
	rev++
	assertValid(t, &br.Map, "после branch")

	// Ветвление разрезало E1 и добавило стрелку с ветвью.
	if !hasEdge(t, &br.Map, "E1") || !hasEdge(t, &br.Map, "E1_CONT") || !hasTurnout(t, &br.Map, "SW") {
		t.Fatalf("branch: ожидались E1, E1_CONT и SW: %s", jsonString(t, br.Map.Topology))
	}
	// Каждое ребро покрыто run'ом.
	if err := runsCoverAllEdges(&br.Map); err != nil {
		t.Fatalf("run'ы после branch: %v", err)
	}

	// 3. Положить платформу на E2.
	pl, err := st.Apply(Intent{Op: OpPlace, Place: PlaceIntent{
		Element: "E2", From: 20, To: 60, Side: "right", Offset: 1.745, Width: 3.0, Height: 0.2, SlabThickness: 0.35,
	}})
	if err != nil {
		t.Fatalf("place: %v", err)
	}
	rev++
	assertValid(t, &pl.Map, "после place")

	// 4. Замкнуть конец упором (стык — упор игнорируется валидатором, но
	// правка обязана пройти).
	capRes, err := st.Apply(Intent{Op: OpCap, Cap: CapIntent{Port: "N1.P1"}})
	if err != nil {
		t.Fatalf("cap: %v", err)
	}
	rev++
	assertValid(t, &capRes.Map, "после cap")

	// 5. Стереть концевое ребро E2 целиком с каскадом.
	er, err := st.Apply(Intent{Op: OpErase, Erase: EraseIntent{Target: "E2", Mode: EraseCascade}})
	if err != nil {
		t.Fatalf("erase: %v", err)
	}
	rev++
	assertValid(t, &er.Map, "после erase")
	if er.Revision != rev {
		t.Fatalf("ревизия: %d, ожидается %d", er.Revision, rev)
	}
	if st.Revision() != rev {
		t.Fatalf("текущая ревизия %d, ожидается %d", st.Revision(), rev)
	}
}

// ---- Критерий приёмки: неудачная правка не оставляет следов ----

func TestFailedApplyLeavesMapByteIdentical(t *testing.T) {
	st := newStore(t, testBaseMap())
	before := st.Current()
	rev := st.Revision()

	bad := []Intent{
		// Платформа шириной 0.5 м — валидатор отвергнет.
		{Op: OpPlace, Place: PlaceIntent{Element: "E2", From: 10, To: 20, Side: "right", Offset: 1.745, Width: 0.5, Height: 0.2, SlabThickness: 0.35}},
		// Платформа за концом элемента.
		{Op: OpPlace, Place: PlaceIntent{Element: "E2", From: 90, To: 150, Side: "right", Offset: 1.745, Width: 3, Height: 0.2, SlabThickness: 0.35}},
		// Ветвление ровно на конце ребра.
		{Op: OpBranch, Branch: BranchIntent{Edge: "E1", AtU: 100, Hand: "right"}},
		// Продление от стыка (не лист).
		{Op: OpExtend, Extend: ExtendIntent{Port: "N1.P1", Chain: mustChain(t, 10)}},
		// Продление от порта стрелки.
		{Op: OpExtend, Extend: ExtendIntent{Port: "SW1.C", Chain: mustChain(t, 10)}},
		// Стирка несуществующей цели.
		{Op: OpErase, Erase: EraseIntent{Target: "NO_SUCH", Mode: EraseCascade}},
		// Стирка сооружения (не ребро и не стрелка).
		{Op: OpErase, Erase: EraseIntent{Target: "PLAT_X", Mode: EraseCascade}},
		// Неизвестная правка.
		{Op: Op(99)},
	}
	for i, in := range bad {
		if _, err := st.Apply(in); err == nil {
			t.Fatalf("правка %d: ожидалась ошибка, применена", i)
		}
		after := st.Current()
		assertJSONEqual(t, before, after, "карта после неудачной правки")
		if st.Revision() != rev {
			t.Fatalf("правка %d: ревизия изменилась после отказа", i)
		}
	}

	// Якорное ребро стереть нельзя: якорь осиротеет, валидатор откажет.
	if _, err := st.Apply(Intent{Op: OpErase, Erase: EraseIntent{Target: "E0", Mode: EraseCascade}}); err == nil {
		t.Fatal("стирка якорного ребра: ожидалась ошибка")
	}
	assertJSONEqual(t, before, st.Current(), "карта после отказа стирки якорного ребра")
}

// ---- Критерий приёмки: предпросмотр стерки совпадает с фактом ----

func TestErasePreviewMatchesActual(t *testing.T) {
	cases := []struct {
		name           string
		target         string
		mode           EraseMode
		wantRemoved    []string
		wantStructures []string
		wantCapped     []string
	}{
		// Концевое ребро: его конец на стыке становится висящим и закрывается
		// упором, порт с назначением остаётся как есть.
		{name: "концевое ребро", target: "E2", mode: EraseCascade,
			wantRemoved: []string{"E2"}, wantCapped: []string{"N2.P1"}},
		// Среднее ребро: оба конца становятся висящими.
		{name: "среднее ребро", target: "E1", mode: EraseCascade,
			wantRemoved: []string{"E1"}, wantCapped: []string{"N1.P1", "N2.P1"}},
		// Среднее ребро, режим выбора: каскад не выходит за выбранное.
		{name: "среднее ребро выборочно", target: "E1", mode: EraseSelection,
			wantRemoved: []string{"E1"}, wantCapped: []string{"N1.P1", "N2.P1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st := newStore(t, testBaseMap())
			in := Intent{Op: OpErase, Erase: EraseIntent{Target: tc.target, Mode: tc.mode}}

			// Предпросмотр — чистый расчёт: текущая карта не меняется.
			rev := st.Revision()
			prev, err := st.Preview(in)
			if err != nil {
				t.Fatalf("Preview: %v", err)
			}
			if st.Revision() != rev {
				t.Fatalf("Preview изменил ревизию: %d → %d", rev, st.Revision())
			}
			assertCascade(t, prev.Cascade, tc.wantRemoved, tc.wantStructures, tc.wantCapped)

			before := st.Current()
			res, err := st.Apply(in)
			if err != nil {
				t.Fatalf("Apply: %v", err)
			}

			// Предпросмотр обязан совпадать с фактом: карты одинаковы, отчёты
			// одинаковы, отчёт совпадает с реальным диффом.
			assertJSONEqual(t, prev.Map, res.Map, "карта предпросмотра и факта")
			assertJSONEqual(t, prev.Cascade, res.Cascade, "отчёт предпросмотра и факта")

			gotRemoved, gotStructures, gotCapped := mapDiff(before, res.Map)
			assertJSONEqual(t, tc.wantRemoved, gotRemoved, "фактически удалённые элементы")
			assertJSONEqual(t, tc.wantStructures, gotStructures, "фактически порванные сооружения")
			assertJSONEqual(t, tc.wantCapped, gotCapped, "фактически закрытые упором концы")
			assertValid(t, &res.Map, "карта после стерки")
		})
	}
}

// Стерка стрелки целиком с зависимостями: уходит стрелка, все три внешних
// ребра и платформа на одном из них.
func TestEraseTurnoutCascade(t *testing.T) {
	m := testBaseMap()
	m.Topology.Structures = []mapfmt.Structure{{
		ID: "PLAT_EA", Kind: "platform",
		Span: []netloc.IntervalU{{Element: "EA", From: 10, To: 30}},
		Side: "right", Offset: 1.745, Width: 3.0, Height: 0.2, SlabThickness: 0.35,
	}}
	// Отдельная неякорная компонента со стрелкой — её стирка не трогает якорь.
	m.Topology.Nodes = append(m.Topology.Nodes,
		mapfmt.Node{ID: "N_A", Ports: []mapfmt.Port{{ID: "P1", Purpose: "buffer_stop"}}},
		mapfmt.Node{ID: "N_BR1", Ports: []mapfmt.Port{{ID: "P1", Purpose: "buffer_stop"}}},
		mapfmt.Node{ID: "N_BR2", Ports: []mapfmt.Port{{ID: "P1", Purpose: "buffer_stop"}}},
	)
	s, d := rightTurnout(t)
	m.Topology.Turnouts = append(m.Topology.Turnouts, mapfmt.Turnout{
		ID: "SWX", Kind: mapfmt.KindRail, Hand: "right",
		Ports: mapfmt.TurnoutPorts{Common: "C", Straight: "S", Diverging: "D"},
	})
	m.Geometry.Turnouts = map[string]mapfmt.TurnoutGeometry{"SWX": {Straight: toAlignments(t, s), Diverging: toAlignments(t, d)}}
	m.Topology.Edges = append(m.Topology.Edges,
		mapfmt.Edge{ID: "EA", Kind: mapfmt.KindRail, From: "N_A.P1", To: "SWX.C"},
		mapfmt.Edge{ID: "EB", Kind: mapfmt.KindRail, From: "SWX.S", To: "N_BR1.P1"},
		mapfmt.Edge{ID: "EC", Kind: mapfmt.KindRail, From: "SWX.D", To: "N_BR2.P1"},
	)
	m.Geometry.Edges["EA"] = mapfmt.Alignments{Horizontal: []mapfmt.HPrim{{Kind: "straight", Length: 40}}}
	m.Geometry.Edges["EB"] = mapfmt.Alignments{Horizontal: []mapfmt.HPrim{{Kind: "straight", Length: 40}}}
	m.Geometry.Edges["EC"] = mapfmt.Alignments{Horizontal: []mapfmt.HPrim{{Kind: "straight", Length: 40}}}
	// Новая компонента получает свои run'ы.
	m.Construction.Runs = append(m.Construction.Runs,
		mapfmt.ConstructionRun{ID: "RUN_EA", Coordinate: "u", Phase: 0, Spans: []netloc.IntervalU{{Element: "EA", From: 0, To: 40, Direction: "forward"}}},
		mapfmt.ConstructionRun{ID: "RUN_EB", Coordinate: "u", Phase: 0, Spans: []netloc.IntervalU{{Element: "EB", From: 0, To: 40, Direction: "forward"}}},
		mapfmt.ConstructionRun{ID: "RUN_EC", Coordinate: "u", Phase: 0, Spans: []netloc.IntervalU{{Element: "EC", From: 0, To: 40, Direction: "forward"}}},
	)

	st := newStore(t, m)
	in := Intent{Op: OpErase, Erase: EraseIntent{Target: "SWX", Mode: EraseCascade}}

	// Режим выбора стрелку не сотрёт: каскад обязан унести внешние рёбра.
	if _, err := st.Apply(Intent{Op: OpErase, Erase: EraseIntent{Target: "SWX", Mode: EraseSelection}}); err == nil {
		t.Fatal("стирка стрелки в режиме выбора: ожидалась ошибка каскада")
	}

	prev, err := st.Preview(in)
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	assertCascade(t, prev.Cascade,
		[]string{"EA", "EB", "EC", "SWX"},
		[]string{"PLAT_EA"}, nil)

	before := st.Current()
	res, err := st.Apply(in)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	assertJSONEqual(t, prev.Map, res.Map, "карта предпросмотра и факта")
	gotRemoved, gotStructures, gotCapped := mapDiff(before, res.Map)
	assertJSONEqual(t, []string{"EA", "EB", "EC", "SWX"}, gotRemoved, "удалённые элементы")
	assertJSONEqual(t, []string{"PLAT_EA"}, gotStructures, "порванные сооружения")
	assertJSONEqual(t, []string(nil), gotCapped, "закрытые упором концы")
	assertValid(t, &res.Map, "карта после стерки стрелки")

	// Платформа лежала на ушедшем ребре EA — её не осталось.
	if len(res.Map.Topology.Structures) != 0 {
		t.Fatalf("сооружения уцелели: %s", jsonString(t, res.Map.Topology.Structures))
	}
}

// Стерка стрелки с висящим концом: каскад показывает, какой конец закроется
// упором.
func TestEraseTurnoutCapsHangingEnd(t *testing.T) {
	m := testBaseMap()
	// Неякорная компонента: стрелка, чьё продолжение упирается в стык.
	m.Topology.Nodes = append(m.Topology.Nodes,
		mapfmt.Node{ID: "N_A", Ports: []mapfmt.Port{{ID: "P1", Purpose: "buffer_stop"}}},
		mapfmt.Node{ID: "N_J", Ports: []mapfmt.Port{{ID: "P1"}}},
		mapfmt.Node{ID: "N_C", Ports: []mapfmt.Port{{ID: "P1", Purpose: "buffer_stop"}}},
		mapfmt.Node{ID: "N_D", Ports: []mapfmt.Port{{ID: "P1", Purpose: "buffer_stop"}}},
	)
	s, d := rightTurnout(t)
	m.Topology.Turnouts = append(m.Topology.Turnouts, mapfmt.Turnout{
		ID: "SWZ", Kind: mapfmt.KindRail, Hand: "right",
		Ports: mapfmt.TurnoutPorts{Common: "C", Straight: "S", Diverging: "D"},
	})
	m.Geometry.Turnouts = map[string]mapfmt.TurnoutGeometry{"SWZ": {Straight: toAlignments(t, s), Diverging: toAlignments(t, d)}}
	m.Topology.Edges = append(m.Topology.Edges,
		mapfmt.Edge{ID: "EA", Kind: mapfmt.KindRail, From: "N_A.P1", To: "SWZ.C"},
		mapfmt.Edge{ID: "EB", Kind: mapfmt.KindRail, From: "SWZ.S", To: "N_J.P1"},
		mapfmt.Edge{ID: "EC", Kind: mapfmt.KindRail, From: "N_J.P1", To: "N_C.P1"},
		mapfmt.Edge{ID: "ED", Kind: mapfmt.KindRail, From: "SWZ.D", To: "N_D.P1"},
	)
	m.Geometry.Edges["EA"] = mapfmt.Alignments{Horizontal: []mapfmt.HPrim{{Kind: "straight", Length: 40}}}
	m.Geometry.Edges["EB"] = mapfmt.Alignments{Horizontal: []mapfmt.HPrim{{Kind: "straight", Length: 40}}}
	m.Geometry.Edges["EC"] = mapfmt.Alignments{Horizontal: []mapfmt.HPrim{{Kind: "straight", Length: 40}}}
	m.Geometry.Edges["ED"] = mapfmt.Alignments{Horizontal: []mapfmt.HPrim{{Kind: "straight", Length: 40}}}
	m.Construction.Runs = append(m.Construction.Runs,
		mapfmt.ConstructionRun{ID: "RUN_EA", Coordinate: "u", Phase: 0, Spans: []netloc.IntervalU{{Element: "EA", From: 0, To: 40, Direction: "forward"}}},
		mapfmt.ConstructionRun{ID: "RUN_EB_EC", Coordinate: "u", Phase: 0, Spans: []netloc.IntervalU{
			{Element: "EB", From: 0, To: 40, Direction: "forward"},
			{Element: "EC", From: 0, To: 40, Direction: "forward"},
		}},
		mapfmt.ConstructionRun{ID: "RUN_ED", Coordinate: "u", Phase: 0, Spans: []netloc.IntervalU{{Element: "ED", From: 0, To: 40, Direction: "forward"}}},
	)

	st := newStore(t, m)
	in := Intent{Op: OpErase, Erase: EraseIntent{Target: "SWZ", Mode: EraseCascade}}
	prev, err := st.Preview(in)
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	assertCascade(t, prev.Cascade,
		[]string{"EA", "EB", "ED", "SWZ"},
		nil, []string{"N_J.P1"})

	res, err := st.Apply(in)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	assertJSONEqual(t, prev.Map, res.Map, "карта предпросмотра и факта")
	assertValid(t, &res.Map, "карта после стерки стрелки с висящим концом")

	// Уцелевшее ребро EC получает собственный run; прежняя решётка
	// (RUN_E0_E1_E2) не тронута.
	runs := res.Map.Construction.Runs
	foundEC := false
	for i := range runs {
		if len(runs[i].Spans) == 1 && runs[i].Spans[0].Element == "EC" {
			foundEC = true
		}
	}
	if !foundEC {
		t.Fatalf("run'ы после стерки: нет run'а ребра EC: %s", jsonString(t, runs))
	}
	if len(runs) != 2 {
		t.Fatalf("run'ов %d, ожидалось 2: %s", len(runs), jsonString(t, runs))
	}
}

// ---- Ревизии растут только вперёд ----

// Ревизия рождается на каждом применении и монотонно растёт. Вернуть её назад
// нечем: отмены нет (см. шапку пакета), поэтому проверять «номера не
// переиспользуются после отката» больше не на чем — переиспользовать их мог
// только откат.
func TestRevisionsGrowForward(t *testing.T) {
	st := newStore(t, testBaseMap())

	applyPlace := func(from, to float64) Result {
		res, err := st.Apply(Intent{Op: OpPlace, Place: PlaceIntent{
			Element: "E2", From: from, To: to, Side: "right", Offset: 1.745, Width: 3.0, Height: 0.2, SlabThickness: 0.35,
		}})
		if err != nil {
			t.Fatalf("Apply: %v", err)
		}
		return res
	}
	r2 := applyPlace(20, 60)
	r3 := applyPlace(70, 90)

	if !(r2.Revision == 2 && r3.Revision == 3) {
		t.Fatalf("ревизии: %d, %d — ожидались 2, 3", r2.Revision, r3.Revision)
	}
	if st.Revision() != 3 {
		t.Fatalf("текущая ревизия %d, ожидалась 3", st.Revision())
	}
	assertJSONEqual(t, r3.Map, st.Current(), "текущая карта и результат последней правки")
}

// ---- Run'ы: пересчёт и стабильность ----

// Пересчёт run'ов на неизменной топологии воспроизводит прежнюю решётку
// байт в байт, включая авторские ID.
func TestRunsReproducedOnUnchangedTopology(t *testing.T) {
	st := newStore(t, testBaseMap())
	res, err := st.Preview(Intent{Op: OpPlace, Place: PlaceIntent{
		Element: "E2", From: 20, To: 60, Side: "right", Offset: 1.745, Width: 3.0, Height: 0.2, SlabThickness: 0.35,
	}})
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	assertJSONEqual(t, testBaseMap().Construction.Runs, res.Map.Construction.Runs,
		"run'ы после правки, не меняющей топологию")

	// Карта с настоящей горловиной — из фабрики, а не из файла.
	fx := *seedmap.Station()
	assertValid(t, &fx, "станция фабрики")
	st2 := newStore(t, &fx)
	before := fx.Construction.Runs
	res2, err := st2.Preview(Intent{Op: OpCap, Cap: CapIntent{Port: "N_STOP_MAIN.P1"}})
	if err != nil {
		t.Fatalf("Preview над fixture: %v", err)
	}
	assertJSONEqual(t, before, res2.Map.Construction.Runs,
		"run'ы fixture_station после правки, не меняющей топологию")
}

// Продление пути сливает run продолжения с run'ом исходной решётки: шпалы не
// переставляются через стык.
func TestExtendMergesRun(t *testing.T) {
	st := newStore(t, testBaseMap())
	res, err := st.Apply(Intent{Op: OpExtend, Extend: ExtendIntent{
		Port:  "N_END.P1",
		Chain: mustChain(t, 50),
	}})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	// Прежние рёбра и E_EXT — одна физическая решётка: один run, фаза
	// унаследована от прежнего run'а.
	runs := res.Map.Construction.Runs
	if len(runs) != 1 {
		t.Fatalf("run'ов %d, ожидался 1: %s", len(runs), jsonString(t, runs))
	}
	merged := &runs[0]
	if len(merged.Spans) != 4 {
		t.Fatalf("слитый run: спанов %d, ожидалось 4: %s", len(merged.Spans), jsonString(t, merged))
	}
	if merged.Spans[0].Element != "E0" || merged.Spans[0].Direction != "forward" ||
		merged.Spans[3].Element != "E_EXT" || merged.Spans[3].Direction != "forward" {
		t.Fatalf("слитый run: %s", jsonString(t, merged))
	}
	if merged.Phase != 0 {
		t.Fatalf("фаза слитого run'а %v, ожидалась 0", merged.Phase)
	}
	assertValid(t, &res.Map, "карта после продления")
}

// Два ребра, оба заканчивающиеся в одном порту (стык «конец-в-конец», как
// ST_A_E_E34/ST_A_E_T4), сливаются в один run, второе проходится в обратном
// направлении.
func TestRunsMergeAcrossToToJoint(t *testing.T) {
	m := &mapfmt.Map{
		FormatVersion: 2,
		MapID:         "M",
		MapRevision:   1,
		// Якорь на ребре E6 (его начало), чтобы стирка E5 не осиротила якорь.
		Anchors: map[string]mapfmt.Anchor{"N_B.P1": {X: 200, Y: 0, Z: 0, Heading: math.Pi}},
		Topology: mapfmt.Topology{
			Nodes: []mapfmt.Node{
				{ID: "N_A", Ports: []mapfmt.Port{{ID: "P1", Purpose: "map_boundary"}}},
				{ID: "N_J", Ports: []mapfmt.Port{{ID: "P1"}}},
				{ID: "N_B", Ports: []mapfmt.Port{{ID: "P1", Purpose: "buffer_stop"}}},
			},
			Edges: []mapfmt.Edge{
				{ID: "E5", Kind: mapfmt.KindRail, From: "N_A.P1", To: "N_J.P1"},
				{ID: "E6", Kind: mapfmt.KindRail, From: "N_B.P1", To: "N_J.P1"},
			},
		},
		Geometry: mapfmt.Geometry{
			Edges: map[string]mapfmt.Alignments{
				"E5": {Horizontal: []mapfmt.HPrim{{Kind: "straight", Length: 100}}},
				"E6": {Horizontal: []mapfmt.HPrim{{Kind: "straight", Length: 100}}},
			},
		},
		Construction: &mapfmt.Construction{
			DefaultType: "TRACK_MAIN_1520",
			Types:       []mapfmt.TrackType{{ID: "TRACK_MAIN_1520", Gauge: 1.520, Rail: mapfmt.TrackRail{Height: 0.18, HeadWidth: 0.075}, Sleeper: mapfmt.TrackSleeper{Pitch: 0.6, Length: 2.5, Width: 0.28, Height: 0.20}, Ballast: mapfmt.TrackBallast{HalfWidth: 1.75, Depth: 0.30, CribDepth: 0.10, SideSlope: 1.5}}},
			Runs: []mapfmt.ConstructionRun{
				{ID: "RUN_E5_E6", Coordinate: "u", Phase: 0, Spans: []netloc.IntervalU{
					{Element: "E5", From: 0, To: 100, Direction: "forward"},
					{Element: "E6", From: 0, To: 100, Direction: "reverse"},
				}},
			},
		},
	}
	st := newStore(t, m)

	// Правка, не меняющая топологию: run воспроизводится, включая ID.
	res, err := st.Preview(Intent{Op: OpCap, Cap: CapIntent{Port: "N_J.P1"}})
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	assertJSONEqual(t, m.Construction.Runs, res.Map.Construction.Runs, "run'ы To-To стыка")

	// Стираем одно ребро: второй остаётся, стык закрывается упором.
	er, err := st.Apply(Intent{Op: OpErase, Erase: EraseIntent{Target: "E5", Mode: EraseCascade}})
	if err != nil {
		t.Fatalf("Erase: %v", err)
	}
	assertCascade(t, er.Cascade, []string{"E5"}, nil, []string{"N_J.P1"})
	runs := er.Map.Construction.Runs
	if len(runs) != 1 || runs[0].Spans[0].Element != "E6" {
		t.Fatalf("run'ы после стирки: %s", jsonString(t, runs))
	}
	assertValid(t, &er.Map, "карта после стирки одного ребра стыка")
}

// Фаза run'а — авторитетный факт: правка, не меняющая физическую решётку,
// обязана её сохранить, а не сбросить в ноль.
func TestRunsPreservePhaseAcrossEdit(t *testing.T) {
	m := testBaseMap()
	m.Construction.Runs[0].Phase = 0.3
	st := newStore(t, m)

	res, err := st.Apply(Intent{Op: OpPlace, Place: PlaceIntent{
		Element: "E2", From: 20, To: 60, Side: "right", Offset: 1.745, Width: 3.0, Height: 0.2, SlabThickness: 0.35,
	}})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	got := map[string]float64{}
	for _, r := range res.Map.Construction.Runs {
		got[r.ID] = r.Phase
	}
	if got["RUN_E0_E1_E2"] != 0.3 {
		t.Fatalf("фаза RUN_E0_E1_E2 %v, ожидалась 0.3: %s", got["RUN_E0_E1_E2"], jsonString(t, res.Map.Construction.Runs))
	}
}
