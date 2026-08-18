package match

import (
	"math/rand"
	"strings"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/netloc"
	"github.com/shady2k/ClearAhead/server/internal/seedmap"
	"github.com/shady2k/ClearAhead/server/internal/track"
	"github.com/shady2k/ClearAhead/server/internal/units"
)

const occupancySeed = 20260815

func metres(t *testing.T, v float64) units.Distance {
	t.Helper()
	d, err := units.MetersToDistance(v)
	if err != nil {
		t.Fatalf("перевод %v м: %v", v, err)
	}
	return d
}

// twoUnits — партия с двумя машинами на главном пути, расставленными вручную.
//
// Вручную, а не через Start: расстановка ОТКАЗЫВАЕТ при наложении, а половина
// проверок здесь — про то, как отказ наступает, и им нужна партия до отказа.
func twoUnits(t *testing.T, u1, u2 float64) (*Match, *track.CompiledNetwork) {
	t.Helper()
	net, s := station(t), set(t)
	st, ok := s.StockType("VL80")
	if !ok {
		t.Fatal("в наборе фикстуры нет паспорта VL80")
	}
	m := &Match{ID: "M1", Region: "ST_A", Units: []Unit{
		{ID: loco1ID, Name: "LOCO_1", Type: "VL80", At: netloc.PointU{
			Element: seedmap.StationMain, U: u1, Direction: netloc.DirForward}},
		{ID: loco2ID, Name: "LOCO_2", Type: "VL80", At: netloc.PointU{
			Element: seedmap.StationMain, U: u2, Direction: netloc.DirForward}},
	}}
	for _, u := range m.Units {
		mo, err := StartMotion(u, st, net.Elements[seedmap.StationMain])
		if err != nil {
			t.Fatalf("состояние единицы %s: %v", u.ID, err)
		}
		m.SetMotion(u.ID, mo)
	}
	return m, net
}

// TestOccupancyRebuildMatchesIncremental — DIFFERENTIAL-ТЕСТ индекса занятости.
//
// # Зачем он и что ловит
//
// Индекс копится по одному телу (setOccupancy в SetMotion), а полный пересчёт
// (RebuildOccupancy) читает мир целиком. Сойтись они обязаны всегда, и вся
// ценность накопления держится на этом: разойдясь, индекс начинает отвечать про
// мир, которого уже нет, — и заметить это по одному лишь поведению почти
// невозможно, потому что каждый следующий шаг чинит половину расхождений сам.
//
// Ловит ровно два способа сломать накопление, и оба неочевидны: НЕ СНЯТУЮ ссылку
// на элемент, с которого тело ушло (элемент остаётся занятым навсегда), и
// ПУСТОЙ СПИСОК, оставленный вместо удаления ключа (индекс копит имена
// элементов, по которым когда-то проезжали).
//
// Прогон длинный и случайный НАРОЧНО: тело возит по стрелке в обе стороны,
// набирая и теряя визиты. Короткий детерминированный прогон проверил бы одну
// последовательность из многих.
func TestOccupancyRebuildMatchesIncremental(t *testing.T) {
	m, net := twoUnits(t, 60, 150)
	top := track.NewTopology(net)
	rng := rand.New(rand.NewSource(occupancySeed))
	// seen — элементы, побывавшие занятыми ХОТЬ РАЗ. Считать в конце то, что
	// занято СЕЙЧАС, было ошибкой: тела вправе вернуться туда же, откуда ушли, и
	// прогон, прошедший всю станцию, выглядел бы неподвижным.
	seen := map[string]bool{}
	for i := range 600 {
		branch := track.BranchStraight
		if rng.Intn(4) == 0 {
			branch = track.BranchDiverging
		}
		w := top.At(func(string) string { return branch })
		id := m.Units[rng.Intn(len(m.Units))].ID
		mo, ok := m.MotionOf(id)
		if !ok {
			t.Fatalf("шаг %d: у %s нет состояния", i, id)
		}
		moved, err := slideSpan(mo.Span, w, metres(t, (rng.Float64()-0.5)*50))
		if err != nil {
			t.Fatalf("шаг %d: %v", i, err)
		}
		if err := mo.SetSpan(moved); err != nil {
			t.Fatalf("шаг %d: %v", i, err)
		}
		m.SetMotion(id, mo)
		if err := m.checkOccupancy(); err != nil {
			t.Fatalf("шаг %d (тело %s, ветвь %s): %v", i, id[:8], branch, err)
		}
		for el := range m.Occupied {
			seen[el] = true
		}
	}
	// Прогон обязан быть содержательным: если тела не сходили со своего
	// элемента, differential-тест проверил бы неподвижный индекс — то есть
	// ничего.
	if len(seen) < 3 {
		t.Fatalf("за прогон занятыми побывали %d элементов — тела почти не ездили", len(seen))
	}
	t.Logf("за прогон занятыми побывали %d элементов", len(seen))
}

// slideSpan — сдвиг отрезка вдоль хода B → A. Копия того, что делает физика; она
// здесь потому, что звать физику из партии нельзя (импорт идёт в другую сторону).
func slideSpan(sp track.Span, w track.Walk, d units.Distance) (track.Span, error) {
	grow, trim := track.Span.GrowA, track.Span.TrimB
	if d < 0 {
		grow, trim = track.Span.GrowB, track.Span.TrimA
		d = -d
	}
	grown, stuck, err := grow(sp, w, d)
	if err != nil {
		return nil, err
	}
	got := d - stuck
	if got == 0 {
		return sp, nil
	}
	return trim(grown, got)
}

// TestOccupancyDropsTheAbandonedElement — тело, ушедшее с элемента, перестаёт
// его занимать, и ключ из индекса ИСЧЕЗАЕТ.
//
// Отдельно от differential-теста, потому что тот говорит «разошлось», а этот —
// «вот что именно и почему». Пустой список вместо удаления ключа выглядит
// безобидно ровно до первого вопроса «занят ли элемент»: len(refs) == 0 ответит
// верно, а вот обход индекса — нет.
func TestOccupancyDropsTheAbandonedElement(t *testing.T) {
	m, net := twoUnits(t, 20, 150)
	w := track.NewTopology(net).At(func(string) string { return track.BranchStraight })
	mo, _ := m.MotionOf(loco1ID)
	// Уводим первое тело целиком на проход стрелки: 20 − 16.42 = 3.58 м до
	// границы, плюс вся длина машины.
	moved, err := slideSpan(mo.Span, w, -metres(t, 40))
	if err != nil {
		t.Fatalf("сдвиг: %v", err)
	}
	if err := mo.SetSpan(moved); err != nil {
		t.Fatalf("состояние: %v", err)
	}
	m.SetMotion(loco1ID, mo)

	passage := seedmap.StationSW1 + mapfmt.PassageStraight
	if len(m.OccupiedBy(passage)) != 1 {
		t.Fatalf("проход стрелки занят %d телами, ожидалось одно: %+v",
			len(m.OccupiedBy(passage)), m.Occupied)
	}
	// На главном пути осталось только второе тело.
	main := m.OccupiedBy(seedmap.StationMain)
	if len(main) != 1 || main[0].Unit != loco2ID {
		t.Fatalf("на главном пути %+v, ожидалось одно тело %s", main, loco2ID)
	}
	if err := m.checkOccupancy(); err != nil {
		t.Fatal(err)
	}
}

// TestConflictFindsTheOtherBody — запрет наложения отвечает КЕМ и ГДЕ.
//
// Отказ без места заставляет искать причину глазами по всей карте; поэтому
// проверяется не признак, а обе части ответа.
func TestConflictFindsTheOtherBody(t *testing.T) {
	m, _ := twoUnits(t, 60, 150)
	mo, _ := m.MotionOf(loco2ID)
	// Своё тело в конфликт не идёт: оно накладывается само на себя всегда.
	if _, _, busy := m.Conflict(loco2ID, mo.Span); busy {
		t.Fatal("тело объявлено наложившимся само на себя")
	}
	// Чужое — идёт.
	ref, at, busy := m.Conflict(loco1ID, mo.Span)
	if !busy {
		t.Fatal("тело, стоящее на месте второго, наложения не дало")
	}
	if ref.Unit != loco2ID {
		t.Fatalf("наложение приписано %s, а на месте стоит %s", ref.Unit, loco2ID)
	}
	if at.Element != seedmap.StationMain || at.To <= at.From {
		t.Fatalf("место наложения названо как %+v", at)
	}
	// Ссылка ведёт на живой визит, а не на копию координат.
	iv, ok := ref.Interval(*m)
	if !ok || iv != mo.Span[ref.Visit] {
		t.Fatalf("ссылка %+v не разрешается в визит отрезка", ref)
	}
}

// TestPlacementRefusesOverlapWithMessage — расстановка отказывает и НАЗЫВАЕТ
// обе стороны по читаемой метке, а не по UUID.
func TestPlacementRefusesOverlapWithMessage(t *testing.T) {
	doc := good()
	doc["units"] = append(doc["units"].([]any), map[string]any{
		"id": loco2ID, "name": "LOCO_2", "type": "VL80",
		"at": map[string]any{"element": seedmap.StationMain, "u": 160.0, "direction": "forward"},
	})
	_, err := Start("M1", write(t, doc), station(t), set(t))
	if err == nil {
		t.Fatal("две машины в 10 м друг от друга приняты")
	}
	for _, want := range []string{"накладывается", "LOCO_1", "LOCO_2", seedmap.StationMain} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("в отказе нет %q: %v", want, err)
		}
	}
}

// TestPlacementAllowsTouchingBodies — стоящие ВПЛОТНУЮ не накладываются.
//
// Полуоткрытые интервалы (ClearAhead-5zd): при целых микрометрах равенство
// концов достижимо, и закрытый интервал объявил бы столкновением два вагона в
// сцепе. Проверка стоит рядом с отказом нарочно — граница между «нельзя» и
// «можно» проходит по одному микрометру, и обе её стороны надо назвать.
func TestPlacementAllowsTouchingBodies(t *testing.T) {
	doc := good()
	// 150 и 150 + 32.84: концы сходятся ровно.
	doc["units"] = append(doc["units"].([]any), map[string]any{
		"id": loco2ID, "name": "LOCO_2", "type": "VL80",
		"at": map[string]any{"element": seedmap.StationMain, "u": 182.84, "direction": "forward"},
	})
	m, err := Start("M1", write(t, doc), station(t), set(t))
	if err != nil {
		t.Fatalf("машины, стоящие вплотную, отвергнуты: %v", err)
	}
	if len(m.OccupiedBy(seedmap.StationMain)) != 2 {
		t.Fatalf("на главном пути %d тел, ожидалось два", len(m.OccupiedBy(seedmap.StationMain)))
	}
	if err := m.checkOccupancy(); err != nil {
		t.Fatal(err)
	}
}

// TestDeviceBusyNamesTheBody — «стрелка занята» отвечает КЕМ.
//
// Первый вопрос диспетчерской половины (ClearAhead-duf) задаётся так, и ответ
// «занята» без имени — это отказ, после которого игрок ищет причину глазами.
func TestDeviceBusyNamesTheBody(t *testing.T) {
	m, net := twoUnits(t, 20, 150)
	if _, busy := m.DeviceBusy(net, seedmap.StationSW1); busy {
		t.Fatal("стрелка занята, хотя оба тела на главном пути")
	}
	w := track.NewTopology(net).At(func(string) string { return track.BranchStraight })
	mo, _ := m.MotionOf(loco1ID)
	moved, err := slideSpan(mo.Span, w, -metres(t, 10))
	if err != nil {
		t.Fatalf("сдвиг: %v", err)
	}
	if err := mo.SetSpan(moved); err != nil {
		t.Fatalf("состояние: %v", err)
	}
	m.SetMotion(loco1ID, mo)
	who, busy := m.DeviceBusy(net, seedmap.StationSW1)
	if !busy || who != loco1ID {
		t.Fatalf("стрелку занял %q (занята: %v), а хвост завёл туда %s", who, busy, loco1ID)
	}
}

// TestReferenceFollowsTheSpan — точка отсчёта не расходится с отрезком.
//
// Element, S и Facing — КЭШ середины отрезка, и цена кэша названа у самих полей:
// он вправе разойтись с записью. Уплачена она единственным писателем (SetSpan), а
// сторожем служит эта проверка: она пересчитывает середину независимо и на
// длинном случайном прогоне.
func TestReferenceFollowsTheSpan(t *testing.T) {
	m, net := twoUnits(t, 60, 150)
	top := track.NewTopology(net)
	rng := rand.New(rand.NewSource(occupancySeed))
	for i := range 400 {
		w := top.At(func(string) string { return track.BranchStraight })
		id := m.Units[rng.Intn(len(m.Units))].ID
		mo, _ := m.MotionOf(id)
		moved, err := slideSpan(mo.Span, w, metres(t, (rng.Float64()-0.5)*50))
		if err != nil {
			t.Fatalf("шаг %d: %v", i, err)
		}
		if err := mo.SetSpan(moved); err != nil {
			t.Fatalf("шаг %d: %v", i, err)
		}
		m.SetMotion(id, mo)

		got, _ := m.MotionOf(id)
		el, at, dir, ok := got.Span.PointAt(got.Span.Length() / 2)
		if !ok {
			t.Fatalf("шаг %d: середина отрезка не нашлась", i)
		}
		if got.Element != el || got.S != at || got.Facing != dir {
			t.Fatalf("шаг %d: точка отсчёта (%s, %s, %s) разошлась с серединой отрезка (%s, %s, %s)",
				i, got.Element, got.S, got.Facing, el, at, dir)
		}
	}
}
