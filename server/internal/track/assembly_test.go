package track

import (
	"math"
	"strings"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/seedmap"
	"github.com/shady2k/ClearAhead/server/internal/units"
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
		Grow: math.Copysign(1, face), Near: -0.0375, Far: 0.1125, Head: 0.075,
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
	if _, err := Validate(a, els); err == nil {
		t.Fatal("рельс, начинающийся в 0.115 м от предыдущего, обязан получить отказ")
	}
	// Проверяется ЧИСЛО, а не текст отказа: слова меняются при первой же правке
	// формулировки, а доля общего сечения — свойство модели.
	//
	// Сечение 0.15 м, сдвиг 0.115 м, значит общими остаются 0.035 м — 23 %.
	breaks, err := Breaks(a, els)
	if err != nil {
		t.Fatalf("разбор несмыканий: %v", err)
	}
	var found bool
	for _, b := range breaks {
		if b.Kind == BreakStep && math.Abs(b.Overlap-0.035/0.15) < 1e-3 {
			found = true
		}
	}
	if !found {
		t.Fatalf("прыжок тела на 0.115 м не найден среди %d несмыканий", len(breaks))
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

// СБОРКА СТРЕЛКИ: корень остряка СОМКНУТ, наружная нитка бокового — нет.
//
// # Что здесь было и чем кончилось
//
// Тест заведён 2026-08-16 как доказательство ДВУХ дефектов корня, которые до
// него видел только снимок экрана:
//
//	step — грань сходилась точно, а тела расходились: остряк рос к оси, нитка за
//	       корнем наружу, общего у них оставалась ровно головка (50 %);
//	open — наружная нитка бокового прохода возникала в стороне от всякого металла.
//
// ПЕРВЫЙ ЗАКРЫТ, и закрыт не подгонкой стороны роста, а профилем: остряк катают
// из острякового ОР65, который ниже Р65 на 40 мм и лежит на подушке в ту же
// толщину. Подошвы расходятся ПО ВЫСОТЕ, и потому телу остряка можно расти
// наружу, как всякому рельсу. В корне его 132 мм целиком укладываются в 150 мм
// закорневой нитки — это и есть стык.
//
// ВТОРОЙ ОТКРЫТ и держится не данными, а моделью: у настоящего перевода наружная
// нить бокового пути — это КРИВОЙ РАМНЫЙ РЕЛЬС, продолженный наружным
// соединительным рельсом, то есть ОДИН физический рельс на два маршрута. Part
// несёт ровно один Element, и выразить это нечем (ClearAhead-ax7m.3).
func TestSeedTurnoutRootMatesButOuterThreadDoesNot(t *testing.T) {
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
	breaks, err := Breaks(a, els)
	if err != nil {
		t.Fatalf("разбор несмыканий: %v", err)
	}
	for _, b := range breaks {
		t.Logf("  %-8s %-52s [%s] u=%7.3f -> %-52s в %.4f м, общего %.0f %%",
			b.Kind, b.Port.Part, b.Port.End, b.Port.U, b.Nearest.Part, b.Distance, b.Overlap*100)
	}

	// КОРЕНЬ ОСТРЯКА СОМКНУТ. Утверждение положительное, и это важнее отказа:
	// оно сторожит починку от отката. Ни один прыжок тела на корне не допускается
	// ни у одного прохода.
	bladeRootU := m.Construction.TurnoutTypes[0].Switch.BladeLengthDiverging
	for _, b := range breaks {
		if b.Kind == BreakStep && math.Abs(b.Port.U-bladeRootU) < 1e-6 {
			t.Fatalf("корень остряка снова разъехался: %s общего %.0f %%", b.Port.Part, b.Overlap*100)
		}
	}

	// НАРУЖНАЯ НИТКА — единственное, что осталось. Ровно одно несмыкание: больше
	// значило бы новую поломку, меньше — что проверка ослепла.
	if len(breaks) != 1 || breaks[0].Kind != BreakOpen {
		t.Fatalf("ожидалось ровно одно несмыкание вида open, получено %d: %+v", len(breaks), breaks)
	}
	open := breaks[0]
	if !strings.Contains(open.Port.Part, "-0.760") {
		t.Fatalf("несомкнута не наружная нитка бокового прохода, а %s", open.Port.Part)
	}

	// Зазор сверяется С САМОЙ ГЕОМЕТРИЕЙ: у бокового прохода начальный угол
	// остряка и две дуги разного радиуса, и всякая замкнутая формула была бы
	// третьим описанием той же кривой.
	dv := els[owner+mapfmt.PassageDiverging]
	st := els[owner+mapfmt.PassageStraight]
	du, err := units.MetersToDistance(bladeRootU)
	if err != nil {
		t.Fatalf("координата корня: %v", err)
	}
	pd, err := dv.Plan.PoseAt(dv.Start.Plan, du)
	if err != nil {
		t.Fatalf("поза бокового: %v", err)
	}
	ps, err := st.Plan.PoseAt(st.Start.Plan, du)
	if err != nil {
		t.Fatalf("поза прямого: %v", err)
	}
	wantGap := math.Hypot(pd.X-ps.X, pd.Y-ps.Y)
	if math.Abs(open.Distance-wantGap) > 2e-3 {
		t.Fatalf("зазор %.4f м, а оси проходов на %.3f м расходятся на %.4f", open.Distance, bladeRootU, wantGap)
	}
}
