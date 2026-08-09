package solver_test

import (
	"math"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/geom"
	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/solver"
)

// Эталоны посчитаны из геометрии окружности вручную, а не той функцией,
// которую они проверяют. Достижимое множество v1 — передняя полуплоскость
// начальной позы без «линзы» из двух кругов радиуса MinRadiusM (180 м),
// касательных в начале; дуга меньше MinRadiusM не возвращается ни в каком
// статусе.
//
// Допуск по положению — микрометр (разрешение домена), по направлению — 1e-9:
// эталоны здесь замкнутые (хорда дуги обязана лечь ровно в цель).

const (
	posTolM = 1e-6
	headTol = 1e-9
	minRadM = 180.0 // mapfmt.DefaultProfile().MinRadiusM
)

// arcTheta — угол дуги радиуса 425 м через (200, 50) из (0, 0) курсом на восток:
// 2·atan2(50, 200) ≈ 0.4899573262537283 рад. Эталон посчитан вручную.
var arcTheta = 2 * math.Atan2(50, 200)

type tc struct {
	name     string
	start    geom.Pose
	target   geom.Pose
	intent   solver.Intent
	want     solver.Status
	wantCode string     // ожидаемый код нарушения на adjusted/infeasible
	wantLen  int        // ожидаемое число примитивов
	wantTerm geom.Pose  // ожидаемая конечная поза (на feasible)
	wantNear *geom.Pose // ожидаемая ближайшая допустимая точка (на infeasible)
}

var table = []tc{
	// Достижимая точка.
	{
		name:  "прямая вперёд по курсу",
		start: geom.Pose{X: 0, Y: 0}, target: geom.Pose{X: 100, Y: 0},
		intent: solver.IntentExtend, want: solver.StatusFeasible, wantLen: 1,
		wantTerm: geom.Pose{X: 100, Y: 0},
	},
	{
		name:  "прямая с ненулевым курсом",
		start: geom.Pose{X: 10, Y: 20, Heading: math.Pi / 2}, target: geom.Pose{X: 10, Y: 120},
		intent: solver.IntentExtend, want: solver.StatusFeasible, wantLen: 1,
		wantTerm: geom.Pose{X: 10, Y: 120, Heading: math.Pi / 2},
	},
	{
		name:  "одна дуга влево через точку",
		start: geom.Pose{X: 0, Y: 0}, target: geom.Pose{X: 200, Y: 50},
		intent: solver.IntentExtend, want: solver.StatusFeasible, wantLen: 1,
		wantTerm: geom.Pose{X: 200, Y: 50, Heading: arcTheta},
	},
	{
		name:  "одна дуга вправо через точку",
		start: geom.Pose{X: 0, Y: 0}, target: geom.Pose{X: 200, Y: -50},
		intent: solver.IntentExtend, want: solver.StatusFeasible, wantLen: 1,
		wantTerm: geom.Pose{X: 200, Y: -50, Heading: -arcTheta},
	},
	{
		name:  "перпендикуляр на расстоянии 2Rmin: полукруг",
		start: geom.Pose{X: 0, Y: 0}, target: geom.Pose{X: 0, Y: 400},
		intent: solver.IntentExtend, want: solver.StatusFeasible, wantLen: 1,
		wantTerm: geom.Pose{X: 0, Y: 400, Heading: math.Pi},
	},
	{
		name:  "почти на луче: боковое отклонение в допуске",
		start: geom.Pose{X: 0, Y: 0}, target: geom.Pose{X: 100, Y: 1e-7},
		intent: solver.IntentExtend, want: solver.StatusFeasible, wantLen: 1,
		wantTerm: geom.Pose{X: 100, Y: 0},
	},
	// Точка, требующая радиуса меньше минимального: жёсткая граница нормы.
	// Тихая подгонка запрещена — adjusted с причиной и трассой до границы линзы.
	{
		name:  "линза в стороне: adjusted до ближайшей допустимой",
		start: geom.Pose{X: 0, Y: 0}, target: geom.Pose{X: 100, Y: 50},
		intent: solver.IntentExtend, want: solver.StatusAdjusted, wantLen: 1,
		wantCode: solver.CodeRadiusBelowMinimum,
	},
	{
		name:  "линза у начала сбоку: adjusted до границы",
		start: geom.Pose{X: 0, Y: 0}, target: geom.Pose{X: 10, Y: 0.5},
		intent: solver.IntentExtend, want: solver.StatusAdjusted, wantLen: 1,
		wantCode: solver.CodeRadiusBelowMinimum,
	},
	// Точка позади начальной позы.
	{
		name:  "позади по курсу",
		start: geom.Pose{X: 0, Y: 0}, target: geom.Pose{X: -100, Y: 0},
		intent: solver.IntentExtend, want: solver.StatusInfeasible, wantLen: 0,
		wantCode: solver.CodeTargetBehind, wantNear: &geom.Pose{X: 0, Y: 0},
	},
	{
		name:  "позади при ненулевом курсе",
		start: geom.Pose{X: 0, Y: 0, Heading: math.Pi / 2}, target: geom.Pose{X: 0, Y: -100},
		intent: solver.IntentExtend, want: solver.StatusInfeasible, wantLen: 0,
		wantCode: solver.CodeTargetBehind, wantNear: &geom.Pose{X: 0, Y: 0, Heading: math.Pi / 2},
	},
	// Точка, требующая обратной кривой (S-образной трассы) — вне семейства v1.
	{
		name:  "позади и в стороне: обратная кривая",
		start: geom.Pose{X: 0, Y: 0}, target: geom.Pose{X: -100, Y: 30},
		intent: solver.IntentExtend, want: solver.StatusInfeasible, wantLen: 0,
		wantCode: solver.CodeReverseCurve, wantNear: &geom.Pose{X: 0, Y: 0},
	},
	{
		name:  "позади и далеко в стороне: граница — перпендикуляр",
		start: geom.Pose{X: 0, Y: 0}, target: geom.Pose{X: -100, Y: 400},
		intent: solver.IntentExtend, want: solver.StatusInfeasible, wantLen: 0,
		wantCode: solver.CodeReverseCurve, wantNear: &geom.Pose{X: 0, Y: 400},
	},
	// Вырожденные и невалидные входы — отказ, а не паника.
	{
		name:  "цель совпадает с началом",
		start: geom.Pose{X: 0, Y: 0}, target: geom.Pose{X: 0, Y: 0},
		intent: solver.IntentExtend, want: solver.StatusInfeasible, wantLen: 0,
		wantCode: solver.CodeZeroLength, wantNear: &geom.Pose{X: 0, Y: 0},
	},
	{
		name:  "неизвестное намерение",
		start: geom.Pose{X: 0, Y: 0}, target: geom.Pose{X: 100, Y: 0},
		intent: solver.Intent(42), want: solver.StatusInfeasible, wantLen: 0,
		wantCode: solver.CodeUnknownIntent,
	},
	// Намерения: геометрия общая, разница — в предупреждениях.
	{
		name:  "connect: геометрия общая, предупреждение о касательной",
		start: geom.Pose{X: 0, Y: 0}, target: geom.Pose{X: 100, Y: 0},
		intent: solver.IntentConnect, want: solver.StatusFeasible, wantLen: 1,
		wantTerm: geom.Pose{X: 100, Y: 0},
	},
	{
		name:  "terminate: геометрия общая, предупреждение об упоре",
		start: geom.Pose{X: 0, Y: 0}, target: geom.Pose{X: 100, Y: 0},
		intent: solver.IntentTerminate, want: solver.StatusFeasible, wantLen: 1,
		wantTerm: geom.Pose{X: 100, Y: 0},
	},
}

func TestSolveTable(t *testing.T) {
	profile := mapfmt.DefaultProfile()
	for _, c := range table {
		t.Run(c.name, func(t *testing.T) {
			r := solver.Solve(c.start, c.target, c.intent, profile, nil)

			if r.Status != c.want {
				t.Fatalf("статус: got %q, want %q (violations: %v)", r.Status, c.want, r.Violations)
			}
			if len(r.Alignment) != c.wantLen {
				t.Fatalf("число примитивов: got %d, want %d", len(r.Alignment), c.wantLen)
			}

			switch c.want {
			case solver.StatusFeasible:
				if r.NearestFeasible != nil {
					t.Errorf("feasible: NearestFeasible должен быть nil, got %v", *r.NearestFeasible)
				}
				if len(r.Violations) != 0 {
					t.Errorf("feasible: violations должны быть пусты, got %v", r.Violations)
				}
				assertPose(t, "конечная поза", r.Terminal, c.wantTerm)
			case solver.StatusAdjusted:
				// Причина обязана быть, и трасса обязана НЕ дойти до курсора.
				if len(r.Violations) == 0 || r.Violations[0].Code != c.wantCode {
					t.Errorf("adjusted: код нарушения: got %v, want %s", r.Violations, c.wantCode)
				}
				if r.NearestFeasible == nil {
					t.Fatal("adjusted: NearestFeasible не заполнен")
				}
				if distM(r.Terminal, c.target) < 0.1 {
					t.Errorf("adjusted: трасса дошла до курсора (%.6f м) — подгонка, а не отказ",
						distM(r.Terminal, c.target))
				}
				// Контракт: трасса построена до ближайшей допустимой точки.
				assertPose(t, "ближайшая допустимая точка", *r.NearestFeasible, r.Terminal)
			case solver.StatusInfeasible:
				// Приёмка: на каждом infeasible есть и причина, и ближайшая точка.
				if len(r.Violations) == 0 {
					t.Error("infeasible без причины")
				}
				if r.Violations[0].Code != c.wantCode {
					t.Errorf("infeasible: код нарушения: got %v, want %s", r.Violations, c.wantCode)
				}
				if r.NearestFeasible == nil {
					t.Error("infeasible без ближайшей допустимой точки")
				}
				if c.wantNear != nil {
					assertPose(t, "ближайшая допустимая точка", *r.NearestFeasible, *c.wantNear)
				}
			}

			// Жёсткая граница: ни одна дуга не меньше MinRadiusM.
			for i, p := range r.Alignment {
				if p.Kind == geom.KindArc && p.Radius < minRadM {
					t.Errorf("примитив %d: радиус %.6f м меньше минимального %.1f м", i, p.Radius, minRadM)
				}
			}
		})
	}
}

// TestAdjustedLandsOnLensBoundary — adjusted обязан лечь на границу линзы:
// окружность радиуса MinRadiusM, касательную в начале, — и иметь радиус ровно
// MinRadiusM (не меньше и не «подогнанный» под курсор).
func TestAdjustedLandsOnLensBoundary(t *testing.T) {
	profile := mapfmt.DefaultProfile()
	cases := []struct {
		name   string
		target geom.Pose
		side   int // знак бокового отклонения: +1 влево, -1 вправо
	}{
		{"влево от курса", geom.Pose{X: 100, Y: 50}, +1},
		{"вправо от курса", geom.Pose{X: 100, Y: -50}, -1},
		{"у начала сбоку", geom.Pose{X: 10, Y: 0.5}, +1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := solver.Solve(geom.Pose{}, c.target, solver.IntentExtend, profile, nil)
			if r.Status != solver.StatusAdjusted {
				t.Fatalf("статус: got %q, want adjusted", r.Status)
			}
			if len(r.Alignment) != 1 || r.Alignment[0].Kind != geom.KindArc {
				t.Fatalf("ожидалась одна дуга, got %v", r.Alignment)
			}
			arc := r.Alignment[0]
			if arc.Radius != minRadM {
				t.Errorf("радиус: got %.6f, want ровно %.1f", arc.Radius, minRadM)
			}
			// Конец дуги — на граничной окружности: центр в начале + Rmin·нормаль.
			cx := 0.0
			cy := float64(c.side) * minRadM
			if got := distM(r.Terminal, geom.Pose{X: cx, Y: cy}); math.Abs(got-minRadM) > posTolM {
				t.Errorf("конец дуги вне границы линзы: расстояние до центра %.6f, want %.1f", got, minRadM)
			}
			// Вращение — в сторону цели.
			if (arc.Angle > 0) != (float64(c.side) > 0) {
				t.Errorf("знак угла дуги %v не соответствует стороне цели %+d", arc.Angle, c.side)
			}
		})
	}
}

// TestLensProjectionIsNearest — ближайшая допустимая точка к цели в линзе — это
// проекция цели на граничную окружность: она ближе к цели, чем начало, и на
// границе нет ни одной допустимой точки между ней и целью.
func TestLensProjectionIsNearest(t *testing.T) {
	profile := mapfmt.DefaultProfile()
	target := geom.Pose{X: 100, Y: 50}
	r := solver.Solve(geom.Pose{}, target, solver.IntentExtend, profile, nil)
	if r.Status != solver.StatusAdjusted || r.NearestFeasible == nil {
		t.Fatalf("ожидался adjusted с ближайшей точкой, got %v", r.Status)
	}
	center := geom.Pose{X: 0, Y: minRadM}
	// Цель лежит ВНУТРИ граничной окружности (|target−center| < Rmin), поэтому
	// проекция на окружность лежит на луче центр→цель ЗА целью: цель между
	// центром и проекцией, все три коллинеарны.
	d1 := distM(target, *r.NearestFeasible)
	d2 := distM(*r.NearestFeasible, center)
	d3 := distM(target, center)
	if math.Abs(d3+d1-d2) > posTolM {
		t.Errorf("проекция не на луче центр→цель: %f + %f != %f", d3, d1, d2)
	}
	if !(d3 < d2) {
		t.Errorf("цель не между центром и проекцией: %f >= %f", d3, d2)
	}
	// Проекция — на границе линзы: ровно Rmin от центра.
	if got := distM(*r.NearestFeasible, center); math.Abs(got-minRadM) > posTolM {
		t.Errorf("проекция вне границы: %f, want %f", got, minRadM)
	}
	// Ближайшая точка строго ближе к цели, чем начало.
	if d1 >= distM(target, geom.Pose{}) {
		t.Errorf("ближайшая точка не ближе начала: %f >= %f", d1, distM(target, geom.Pose{}))
	}
}

func TestInfeasibleAlwaysCarriesReasonAndNearestPoint(t *testing.T) {
	profile := mapfmt.DefaultProfile()
	for _, c := range table {
		if c.want != solver.StatusInfeasible {
			continue
		}
		t.Run(c.name, func(t *testing.T) {
			r := solver.Solve(c.start, c.target, c.intent, profile, nil)
			if len(r.Violations) == 0 {
				t.Error("infeasible без причины")
			}
			if r.NearestFeasible == nil {
				t.Error("infeasible без ближайшей допустимой точки")
			}
			if len(r.Alignment) != 0 {
				t.Errorf("infeasible с трассой из %d примитивов", len(r.Alignment))
			}
			if r.DeviceProposals == nil {
				t.Error("DeviceProposals: сериализация даст null, ожидается []")
			}
			if len(r.DeviceProposals) != 0 {
				t.Errorf("DeviceProposals: got %v, ожидается пусто", r.DeviceProposals)
			}
		})
	}
}

func TestIntentWarningsAndProposals(t *testing.T) {
	profile := mapfmt.DefaultProfile()
	extend := solver.Solve(geom.Pose{}, geom.Pose{X: 100}, solver.IntentExtend, profile, nil)
	connect := solver.Solve(geom.Pose{}, geom.Pose{X: 100}, solver.IntentConnect, profile, nil)
	terminate := solver.Solve(geom.Pose{}, geom.Pose{X: 100}, solver.IntentTerminate, profile, nil)

	if len(extend.Warnings) != 0 {
		t.Errorf("extend: warnings должны быть пусты, got %v", extend.Warnings)
	}
	if len(connect.Warnings) == 0 {
		t.Error("connect: ожидалось предупреждение о касательной")
	}
	if len(terminate.Warnings) == 0 {
		t.Error("terminate: ожидалось предупреждение об упоре")
	}
	for _, r := range []solver.Result{extend, connect, terminate} {
		if len(r.DeviceProposals) != 0 {
			t.Errorf("v1: предложения устройств должны быть пусты, got %v", r.DeviceProposals)
		}
	}
}

func TestInvalidProfile(t *testing.T) {
	cases := []mapfmt.Profile{
		{},
		{Version: 1, MinRadiusM: -10},
		{Version: 1, MinRadiusM: math.NaN()},
	}
	for _, p := range cases {
		r := solver.Solve(geom.Pose{}, geom.Pose{X: 100}, solver.IntentExtend, p, nil)
		if r.Status != solver.StatusInfeasible {
			t.Fatalf("профиль %+v: статус %q, want infeasible", p, r.Status)
		}
		if len(r.Violations) == 0 || r.Violations[0].Code != solver.CodeInvalidProfile {
			t.Errorf("профиль %+v: код нарушения %v, want %s", p, r.Violations, solver.CodeInvalidProfile)
		}
	}
}

// TestChainEndsAtTargetForFeasible — на feasible трасса обязана закончиться ровно
// в точке под курсором: это контракт «достижимая точка».
func TestChainEndsAtTargetForFeasible(t *testing.T) {
	profile := mapfmt.DefaultProfile()
	feasible := []geom.Pose{
		{X: 100, Y: 0},
		{X: 200, Y: 50},
		{X: 200, Y: -50},
		{X: 0, Y: 400},
	}
	for _, target := range feasible {
		r := solver.Solve(geom.Pose{}, target, solver.IntentExtend, profile, nil)
		if r.Status != solver.StatusFeasible {
			t.Fatalf("цель %+v: статус %q, want feasible", target, r.Status)
		}
		if got := distM(r.Terminal, target); got > posTolM {
			t.Errorf("цель %+v: конец трассы в %f м от цели", target, got)
		}
	}
}

func distM(a, b geom.Pose) float64 {
	return math.Hypot(a.X-b.X, a.Y-b.Y)
}

func assertPose(t *testing.T, name string, got, want geom.Pose) {
	t.Helper()
	if d := distM(got, want); d > posTolM {
		t.Errorf("%s: положение off by %.6f м (got %+v, want %+v)", name, d, got, want)
	}
	if dh := math.Abs(got.Heading - want.Heading); dh > headTol {
		t.Errorf("%s: курс off by %.9f рад (got %v, want %v)", name, dh, got.Heading, want.Heading)
	}
}

// Чистая функция остаётся тотальной: NaN/Inf в аргументах — infeasible с
// причиной, а не паника в геометрии.
func TestNonFiniteInputs(t *testing.T) {
	profile := mapfmt.DefaultProfile()
	cases := []struct {
		name   string
		start  geom.Pose
		target geom.Pose
	}{
		{"NaN в цели", geom.Pose{}, geom.Pose{X: math.NaN()}},
		{"Inf в цели", geom.Pose{}, geom.Pose{X: math.Inf(1)}},
		{"NaN в начале", geom.Pose{X: math.NaN()}, geom.Pose{X: 100}},
		{"Inf в курсе начала", geom.Pose{Heading: math.Inf(1)}, geom.Pose{X: 100}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := solver.Solve(c.start, c.target, solver.IntentExtend, profile, nil)
			if r.Status != solver.StatusInfeasible {
				t.Fatalf("статус: got %q, want infeasible", r.Status)
			}
			if len(r.Violations) == 0 || r.Violations[0].Code != solver.CodeInvalidTarget {
				t.Errorf("код нарушения: got %v, want %s", r.Violations, solver.CodeInvalidTarget)
			}
			if r.NearestFeasible == nil {
				t.Error("infeasible без ближайшей допустимой точки")
			}
			if !allFinitePose(*r.NearestFeasible) {
				t.Errorf("ближайшая точка не конечна: %+v", *r.NearestFeasible)
			}
		})
	}
	// Заголовок цели контрактом игнорируется: NaN там не повод для отказа.
	r := solver.Solve(geom.Pose{}, geom.Pose{X: 100, Heading: math.NaN()}, solver.IntentExtend, profile, nil)
	if r.Status != solver.StatusFeasible {
		t.Errorf("NaN в заголовке цели: статус %q, want feasible", r.Status)
	}
}

func allFinitePose(p geom.Pose) bool {
	for _, v := range []float64{p.X, p.Y, p.Heading} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
	}
	return true
}

// Цель ровно в центре граничной окружности: проекция не определена, решатель
// обязан вернуть детерминированную точку границы, а не упасть на 0/0.
func TestTargetAtLensCenter(t *testing.T) {
	profile := mapfmt.DefaultProfile()
	cases := []struct {
		name   string
		target geom.Pose
		side   int
	}{
		{"центр левой окружности", geom.Pose{X: 0, Y: minRadM}, +1},
		{"центр правой окружности", geom.Pose{X: 0, Y: -minRadM}, -1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := solver.Solve(geom.Pose{}, c.target, solver.IntentExtend, profile, nil)
			if r.Status != solver.StatusAdjusted {
				t.Fatalf("статус: got %q, want adjusted (violations: %v)", r.Status, r.Violations)
			}
			if len(r.Alignment) != 1 || r.Alignment[0].Kind != geom.KindArc {
				t.Fatalf("ожидалась одна дуга, got %v", r.Alignment)
			}
			if r.Alignment[0].Radius != minRadM {
				t.Errorf("радиус: got %.6f, want ровно %.1f", r.Alignment[0].Radius, minRadM)
			}
			if r.NearestFeasible == nil {
				t.Fatal("adjusted без ближайшей допустимой точки")
			}
			// Точка границы детерминирована: вперёд по курсу, на окружности —
			// центр (0, side·Rmin), самая передняя точка (Rmin, side·Rmin).
			want := geom.Pose{X: minRadM, Y: float64(c.side) * minRadM}
			if got := distM(*r.NearestFeasible, want); got > posTolM {
				t.Errorf("ближайшая точка %+v, want %+v (off by %.6f м)", *r.NearestFeasible, want, got)
			}
			if !allFinitePose(r.Terminal) {
				t.Errorf("конечная поза не конечна: %+v", r.Terminal)
			}
		})
	}
}
