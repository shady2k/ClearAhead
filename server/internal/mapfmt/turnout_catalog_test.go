package mapfmt_test

import (
	"math"
	"strings"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/seedmap"
)

// turnout_catalog_test.go — ПРОВЕРКИ КАТАЛОГА ПРОЕКТОВ.
//
// # Почему они здесь, а не в валидации карты
//
// До 2026-08-17 проекты лежали в карте, и портились те же числа через фикстуру:
// seedmap.Station(withTurnoutType(...)). Каталог уехал в код сервера, и порча
// карты до него больше не достаёт — портить надо КОПИЮ ЗАПИСИ, а спрашивать
// правила у mapfmt.ValidateTurnoutProject.
//
// Разница не в форме, а в том, чья это ошибка: раньше — автора карты, теперь —
// того, кто правит каталог. Ловится она поэтому сборкой, а не входом.

func project(t *testing.T) mapfmt.TurnoutType {
	t.Helper()
	p, err := mapfmt.TurnoutProjectByID("r65-1-9-2434")
	if err != nil {
		t.Fatalf("проект Р65 1/9 не найден в каталоге: %v", err)
	}
	return p
}

// Каждая запись каталога отвечает собственным правилам. Проверяется ВЕСЬ
// каталог, а не одна знакомая запись: новый проект, добавленный с опечаткой,
// обязан падать здесь, а не в кадре.
func TestCatalogProjectsHoldTheirOwnRules(t *testing.T) {
	ids := mapfmt.TurnoutProjectIDs()
	if len(ids) == 0 {
		t.Fatal("каталог пуст: карта со стрелкой не построится ни одна")
	}
	for _, id := range ids {
		p, err := mapfmt.TurnoutProjectByID(id)
		if err != nil {
			t.Fatalf("проект %s перечислен, но не разрешается: %v", id, err)
		}
		if err := mapfmt.ValidateTurnoutProject(p); err != nil {
			t.Errorf("проект %s: %v", id, err)
		}
	}
}

func TestCatalogRejectsUnknownProject(t *testing.T) {
	_, err := mapfmt.TurnoutProjectByID("нет такого проекта")
	if err == nil {
		t.Fatal("несуществующий проект разрешился")
	}
	if !strings.Contains(err.Error(), "не найден в каталоге") {
		t.Fatalf("отказ пришёл не по той причине: %v", err)
	}
}

// Числа остряка и крестовинного комплекта проверяются диапазоном и
// согласованностью между собой.
//
// Ход шире трёх ширин головки, раструб уже желоба, нитка короче своих отгибов —
// не диапазонные проверки, и это важно: каждое число порознь законно. Первое
// даёт разрыв пути с провалом колеса, второе — воронку, сужающуюся навстречу
// колесу, третье — контррельс, состоящий из одних отгибов.
func TestCatalogRejectsImpossibleNumbers(t *testing.T) {
	cases := []struct {
		name    string
		corrupt func(*mapfmt.TurnoutType)
		reason  string
	}{
		{"остряк короче метра", func(p *mapfmt.TurnoutType) { p.Switch.BladeLengthStraight = 0.5 }, "blade_length_straight"},
		{"кривой остряк длиннее двадцати метров", func(p *mapfmt.TurnoutType) { p.Switch.BladeLengthDiverging = 25 }, "blade_length_diverging"},
		{"ход остряка ничтожен", func(p *mapfmt.TurnoutType) { p.Switch.Throw = 0.001 }, "switch.throw"},
		{"желоб ничтожен", func(p *mapfmt.TurnoutType) { p.FrogSet.Flangeway = 0.001 }, "frog_set.flangeway"},
		{"желоб контррельса огромен", func(p *mapfmt.TurnoutType) { p.FrogSet.CheckFlangeway = 0.5 }, "frog_set.check_flangeway"},
		{"сердечник длиной с перегон", func(p *mapfmt.TurnoutType) { p.FrogSet.CoreLength = 50 }, "frog_set.core_length"},
		{"усовик начинается в самой точке", func(p *mapfmt.TurnoutType) { p.FrogSet.WingApproach = 0 }, "frog_set.wing_approach"},
		{"отгиб длиннее контррельса", func(p *mapfmt.TurnoutType) { p.FrogSet.Flare = 1.9 }, "не помещаются"},
		{"раструб уже желоба", func(p *mapfmt.TurnoutType) { p.FrogSet.FlareGap = 0.03 }, "не шире рабочего желоба"},
		{"марки нет вовсе", func(p *mapfmt.TurnoutType) { p.Frog = "" }, "не указана марка"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := project(t)
			c.corrupt(&p)
			err := mapfmt.ValidateTurnoutProject(p)
			if err == nil {
				t.Fatalf("испорченный проект принят")
			}
			if !strings.Contains(err.Error(), c.reason) {
				t.Fatalf("отказ пришёл не по той причине: %v", err)
			}
		})
	}
}

// НАЧАЛЬНЫЙ УГОЛ И КРИВЫЕ ВМЕСТЕ ДАЮТ МАРКУ.
//
// Правило переехало сюда из валидации карты (checkTurnoutTurnAgreement), где
// сверяло каталог карты с её же geometry.turnouts. Источник теперь один, и
// проверяется не согласие источников, а внутренняя правда записи: марка
// объявлена дробью и обязана быть тем углом, который перевод набирает.
func TestCatalogPassagesMakeTheirFrogNumber(t *testing.T) {
	p := project(t)
	p.Passages.Diverging[1].Angle *= 1.05
	err := mapfmt.ValidateTurnoutProject(p)
	if err == nil {
		t.Fatal("проект, не доходящий до своей марки, принят")
	}
	if !strings.Contains(err.Error(), "не под своей маркой") {
		t.Fatalf("отказ пришёл не по той причине: %v", err)
	}
}

// ЗАДНИЙ СТЫК ПЕРЕВОДА — ОДИН ПОПЕРЕЧНИК НА ОБА ПРОХОДА.
//
// Замер, не заложенный никем: боковой проход проекта 2434, пройденный по эпюре,
// кончается в 27.5040 м по оси прямого — ровно там же, где прямой (27.504 м),
// и в 2.1335 м вбок. Совпадение до десятых миллиметра при том, что длина
// прямого прохода ВЫВЕДЕНА из a и b эпюры, а боковой набирает её двумя дугами и
// вставкой.
//
// Тест закрепляет свойство, а не число: разойдись они — задний стык перевода
// стал бы косым, и рельсы за крестовиной начинались бы на разных поперечниках.
func TestCatalogPassagesEndOnTheSameCross(t *testing.T) {
	p := project(t)
	straight, diverging, err := p.Alignments(mapfmt.HandRight)
	if err != nil {
		t.Fatalf("выравнивания правой стрелки: %v", err)
	}
	var want float64
	for _, h := range straight.Horizontal {
		want += h.Length
	}
	// Начальный курс — со знаком РУКОСТИ, как и дуги: правая стрелка выходит из
	// острия под −β0. Возьми тест модуль — он мерил бы не эту стрелку.
	x, _, _ := walkChain(diverging.Horizontal, -p.Switch.InitialAngle)
	// Допуск — миллиметр: эпюра записана до десятых миллиметра, и требовать
	// точнее значило бы проверять разрядность записи, а не геометрию.
	if math.Abs(x-want) > 1e-3 {
		t.Fatalf("боковой проход кончается в %.4f м по оси прямого, а прямой длиной %.4f м", x, want)
	}
}

// Рукость ПОДПИСЫВАЕТ углы, а каталог остаётся нетронутым.
//
// Вторая половина важнее первой: каталог один на весь процесс, и подписанная в
// нём дуга досталась бы следующей стрелке уже со знаком — левая стрелка сделала
// бы левой каждую следующую правую.
func TestPassagesAreSignedByHandWithoutTouchingCatalog(t *testing.T) {
	p := project(t)
	right, rightDiv, err := p.Alignments(mapfmt.HandRight)
	if err != nil {
		t.Fatalf("правая: %v", err)
	}
	_, leftDiv, err := p.Alignments(mapfmt.HandLeft)
	if err != nil {
		t.Fatalf("левая: %v", err)
	}
	for i, h := range rightDiv.Horizontal {
		if h.Kind != "arc" {
			continue
		}
		if h.Angle >= 0 {
			t.Errorf("правая стрелка, дуга %d: угол %v, ожидался отрицательный", i, h.Angle)
		}
		if got := leftDiv.Horizontal[i].Angle; got != -h.Angle {
			t.Errorf("левая стрелка, дуга %d: угол %v, ожидался %v", i, got, -h.Angle)
		}
	}
	// Прямой проход рукостью не подписывается: в нём нет дуг.
	for i, h := range right.Horizontal {
		if h.Kind != "straight" {
			t.Errorf("прямой проход, примитив %d: %s, ожидалась прямая", i, h.Kind)
		}
	}
	fresh := project(t)
	for i, h := range fresh.Passages.Diverging {
		if h.Angle < 0 {
			t.Fatalf("каталог подписан рукостью: дуга %d уехала в %v", i, h.Angle)
		}
	}
}

// walkChain — конец цепочки в плане: x вдоль начального курса, y поперёк, курс.
//
// Численно, мелким шагом, а не по формуле дуги: тест проверяет СВОЙСТВО эпюры, и
// повтори он здесь ту же тригонометрию, что и построитель, — проверял бы
// согласие формулы с собой.
func walkChain(chain []mapfmt.HPrim, heading0 float64) (x, y, heading float64) {
	heading = heading0
	const steps = 2000
	for _, h := range chain {
		switch h.Kind {
		case "arc":
			l := h.Radius * math.Abs(h.Angle)
			for i := 0; i < steps; i++ {
				x += math.Cos(heading) * l / steps
				y += math.Sin(heading) * l / steps
				heading += h.Angle / steps
			}
		default:
			x += math.Cos(heading) * h.Length
			y += math.Sin(heading) * h.Length
		}
	}
	return x, y, heading
}

// Карта, ссылающаяся мимо каталога, отвергается ПО ПРИЧИНЕ, а не по следствию.
//
// Обе проверки жили в construction_test.go, пока каталог лежал в карте и был
// частью рецепта решётки. Отказ приходит теперь из основной валидации: от
// ссылки зависит существование проходов, и стрелка без разрешимой ссылки — это
// стрелка без геометрии, а не стрелка без рецепта.
func TestMapWithoutTurnoutProjectIsRejected(t *testing.T) {
	rejects(t, seedmap.Station(seedmap.Mutate(func(m *mapfmt.Map) {
		m.Topology.Turnouts[0].TurnoutType = ""
	})), "не указан turnout_type")
}

func TestMapWithUnknownTurnoutProjectIsRejected(t *testing.T) {
	rejects(t, seedmap.Station(seedmap.Mutate(func(m *mapfmt.Map) {
		m.Topology.Turnouts[0].TurnoutType = "проекта с таким именем нет"
	})), "не найден в каталоге")
}
