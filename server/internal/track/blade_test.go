package track

import (
	"math"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/seedmap"
)

// TestBladesLieOnTheThreadsThatMeetAtTheFrog — ОСТРЯК И КРЕСТОВИНА ЛЕЖАТ НА
// ОДНОЙ ПАРЕ НИТОК.
//
// Это не совпадение чисел, а утверждение об устройстве: подвижны ровно те две
// нитки, которые дальше пересекаются в крестовине, — начало и конец одной пары.
// Разойдись они, остряк отводил бы нитку, которая в крестовину не приходит, и
// перевод стрелки уводил бы машину не туда, куда показывает указатель.
//
// Проверяется ЗНАК смещения против рукости, а не значение: значение есть
// половина колеи и проверяется соседним утверждением.
func TestBladesLieOnTheThreadsThatMeetAtTheFrog(t *testing.T) {
	m := seedmap.Station()
	_, rg, err := Compile(m)
	if err != nil {
		t.Fatalf("компиляция: %v", err)
	}
	if len(rg.TurnoutBlades) != 2*len(m.Topology.Turnouts) {
		t.Fatalf("остряков %d при %d стрелках — у каждой их ровно два",
			len(rg.TurnoutBlades), len(m.Topology.Turnouts))
	}
	hands := map[string]string{}
	for _, tt := range m.Topology.Turnouts {
		hands[tt.ID] = tt.Hand
	}
	half := m.Construction.Types[0].Gauge / 2
	for _, b := range rg.TurnoutBlades {
		// У правой стрелки прямой проход подвижен на −gauge/2, боковой на
		// +gauge/2 — те же две нитки, что перечислены в frogFeature.
		want := -half
		if (hands[b.Owner] == mapfmt.HandRight) != (b.Branch == "straight") {
			want = +half
		}
		if math.Abs(b.Offset-want) > 1e-9 {
			t.Fatalf("стрелка %s, ветвь %s, рукость %s: остряк на %+.4f, а нитка крестовины на %+.4f",
				b.Owner, b.Branch, hands[b.Owner], b.Offset, want)
		}
		if b.Throw <= 0 || b.Length <= 0 {
			t.Fatalf("стрелка %s, ветвь %s: остряк длиной %g с ходом %g", b.Owner, b.Branch, b.Length, b.Throw)
		}
	}
}

// TestBladeIsNotLongerThanItsPassage — остряк не длиннее того, на чём лежит.
//
// Длина приходит от ТИПА ПУТИ, а проход — от геометрии устройства, и связи
// между ними нет никакой: у короткого перевода тип назначит остряк длиннее
// прохода. Обрезка объявлена (blade.go) и проверяется здесь, потому что без неё
// клиент откладывал бы отвод за концом элемента — то есть на соседнем пути.
//
// Карта теста ЗАВЕДОМО НЕ ПРОХОДИТ ВАЛИДАЦИЮ (40 м при потолке 20), и это
// нарочно: проверяется поведение КОМПИЛЯТОРА, который «не полагается на то, что
// его позвали после валидатора» (blade.go). Валидатор проверяется своим тестом.
func TestBladeIsNotLongerThanItsPassage(t *testing.T) {
	m := seedmap.Station()
	_, els, err := Propagate(m)
	if err != nil {
		t.Fatalf("распространение поз: %v", err)
	}
	// ПРОЕКТ ПОРТИТСЯ У ВЫЗОВА, А НЕ В КАРТЕ: каталог живёт в коде сервера, и
	// фикстура до него не достаёт. Портится копия записи, построитель зовётся
	// напрямую — проверяется ровно его обрезка.
	dt := seedmap.TurnoutProjectForTest()
	dt.Switch.BladeLengthStraight = 40.0
	dt.Switch.BladeLengthDiverging = 40.0
	types := map[string]mapfmt.TrackType{}
	for i := range m.Construction.Types {
		types[m.Construction.Types[i].ID] = m.Construction.Types[i]
	}
	var blades []RenderTurnoutBlade
	for _, x := range m.Topology.Turnouts {
		got, err := turnoutBlades(els, types, m.Construction, x, dt)
		if err != nil {
			t.Fatalf("остряки стрелки %s: %v", x.ID, err)
		}
		blades = append(blades, got...)
	}
	for _, b := range blades {
		el, ok := els[b.Passage]
		if !ok {
			t.Fatalf("проход %s не скомпилирован", b.Passage)
		}
		length := el.Plan.Length().Meters()
		if b.Length > length+1e-9 {
			t.Fatalf("остряк %.3f м на проходе %.3f м", b.Length, length)
		}
		if b.Length < length-1e-9 {
			t.Fatalf("остряк %.3f м при проходе %.3f м — обрезка обязана дать всю длину прохода",
				b.Length, length)
		}
	}
}

// TestBladeSpaceIsOwnedByNamedTurnoutParts — место остряка освобождается самой
// сборкой, а не разворотом сечения и не маской маршрутной нитки.
func TestBladeSpaceIsOwnedByNamedTurnoutParts(t *testing.T) {
	m := seedmap.Station()
	_, rg, err := Compile(m)
	if err != nil {
		t.Fatalf("компиляция: %v", err)
	}
	if len(rg.TurnoutBlades) == 0 {
		t.Fatal("остряков нет вовсе — мерить нечего")
	}
	for _, b := range rg.TurnoutBlades {
		if math.Abs(math.Abs(b.Grow)-1) > 1e-9 {
			t.Fatalf("стрелка %s, ветвь %s: сторона роста %+.4f, ожидается ±1", b.Owner, b.Branch, b.Grow)
		}
		for _, r := range rg.Rails {
			// Общая грань законна: рамный рельс лежит на ней же, а остряк
			// прижат к нему изнутри колеи. Незаконно совпадение сторон, в
			// которые растут ТЕЛА, — тогда они занимают один объём.
			//
			// У остряка сторон две, и путать их нельзя: Grow продолжает рабочую
			// нитку за корнем, BodyGrow размещает само тело. Сравнение с Grow
			// показывало бы наложение там, где его нет.
			if r.Owner == b.Owner && r.Element == b.Passage && r.Grow == b.BodyGrow &&
				math.Abs(r.Face-b.Offset) < 1e-9 && r.From < b.Length-1e-9 {
				t.Fatalf("деталь %s занимает место остряка %s до корня %.3f", r.ID, b.Passage, b.Length)
			}
		}
	}
}

// СТРОЖКА ПРИЕЗЖАЕТ С СЕРВЕРА и масштабируется головкой своего типа.
//
// До 2026-08-16 таблица жила константой класса Blade в клиенте и кончалась на
// 0.075 — ширине головки затравочного типа. Тест закрывает именно это: тип с
// другой головкой обязан получить другую таблицу, иначе допущение просто
// переехало бы вместе с числами.
func TestBladeSectionScalesWithHeadWidth(t *testing.T) {
	base := mapfmt.TrackRail{HeadWidth: 0.075}
	wide := mapfmt.TrackRail{HeadWidth: 0.150}

	got := bladeSection(base)
	if len(got) != len(bladeTaperModel) {
		t.Fatalf("станций %d, ожидалось %d", len(got), len(bladeTaperModel))
	}
	// В острие головка ОСТРОГАНА до миллиметров: остряк, вышедший там на полное
	// сечение, читается вторым рельсом, приставленным к первому.
	if math.Abs(got[0].HeadWidth-0.006) > 1e-9 {
		t.Fatalf("головка в острие %.6f, ожидалось 0.006", got[0].HeadWidth)
	}
	// В корне — полная головка своего типа: там остряк уже обычный рельс.
	last := got[len(got)-1]
	if math.Abs(last.HeadWidth-0.075) > 1e-9 {
		t.Fatalf("головка в корне %.6f, ожидалось 0.075", last.HeadWidth)
	}
	if last.Sink != 0 {
		t.Fatalf("в корне остряк обязан быть вровень с рамным, понижение %.4f", last.Sink)
	}

	// Головка вдвое шире — вдвое шире вся таблица, и ни одно число не осталось
	// прежним по недосмотру.
	for i, w := range bladeSection(wide) {
		if math.Abs(w.HeadWidth-2*got[i].HeadWidth) > 1e-9 {
			t.Fatalf("станция %d: головка %.6f, ожидалась вдвое от %.6f", i, w.HeadWidth, got[i].HeadWidth)
		}
		// Понижение и накат — НЕ доли головки: первое есть зазор строжки, второе
		// след колеса. Оба обязаны остаться теми же.
		if w.Sink != got[i].Sink || w.RideWidth != got[i].RideWidth {
			t.Fatalf("станция %d: понижение или накат поехали за головкой", i)
		}
	}
}

// Накат начинается НЕ В ОСТРИЕ: пока остряк острогана и понижен, колесо идёт по
// рамному рельсу. Полоса, начатая с нуля, означала бы, что колесо катится по
// миллиметровому клину.
func TestBladeRideStartsAfterToe(t *testing.T) {
	got := bladeSection(mapfmt.TrackRail{HeadWidth: 0.075})
	for _, s := range got {
		switch {
		case s.U <= BladeRideFrom && s.RideWidth != 0:
			t.Fatalf("на %.2f м накат %.4f, ожидался ноль", s.U, s.RideWidth)
		case s.U >= BladeRideFull && math.Abs(s.RideWidth-RailRideWidth) > 1e-9:
			t.Fatalf("на %.2f м накат %.4f, ожидался полный %.4f", s.U, s.RideWidth, RailRideWidth)
		}
	}
	// Понижение обязано сойти на нет НЕ ПОЗЖЕ, чем колесо встало на остряк:
	// иначе оно въезжает на деталь, лежащую ниже рамного рельса.
	for _, s := range got {
		if s.U >= BladeRideFull && s.Sink != 0 {
			t.Fatalf("на %.2f м накат уже полный, а понижение ещё %.4f", s.U, s.Sink)
		}
	}
}

// НОРМА: где головка остряка достигла 50 мм, понижения быть не должно.
//
// Единственное требование к строжке, взятое у нормы, а не объявленное нами.
// Русская эксплуатационная практика проверяет не координату вдоль остряка, а
// СЕЧЕНИЕ: понижение остряка против рамного рельса на 2 мм и более при ширине
// головки поверху 50 мм и более — уже неисправность.
//
// Тест сторожит именно МОДЕЛЬ, а не данные карты: числа u в bladeTaperModel
// объявлены приближением и вправе меняться, а это соотношение меняться не
// вправе. Правка таблицы, нарушившая норму, обязана уронить сборку здесь.
func TestBladeIsLevelWhereHeadReachesNorm(t *testing.T) {
	const maxSink = 0.002
	got := bladeSection(mapfmt.TrackRail{HeadWidth: 0.075})
	full := got[len(got)-1].HeadWidth
	var checked int
	for _, s := range got {
		if s.HeadWidth/full < BladeLevelHeadShare {
			continue
		}
		checked++
		if s.Sink > maxSink {
			t.Fatalf("на %.2f м головка %.0f %% полной, а понижение %.4f м — норма разрешает %.4f",
				s.U, 100*s.HeadWidth/full, s.Sink, maxSink)
		}
	}
	if checked == 0 {
		t.Fatal("ни одна станция не дошла до нормируемой ширины головки — норма не проверена ничем")
	}
}

// Норма проверяется НА ЛЮБОМ ТИПЕ, а не только на затравочном.
//
// Хранить норму долей и проверять на одной ширине головки значило бы держать
// доказательство там, где допущение уже снято.
func TestBladeNormHoldsForAnyHeadWidth(t *testing.T) {
	for _, head := range []float64{0.070, 0.075, 0.150} {
		got := bladeSection(mapfmt.TrackRail{HeadWidth: head})
		full := got[len(got)-1].HeadWidth
		for _, s := range got {
			if s.HeadWidth/full >= BladeLevelHeadShare && s.Sink > 0.002 {
				t.Fatalf("головка %.3f: на %.2f м доля %.2f, понижение %.4f", head, s.U, s.HeadWidth/full, s.Sink)
			}
		}
	}
}
