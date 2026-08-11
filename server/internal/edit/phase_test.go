package edit

import (
	"math"
	"sort"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/netloc"
)

// ---- Требование 6: смена сегментации карты не переставляет шпалы ----
//
// Тесты сравнивают ФИЗИЧЕСКИЕ позиции шпал (мировые координаты вдоль цепочки
// карты), а не поля структур: сверка полей прошла бы и при неверной формуле,
// потому что проверяла бы написанное против написанного. Мировая ось — своя,
// независимая от run'ов: отсчёт от порта якоря по геометрии рёбер.

// ---- Мировая ось тестовых карт ----

// worldFrame — мировая ось вдоль физической цепочки карты. Для тестовых карт
// (цепочки прямых рёбер от якоря) это точная мировая координата: offset —
// смещение From-порта ребра, length — его длина.
type worldFrame struct {
	offset map[string]float64
	length map[string]float64
}

func buildWorldFrame(t *testing.T, m *mapfmt.Map) worldFrame {
	t.Helper()
	f := worldFrame{offset: map[string]float64{}, length: map[string]float64{}}
	for _, e := range m.Topology.Edges {
		f.length[e.ID] = alignmentLengthMeters(t, m.Geometry.Edges[e.ID])
	}
	start := ""
	for p := range m.Anchors {
		start = p
		break
	}
	if start == "" {
		t.Fatal("карта без якоря")
	}
	pos := map[string]float64{start: 0}
	seen := map[string]bool{start: true}
	queue := []string{start}
	for len(queue) > 0 {
		p := queue[0]
		queue = queue[1:]
		for _, e := range m.Topology.Edges {
			if e.From == p && !seen[e.To] {
				f.offset[e.ID] = pos[p]
				pos[e.To] = pos[p] + f.length[e.ID]
				seen[e.To] = true
				queue = append(queue, e.To)
			}
		}
	}
	return f
}

// alignmentLengthMeters — длина горизонтальной цепочки в метрах, без
// округления: точная позиционная база теста.
func alignmentLengthMeters(t *testing.T, al mapfmt.Alignments) float64 {
	t.Helper()
	total := 0.0
	for _, h := range al.Horizontal {
		switch h.Kind {
		case "straight":
			total += h.Length
		case "arc":
			total += math.Abs(h.Radius * h.Angle)
		default:
			t.Fatalf("неизвестный примитив %q", h.Kind)
		}
	}
	return total
}

// runPoint — мировая позиция точки run'а на кумулятивной координате g.
func (f worldFrame) runPoint(t *testing.T, r mapfmt.ConstructionRun, g float64) float64 {
	t.Helper()
	cum := 0.0
	for _, sp := range r.Spans {
		off, ok := f.offset[sp.Element]
		if !ok {
			t.Fatalf("нет смещения элемента %s", sp.Element)
		}
		spanLen := sp.To - sp.From
		if g <= cum+spanLen {
			local := g - cum
			if sp.Direction == "reverse" {
				return off + f.length[sp.Element] - sp.From - local
			}
			return off + sp.From + local
		}
		cum += spanLen
	}
	t.Fatalf("точка %v вне run'а (длина %v)", g, cum)
	return 0
}

// runCoord — кумулятивная координата run'а в физической точке x; ok=false,
// если точка не покрыта ни одним спаном.
func (f worldFrame) runCoord(t *testing.T, r mapfmt.ConstructionRun, x float64) (float64, bool) {
	t.Helper()
	cum := 0.0
	for _, sp := range r.Spans {
		off, ok := f.offset[sp.Element]
		if !ok {
			t.Fatalf("нет смещения элемента %s", sp.Element)
		}
		U := f.length[sp.Element]
		var lo, hi float64
		if sp.Direction == "reverse" {
			lo, hi = off+U-sp.To, off+U-sp.From
		} else {
			lo, hi = off+sp.From, off+sp.To
		}
		if x >= lo && x <= hi {
			if sp.Direction == "reverse" {
				return cum + (off + U - sp.From - x), true
			}
			return cum + (x - off - sp.From), true
		}
		cum += sp.To - sp.From
	}
	return 0, false
}

// sleeperPositions — мировые позиции шпал run'а: полуоткрытое правило
// размещения phase + n·pitch ∈ [0, длина run'а).
func sleeperPositions(t *testing.T, m *mapfmt.Map, f worldFrame, r mapfmt.ConstructionRun) []float64 {
	t.Helper()
	pitch := runPitch(m, r)
	if pitch <= 0 {
		t.Fatalf("шаг шпал run'а %s не разрешился", r.ID)
	}
	var out []float64
	for g := r.Phase; g < runLen(r); g += pitch {
		out = append(out, f.runPoint(t, r, g))
	}
	return out
}

func runLen(r mapfmt.ConstructionRun) float64 {
	var L float64
	for _, sp := range r.Spans {
		L += sp.To - sp.From
	}
	return L
}

// assertSameSleeperSet — множества физических позиций шпал совпадают.
func assertSameSleeperSet(t *testing.T, a, b []float64, what string) {
	t.Helper()
	sort.Float64s(a)
	sort.Float64s(b)
	if len(a) != len(b) {
		t.Fatalf("%s: шпал %d до и %d после", what, len(a), len(b))
	}
	const tol = 1e-9
	for i := range a {
		if math.Abs(a[i]-b[i]) > tol {
			t.Fatalf("%s: шпала %d: %v до, %v после (разница %v)", what, i, a[i], b[i], a[i]-b[i])
		}
	}
}

// assertEverySleeperStillPresent — каждая шпала из a осталась в b.
func assertEverySleeperStillPresent(t *testing.T, a, b []float64, what string) {
	t.Helper()
	sort.Float64s(a)
	sort.Float64s(b)
	const tol = 1e-9
	j := 0
	for _, x := range a {
		for j < len(b) && b[j] < x-tol {
			j++
		}
		if j >= len(b) || math.Abs(b[j]-x) > tol {
			t.Fatalf("%s: шпала в точке %v исчезла", what, x)
		}
	}
}

// ---- 1. reverseRun: фаза g' = L − g (по модулю шага) ----

// Разворот run'а обязан оставить шпалы на тех же физических местах, а вход —
// не тронутым (функция принимает run по значению).
func TestReverseRunKeepsSleepersOnSamePhysicalSpots(t *testing.T) {
	m := testBaseMap()
	// Run начинается у порта N_END.P1 (первый спад — E2 reverse) — форма,
	// которую разворачивает extendRuns при продлении от порта.
	m.Construction.Runs[0] = mapfmt.ConstructionRun{
		ID: "RUN_E2_E1_E0", Coordinate: "u", Phase: 0.2,
		Spans: []netloc.IntervalU{
			{Element: "E2", From: 0, To: 100, Direction: "reverse"},
			{Element: "E1", From: 0, To: 100, Direction: "reverse"},
			{Element: "E0", From: 0, To: 100, Direction: "reverse"},
		},
	}
	assertValid(t, m, "run, начинающийся у порта")

	in := m.Construction.Runs[0]
	inSpans := make([]netloc.IntervalU, len(in.Spans))
	copy(inSpans, in.Spans)

	got := reverseRun(m, in)

	// Вход не мутируется: спад и фаза должны остаться как были.
	assertJSONEqual(t, inSpans, in.Spans, "спаны входа после reverseRun")
	// Структура: порядок наоборот, направления перевёрнуты, ID сохранён,
	// фаза нормализована по шагу: (300 − 0.2) mod 0.6 = 0.4.
	wantSpans := []netloc.IntervalU{
		{Element: "E0", From: 0, To: 100, Direction: "forward"},
		{Element: "E1", From: 0, To: 100, Direction: "forward"},
		{Element: "E2", From: 0, To: 100, Direction: "forward"},
	}
	assertJSONEqual(t, wantSpans, got.Spans, "спаны после разворота")
	if math.Abs(got.Phase-0.4) > 1e-9 {
		t.Fatalf("фаза после разворота %v, ожидалась 0.4", got.Phase)
	}
	if got.ID != in.ID || got.Coordinate != in.Coordinate {
		t.Fatalf("reverseRun изменил id/coordinate: %s", jsonString(t, got))
	}

	// Физические позиции шпал до и после — одно и то же множество точек.
	frame := buildWorldFrame(t, m)
	pre := sleeperPositions(t, m, frame, in)
	post := sleeperPositions(t, m, frame, got)
	assertSameSleeperSet(t, pre, post, "разворот run'а")
}

// ---- 2. extendRuns: run начинался у порта (idx == 0) ----

// Продление от порта, где run начинался: ветка idx == 0 — run разворачивается
// и продолжается новым ребром, шпалы прежней решётки остаются на местах.
func TestExtendReversesRunStartingAtPort(t *testing.T) {
	m := testBaseMap()
	m.Construction.Runs[0] = mapfmt.ConstructionRun{
		ID: "RUN_E2_E1_E0", Coordinate: "u", Phase: 0.2,
		Spans: []netloc.IntervalU{
			{Element: "E2", From: 0, To: 100, Direction: "reverse"},
			{Element: "E1", From: 0, To: 100, Direction: "reverse"},
			{Element: "E0", From: 0, To: 100, Direction: "reverse"},
		},
	}
	st := newStore(t, m)

	res, err := st.Apply(Intent{Op: OpExtend, Extend: ExtendIntent{
		Port:  "N_END.P1",
		Chain: mustChain(t, 50),
	}})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	assertValid(t, &res.Map, "карта после продления от порта")

	runs := res.Map.Construction.Runs
	if len(runs) != 1 {
		t.Fatalf("run'ов %d, ожидался 1: %s", len(runs), jsonString(t, runs))
	}
	r := runs[0]
	wantSpans := []netloc.IntervalU{
		{Element: "E0", From: 0, To: 100, Direction: "forward"},
		{Element: "E1", From: 0, To: 100, Direction: "forward"},
		{Element: "E2", From: 0, To: 100, Direction: "forward"},
		{Element: "E_EXT", From: 0, To: 50, Direction: "forward"},
	}
	assertJSONEqual(t, wantSpans, r.Spans, "run после продления")
	if math.Abs(r.Phase-0.4) > 1e-9 {
		t.Fatalf("фаза %v, ожидалась 0.4", r.Phase)
	}

	// Шпалы прежней решётки остались на своих физических местах.
	preFrame := buildWorldFrame(t, m)
	postFrame := buildWorldFrame(t, &res.Map)
	pre := sleeperPositions(t, m, preFrame, m.Construction.Runs[0])
	post := sleeperPositions(t, &res.Map, postFrame, r)
	assertEverySleeperStillPresent(t, pre, post, "продление от порта")
}

// ---- 3. splitRuns: рез не двигает решётку ----

// splitTestMap — цепочка E0/E1/E2 с одним run'ом из spans, фаза 0.2.
func splitTestMap(spans []netloc.IntervalU) *mapfmt.Map {
	m := testBaseMap()
	m.Construction.Runs[0] = mapfmt.ConstructionRun{
		ID: "RUN_E0_E1_E2", Coordinate: "u", Phase: 0.2, Spans: spans,
	}
	return m
}

func TestSplitRunsKeepsCumulativeLength(t *testing.T) {
	for _, tc := range []struct {
		name  string
		spans []netloc.IntervalU
		want  []netloc.IntervalU
	}{
		{
			name: "forward",
			spans: []netloc.IntervalU{
				{Element: "E0", From: 0, To: 100, Direction: "forward"},
				{Element: "E1", From: 0, To: 100, Direction: "forward"},
				{Element: "E2", From: 0, To: 100, Direction: "forward"},
			},
			// Голова [0, 50] остаётся на E1, хвост [0, 50] переходит на E1_CONT;
			// направление сохранено.
			want: []netloc.IntervalU{
				{Element: "E0", From: 0, To: 100, Direction: "forward"},
				{Element: "E1", From: 0, To: 50, Direction: "forward"},
				{Element: "E1_CONT", From: 0, To: 50, Direction: "forward"},
				{Element: "E2", From: 0, To: 100, Direction: "forward"},
			},
		},
		{
			name: "reverse",
			spans: []netloc.IntervalU{
				{Element: "E0", From: 0, To: 100, Direction: "reverse"},
				{Element: "E1", From: 0, To: 100, Direction: "reverse"},
				{Element: "E2", From: 0, To: 100, Direction: "reverse"},
			},
			// Reverse проходится от хвоста к голове: въезд с конца ребра,
			// поэтому порядок после реза — E1_CONT (хвост) перед E1 (голова).
			want: []netloc.IntervalU{
				{Element: "E0", From: 0, To: 100, Direction: "reverse"},
				{Element: "E1_CONT", From: 0, To: 50, Direction: "reverse"},
				{Element: "E1", From: 0, To: 50, Direction: "reverse"},
				{Element: "E2", From: 0, To: 100, Direction: "reverse"},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pre := splitTestMap(tc.spans)

			// Состояние после реза: E1 — голова [0, 50], E1_CONT — хвост [0, 50];
			// оба обычные рёбра цепочки со стыком в N_X, физически ничего не
			// сдвинулось — тест изолирует splitRuns от устройства стрелки.
			post := splitTestMap(tc.spans)
			post.Topology.Edges = []mapfmt.Edge{
				{ID: "E0", From: "N_B.P1", To: "N1.P1"},
				{ID: "E1", From: "N1.P1", To: "N_X.P1"},
				{ID: "E1_CONT", From: "N_X.P1", To: "N2.P1"},
				{ID: "E2", From: "N2.P1", To: "N_END.P1"},
			}
			post.Topology.Nodes = append(post.Topology.Nodes, mapfmt.Node{ID: "N_X", Ports: []mapfmt.Port{{ID: "P1"}}})
			post.Geometry.Edges["E1"] = straightAlign(50)
			post.Geometry.Edges["E1_CONT"] = straightAlign(50)

			got, err := splitRuns(post, post.Construction.Runs, "E1", "E1_CONT")
			if err != nil {
				t.Fatalf("splitRuns: %v", err)
			}
			if len(got) != 1 {
				t.Fatalf("run'ов %d, ожидался 1", len(got))
			}
			assertJSONEqual(t, tc.want, got[0].Spans, "спаны после реза")
			if got[0].Phase != 0.2 {
				t.Fatalf("фаза %v, ожидалась 0.2", got[0].Phase)
			}
			if preLen, postLen := runLen(pre.Construction.Runs[0]), runLen(got[0]); preLen != postLen {
				t.Fatalf("длина run'а: %v до, %v после", preLen, postLen)
			}

			// Кумулятивная длина run'а в каждой физической точке не изменилась.
			assertRunCoordPreserved(t, pre, post, pre.Construction.Runs[0], got[0])
		})
	}
}

func straightAlign(length float64) mapfmt.Alignments {
	return mapfmt.Alignments{Horizontal: []mapfmt.HPrim{{Kind: "straight", Length: length}}}
}

// assertRunCoordPreserved — кумулятивная длина run'а в каждой физической
// точке до и после реза одинакова. Точки опознаются по мировой оси карт:
// после реза голова и хвост — соседние рёбра той же цепочки.
func assertRunCoordPreserved(t *testing.T, preMap, postMap *mapfmt.Map, pre, post mapfmt.ConstructionRun) {
	t.Helper()
	preFrame := buildWorldFrame(t, preMap)
	postFrame := buildWorldFrame(t, postMap)

	L := runLen(pre)
	pitch := runPitch(preMap, pre)
	if pitch <= 0 {
		t.Fatalf("шаг шпал run'а не разрешился")
	}
	var gs []float64
	for g := 0.05; g < L; g += 0.5 {
		gs = append(gs, g)
	}
	for g := pre.Phase; g < L; g += pitch {
		gs = append(gs, g)
	}
	sort.Float64s(gs)
	if len(gs) == 0 {
		t.Fatal("нет точек выборки")
	}

	const tol = 1e-9
	for _, g := range gs {
		x := preFrame.runPoint(t, pre, g)
		gPost, ok := postFrame.runCoord(t, post, x)
		if !ok {
			t.Fatalf("физическая точка %v покрыта run'ом до реза, после — нет", x)
		}
		if math.Abs(gPost-g) > tol {
			t.Fatalf("физическая точка %v: кумулятивная длина %v до реза, %v после", x, g, gPost)
		}
	}
}
