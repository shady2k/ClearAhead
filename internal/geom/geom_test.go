package geom_test

import (
	"math"
	"testing"

	"github.com/shady2k/ClearAhead/internal/geom"
	"github.com/shady2k/ClearAhead/internal/units"
)

// Эталоны здесь посчитаны из геометрии окружности вручную, а не той функцией,
// которую они проверяют (vertical-slice-design §7). Все случаи подобраны так,
// чтобы ответ выписывался в замкнутом виде: четверти и половины оборота, углы
// 45°, центр дуги в очевидной точке.
//
// Допуск спека — 0,5 мм по положению и 1e-5 рад по направлению. Здесь взят
// микрометр: это собственное разрешение домена, ниже которого вопрос «где точка»
// не имеет смысла, и при этом на три порядка строже спека — ошибка в формуле
// сюда не пролезет, а квантование смещения до целых микрометров пролезет.
const (
	tolPos     = 1e-6 // метры, то есть один микрометр
	tolHeading = 1e-8 // радианы
)

func m(v float64) units.Distance {
	d, err := units.MetersToDistance(v)
	if err != nil {
		panic(err)
	}
	return d
}

func straight(t *testing.T, length float64) geom.Primitive {
	t.Helper()
	p, err := geom.Straight(m(length))
	if err != nil {
		t.Fatalf("geom.Straight(%v): %v", length, err)
	}
	return p
}

func arc(t *testing.T, radiusM, angleRad float64) geom.Primitive {
	t.Helper()
	p, err := geom.Arc(radiusM, angleRad)
	if err != nil {
		t.Fatalf("geom.Arc(%v, %v): %v", radiusM, angleRad, err)
	}
	return p
}

func assertPose(t *testing.T, name string, got, want geom.Pose) {
	t.Helper()
	if math.Abs(got.X-want.X) > tolPos || math.Abs(got.Y-want.Y) > tolPos {
		t.Errorf("%s: положение (%.12f, %.12f), ожидалось (%.12f, %.12f)", name, got.X, got.Y, want.X, want.Y)
	}
	// Направления сравниваются по кратчайшей дуге: 179° и -181° — одно и то же.
	dh := math.Mod(got.Heading-want.Heading+3*math.Pi, 2*math.Pi) - math.Pi
	if math.Abs(dh) > tolHeading {
		t.Errorf("%s: направление %.15f рад, ожидалось %.15f рад (расхождение %.3e)", name, got.Heading, want.Heading, dh)
	}
}

// Прямая: сдвиг ровно на длину в направлении курса, курс не меняется.
func TestStraight(t *testing.T) {
	cases := []struct {
		name   string
		start  geom.Pose
		length float64
		want   geom.Pose
	}{
		{"на восток", geom.Pose{}, 100, geom.Pose{X: 100, Y: 0}},
		{"на север", geom.Pose{Heading: math.Pi / 2}, 40, geom.Pose{X: 0, Y: 40, Heading: math.Pi / 2}},
		{"на запад со смещением", geom.Pose{X: 10, Y: -5, Heading: math.Pi}, 25, geom.Pose{X: -15, Y: -5, Heading: math.Pi}},
		{"под 45°", geom.Pose{Heading: math.Pi / 4}, math.Sqrt2, geom.Pose{X: 1, Y: 1, Heading: math.Pi / 4}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assertPose(t, c.name, straight(t, c.length).End(c.start), c.want)
		})
	}
}

// Дуга: четверти и половины оборота, центр в очевидной точке.
//
// Проверка ведётся от центра: при повороте влево на 90° из (0,0) курсом на
// восток центр лежит в (0,R), конец — в (R,R), курс — на север.
func TestArcQuarterAndHalfTurns(t *testing.T) {
	cases := []struct {
		name   string
		start  geom.Pose
		radius float64
		angle  float64
		want   geom.Pose
	}{
		{"влево на 90°", geom.Pose{}, 100, math.Pi / 2, geom.Pose{X: 100, Y: 100, Heading: math.Pi / 2}},
		{"вправо на 90°", geom.Pose{}, 100, -math.Pi / 2, geom.Pose{X: 100, Y: -100, Heading: -math.Pi / 2}},
		{"влево на 180°", geom.Pose{}, 60, math.Pi, geom.Pose{X: 0, Y: 120, Heading: math.Pi}},
		{"вправо на 180°", geom.Pose{}, 60, -math.Pi, geom.Pose{X: 0, Y: -120, Heading: -math.Pi}},
		// Старт курсом на север, поворот влево на 180°: центр в (-50,0),
		// конец — в (-100,0), курс — на юг.
		{"с севера влево на 180°", geom.Pose{Heading: math.Pi / 2}, 50, math.Pi, geom.Pose{X: -100, Y: 0, Heading: -math.Pi / 2}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assertPose(t, c.name, arc(t, c.radius, c.angle).End(c.start), c.want)
		})
	}
}

// Точка в середине дуги: 45° из (0,0) курсом на восток, R=100, центр (0,100).
// Конец = центр + 100*(sin45°, -cos45°).
func TestArcMidpoint(t *testing.T) {
	a := arc(t, 100, math.Pi/2)
	half := m(100 * math.Pi / 4) // половина длины дуги
	s := math.Sqrt2 / 2
	assertPose(t, "середина дуги", a.Advance(geom.Pose{}, half), geom.Pose{
		X:       100 * s,
		Y:       100 - 100*s,
		Heading: math.Pi / 4,
	})
}

// Длина дуги выводится из радиуса и угла, а не задаётся отдельно.
func TestArcLength(t *testing.T) {
	a := arc(t, 200, math.Pi/2)
	want := m(200 * math.Pi / 2)
	if a.Length != want {
		t.Errorf("длина дуги %s, ожидалось %s", a.Length, want)
	}
}

// Составная цепочка: прямая 100 на восток, затем поворот влево на 90° радиусом
// 100. Центр второй дуги — (100,100), конец — (200,100) курсом на север.
func TestChainCompositeCurve(t *testing.T) {
	chain := geom.Chain{straight(t, 100), arc(t, 100, math.Pi/2)}

	if got, want := chain.Length(), m(100)+m(100*math.Pi/2); got != want {
		t.Errorf("длина цепочки %s, ожидалось %s", got, want)
	}
	assertPose(t, "конец цепочки", chain.End(geom.Pose{}), geom.Pose{X: 200, Y: 100, Heading: math.Pi / 2})
}

// Смещение ровно на стыке звеньев: обе стороны обязаны дать одну позу.
// Это тот случай, где ошибка на единицу прячется дольше всего.
func TestChainPoseAtJoint(t *testing.T) {
	first := straight(t, 100)
	chain := geom.Chain{first, arc(t, 100, math.Pi/2)}

	atJoint, err := chain.PoseAt(geom.Pose{}, first.Length)
	if err != nil {
		t.Fatalf("PoseAt на стыке: %v", err)
	}
	assertPose(t, "стык", atJoint, geom.Pose{X: 100, Y: 0, Heading: 0})

	// Микрометр до стыка и микрометр после — непрерывность, а не скачок.
	before, err := chain.PoseAt(geom.Pose{}, first.Length-units.Micrometer)
	if err != nil {
		t.Fatalf("PoseAt перед стыком: %v", err)
	}
	after, err := chain.PoseAt(geom.Pose{}, first.Length+units.Micrometer)
	if err != nil {
		t.Fatalf("PoseAt после стыка: %v", err)
	}
	if gap := math.Hypot(after.X-before.X, after.Y-before.Y); gap > 1e-5 {
		t.Errorf("разрыв на стыке: %.9f м между точками в микрометре друг от друга", gap)
	}
}

// Начало и конец цепочки.
func TestChainPoseAtEnds(t *testing.T) {
	chain := geom.Chain{straight(t, 100), arc(t, 100, math.Pi/2)}

	begin, err := chain.PoseAt(geom.Pose{}, 0)
	if err != nil {
		t.Fatalf("PoseAt(0): %v", err)
	}
	assertPose(t, "начало", begin, geom.Pose{})

	end, err := chain.PoseAt(geom.Pose{}, chain.Length())
	if err != nil {
		t.Fatalf("PoseAt(длина): %v", err)
	}
	assertPose(t, "конец", end, geom.Pose{X: 200, Y: 100, Heading: math.Pi / 2})
}

// Выход за пределы — отказ, а не обрезание до границы. Молчаливое приведение
// спрятало бы ошибку до момента, когда поезд окажется не там, где считает движок.
func TestChainPoseAtOutOfRange(t *testing.T) {
	chain := geom.Chain{straight(t, 100)}

	if _, err := chain.PoseAt(geom.Pose{}, -units.Micrometer); err == nil {
		t.Error("отрицательное смещение принято, ожидался отказ")
	}
	if _, err := chain.PoseAt(geom.Pose{}, chain.Length()+units.Micrometer); err == nil {
		t.Error("смещение за длиной принято, ожидался отказ")
	}
}

// Разрыв направления около ±π: 135° плюс поворот влево на 90° даёт 225°,
// то есть -135° после приведения. Значение обязано остаться в (-π, π].
func TestHeadingWrapAtPi(t *testing.T) {
	got := arc(t, 100, math.Pi/2).End(geom.Pose{Heading: 3 * math.Pi / 4})
	if got.Heading > math.Pi || got.Heading <= -math.Pi {
		t.Errorf("направление %.15f вне (-π, π]", got.Heading)
	}
	assertPose(t, "переход через π", got, geom.Pose{
		// Центр лежит в 100 м слева от курса 135°, то есть по азимуту 225°:
		// (-70.7107, -70.7107). Конец = центр + 100*(sin225°, -cos225°) =
		// (-141.4214, 0).
		X:       -100 * math.Sqrt2,
		Y:       0,
		Heading: -3 * math.Pi / 4,
	})
}

// Очень короткий сегмент: один микрометр не должен ни вырождаться, ни терять
// направление.
func TestVeryShortSegment(t *testing.T) {
	chain := geom.Chain{straight(t, 0.000001)}
	if chain.Length() != units.Micrometer {
		t.Fatalf("длина %s, ожидался микрометр", chain.Length())
	}
	end := chain.End(geom.Pose{Heading: math.Pi / 2})
	assertPose(t, "микрометровый сегмент", end, geom.Pose{X: 0, Y: 0.000001, Heading: math.Pi / 2})
}

// Оба направления обхода: пройти цепочку с конца назад — то же место,
// курс развёрнут на 180°.
func TestReverseTraversal(t *testing.T) {
	chain := geom.Chain{straight(t, 100), arc(t, 100, math.Pi/2)}
	const offset = 150 * units.Meter

	forward, err := chain.PoseAt(geom.Pose{}, offset)
	if err != nil {
		t.Fatalf("PoseAt: %v", err)
	}
	back := geom.Reverse(geom.Reverse(forward))
	assertPose(t, "двойной разворот", back, forward)

	rev := geom.Reverse(forward)
	if math.Abs(math.Abs(rev.Heading-forward.Heading)-math.Pi) > tolHeading {
		t.Errorf("разворот дал %.15f против %.15f, ожидалась разница π", rev.Heading, forward.Heading)
	}
	if rev.X != forward.X || rev.Y != forward.Y {
		t.Error("разворот сдвинул точку")
	}
}

// Отказы конструкторов: вырожденные примитивы не должны молча появляться.
func TestPrimitiveValidation(t *testing.T) {
	if _, err := geom.Straight(0); err == nil {
		t.Error("прямая нулевой длины принята")
	}
	if _, err := geom.Straight(-units.Meter); err == nil {
		t.Error("прямая отрицательной длины принята")
	}
	if _, err := geom.Arc(0, math.Pi/2); err == nil {
		t.Error("дуга нулевого радиуса принята")
	}
	if _, err := geom.Arc(-100, math.Pi/2); err == nil {
		t.Error("дуга отрицательного радиуса принята")
	}
	if _, err := geom.Arc(100, 0); err == nil {
		t.Error("дуга нулевого угла принята")
	}
	if _, err := geom.Arc(100, math.NaN()); err == nil {
		t.Error("дуга с NaN принята")
	}
	if _, err := geom.Arc(100, 7); err == nil {
		t.Error("дуга больше полного оборота принята")
	}
}

func TestComposeInvertRoundTrip(t *testing.T) {
	for _, p := range []geom.Pose{
		{X: 100, Y: -50, Heading: 0.7},
		{X: -3, Y: 0.5, Heading: -3.0},
		{X: 0, Y: 0, Heading: 0},
	} {
		got := geom.Compose(p, geom.Invert(p))
		if math.Abs(got.X) > 1e-9 || math.Abs(got.Y) > 1e-9 || math.Abs(got.Heading) > 1e-9 {
			t.Fatalf("Compose(%v, Invert(%v)) = %v, ожидалась нулевая поза", p, p, got)
		}
	}
}

// TestChainIsRigidMotion — то свойство, ради которого Compose существует:
// цепочка целиком есть одно движение, поэтому её можно применить и отменить,
// не разбирая на звенья.
func TestChainIsRigidMotion(t *testing.T) {
	a, err := geom.Straight(300 * units.Meter)
	if err != nil {
		t.Fatal(err)
	}
	b, err := geom.Arc(300, -0.1107)
	if err != nil {
		t.Fatal(err)
	}
	c, err := geom.Arc(300, 0.1107)
	if err != nil {
		t.Fatal(err)
	}
	chain := geom.Chain{a, b, c}
	start := geom.Pose{X: 17, Y: -4, Heading: 0.35}

	delta := chain.End(geom.Pose{})
	direct := chain.End(start)
	viaCompose := geom.Compose(start, delta)
	if math.Abs(direct.X-viaCompose.X) > 1e-9 ||
		math.Abs(direct.Y-viaCompose.Y) > 1e-9 ||
		math.Abs(direct.Heading-viaCompose.Heading) > 1e-9 {
		t.Fatalf("цепочка не жёсткое движение: прямо %v, через Compose %v", direct, viaCompose)
	}
	// И обратно: по концу восстанавливается начало.
	back := geom.Compose(direct, geom.Invert(delta))
	if math.Abs(back.X-start.X) > 1e-9 || math.Abs(back.Y-start.Y) > 1e-9 {
		t.Fatalf("восстановленное начало %v, ожидалось %v", back, start)
	}
}
