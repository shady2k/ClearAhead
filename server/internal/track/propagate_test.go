package track

import (
	"math"
	"strings"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/seedmap"
)

// valid — карта, которая обязана остаться валидной после правки.
//
// Прежний loadMap разбирал JSON и валидировал каждую фикстуру; разбора больше
// нет, а валидация нужна по-прежнему: правка, случайно сделавшая карту
// негодной, обесценила бы тест, который её берёт, и падал бы он не там и не про
// то. Карты фабрики без правок проверяет сама фабрика.
func valid(t *testing.T, m *mapfmt.Map) *mapfmt.Map {
	t.Helper()
	if err := mapfmt.Validate(m); err != nil {
		t.Fatalf("фикстура невалидна: %v", err)
	}
	return m
}

// TestPropagateChain — поза переносится вдоль элемента, а высота остаётся
// якорной: профиля у перегона нет, значит z обязан дойти до дальнего конца
// неизменным. Heading на дальнем конце смотрит ВНУТРЬ элемента, то есть назад.
func TestPropagateChain(t *testing.T) {
	m := seedmap.Line()
	edge := m.Topology.Edges[0]
	anchor := m.Anchors[edge.From]

	poses, els, err := Propagate(m)
	if err != nil {
		t.Fatalf("распространение: %v", err)
	}
	p := poses[Incidence{Port: edge.To, Element: seedmap.LineEdgeID}]
	if math.Abs(p.Plan.X-seedmap.LineLengthM) > 1e-6 || math.Abs(p.Plan.Y) > 1e-6 {
		t.Fatalf("%s в (%v, %v), ожидалось (%v, 0)", edge.To, p.Plan.X, p.Plan.Y, seedmap.LineLengthM)
	}
	if math.Abs(math.Abs(p.Plan.Heading)-math.Pi) > 1e-9 {
		t.Fatalf("heading %s = %v, ожидалось ±π", edge.To, p.Plan.Heading)
	}
	if math.Abs(p.Z-anchor.Z) > 1e-9 {
		t.Fatalf("z %s = %v, ожидалось %v (профиля нет)", edge.To, p.Z, anchor.Z)
	}
	if len(els) != 1 {
		t.Fatalf("элементов %d, ожидался 1", len(els))
	}
}

// TestPropagateChainThroughJunction — на порту, где сходятся несколько концов,
// поза переносится через порт, а не только вдоль элемента: подход станции
// кончается в общем порту стрелки, и оттуда её получают оба прохода.
func TestPropagateChainThroughJunction(t *testing.T) {
	poses, els, err := Propagate(seedmap.Station())
	if err != nil {
		t.Fatalf("распространение: %v", err)
	}
	// Подход — прямая 120 м от якоря в нуле, поэтому общий порт SW1 стоит в
	// (120, 0), а конец подхода смотрит назад.
	p := poses[Incidence{Port: seedmap.StationSW1 + ".C", Element: seedmap.StationApproach}]
	if math.Abs(p.Plan.X-120) > 1e-6 || math.Abs(p.Plan.Y) > 1e-6 {
		t.Fatalf("общий порт SW1 в (%v, %v), ожидалось (120, 0)", p.Plan.X, p.Plan.Y)
	}
	// Проход, начинающийся в том же порту, стоит там же и смотрит вперёд.
	s := poses[Incidence{Port: seedmap.StationSW1 + ".C", Element: seedmap.StationSW1 + mapfmt.PassageStraight}]
	if math.Hypot(s.Plan.X-p.Plan.X, s.Plan.Y-p.Plan.Y) > 1e-9 {
		t.Fatalf("проход и ребро разошлись в одном порту: (%v, %v) против (%v, %v)",
			s.Plan.X, s.Plan.Y, p.Plan.X, p.Plan.Y)
	}
	// Пять рёбер плюс по два прохода на каждую из двух стрелок.
	if len(els) != 9 {
		t.Fatalf("элементов %d, ожидалось 9", len(els))
	}
}

// TestPropagateRejectsUnanchored — компонента без якоря не имеет абсолютного
// положения, и вывести его неоткуда: это отказ, а не «позы по умолчанию».
func TestPropagateRejectsUnanchored(t *testing.T) {
	m := seedmap.Line(seedmap.Mutate(func(m *mapfmt.Map) {
		m.Anchors = map[string]mapfmt.Anchor{}
	}))
	if _, _, err := Propagate(m); err == nil {
		t.Fatal("ожидался отказ: компонента без якоря")
	}
}

// ringWith строит кольцо из четырёх дуг на π/2: замкнутую окружность радиуса
// 300 м, у которой у последней дуги радиус подменён на lastRadius.
//
// Фабрика такого не даёт и дать не может по существу: ни перегон, ни станция
// цикла не содержат, а без цикла невязке замыкания взяться неоткуда — каждая
// поза выводится ровно один раз. Поэтому карта здесь собирается на месте.
//
// Углы не трогаются, поэтому направление сходится точно при любом радиусе, а
// расходится только положение — так зонд бьёт ровно в допуск по положению и
// ничего не смешивает. Ошибка замыкания при подмене ΔR равна ΔR·√2.
// ringWith — кольцо из фабрики. Цикл живёт там, а не здесь: он нужен и другим
// пакетам, а фикстура, размноженная по тестам, расходится.
func ringWith(t *testing.T, lastRadius float64) *mapfmt.Map {
	t.Helper()
	return valid(t, seedmap.Ring(lastRadius))
}

// TestPropagateClosingCycle — положительный случай: кольцо, которое сходится.
//
// Без него тест на невязку бесполезен: проверка, которая отвергает всё подряд,
// тоже «ловит расхождение».
func TestPropagateClosingCycle(t *testing.T) {
	if _, _, err := Propagate(ringWith(t, 300.0)); err != nil {
		t.Fatalf("замкнутое кольцо должно приниматься, получен отказ: %v", err)
	}
}

// TestPropagateClosureWithinTolerance — кольцо с невязкой 0,7 мм принимается.
//
// Вместе с TestPropagateClosureMismatch это зонд по обе стороны границы: без
// него допуск мог бы быть нулевым, и проверка отвергала бы любую честную карту.
// ΔR = 0,5 мм даёт невязку 0,5·√2 ≈ 0,71 мм — под допуском 1 мм.
func TestPropagateClosureWithinTolerance(t *testing.T) {
	if _, _, err := Propagate(ringWith(t, 300.0005)); err != nil {
		t.Fatalf("невязка 0,71 мм под допуском 1 мм должна приниматься, получен отказ: %v", err)
	}
}

// TestPropagateClosureMismatch — кольцо с невязкой 7 мм отвергается.
//
// Первая редакция этого теста строила «треугольник» из трёх прямых одного
// направления. Он не смыкался вовсе — расхождение было в сотни метров, то есть
// тест доказывал лишь, что проверка отвергает заведомый мусор, и о допуске в
// 1 мм не говорил ничего. Найдено воркером при реализации.
//
// ΔR = 5 мм даёт невязку 5·√2 ≈ 7,07 мм — семь допусков, а не триста тысяч.
func TestPropagateClosureMismatch(t *testing.T) {
	_, _, err := Propagate(ringWith(t, 300.005))
	if err == nil {
		t.Fatal("ожидался отказ по невязке замыкания")
	}
	if !strings.Contains(err.Error(), "невязк") {
		t.Fatalf("ошибка не про невязку: %v", err)
	}
	// Число в сообщении должно быть миллиметрами того же порядка: иначе зонд
	// снова бьёт мимо границы, а мы этого не заметим.
	if !strings.Contains(err.Error(), "7.0") {
		t.Fatalf("в сообщении ожидалась невязка около 7 мм, получено: %v", err)
	}
}

// СТЫК ДВУХ ОБЫЧНЫХ РЁБЕР. Перенос позы через порт, в котором сходятся два
// ребра, — отдельная ветка: на станции все сходящиеся порты принадлежат
// стрелкам, а перегон из одного ребра стыка не имеет вовсе. Без этого теста
// ветка остаётся непокрытой, и заметить это по зелёному прогону невозможно.
func TestPropagateThroughPlainJoint(t *testing.T) {
	m := valid(t, seedmap.Corridor())
	poses, els, err := Propagate(m)
	if err != nil {
		t.Fatalf("распространение: %v", err)
	}
	if len(els) != 2 {
		t.Fatalf("элементов %d, ожидалось 2", len(els))
	}
	// Поза конца первого ребра и поза начала второго — это один и тот же порт,
	// пройденный с разных сторон: положения обязаны совпасть.
	firstEnd, ok1 := poses[Incidence{Port: seedmap.CorridorJoint, Element: seedmap.CorridorFirst}]
	secondStart, ok2 := poses[Incidence{Port: seedmap.CorridorJoint, Element: seedmap.CorridorSecond}]
	if !ok1 || !ok2 {
		t.Fatalf("позы стыка не выведены: %v %v", ok1, ok2)
	}
	if firstEnd.Plan.X != secondStart.Plan.X || firstEnd.Plan.Y != secondStart.Plan.Y {
		t.Fatalf("стык разошёлся: (%v, %v) против (%v, %v)",
			firstEnd.Plan.X, firstEnd.Plan.Y, secondStart.Plan.X, secondStart.Plan.Y)
	}
	if firstEnd.Z != secondStart.Z {
		t.Fatalf("отметки стыка разошлись: %v против %v", firstEnd.Z, secondStart.Z)
	}
}
