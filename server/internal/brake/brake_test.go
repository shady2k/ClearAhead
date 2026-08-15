package brake

import (
	"math"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/units"
)

// spec — уставки грузового поезда, те же, что кладёт набор для ВЛ80. Своя копия
// в тесте была бы вторым источником истины; здесь она стоит потому, что пакету
// набор не виден, и число проверяется как ЧИСЛО, а не как «то, что в файле».
func spec() Spec {
	return Spec{
		Charge:          FromKgf(5.4),
		FullServiceDrop: FromKgf(1.5),
		CylinderFull:    FromKgf(3.8),
		ServiceRate:     FromKgf(0.22),
		EmergencyRate:   FromKgf(0.9),
		ChargeRate:      FromKgf(0.6),
		LeakRate:        FromKgf(0.02),
		MainMin:         FromKgf(7.5),
		MainMax:         FromKgf(9.0),
		CompressorRate:  FromKgf(0.2),
		CylinderRate:    FromKgf(0.9),
	}
}

// run — прогнать пневматику seconds секунд шагом физики и вернуть состояние.
// Шаг тот же, что у сима (20 мс): пневматика обязана вести себя одинаково на
// том шаге, на котором её действительно считают.
func run(s State, sp Spec, h Handle, seconds float64) State {
	return runWith(s, sp, h, 0, seconds)
}

// runWith — то же, но с заданным краном вспомогательного тормоза.
func runWith(s State, sp Spec, h Handle, indep Pressure, seconds float64) State {
	steps := int(seconds * 50)
	for range steps {
		s = Step(s, sp, h, indep, 20*units.Millisecond)
	}
	return s
}

func TestChargedStartsUnderPressure(t *testing.T) {
	s := Charged(spec())
	if s.Pipe != FromKgf(5.4) {
		t.Fatalf("магистраль заряженной машины %s, ожидалось 5.40 кгс/см²", s.Pipe)
	}
	if s.Cylinder != 0 {
		t.Fatalf("цилиндр заряженной машины %s, ожидался пустым", s.Cylinder)
	}
	if e := s.Effort(spec()); e != 0 {
		t.Fatalf("доля нажатия у заряженной машины %d‰, ожидался ноль", e)
	}
}

// ТЕМП СЛУЖЕБНОЙ РАЗРЯДКИ — ЗАМЕР, А НЕ ОБЪЯВЛЕНИЕ. Уставка 0.22 кгс/см² в
// секунду; за пять секунд магистраль обязана опуститься на 1.1, и это
// проверяется прогоном шагом физики, а не умножением в уме: между уставкой и
// поведением стоит целочисленное деление на каждом из 250 шагов.
func TestServiceDischargeFollowsRate(t *testing.T) {
	sp := spec()
	s := run(Charged(sp), sp, HandleService, 5)
	want := FromKgf(5.4 - 5*0.22)
	if d := s.Pipe - want; d > FromKgf(0.005) || d < -FromKgf(0.005) {
		t.Fatalf("магистраль после 5 с служебной разрядки %s, ожидалось %s", s.Pipe, want)
	}
}

// ПЕРВАЯ СТУПЕНЬ ТОРМОЖЕНИЯ — 0.5…0.6 кгс/см² разрядки. Проверяется СЛЕДСТВИЕ, а
// не сама разрядка: на первой ступени цилиндр обязан наполниться примерно на
// треть полного служебного (0.55 из 1.5), то есть тормоз уже есть, но он не
// полный.
func TestFirstStepGivesPartialEffort(t *testing.T) {
	sp := spec()
	s := run(Charged(sp), sp, HandleService, 2.5) // 0.55 кгс/см²
	s = run(s, sp, HandleHold, 5)                 // перекрыша: цилиндр успевает наполниться
	e := s.Effort(sp)
	if e < 300 || e > 400 {
		t.Fatalf("доля нажатия на первой ступени %d‰, ожидалось около 367‰ (0.55 из 1.5); цилиндр %s", e, s.Cylinder)
	}
}

// ПОЛНОЕ СЛУЖЕБНОЕ: разрядка на 1.5 даёт полное нажатие и БОЛЬШЕ НЕ РАСТЁТ.
// Второе важнее первого: воздухораспределитель упирается, и дальнейшая разрядка
// тормоза не усиливает — иначе служебным можно было бы получить экстренное.
func TestFullServiceCapsCylinder(t *testing.T) {
	sp := spec()
	s := run(Charged(sp), sp, HandleService, 7) // 1.54 — чуть больше полной разрядки
	s = run(s, sp, HandleHold, 8)
	if s.Cylinder != sp.CylinderFull {
		t.Fatalf("цилиндр при полном служебном %s, ожидалось %s", s.Cylinder, sp.CylinderFull)
	}
	deeper := run(s, sp, HandleService, 5) // разряжаем ещё
	deeper = run(deeper, sp, HandleHold, 5)
	if deeper.Cylinder != sp.CylinderFull {
		t.Fatalf("цилиндр после разрядки СВЕРХ полной %s, ожидалось прежнее %s — распределитель обязан упереться",
			deeper.Cylinder, sp.CylinderFull)
	}
	if e := deeper.Effort(sp); e != 1000 {
		t.Fatalf("доля нажатия %d‰, ожидалась полная", e)
	}
}

// ПЕРЕКРЫША БЕЗ ПИТАНИЯ ОТЛИЧАЕТСЯ ОТ ПЕРЕКРЫШИ С ПИТАНИЕМ, и отличие — утечка.
// Без этой разницы два положения крана были бы одним, а на пульте — двумя
// одинаковыми, то есть обманкой.
func TestLapLeaksAndHoldDoesNot(t *testing.T) {
	sp := spec()
	braked := run(Charged(sp), sp, HandleService, 2.5)
	lap := run(braked, sp, HandleLap, 30)
	hold := run(braked, sp, HandleHold, 30)
	if lap.Pipe >= braked.Pipe {
		t.Fatalf("перекрыша без питания не травит: было %s, стало %s", braked.Pipe, lap.Pipe)
	}
	if hold.Pipe != braked.Pipe {
		t.Fatalf("перекрыша с питанием не держит: было %s, стало %s", braked.Pipe, hold.Pipe)
	}
	// И следствие, ради которого разница существует: без питания тормоз сам
	// собой УСИЛИВАЕТСЯ, потому что разрядка растёт.
	if lap.Effort(sp) <= hold.Effort(sp) {
		t.Fatalf("утечка не усилила тормоз: без питания %d‰, с питанием %d‰",
			lap.Effort(sp), hold.Effort(sp))
	}
}

// ЭКСТРЕННОЕ БЫСТРЕЕ СЛУЖЕБНОГО, и насколько — замер. Уставка требует не менее
// 0.8 кгс/см² в секунду против 0.22 служебного: за одну секунду экстренное
// обязано увести магистраль дальше, чем служебное за три.
func TestEmergencyOutrunsService(t *testing.T) {
	sp := spec()
	em := run(Charged(sp), sp, HandleEmergency, 1)
	sv := run(Charged(sp), sp, HandleService, 3)
	if em.Pipe >= sv.Pipe {
		t.Fatalf("экстренное за 1 с (%s) не глубже служебного за 3 с (%s)", em.Pipe, sv.Pipe)
	}
	full := run(Charged(sp), sp, HandleEmergency, 7)
	if full.Pipe != 0 {
		t.Fatalf("магистраль после экстренного %s, ожидался ноль — разрядка идёт в атмосферу", full.Pipe)
	}
}

// ОТПУСК ВОЗВРАЩАЕТ МАШИНУ В ХОД: первое положение заряжает магистраль, цилиндр
// опорожняется, доля нажатия уходит в ноль. Без этого тормоз был бы дорогой в
// одну сторону.
func TestReleaseRechargesAndFreesWheels(t *testing.T) {
	sp := spec()
	s := run(Charged(sp), sp, HandleService, 7)
	s = run(s, sp, HandleHold, 8)
	if s.Effort(sp) != 1000 {
		t.Fatalf("перед отпуском тормоз не полный: %d‰", s.Effort(sp))
	}
	s = run(s, sp, HandleRelease, 10)
	if s.Pipe < sp.Charge {
		t.Fatalf("магистраль после отпуска %s, ожидалось не ниже зарядного %s", s.Pipe, sp.Charge)
	}
	if s.Cylinder != 0 || s.Effort(sp) != 0 {
		t.Fatalf("после отпуска цилиндр %s, доля %d‰ — колодки не отошли", s.Cylinder, s.Effort(sp))
	}
}

// ПОЕЗДНОЕ ПОЛОЖЕНИЕ ВЕДЁТ К ЗАРЯДНОМУ С ДВУХ СТОРОН: и снизу (после
// торможения), и сверху (после сверхзарядки первым положением).
func TestRunHoldsChargeFromBothSides(t *testing.T) {
	sp := spec()
	low := run(run(Charged(sp), sp, HandleService, 3), sp, HandleRun, 20)
	if low.Pipe != sp.Charge {
		t.Fatalf("снизу поездное привело к %s, ожидалось зарядное %s", low.Pipe, sp.Charge)
	}
	high := run(Charged(sp), sp, HandleRelease, 10) // сверхзарядка до главных резервуаров
	if high.Pipe <= sp.Charge {
		t.Fatalf("отпуск не дал сверхзарядки: %s", high.Pipe)
	}
	back := run(high, sp, HandleRun, 20)
	if back.Pipe != sp.Charge {
		t.Fatalf("сверху поездное привело к %s, ожидалось зарядное %s", back.Pipe, sp.Charge)
	}
}

// КРАН ВСПОМОГАТЕЛЬНОГО ТОРМОЗА РАБОТАЕТ БЕЗ МАГИСТРАЛИ и не складывается с ней.
// Оба — про один цилиндр, и сумма дала бы машине тормоз, которого у неё нет.
func TestIndependentValveTakesTheLarger(t *testing.T) {
	sp := spec()
	alone := runWith(Charged(sp), sp, HandleRun, FromKgf(2.0), 8)
	if alone.Cylinder != FromKgf(2.0) {
		t.Fatalf("вспомогательный один: цилиндр %s, ожидалось %s", alone.Cylinder, FromKgf(2.0))
	}
	both := run(Charged(sp), sp, HandleService, 7)
	both = run(both, sp, HandleHold, 8)
	both = runWith(both, sp, HandleHold, FromKgf(2.0), 8)
	if both.Cylinder != sp.CylinderFull {
		t.Fatalf("оба сразу: цилиндр %s, ожидалось большее из двух (%s)", both.Cylinder, sp.CylinderFull)
	}
}

// ЦИЛИНДР НЕ ПРЫГАЕТ. Тормоз, наступающий в тот же миг, когда тронули ручку, —
// это не тормоз, а телепорт нажатия: замеряем, что наполнение занимает время.
func TestCylinderFillsOverTime(t *testing.T) {
	sp := spec()
	// МАГИСТРАЛЬ РАЗРЯЖЕНА РУКОЙ, а не краном, и это не поблажка тесту: темп
	// разрядки и темп наполнения цилиндра — два РАЗНЫХ числа, и торможение краном
	// смешивает их. Здесь проверяется второе, поэтому первое убрано из опыта.
	s := Charged(sp)
	s.Pipe = sp.Charge - sp.FullServiceDrop
	quick := Step(s, sp, HandleHold, 0, 20*units.Millisecond)
	if quick.Cylinder >= sp.CylinderFull {
		t.Fatalf("цилиндр наполнился за один шаг: %s", quick.Cylinder)
	}
	if quick.Cylinder == 0 {
		t.Fatalf("цилиндр не тронулся за шаг вовсе — наполнения нет")
	}
	slow := run(s, sp, HandleHold, 10)
	if slow.Cylinder != sp.CylinderFull {
		t.Fatalf("цилиндр не наполнился за 10 с: %s", slow.Cylinder)
	}
}

// КОМПРЕССОР ПОДДЕРЖИВАЕТ ЗАПАС, из которого заряжают магистраль. Проверяется
// следствие: после долгой езды с торможениями резервуары не опустели.
func TestCompressorKeepsMainReservoir(t *testing.T) {
	sp := spec()
	s := Charged(sp)
	s.Main = sp.MainMin
	s = run(s, sp, HandleRun, 20)
	if s.Main != sp.MainMax {
		t.Fatalf("резервуары после 20 с накачки %s, ожидалось %s", s.Main, sp.MainMax)
	}
}

// ШАГ НЕ ВЛИЯЕТ НА ИТОГ. Пневматику считают шагом 20 мс, но выбор шага —
// внутреннее дело сима, и поведение обязано быть тем же на вдвое мелком.
// Допуск 10 тысячных: целочисленное деление на каждом шаге неизбежно копит
// остаток, и требовать байт в байт значило бы требовать невозможного.
func TestStepSizeDoesNotChangeOutcome(t *testing.T) {
	sp := spec()
	coarse := Charged(sp)
	for range 250 {
		coarse = Step(coarse, sp, HandleService, 0, 20*units.Millisecond)
	}
	fine := Charged(sp)
	for range 500 {
		fine = Step(fine, sp, HandleService, 0, 10*units.Millisecond)
	}
	if d := math.Abs(float64(coarse.Pipe - fine.Pipe)); d > float64(FromKgf(0.01)) {
		t.Fatalf("шаг 20 мс дал %s, шаг 10 мс — %s: расхождение %.0f миллионных", coarse.Pipe, fine.Pipe, d)
	}
}

// ОТКАЗЫ ПАСПОРТА. Пневматика без чисел не «работает по умолчанию» — она
// отвергается, как отвергается карта с пропущенным полем.
func TestSpecRefusesNonsense(t *testing.T) {
	cases := []struct {
		name string
		edit func(*Spec)
		want string
	}{
		{"нет зарядного", func(s *Spec) { s.Charge = 0 }, "зарядное давление"},
		{"разрядка глубже зарядного", func(s *Spec) { s.FullServiceDrop = FromKgf(6) }, "полное служебное"},
		{"резервуары ниже зарядного", func(s *Spec) { s.MainMax = FromKgf(5); s.MainMin = FromKgf(4) }, "главные резервуары"},
		{"экстренное не быстрее служебного", func(s *Spec) { s.EmergencyRate = FromKgf(0.1) }, "не быстрее служебной"},
		{"нет темпа наполнения цилиндра", func(s *Spec) { s.CylinderRate = 0 }, "цилиндра"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sp := spec()
			c.edit(&sp)
			err := sp.Validate()
			if err == nil {
				t.Fatalf("ожидался отказ (%s), паспорт принят", c.want)
			}
			if !contains(err.Error(), c.want) {
				t.Fatalf("отказ %q не содержит %q", err, c.want)
			}
		})
	}
	if err := spec().Validate(); err != nil {
		t.Fatalf("исправный паспорт отвергнут: %v", err)
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// У РАЗНЫХ МАШИН РАЗНАЯ ТОРМОЗНАЯ СИСТЕМА — не только уставки, но и НАБОР
// органов (слово владельца 2026-08-15). Кран вспомогательного тормоза есть не у
// всякой машины, и «нет крана» обязано отличаться от «кран в нуле»: первое —
// отказ команде и отсутствие рукоятки на пульте, второе — рабочее положение.
func TestIndependentValveMayBeAbsent(t *testing.T) {
	with := spec()
	with.IndependentMax = FromKgf(4.0)
	if !with.HasIndependent() {
		t.Fatal("машина с краном не признаёт его своим")
	}
	if _, err := with.SetIndependent(FromKgf(2.0)); err != nil {
		t.Fatalf("исправная команда отвергнута: %v", err)
	}
	if _, err := with.SetIndependent(FromKgf(9.0)); err == nil {
		t.Fatal("давление выше предельного принято")
	}

	without := spec() // IndependentMax не задан — крана нет
	if without.HasIndependent() {
		t.Fatal("машина без крана считает, что он есть")
	}
	if _, err := without.SetIndependent(FromKgf(2.0)); err == nil {
		t.Fatal("команда крану, которого нет, принята молча")
	}
}
