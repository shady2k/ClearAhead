package track

import (
	"math"
	"strings"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/seedmap"
)

// Ось для рукотворных сборок: прямая, чтобы вынос по левой нормали читался
// глазами и ошибка теста не пряталась за кривой.
//
// Возвращается ДЛИНА, и рукотворные детали обязаны покрыть элемент целиком:
// иначе последний порт окажется внутренним, и тест провалится по своей же
// недоделке, а не по тому, что проверяет.
func straightElement(t *testing.T) (map[string]Element, float64) {
	t.Helper()
	m := seedmap.Station(seedmap.WithTerrain())
	_, els, err := Propagate(m)
	if err != nil {
		t.Fatalf("распространение поз: %v", err)
	}
	for _, el := range els {
		if len(el.Plan) == 1 && el.Plan.Length().Meters() >= 40 {
			return map[string]Element{"E": el}, el.Prof.LengthU().Meters()
		}
	}
	t.Fatal("в затравке нет прямого элемента длиннее 40 м")
	return nil, 0
}

// railPart — рукотворная нитка с НАСТОЯЩИМ телом.
//
// Сечение затравки: от −0.0375 до +0.1125 от рабочей грани, то есть 0.15 м.
// Без тела вопрос «продолжен ли металл» задавать не о чем, и вырожденная деталь
// в тесте прятала бы именно то, что проверяется.
func railPart(id string, from, to, face float64) Part {
	return Part{
		ID: id, Kind: PartRail, Owner: "T", Element: "E",
		FromU: from, ToU: to, FaceFrom: face, FaceTo: face,
		Grow: math.Copysign(1, face), Near: -0.0375, Far: 0.1125,
		ScaleFrom: 1, ScaleTo: 1,
	}
}

func TestMatingPartsValidate(t *testing.T) {
	els, length := straightElement(t)
	a := Assembly{
		Owner: "T",
		Parts: []Part{
			railPart("a", 0, 10, 0.76),
			railPart("b", 10, length, 0.76),
		},
	}
	if _, err := Validate(a, els); err != nil {
		t.Fatalf("сомкнутые встык детали обязаны пройти: %v", err)
	}
}

// Деталь, возобновляющаяся В СТОРОНЕ от предыдущей, — ровно та форма дефекта,
// что стоит на корне остряка. Проверка обязана видеть её ПО ПОЛОЖЕНИЮ ГРАНИ, а
// не по совпадению координаты вдоль пути: u у обеих одинаков.
func TestSidewaysJumpRefused(t *testing.T) {
	els, length := straightElement(t)
	a := Assembly{
		Owner: "T",
		Parts: []Part{
			railPart("a", 0, 10, 0.76),
			railPart("b", 10, length, 0.875),
		},
	}
	_, err := Validate(a, els)
	if err == nil {
		t.Fatal("рельс, начинающийся в 0.115 м от предыдущего, обязан получить отказ")
	}
	if !strings.Contains(err.Error(), "0.1150") {
		t.Fatalf("отказ обязан называть расстояние; получено: %v", err)
	}
}

// Объявленный разрыв проверку проходит: желоб и изолирующий стык — намерение, а
// не дефект. Без этой ветки модель звала бы дефектом сам перевод.
func TestDeclaredGapPasses(t *testing.T) {
	els, length := straightElement(t)
	a := Assembly{
		Owner: "T",
		Parts: []Part{
			railPart("a", 0, 10, 0.76),
			railPart("b", 12, length, 0.76),
		},
		Gaps: []Gap{{Kind: "insulated", Element: "E", Face: 0.76, From: 10, To: 12, Why: "изолирующий стык"}},
	}
	if _, err := Validate(a, els); err != nil {
		t.Fatalf("объявленный разрыв обязан пройти: %v", err)
	}
}

// Порт на КОНЦЕ элемента свободен законно: там кончается ребро, и что дальше —
// вопрос топологии сети, а не сборки устройства.
func TestElementEndPortMayBeFree(t *testing.T) {
	els, length := straightElement(t)
	a := Assembly{
		Owner: "T",
		Parts: []Part{railPart("a", 0, length, 0.76)},
	}
	if _, err := Validate(a, els); err != nil {
		t.Fatalf("деталь во всю длину элемента обязана пройти: %v", err)
	}
}

// ГЛАВНЫЙ ТЕСТ ШАГА: нынешняя модель стрелки не смыкается, и это видно в Go.
//
// Дефект известен и померен вручную: у бокового прохода вырезаны обе нитки на
// длину остряка (8.3 м), а остряк дан один. Наружная нитка возобновляется в
// 0.1145 м от рамного рельса — при головке 0.075 м это 39.5 мм чистого воздуха.
// До этого дня единственным способом такое увидеть был снимок экрана.
func TestSeedTurnoutAssemblyIsBroken(t *testing.T) {
	m := seedmap.Station(seedmap.WithTerrain())
	_, els, err := Propagate(m)
	if err != nil {
		t.Fatalf("распространение поз: %v", err)
	}
	_, rg, err := Compile(m)
	if err != nil {
		t.Fatalf("компиляция: %v", err)
	}
	owner := m.Topology.Turnouts[0].ID

	a, err := AssembleTurnout(rg, els, owner)
	if err != nil {
		t.Fatalf("сборка: %v", err)
	}
	if _, err := Validate(a, els); err == nil {
		t.Fatal("сборка стрелки обязана получить отказ: корень остряка не сомкнут")
	}

	breaks, err := Breaks(a, els)
	if err != nil {
		t.Fatalf("разбор несмыканий: %v", err)
	}
	t.Logf("несомкнутых портов у %s: %d", owner, len(breaks))
	for _, b := range breaks {
		t.Logf("  %-8s %-52s [%s] u=%7.3f -> %-52s в %.4f м, общего %.0f %%",
			b.Kind, b.Port.Part, b.Port.End, b.Port.U, b.Nearest.Part, b.Distance, b.Overlap*100)
	}

	// КОРЕНЬ ОСТРЯКА ЛОМАЕТСЯ С ДВУХ СТОРОН, и виды несмыкания у них разные.
	// Проверяются обе: до 2026-08-16 вторую видел только Godot, а первую не видел
	// никто.
	const bladeRootU = 8.3
	at := func(kind string, pred func(Break) bool) *Break {
		for i := range breaks {
			b := &breaks[i]
			if b.Kind == kind && math.Abs(b.Port.U-bladeRootU) < 1e-6 && pred(*b) {
				return b
			}
		}
		return nil
	}

	// НАРУЖНАЯ нитка бокового: примыкать не к чему вовсе.
	//
	// Замер, а не круглое число. Расстояние ПРОСТРАНСТВЕННОЕ, и потому больше
	// поперечного расхождения граней (0.1145 м): у дуги и хорды на одном u концы
	// разнесены ещё и вдоль пути. Независимо посчитано codex — 0.11643 м.
	open := at(BreakOpen, func(b Break) bool {
		return strings.Contains(b.Port.Part, "diverging") && strings.Contains(b.Port.Part, "-0.760")
	})
	if open == nil {
		t.Fatal("обрыв наружной нитки бокового прохода не найден — проверка смотрит не туда")
	}
	if math.Abs(open.Distance-0.1166) > 5e-4 {
		t.Fatalf("зазор на корне остряка %.4f м, ожидался 0.1166 м", open.Distance)
	}

	// ВНУТРЕННЯЯ нитка: грань сошлась ТОЧНО, а тело прыгнуло. Остряк растёт к оси,
	// нитка за корнем наружу, и общей у них остаётся ровно головка.
	//
	// Пятьдесят процентов — не оценка: сечение затравки идёт от −0.0375 до
	// +0.1125 от рабочей грани, то есть 0.15 м; общими оказываются 0.075 м.
	// Ровно это же число выдаёт проверка мешей 44_mesh_shell.gd, резавшая
	// треугольники плоскостью, — и в том, что оба способа сошлись, весь смысл
	// переезда.
	step := at(BreakStep, func(b Break) bool { return strings.Contains(b.Port.Part, PartBlade) })
	if step == nil {
		t.Fatal("прыжок тела на корне остряка не найден — сечение до проверки не доехало")
	}
	if step.Distance > RunningFaceTol {
		t.Fatalf("грань обязана была сойтись точно, разошлась на %.4f м", step.Distance)
	}
	if math.Abs(step.Overlap-0.5) > 1e-3 {
		t.Fatalf("общего сечения %.1f %%, ожидалось 50 %%", step.Overlap*100)
	}
}
