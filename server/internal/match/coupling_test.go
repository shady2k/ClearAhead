package match

// coupling_test.go — СЦЕПКА И РАСЦЕПКА.
//
// Проверяется не «функция вернула сцеп», а те три вещи, ради которых она
// написана: смычка узнаётся геометрией, порядок членов остаётся цепочкой, и
// перевёрнутая машина остаётся перевёрнутой.

import (
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/netloc"
	"github.com/shady2k/ClearAhead/server/internal/seedmap"
	"github.com/shady2k/ClearAhead/server/internal/track"
	"github.com/shady2k/ClearAhead/server/internal/units"
)

// Габарит машин фикстуры. Складываются в расстановке: тела ставятся вплотную,
// то есть точки отсчёта разнесены на полусумму длин.
const (
	locoLen = 32.84
	gonLen  = 13.92
)

// coupled — партия из локомотива и полувагона, стоящих ВПЛОТНУЮ на главном пути.
//
// gonDir — каким концом повёрнут вагон: прямо (его конец B смотрит на
// локомотив) или задом наперёд (конец A). Обе постановки законны и дают разный
// порядок в получившемся сцепе, поэтому фикстура их и различает.
func coupled(t *testing.T, gonDir netloc.Direction) (*Match, *track.CompiledNetwork) {
	t.Helper()
	net := station(t)
	doc := map[string]any{
		"format_version": FormatVersion,
		"region":         "ST_A",
		"units": []any{
			map[string]any{
				"id": loco1ID, "name": "LOCO_1", "type": "VL80",
				"at": map[string]any{"element": seedmap.StationMain, "u": 100.0, "direction": "forward"},
			},
			map[string]any{
				"id": loco2ID, "name": "GON_1", "type": "GON", "load": 0.0,
				"at": map[string]any{"element": seedmap.StationMain,
					"u": 100.0 + locoLen/2 + gonLen/2, "direction": string(gonDir)},
			},
		},
	}
	m, err := Start("M1", write(t, doc), net, setWithWagon(t))
	if err != nil {
		t.Fatalf("расстановка: %v", err)
	}
	return m, net
}

// СЦЕПКА ДАЁТ ЦЕПОЧКУ, а не два списка рядом.
//
// Локомотив стоит на меньших u, вагон — на больших и повёрнут прямо: значит
// вагон примкнул к концу A локомотива своим концом B, и в новом сцепе он идёт
// ПОСЛЕ локомотива, не перевернувшись.
func TestCoupleMakesOneChain(t *testing.T) {
	m, net := coupled(t, netloc.DirForward)
	got, err := m.Couple(net, loco1ID, loco2ID, "TRAIN", 0, 0)
	if err != nil {
		t.Fatalf("сцепка: %v", err)
	}
	if len(got.Members) != 2 {
		t.Fatalf("в сцепе %d членов, ожидалось два: %+v", len(got.Members), got.Members)
	}
	if got.Members[0].UnitID != loco1ID || got.Members[1].UnitID != loco2ID {
		t.Fatalf("порядок членов %+v: от конца B к концу A ожидались локомотив, затем вагон", got.Members)
	}
	if got.Members[0].Flipped || got.Members[1].Flipped {
		t.Fatalf("кто-то оказался перевёрнутым: %+v", got.Members)
	}
	// ПРЕЖНИХ СЦЕПОВ БОЛЬШЕ НЕТ: сцеп это связь, и после сцепки прежних связей
	// не существует. Оставь их — и следующий тик посчитал бы одни и те же
	// машины трижды.
	if len(m.Consists) != 1 || m.Consists[0].ID != "TRAIN" {
		t.Fatalf("в партии %d сцепов: %+v", len(m.Consists), m.Consists)
	}
	c, ok := m.ConsistOf(loco2ID)
	if !ok || c.ID != "TRAIN" {
		t.Fatalf("вагон не в новом сцепе: %+v", c)
	}
}

// ПЕРЕВЁРНУТАЯ МАШИНА ОСТАЁТСЯ ПЕРЕВЁРНУТОЙ.
//
// Вагон поставлен задом наперёд — его конец A смотрит на локомотив. В цепочке,
// читаемой от конца B к концу A, он обязан оказаться с поднятым признаком
// поворота: иначе следующий же шаг физики повезёт его в другую сторону, чем
// весь состав (sim.dirA).
func TestCoupleFlipsTheOneThatCameBackwards(t *testing.T) {
	m, net := coupled(t, netloc.DirReverse)
	got, err := m.Couple(net, loco1ID, loco2ID, "TRAIN", 0, 0)
	if err != nil {
		t.Fatalf("сцепка: %v", err)
	}
	if got.Members[1].UnitID != loco2ID {
		t.Fatalf("порядок членов %+v", got.Members)
	}
	if !got.Members[1].Flipped {
		t.Fatal("вагон, примкнувший концом A, не помечен перевёрнутым — в составе он поедет назад")
	}
	if got.Members[0].Flipped {
		t.Fatal("локомотив перевернулся, хотя его не трогали")
	}
}

// СЦЕПКА ОТ ПЕРЕМЕНЫ МЕСТ НЕ МЕНЯЕТСЯ: тот же состав, каким бы из двух его ни
// назвали первым. Иначе игрок получал бы разный поезд в зависимости от того, с
// какой машины он щёлкнул.
func TestCoupleIsSymmetric(t *testing.T) {
	m1, net1 := coupled(t, netloc.DirForward)
	a, err := m1.Couple(net1, loco1ID, loco2ID, "TRAIN", 0, 0)
	if err != nil {
		t.Fatalf("сцепка: %v", err)
	}
	m2, net2 := coupled(t, netloc.DirForward)
	b, err := m2.Couple(net2, loco2ID, loco1ID, "TRAIN", 0, 0)
	if err != nil {
		t.Fatalf("обратная сцепка: %v", err)
	}
	if len(a.Members) != len(b.Members) {
		t.Fatalf("разное число членов: %d и %d", len(a.Members), len(b.Members))
	}
	for i := range a.Members {
		if a.Members[i] != b.Members[i] {
			t.Fatalf("порядок разошёлся на %d: %+v против %+v", i, a.Members, b.Members)
		}
	}
}

// ОТКАЗЫ СЦЕПКИ. Каждый — ошибка своего рода, и каждый обязан называться своим
// текстом: молчаливое «не сцепилось» заставляет искать причину глазами.
func TestCoupleRefusals(t *testing.T) {
	t.Run("тела стоят порознь", func(t *testing.T) {
		net := station(t)
		doc := map[string]any{
			"format_version": FormatVersion,
			"region":         "ST_A",
			"units": []any{
				map[string]any{
					"id": loco1ID, "name": "LOCO_1", "type": "VL80",
					"at": map[string]any{"element": seedmap.StationMain, "u": 100.0, "direction": "forward"},
				},
				map[string]any{
					"id": loco2ID, "name": "GON_1", "type": "GON", "load": 0.0,
					// На пять метров дальше, чем нужно для смычки.
					"at": map[string]any{"element": seedmap.StationMain,
						"u": 100.0 + locoLen/2 + gonLen/2 + 5, "direction": "forward"},
				},
			},
		}
		m, err := Start("M1", write(t, doc), net, setWithWagon(t))
		if err != nil {
			t.Fatalf("расстановка: %v", err)
		}
		if _, err := m.Couple(net, loco1ID, loco2ID, "TRAIN", 0, 0); err == nil {
			t.Fatal("сцепились тела, стоящие в пяти метрах друг от друга")
		}
	})

	// СЦЕПКА НА ХОДУ БЕЗ МАСС — ОТКАЗ, А НЕ ТИХИЙ НОЛЬ. Правило «сцепляют
	// стоящие» ушло из домена к команде (см. TestCoupleOnTheMoveKeepsMomentum),
	// но безмассовый удар посчитать нельзя: молча остановленный состав выглядел
	// бы как дефект физики, а не как забытый аргумент.
	t.Run("удар без масс", func(t *testing.T) {
		m, net := coupled(t, netloc.DirForward)
		c, _ := m.ConsistOf(loco1ID)
		c.Speed = 1_000_000 // метр в секунду
		m.SetConsist(c)
		if _, err := m.Couple(net, loco1ID, loco2ID, "TRAIN", 0, 0); err == nil {
			t.Fatal("сцепка на ходу прошла с неназванными массами")
		}
	})

	t.Run("сцеп сам с собой", func(t *testing.T) {
		m, net := coupled(t, netloc.DirForward)
		if _, err := m.Couple(net, loco1ID, loco1ID, "TRAIN", 0, 0); err == nil {
			t.Fatal("сцеп сцепился сам с собой")
		}
	})

	t.Run("нет такого сцепа", func(t *testing.T) {
		m, net := coupled(t, netloc.DirForward)
		if _, err := m.Couple(net, loco1ID, "НЕТ_ТАКОГО", "TRAIN", 0, 0); err == nil {
			t.Fatal("сцепка с несуществующим сцепом прошла")
		}
	})

	t.Run("новый сцеп без имени", func(t *testing.T) {
		m, net := coupled(t, netloc.DirForward)
		if _, err := m.Couple(net, loco1ID, loco2ID, "", 0, 0); err == nil {
			t.Fatal("сцепка прошла без имени нового сцепа")
		}
	})
}

// РАСЦЕПКА ДЕЛИТ ЦЕПОЧКУ НАДВОЕ и обе части оставляет живыми сцепами.
func TestUncoupleSplitsTheChain(t *testing.T) {
	m, net := coupled(t, netloc.DirForward)
	if _, err := m.Couple(net, loco1ID, loco2ID, "TRAIN", 0, 0); err != nil {
		t.Fatalf("сцепка: %v", err)
	}
	head, tail, err := m.Uncouple("TRAIN", loco1ID, "TAIL")
	if err != nil {
		t.Fatalf("расцепка: %v", err)
	}
	if len(head.Members) != 1 || head.Members[0].UnitID != loco1ID {
		t.Fatalf("часть со стороны конца B: %+v", head.Members)
	}
	if len(tail.Members) != 1 || tail.Members[0].UnitID != loco2ID {
		t.Fatalf("часть со стороны конца A: %+v", tail.Members)
	}
	if len(m.Consists) != 2 {
		t.Fatalf("в партии %d сцепов после расцепки", len(m.Consists))
	}
	// Каждая машина ровно в одном сцепе: две записи об одной означали бы, что
	// следующий тик посчитает её дважды.
	for _, id := range []string{loco1ID, loco2ID} {
		count := 0
		for _, c := range m.Consists {
			if c.Has(id) {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("единица %s числится в %d сцепах", id, count)
		}
	}
}

// ОТКАЗЫ РАСЦЕПКИ.
func TestUncoupleRefusals(t *testing.T) {
	m, net := coupled(t, netloc.DirForward)
	if _, err := m.Couple(net, loco1ID, loco2ID, "TRAIN", 0, 0); err != nil {
		t.Fatalf("сцепка: %v", err)
	}
	// За последней единицей расцеплять нечего: это не пустая часть, а команда,
	// которая ничего не значит.
	if _, _, err := m.Uncouple("TRAIN", loco2ID, "TAIL"); err == nil {
		t.Fatal("расцепка за последней единицей прошла")
	}
	if _, _, err := m.Uncouple("TRAIN", "НЕТ_ТАКОЙ", "TAIL"); err == nil {
		t.Fatal("расцепка по чужой единице прошла")
	}
	if _, _, err := m.Uncouple("НЕТ_ТАКОГО", loco1ID, "TAIL"); err == nil {
		t.Fatal("расцепка несуществующего сцепа прошла")
	}
	if _, _, err := m.Uncouple("TRAIN", loco1ID, ""); err == nil {
		t.Fatal("расцепка прошла без имени новой части")
	}
}

// СЦЕПКА НА ХОДУ СОХРАНЯЕТ КОЛИЧЕСТВО ДВИЖЕНИЯ.
//
// Это и есть автосцепка: локомотив налетел на стоящий вагон, и дальше они идут
// одним телом — медленнее локомотива и быстрее вагона. Число проверяется
// СЧЁТОМ ОТДЕЛЬНО ОТ КОДА: 192 т на метре в секунду против 94 т в покое дают
// 192/(192+94) = 0.6713… м/с. Проверка вида «вернулось то, что вернулось»
// прошла бы и на потерянной массе.
func TestCoupleOnTheMoveKeepsMomentum(t *testing.T) {
	m, net := coupled(t, netloc.DirForward)
	c, _ := m.ConsistOf(loco1ID)
	c.Speed = 1_000_000 // метр в секунду
	m.SetConsist(c)

	const locoMass = 192_000 // кг, паспорт ВЛ80
	const gonMass = 94_000   // кг, гружёный полувагон фикстуры: тара 24 плюс 70
	got, err := m.Couple(net, loco1ID, loco2ID, "TRAIN", locoMass, gonMass)
	if err != nil {
		t.Fatalf("сцепка на ходу: %v", err)
	}
	want := units.Speed(1_000_000 * locoMass / (locoMass + gonMass))
	if diff := got.Speed - want; diff > 1 || diff < -1 {
		t.Fatalf("после удара сцеп идёт %d мкм/с, посчитано %d", got.Speed, want)
	}
}

// УДАР СЧИТАЕТСЯ В СИСТЕМЕ ОТСЧЁТА НОВОГО СЦЕПА, а не каждого прежнего в своей.
//
// Вагон стоит задом наперёд: в цепочке он перевернётся, и вместе с порядком
// обязан перевернуться знак ЕГО скорости. Пусти его сюда как есть — и вагон,
// катящийся навстречу локомотиву, сложился бы с ним как догоняющий.
func TestImpactSpeaksInTheNewConsistFrame(t *testing.T) {
	m, net := coupled(t, netloc.DirReverse)
	// Оба идут навстречу друг другу с одинаковой скоростью по своим осям: у
	// перевёрнутого вагона ход B → A направлен на локомотив.
	for _, id := range []string{loco1ID, loco2ID} {
		c, _ := m.ConsistOf(id)
		c.Speed = 1_000_000
		m.SetConsist(c)
	}
	got, err := m.Couple(net, loco1ID, loco2ID, "TRAIN", 100_000, 100_000)
	if err != nil {
		t.Fatalf("сцепка: %v", err)
	}
	// Равные массы, встречные ходы, одинаковые модули — сцеп обязан встать.
	if got.Speed != 0 {
		t.Fatalf("встречный удар равных масс дал %d мкм/с, ожидался покой", got.Speed)
	}
}

// ИМЯ СЦЕПА ОТ УДАРА ВЫВОДИТСЯ, А НЕ ВЫДАЁТСЯ, и не зависит от порядка.
//
// Кто в кого въехал — вопрос к физике, а не к тождеству получившегося тела:
// два прогона одной расстановки обязаны назвать его одинаково, иначе
// канонический хеш состояния разошёлся бы между загрузками одного мира.
func TestAutoConsistIDIsDerivedAndOrderFree(t *testing.T) {
	a, b := AutoConsistID(loco1ID, loco2ID), AutoConsistID(loco2ID, loco1ID)
	if a != b {
		t.Fatalf("порядок изменил имя: %s против %s", a, b)
	}
	if a == AutoConsistID(loco1ID, "ДРУГОЙ") {
		t.Fatal("разные пары дали одно имя")
	}
	if err := mapfmt.ValidID("сцеп", a); err != nil {
		t.Fatalf("выведенное имя не проходит проверку идентификатора: %v", err)
	}
}
