package match

// coupling_test.go — СЦЕПКА И РАСЦЕПКА.
//
// Проверяется не «функция вернула сцеп», а те три вещи, ради которых она
// написана: смычка узнаётся геометрией, порядок членов остаётся цепочкой, и
// перевёрнутая машина остаётся перевёрнутой.

import (
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/netloc"
	"github.com/shady2k/ClearAhead/server/internal/seedmap"
	"github.com/shady2k/ClearAhead/server/internal/track"
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
	got, err := m.Couple(net, loco1ID, loco2ID, "TRAIN")
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
	got, err := m.Couple(net, loco1ID, loco2ID, "TRAIN")
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
	a, err := m1.Couple(net1, loco1ID, loco2ID, "TRAIN")
	if err != nil {
		t.Fatalf("сцепка: %v", err)
	}
	m2, net2 := coupled(t, netloc.DirForward)
	b, err := m2.Couple(net2, loco2ID, loco1ID, "TRAIN")
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
		if _, err := m.Couple(net, loco1ID, loco2ID, "TRAIN"); err == nil {
			t.Fatal("сцепились тела, стоящие в пяти метрах друг от друга")
		}
	})

	t.Run("сцепка на ходу", func(t *testing.T) {
		m, net := coupled(t, netloc.DirForward)
		c, _ := m.ConsistOf(loco1ID)
		c.Speed = 1_000_000 // метр в секунду
		m.SetConsist(c)
		if _, err := m.Couple(net, loco1ID, loco2ID, "TRAIN"); err == nil {
			t.Fatal("сцепка прошла на ходу")
		}
	})

	t.Run("сцеп сам с собой", func(t *testing.T) {
		m, net := coupled(t, netloc.DirForward)
		if _, err := m.Couple(net, loco1ID, loco1ID, "TRAIN"); err == nil {
			t.Fatal("сцеп сцепился сам с собой")
		}
	})

	t.Run("нет такого сцепа", func(t *testing.T) {
		m, net := coupled(t, netloc.DirForward)
		if _, err := m.Couple(net, loco1ID, "НЕТ_ТАКОГО", "TRAIN"); err == nil {
			t.Fatal("сцепка с несуществующим сцепом прошла")
		}
	})

	t.Run("новый сцеп без имени", func(t *testing.T) {
		m, net := coupled(t, netloc.DirForward)
		if _, err := m.Couple(net, loco1ID, loco2ID, ""); err == nil {
			t.Fatal("сцепка прошла без имени нового сцепа")
		}
	})
}

// РАСЦЕПКА ДЕЛИТ ЦЕПОЧКУ НАДВОЕ и обе части оставляет живыми сцепами.
func TestUncoupleSplitsTheChain(t *testing.T) {
	m, net := coupled(t, netloc.DirForward)
	if _, err := m.Couple(net, loco1ID, loco2ID, "TRAIN"); err != nil {
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
	if _, err := m.Couple(net, loco1ID, loco2ID, "TRAIN"); err != nil {
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
