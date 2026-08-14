package norms

import (
	"strings"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/physics"
	"github.com/shady2k/ClearAhead/server/internal/units"
)

// mustSpeed собирает расчётную скорость из километров в час; отказ — ошибка
// теста, а не проверяемое поведение пакета.
func mustSpeed(t *testing.T, kmh float64) units.Speed {
	t.Helper()
	v, err := units.KmhToSpeed(kmh)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// mainLineII — перегонный главный путь II категории: базовая строка, на
// которой строится большинство проверок.
func mainLineII(t *testing.T) Context {
	t.Helper()
	return Context{
		Category:   CategoryII,
		Purpose:    PurposeMainLine,
		Speed:      mustSpeed(t, 80),
		Conditions: ConditionsNormal,
	}
}

// TestEveryOutcomeAppears — каждый из пяти исходов матрицы встречается хотя бы
// раз: исходы не сливаются, AllowedWith не вырождается в Allowed, а
// UnknownContext не прячется за пределами ближайшей строки.
func TestEveryOutcomeAppears(t *testing.T) {
	ctx := mainLineII(t)
	cases := []struct {
		name string
		got  Outcome
	}{
		{"allowed", Gradient(ctx, 14000, 0).Outcome},
		{"allowedWith", Embankment(ctx, 8*units.Meter).Outcome},
		{"requiresIndividualDesign", Radius(ctx, 200*units.Meter).Outcome},
		{"forbidden", Gradient(ctx, 34000, 0).Outcome},
		{"unknownContext", Radius(Context{
			Category:   CategoryII,
			Purpose:    "депо",
			Speed:      mustSpeed(t, 25),
			Conditions: ConditionsNormal,
		}, 500*units.Meter).Outcome},
	}
	seen := map[Outcome]bool{}
	for _, c := range cases {
		if seen[c.got] {
			continue
		}
		seen[c.got] = true
	}
	for _, o := range []Outcome{Allowed, AllowedWith, RequiresIndividualDesign, Forbidden, UnknownContext} {
		if !seen[o] {
			t.Errorf("исход %v не встретился ни в одном ответе матрицы", o)
		}
	}
}

// TestUnknownContextRefusesInsteadOfNearestRow — сочетания, которого в матрице
// нет (депо, промплощадка, станционный путь), обязаны получить отказ, а не
// предел похожей строки: норму для них никто не прочитал (§2.1.6, решение
// координатора).
func TestUnknownContextRefusesInsteadOfNearestRow(t *testing.T) {
	ctx := Context{
		Category:   CategoryII,
		Purpose:    "депо",
		Speed:      mustSpeed(t, 25),
		Conditions: ConditionsNormal,
	}
	got := Gradient(ctx, 1000, 0)
	if got.Outcome != UnknownContext {
		t.Fatalf("депо: исход %v, ожидался UnknownContext, а не предел II категории", got.Outcome)
	}
	if got.Reason == "" {
		t.Fatal("отказ UnknownContext обязан нести текст")
	}
	// предел ближайшей строки (II категория, 15 ‰) не должен просочиться в отказ.
	if strings.Contains(got.Reason, "15") {
		t.Errorf("отказ подставил предел соседней строки: %q", got.Reason)
	}
}

// TestRadiusOutsideSeriesRejected — радиус не член ряда, а число из диапазона:
// 450 м лежит между 400 и 500, но в ряд не входит и отвергается (СП 119.13330,
// п. 4.4). Ниже ряда (170 м) — тоже не член.
func TestRadiusOutsideSeriesRejected(t *testing.T) {
	ctx := mainLineII(t)
	for _, r := range []units.Distance{450 * units.Meter, 460 * units.Meter, 170 * units.Meter} {
		if got := Radius(ctx, r); got.Outcome != Forbidden {
			t.Errorf("радиус %s: исход %v, ожидался Forbidden", r, got.Outcome)
		}
	}
}

// TestRadiusMemberBelow300RequiresIndividualDesign — члены ряда 250, 200 и 180 м
// законны как ЧЛЕНЫ ряда, но меньше 300 м и потому допускаются только при
// технико-экономическом обосновании, которого в игре нет: исход
// RequiresIndividualDesign, а НЕ Forbidden — ряд их знает (§2.1.4, решение
// координатора). Граница 300 м и члены выше неё разрешены.
func TestRadiusMemberBelow300RequiresIndividualDesign(t *testing.T) {
	ctx := mainLineII(t)
	for _, r := range []units.Distance{250 * units.Meter, 200 * units.Meter, 180 * units.Meter} {
		got := Radius(ctx, r)
		if got.Outcome != RequiresIndividualDesign {
			t.Errorf("радиус %s — член ряда: исход %v, ожидался RequiresIndividualDesign", r, got.Outcome)
		}
		if got.Reason == "" {
			t.Errorf("радиус %s: отказ обязан нести текст", r)
		}
	}
	for _, r := range []units.Distance{300 * units.Meter, 500 * units.Meter, 4000 * units.Meter} {
		if got := Radius(ctx, r); got.Outcome != Allowed {
			t.Errorf("радиус %s: исход %v, ожидался Allowed", r, got.Outcome)
		}
	}
}

// TestCurveCorrectionMatchesCurveResistance — предел уклона в кривой уменьшается
// на величину, эквивалентную дополнительному сопротивлению от кривой, и эта
// величина берётся ВЫЗОВОМ physics.CurveResistance, а не переписанным числом:
// тест сравнивает с самой функцией (§2.1.3). На прямой поправки нет.
func TestCurveCorrectionMatchesCurveResistance(t *testing.T) {
	ctx := mainLineII(t)
	const base = int64(15000) // II категория: 15,0 ‰
	radius := 350 * units.Meter
	limit := base - int64(physics.CurveResistance(radius))

	if got := Gradient(ctx, base, 0); got.Outcome != Allowed {
		t.Fatalf("на прямой руководящий 15,0 ‰ должен проходить, получено %v", got.Outcome)
	}
	if got := Gradient(ctx, base, radius); got.Outcome != Forbidden {
		t.Fatalf("в кривой R=350 руководящий 15,0 ‰ обязан отказать (предел %d), получено %v", limit, got.Outcome)
	}
	if got := Gradient(ctx, limit, radius); got.Outcome != Allowed {
		t.Fatalf("в кривой R=350 предел %d обязан проходить, получено %v", limit, got.Outcome)
	}
	if got := Gradient(ctx, limit+1, radius); got.Outcome != Forbidden {
		t.Fatalf("в кривой R=350 предел+1 обязан отказать, получено %v", got.Outcome)
	}
	// на прямой предел — руководящий уклон без поправки.
	if got := Gradient(ctx, limit, 0); got.Outcome != Allowed {
		t.Fatalf("на прямой 13,0 ‰ при руководящем 15,0 ‰ обязан проходить, получено %v", got.Outcome)
	}
}

// TestRefusalNamesMagnitudeAndCategory — отказ называет и величину, и категорию:
// без категории игрок не поймёт, почему тот же уклон вчера прошёл (бриф W1-B).
func TestRefusalNamesMagnitudeAndCategory(t *testing.T) {
	ctx := mainLineII(t)
	got := Gradient(ctx, 34000, 0)
	if got.Outcome != Forbidden {
		t.Fatalf("уклон 34,0 ‰ на II категории: исход %v, ожидался Forbidden", got.Outcome)
	}
	for _, want := range []string{"34", "линии II категории"} {
		if !strings.Contains(got.Reason, want) {
			t.Errorf("отказ %q не содержит %q", got.Reason, want)
		}
	}

	access := Context{
		Category:   CategoryIV,
		Purpose:    PurposeAccess,
		Speed:      mustSpeed(t, 40),
		Conditions: ConditionsNormal,
	}
	if got := Gradient(access, 35000, 0); !strings.Contains(got.Reason, "подъездного пути IV категории") {
		t.Errorf("отказ для подъездного пути не называет назначение: %q", got.Reason)
	}
}

// TestHardConditionsBranchAbsent — ветвь «до 40 ‰ в трудных условиях» не
// реализована: 35 ‰ на подъездном пути IV категории не проходит ни в
// нормальных условиях (предел IV — 30 ‰), ни в трудных (исход
// RequiresIndividualDesign, а не подстановка мягкой строки).
func TestHardConditionsBranchAbsent(t *testing.T) {
	access := Context{
		Category:   CategoryIV,
		Purpose:    PurposeAccess,
		Speed:      mustSpeed(t, 40),
		Conditions: ConditionsNormal,
	}
	if got := Gradient(access, 35000, 0); got.Outcome != Forbidden {
		t.Fatalf("35 ‰ на подъездном IV в нормальных условиях: исход %v, ожидался Forbidden", got.Outcome)
	}
	if got := Gradient(access, 30000, 0); got.Outcome != Allowed {
		t.Fatalf("30 ‰ на подъездном IV в нормальных условиях: исход %v, ожидался Allowed", got.Outcome)
	}

	difficult := access
	difficult.Conditions = ConditionsDifficult
	if got := Gradient(difficult, 35000, 0); got.Outcome != RequiresIndividualDesign {
		t.Fatalf("трудные условия: исход %v, ожидался RequiresIndividualDesign (не Allowed и не UnknownContext)", got.Outcome)
	}
	if got := Gradient(difficult, 35000, 0); got.Reason == "" {
		t.Fatal("отказ трудных условий обязан нести текст")
	}
}

// TestSpeedStaysInKey — скорость — измерение ключа, но все строки матрицы
// объявляют её безразличной (СТН не связывает предел со скоростью; «независимо
// от грузонапряжённости» — та же форма). Различие «в ключе, но безразлична» и
// «вне ключа» проверяемо: ссылки на поле Speed в этом тесте не дают убрать
// измерение из ключа — удалили поле, тест не компилируется (решение
// координатора, ОТВЕТ 2).
func TestSpeedStaysInKey(t *testing.T) {
	slow := mainLineII(t)
	fast := slow
	fast.Speed = mustSpeed(t, 160)
	if slow.Speed == fast.Speed {
		t.Fatal("тест не различает скорости — страховка не работает")
	}
	gotSlow, gotFast := Gradient(slow, 10000, 0), Gradient(fast, 10000, 0)
	if gotSlow != gotFast {
		t.Errorf("скорость изменила исход: %v против %v", gotSlow, gotFast)
	}
}

// TestUnknownConditionsValueIsUnknownContext — значение условий, которого норма
// не знает, — UnknownContext, а не подбор нормальной строки.
func TestUnknownConditionsValueIsUnknownContext(t *testing.T) {
	ctx := mainLineII(t)
	ctx.Conditions = "болотистые"
	if got := Gradient(ctx, 10000, 0); got.Outcome != UnknownContext {
		t.Fatalf("непрочитанные условия: исход %v, ожидался UnknownContext", got.Outcome)
	}
}

// TestEmbankmentHeightBands — насыпь нормируется порогом: низкая (до 1 м) и
// средняя (1–6 м) принимаются без меры; высокая (6–12 м) — только с бермами
// либо уменьшением крутизны откоса; особо высокая (свыше 12 м) — индивидуальное
// проектирование, которого в игре нет (СП 32-104-98, СП 119.13330; §2.1.5).
func TestEmbankmentHeightBands(t *testing.T) {
	ctx := mainLineII(t)
	for _, h := range []units.Distance{1 * units.Meter, 6 * units.Meter} {
		if got := Embankment(ctx, h); got.Outcome != Allowed {
			t.Errorf("насыпь %s: исход %v, ожидался Allowed", h, got.Outcome)
		}
	}
	got := Embankment(ctx, 8*units.Meter)
	if got.Outcome != AllowedWith {
		t.Fatalf("насыпь 8 м: исход %v, ожидался AllowedWith(мера)", got.Outcome)
	}
	if got.Measure == "" {
		t.Fatal("AllowedWith обязан назвать меру (бермы либо уменьшение крутизны откоса)")
	}
	if got := Embankment(ctx, 12*units.Meter); got.Outcome != AllowedWith {
		t.Errorf("насыпь 12 м: исход %v, ожидался AllowedWith", got.Outcome)
	}
	got = Embankment(ctx, 13*units.Meter)
	if got.Outcome != RequiresIndividualDesign {
		t.Fatalf("насыпь 13 м: исход %v, ожидался RequiresIndividualDesign", got.Outcome)
	}
	if got.Reason == "" {
		t.Fatal("отказ особо высокой насыпи обязан нести текст")
	}
}
