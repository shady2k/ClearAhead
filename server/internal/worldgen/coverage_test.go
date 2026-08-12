package worldgen

import (
	"math"
	"strconv"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/chunk"
	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/seedmap"
	"github.com/shady2k/ClearAhead/server/internal/terrain"
	"github.com/shady2k/ClearAhead/server/internal/track"
)

// Замер покрытия. Дефект ClearAhead-cue был найден снаружи — скриптом по
// числам манифеста и глазами на снимке экрана, — и это ровно то, чего здесь не
// хватало: правило покрытия проверялось только на «чанки порождены», а не на
// «земля покрыта». Поэтому тесты ниже не спрашивают правило, покрывает ли оно
// площадь, а МЕРЯЮТ площадь пробами и считают доли.
//
// Прежнее правило и пирамида по центру оставлены здесь живым кодом: дефект
// обязан быть воспроизводим, а цена нового правила — сравнима с ценой старого
// на одних и тех же пробах.

// legacyBandRule — прежнее правило: клетка уровня L хранится тогда и только
// тогда, когда LevelFor(расстояние от ЦЕНТРА клетки) == L. Оно и оставляло
// дыры: полосы заданы расстоянием, а клетки нарезаны сетками разного шага.
func (s selector) legacyBandRule(a chunk.Address) bool {
	side := chunk.SideM(a.Level)
	x0, z0 := a.OriginM()
	d, ok := s.field.DistanceToAxis(x0+side/2, z0+side/2)
	if !ok {
		return false
	}
	want, ok := s.rule.LevelFor(d)
	return ok && want == a.Level
}

// centerPyramidRule — пирамида (круги вложены), но попадание в круг меряется по
// центру клетки. Дыр не оставляет, зато не покрывает круг уровня целиком.
func (s selector) centerPyramidRule(a chunk.Address) bool {
	side := chunk.SideM(a.Level)
	x0, z0 := a.OriginM()
	d, ok := s.field.DistanceToAxis(x0+side/2, z0+side/2)
	return ok && d <= s.rule.RadiusM(a.Level)
}

// storedSet собирает адреса, которые правило велит хранить.
func storedSet(s selector, rule func(chunk.Address) bool) map[chunk.Address]bool {
	out := map[chunk.Address]bool{}
	_, _, err := walk(s, "", rule, func(a chunk.Address) error {
		out[a] = true
		return nil
	})
	if err != nil {
		panic(err)
	}
	return out
}

// tally — что показали пробы.
type tally struct {
	inReach  int // проб внутри охвата (ось ближе радиуса последнего уровня)
	holes    int // проб, не покрытых ни одним уровнем
	overlaps int // проб, покрытых более чем одним уровнем
	levels   int // сумма числа покрывающих уровней по всем пробам
	// inCircle/покрыто — по уровням: сколько проб попало внутрь круга уровня и
	// сколько из них накрыто клеткой ЭТОГО уровня.
	// Длина массивов — по потолку ФОРМЫ (chunk.MaxLevelLimit), а не по правилу:
	// правило приезжает картой и константой длины быть больше не может.
	inCircle [chunk.MaxLevelLimit + 1]int
	covered  [chunk.MaxLevelLimit + 1]int
}

func (i tally) share(n int) float64 {
	if i.inReach == 0 {
		return 0
	}
	return 100 * float64(n) / float64(i.inReach)
}

// meanLevels — сколько уровней в среднем накрывает пробу. Число говорит о цене
// перекрытия там, где доля наложений упирается в сто процентов.
func (i tally) meanLevels() float64 {
	if i.inReach == 0 {
		return 0
	}
	return float64(i.levels) / float64(i.inReach)
}

// probes прогоняют сетку точек с заданным шагом по габариту оси, расширенному на
// охват, и считают покрытие по хранимому множеству адресов.
//
// Точки вне охвата пропускаются: дальше последнего радиуса не хранится ничего,
// и это не дыра, а объявленная разреженность (chunk: «отсутствие чанка законно,
// клиент рисует базовую поверхность»).
func probes(s selector, stored map[chunk.Address]bool, reach, step float64) tally {
	var it tally
	for x := s.minX - reach; x <= s.maxX+reach; x += step {
		for z := s.minY - reach; z <= s.maxY+reach; z += step {
			d, ok := s.field.DistanceToAxis(x, z)
			if !ok {
				continue
			}
			it.inReach++
			levels := 0
			for l := 0; l <= s.rule.MaxLevel; l++ {
				side := chunk.SideM(l)
				a := chunk.Address{
					Level: l,
					CX:    int(math.Floor(x / side)),
					CZ:    int(math.Floor(z / side)),
				}
				covered := stored[a]
				if covered {
					levels++
				}
				if d <= s.rule.RadiusM(l) {
					it.inCircle[l]++
					if covered {
						it.covered[l]++
					}
				}
			}
			it.levels += levels
			switch {
			case levels == 0:
				it.holes++
			case levels > 1:
				it.overlaps++
			}
		}
	}
	return it
}

// TestCoverageHasNoHoles — главный замер биды ClearAhead-cue.
//
// Утверждается ровно то, что было измерено снаружи и обязано перестать быть
// правдой: непокрытой площади внутри охвата больше нет. Наложения при этом
// объявлены нормой — круги уровней вложены нарочно, — но их доля названа
// числом, а не оставлена «где-то там».
func TestCoverageHasNoHoles(t *testing.T) {
	f := seedField(t)
	s, ok := selectorFor(f)
	if !ok {
		t.Fatal("у затравки нет оси")
	}
	after := storedSet(s, s.covers)
	before := storedSet(s, s.legacyBandRule)

	// Вблизи пути — там же, где дефект видели глазами: шаг 10 м, полоса вокруг
	// станции шириной в радиус первого уровня.
	nearReach := s.rule.RadiusM(1)
	afterProbes := probes(s, after, nearReach, 10)
	beforeProbes := probes(s, before, nearReach, 10)

	t.Logf("вблизи (%d проб, шаг 10 м):", afterProbes.inReach)
	t.Logf("  полосы по центру (было): дыр %.2f %%, наложений %.2f %%, уровней на пробу %.2f",
		beforeProbes.share(beforeProbes.holes), beforeProbes.share(beforeProbes.overlaps), beforeProbes.meanLevels())
	t.Logf("  пирамида по телу (стало): дыр %.2f %%, наложений %.2f %%, уровней на пробу %.2f",
		afterProbes.share(afterProbes.holes), afterProbes.share(afterProbes.overlaps), afterProbes.meanLevels())

	// Дефект обязан быть воспроизводим: если прежнее правило дыр не даёт, то
	// мерить нечего и тест ничего не проверяет.
	if beforeProbes.holes == 0 {
		t.Fatal("прежнее правило не оставило дыр — замер потерял смысл")
	}
	if afterProbes.holes != 0 {
		t.Errorf("вблизи не покрыто %d проб из %d (%.2f %%), должно быть ноль",
			afterProbes.holes, afterProbes.inReach, afterProbes.share(afterProbes.holes))
	}

	// По всему охвату — до восьми километров от оси. Шаг крупнее: площадь
	// круга последнего уровня в двести раз больше ближней полосы.
	fullReach := s.rule.ReachM()
	afterAllProbes := probes(s, after, fullReach, 128)
	beforeAllProbes := probes(s, before, fullReach, 128)
	t.Logf("весь охват (%d проб, шаг 128 м):", afterAllProbes.inReach)
	t.Logf("  полосы по центру (было): дыр %.2f %%, наложений %.2f %%, уровней на пробу %.2f",
		beforeAllProbes.share(beforeAllProbes.holes), beforeAllProbes.share(beforeAllProbes.overlaps), beforeAllProbes.meanLevels())
	t.Logf("  пирамида по телу (стало): дыр %.2f %%, наложений %.2f %%, уровней на пробу %.2f",
		afterAllProbes.share(afterAllProbes.holes), afterAllProbes.share(afterAllProbes.overlaps), afterAllProbes.meanLevels())
	if afterAllProbes.holes != 0 {
		t.Errorf("в охвате не покрыто %d проб из %d (%.2f %%), должно быть ноль",
			afterAllProbes.holes, afterAllProbes.inReach, afterAllProbes.share(afterAllProbes.holes))
	}
}

// TestLevelCircleIsFullyCovered — гарантия, ради которой попадание меряется по
// ТЕЛУ клетки, а не по её центру: внутри R_L земля есть на уровне L.
//
// Каждый уровень меряется своей сеткой: шаг R_L/64, чтобы плотность проб внутри
// круга не зависела от уровня.
func TestLevelCircleIsFullyCovered(t *testing.T) {
	f := seedField(t)
	s, ok := selectorFor(f)
	if !ok {
		t.Fatal("у затравки нет оси")
	}
	after := storedSet(s, s.covers)
	center := storedSet(s, s.centerPyramidRule)

	for l := 0; l <= s.rule.MaxLevel; l++ {
		r := s.rule.RadiusM(l)
		afterProbes := probes(s, after, r, r/64)
		centerProbes := probes(s, center, r, r/64)
		coveredPct := func(it tally) float64 {
			if it.inCircle[l] == 0 {
				return 0
			}
			return 100 * float64(it.covered[l]) / float64(it.inCircle[l])
		}
		t.Logf("уровень %d (R = %.0f м, %d проб в круге): по телу %.2f %%, по центру %.2f %%",
			l, r, afterProbes.inCircle[l], coveredPct(afterProbes), coveredPct(centerProbes))
		if afterProbes.covered[l] != afterProbes.inCircle[l] {
			t.Errorf("уровень %d покрывает свой круг на %.2f %%: %d проб из %d",
				l, coveredPct(afterProbes), afterProbes.covered[l], afterProbes.inCircle[l])
		}
	}
}

// clientPick — уровень, которым КЛИЕНТ накроет точку, и накроет ли вообще.
//
// Правило клиента другое по существу, чем правило сервера, и путать их нельзя:
// сервер решает про КЛЕТКУ («хранить ли её»), клиент решает про ТОЧКУ («каким
// уровнем её нарисовать»). Из манифеста выводимо ровно второе — LevelFor(d)
// называет самый подробный уровень, круг которого точку накрывает, — и больше
// клиенту знать не нужно: ни по телу или по центру сервер отбирал клетки, ни
// какие клетки у него в базе.
//
// Спуск к более грубому уровню на пустой ответ — ЧАСТЬ ПРАВИЛА, а не запасной
// путь: расстояние клиент считает по своей выборке оси и вправе разойтись с
// серверным. Цена расхождения замерена в
// TestClientLevelChoiceToleratesDistanceError.
func clientPick(rule chunk.Rule, stored map[chunk.Address]bool, x, z, dist float64) (level int, drawn bool) {
	l, ok := rule.LevelFor(dist)
	if !ok {
		return 0, false
	}
	for ; l <= rule.MaxLevel; l++ {
		if stored[cellAt(l, x, z)] {
			return l, true
		}
	}
	return 0, false
}

// cellAt — клетка уровня, содержащая точку.
func cellAt(level int, x, z float64) chunk.Address {
	side := chunk.SideM(level)
	return chunk.Address{Level: level, CX: int(math.Floor(x / side)), CZ: int(math.Floor(z / side))}
}

// clientProbes прогоняет ту же сетку проб, что и probes, но спрашивает не «чем
// покрыто место», а «что нарисует в нём клиент». Расстояние клиенту даёт тот же
// оракул, смещённый на err: так меряется расхождение его выборки оси с нашей.
//
// Возвращает: проб внутри охвата, проб без земли, проб, нарисованных грубее,
// чем назвал бы точный клиент, и проб, потерянных каймой последнего круга.
func clientProbes(s selector, stored map[chunk.Address]bool, reach, step, err float64, descend bool) (inReach, holes, coarser, rim int) {
	for x := s.minX - reach; x <= s.maxX+reach; x += step {
		for z := s.minY - reach; z <= s.maxY+reach; z += step {
			d, ok := s.field.DistanceToAxis(x, z)
			if !ok {
				continue
			}
			inReach++
			want, _ := s.rule.LevelFor(d)
			seen := math.Max(0, d+err)
			var (
				level int
				drawn bool
			)
			if descend {
				level, drawn = clientPick(s.rule, stored, x, z, seen)
			} else {
				// Клиент без спуска: спросил названный уровень, получил 204 и
				// на этом остановился. Так устроен сегодняшний клиент.
				if l, in := s.rule.LevelFor(seen); in {
					level, drawn = l, stored[cellAt(l, x, z)]
				}
			}
			switch {
			case drawn && level > want:
				coarser++
			case !drawn && d > s.rule.ReachM()-math.Abs(err):
				// Кайма шириной с ошибку у внешней границы последнего круга:
				// ошибшийся клиент считает, что вышел за охват. Это не дыра, а
				// сдвинутый край мира — за ним отсутствие земли законно.
				rim++
			case !drawn:
				holes++
			}
		}
	}
	return inReach, holes, coarser, rim
}

// TestClientDrawsExactlyOneLayer — почему наложение уровней законно.
//
// Сервер хранит круги вложенными нарочно: вблизи пути пробу накрывает в среднем
// 4.39 уровня (TestCoverageHasNoHoles). Само по себе это z-fighting — четыре
// поверхности разной подробности в одном месте. Законным наложение делает ровно
// одно свойство, и оно проверяется здесь: в каждой точке клиент рисует ОДИН
// уровень, тот, что назвал LevelFor, и этот уровень у сервера ЕСТЬ ВСЕГДА —
// спускаться не приходится ни разу.
//
// Спуск, потребовавшийся хоть где-то, означал бы шов подробности: в соседних
// точках рисуются разные уровни без причины в данных. Поэтому доля спусков тоже
// обязана быть нулём, а не «малой».
//
// Прежнее правило меряется тем же клиентом и на тех же пробах: клиент,
// выводящий уровень по LevelFor, оставался на нём без земли — это и есть чёрные
// квадраты со снимка (ClearAhead-cue).
func TestClientDrawsExactlyOneLayer(t *testing.T) {
	f := seedField(t)
	s, ok := selectorFor(f)
	if !ok {
		t.Fatal("у затравки нет оси")
	}
	stored := storedSet(s, s.covers)
	legacy := storedSet(s, s.legacyBandRule)

	windows := []struct {
		name        string
		reach, step float64
	}{
		{"вблизи", s.rule.RadiusM(1), 10},
		{"весь охват", s.rule.ReachM(), 128},
	}
	for _, w := range windows {
		inReach, holes, coarser, _ := clientProbes(s, stored, w.reach, w.step, 0, true)
		lIn, lHoles, _, _ := clientProbes(s, legacy, w.reach, w.step, 0, true)
		t.Logf("%s (%d проб, шаг %.0f м):", w.name, inReach, w.step)
		t.Logf("  полосы по центру (было): клиент без земли %.2f %%",
			100*float64(lHoles)/float64(lIn))
		t.Logf("  пирамида по телу (стало): клиент без земли %.2f %%, спусков %.2f %%",
			100*float64(holes)/float64(inReach), 100*float64(coarser)/float64(inReach))
		if lHoles == 0 {
			t.Errorf("%s: на прежнем правиле клиент не остался без земли — замер потерял смысл", w.name)
		}
		if holes != 0 {
			t.Errorf("%s: клиент остался без земли на %d пробах из %d (%.2f %%)",
				w.name, holes, inReach, 100*float64(holes)/float64(inReach))
		}
		if coarser != 0 {
			t.Errorf("%s: спуск потребовался на %d пробах из %d — уровень, названный LevelFor, обязан существовать",
				w.name, coarser, inReach)
		}
	}
}

// TestClientLevelChoiceToleratesDistanceError — цена того, что расстояние до оси
// клиент меряет СВОЕЙ выборкой.
//
// Бида ClearAhead-cue называла это второй половиной дефекта: манифест отдавал
// числа правила, но не отдавал шага, которым выбрана ось, и клиент его угадывал.
// Шаг теперь отдаётся полем axis_step_m (terrain.AxisStepM), но угадывание — не
// единственный источник расхождения: округление, выборка по авторской
// координате, своя длина дуги. Поэтому важно не совпадение, а свойство:
// расхождение обязано стоить ПОДРОБНОСТИ, а не земли.
//
// Замер идёт по растущей ошибке в обе стороны, от половины шага выборки до
// стороны чанка уровня 0, и рядом меряется клиент БЕЗ спуска.
//
// Что замер показал и чего рассуждением видно не было: половина шага выборки
// почти ничего не стоит даже КЛИЕНТУ БЕЗ СПУСКА — 0.01 % площади вблизи пути.
// Держит это отбор по ТЕЛУ клетки: клетка хранится, если в круг попала любая её
// точка, а сторона клетки на два порядка больше ошибки, и клетка вокруг
// ошибочно названной точки почти всегда уже отобрана. Цена растёт со стороной
// клетки: ошибка в четверть стороны стоит клиенту без спуска 0.54 % площади, а
// в целую сторону — 9.20 %. Со спуском все шесть замеров дают ноль.
//
// Отсюда и место спуска в контракте: сегодня он страховка, а не костыль, и
// платит за него тот, кто ошибся, — подробностью (13.57 % площади на уровень
// грубее при ошибке в 64 м).
func TestClientLevelChoiceToleratesDistanceError(t *testing.T) {
	f := seedField(t)
	s, ok := selectorFor(f)
	if !ok {
		t.Fatal("у затравки нет оси")
	}
	stored := storedSet(s, s.covers)

	// Ближняя полоса шагом 10 м: расхождение уровней живёт узкой каймой вокруг
	// границ кругов, и на сетке 128 м такая кайма в пробы просто не попадает.
	nearReach := s.rule.RadiusM(1)
	eps := terrain.AxisStepM / 2
	side := chunk.SideM(0)
	var naiveLost float64
	for _, e := range []float64{-side, -side / 4, -eps, +eps, +side / 4, +side} {
		inReach, holes, coarser, rim := clientProbes(s, stored, nearReach, 10, e, true)
		_, naiveHoles, _, _ := clientProbes(s, stored, nearReach, 10, e, false)
		share := func(n int) float64 { return 100 * float64(n) / float64(inReach) }
		t.Logf("ошибка %+7.1f м (%d проб, шаг 10 м): со спуском — без земли %.2f %%, грубее %.2f %%; без спуска — без земли %.2f %%",
			e, inReach, share(holes), share(coarser), share(naiveHoles))
		if holes != 0 {
			t.Errorf("ошибка %+.1f м: клиент со спуском остался без земли на %d пробах из %d (%.2f %%)",
				e, holes, inReach, share(holes))
		}
		if rim != 0 {
			t.Errorf("ошибка %+.1f м: %d проб отнесены к кайме там, где до края охвата далеко", e, rim)
		}
		if math.Abs(e) >= side {
			naiveLost = math.Max(naiveLost, share(naiveHoles))
		}
	}
	// Если и ошибка размером с клетку ничего не стоит клиенту без спуска, то
	// абзац выше врёт про то, зачем спуск нужен, и его пора переписать.
	if naiveLost == 0 {
		t.Error("клиент без спуска не потерял земли даже при ошибке в сторону чанка — замер потерял смысл")
	}

	// Кайма последнего круга. Клиент, завысивший расстояние, считает, что вышел
	// за охват, и перестаёт рисовать на ошибку раньше: край мира сдвигается
	// внутрь, дырой это не становится — за краем земли нет и по контракту.
	fullReach := s.rule.ReachM()
	inReach, holes, coarser, rim := clientProbes(s, stored, fullReach, 128, +eps, true)
	t.Logf("весь охват, ошибка %+.1f м (%d проб, шаг 128 м): без земли %.2f %%, грубее %.2f %%, кайма %.2f %% (край сдвинут на %.1f м при радиусе %.0f м)",
		eps, inReach, 100*float64(holes)/float64(inReach), 100*float64(coarser)/float64(inReach),
		100*float64(rim)/float64(inReach), eps, fullReach)
	if holes != 0 {
		t.Errorf("весь охват: клиент остался без земли на %d пробах из %d", holes, inReach)
	}
}

// TestRuleCost — цена перекрытия в чанках и байтах.
//
// Замер, а не оценка: те же карты, тот же обход, разные правила. Числа отсюда
// названы в комментарии selector.covers и в chunk.
//
// Коридор в 200 км нужен ради экстраполяции на сеть: у затравки почти вся
// стоимость — это круги дальних уровней вокруг короткой станции, и отношение
// правил на ней говорит о станции, а не о сети. На длинной прямой оси
// стоимость линейна по длине пути, поэтому «сеть 2000 км» получается умножением
// на десять — тем же способом, каким считалась прежняя оценка в 388 МБ.
func TestRuleCost(t *testing.T) {
	maps := []struct {
		name string
		m    *mapfmt.Map
		// networkFactor — множитель до сети 2000 км, 0 если экстраполяция бессмысленна.
		networkFactor float64
	}{
		{name: "затравка", m: seedmap.Station(seedmap.WithTerrain())},
		{name: "коридор 20 км", m: straightCorridor(t, 20e3)},
		{name: "коридор 200 км", m: straightCorridor(t, 200e3), networkFactor: 10},
	}
	for _, tc := range maps {
		s, ok := selectorFor(fieldForMap(t, tc.m))
		if !ok {
			t.Fatalf("%s: нет оси", tc.name)
		}
		rules := []struct {
			name string
			f    func(chunk.Address) bool
		}{
			{"полосы по центру (было)", s.legacyBandRule},
			{"пирамида по центру", s.centerPyramidRule},
			{"пирамида по телу (стало)", s.covers},
		}
		var base int
		for i, r := range rules {
			var byLevel [chunk.MaxLevelLimit + 1]int
			_, chunks, err := walk(s, "", r.f, func(a chunk.Address) error {
				byLevel[a.Level]++
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if i == 0 {
				base = chunks
			}
			mb := float64(chunks*chunk.HeightsBytes) / 1e6
			network := ""
			if tc.networkFactor > 0 {
				network = ", сеть 2000 км: " + format(mb*tc.networkFactor) + " МБ"
			}
			t.Logf("%s / %s: чанков %d (× %.2f к прежнему), %.2f МБ%s, по уровням %v",
				tc.name, r.name, chunks, float64(chunks)/float64(base), mb, network, byLevel)
		}
	}
}

func format(v float64) string {
	return strconv.FormatFloat(v, 'f', 0, 64)
}

// fieldForMap строит поле по карте — то же, что делает Generate между валидацией
// и обходом.
func fieldForMap(tb testing.TB, m *mapfmt.Map) *terrain.Field {
	tb.Helper()
	_, els, err := track.Propagate(m)
	if err != nil {
		tb.Fatal(err)
	}
	f, err := terrain.New(m, els)
	if err != nil {
		tb.Fatal(err)
	}
	return f
}
