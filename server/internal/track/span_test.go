package track

import (
	"math/rand"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/netloc"
	"github.com/shady2k/ClearAhead/server/internal/seedmap"
	"github.com/shady2k/ClearAhead/server/internal/units"
)

// Проверки отрезка пути делятся надвое, и деление не для порядка.
//
// СВОЙСТВА (property) проверяют то, что обязано выполняться при ЛЮБОМ отрезке и
// любом сдвиге: длина не меняется, двукратный разворот — тождество, сдвиг туда и
// обратно возвращает на место. Их сила в том, что они не знают чисел фикстуры и
// потому не подтверждают код им же.
//
// ПРИМЕРЫ проверяют то, что верно про ЭТУ сеть: сколько визитов у тела на
// стрелке, каким портом оно вошло, где встал конец у упора. Их сила в том, что
// они читаемы и ловят ошибку в конкретном месте.

const spanSeed = 20260815

func spanNet(t *testing.T) *CompiledNetwork {
	t.Helper()
	cn, _, err := Compile(seedmap.Station())
	if err != nil {
		t.Fatalf("компиляция фикстуры: %v", err)
	}
	return cn
}

// spanWalk — обход при прямом положении обоих остряков.
func spanWalk(net *CompiledNetwork) Walk {
	return NewTopology(net).At(func(string) string { return BranchStraight })
}

func metres(v float64) units.Distance {
	d, err := units.MetersToDistance(v)
	if err != nil {
		panic(err)
	}
	return d
}

// spanAt — отрезок длиной length, лежащий серединой на u = at элемента.
func spanAt(t *testing.T, net *CompiledNetwork, element string, at, length float64,
	dir netloc.Direction) Span {
	t.Helper()
	el, ok := net.Elements[element]
	if !ok {
		t.Fatalf("в сети нет элемента %s", element)
	}
	center, err := el.Prof.UToS(metres(at))
	if err != nil {
		t.Fatalf("перевод u в s: %v", err)
	}
	half := metres(length / 2)
	sp := Span{{Element: element, From: center - half, To: center + half, Direction: dir}}
	if err := sp.Connected(net); err != nil {
		t.Fatalf("фикстурный отрезок несвязен: %v", err)
	}
	return sp
}

// slide — сдвинуть отрезок на d вдоль хода B → A (знак значим).
//
// Своя копия того, что делает физика (sim.shift), и это НЕ дублирование по
// недосмотру: проверять надо сам отрезок, а звать сюда физику нельзя — импорт
// идёт в другую сторону. Копия при этом короткая и без своих решений: она
// складывает наращивание с усечением, а всё содержательное лежит в них.
func slide(t *testing.T, sp Span, w Walk, d units.Distance) (Span, units.Distance) {
	t.Helper()
	grow, trim := Span.GrowA, Span.TrimB
	if d < 0 {
		grow, trim = Span.GrowB, Span.TrimA
		d = -d
	}
	grown, stuck, err := grow(sp, w, d)
	if err != nil {
		t.Fatalf("наращивание: %v", err)
	}
	got := d - stuck
	if got == 0 {
		return sp, 0
	}
	out, err := trim(grown, got)
	if err != nil {
		t.Fatalf("усечение: %v", err)
	}
	return out, got
}

func sameSpan(a, b Span) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestSpanReverseIsAnInvolution — развернуть дважды значит не трогать.
//
// СВОЙСТВО, а не пример, и оно несущее: наращивание с конца B и усечение с конца
// A написаны разворотом (Span.GrowB, Span.TrimA). Разворот, теряющий что-нибудь,
// терял бы это ровно при ходе назад — то есть в половине случаев и молча.
func TestSpanReverseIsAnInvolution(t *testing.T) {
	net := spanNet(t)
	w := spanWalk(net)
	rng := rand.New(rand.NewSource(spanSeed))
	sp := spanAt(t, net, seedmap.StationMain, 120, 34.18, netloc.DirForward)
	for i := range 200 {
		if got := sp.Reverse().Reverse(); !sameSpan(got, sp) {
			t.Fatalf("шаг %d: двойной разворот изменил отрезок:\n было %+v\n стало %+v", i, sp, got)
		}
		sp, _ = slide(t, sp, w, metres((rng.Float64()-0.5)*40))
	}
}

// TestSpanReverseKeepsLengthAndConnection — разворот не трогает ни длины, ни
// связности: он меняет только то, с какого конца отрезок читают.
func TestSpanReverseKeepsLengthAndConnection(t *testing.T) {
	net := spanNet(t)
	w := spanWalk(net)
	rng := rand.New(rand.NewSource(spanSeed))
	sp := spanAt(t, net, seedmap.StationMain, 40, 34.18, netloc.DirReverse)
	for i := range 200 {
		back := sp.Reverse()
		if back.Length() != sp.Length() {
			t.Fatalf("шаг %d: длина после разворота %s, была %s", i, back.Length(), sp.Length())
		}
		if err := back.Connected(net); err != nil {
			t.Fatalf("шаг %d: развёрнутый отрезок несвязен: %v", i, err)
		}
		// Концы обязаны поменяться местами: конец A развёрнутого — это конец B
		// исходного, и та же точка на той же координате.
		elA, sA, _, okA := back.PointAt(0)
		elB, sB, _, okB := sp.PointAt(sp.Length())
		if !okA || !okB || elA != elB || sA != sB {
			t.Fatalf("шаг %d: конец B развёрнутого (%s, %s) не совпал с концом A исходного (%s, %s)",
				i, elA, sA, elB, sB)
		}
		sp, _ = slide(t, sp, w, metres((rng.Float64()-0.5)*40))
	}
}

// TestSpanKeepsItsLength — тело не растягивается и не сжимается.
//
// Инвариант, из которого растёт всё остальное: точка отсчёта — это СЕРЕДИНА
// отрезка, и она означает то, что означает, только пока длина отрезка равна
// длине тела. Наращивание без усечения (или наоборот) даёт состав, растущий с
// каждым тиком, — и это не выдумка про запас, а первое, что выйдет при
// перестановке двух строк.
func TestSpanKeepsItsLength(t *testing.T) {
	net := spanNet(t)
	w := spanWalk(net)
	rng := rand.New(rand.NewSource(spanSeed))
	sp := spanAt(t, net, seedmap.StationMain, 120, 34.18, netloc.DirForward)
	want := sp.Length()
	for i := range 500 {
		sp, _ = slide(t, sp, w, metres((rng.Float64()-0.5)*60))
		if sp.Length() != want {
			t.Fatalf("шаг %d: длина отрезка стала %s, была %s", i, sp.Length(), want)
		}
		if err := sp.Connected(net); err != nil {
			t.Fatalf("шаг %d: отрезок разъехался: %v", i, err)
		}
	}
}

// TestSpanReturnsWhereItStarted — сдвиг туда и обратно возвращает на место.
//
// Свойство ловит то, чего не поймает ни один пример: потерю остатка на границе.
// Прежняя редакция движения переносила остаток пути через границу вручную, и
// цена ошибки в этом переносе — тихая потеря сантиметров на КАЖДОЙ границе,
// незаметная в одном проезде и накапливающаяся за прогон.
//
// Сдвиги нарочно берутся длиннее прохода стрелки (33.5 м): путь туда и обратно
// обязан быть обратимым и тогда, когда по дороге сменилось несколько элементов.
func TestSpanReturnsWhereItStarted(t *testing.T) {
	net := spanNet(t)
	w := spanWalk(net)
	rng := rand.New(rand.NewSource(spanSeed))
	start := spanAt(t, net, seedmap.StationMain, 40, 34.18, netloc.DirForward)
	for i := range 300 {
		d := metres(rng.Float64() * 80)
		there, got := slide(t, start, w, -d)
		back, gotBack := slide(t, there, w, got)
		if gotBack != got {
			t.Fatalf("шаг %d: туда прошли %s, обратно %s", i, got, gotBack)
		}
		if !sameSpan(back, start) {
			t.Fatalf("шаг %d: сдвиг на %s туда и обратно не вернул отрезок:\n было %+v\n стало %+v",
				i, got, start, back)
		}
	}
}

// TestSpanGrowsThroughThePort — тело переходит границу и лежит на двух
// элементах сразу, а порты сходятся.
//
// Это ровно то, чего не умела точка: у машины 34.18 м, поставленной серединой на
// u = 10 главного пути, хвост длиной 7.09 м лежит на проходе стрелки. До отрезка
// такого состояния не существовало вовсе — расстановка его отвергала, а физика
// делала вид, что конец машины в полудлине от середины.
func TestSpanGrowsThroughThePort(t *testing.T) {
	net := spanNet(t)
	w := spanWalk(net)
	// Ставим целиком на главном пути и сдвигаем назад так, чтобы хвост ушёл на
	// стрелку: у машины конец B смотрит против роста u (Direction forward), то
	// есть уходит к порту SW1.S.
	sp := spanAt(t, net, seedmap.StationMain, 20, 34.18, netloc.DirForward)
	sp, got := slide(t, sp, w, -metres(10))
	if got != metres(10) {
		t.Fatalf("сдвинулись на %s вместо 10 м", got)
	}
	if len(sp) != 2 {
		t.Fatalf("визитов %d, ожидалось два: %+v", len(sp), sp)
	}
	if err := sp.Connected(net); err != nil {
		t.Fatalf("отрезок через границу несвязен: %v", err)
	}
	tail, head := sp[0], sp[1]
	passage := seedmap.StationSW1 + mapfmt.PassageStraight
	if tail.Element != passage {
		t.Fatalf("хвост лёг на %s, а ехали через прямой проход %s", tail.Element, passage)
	}
	if head.Element != seedmap.StationMain {
		t.Fatalf("голова ушла с главного пути на %s", head.Element)
	}
	// Порты сходятся: выход хвостового визита есть вход головного.
	_, exit := VisitPorts(tail, net.Elements[tail.Element])
	entry, _ := VisitPorts(head, net.Elements[head.Element])
	if exit != entry || exit != seedmap.StationSW1+".S" {
		t.Fatalf("визиты сошлись в %s / %s, а стрелка отдаёт главный путь портом %s.S",
			exit, entry, seedmap.StationSW1)
	}
	// Длина цела, и она распределена между двумя элементами.
	if sp.Length() != metres(34.18) {
		t.Fatalf("длина через границу %s", sp.Length())
	}
	if tail.To-tail.From == 0 || head.To-head.From == 0 {
		t.Fatalf("один из визитов пустой: %+v", sp)
	}
}

// TestSpanStopsAtTheDeadEnd — у упора наращивание отдаёт непройденный остаток, и
// конец отрезка встаёт РОВНО В ПОРТУ.
//
// Утверждение не знает ни полудлины, ни числа элементов под телом: оно про
// конец, а не про середину, и потому переживёт состав.
func TestSpanStopsAtTheDeadEnd(t *testing.T) {
	net := spanNet(t)
	w := spanWalk(net)
	el := net.Elements[seedmap.StationMain]
	sp := spanAt(t, net, seedmap.StationMain, 200, 34.18, netloc.DirForward)
	room := el.LengthS - sp[0].To
	sp, got := slide(t, sp, w, room+metres(50))
	if got != room {
		t.Fatalf("прошли %s, а до упора было %s", got, room)
	}
	if len(sp) != 1 || sp[0].To != el.LengthS {
		t.Fatalf("конец отрезка встал на %+v, а порт на %s", sp, el.LengthS)
	}
	// Дальше не двигается вовсе: упор — это покой, а не колебание.
	again, more := slide(t, sp, w, metres(50))
	if more != 0 || !sameSpan(again, sp) {
		t.Fatalf("у упора отрезок всё-таки сдвинулся на %s: %+v", more, again)
	}
}

// TestSpanFollowsTheSwitch — куда лёг хвост, решает ПОЛОЖЕНИЕ ОСТРЯКА, и
// записывается это положением В ТОТ МИГ, когда конец переходил границу.
//
// Вторая половина проверки — та, ради которой занятость и заводят: остряк,
// переставленный ПОСЛЕ перехода, отрезка не меняет. Спрашивать сеть заново
// значило бы получить путь, по которому тело не ехало.
func TestSpanFollowsTheSwitch(t *testing.T) {
	net := spanNet(t)
	top := NewTopology(net)
	// Едем от стрелки SW1 по проходу к E_CROSS: боковое положение.
	diverging := top.At(func(string) string { return BranchDiverging })
	sp := spanAt(t, net, seedmap.StationApproach, 100, 34.18, netloc.DirForward)
	sp, _ = slide(t, sp, diverging, metres(30))
	if len(sp) < 2 {
		t.Fatalf("не дошли до стрелки: %+v", sp)
	}
	head := sp[len(sp)-1]
	if head.Element != seedmap.StationSW1+mapfmt.PassageDiverging {
		t.Fatalf("при боковом положении голова ушла на %s", head.Element)
	}

	// ОСТРЯК ПЕРЕСТАВЛЕН ПОД ТЕЛОМ. Отрезок обязан остаться тем, что записано.
	was := append(Span(nil), sp...)
	straight := top.At(func(string) string { return BranchStraight })
	if !sameSpan(was, sp) {
		t.Fatal("копия отрезка разошлась с оригиналом до всякого движения")
	}
	// Двигаемся ВПЕРЁД: голова идёт по новому положению, хвост остаётся там, где
	// проехал. Проверяем, что хвостовой визит не переписан на прямой проход.
	sp, _ = slide(t, sp, straight, metres(1))
	if sp[0].Element != was[0].Element {
		t.Fatalf("хвостовой визит переписан с %s на %s после перевода остряка",
			was[0].Element, sp[0].Element)
	}
	if err := sp.Connected(net); err != nil {
		t.Fatalf("отрезок после перевода остряка несвязен: %v", err)
	}
}

// TestSpanMiddleIsTheReferencePoint — середина отрезка и есть точка отсчёта.
//
// Проверяется независимым счётом: пока тело лежит на одном элементе, середина
// отрезка обязана совпасть с полусуммой концов. Совпадение на границе так не
// проверить — там полусумма координат разных элементов бессмысленна, — и именно
// поэтому середина считается ПО ДЛИНЕ, а не по координате.
func TestSpanMiddleIsTheReferencePoint(t *testing.T) {
	net := spanNet(t)
	sp := spanAt(t, net, seedmap.StationMain, 120, 34.18, netloc.DirForward)
	el, at, dir, ok := sp.Middle()
	if !ok {
		t.Fatal("середина не нашлась")
	}
	want := (sp[0].From + sp[0].To) / 2
	if el != seedmap.StationMain || at != want || dir != netloc.DirForward {
		t.Fatalf("середина (%s, %s, %s), ожидалась (%s, %s, forward)",
			el, at, dir, seedmap.StationMain, want)
	}
}

// TestSpanOverlapIsHalfOpen — касание концами наложением НЕ считается.
//
// Соглашение принято раньше и не здесь (ClearAhead-5zd), но цена ошибки в нём
// наступает именно здесь: при целых микрометрах равенство концов достижимо, и
// закрытый интервал объявил бы столкновением два тела, стоящих вплотную.
func TestSpanOverlapIsHalfOpen(t *testing.T) {
	a := Span{{Element: seedmap.StationMain, From: metres(10), To: metres(20),
		Direction: netloc.DirForward}}
	touching := Span{{Element: seedmap.StationMain, From: metres(20), To: metres(30),
		Direction: netloc.DirForward}}
	if _, _, ok := a.Overlaps(touching); ok {
		t.Fatal("тела, стоящие вплотную, объявлены наложившимися")
	}
	biting := Span{{Element: seedmap.StationMain, From: metres(19.999999), To: metres(30),
		Direction: netloc.DirForward}}
	el, at, ok := a.Overlaps(biting)
	if !ok {
		t.Fatal("наложение в микрометр не замечено")
	}
	if el != seedmap.StationMain || at.From != metres(19.999999) || at.To != metres(20) {
		t.Fatalf("наложение названо как (%s, %s..%s)", el, at.From, at.To)
	}
	// Разные элементы не накладываются никогда, как бы ни совпадали координаты.
	elsewhere := Span{{Element: seedmap.StationSiding, From: metres(10), To: metres(20),
		Direction: netloc.DirForward}}
	if _, _, ok := a.Overlaps(elsewhere); ok {
		t.Fatal("совпадение координат на РАЗНЫХ элементах объявлено наложением")
	}
}

// TestSpanGoesOnTheWireInU — на провод отрезок уезжает в u и в том же порядке.
//
// Правило провода: s не покидает сервера (netloc, запрет 1). Проверяется на
// самой проекции, а не на типе: тип легко потерять при сборке ответа.
func TestSpanGoesOnTheWireInU(t *testing.T) {
	net := spanNet(t)
	w := spanWalk(net)
	sp := spanAt(t, net, seedmap.StationMain, 20, 34.18, netloc.DirForward)
	sp, _ = slide(t, sp, w, -metres(10))
	wire, err := sp.ToU(net)
	if err != nil {
		t.Fatalf("проекция на провод: %v", err)
	}
	if len(wire) != len(sp) {
		t.Fatalf("визитов на проводе %d, в отрезке %d", len(wire), len(sp))
	}
	var total float64
	for i, iv := range wire {
		if iv.Element != sp[i].Element || iv.Direction != sp[i].Direction {
			t.Fatalf("визит %d на проводе (%s, %s), в отрезке (%s, %s)",
				i, iv.Element, iv.Direction, sp[i].Element, sp[i].Direction)
		}
		if iv.From > iv.To {
			t.Fatalf("визит %d на проводе вывернут: [%v, %v]", i, iv.From, iv.To)
		}
		total += iv.To - iv.From
	}
	// Длина в u не обязана совпадать с длиной в s на уклоне; фикстура плоская,
	// поэтому здесь они сходятся — и это сверяется с ДОПУСКОМ, а не байт в байт.
	if want := sp.Length().Meters(); total < want-1e-6 || total > want+1e-6 {
		t.Fatalf("длина отрезка на проводе %.6f м, в s %.6f м", total, want)
	}
}

// TestSpanRefusesTheBrokenShape — валидатор отказывает, а не чинит.
func TestSpanRefusesTheBrokenShape(t *testing.T) {
	net := spanNet(t)
	cases := []struct {
		name string
		sp   Span
	}{
		{"пустой", Span{}},
		{"без направления", Span{{Element: seedmap.StationMain, From: 0, To: metres(10)}}},
		{"за пределами элемента", Span{{Element: seedmap.StationMain,
			From: 0, To: metres(1000), Direction: netloc.DirForward}}},
		{"элемента нет в сети", Span{{Element: "нет такого",
			From: 0, To: metres(10), Direction: netloc.DirForward}}},
		{"визиты не сходятся", Span{
			{Element: seedmap.StationMain, From: 0, To: metres(10), Direction: netloc.DirForward},
			{Element: seedmap.StationSiding, From: 0, To: metres(10), Direction: netloc.DirForward},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.sp.Connected(net); err == nil {
				t.Fatal("испорченный отрезок принят")
			}
		})
	}
}
