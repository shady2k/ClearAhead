package physics

import (
	"math"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/units"
)

// vl80Resistance — коэффициенты электровоза по ПТР: w = 1.9 + 0.01V + 0.0003V².
func vl80Resistance(t *testing.T) Resistance {
	t.Helper()
	r, err := ResistanceFrom(1.9, 0.01, 0.0003)
	if err != nil {
		t.Fatalf("коэффициенты: %v", err)
	}
	return r
}

func kmh(t *testing.T, v float64) units.Speed {
	t.Helper()
	s, err := units.KmhToSpeed(v)
	if err != nil {
		t.Fatalf("%v км/ч: %v", v, err)
	}
	return s
}

func tonnes(t *testing.T, m float64) units.Mass {
	t.Helper()
	v, err := units.TonnesToMass(m)
	if err != nil {
		t.Fatalf("%v т: %v", m, err)
	}
	return v
}

// TestBasicResistanceMatchesHandCalculation — число против числа, посчитанного
// руками из формулы ПТР. Допуск ЕДИНИЦА последнего разряда (0.001 Н/кН): это
// целочисленная арифметика, и расхождение больше единицы означало бы ошибку в
// шкале, а не в округлении.
func TestBasicResistanceMatchesHandCalculation(t *testing.T) {
	r := vl80Resistance(t)
	cases := []struct {
		v    float64 // км/ч
		want int64   // тысячные Н/кН, посчитано руками
	}{
		{0, 1900},   // 1.9
		{10, 2030},  // 1.9 + 0.1 + 0.03
		{50, 3150},  // 1.9 + 0.5 + 0.75
		{100, 5900}, // 1.9 + 1.0 + 3.0
		{110, 6630}, // 1.9 + 1.1 + 3.63 — конструкционная скорость ВЛ80
	}
	for _, c := range cases {
		got := int64(r.At(kmh(t, c.v)))
		if got-c.want > 1 || c.want-got > 1 {
			t.Errorf("%.0f км/ч: сопротивление %d, руками %d (тысячные Н/кН)", c.v, got, c.want)
		}
	}
}

// TestResistanceIgnoresDirection — сопротивление направлено против движения и на
// заднем ходу не становится отрицательным.
func TestResistanceIgnoresDirection(t *testing.T) {
	r := vl80Resistance(t)
	if f, b := r.At(kmh(t, 60)), r.At(kmh(t, -60)); f != b {
		t.Fatalf("вперёд %d, назад %d — сопротивление обязано быть одинаковым", f, b)
	}
}

// TestGradeResistanceEqualsGradeItself — тождество ПТР: удельное сопротивление
// от уклона численно равно уклону в промилле. Проверяется потому, что это
// ЕДИНСТВЕННОЕ место, где две разные величины намеренно совпадают числом, и
// правка шкалы с любой стороны обязана его сломать.
func TestGradeResistanceEqualsGradeItself(t *testing.T) {
	for _, milliPermille := range []int64{0, 2500, -2500, 40_000} {
		if got := int64(GradeResistance(milliPermille)); got != milliPermille {
			t.Fatalf("уклон %d даёт %d", milliPermille, got)
		}
	}
}

// TestCurveResistanceMatchesPTR — w = 700/R.
//
// Радиус 700 м взят первым нарочно: на нём формула даёт ровно 1 Н/кН, и это
// значение приводится в источниках отдельно как проверочное.
func TestCurveResistanceMatchesPTR(t *testing.T) {
	cases := []struct {
		radiusM float64
		want    int64 // тысячные Н/кН
	}{
		{700, 1000},
		{500, 1400},
		{200, 3500},
		{1000, 700},
	}
	for _, c := range cases {
		r, err := units.MetersToDistance(c.radiusM)
		if err != nil {
			t.Fatalf("радиус: %v", err)
		}
		if got := int64(CurveResistance(r)); got != c.want {
			t.Errorf("R = %.0f м: %d, ожидалось %d", c.radiusM, got, c.want)
		}
	}
}

// TestStraightTrackHasNoCurveResistance — прямая приходит нулевым радиусом (у
// примитива straight поля radius нет вовсе), и это обязано давать ноль, а не
// деление на ноль.
func TestStraightTrackHasNoCurveResistance(t *testing.T) {
	if got := CurveResistance(0); got != 0 {
		t.Fatalf("прямая дала %d", got)
	}
}

// TestAdhesionMatchesHandCalculation — ψ = 0.28 + 3/(50+20V) − 0.0007V.
func TestAdhesionMatchesHandCalculation(t *testing.T) {
	cases := []struct {
		v    float64
		want int64 // тысячные
	}{
		{0, 340},   // 0.28 + 0.06
		{50, 248},  // 0.28 + 0.00286 − 0.035
		{100, 211}, // 0.28 + 0.00146 − 0.07
	}
	for _, c := range cases {
		got := int64(Adhesion(kmh(t, c.v)))
		if got-c.want > 1 || c.want-got > 1 {
			t.Errorf("%.0f км/ч: сцепление %d, руками %d (тысячные)", c.v, got, c.want)
		}
	}
}

// TestStartingEffortReproducesPublishedChme3 — САМАЯ ЦЕННАЯ ПРОВЕРКА ПАКЕТА.
//
// Паспорт ЧМЭ3 называет силу тяги трогания ВМЕСТЕ с коэффициентом, при котором
// она посчитана: «при коэффициенте сцепления 0,25 — не менее 30,8 тс» при
// служебной массе 123 т и осевой формуле 3о−3о (все шесть осей движущие, значит
// сцепной вес равен служебной массе).
//
// Это редкий случай, когда источник позволяет проверить не результат модели, а
// саму механику: если наше F = ψ·P_сц воспроизводит опубликованное число, то
// сцепление, вес и перевод тонны-силы согласованы все разом. Расхождение здесь
// означало бы ошибку в g, в шкале массы или в шкале силы — то есть в том, на чём
// стоит весь пакет.
func TestStartingEffortReproducesPublishedChme3(t *testing.T) {
	const publishedTF = 30.8 // тс, паспорт
	adhesive := tonnes(t, 123)
	got := MilliAdhesion(250).On(adhesive.Weight())
	// Допуск 0.1 тс — разряд, в котором паспорт и напечатан. Требовать точнее
	// значило бы требовать от источника точности, которой в нём нет.
	if diff := math.Abs(got.TonnesForce() - publishedTF); diff > 0.1 {
		t.Fatalf("сила трогания %.2f тс (%.0f кН), паспорт называет %.1f тс, расхождение %.2f тс",
			got.TonnesForce(), got.Kilonewtons(), publishedTF, diff)
	}
}

// vl80 — боевая машина затравки, числами из паспорта.
func vl80(t *testing.T) Locomotive {
	t.Helper()
	// Мощность на ободе выведена из ДЛИТЕЛЬНОГО режима: 40.9 тс при 53.6 км/ч.
	// Разбор, почему не из мощности двигателя, — у content.StockType.Traction.
	force := MilliAdhesion(1000).On(tonnes(t, 40.9).Weight()) // 40.9 тс в ньютонах
	speed := kmh(t, 53.6)
	return Locomotive{
		Mass:         tonnes(t, 192),
		AdhesiveMass: tonnes(t, 192),
		RimPower:     int64(force) * int64(speed) / 1_000_000,
		MaxSpeed:     kmh(t, 110),
		Res:          vl80Resistance(t),
	}
}

// TestTractiveEffortIsAdhesionLimitedAtStart — трогается машина сцеплением, а не
// мощностью: на нуле мощность не ограничивает вовсе.
func TestTractiveEffortIsAdhesionLimitedAtStart(t *testing.T) {
	l := vl80(t)
	got := l.TractiveEffort(0)
	want := MilliAdhesion(340).On(l.AdhesiveMass.Weight()) // ψ(0) = 0.34
	if got != want {
		t.Fatalf("сила трогания %d Н, по сцеплению %d Н", got, want)
	}
	// 0.34 · 192 т — около 65 тс. Проверка порядка, а не равенства: она ловит
	// ошибку в разряде, из-за которой машина трогалась бы с шести или с
	// шестисот тонн-силы.
	if tf := got.TonnesForce(); tf < 60 || tf > 70 {
		t.Fatalf("сила трогания %.1f тс — вне правдоподобного порядка", tf)
	}
}

// TestContinuousRegimeReproducesItsOwnPoint — на скорости длительного режима
// огибающая обязана дать паспортную силу этого режима.
//
// Проверка выглядит круговой (мощность на ободе выведена из этой же пары), и
// она такой и является ПО ЧИСЛУ — но не по смыслу: круг проходит через два
// перевода шкал и одно деление, и любая ошибка в них его разомкнёт. Заодно
// проверяется, что на этой скорости ограничивает именно мощность, а не
// сцепление, — иначе паспортная точка была бы недостижима.
func TestContinuousRegimeReproducesItsOwnPoint(t *testing.T) {
	l := vl80(t)
	v := kmh(t, 53.6)
	got := l.TractiveEffort(v)
	if diff := math.Abs(got.TonnesForce() - 40.9); diff > 0.1 {
		t.Fatalf("на 53.6 км/ч сила %.2f тс, паспорт называет 40.9 тс", got.TonnesForce())
	}
	if byAdhesion := Adhesion(v).On(l.AdhesiveMass.Weight()); byAdhesion <= got {
		t.Fatalf("сцепление (%.1f тс) не выше тяги (%.1f тс) — паспортный режим недостижим",
			byAdhesion.TonnesForce(), got.TonnesForce())
	}
}

// TestTractiveEffortFallsWithSpeed — огибающая монотонно убывает: машина,
// которая на ста километрах тянет сильнее, чем на пятидесяти, — это ошибка в
// пределе по мощности.
func TestTractiveEffortFallsWithSpeed(t *testing.T) {
	l := vl80(t)
	prev := l.TractiveEffort(kmh(t, 1))
	for v := 5.0; v <= 110; v += 5 {
		got := l.TractiveEffort(kmh(t, v))
		if got > prev {
			t.Fatalf("на %.0f км/ч тяга %d Н выросла против предыдущей %d Н", v, got, prev)
		}
		prev = got
	}
}

// TestResistSumsThreeComponents — три составляющие складываются, и складываются
// напрямую: это ПТР, а не наше удобство.
func TestResistSumsThreeComponents(t *testing.T) {
	l := vl80(t)
	v := kmh(t, 50)
	radius, err := units.MetersToDistance(500)
	if err != nil {
		t.Fatalf("радиус: %v", err)
	}
	// Руками: 3.15 (основное) + 2.5 (подъём 2.5 ‰) + 1.4 (кривая R=500) = 7.05 Н/кН
	// на весе 192 т = 1 883 088 Н, то есть 7.05 · 1883.088 = 13 276 Н.
	got := l.Resist(v, 2500, radius)
	if got < 13_200 || got > 13_350 {
		t.Fatalf("сопротивление %d Н, руками около 13 276 Н", got)
	}
	// Спуск той же крутизны обязан УМЕНЬШИТЬ сумму ровно на удвоенный вклад
	// уклона: знак есть только у него.
	down := l.Resist(v, -2500, radius)
	if diff := got - down; diff < 9_350 || diff > 9_500 {
		t.Fatalf("подъём минус спуск = %d Н, ожидалось около 2·2.5·1883 = 9415 Н", diff)
	}
}

// TestResistanceFromRefusesNonsense — валидатор отказывает, а не чинит.
func TestResistanceFromRefusesNonsense(t *testing.T) {
	cases := []struct {
		name    string
		a, b, c float64
	}{
		{"a нулевое", 0, 0.01, 0.0003},
		{"a отрицательное", -1.9, 0.01, 0.0003},
		{"b отрицательное", 1.9, -0.01, 0.0003},
		{"не число", math.NaN(), 0.01, 0.0003},
		{"бесконечность", math.Inf(1), 0.01, 0.0003},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := ResistanceFrom(c.a, c.b, c.c); err == nil {
				t.Fatal("принято, ожидался отказ")
			}
		})
	}
}

// TestDivRoundGoesToNearestBothWays — округление к ближайшему, а не
// отбрасывание, и одинаково по обе стороны нуля: иначе подъём и спуск одной
// крутизны дали бы разные по модулю силы.
func TestDivRoundGoesToNearestBothWays(t *testing.T) {
	cases := []struct{ num, den, want int64 }{
		{10, 4, 3}, {-10, 4, -3},
		{9, 4, 2}, {-9, 4, -2},
		{6, 4, 2}, {-6, 4, -2},
		{0, 4, 0},
	}
	for _, c := range cases {
		if got := divRound(c.num, c.den); got != c.want {
			t.Errorf("divRound(%d, %d) = %d, ожидалось %d", c.num, c.den, got, c.want)
		}
	}
}

// TestFrictionMatchesMPSFormula — коэффициент трения колодок против формул,
// утверждённых МПС. Значения на нуле совпадают с обоими источниками (0.27 и
// 0.36), а на сотне разводят дробный вид с линейным вдвое — ради этого разбора
// проверка и стоит.
func TestFrictionMatchesMPSFormula(t *testing.T) {
	cases := []struct {
		kind ShoeKind
		v    float64
		want int64 // тысячные
	}{
		{ShoesCastIron, 0, 270},    // 0.27 · 100/100
		{ShoesCastIron, 20, 162},   // 0.27 · 120/200
		{ShoesCastIron, 100, 90},   // 0.27 · 200/600
		{ShoesComposite, 0, 360},   // 0.36 · 150/150
		{ShoesComposite, 100, 257}, // 0.36 · 250/350
	}
	for _, c := range cases {
		got := int64(Friction(c.kind, kmh(t, c.v)))
		if got-c.want > 1 || c.want-got > 1 {
			t.Errorf("%s на %.0f км/ч: %d, руками %d (тысячные)", c.kind, c.v, got, c.want)
		}
	}
}

// TestCastIronLosesMoreThanCompositeWithSpeed — чугунная колодка теряет трение
// со скоростью ЗАМЕТНО сильнее композиционной, и это не свойство наших формул,
// а причина, по которой композиционные колодки вообще появились. Проверка
// сторожит от подстановки одной формулы вместо другой: перепутав их, получим
// правдоподобные числа с неверной зависимостью.
func TestCastIronLosesMoreThanCompositeWithSpeed(t *testing.T) {
	fast := kmh(t, 100)
	iron := float64(Friction(ShoesCastIron, fast)) / float64(Friction(ShoesCastIron, 0))
	comp := float64(Friction(ShoesComposite, fast)) / float64(Friction(ShoesComposite, 0))
	if iron >= comp {
		t.Fatalf("на 100 км/ч чугун сохраняет %.2f доли, композит %.2f — ожидалось наоборот", iron, comp)
	}
}

// TestBrakeForceMatchesHandCalculation — полное служебное торможение ВЛ80.
//
// Руками: расчётное нажатие 14.0 тс на ось (таблица ПТР, гружёный режим) на
// восьми осях даёт 112 тс; при трогании φкр = 0.27, то есть 30.2 тс тормозной
// силы. На массе 192 т это замедление около 0.15 g.
func TestBrakeForceMatchesHandCalculation(t *testing.T) {
	l := vl80(t)
	l.Shoes = ShoesCastIron
	l.BrakedAxles = 8
	l.AxleBrakeForce = MilliAdhesion(1000).On(tonnes(t, 14).Weight())
	got := l.BrakeForce(0)
	if diff := math.Abs(got.TonnesForce() - 30.2); diff > 0.2 {
		t.Fatalf("тормозная сила %.2f тс, руками 30.2 тс", got.TonnesForce())
	}
	// Замедление порядка 0.15 g. Проверка порядка ловит ошибку в разряде, из-за
	// которой машина останавливалась бы за метр или за десять километров.
	decel := float64(got) / float64(l.Mass)
	if decel < 1.0 || decel > 2.5 {
		t.Fatalf("замедление %.2f м/с² — вне правдоподобного порядка", decel)
	}
}

// TestBrakeStaysWithinAdhesionAtStart — полное служебное торможение НЕ должно
// превышать сцепления, иначе колёса встают на юз при первом же применении.
//
// Это не проверка нашей арифметики, а сверка двух независимых таблиц ПТР —
// нажатий и сцепления — между собой. Разойдись они, виновата была бы не модель.
func TestBrakeStaysWithinAdhesionAtStart(t *testing.T) {
	l := vl80(t)
	l.Shoes = ShoesCastIron
	l.BrakedAxles = 8
	l.AxleBrakeForce = MilliAdhesion(1000).On(tonnes(t, 14).Weight())
	brake := l.BrakeForce(0)
	limit := Adhesion(0).On(l.AdhesiveMass.Weight())
	if brake >= limit {
		t.Fatalf("тормоз %.1f тс не ниже предела сцепления %.1f тс — колёса встанут на юз",
			brake.TonnesForce(), limit.TonnesForce())
	}
}

// vl80Slipping — та же боевая машина, но с ОБЪЯВЛЕННЫМ пределом двигателей.
// Отдельной функцией, а не полем в vl80: без предела машина не буксует вовсе, и
// прежние проверки огибающей обязаны видеть её прежней.
func vl80Slipping(t *testing.T) Locomotive {
	t.Helper()
	l := vl80(t)
	l.MaxForce = MilliAdhesion(1000).On(tonnes(t, 91.8).Weight()) // 900 кН
	return l
}

// TestFullNotchSlipsFromStandstill — ГЛАВНОЕ СЛЕДСТВИЕ: с места на последней
// позиции машина БУКСУЕТ, а не едет.
//
// Проверка заведена по замечанию владельца 2026-08-15: «можно установить
// контроллер сразу на 33 и он поедет, а должен буксовать». До разведения
// пределов это было невозможно по построению — доля бралась от числа, уже
// ограниченного сцеплением.
func TestFullNotchSlipsFromStandstill(t *testing.T) {
	l := vl80Slipping(t)
	full, slipping := l.Traction(0, 1000)
	if !slipping {
		t.Fatalf("на полной позиции с места не забуксовала: сила %.1f кН, сцепление держит %.1f кН",
			full.Kilonewtons(), l.AdhesionLimit(0).Kilonewtons())
	}
	// И ЕДЕТ ХУЖЕ, а не так же: буксование обязано СТОИТЬ силы, иначе машинисту
	// всё равно, буксует он или нет.
	if full >= l.AdhesionLimit(0) {
		t.Fatalf("буксующая машина даёт %.1f кН при сцеплении %.1f кН — буксование бесплатно",
			full.Kilonewtons(), l.AdhesionLimit(0).Kilonewtons())
	}
}

// TestGentleNotchDoesNotSlip — и обратное: осторожный набор позиции не буксует.
// Без этой половины «буксует на полной» доказывало бы лишь то, что машина
// буксует всегда.
func TestGentleNotchDoesNotSlip(t *testing.T) {
	l := vl80Slipping(t)
	hold := l.AdhesionLimit(0)
	for notch := 1; notch <= 33; notch++ {
		permille := int64(notch) * 1000 / 33
		f, slipping := l.Traction(0, permille)
		want := units.Force(divRound(int64(l.TractionLimit(0))*permille, 1000))
		if want <= hold {
			if slipping {
				t.Fatalf("позиция %d: буксует, хотя двигатели просят %.1f кН при сцеплении %.1f кН",
					notch, want.Kilonewtons(), hold.Kilonewtons())
			}
			if f != want {
				t.Fatalf("позиция %d: сила %.1f кН, ожидалась %.1f кН", notch, f.Kilonewtons(), want.Kilonewtons())
			}
		} else if !slipping {
			t.Fatalf("позиция %d: двигатели просят %.1f кН больше сцепления %.1f кН, а буксования нет",
				notch, want.Kilonewtons(), hold.Kilonewtons())
		}
	}
	// ГРАНИЦА НАЗВАНА ЧИСЛОМ: до какой позиции машина трогается без буксования.
	last := 0
	for notch := 1; notch <= 33; notch++ {
		if _, slipping := l.Traction(0, int64(notch)*1000/33); !slipping {
			last = notch
		}
	}
	if last <= 1 || last >= 33 {
		t.Fatalf("без буксования берутся позиции до %d из 33 — граница вырождена", last)
	}
	t.Logf("с места без буксования берутся позиции до %d из 33 (сцепление держит %.1f кН, двигатели дают %.1f кН)",
		last, hold.Kilonewtons(), l.TractionLimit(0).Kilonewtons())
}

// TestNoMaxForceKeepsOldBehaviour — машина без объявленного предела двигателей
// не буксует вовсе, и это законно, а не недосмотр: у неё ограничение другой
// природы, и подставлять ей электровозное значило бы выдумать.
func TestNoMaxForceKeepsOldBehaviour(t *testing.T) {
	l := vl80Slipping(t)
	l.MaxForce = 0
	f, slipping := l.Traction(0, 1000)
	if slipping {
		t.Fatal("машина без объявленного предела двигателей забуксовала")
	}
	if f != l.TractiveEffort(0) {
		t.Fatalf("сила %.1f кН, ожидалась прежняя огибающая %.1f кН", f.Kilonewtons(), l.TractiveEffort(0).Kilonewtons())
	}
}

// TestSlipEasesWithSpeed — на ходу буксовать труднее, чем с места, и это не
// вкус: сцепление с ростом скорости падает, но и предел мощности падает быстрее,
// поэтому позиция, срывавшая машину на месте, на скорости её держит.
func TestSlipEasesWithSpeed(t *testing.T) {
	l := vl80Slipping(t)
	fast, err := units.KmhToSpeed(60)
	if err != nil {
		t.Fatalf("скорость: %v", err)
	}
	if _, slipping := l.Traction(0, 1000); !slipping {
		t.Fatal("с места на полной позиции не буксует — предпосылка проверки неверна")
	}
	if _, slipping := l.Traction(fast, 1000); slipping {
		t.Fatalf("на 60 км/ч полная позиция всё ещё буксует: двигатели дают %.1f кН, сцепление держит %.1f кН",
			l.TractionLimit(fast).Kilonewtons(), l.AdhesionLimit(fast).Kilonewtons())
	}
}

// balanceKmh — на какой скорости позиция уравновешивается основным
// сопротивлением одиночной машины на площадке. Перебором с шагом 0.1 км/ч:
// решать уравнение аналитически незачем, а перебор говорит ровно то, что видит
// машинист, — куда стрелка скоростемера приходит и встаёт.
//
// −1 значит «не уравновешивается до конструкционной»: машина упирается в предел
// паспорта, а не в физику.
func balanceKmh(t *testing.T, l Locomotive, permille int64) float64 {
	t.Helper()
	for i := 0; ; i++ {
		v := units.Speed(int64(i) * int64(kmh(t, 0.1)))
		if v > l.MaxSpeed {
			return -1
		}
		f, _ := l.Traction(v, permille)
		if f <= l.Res.At(v).On(l.Mass.Weight()) {
			return float64(i) / 10
		}
	}
}

// TestNotchHasItsOwnBalancingSpeed — ГЛАВНОЕ СЛЕДСТВИЕ характеристики позиций:
// у каждой позиции СВОЯ скорость, на которой машина перестаёт разгоняться.
//
// Проверка заведена по замечанию владельца 2026-08-15: «даже в положении 1 он
// набирает скорость бесконечно, так не может быть». До третьего предела
// (противо-ЭДС) позиция была долей МОЩНОСТИ, и замер давал на первой позиции
// 77 км/ч, на второй 107, а с третьей — вовсе ничего: сила падала как 1/v и
// сопротивление её не догоняло.
//
// Числа тут НЕ ЭТАЛОН, а границы здравого смысла: первая позиция — шаг человека,
// последняя — почти конструкционная, между ними монотонно.
func TestNotchHasItsOwnBalancingSpeed(t *testing.T) {
	l := vl80Slipping(t)
	prev := 0.0
	for _, notch := range []int{1, 2, 5, 10, 16, 23, 33} {
		got := balanceKmh(t, l, int64(notch)*1000/33)
		if got < 0 {
			t.Fatalf("позиция %d не уравновешивается до конструкционной — сила не обращается в ноль ни на какой скорости",
				notch)
		}
		if got <= prev {
			t.Fatalf("позиция %d уравновешивается на %.1f км/ч, а предыдущая на %.1f — позиции не растут",
				notch, got, prev)
		}
		prev = got
		t.Logf("позиция %2d из 33: равновесие на %.1f км/ч", notch, got)
	}
	first := balanceKmh(t, l, 1000/33)
	if first > 10 {
		t.Fatalf("первая позиция выводит одиночную машину на %.1f км/ч — это не первая позиция", first)
	}
	top := float64(l.MaxSpeed.MilliKmh()) / 1000
	last := balanceKmh(t, l, 1000)
	if last < top*0.95 {
		t.Fatalf("последняя позиция выводит только на %.1f км/ч при конструкционной %.1f — машина слабее паспорта",
			last, top)
	}
}

// TestLastNotchMeetsTheContinuousRating — СВЕРКА С ПАСПОРТОМ, а не с самим
// собой: на последней позиции в точке длительного режима модель обязана дать
// ровно паспортную силу. Это единственная точка тяговой характеристики, о
// которой паспорт вообще что-то говорит, и промахнуться в ней значило бы, что
// три предела сошлись не в ту фигуру.
func TestLastNotchMeetsTheContinuousRating(t *testing.T) {
	l := vl80Slipping(t)
	want := MilliAdhesion(1000).On(tonnes(t, 40.9).Weight()) // 40.9 тс = 401.1 кН
	got := l.NotchEffort(kmh(t, 53.6), 1000)
	if d := got - want; d > want/100 || -d > want/100 {
		t.Fatalf("на последней позиции при 53.6 км/ч %.1f кН, паспорт длительного режима %.1f кН",
			got.Kilonewtons(), want.Kilonewtons())
	}
}

// TestNotchPullsHarderAgainstTheRoll — трогание ПОД УКЛОН: машину катит
// навстречу тяге, и противо-ЭДС при этом не съедает напряжение, а добавляет к
// нему. Сила обязана быть не меньше, чем на стоянке.
//
// Проверка сторожит модуль скорости: он выглядит безобидно и переворачивает
// знак ровно здесь — машина, которую покатило назад, тянула бы слабее стоящей.
func TestNotchPullsHarderAgainstTheRoll(t *testing.T) {
	l := vl80Slipping(t)
	still := l.NotchEffort(0, 300)
	rolling := l.NotchEffort(-kmh(t, 20), 300)
	if rolling < still {
		t.Fatalf("на встречном ходу %.1f кН, на стоянке %.1f кН — противо-ЭДС вычлась не тем знаком",
			rolling.Kilonewtons(), still.Kilonewtons())
	}
}
