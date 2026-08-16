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

func TestMatingPartsValidate(t *testing.T) {
	els, length := straightElement(t)
	a := Assembly{
		Owner: "T",
		Parts: []Part{
			{ID: "a", Kind: PartRail, Owner: "T", Element: "E", FromU: 0, ToU: 10, FaceFrom: 0.76, FaceTo: 0.76},
			{ID: "b", Kind: PartRail, Owner: "T", Element: "E", FromU: 10, ToU: length, FaceFrom: 0.76, FaceTo: 0.76},
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
			{ID: "a", Kind: PartRail, Owner: "T", Element: "E", FromU: 0, ToU: 10, FaceFrom: 0.76, FaceTo: 0.76},
			{ID: "b", Kind: PartRail, Owner: "T", Element: "E", FromU: 10, ToU: length, FaceFrom: 0.875, FaceTo: 0.875},
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
			{ID: "a", Kind: PartRail, Owner: "T", Element: "E", FromU: 0, ToU: 10, FaceFrom: 0.76, FaceTo: 0.76},
			{ID: "b", Kind: PartRail, Owner: "T", Element: "E", FromU: 12, ToU: length, FaceFrom: 0.76, FaceTo: 0.76},
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
		Parts: []Part{{ID: "a", Kind: PartRail, Owner: "T", Element: "E",
			FromU: 0, ToU: length, FaceFrom: 0.76, FaceTo: 0.76}},
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
		t.Logf("  %-52s [%s] u=%7.3f -> %-52s в %.4f м",
			b.Port.Part, b.Port.End, b.Port.U, b.Nearest.Part, b.Distance)
	}

	// Корень остряка: наружная нитка бокового прохода возобновляется на 8.3 м.
	const bladeRootU = 8.3
	var found *Break
	for i := range breaks {
		b := &breaks[i]
		if b.Port.End == PortAtStart && math.Abs(b.Port.U-bladeRootU) < 1e-6 &&
			strings.Contains(b.Port.Part, "diverging") {
			if found == nil || b.Distance < found.Distance {
				found = b
			}
		}
	}
	if found == nil {
		t.Fatal("несмыкание на корне остряка не найдено — проверка смотрит не туда")
	}
	// Замер, а не круглое число. Расстояние ПРОСТРАНСТВЕННОЕ, и потому больше
	// поперечного расхождения граней (0.1145 м): у дуги и хорды на одном u концы
	// разнесены ещё и вдоль пути. Независимо посчитано codex — 0.11643 м.
	const wantGap = 0.1166
	if math.Abs(found.Distance-wantGap) > 5e-4 {
		t.Fatalf("зазор на корне остряка %.4f м, ожидался %.4f м", found.Distance, wantGap)
	}
}
