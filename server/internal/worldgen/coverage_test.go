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
	want, ok := chunk.LevelFor(d)
	return ok && want == a.Level
}

// centerPyramidRule — пирамида (круги вложены), но попадание в круг меряется по
// центру клетки. Дыр не оставляет, зато не покрывает круг уровня целиком.
func (s selector) centerPyramidRule(a chunk.Address) bool {
	side := chunk.SideM(a.Level)
	x0, z0 := a.OriginM()
	d, ok := s.field.DistanceToAxis(x0+side/2, z0+side/2)
	return ok && d <= chunk.RadiusM(a.Level)
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
	inCircle [chunk.MaxLevel + 1]int
	covered  [chunk.MaxLevel + 1]int
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
			for l := 0; l <= chunk.MaxLevel; l++ {
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
				if d <= chunk.RadiusM(l) {
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
	nearReach := chunk.RadiusM(1)
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
	fullReach := chunk.RadiusM(chunk.MaxLevel)
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

	for l := 0; l <= chunk.MaxLevel; l++ {
		r := chunk.RadiusM(l)
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
			var byLevel [chunk.MaxLevel + 1]int
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
