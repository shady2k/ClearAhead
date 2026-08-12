package track

import (
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/netloc"
	"github.com/shady2k/ClearAhead/server/internal/seedmap"
	"github.com/shady2k/ClearAhead/server/internal/units"
)

// TestCompileFlatLengths — станция плоская, поэтому пространственная длина
// совпадает с плановой, а порядок элементов в проводе детерминирован.
func TestCompileFlatLengths(t *testing.T) {
	cn, rg, err := Compile(seedmap.Station())
	if err != nil {
		t.Fatalf("компиляция: %v", err)
	}
	// Подход — прямая 120 м.
	approach := cn.Elements[seedmap.StationApproach]
	if approach.LengthS != 120*units.Meter || approach.LengthU != 120*units.Meter {
		t.Fatalf("%s: u=%s s=%s, ожидалось по 120m", seedmap.StationApproach, approach.LengthU, approach.LengthS)
	}
	// Пять рёбер плюс по два прохода на каждую из двух стрелок.
	if len(rg.Elements) != 9 {
		t.Fatalf("в RenderGeometry %d элементов, ожидалось 9", len(rg.Elements))
	}
	if rg.Elements[0].ID != seedmap.StationApproach {
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
	m := seedmap.Line(seedmap.WithoutConstruction(), seedmap.Mutate(func(m *mapfmt.Map) {
		half := mapfmt.HPrim{Kind: "straight", Length: 0.0000005}
		m.Geometry.Edges[seedmap.LineEdgeID] = mapfmt.Alignments{
			Horizontal: []mapfmt.HPrim{half, half, half},
		}
	}))
	cn, _, err := Compile(valid(t, m))
	if err != nil {
		t.Fatalf("компиляция: %v", err)
	}
	if got := cn.Elements[seedmap.LineEdgeID].LengthU; got != 3*units.Micrometer {
		t.Fatalf("длина %d мкм, ожидалось 3: правило округления не сумма округлённых", int64(got))
	}
}

func TestCompileDeterministic(t *testing.T) {
	a1, b1, err := Compile(seedmap.Station())
	if err != nil {
		t.Fatalf("компиляция 1: %v", err)
	}
	a2, b2, err := Compile(seedmap.Station())
	if err != nil {
		t.Fatalf("компиляция 2: %v", err)
	}
	if a1.Elements[seedmap.StationApproach].LengthS != a2.Elements[seedmap.StationApproach].LengthS {
		t.Fatal("длина зависит от запуска")
	}
	if b1.Elements[0].Start != b2.Elements[0].Start {
		t.Fatal("стартовая поза зависит от запуска")
	}
}

// approachWithGrade — станция, у которой подход получил вертикальный профиль, и
// платформа TSP на нём.
//
// Профиль подхода начинается и кончается нулевым уклоном (замыкание с якорем и
// с проходами стрелки), а в середине уходит в 20‰: пространственная координата
// s расходится с u, поэтому тест видит, что спаны клиента взяты в u из карты, а
// симуляции — в s.
func approachWithGrade() *mapfmt.Map {
	return seedmap.Station(
		seedmap.WithStructure(platform("TSP", seedmap.StationApproach, 10, 90)),
		seedmap.Mutate(func(m *mapfmt.Map) {
			a := m.Geometry.Edges[seedmap.StationApproach]
			a.Vertical = []mapfmt.VPrim{
				{Kind: "grade", Length: 20, SlopePermille: 0},
				{Kind: "vertical_curve", Length: 60, EndSlopePermille: 20},
				{Kind: "grade", Length: 20, SlopePermille: 20},
				{Kind: "vertical_curve", Length: 20, EndSlopePermille: 0},
			}
			m.Geometry.Edges[seedmap.StationApproach] = a
		}),
	)
}

// платформа — сооружение на элементе от fromM до toM.
func platform(id, element string, fromM, toM float64) mapfmt.Structure {
	return mapfmt.Structure{
		ID:     id,
		Kind:   "platform",
		Span:   netloc.LinearU{{Element: element, From: fromM, To: toM}},
		Side:   "right",
		Offset: 1.745,
		Width:  3.0,
		// Вертикаль обязательна с редакции 6: платформа без высоты не рисуется
		// и отвергается валидатором. Отступ 1.745 — нормируемый для НИЗКОЙ
		// платформы, и высота 0.2 с ним согласована: высокая при таком отступе
		// нарушила бы габарит.
		Height:        0.2,
		SlabThickness: 0.35,
	}
}

func TestCompileRenderRole(t *testing.T) {
	_, rg, err := Compile(valid(t, approachWithGrade()))
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
		{seedmap.StationSW1 + mapfmt.PassageStraight, "straight"},
		{seedmap.StationSW1 + mapfmt.PassageDiverging, "diverging"},
	} {
		e := byID[tc.id]
		if e.Role == nil {
			t.Fatalf("%s: роль не назначена", tc.id)
		}
		if e.Role.Turnout != seedmap.StationSW1 || e.Role.Branch != tc.branch ||
			e.Role.Hand != "right" || e.Role.Frog != "1/9" {
			t.Fatalf("%s: роль %+v, ожидалась ветвь %s стрелки %s right 1/9",
				tc.id, e.Role, tc.branch, seedmap.StationSW1)
		}
	}
	for _, id := range []string{seedmap.StationApproach, seedmap.StationMain, seedmap.StationSiding} {
		if e := byID[id]; e.Role != nil {
			t.Fatalf("обычный путь %s получил роль %+v", id, e.Role)
		}
	}
}

// TestCompileStructureSpansInU — спаны клиента в координате u, ровно как в
// карте; симуляционный спан в s и на уклоне длиннее. Конвертировать обратно
// из s нельзя: для плоской станции они совпадают, в общем случае нет.
func TestCompileStructureSpansInU(t *testing.T) {
	cn, rg, err := Compile(valid(t, approachWithGrade()))
	if err != nil {
		t.Fatalf("компиляция: %v", err)
	}
	// Платформа фабрики на главном пути, три её упора (заведены 2026-08-12
	// вместе с вертикалью: до того затравка их не создавала вовсе) и
	// добавленная платформа на подходе.
	if len(rg.Structures) != 5 {
		t.Fatalf("в RenderGeometry %d сооружений, ожидалось 5", len(rg.Structures))
	}
	var st RenderStructure
	for _, cand := range rg.Structures {
		if cand.ID == "TSP" {
			st = cand
		}
	}
	if st.ID != "TSP" || st.Kind != "platform" || st.Side != "right" {
		t.Fatalf("объект %+v, ожидался TSP platform right", st)
	}
	if st.Offset != 1.745 || st.Width != 3.0 {
		t.Fatalf("размеры платформы (%v, %v) — ожидались (1.745, 3.0) из карты", st.Offset, st.Width)
	}
	// Вертикаль едет тем же путём и из той же карты: без неё платформа
	// рисуется полосой на отметке оси, а высота верха плиты — выдумкой клиента.
	if st.Height != 0.2 || st.SlabThickness != 0.35 {
		t.Fatalf("вертикаль платформы (%v, %v) — ожидались (0.2, 0.35) из карты", st.Height, st.SlabThickness)
	}
	if len(st.Spans) != 1 {
		t.Fatalf("у %s %d спанов, ожидался 1", st.ID, len(st.Spans))
	}
	sp := st.Spans[0]
	if sp.Element != seedmap.StationApproach || sp.From != 10.0 || sp.To != 90.0 {
		t.Fatalf("спан клиента (%s, %v, %v) — ожидались значения u из карты (%s, 10, 90)",
			sp.Element, sp.From, sp.To, seedmap.StationApproach)
	}
	ss := cn.Structures["TSP"]
	if len(ss) != 1 {
		t.Fatalf("у CompiledNetwork %d спанов, ожидался 1", len(ss))
	}
	// Начало в плоском участке: s == u. Конец на уклоне: s > u.
	if ss[0].From.Meters() != 10.0 || ss[0].To.Meters() <= 90.0 {
		t.Fatalf("симуляционный спан (%s, %v, %v) — начало в s==u, конец обязан превышать 90",
			ss[0].Element, ss[0].From.Meters(), ss[0].To.Meters())
	}
}

// TestCompileFrogOptional — марка крестовины необязательна и в карте, и в
// проводе (спека §7): роль ветви с опущенной маркой уходит клиенту, крестовина
// строится из особенности frog (§5), марка показывается подписью, если есть.
func TestCompileFrogOptional(t *testing.T) {
	m := seedmap.Station(seedmap.Mutate(func(m *mapfmt.Map) {
		for i := range m.Topology.Turnouts {
			m.Topology.Turnouts[i].Frog = ""
		}
	}))
	_, rg, err := Compile(m)
	if err != nil {
		t.Fatalf("стрелка без марки обязана компилироваться: %v", err)
	}
	for _, e := range rg.Elements {
		if e.Role == nil {
			continue
		}
		if e.Role.Frog != "" {
			t.Fatalf("%s: марка %q должна была остаться опущенной", e.ID, e.Role.Frog)
		}
	}
}

// TestCompileConstructionWire — типы и run'ы уезжают в провод: умолчание
// разрешено компилятором (в проводе у каждого run явный type), run'ы
// отсортированы по id, спаны — в авторском порядке, пустые массивы карты без
// блока — «[]», а не null.
func TestCompileConstructionWire(t *testing.T) {
	// Run'ы записаны в обратном порядке: сортировка в проводе обязана быть
	// свойством компилятора, а не удачей авторской записи.
	m := seedmap.Station(seedmap.Mutate(func(m *mapfmt.Map) {
		runs := m.Construction.Runs
		for i, j := 0, len(runs)-1; i < j; i, j = i+1, j-1 {
			runs[i], runs[j] = runs[j], runs[i]
		}
	}))
	if m.Construction.Runs[0].ID != "RUN_STUB" {
		t.Fatalf("порядок run'ов в карте не перевёрнут: первый %s — проверять сортировку не на чем",
			m.Construction.Runs[0].ID)
	}
	_, rg, err := Compile(valid(t, m))
	if err != nil {
		t.Fatalf("компиляция: %v", err)
	}
	if len(rg.TrackTypes) != 1 || rg.TrackTypes[0].ID != seedmap.TrackTypeID {
		t.Fatalf("типы в проводе %+v", rg.TrackTypes)
	}
	tt := rg.TrackTypes[0]
	if tt.Gauge != 1.520 || tt.Sleeper.Pitch != 0.543 || tt.Sleeper.Length != 2.75 ||
		tt.Sleeper.Width != 0.28 || tt.Ballast.HalfWidth != 1.75 {
		t.Fatalf("тип в проводе %+v, ожидались числа из карты", tt)
	}
	if rg.PlacementAlgorithm != PlacementAlgorithm {
		t.Fatalf("placement_algorithm %q, ожидалось %q", rg.PlacementAlgorithm, PlacementAlgorithm)
	}
	if len(rg.ConstructionRuns) != 4 {
		t.Fatalf("run'ов в проводе %d, ожидалось 4", len(rg.ConstructionRuns))
	}
	// Сортировка по id, несмотря на авторский порядок в карте.
	if rg.ConstructionRuns[0].ID != "RUN_APPROACH_CROSS" || rg.ConstructionRuns[3].ID != "RUN_STUB" {
		t.Fatalf("run'ы не отсортированы по id: %s, %s, %s, %s", rg.ConstructionRuns[0].ID,
			rg.ConstructionRuns[1].ID, rg.ConstructionRuns[2].ID, rg.ConstructionRuns[3].ID)
	}
	for _, r := range rg.ConstructionRuns {
		if r.Type != seedmap.TrackTypeID {
			t.Fatalf("run %s: type %q в проводе неявный", r.ID, r.Type)
		}
		if r.Coordinate != "u" || len(r.Spans) == 0 {
			t.Fatalf("run %s: %+v", r.ID, r)
		}
		for _, s := range r.Spans {
			if s.Direction != "forward" {
				t.Fatalf("run %s: спан %+v без направления", r.ID, s)
			}
		}
	}
	// Спаны — в авторском порядке прохождения, а не отсортированы: run по
	// горловине идёт подходом, затем съездом.
	cross := rg.ConstructionRuns[0]
	if len(cross.Spans) != 2 || cross.Spans[0].Element != seedmap.StationApproach ||
		cross.Spans[1].Element != seedmap.StationCross {
		t.Fatalf("спаны %s не в авторском порядке: %+v", cross.ID, cross.Spans)
	}
	// Карта без блока: массивы пустые, но не null (форма контракта).
	_, rg2, err := Compile(seedmap.Line(seedmap.WithoutConstruction()))
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
