package edit

import (
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"math"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/geom"
	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/netloc"
	"github.com/shady2k/ClearAhead/server/internal/seedmap"
	"github.com/shady2k/ClearAhead/server/internal/terrain"
	"github.com/shady2k/ClearAhead/server/internal/units"
	"github.com/shady2k/ClearAhead/server/internal/uuidv7"
)

// ---- Помощники ----

// Идентификаторы фикстур — UUIDv7 (решение владельца 2026-08-13 «UUIDv7
// везде»): тождество элемента — UUID, читаемая метка — отдельное поле name.
// Таблица фиксирована: тесты воспроизводимы, эталоны не зависят от времени и
// случайности.
const (
	eIDN_B     = "01a3185c-7001-7242-8242-000000424242" // метка N_B
	eIDN1      = "01a3185c-7002-7242-8242-000001424242" // метка N1
	eIDN2      = "01a3185c-7003-7242-8242-000002424242" // метка N2
	eIDNEND    = "01a3185c-7004-7242-8242-000003424242" // метка N_END
	eIDE0      = "01a3185c-7005-7242-8242-000004424242" // метка E0
	eIDE1      = "01a3185c-7006-7242-8242-000005424242" // метка E1
	eIDE2      = "01a3185c-7007-7242-8242-000006424242" // метка E2
	eIDRUN     = "01a3185c-7008-7242-8242-000007424242" // метка RUN_E0_E1_E2
	eIDTYPE    = "01a3185c-7009-7242-8242-000008424242" // метка TRACK_MAIN_1520
	eIDNA      = "01a3185c-700a-7242-8242-000009424242" // метка N_A
	eIDNJ      = "01a3185c-700b-7242-8242-00000a424242" // метка N_J
	eIDNB      = "01a3185c-700c-7242-8242-00000b424242" // метка N_B
	eIDE5      = "01a3185c-700d-7242-8242-00000c424242" // метка E5
	eIDE6      = "01a3185c-700e-7242-8242-00000d424242" // метка E6
	eIDRUN56   = "01a3185c-700f-7242-8242-00000e424242" // метка RUN_E5_E6
	eIDBR1     = "01a3185c-7010-7242-8242-00000f424242" // метка N_BR1
	eIDBR2     = "01a3185c-7011-7242-8242-000010424242" // метка N_BR2
	eIDSWX     = "01a3185c-7012-7242-8242-000011424242" // метка SWX
	eIDEA      = "01a3185c-7013-7242-8242-000012424242" // метка EA
	eIDEB      = "01a3185c-7014-7242-8242-000013424242" // метка EB
	eIDEC      = "01a3185c-7015-7242-8242-000014424242" // метка EC
	eIDED      = "01a3185c-7016-7242-8242-000015424242" // метка ED
	eIDPLATEA  = "01a3185c-7017-7242-8242-000016424242" // метка PLAT_EA
	eIDRUNEA   = "01a3185c-7018-7242-8242-000017424242" // метка RUN_EA
	eIDRUNEB   = "01a3185c-7019-7242-8242-000018424242" // метка RUN_EB
	eIDRUNEC   = "01a3185c-701a-7242-8242-000019424242" // метка RUN_EC
	eIDSWZ     = "01a3185c-701b-7242-8242-00001a424242" // метка SWZ
	eIDNC      = "01a3185c-701c-7242-8242-00001b424242" // метка N_C
	eIDND      = "01a3185c-701d-7242-8242-00001c424242" // метка N_D
	eIDRUNEBEC = "01a3185c-701e-7242-8242-00001d424242" // метка RUN_EB_EC
	eIDRUNED   = "01a3185c-701f-7242-8242-00001e424242" // метка RUN_ED
	eIDNX      = "01a3185c-7020-7242-8242-00001f424242" // метка N_X
	eIDE1CONT  = "01a3185c-7021-7242-8242-000020424242" // метка E1_CONT
	eIDRUN21   = "01a3185c-7022-7242-8242-000021424242" // метка RUN_E2_E1_E0
)

// testBaseMap — маленькая законная станция-цепочка с решёткой: три ребра,
// якорь на западном конце, оба свободных конца назначены. Три ребра стыкуются
// в обычных портах — одна физическая решётка, один run в порядке прохождения.
func testBaseMap() *mapfmt.Map {
	return &mapfmt.Map{
		FormatVersion: 2,
		MapID:         "T",
		MapRevision:   1,
		Anchors: map[string]mapfmt.Anchor{
			eIDN_B + ".P1": {X: 0, Y: 0, Z: 0, Heading: 0},
		},
		Topology: mapfmt.Topology{
			Nodes: []mapfmt.Node{
				{ID: eIDN_B, Name: "N_B", Ports: []mapfmt.Port{{ID: "P1", Purpose: "map_boundary"}}},
				{ID: eIDN1, Name: "N1", Ports: []mapfmt.Port{{ID: "P1"}}},
				{ID: eIDN2, Name: "N2", Ports: []mapfmt.Port{{ID: "P1"}}},
				{ID: eIDNEND, Name: "N_END", Ports: []mapfmt.Port{{ID: "P1", Purpose: "buffer_stop"}}},
			},
			Edges: []mapfmt.Edge{
				{ID: eIDE0, Name: "E0", Kind: mapfmt.KindRail, From: eIDN_B + ".P1", To: eIDN1 + ".P1"},
				{ID: eIDE1, Name: "E1", Kind: mapfmt.KindRail, From: eIDN1 + ".P1", To: eIDN2 + ".P1"},
				{ID: eIDE2, Name: "E2", Kind: mapfmt.KindRail, From: eIDN2 + ".P1", To: eIDNEND + ".P1"},
			},
		},
		Geometry: mapfmt.Geometry{
			Edges: map[string]mapfmt.Alignments{
				eIDE0: {Horizontal: []mapfmt.HPrim{{Kind: "straight", Length: 100}}},
				eIDE1: {Horizontal: []mapfmt.HPrim{{Kind: "straight", Length: 100}}},
				eIDE2: {Horizontal: []mapfmt.HPrim{{Kind: "straight", Length: 100}}},
			},
		},
		Construction: &mapfmt.Construction{
			DefaultType: eIDTYPE,
			Types: []mapfmt.TrackType{{
				ID:      eIDTYPE,
				Name:    "TRACK_MAIN_1520",
				Gauge:   1.520,
				Rail:    mapfmt.TrackRail{Height: 0.18, HeadWidth: 0.075},
				Sleeper: mapfmt.TrackSleeper{Pitch: 0.6, Length: 2.5, Width: 0.28, Height: 0.20},
				Ballast: mapfmt.TrackBallast{HalfWidth: 1.75, Depth: 0.30, CribDepth: 0.10, SideSlope: 1.5},
				// Брусья обязательны у типа, на который ссылается стрелка: фикстуры
				// правок стрелки содержат, и без блока карта не проходит вход.
				Timber: &mapfmt.TrackTimber{Pitch: 0.50, LengthMax: 5.50, Width: 0.30, Height: 0.20},
			}},
			Runs: []mapfmt.ConstructionRun{
				{ID: eIDRUN, Name: "RUN_E0_E1_E2", Coordinate: "u", Phase: 0, Spans: []netloc.IntervalU{
					{Element: eIDE0, From: 0, To: 100, Direction: "forward"},
					{Element: eIDE1, From: 0, To: 100, Direction: "forward"},
					{Element: eIDE2, From: 0, To: 100, Direction: "forward"},
				}},
			},
		},
	}
}

// parallelBase — две параллельные компоненты в 20 м друг от друга: коридоры
// земляных работ (ширина reach на рецепте — 36.5 м) пересекаются, элементы
// разные. База для теста «производный рельеф не конфликтует».
func parallelBase() *mapfmt.Map {
	return &mapfmt.Map{
		FormatVersion: 2,
		MapID:         "P",
		MapRevision:   1,
		Anchors: map[string]mapfmt.Anchor{
			eIDNA + ".P1": {X: 0, Y: 0, Z: 0, Heading: 0},
			eIDNB + ".P1": {X: 0, Y: 20, Z: 0, Heading: 0},
		},
		Topology: mapfmt.Topology{
			Nodes: []mapfmt.Node{
				{ID: eIDNA, Name: "N_A", Ports: []mapfmt.Port{{ID: "P1", Purpose: "map_boundary"}}},
				{ID: eIDN1, Name: "N1", Ports: []mapfmt.Port{{ID: "P1", Purpose: "buffer_stop"}}},
				{ID: eIDNB, Name: "N_B", Ports: []mapfmt.Port{{ID: "P1", Purpose: "map_boundary"}}},
				{ID: eIDN2, Name: "N2", Ports: []mapfmt.Port{{ID: "P1", Purpose: "buffer_stop"}}},
			},
			Edges: []mapfmt.Edge{
				{ID: eIDEA, Name: "EA", Kind: mapfmt.KindRail, From: eIDNA + ".P1", To: eIDN1 + ".P1"},
				{ID: eIDEB, Name: "EB", Kind: mapfmt.KindRail, From: eIDNB + ".P1", To: eIDN2 + ".P1"},
			},
		},
		Geometry: mapfmt.Geometry{
			Edges: map[string]mapfmt.Alignments{
				eIDEA: {Horizontal: []mapfmt.HPrim{{Kind: "straight", Length: 40}}},
				eIDEB: {Horizontal: []mapfmt.HPrim{{Kind: "straight", Length: 40}}},
			},
		},
		Construction: &mapfmt.Construction{
			DefaultType: eIDTYPE,
			Types:       []mapfmt.TrackType{{ID: eIDTYPE, Name: "TRACK_MAIN_1520", Gauge: 1.520, Rail: mapfmt.TrackRail{Height: 0.18, HeadWidth: 0.075}, Sleeper: mapfmt.TrackSleeper{Pitch: 0.6, Length: 2.5, Width: 0.28, Height: 0.20}, Ballast: mapfmt.TrackBallast{HalfWidth: 1.75, Depth: 0.30, CribDepth: 0.10, SideSlope: 1.5}, Timber: &mapfmt.TrackTimber{Pitch: 0.50, LengthMax: 5.50, Width: 0.30, Height: 0.20}}},
			Runs: []mapfmt.ConstructionRun{
				{ID: eIDRUNEA, Name: "RUN_EA", Coordinate: "u", Phase: 0, Spans: []netloc.IntervalU{{Element: eIDEA, From: 0, To: 40, Direction: "forward"}}},
				{ID: eIDRUNEB, Name: "RUN_EB", Coordinate: "u", Phase: 0, Spans: []netloc.IntervalU{{Element: eIDEB, From: 0, To: 40, Direction: "forward"}}},
			},
		},
	}
}

func newService(t *testing.T, m *mapfmt.Map) *Service {
	t.Helper()
	svc, err := NewService(m, uuidv7.Deterministic())
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return svc
}

func openBuilder(t *testing.T, svc *Service, account Account) *Session {
	t.Helper()
	sess, err := svc.OpenSession(account)
	if err != nil {
		t.Fatalf("OpenSession(%q): %v", account, err)
	}
	return sess
}

// worldOf — закоммиченный мир сервиса (для тестов): серверная поверхность,
// а не поле структуры.
func worldOf(t *testing.T, svc *Service) *mapfmt.Map {
	t.Helper()
	v, err := svc.Views(RoleDriver)
	if err != nil {
		t.Fatalf("Views: %v", err)
	}
	return &v.Committed
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

// edgeIDByName — ID ребра по метке (в тестовых картах метки уникальны).
// Тождество нового элемента правка берёт из источника, поэтому проверить
// созданное ребро можно только по метке.
func edgeIDByName(t *testing.T, m *mapfmt.Map, name string) string {
	t.Helper()
	for _, e := range m.Topology.Edges {
		if e.Name == name {
			return e.ID
		}
	}
	t.Fatalf("нет ребра с меткой %s", name)
	return ""
}

func hasEdgeName(t *testing.T, m *mapfmt.Map, name string) bool {
	t.Helper()
	for _, e := range m.Topology.Edges {
		if e.Name == name {
			return true
		}
	}
	return false
}

func hasTurnoutName(t *testing.T, m *mapfmt.Map, name string) bool {
	t.Helper()
	for _, s := range m.Topology.Turnouts {
		if s.Name == name {
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

// ---- Критерий приёмки: последовательность операций, каждая валидна ----

func TestSequenceOfEditsValidates(t *testing.T) {
	svc := newService(t, testBaseMap())
	sess := openBuilder(t, svc, "a")

	// 1. Продлить путь от тупика.
	ext, err := sess.Apply(Intent{Op: OpExtend, Extend: ExtendIntent{
		Port:  eIDNEND + ".P1",
		Chain: mustChain(t, 50),
	}})
	if err != nil {
		t.Fatalf("extend: %v", err)
	}
	assertValid(t, &ext.Map, "после extend")

	// 2. Ответвиться от середины E1.
	s, d := rightTurnout(t)
	br, err := sess.Apply(Intent{Op: OpBranch, Branch: BranchIntent{
		Edge:      eIDE1,
		AtU:       50,
		Hand:      "right",
		Drive:     mapfmt.DriveManual,
		Straight:  s,
		Diverging: d,
		Branch:    mustChain(t, 40),
	}})
	if err != nil {
		t.Fatalf("branch: %v", err)
	}
	assertValid(t, &br.Map, "после branch")

	// Ветвление разрезало E1 и добавило стрелку с ветвью.
	if !hasEdge(t, &br.Map, eIDE1) || !hasEdgeName(t, &br.Map, "E1_CONT") || !hasTurnoutName(t, &br.Map, "SW") {
		t.Fatalf("branch: ожидались E1, E1_CONT и SW: %s", jsonString(t, br.Map.Topology))
	}
	// Каждое ребро покрыто run'ом.
	if err := runsCoverAllEdges(&br.Map); err != nil {
		t.Fatalf("run'ы после branch: %v", err)
	}

	// 3. Положить платформу на E2.
	pl, err := sess.Apply(Intent{Op: OpPlace, Place: PlaceIntent{
		Element: eIDE2, From: 20, To: 60, Side: "right", Offset: 1.745, Width: 3.0, Height: 0.2, SlabThickness: 0.35,
	}})
	if err != nil {
		t.Fatalf("place: %v", err)
	}
	assertValid(t, &pl.Map, "после place")

	// 4. Замкнуть конец упором (стык — упор игнорируется валидатором, но
	// правка обязана пройти).
	capRes, err := sess.Apply(Intent{Op: OpCap, Cap: CapIntent{Port: eIDN1 + ".P1"}})
	if err != nil {
		t.Fatalf("cap: %v", err)
	}
	assertValid(t, &capRes.Map, "после cap")

	// 5. Стереть концевое ребро E2 целиком с каскадом.
	er, err := sess.Apply(Intent{Op: OpErase, Erase: EraseIntent{Target: eIDE2, Mode: EraseCascade}})
	if err != nil {
		t.Fatalf("erase: %v", err)
	}
	assertValid(t, &er.Map, "после erase")

	// Журнал вырос на пять операций; мир не тронут — коммита не было.
	if got := len(sess.Journal()); got != 5 {
		t.Fatalf("журнал: операций %d, ожидалось 5", got)
	}
	if w := worldOf(t, svc); len(w.Topology.Turnouts) != 0 || len(w.Topology.Structures) != 0 {
		t.Fatalf("макет протёк в мир: %s", jsonString(t, w.Topology))
	}
}

// ---- Критерий приёмки: неудачная операция не оставляет следов ----

func TestFailedApplyLeavesMockupAndWorldUntouched(t *testing.T) {
	svc := newService(t, testBaseMap())
	sess := openBuilder(t, svc, "a")
	before, err := sess.Mockup()
	if err != nil {
		t.Fatalf("Mockup: %v", err)
	}

	bad := []Intent{
		// Платформа шириной 0.5 м — валидатор отвергнет.
		{Op: OpPlace, Place: PlaceIntent{Element: eIDE2, From: 10, To: 20, Side: "right", Offset: 1.745, Width: 0.5, Height: 0.2, SlabThickness: 0.35}},
		// Платформа за концом элемента.
		{Op: OpPlace, Place: PlaceIntent{Element: eIDE2, From: 90, To: 150, Side: "right", Offset: 1.745, Width: 3, Height: 0.2, SlabThickness: 0.35}},
		// Ветвление ровно на конце ребра.
		{Op: OpBranch, Branch: BranchIntent{Edge: eIDE1, AtU: 100, Hand: "right", Drive: mapfmt.DriveManual}},
		// Продление от стыка (не лист).
		{Op: OpExtend, Extend: ExtendIntent{Port: eIDN1 + ".P1", Chain: mustChain(t, 10)}},
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
		if _, err := sess.Apply(in); err == nil {
			t.Fatalf("правка %d: ожидалась ошибка, применена", i)
		}
		if got := len(sess.Journal()); got != 0 {
			t.Fatalf("правка %d: журнал вырос после отказа (%d)", i, got)
		}
		after, err := sess.Mockup()
		if err != nil {
			t.Fatalf("правка %d: Mockup: %v", i, err)
		}
		assertJSONEqual(t, before, after, "макет после неудачной правки")
	}

	// Якорное ребро стереть нельзя: якорь осиротеет, валидатор откажет.
	if _, err := sess.Apply(Intent{Op: OpErase, Erase: EraseIntent{Target: eIDE0, Mode: EraseCascade}}); err == nil {
		t.Fatal("стирка якорного ребра: ожидалась ошибка")
	}

	// Мир не тронут: коммитов не было, закоммиченное — исходная карта.
	assertJSONEqual(t, *testBaseMap(), *worldOf(t, svc), "закоммиченный мир после отказов")
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
		{name: "концевое ребро", target: eIDE2, mode: EraseCascade,
			wantRemoved: []string{eIDE2}, wantCapped: []string{eIDN2 + ".P1"}},
		// Среднее ребро: оба конца становятся висящими.
		{name: "среднее ребро", target: eIDE1, mode: EraseCascade,
			wantRemoved: []string{eIDE1}, wantCapped: []string{eIDN1 + ".P1", eIDN2 + ".P1"}},
		// Среднее ребро, режим выбора: каскад не выходит за выбранное.
		{name: "среднее ребро выборочно", target: eIDE1, mode: EraseSelection,
			wantRemoved: []string{eIDE1}, wantCapped: []string{eIDN1 + ".P1", eIDN2 + ".P1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := newService(t, testBaseMap())
			sess := openBuilder(t, svc, "a")
			in := Intent{Op: OpErase, Erase: EraseIntent{Target: tc.target, Mode: tc.mode}}

			// Предпросмотр — чистый расчёт: журнал и мир не меняются.
			prev, err := sess.Preview(in)
			if err != nil {
				t.Fatalf("Preview: %v", err)
			}
			if got := len(sess.Journal()); got != 0 {
				t.Fatalf("Preview вырос журнал: %d операций", got)
			}
			assertCascade(t, prev.Cascade, tc.wantRemoved, tc.wantStructures, tc.wantCapped)

			before, err := sess.Mockup()
			if err != nil {
				t.Fatalf("Mockup: %v", err)
			}
			res, err := sess.Apply(in)
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
		ID: eIDPLATEA, Name: "PLAT_EA", Kind: "platform",
		Span: []netloc.IntervalU{{Element: eIDEA, From: 10, To: 30}},
		Side: "right", Offset: 1.745, Width: 3.0, Height: 0.2, SlabThickness: 0.35,
	}}
	// Отдельная неякорная компонента со стрелкой — её стирка не трогает якорь.
	m.Topology.Nodes = append(m.Topology.Nodes,
		mapfmt.Node{ID: eIDNA, Name: "N_A", Ports: []mapfmt.Port{{ID: "P1", Purpose: "buffer_stop"}}},
		mapfmt.Node{ID: eIDBR1, Name: "N_BR1", Ports: []mapfmt.Port{{ID: "P1", Purpose: "buffer_stop"}}},
		mapfmt.Node{ID: eIDBR2, Name: "N_BR2", Ports: []mapfmt.Port{{ID: "P1", Purpose: "buffer_stop"}}},
	)
	s, d := rightTurnout(t)
	m.Topology.Turnouts = append(m.Topology.Turnouts, mapfmt.Turnout{
		ID: eIDSWX, Name: "SWX", Kind: mapfmt.KindRail, Hand: "right", Drive: mapfmt.DriveManual,
		Ports: mapfmt.TurnoutPorts{Common: "C", Straight: "S", Diverging: "D"},
	})
	m.Geometry.Turnouts = map[string]mapfmt.TurnoutGeometry{eIDSWX: {Straight: toAlignments(t, s), Diverging: toAlignments(t, d)}}
	m.Topology.Edges = append(m.Topology.Edges,
		mapfmt.Edge{ID: eIDEA, Name: "EA", Kind: mapfmt.KindRail, From: eIDNA + ".P1", To: eIDSWX + ".C"},
		mapfmt.Edge{ID: eIDEB, Name: "EB", Kind: mapfmt.KindRail, From: eIDSWX + ".S", To: eIDBR1 + ".P1"},
		mapfmt.Edge{ID: eIDEC, Name: "EC", Kind: mapfmt.KindRail, From: eIDSWX + ".D", To: eIDBR2 + ".P1"},
	)
	m.Geometry.Edges[eIDEA] = mapfmt.Alignments{Horizontal: []mapfmt.HPrim{{Kind: "straight", Length: 40}}}
	m.Geometry.Edges[eIDEB] = mapfmt.Alignments{Horizontal: []mapfmt.HPrim{{Kind: "straight", Length: 40}}}
	m.Geometry.Edges[eIDEC] = mapfmt.Alignments{Horizontal: []mapfmt.HPrim{{Kind: "straight", Length: 40}}}
	// Новая компонента получает свои run'ы.
	m.Construction.Runs = append(m.Construction.Runs,
		mapfmt.ConstructionRun{ID: eIDRUNEA, Name: "RUN_EA", Coordinate: "u", Phase: 0, Spans: []netloc.IntervalU{{Element: eIDEA, From: 0, To: 40, Direction: "forward"}}},
		mapfmt.ConstructionRun{ID: eIDRUNEB, Name: "RUN_EB", Coordinate: "u", Phase: 0, Spans: []netloc.IntervalU{{Element: eIDEB, From: 0, To: 40, Direction: "forward"}}},
		mapfmt.ConstructionRun{ID: eIDRUNEC, Name: "RUN_EC", Coordinate: "u", Phase: 0, Spans: []netloc.IntervalU{{Element: eIDEC, From: 0, To: 40, Direction: "forward"}}},
	)

	svc := newService(t, m)
	sess := openBuilder(t, svc, "a")
	in := Intent{Op: OpErase, Erase: EraseIntent{Target: eIDSWX, Mode: EraseCascade}}

	// Режим выбора стрелку не сотрёт: каскад обязан унести внешние рёбра.
	if _, err := sess.Apply(Intent{Op: OpErase, Erase: EraseIntent{Target: eIDSWX, Mode: EraseSelection}}); err == nil {
		t.Fatal("стирка стрелки в режиме выбора: ожидалась ошибка каскада")
	}

	prev, err := sess.Preview(in)
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	assertCascade(t, prev.Cascade,
		[]string{eIDSWX, eIDEA, eIDEB, eIDEC},
		[]string{eIDPLATEA}, nil)

	before, err := sess.Mockup()
	if err != nil {
		t.Fatalf("Mockup: %v", err)
	}
	res, err := sess.Apply(in)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	assertJSONEqual(t, prev.Map, res.Map, "карта предпросмотра и факта")
	gotRemoved, gotStructures, gotCapped := mapDiff(before, res.Map)
	assertJSONEqual(t, []string{eIDSWX, eIDEA, eIDEB, eIDEC}, gotRemoved, "удалённые элементы")
	assertJSONEqual(t, []string{eIDPLATEA}, gotStructures, "порванные сооружения")
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
		mapfmt.Node{ID: eIDNA, Name: "N_A", Ports: []mapfmt.Port{{ID: "P1", Purpose: "buffer_stop"}}},
		mapfmt.Node{ID: eIDNJ, Name: "N_J", Ports: []mapfmt.Port{{ID: "P1"}}},
		mapfmt.Node{ID: eIDNC, Name: "N_C", Ports: []mapfmt.Port{{ID: "P1", Purpose: "buffer_stop"}}},
		mapfmt.Node{ID: eIDND, Name: "N_D", Ports: []mapfmt.Port{{ID: "P1", Purpose: "buffer_stop"}}},
	)
	s, d := rightTurnout(t)
	m.Topology.Turnouts = append(m.Topology.Turnouts, mapfmt.Turnout{
		ID: eIDSWZ, Name: "SWZ", Kind: mapfmt.KindRail, Hand: "right", Drive: mapfmt.DriveElectric,
		Ports: mapfmt.TurnoutPorts{Common: "C", Straight: "S", Diverging: "D"},
	})
	m.Geometry.Turnouts = map[string]mapfmt.TurnoutGeometry{eIDSWZ: {Straight: toAlignments(t, s), Diverging: toAlignments(t, d)}}
	m.Topology.Edges = append(m.Topology.Edges,
		mapfmt.Edge{ID: eIDEA, Name: "EA", Kind: mapfmt.KindRail, From: eIDNA + ".P1", To: eIDSWZ + ".C"},
		mapfmt.Edge{ID: eIDEB, Name: "EB", Kind: mapfmt.KindRail, From: eIDSWZ + ".S", To: eIDNJ + ".P1"},
		mapfmt.Edge{ID: eIDEC, Name: "EC", Kind: mapfmt.KindRail, From: eIDNJ + ".P1", To: eIDNC + ".P1"},
		mapfmt.Edge{ID: eIDED, Name: "ED", Kind: mapfmt.KindRail, From: eIDSWZ + ".D", To: eIDND + ".P1"},
	)
	m.Geometry.Edges[eIDEA] = mapfmt.Alignments{Horizontal: []mapfmt.HPrim{{Kind: "straight", Length: 40}}}
	m.Geometry.Edges[eIDEB] = mapfmt.Alignments{Horizontal: []mapfmt.HPrim{{Kind: "straight", Length: 40}}}
	m.Geometry.Edges[eIDEC] = mapfmt.Alignments{Horizontal: []mapfmt.HPrim{{Kind: "straight", Length: 40}}}
	m.Geometry.Edges[eIDED] = mapfmt.Alignments{Horizontal: []mapfmt.HPrim{{Kind: "straight", Length: 40}}}
	m.Construction.Runs = append(m.Construction.Runs,
		mapfmt.ConstructionRun{ID: eIDRUNEA, Name: "RUN_EA", Coordinate: "u", Phase: 0, Spans: []netloc.IntervalU{{Element: eIDEA, From: 0, To: 40, Direction: "forward"}}},
		mapfmt.ConstructionRun{ID: eIDRUNEBEC, Name: "RUN_EB_EC", Coordinate: "u", Phase: 0, Spans: []netloc.IntervalU{
			{Element: eIDEB, From: 0, To: 40, Direction: "forward"},
			{Element: eIDEC, From: 0, To: 40, Direction: "forward"},
		}},
		mapfmt.ConstructionRun{ID: eIDRUNED, Name: "RUN_ED", Coordinate: "u", Phase: 0, Spans: []netloc.IntervalU{{Element: eIDED, From: 0, To: 40, Direction: "forward"}}},
	)

	svc := newService(t, m)
	sess := openBuilder(t, svc, "a")
	in := Intent{Op: OpErase, Erase: EraseIntent{Target: eIDSWZ, Mode: EraseCascade}}
	prev, err := sess.Preview(in)
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	assertCascade(t, prev.Cascade,
		[]string{eIDEA, eIDEB, eIDED, eIDSWZ},
		nil, []string{eIDNJ + ".P1"})

	res, err := sess.Apply(in)
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
		if len(runs[i].Spans) == 1 && runs[i].Spans[0].Element == eIDEC {
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

// ---- Критерий приёмки: мировая ревизия растёт только на коммите ----

// Мировая ревизия — номер принятого состояния: макет её не двигает, вернуть
// её назад нечем — отмены нет.
func TestCommitsAdvanceWorldRevision(t *testing.T) {
	svc := newService(t, testBaseMap())
	sess := openBuilder(t, svc, "a")

	if _, err := sess.Apply(Intent{Op: OpPlace, Place: PlaceIntent{
		Element: eIDE2, From: 20, To: 60, Side: "right", Offset: 1.745, Width: 3.0, Height: 0.2, SlabThickness: 0.35,
	}}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if got := worldOf(t, svc).MapRevision; got != 1 {
		t.Fatalf("макет сдвинул мировую ревизию: %d", got)
	}

	if err := sess.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if got := worldOf(t, svc).MapRevision; got != 2 {
		t.Fatalf("после коммита ревизия %d, ожидалась 2", got)
	}

	// Принятый макет опустел; второй коммит — отказ, ревизия не растёт.
	if err := sess.Commit(); err == nil {
		t.Fatal("коммит пустой транзакции: ожидался отказ")
	}
	if got := worldOf(t, svc).MapRevision; got != 2 {
		t.Fatalf("ревизия выросла после отказа: %d", got)
	}
}

// ---- Критерий приёмки: транзакция хранит ОПЕРАЦИИ, а не снимок ----

// После переигрывания на изменившейся базе результат совпадает с прямым
// применением тех же операций: журнал — единственное хранилище макета.
func TestTransactionStoresOperations(t *testing.T) {
	svc := newService(t, testBaseMap())
	sA := openBuilder(t, svc, "a")
	sB := openBuilder(t, svc, "b")

	// B держит в журнале только операции: платформа на E2.
	if _, err := sB.Apply(Intent{Op: OpPlace, Place: PlaceIntent{
		Element: eIDE2, From: 20, To: 60, Side: "right", Offset: 1.745, Width: 3.0, Height: 0.2, SlabThickness: 0.35,
	}}); err != nil {
		t.Fatalf("apply B: %v", err)
	}
	ops := sB.Journal()
	if len(ops) != 1 || ops[0].Op != OpPlace {
		t.Fatalf("журнал B: %v", ops)
	}

	// База меняется под B: A коммитит продолжение от тупика.
	if _, err := sA.Apply(Intent{Op: OpExtend, Extend: ExtendIntent{Port: eIDNEND + ".P1", Chain: mustChain(t, 50)}}); err != nil {
		t.Fatalf("apply A: %v", err)
	}
	if err := sA.Commit(); err != nil {
		t.Fatalf("commit A: %v", err)
	}

	// Макет B, переигранный на изменившейся базе, видит чужое продолжение:
	// снимок, снятый при открытии, его бы не показал.
	mock, err := sB.Mockup()
	if err != nil {
		t.Fatalf("mockup B: %v", err)
	}
	if !hasEdgeName(t, &mock, "E_EXT") {
		t.Fatalf("макет B не видит коммит A: продолжение отсутствует")
	}
	if got := len(sB.Journal()); got != 1 {
		t.Fatalf("журнал B изменился от чужого коммита: %d операций", got)
	}

	// Коммит B переигрывает те же операции на текущей базе — результат
	// совпадает с прямым применением: и продолжение A, и платформа B.
	if err := sB.Commit(); err != nil {
		t.Fatalf("commit B: %v", err)
	}
	v, err := svc.Views(RoleDriver)
	if err != nil {
		t.Fatalf("Views: %v", err)
	}
	if !hasEdgeName(t, &v.Committed, "E_EXT") {
		t.Fatalf("мир после коммита B потерял продолжение A")
	}
	if len(v.Committed.Topology.Structures) != 1 {
		t.Fatalf("платформа B не принята: %s", jsonString(t, v.Committed.Topology.Structures))
	}
	assertValid(t, &v.Committed, "мир после обоих коммитов")
}

// ---- Критерий приёмки: незакоммиченное не видно не-строителю ----

// Фильтрация серверная: тест ходит через серверную поверхность (Views), а не
// через поля транзакции. ДСП и машинист видят только закоммиченное.
func TestNonBuilderSeesNoMockups(t *testing.T) {
	svc := newService(t, testBaseMap())
	sA := openBuilder(t, svc, "a")
	if _, err := sA.Apply(Intent{Op: OpExtend, Extend: ExtendIntent{Port: eIDNEND + ".P1", Chain: mustChain(t, 50)}}); err != nil {
		t.Fatalf("apply: %v", err)
	}

	for _, role := range []Role{RoleDispatcher, RoleDriver} {
		v, err := svc.Views(role)
		if err != nil {
			t.Fatalf("Views(%d): %v", role, err)
		}
		if len(v.Mockups) != 0 {
			t.Fatalf("роль %d видит %d макетов", role, len(v.Mockups))
		}
		if hasEdgeName(t, &v.Committed, "E_EXT") {
			t.Fatalf("роль %d видит незакоммиченное", role)
		}
	}

	v, err := svc.Views(RoleBuilder)
	if err != nil {
		t.Fatalf("Views(строитель): %v", err)
	}
	if len(v.Mockups) != 1 {
		t.Fatalf("строитель видит %d макетов, ожидался 1", len(v.Mockups))
	}
	if v.Mockups[0].Account != "a" {
		t.Fatalf("владелец макета %q, ожидалась a", v.Mockups[0].Account)
	}
	if !hasEdgeName(t, &v.Mockups[0].Map, "E_EXT") {
		t.Fatalf("строитель не видит открытый макет")
	}

	// Строители видят стройку друг друга (§2): вторая запись без операций тоже
	// открытый макет, и её видно всем строителям.
	if _, err := svc.OpenSession("b"); err != nil {
		t.Fatalf("OpenSession b: %v", err)
	}
	v, err = svc.Views(RoleBuilder)
	if err != nil {
		t.Fatalf("Views(строитель): %v", err)
	}
	if len(v.Mockups) != 2 {
		t.Fatalf("строитель видит %d макетов, ожидалось 2", len(v.Mockups))
	}
	if v.Mockups[0].Account != "a" || v.Mockups[1].Account != "b" {
		t.Fatalf("макеты в серверном порядке: %q, %q", v.Mockups[0].Account, v.Mockups[1].Account)
	}
}

// ---- Критерий приёмки: транзакция переживает разрыв связи ----

// Сессия — ручка, макет принадлежит учётной записи и живёт в сервисе.
// Сессию отбрасываем, новую открываем — макет цел, журнал не пересоздавался.
func TestTransactionSurvivesConnectionLoss(t *testing.T) {
	svc := newService(t, testBaseMap())
	s1, err := svc.OpenSession("alice")
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	if _, err := s1.Apply(Intent{Op: OpExtend, Extend: ExtendIntent{Port: eIDNEND + ".P1", Chain: mustChain(t, 50)}}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	// «Разрыв связи»: s1 отбрасывается без всякого закрытия. Закрывать нечего —
	// у сессии нет состояния, которым можно было бы убить макет.
	s1 = nil

	s2, err := svc.OpenSession("alice")
	if err != nil {
		t.Fatalf("переподключение: %v", err)
	}
	mock, err := s2.Mockup()
	if err != nil {
		t.Fatalf("mockup: %v", err)
	}
	if !hasEdgeName(t, &mock, "E_EXT") {
		t.Fatalf("макет потерян при разрыве связи")
	}
	if got := len(s2.Journal()); got != 1 {
		t.Fatalf("журнал пересоздан: операций %d, ожидалась 1", got)
	}

	// Чужая учётная запись получает собственный пустой макет: совместного
	// владения нет.
	s3, err := svc.OpenSession("bob")
	if err != nil {
		t.Fatalf("OpenSession bob: %v", err)
	}
	mockB, err := s3.Mockup()
	if err != nil {
		t.Fatalf("mockup bob: %v", err)
	}
	if hasEdgeName(t, &mockB, "E_EXT") {
		t.Fatalf("макет alice виден в собственном макете bob")
	}
}

// ---- Критерий приёмки: коммит, трогающий путь, без закрытия — отказ ----

// Врезка в существующий элемент отбивается по ПРЕДУСЛОВИЮ, а не по
// конфликту, даже когда конкурента нет. Закрытие даёт ДСП; механики
// закрытия пока не существует — отказ честен, молчаливый пропуск
// предусловия нет.
func TestCommitRequiresClosure(t *testing.T) {
	svc := newService(t, testBaseMap())
	sess := openBuilder(t, svc, "a")

	s, d := rightTurnout(t)
	if _, err := sess.Apply(Intent{Op: OpBranch, Branch: BranchIntent{
		Edge: eIDE1, AtU: 50, Hand: "right", Drive: mapfmt.DriveManual, Straight: s, Diverging: d, Branch: mustChain(t, 40),
	}}); err != nil {
		t.Fatalf("apply: %v", err)
	}

	err := sess.Commit()
	if !errors.Is(err, ErrClosureRequired) {
		t.Fatalf("ожидался отказ по предусловию, получено: %v", err)
	}
	if errors.Is(err, ErrConflict) {
		t.Fatalf("отказ обязан быть по предусловию, а не по конфликту: %v", err)
	}
	if !strings.Contains(err.Error(), eIDE1) {
		t.Fatalf("отказ не называет элемент: %v", err)
	}

	// Мир не принял ничего, макет цел — автор может переиграть.
	if w := worldOf(t, svc); len(w.Topology.Turnouts) != 0 {
		t.Fatalf("отбитый коммит оставил след в мире: %s", jsonString(t, w.Topology.Turnouts))
	}
	if got := len(sess.Journal()); got != 1 {
		t.Fatalf("макет не возвращён автору: операций %d", got)
	}
}

// ---- Критерий приёмки: конфликт — отбой на коммите второго ----

// Сценарий владельца: два макета врезаются в один НОВЫЙ элемент; движения на
// нём нет по построению — предусловие выполнено у обоих; первый коммитится,
// второй получает отбой ПО КОНФЛИКТУ, а не занятие вперёд. Два отбоя
// (предусловие и конфликт) различимы вызывающим по типу ошибки, а не по
// строке для человека.
func TestConflictRejectedAtCommit(t *testing.T) {
	svc := newService(t, testBaseMap())
	sA := openBuilder(t, svc, "a")
	sB := openBuilder(t, svc, "b")

	// A строит НОВЫЙ путь в чистом поле: продолжение от тупика. Предусловие
	// выполнено — движения на нём нет по построению.
	if _, err := sA.Apply(Intent{Op: OpExtend, Extend: ExtendIntent{Port: eIDNEND + ".P1", Chain: mustChain(t, 50)}}); err != nil {
		t.Fatalf("apply A: %v", err)
	}
	if err := sA.Commit(); err != nil {
		t.Fatalf("первый коммит: %v", err)
	}
	extID := edgeIDByName(t, worldOf(t, svc), "E_EXT")

	// B врезается в тот же элемент: операция применима (элемент уже в
	// закоммиченном мире), предусловие выполнено (элемент создан после
	// открытия макета B — движения на нём не было по построению). Отбой —
	// только на коммите, занятия участка вперёд нет.
	s, d := rightTurnout(t)
	if _, err := sB.Apply(Intent{Op: OpBranch, Branch: BranchIntent{
		Edge: extID, AtU: 25, Hand: "right", Drive: mapfmt.DriveManual, Straight: s, Diverging: d, Branch: mustChain(t, 20),
	}}); err != nil {
		t.Fatalf("apply B: %v", err)
	}

	err := sB.Commit()
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("ожидался отбой по конфликту, получено: %v", err)
	}
	if errors.Is(err, ErrClosureRequired) {
		t.Fatalf("отбой обязан быть по конфликту, а не по предусловию: %v", err)
	}
	if !strings.Contains(err.Error(), extID) {
		t.Fatalf("отбой не называет элемент: %v", err)
	}

	// Мир остался с коммитом A; макет B возвращён автору целиком.
	if w := worldOf(t, svc); len(w.Topology.Turnouts) != 0 {
		t.Fatalf("отбитый коммит B оставил стрелку в мире: %s", jsonString(t, w.Topology.Turnouts))
	}
	if got := len(sB.Journal()); got != 1 {
		t.Fatalf("макет B не возвращён автору: операций %d", got)
	}
}

// ---- Критерий приёмки: производный рельеф НЕ конфликтует ----

// Два макета, чьи земляные коридоры пересекаются, но элементы разные, — НЕ
// конфликт (спека §5): земляные работы — функция закоммиченных осей, а не
// независимая запись; конфликтуют элементы. Коридор earthworks на рецепте —
// reach = 36.5 м; компоненты в 20 м друг от друга перекрывают коридоры, но
// оси параллельны и не пересекаются — модель конфликта на геометрию не
// смотрит вовсе.
func TestEarthworksCorridorsDoNotConflict(t *testing.T) {
	svc := newService(t, parallelBase())
	sA := openBuilder(t, svc, "a")
	sB := openBuilder(t, svc, "b")

	if _, err := sA.Apply(Intent{Op: OpExtend, Extend: ExtendIntent{Port: eIDN1 + ".P1", Chain: mustChain(t, 100)}}); err != nil {
		t.Fatalf("apply A: %v", err)
	}
	if _, err := sB.Apply(Intent{Op: OpExtend, Extend: ExtendIntent{Port: eIDN2 + ".P1", Chain: mustChain(t, 100)}}); err != nil {
		t.Fatalf("apply B: %v", err)
	}

	if err := sA.Commit(); err != nil {
		t.Fatalf("коммит A: %v", err)
	}
	if err := sB.Commit(); err != nil {
		t.Fatalf("коммит B: %v", err)
	}

	w := worldOf(t, svc)
	if len(w.Topology.Edges) != 4 {
		t.Fatalf("рёбер %d, ожидалось 4: %s", len(w.Topology.Edges), jsonString(t, w.Topology.Edges))
	}
	assertValid(t, w, "мир после обоих коммитов")
}

// ---- Критерий приёмки: отмены НЕТ ----

// Отмены нет — проверить это можно только механически: вызвать её нечем.
// Обходим экспортируемую поверхность пакета и убеждаемся, что ни стека
// ревизий, ни операции отката, ни прежнего Store (режим прямой правки мира,
// спека §1) в API нет. Комментарий «отмены нет» — не доказательство;
// отсутствие имени в API — да.
func TestUndoRemoved(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	var files []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go") {
			files = append(files, name)
		}
	}
	if len(files) == 0 {
		t.Fatal("не нашлись исходники пакета")
	}
	undoRe := regexp.MustCompile(`(?i)undo|revert|rollback|redo|restore|history|revision`)
	var found []string
	hasStore := false
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("ReadFile %s: %v", f, err)
		}
		astFile, err := parser.ParseFile(token.NewFileSet(), f, src, 0)
		if err != nil {
			t.Fatalf("ParseFile %s: %v", f, err)
		}
		for _, decl := range astFile.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if d.Name.IsExported() && undoRe.MatchString(d.Name.Name) {
					found = append(found, f+": "+d.Name.Name)
				}
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch sp := spec.(type) {
					case *ast.TypeSpec:
						if sp.Name.IsExported() {
							if undoRe.MatchString(sp.Name.Name) {
								found = append(found, f+": "+sp.Name.Name)
							}
							if sp.Name.Name == "Store" {
								hasStore = true
							}
						}
					case *ast.ValueSpec:
						for _, nm := range sp.Names {
							if nm.IsExported() && undoRe.MatchString(nm.Name) {
								found = append(found, f+": "+nm.Name)
							}
						}
					}
				}
			}
		}
	}
	if len(found) > 0 {
		t.Fatalf("в API пакета осталась поверхность отмены: %s", strings.Join(found, "; "))
	}
	if hasStore {
		t.Fatal("в API пакета остался прежний Store — режим прямой правки мира")
	}
}

// ---- Run'ы: пересчёт и стабильность ----

// Пересчёт run'ов на неизменной топологии воспроизводит прежнюю решётку
// байт в байт, включая авторские ID.
func TestRunsReproducedOnUnchangedTopology(t *testing.T) {
	svc := newService(t, testBaseMap())
	sess := openBuilder(t, svc, "a")
	res, err := sess.Preview(Intent{Op: OpPlace, Place: PlaceIntent{
		Element: eIDE2, From: 20, To: 60, Side: "right", Offset: 1.745, Width: 3.0, Height: 0.2, SlabThickness: 0.35,
	}})
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	assertJSONEqual(t, testBaseMap().Construction.Runs, res.Map.Construction.Runs,
		"run'ы после правки, не меняющей топологию")

	// Карта с настоящей горловиной — из фабрики, а не из файла.
	fx := *seedmap.Station()
	assertValid(t, &fx, "станция фабрики")
	svc2 := newService(t, &fx)
	sess2 := openBuilder(t, svc2, "a")
	before := fx.Construction.Runs
	res2, err := sess2.Preview(Intent{Op: OpCap, Cap: CapIntent{Port: seedmap.StationStopMainNode + ".P1"}})
	if err != nil {
		t.Fatalf("Preview над fixture: %v", err)
	}
	assertJSONEqual(t, before, res2.Map.Construction.Runs,
		"run'ы fixture_station после правки, не меняющей топологию")
}

// Продление пути сливает run продолжения с run'ом исходной решётки: шпалы не
// переставляются через стык.
func TestExtendMergesRun(t *testing.T) {
	svc := newService(t, testBaseMap())
	sess := openBuilder(t, svc, "a")
	res, err := sess.Apply(Intent{Op: OpExtend, Extend: ExtendIntent{
		Port:  eIDNEND + ".P1",
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
	// Новое ребро получает UUID из источника тождества, метка E_EXT.
	extID := edgeIDByName(t, &res.Map, "E_EXT")
	if merged.Spans[0].Element != eIDE0 || merged.Spans[0].Direction != "forward" ||
		merged.Spans[3].Element != extID || merged.Spans[3].Direction != "forward" {
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
		Anchors: map[string]mapfmt.Anchor{eIDNB + ".P1": {X: 200, Y: 0, Z: 0, Heading: math.Pi}},
		Topology: mapfmt.Topology{
			Nodes: []mapfmt.Node{
				{ID: eIDNA, Name: "N_A", Ports: []mapfmt.Port{{ID: "P1", Purpose: "map_boundary"}}},
				{ID: eIDNJ, Name: "N_J", Ports: []mapfmt.Port{{ID: "P1"}}},
				{ID: eIDNB, Name: "N_B", Ports: []mapfmt.Port{{ID: "P1", Purpose: "buffer_stop"}}},
			},
			Edges: []mapfmt.Edge{
				{ID: eIDE5, Name: "E5", Kind: mapfmt.KindRail, From: eIDNA + ".P1", To: eIDNJ + ".P1"},
				{ID: eIDE6, Name: "E6", Kind: mapfmt.KindRail, From: eIDNB + ".P1", To: eIDNJ + ".P1"},
			},
		},
		Geometry: mapfmt.Geometry{
			Edges: map[string]mapfmt.Alignments{
				eIDE5: {Horizontal: []mapfmt.HPrim{{Kind: "straight", Length: 100}}},
				eIDE6: {Horizontal: []mapfmt.HPrim{{Kind: "straight", Length: 100}}},
			},
		},
		Construction: &mapfmt.Construction{
			DefaultType: eIDTYPE,
			Types:       []mapfmt.TrackType{{ID: eIDTYPE, Name: "TRACK_MAIN_1520", Gauge: 1.520, Rail: mapfmt.TrackRail{Height: 0.18, HeadWidth: 0.075}, Sleeper: mapfmt.TrackSleeper{Pitch: 0.6, Length: 2.5, Width: 0.28, Height: 0.20}, Ballast: mapfmt.TrackBallast{HalfWidth: 1.75, Depth: 0.30, CribDepth: 0.10, SideSlope: 1.5}, Timber: &mapfmt.TrackTimber{Pitch: 0.50, LengthMax: 5.50, Width: 0.30, Height: 0.20}}},
			Runs: []mapfmt.ConstructionRun{
				{ID: eIDRUN56, Name: "RUN_E5_E6", Coordinate: "u", Phase: 0, Spans: []netloc.IntervalU{
					{Element: eIDE5, From: 0, To: 100, Direction: "forward"},
					{Element: eIDE6, From: 0, To: 100, Direction: "reverse"},
				}},
			},
		},
	}
	svc := newService(t, m)
	sess := openBuilder(t, svc, "a")

	// Правка, не меняющая топологию: run воспроизводится, включая ID.
	res, err := sess.Preview(Intent{Op: OpCap, Cap: CapIntent{Port: eIDNJ + ".P1"}})
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	assertJSONEqual(t, m.Construction.Runs, res.Map.Construction.Runs, "run'ы To-To стыка")

	// Стираем одно ребро: второй остаётся, стык закрывается упором.
	er, err := sess.Apply(Intent{Op: OpErase, Erase: EraseIntent{Target: eIDE5, Mode: EraseCascade}})
	if err != nil {
		t.Fatalf("Erase: %v", err)
	}
	assertCascade(t, er.Cascade, []string{eIDE5}, nil, []string{eIDNJ + ".P1"})
	runs := er.Map.Construction.Runs
	if len(runs) != 1 || runs[0].Spans[0].Element != eIDE6 {
		t.Fatalf("run'ы после стирки: %s", jsonString(t, runs))
	}
	assertValid(t, &er.Map, "карта после стирки одного ребра стыка")
}

// Фаза run'а — авторитетный факт: правка, не меняющая физическую решётку,
// обязана её сохранить, а не сбросить в ноль.
func TestRunsPreservePhaseAcrossEdit(t *testing.T) {
	m := testBaseMap()
	m.Construction.Runs[0].Phase = 0.3
	svc := newService(t, m)
	sess := openBuilder(t, svc, "a")

	res, err := sess.Apply(Intent{Op: OpPlace, Place: PlaceIntent{
		Element: eIDE2, From: 20, To: 60, Side: "right", Offset: 1.745, Width: 3.0, Height: 0.2, SlabThickness: 0.35,
	}})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	got := map[string]float64{}
	for _, r := range res.Map.Construction.Runs {
		got[r.ID] = r.Phase
	}
	if got[eIDRUN] != 0.3 {
		t.Fatalf("фаза RUN_E0_E1_E2 %v, ожидалась 0.3: %s", got[eIDRUN], jsonString(t, res.Map.Construction.Runs))
	}
}

// ---- Воспроизводимость с внедряемым источником тождества ----

// Операции с подставленным источником тождества воспроизводимы (W3-A): два
// независимых экземпляра uuidv7.Deterministic() выдают одну и ту же
// последовательность, поэтому макеты после одинаковых операций совпадают
// байт в байт. Системный источник uuidv7.New() в этот расчёт не входит —
// Service пользуется только инъекцией.
func TestEditReproducibleWithInjectedSource(t *testing.T) {
	apply := func(t *testing.T) mapfmt.Map {
		t.Helper()
		svc := newService(t, testBaseMap())
		sess := openBuilder(t, svc, "a")
		// Продление и ветвление: вместе они выдают 7 новых идентификаторов —
		// ребро и узел продолжения, стрелка, продолжение и ветвь с узлом и
		// run ветви.
		ext, err := sess.Apply(Intent{Op: OpExtend, Extend: ExtendIntent{
			Port:  eIDNEND + ".P1",
			Chain: mustChain(t, 50),
		}})
		if err != nil {
			t.Fatalf("extend: %v", err)
		}
		s, d := rightTurnout(t)
		br, err := sess.Apply(Intent{Op: OpBranch, Branch: BranchIntent{
			Edge:      eIDE1,
			AtU:       50,
			Hand:      "right",
			Drive:     mapfmt.DriveManual,
			Straight:  s,
			Diverging: d,
			Branch:    mustChain(t, 40),
		}})
		if err != nil {
			t.Fatalf("branch: %v", err)
		}
		assertValid(t, &ext.Map, "продление с источником A/B")
		assertValid(t, &br.Map, "ветвление с источником A/B")
		return br.Map
	}

	a := apply(t)
	b := apply(t)
	assertJSONEqual(t, a, b, "макеты после одинаковых операций с двумя источниками")
}

// ---- Критерий приёмки: прямой терраморфинг ----

// gradeCell — клетка правки уровня 0: фикстура для тестов терраморфинга.
func gradeCell(cx, cz int, height int16) terrain.GradeCell {
	return terrain.GradeCell{Level: 0, CX: cx, CZ: cz, HeightCm: height}
}

// TestGradingConflictByCellAtCommit — два макета, правящие ОДНУ клетку, —
// конфликт на коммите (спека транзакций §5): в прямой правке высоты —
// редактируемые данные, и клетка входит в предмет конфликта наравне с
// идентификатором элемента. Макет, правящий ДРУГУЮ клетку, коммитится
// свободно — конфликт по клетке, а не по площади мира.
func TestGradingConflictByCellAtCommit(t *testing.T) {
	svc := newService(t, testBaseMap())
	sA := openBuilder(t, svc, "a")
	sB := openBuilder(t, svc, "b")

	cell := gradeCell(1, 2, 500)
	if _, err := sA.Apply(Intent{Op: OpGrade, Grade: GradeIntent{Cells: []terrain.GradeCell{cell}}}); err != nil {
		t.Fatalf("apply A: %v", err)
	}
	if err := sA.Commit(); err != nil {
		t.Fatalf("коммит A: %v", err)
	}

	if _, err := sB.Apply(Intent{Op: OpGrade, Grade: GradeIntent{Cells: []terrain.GradeCell{cell}}}); err != nil {
		t.Fatalf("apply B: %v", err)
	}
	if err := sB.Commit(); !errors.Is(err, ErrConflict) {
		t.Fatalf("коммит B поверх той же клетки: %v — ожидался ErrConflict", err)
	}

	sC := openBuilder(t, svc, "c")
	if _, err := sC.Apply(Intent{Op: OpGrade, Grade: GradeIntent{Cells: []terrain.GradeCell{gradeCell(3, 4, 600)}}}); err != nil {
		t.Fatalf("apply C: %v", err)
	}
	if err := sC.Commit(); err != nil {
		t.Fatalf("коммит C (другая клетка): %v — конфликт по клетке, а не по миру", err)
	}
}

// TestGradingSameCellInMockupRefused — в одном макете клетка не может быть
// правлена с РАЗНЫМИ отметками: «последний победил» запрещён (спека §5 — он
// сделал бы результат функцией порядка операций в журнале, а инвариант §3
// контракта требует порядок-независимости). Повтор РАВНОЙ отметки
// идемпотентен: множество, а не слой.
func TestGradingSameCellInMockupRefused(t *testing.T) {
	svc := newService(t, testBaseMap())
	s := openBuilder(t, svc, "a")
	if _, err := s.Apply(Intent{Op: OpGrade, Grade: GradeIntent{Cells: []terrain.GradeCell{gradeCell(1, 2, 500)}}}); err != nil {
		t.Fatalf("первая правка: %v", err)
	}
	if _, err := s.Apply(Intent{Op: OpGrade, Grade: GradeIntent{Cells: []terrain.GradeCell{gradeCell(1, 2, 700)}}}); err == nil {
		t.Fatal("вторая правка той же клетки с другой отметкой принята — обязана отказать")
	}
	if _, err := s.Apply(Intent{Op: OpGrade, Grade: GradeIntent{Cells: []terrain.GradeCell{gradeCell(1, 2, 500)}}}); err != nil {
		t.Fatalf("повтор той же отметки отказан: %v — повтор идемпотентен", err)
	}
	if err := s.Commit(); err != nil {
		t.Fatalf("коммит: %v", err)
	}
}

// TestGradingOrderIndependentInMockup — порядок применения двух правок не
// влияет на результат (спека §3, следствие 1): каждая правка — функция
// природной поверхности и своего исходника, и коммит сворачивает их в одно
// множество клеток. Два порядка дают одно и то же закоммиченное множество.
func TestGradingOrderIndependentInMockup(t *testing.T) {
	run := func(first, second []terrain.GradeCell) terrain.Grading {
		t.Helper()
		svc := newService(t, testBaseMap())
		s := openBuilder(t, svc, "a")
		if _, err := s.Apply(Intent{Op: OpGrade, Grade: GradeIntent{Cells: first}}); err != nil {
			t.Fatalf("apply A: %v", err)
		}
		if _, err := s.Apply(Intent{Op: OpGrade, Grade: GradeIntent{Cells: second}}); err != nil {
			t.Fatalf("apply B: %v", err)
		}
		if err := s.Commit(); err != nil {
			t.Fatalf("коммит: %v", err)
		}
		v, err := svc.Views(RoleBuilder)
		if err != nil {
			t.Fatalf("Views: %v", err)
		}
		return v.Grading
	}
	a := []terrain.GradeCell{gradeCell(1, 2, 500)}
	b := []terrain.GradeCell{gradeCell(3, 4, 700)}
	gAB := run(a, b)
	gBA := run(b, a)
	if !reflect.DeepEqual(gAB, gBA) {
		t.Fatalf("порядок правок повлиял на результат: %+v vs %+v", gAB, gBA)
	}
}

// TestGradingCommittedWorldCarriesPatch — коммит сворачивает правки в
// закоммиченный мир (Views.Grading): правка — ИСХОДНИК рядом с картой, а не
// проекция, и компилятор (worldgen) получает её оттуда. Это половина
// критерия биды «правка переживает пересев»; вторая — пересев воспроизводит
// землю из этого исходника — живёт в worldgen (TestGradingSurvivesReseed).
func TestGradingCommittedWorldCarriesPatch(t *testing.T) {
	svc := newService(t, testBaseMap())
	s := openBuilder(t, svc, "a")
	if _, err := s.Apply(Intent{Op: OpGrade, Grade: GradeIntent{Cells: []terrain.GradeCell{
		gradeCell(1, 2, 500), gradeCell(3, 4, 700),
	}}}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	v, err := svc.Views(RoleDriver)
	if err != nil {
		t.Fatalf("Views: %v", err)
	}
	if len(v.Grading.Cells) != 0 {
		t.Fatalf("до коммита правки уже в закоммиченном мире: %+v", v.Grading.Cells)
	}
	if err := s.Commit(); err != nil {
		t.Fatalf("коммит: %v", err)
	}
	v, err = svc.Views(RoleDriver)
	if err != nil {
		t.Fatalf("Views: %v", err)
	}
	if len(v.Grading.Cells) != 2 {
		t.Fatalf("после коммита правок %d, ожидалось 2: %+v", len(v.Grading.Cells), v.Grading.Cells)
	}
}

// TestGradingNeedsNoClosure — предусловие приёмки висит на пути (спека §3):
// правка земли движения не несёт, и коммит правки не требует закрытия даже
// над картой с существующими элементами.
func TestGradingNeedsNoClosure(t *testing.T) {
	svc := newService(t, testBaseMap())
	s := openBuilder(t, svc, "a")
	if _, err := s.Apply(Intent{Op: OpGrade, Grade: GradeIntent{Cells: []terrain.GradeCell{gradeCell(0, 0, 300)}}}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if err := s.Commit(); err != nil {
		t.Fatalf("коммит правки высот над существующей картой: %v — правка не несёт движения и не требует закрытия", err)
	}
}

// TestGradingLevelNotZeroRefused — правка адресуется клеткой уровня 0, и
// только им: клетки разных уровней пересекались бы в плане, оставаясь
// разными ключами, и конфликт «по клетке» был бы пропущен.
func TestGradingLevelNotZeroRefused(t *testing.T) {
	svc := newService(t, testBaseMap())
	s := openBuilder(t, svc, "a")
	_, err := s.Apply(Intent{Op: OpGrade, Grade: GradeIntent{Cells: []terrain.GradeCell{
		{Level: 1, CX: 0, CZ: 0, HeightCm: 500},
	}}})
	if err == nil {
		t.Fatal("правка уровня 1 принята — разрешён только уровень 0")
	}
}

// TestGradingLaterCommitRedefinesCell — коммит, лёгший ПОСЛЕ создания макета,
// конфликтует, а коммит, лёгший ДО, — прежняя история: клетка, правленная
// прежним коммитом, переопределяется новым. Журнал сервера линеен, и «новое
// определение атома данных» здесь — сам журнал, а не гонка доставки (спека
// §5 запрещает «последний победил» как зависимость от ПОРЯДКА ДОСТАВКИ, а не
// как замену данных на коммите).
func TestGradingLaterCommitRedefinesCell(t *testing.T) {
	svc := newService(t, testBaseMap())
	sA := openBuilder(t, svc, "a")
	if _, err := sA.Apply(Intent{Op: OpGrade, Grade: GradeIntent{Cells: []terrain.GradeCell{gradeCell(1, 2, 500)}}}); err != nil {
		t.Fatalf("apply A: %v", err)
	}
	if err := sA.Commit(); err != nil {
		t.Fatalf("коммит A: %v", err)
	}

	// Макет создан ПОСЛЕ коммита A: окно конфликта пусто, клетка
	// переопределяется новой отметкой.
	sB := openBuilder(t, svc, "b")
	if _, err := sB.Apply(Intent{Op: OpGrade, Grade: GradeIntent{Cells: []terrain.GradeCell{gradeCell(1, 2, 700)}}}); err != nil {
		t.Fatalf("apply B: %v", err)
	}
	if err := sB.Commit(); err != nil {
		t.Fatalf("коммит B поверх клетки прежнего коммита: %v — прежняя история не конфликт", err)
	}
	v, err := svc.Views(RoleDriver)
	if err != nil {
		t.Fatalf("Views: %v", err)
	}
	if len(v.Grading.Cells) != 1 || v.Grading.Cells[0].HeightCm != 700 {
		t.Fatalf("клетка не переопределена: %+v — ожидалась одна клетка с отметкой 700", v.Grading.Cells)
	}
}
