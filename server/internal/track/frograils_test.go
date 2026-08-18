package track

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/seedmap"
	"github.com/shady2k/ClearAhead/server/internal/units"
)

// TestFrogRailsSitOnBothSidesOfTheWheel — АНАТОМИЯ КРЕСТОВИНЫ ЧИСЛАМИ.
//
// Усовик и контррельс держат колесо с ДВУХ сторон, и это не образ: усовик лежит
// СНАРУЖИ нитки, образующей крестовину, контррельс — ВНУТРИ противоположной. Обе
// нитки отделены от рабочих граней желобом, и желоб — норма ПТЭ, а не решение
// художника.
//
// Проверяется знаками и разностями, а не значениями: значения приходят из карты
// и проверены её валидатором.
func TestFrogRailsSitOnBothSidesOfTheWheel(t *testing.T) {
	m := seedmap.Station()
	_, rg, err := Compile(m)
	if err != nil {
		t.Fatalf("компиляция: %v", err)
	}
	// ВОСЕМЬ ЗАПИСЕЙ НА СТРЕЛКУ: усовиков два, но каждый записан ДВУМЯ половинами
	// — до горла и за ним, на разных проходах, — плюс два контррельса и две грани
	// сердечника. Грани — не нитки, но записаны тем же видом, потому что
	// описываются тем же: отрезок вдоль прохода с выносом от его оси.
	//
	// Было шесть, пока усовик считался ниткой своего прохода во всю длину. Такой
	// усовик пересекался со вторым (замер — в frograils.go), и число выросло вместе
	// с починкой: одна деталь, лежащая на двух осях, записывается двумя отрезками.
	frogRails := 0
	for _, r := range rg.Rails {
		if r.Kind == FrogRailWing || r.Kind == FrogRailCheck || r.Kind == FrogRailCasting {
			frogRails++
		}
	}
	if frogRails != 8*len(m.Topology.Turnouts) {
		t.Fatalf("записей крестовины %d при %d стрелках — у каждой их восемь",
			frogRails, len(m.Topology.Turnouts))
	}
	tt := m.Construction.Types[0]
	dt := seedmap.TurnoutProjectForTest()
	half := tt.Gauge / 2
	hands := map[string]string{}
	for _, x := range m.Topology.Turnouts {
		hands[x.ID] = x.Hand
	}
	for _, r := range rg.Rails {
		if r.Kind != FrogRailWing && r.Kind != FrogRailCheck && r.Kind != FrogRailCasting {
			continue
		}
		// Внутренняя нитка прохода — та же, что у остряка и у крестовины.
		inner := -half
		straight := r.Element == r.Owner+mapfmt.PassageStraight
		if (hands[r.Owner] == mapfmt.HandRight) != straight {
			inner = +half
		}
		switch r.Kind {
		case FrogRailWing:
			// За горлом грань обязана держать рабочий желоб по всей прямой части.
			// У приходящей половины Face — координата шва уже на ЧУЖОМ проходе;
			// сравнивать её с ниткой своего прохода как поперечный размер нельзя.
			if strings.Contains(r.ID, "from_throat") {
				if d := math.Abs(math.Abs(r.Face) - math.Abs(inner)); math.Abs(d-dt.FrogSet.Flangeway) > 1e-9 {
					t.Fatalf("усовик %s: желоб %.4f, а норма %.4f", r.ID, d, dt.FrogSet.Flangeway)
				}
			} else if len(r.Plan) < 2 || r.Plan[0].Face != inner {
				t.Fatalf("усовик %s не выходит из своей нитки: план %+v", r.ID, r.Plan)
			}
		case FrogRailCheck:
			// ВНУТРИ противоположной нитки: ближе к оси, чем она.
			opp := -inner
			if math.Abs(r.Face) >= math.Abs(opp) {
				t.Fatalf("контррельс %s: рабочая грань на %+.4f, нитка на %+.4f — контррельс лежит внутри колеи",
					r.Element, r.Face, opp)
			}
			if math.Signbit(r.Face) != math.Signbit(opp) {
				t.Fatalf("контррельс %s оказался по другую сторону оси: %+.4f против нитки %+.4f",
					r.Element, r.Face, opp)
			}
			if d := math.Abs(opp) - math.Abs(r.Face); math.Abs(d-dt.FrogSet.CheckFlangeway) > 1e-9 {
				t.Fatalf("контррельс %s: желоб %.4f, а норма %.4f", r.Element, d, dt.FrogSet.CheckFlangeway)
			}
		case FrogRailCasting:
			// Грань сердечника лежит РОВНО ПО НИТКЕ: отливка есть продолжение
			// обеих ниток, и отступать ей от них некуда.
			if math.Abs(r.Face-inner) > 1e-9 {
				t.Fatalf("грань сердечника на %+.4f, а нитка на %+.4f", r.Face, inner)
			}
			if r.FlareFrom != 0 || r.FlareTo != 0 {
				t.Fatalf("у сердечника отгибы %g и %g — отливка сплошная, раструба у неё нет",
					r.FlareFrom, r.FlareTo)
			}
			continue
		default:
			t.Fatalf("нитка неизвестного вида %q", r.Kind)
		}
		switch r.Kind {
		case FrogRailWing:
			// УСОВИК САДИТСЯ ВНЕШНИМ КОНЦОМ РОВНО НА НИТКУ. Он и есть она, отведённая
			// перед сердечником: отдельной рейкой рядом усовик выглядел двумя рельсами
			// в ладони друг от друга (2026-08-16).
			//
			// ВНЕШНИЙ КОНЕЦ У ПОЛОВИН РАЗНЫЙ: до горла это НАЧАЛО детали, за горлом —
			// её КОНЕЦ, и грани там свои. Второй конец каждой половины смотрит в
			// горло, и садиться на нитку ему нельзя — там он смыкается со второй
			// половиной.
			outer := r.EndFaceFrom
			if strings.Contains(r.ID, "from_throat") {
				outer = r.EndFaceTo
			}
			if math.Abs(outer-inner) > 1e-9 {
				t.Fatalf("усовик %s: конец отгиба на %+.4f, а нитка на %+.4f — он обязан выходить из неё",
					r.ID, outer, inner)
			}
		case FrogRailCheck:
			// У КОНТРРЕЛЬСА РАСТРУБ ШИРЕ РАБОЧЕГО ЖЕЛОБА, и раскрывается он ПРОЧЬ
			// от своей нитки: колесо входит в желоб с любой стороны.
			neighbour := -inner
			mid := math.Abs(r.Face - neighbour)
			end := math.Abs(r.EndFaceFrom - neighbour)
			if end <= mid {
				t.Fatalf("контррельс %s: желоб в раструбе %.4f не больше рабочего %.4f",
					r.Element, end, mid)
			}
		}
		if r.To <= r.From {
			t.Fatalf("нитка %s %s вырождена: [%g, %g]", r.Kind, r.Element, r.From, r.To)
		}
		if r.FlareFrom+r.FlareTo > r.To-r.From+1e-9 {
			t.Fatalf("нитка %s %s: отгибы %g и %g не помещаются в %g",
				r.Kind, r.Element, r.FlareFrom, r.FlareTo, r.To-r.From)
		}
		if r.Grow != 1 && r.Grow != -1 {
			t.Fatalf("нитка %s %s: сторона роста сечения %g", r.Kind, r.Element, r.Grow)
		}
	}
}

// TestFrogRailsAreClippedToTheirPassage — нитка не вылезает за проход.
//
// Длины приходят от ТИПА ПУТИ, а проход — от геометрии устройства, и связи
// между ними нет: контррельс в 4.5 м у короткого перевода не поместится.
// Обрезка объявлена (frograils.go) и проверяется здесь, потому что без неё
// клиент откладывал бы нитку за концом элемента — то есть на соседнем пути.
func TestFrogRailsAreClippedToTheirPassage(t *testing.T) {
	m := seedmap.Station()
	_, rg, err := Compile(m)
	if err != nil {
		t.Fatalf("компиляция: %v", err)
	}
	_, els, err := Propagate(m)
	if err != nil {
		t.Fatalf("распространение поз: %v", err)
	}
	// ПРОЕКТ ПОРТИТСЯ У ВЫЗОВА, А НЕ В КАРТЕ. До 2026-08-17 длины комплекта
	// лежали в карте, и тест правил их фикстурой; каталог уехал в код сервера, и
	// карта до него больше не достаёт. Портится копия записи, и построитель
	// зовётся напрямую — проверяется ровно его обрезка.
	dt := seedmap.TurnoutProjectForTest()
	dt.FrogSet.CheckLength = 100
	dt.FrogSet.CoreLength = 100
	dt.FrogSet.WingApproach = 100
	dt.FrogSet.WingFlare = 100
	types := map[string]mapfmt.TrackType{}
	for i := range m.Construction.Types {
		types[m.Construction.Types[i].ID] = m.Construction.Types[i]
	}
	frogs := map[string]RenderFeature{}
	for _, f := range rg.Features {
		if f.Kind == FeatureFrog {
			frogs[f.Owner] = f
		}
	}
	var rails []RenderRailPart
	for _, x := range m.Topology.Turnouts {
		got, err := frogRails(els, types, m.Construction, x, frogs[x.ID], dt)
		if err != nil {
			t.Fatalf("нитки крестовины %s: %v", x.ID, err)
		}
		rails = append(rails, got...)
	}
	for _, r := range rails {
		el, ok := els[r.Element]
		if !ok {
			t.Fatalf("элемент %s не скомпилирован", r.Element)
		}
		length := el.Plan.Length().Meters()
		if r.From < -1e-9 || r.To > length+1e-9 {
			t.Fatalf("нитка %s на [%g, %g] при длине прохода %g", r.Kind, r.From, r.To, length)
		}
	}
}

// TestCastingLeavesNoRailUnderIt — ПОД СЕРДЕЧНИКОМ НИТКИ НЕТ.
//
// Отливка заводилась перемычкой МЕЖДУ гранями, но сами нитки под ней оставались
// и по-прежнему шли сквозь друг друга: сердечник лёг поверх пересечения, а не
// вместо него. В кадре это семь тел в пятне четыре метра — то, что владелец
// назвал кашей.
//
// Проверяется совпадение разрыва с отливкой ЧИСЛОМ В ЧИСЛО: длину отливки
// считает setSpan, и разойдись с ней разрыв — под краем сердечника торчал бы
// обрубок нитки либо зияла бы щель.
func TestCastingLeavesNoRailUnderIt(t *testing.T) {
	m := seedmap.Station()
	_, rg, err := Compile(m)
	if err != nil {
		t.Fatalf("компиляция: %v", err)
	}
	castings := map[string]RenderRailPart{}
	wings := map[string]RenderRailPart{}
	for _, r := range rg.Rails {
		switch r.Kind {
		case FrogRailCasting:
			castings[r.Element] = r
		case FrogRailWing:
			wings[r.Element] = r
		}
	}
	// Математическая точка крестовины на каждом проходе: её считает особенность,
	// и второго её вычисления тест не заводит.
	frogU := map[string]float64{}
	for _, f := range rg.Features {
		if f.Kind != FeatureFrog {
			continue
		}
		for _, a := range f.Addresses {
			frogU[a.Element] = a.U
		}
	}
	if len(castings) != 2*len(m.Topology.Turnouts) {
		t.Fatalf("граней сердечника %d при %d стрелках — по одной на каждый проход",
			len(castings), len(m.Topology.Turnouts))
	}
	got := 0
	for element, c := range castings {
		// РАЗРЫВ КОРОЧЕ ОТЛИВКИ РОВНО НА НАХЛЁСТ с каждого конца. Встык торец
		// нитки остался бы открытым и виден срезом; довод и число — у
		// CastingOverlap.
		// Разрыв идёт от начала усовика до хвоста отливки: на всём протяжении
		// нитку заменяет сперва отведённая она же, потом сердечник. Конец
		// сверяется с отливкой, начало — с усовиком.
		w, okw := wings[element]
		if !okw {
			t.Fatalf("у прохода %s нет усовика", element)
		}
		for _, r := range rg.Rails {
			if r.Element != element || (r.Kind != TurnoutRailStock && r.Kind != TurnoutRailClosure) {
				continue
			}
			if math.Abs(r.Face-c.Face) < 1e-9 && r.From < c.To && r.To > w.From {
				t.Fatalf("проход %s: физический рельс %s заходит под усовик/сердечник [%.3f, %.3f]",
					element, r.ID, w.From, c.To)
			}
		}
		// ПЕРЕД ОСТРИЁМ СЕРДЕЧНИКА ЕГО МЕТАЛЛА НЕТ. Отливка клалась симметрично
		// точке пересечения и стояла там, где должно быть горло между усовиками.
		if math.Abs((c.From-frogU[element])-FrogPointOffset) > 1e-9 {
			t.Fatalf("проход %s: сердечник начинается на %.3f при точке крестовины %.3f — остриё обязано отстоять на %.3f м",
				element, c.From, frogU[element], FrogPointOffset)
		}
		got++
	}
	if got != len(castings) {
		t.Fatalf("разрывов под сердечником %d при %d гранях — у каждой грани свой",
			got, len(castings))
	}
}

// TestCastingWidensAsTheMarkPrescribes — СЕРДЕЧНИК РАСШИРЯЕТСЯ ПО МАРКЕ.
//
// Ширина отливки на расстоянии s за остриём есть 2·s·tg(α/2), где α — угол
// крестовины: у марки 1/9 это примерно 0.111·s. Число не задаётся нигде — оно
// получается из расхождения самих ниток, — и проверка сверяет геометрию с
// маркой, объявленной в карте (role.frog), то есть два независимых источника.
//
// Заодно закрепляется ширина ОСТРИЯ: 9–12 мм у настоящей крестовины, и наши
// 0.10 м отступа дают ровно столько.
func TestCastingWidensAsTheMarkPrescribes(t *testing.T) {
	m := seedmap.Station()
	_, rg, err := Compile(m)
	if err != nil {
		t.Fatalf("компиляция: %v", err)
	}
	// Оси проходов берутся у Propagate: у скомпилированной сети планов нет, а
	// ширина отливки есть расстояние между нитками, то есть вопрос к плану.
	_, els, err := Propagate(m)
	if err != nil {
		t.Fatalf("развёртка: %v", err)
	}
	// Марка объявлена автором как «1/N»: тангенс угла крестовины равен 1/N.
	var n float64
	if _, err := fmt.Sscanf(m.Topology.Turnouts[0].Frog, "1/%g", &n); err != nil || n <= 0 {
		t.Fatalf("марка %q не читается как 1/N", m.Topology.Turnouts[0].Frog)
	}
	alpha := math.Atan(1 / n)
	want := 2 * math.Tan(alpha/2) // ширина на метр длины
	for _, f := range rg.Features {
		if f.Kind != FeatureFrog {
			continue
		}
		// Ширина отливки = расстояние между гранями, а грани идут по ниткам обоих
		// проходов. Меряется в двух сечениях, разнесённых на метр: разность и есть
		// прирост ширины.
		for _, s := range []float64{0.5, 1.5} {
			w, err := castingWidth(els, m, f, s)
			if err != nil {
				t.Fatalf("ширина сердечника на %.1f м: %v", s, err)
			}
			// ДЕЛИТСЯ НА РАССТОЯНИЕ ОТ ТОЧКИ ПЕРЕСЕЧЕНИЯ, а не на s. Ширина
			// меряется в сечении a.U + FrogPointOffset + s, то есть отступ острия
			// входит в плечо и обязан войти в делитель.
			//
			// До 2026-08-16 здесь стояло w/s, и тест проходил: ошибка гасилась
			// второй ошибкой. Боковой проход шёл ОДНОЙ дугой R=300 на всю длину,
			// крестовина попадала на кривую, и местный угол там был 0.1006 вместо
			// марочных 0.1107 — заниженный угол ровно компенсировал завышенный
			// делитель, и обе неправды укладывались в допуск. Эпюра выпрямила
			// геометрию, и арифметика вылезла.
			arm := FrogPointOffset + s
			if got := w / arm; math.Abs(got-want) > 0.02 {
				t.Fatalf("сердечник в %.1f м от точки пересечения шириной %.4f м (%.4f на метр), а марка %s требует %.4f",
					arm, w, got, m.Topology.Turnouts[0].Frog, want)
			}
		}
		// ОСТРИЁ: у самого начала отливки её ширина — миллиметры, а не сантиметры.
		w, err := castingWidth(els, m, f, 0)
		if err != nil {
			t.Fatalf("ширина острия: %v", err)
		}
		if w < 0.005 || w > 0.02 {
			t.Fatalf("остриё сердечника шириной %.4f м, у настоящей крестовины 9–12 мм", w)
		}
	}
}

// castingWidth — расстояние между гранями сердечника на s метров за остриём.
func castingWidth(els map[string]Element, m *mapfmt.Map, f RenderFeature, s float64) (float64, error) {
	if len(f.Addresses) != 2 {
		return 0, fmt.Errorf("адресов у крестовины %d", len(f.Addresses))
	}
	half := m.Construction.Types[0].Gauge / 2
	hand := m.Topology.Turnouts[0].Hand
	var pts [2][2]float64
	for i, a := range f.Addresses {
		el, ok := els[a.Element]
		if !ok {
			return 0, fmt.Errorf("проход %s не скомпилирован", a.Element)
		}
		// Внутренняя нитка — та же, что у остряка и у крестовины.
		in := -half
		straight := a.Element == f.Owner+mapfmt.PassageStraight
		if (hand == mapfmt.HandRight) != straight {
			in = +half
		}
		x, y := threadPointAt(el, in, a.U+FrogPointOffset+s)
		pts[i] = [2]float64{x, y}
	}
	return math.Hypot(pts[0][0]-pts[1][0], pts[0][1]-pts[1][1]), nil
}

// TestWingEndsAtTheCoreRoot — УСОВИК И СЕРДЕЧНИК КОНЧАЮТСЯ В ОДНОМ СЕЧЕНИИ.
//
// Так на схеме крестовины: желоба (3) идут вдоль ВСЕГО сердечника — от горла до
// его корня (5), — а не обрываются на полпути. Пока у усовика была своя длина,
// он кончался в 23.022 м при корне сердечника в 23.922 м: 0.900 м хвоста отливки
// оставались без желоба, то есть без наружной стенки под гребень колеса ровно
// там, где оно переходит с усовика на сердечник.
//
// Проверяется РАВЕНСТВО КОНЦОВ, а не длина усовика: длины у него больше нет, и
// проверка на неё вернула бы ту же свободу, из-за которой концы разъехались.
func TestWingEndsAtTheCoreRoot(t *testing.T) {
	m := seedmap.Station()
	_, rg, err := Compile(m)
	if err != nil {
		t.Fatalf("компиляция: %v", err)
	}
	seen := 0
	for _, turnout := range m.Topology.Turnouts {
		// По ЭЛЕМЕНТУ, а не по владельцу: у крестовины два усовика и две грани
		// отливки, по одной паре на каждый проход, и сравнивать надо свои со своими.
		wings := map[string]float64{}
		cores := map[string]float64{}
		for _, r := range rg.Rails {
			if r.Owner != turnout.ID {
				continue
			}
			switch r.Kind {
			case FrogRailWing:
				wings[r.Element] = r.To
			case FrogRailCasting:
				cores[r.Element] = r.To
			}
		}
		if len(wings) != 2 || len(cores) != 2 {
			t.Fatalf("стрелка %s: усовиков %d, граней сердечника %d — ожидалось по два",
				turnout.ID, len(wings), len(cores))
		}
		for element, wingEnd := range wings {
			coreEnd, ok := cores[element]
			if !ok {
				t.Fatalf("стрелка %s: у усовика на %s нет своей грани сердечника", turnout.ID, element)
			}
			if math.Abs(wingEnd-coreEnd) > 1e-9 {
				t.Fatalf("стрелка %s, проход %s: усовик кончается в %.3f м, корень сердечника в %.3f м",
					turnout.ID, element, wingEnd, coreEnd)
			}
			seen++
		}
	}
	if seen == 0 {
		t.Fatal("ни одного усовика не проверено — крестовин не нашлось")
	}
}

// TestWingHalvesMeetAtTheThroat — ПОЛОВИНЫ УСОВИКА СМЫКАЮТСЯ В ГОРЛЕ, хотя лежат
// на разных проходах.
//
// Усовик — один изогнутый рельс по краю крестовины: до горла он идёт по нитке
// одного прохода, за горлом — по нитке другого. Записан он двумя отрезками, и
// доказательство, что это ОДНА деталь, — совпадение их концов в мире. Угол в
// этой точке является настоящим изгибом усовика; сглаживать его биссектрисой
// нельзя, потому что рабочая грань за горлом должна остаться прямой.
//
// Меряется мир, а не u: у половин разные оси, и равенство координат прохода
// ничего бы не значило. Ровно на этом разъезде и держался прежний дефект —
// усовики, лежавшие каждый по своей нитке, пересекались на 22.83 м.
func TestWingHalvesMeetAtTheThroat(t *testing.T) {
	m := seedmap.Station()
	dt := seedmap.TurnoutProjectForTest()
	_, els, err := Propagate(m)
	if err != nil {
		t.Fatalf("развёртка: %v", err)
	}
	_, rg, err := Compile(m)
	if err != nil {
		t.Fatalf("компиляция: %v", err)
	}
	for _, turnout := range m.Topology.Turnouts {
		straight := turnout.ID + mapfmt.PassageStraight
		diverging := turnout.ID + mapfmt.PassageDiverging
		// Половина, приходящая в горло, кончается позже своего начала; уходящая —
		// начинается там же, где кончилась приходящая. Отличаются они тем, у какого
		// конца стоит отгиб: у приходящей он в начале, у уходящей в конце.
		ends := map[string][2]float64{}   // элемент -> (u конца приходящей, вынос)
		starts := map[string][2]float64{} // элемент -> (u начала уходящей, вынос)
		for _, r := range rg.Rails {
			if r.Owner != turnout.ID || r.Kind != FrogRailWing {
				continue
			}
			// ПОЛОВИНЫ РАЗЛИЧАЮТСЯ ИМЕНЕМ, А НЕ ТЕМ, У КАКОГО КОНЦА ОТГИБ. Признак
			// «отгиб в начале» держался ровно до тех пор, пока у половины за горлом
			// отгиба в начале не было; с горлом он там появился — она раскрывается с
			// половины горла до рабочего желоба, — и признак стал звать обе половины
			// приходящими.
			if strings.Contains(r.ID, "|to_throat") {
				ends[r.Element] = [2]float64{r.To, r.EndFaceTo}
			} else {
				starts[r.Element] = [2]float64{r.From, r.EndFaceFrom}
			}
		}
		if len(ends) != 2 || len(starts) != 2 {
			t.Fatalf("стрелка %s: половин усовиков %d приходящих и %d уходящих, ожидалось по две",
				turnout.ID, len(ends), len(starts))
		}
		gap := twoThreadDistance(t, els[straight], starts[straight][1], starts[straight][0],
			els[diverging], starts[diverging][1], starts[diverging][0])
		if math.Abs(gap-dt.FrogSet.Throat) > 1e-6 {
			t.Fatalf("стрелка %s: горло %.6f м, требуется %.6f м", turnout.ID, gap, dt.FrogSet.Throat)
		}
		// ПАРА КРЕСТ-НАКРЕСТ: приходящая по боковому продолжается уходящей по
		// прямому, и наоборот. В этом и состоит переход с нитки на нитку.
		for _, pair := range [][2]string{{diverging, straight}, {straight, diverging}} {
			in, out := ends[pair[0]], starts[pair[1]]
			d := twoThreadDistance(t, els[pair[0]], in[1], in[0], els[pair[1]], out[1], out[0])
			if d > 1e-6 {
				t.Fatalf("стрелка %s: половины усовика %s→%s разошлись на %.6f м",
					turnout.ID, pair[0], pair[1], d)
			}
		}
	}
}

// TestWingWorkingFaceIsStraight закрепляет требование ГОСТ 28370-89: боковая
// рабочая грань усовика напротив сердечника прямолинейна. Хвостовой отвод сюда
// не входит — он начинается после рабочего участка.
func TestWingWorkingFaceIsStraight(t *testing.T) {
	m := seedmap.Station()
	_, els, err := Propagate(m)
	if err != nil {
		t.Fatalf("развёртка: %v", err)
	}
	_, rg, err := Compile(m)
	if err != nil {
		t.Fatalf("компиляция: %v", err)
	}
	for _, r := range rg.Rails {
		if r.Kind != FrogRailWing || !strings.Contains(r.ID, "|from_throat") {
			continue
		}
		to := r.To - r.FlareTo
		x0, y0 := threadPointAt(els[r.Element], r.Face, r.From)
		x1, y1 := threadPointAt(els[r.Element], r.Face, to)
		dx, dy := x1-x0, y1-y0
		length := math.Hypot(dx, dy)
		for i := 1; i < 20; i++ {
			u := r.From + (to-r.From)*float64(i)/20
			x, y := threadPointAt(els[r.Element], r.Face, u)
			deviation := math.Abs(dx*(y-y0)-dy*(x-x0)) / length
			if deviation > 0.0015 {
				t.Fatalf("усовик %s: отклонение рабочей грани %.6f м в %.3f м, допускается 0.0015 м",
					r.ID, deviation, u)
			}
		}
	}
}

// TestWingIsBentNotBrokenAtTheThroat — УСОВИК ГНУТ, А НЕ СЛОМАН.
//
// Половины усовика лежат на разных проходах и сходятся в горле. До 2026-08-18
// они сходились ТОЧНО (зазор 0.000000 м), но под разными курсами: замер по ST_A
// назвал перелом — 5.175°, и на кадре это был клюв, которого нет ни на одной
// настоящей крестовине. Проверяется поэтому не смычка (её стережёт
// TestWingHalvesMeetAtTheThroat), а КУРС по обе стороны шва.
//
// Допуск в один градус — не «примерно ноль». Гиб выражен кубикой Эрмита между
// станциями плана, а не идеальной дугой, и в самом шве её касательная совпадает
// с биссектрисой лишь настолько, насколько сходится итерация наклонов. Допуск
// стережёт отсутствие разрыва курса и не закрепляет конкретный модельный радиус:
// тот принадлежит проекту крестовинного комплекта.
func TestWingIsBentNotBrokenAtTheThroat(t *testing.T) {
	m := seedmap.Station()
	_, els, err := Propagate(m)
	if err != nil {
		t.Fatalf("развёртка: %v", err)
	}
	_, rg, err := Compile(m)
	if err != nil {
		t.Fatalf("компиляция: %v", err)
	}
	// Курс рабочей грани в сечении: мир, а не ось прохода. У половин оси разные,
	// и равенство наклонов по своим осям означало бы разные курсы в мире —
	// ровно тот перелом, который чинится.
	course := func(r RenderRailPart, at float64) (float64, float64) {
		const h = 0.02
		el := els[r.Element]
		length := r.To - r.From
		a, b := at-h, at+h
		if a < 0 {
			a = 0
		}
		if b > length {
			b = length
		}
		x0, y0 := threadPointAt(el, planFaceAt(r, a), r.From+a)
		x1, y1 := threadPointAt(el, planFaceAt(r, b), r.From+b)
		n := math.Hypot(x1-x0, y1-y0)
		if n == 0 {
			t.Fatalf("усовик %s выродился в точку у %.3f м", r.ID, at)
		}
		return (x1 - x0) / n, (y1 - y0) / n
	}
	found := 0
	for _, turnout := range m.Topology.Turnouts {
		halves := map[string]RenderRailPart{}
		for _, r := range rg.Rails {
			if r.Owner != turnout.ID || r.Kind != FrogRailWing {
				continue
			}
			halves[r.ContinuousID+"|"+fmt.Sprint(r.ContinuousOrder)] = r
		}
		for key, in := range halves {
			if !strings.HasSuffix(key, "|1") {
				continue
			}
			out, ok := halves[strings.TrimSuffix(key, "|1")+"|2"]
			if !ok {
				t.Fatalf("у усовика %s нет половины за горлом", key)
			}
			ax, ay := course(in, in.To-in.From)
			bx, by := course(out, 0)
			angle := math.Abs(math.Atan2(ax*by-ay*bx, ax*bx+ay*by)) * 180 / math.Pi
			if angle > 1.0 {
				t.Fatalf("стрелка %s: усовик %s ломается в горле на %.3f°, а обязан гнуться",
					turnout.ID, key, angle)
			}
			found++
		}
	}
	if found != 2*len(m.Topology.Turnouts) {
		t.Fatalf("проверено %d швов при %d стрелках — усовиков на стрелке два",
			found, len(m.Topology.Turnouts))
	}
}

// TestFrogCoreClosesTheFlangewayFloor — ПОД ЖЕЛОБОМ ЕСТЬ МЕТАЛЛ.
//
// До 2026-08-18 отливка начиналась в острие, и между горлом и остриём зияла
// сквозная дыра шириной 62…103 мм: на кадре она читалась как выемка, и владелец
// спросил, как по ней поедет поезд. Ехать было по чему — колесо в этой зоне идёт
// по усовикам, — но металла под желобом не было вовсе.
//
// Проверяется три вещи: отливка начинается в горле, впереди острия у неё нет
// поверхности катания (там колесо на усовиках), и дно лежит на объявленной
// глубине желоба.
func TestFrogCoreClosesTheFlangewayFloor(t *testing.T) {
	m := seedmap.Station()
	dt := seedmap.TurnoutProjectForTest()
	_, rg, err := Compile(m)
	if err != nil {
		t.Fatalf("компиляция: %v", err)
	}
	if len(rg.FrogCores) != len(m.Topology.Turnouts) {
		t.Fatalf("отливок %d при %d стрелках", len(rg.FrogCores), len(m.Topology.Turnouts))
	}
	depth := dt.FrogSet.FlangewayDepth
	for _, core := range rg.FrogCores {
		// Длина — от горла до корня, то есть длина половины усовика за горлом.
		// Меньшая из двух половин: у отливки один конец, и станция не должна
		// выходить за конец той, что короче. Половины разной длины на доли
		// миллиметра — они лежат на разных проходах.
		wing := math.Inf(1)
		for _, r := range rg.Rails {
			if r.Owner == core.Owner && r.Kind == FrogRailWing && r.ContinuousOrder == 2 {
				if l := r.To - r.From; l < wing {
					wing = l
				}
			}
		}
		// Допуск, а не байт в байт: обе длины считаны из float64 разными путями,
		// и проект уже терял на побитовом сравнении эталон (CLAUDE.md).
		if math.Abs(core.Length-wing) > 1e-6 {
			t.Fatalf("стрелка %s: отливка длиной %.6f м при усовике за горлом %.6f м",
				core.Owner, core.Length, wing)
		}
		front, tip := 0, 0
		for _, st := range core.Stations {
			if len(st.Section) < 4 {
				t.Fatalf("стрелка %s: сечение отливки в %.3f м из %d точек", core.Owner, st.U, len(st.Section))
			}
			top := st.Section[0][1]
			for _, p := range st.Section {
				if p[1] > top {
					top = p[1]
				}
			}
			switch {
			case math.Abs(top) < 1e-9:
				// Поверхность катания: сечение достаёт до головки рельса.
				tip++
			case math.Abs(top+depth) < 1e-9:
				// Дно желоба: верх отливки утоплен ровно на глубину желоба.
				front++
			default:
				t.Fatalf("стрелка %s: верх отливки в %.3f м стоит на %.4f м, а бывает либо 0, либо −%.3f",
					core.Owner, st.U, top, depth)
			}
		}
		if front == 0 {
			t.Fatalf("стрелка %s: у отливки нет ни одной станции дна — выемка перед остриём вернулась", core.Owner)
		}
		if tip == 0 {
			t.Fatalf("стрелка %s: у отливки нет поверхности катания", core.Owner)
		}
	}
}

// TestWingApproachMeetsControlSections — ДВА КОНТРОЛЬНЫХ СЕЧЕНИЯ ПЕРЕДНЕГО ОТВОДА.
//
// ГОСТ 28370-89 задаёт переднюю геометрию усовиков не формой, а двумя замерами:
// в 50 мм впереди горла просвет между рабочими гранями равен горлу, в 400 мм —
// 65…85 мм. Отмеряются они ОТ ГОРЛА; привязка к математической точке, стоявшая
// здесь до 2026-08-18, геометрически невозможна — у острия сердечника просвет
// уже 103 мм, и сойтись до 62 мм за 150 мм усовики могли бы только уклоном 1:1.
//
// Правило стережёт длину подхода: пока она отмерялась от точки, подход выходил
// 0.74 м вместо 1.1 м и второе сечение давало 88.1 мм — усовик не успевал отойти
// от своей нитки, а нитки за эти 400 мм сходятся на 44 мм.
func TestWingApproachMeetsControlSections(t *testing.T) {
	m := seedmap.Station()
	dt := seedmap.TurnoutProjectForTest()
	_, els, err := Propagate(m)
	if err != nil {
		t.Fatalf("развёртка: %v", err)
	}
	_, rg, err := Compile(m)
	if err != nil {
		t.Fatalf("компиляция: %v", err)
	}
	for _, turnout := range m.Topology.Turnouts {
		wings := make([]RenderRailPart, 0, 4)
		for _, r := range rg.Rails {
			if r.Owner == turnout.ID && r.Kind == FrogRailWing {
				wings = append(wings, r)
			}
		}
		if len(wings) != 4 {
			t.Fatalf("стрелка %s: половин усовика %d, ожидалось четыре", turnout.ID, len(wings))
		}
		el, ok := els[turnout.ID+mapfmt.PassageStraight]
		if !ok {
			t.Fatalf("стрелка %s: прямой проход не скомпилирован", turnout.ID)
		}
		var apex float64
		for _, f := range rg.Features {
			if f.Kind == FeatureFrog && f.Owner == turnout.ID {
				for _, a := range f.Addresses {
					if a.Element == turnout.ID+mapfmt.PassageStraight {
						apex = a.U
					}
				}
			}
		}
		if apex == 0 {
			t.Fatalf("стрелка %s: крестовина не посчитана", turnout.ID)
		}
		// Горло ищется как самое узкое сечение, а не берётся из каталога: проверка
		// обязана мерить то, что построено, иначе она проверяет саму себя.
		throatU, throat := 0.0, math.Inf(1)
		for u := apex - 0.9; u <= apex; u += 0.002 {
			if g := wingGapAt(t, els, el, wings, u); g < throat {
				throat, throatU = g, u
			}
		}
		if math.Abs(throat-dt.FrogSet.Throat) > 0.001 {
			t.Fatalf("стрелка %s: самое узкое сечение %.4f м, а горло объявлено %.4f м",
				turnout.ID, throat, dt.FrogSet.Throat)
		}
		// ГДЕ САМО ГОРЛО. ГОСТ даёт рабочую зону усовика впереди математической
		// точки 350…420 мм, и горло обязано лечь в неё: место его не назначено, а
		// выведено из просвета, и разойдись оно с зоной — разошёлся бы и весь
		// передний отвод.
		if ahead := apex - throatU; ahead < 0.350 || ahead > 0.420 {
			t.Fatalf("стрелка %s: горло в %.3f м впереди точки, ожидается 0.350…0.420 м",
				turnout.ID, ahead)
		}
		if g := wingGapAt(t, els, el, wings, throatU-0.05); math.Abs(g-dt.FrogSet.Throat) > 0.002 {
			t.Fatalf("стрелка %s: в 50 мм впереди горла просвет %.4f м, а горло %.4f м",
				turnout.ID, g, dt.FrogSet.Throat)
		}
		// ДАЛЬНЕЕ СЕЧЕНИЕ ПРОВЕРЯЕТСЯ ТОЛЬКО СНИЗУ, и это честнее, чем видимость
		// полной проверки. ГОСТ даёт вилку 65…85 мм; замер по ST_A — 85.5 мм, то
		// есть на полмиллиметра шире верха. Подогнать не выйдет ничем, кроме места
		// горла: в этом сечении усовики ЛЕЖАТ НА СВОИХ НИТКАХ, и просвет там есть
		// чистое расхождение ниток — 0.111·(358 + 400) мм у марки 1/9. Двигать
		// горло ради вилки нельзя: его место выведено из горла 62 мм, а не выбрано.
		// Открытый вопрос — ClearAhead-vsfb; верх вилки ждёт текста нормы, а не
		// подгонки допуска.
		//
		// Снизу же проверка настоящая: просвет у́же 65 мм означал бы усовики,
		// сходящиеся навстречу колесу, — по такой крестовине не проехать.
		if g := wingGapAt(t, els, el, wings, throatU-0.40); g < 0.065 {
			t.Fatalf("стрелка %s: в 400 мм впереди горла просвет %.4f м, а норма не у́же 0.065 м",
				turnout.ID, g)
		}
	}
}

// wingGapAt — просвет между рабочими гранями двух усовиков в сечении прямого
// прохода. Меряется в мире и поперёк оси: половины лежат на разных проходах, и
// разность их u ничего не значила бы.
func wingGapAt(t *testing.T, els map[string]Element, el Element, wings []RenderRailPart, u float64) float64 {
	t.Helper()
	ax, ay := threadPointAt(el, 0, u)
	tg := tangentAt(el, u)
	nx, ny := -tg.Y, tg.X
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, r := range wings {
		for s := r.From; s <= r.To; s += 0.002 {
			x, y := threadPointAt(els[r.Element], planFaceAt(r, s-r.From), s)
			if math.Abs((x-ax)*tg.X+(y-ay)*tg.Y) < 0.001 {
				off := (x-ax)*nx + (y-ay)*ny
				lo, hi = math.Min(lo, off), math.Max(hi, off)
				break
			}
		}
	}
	if math.IsInf(lo, 1) || math.IsInf(hi, -1) {
		t.Fatalf("в сечении %.3f м усовиков нет — мерить нечего", u)
	}
	return hi - lo
}

// TestWingBendDoesNotNarrowTheFlangeway — ГИБ НЕ СЪЕДАЕТ ЖЕЛОБ.
//
// Дуга гиба срезает вершину ломаной, и сторона среза — вопрос не вкуса: пройди
// грань ЧЕРЕЗ вершину, и желоб у острия сердечника сузился бы против нормы ПТЭ.
// Замер 2026-08-18 назвал цену первой редакции — 41.6 мм вместо 46, то есть
// гребню стало бы тесно ровно там, где он переходит с усовика на сердечник.
//
// Проверяется по ЗАКОНУ ПЛАНА, а не по полю Face: форму строит план, и поле
// называет лишь прямой участок.
func TestWingBendDoesNotNarrowTheFlangeway(t *testing.T) {
	m := seedmap.Station()
	dt := seedmap.TurnoutProjectForTest()
	_, rg, err := Compile(m)
	if err != nil {
		t.Fatalf("компиляция: %v", err)
	}
	tt := m.Construction.Types[0]
	half := tt.Gauge / 2
	checked := 0
	for _, r := range rg.Rails {
		if r.Kind != FrogRailWing || r.ContinuousOrder != 2 {
			continue
		}
		length := r.To - r.From
		// Хвостовой отгиб не проверяется: он на то и отгиб, чтобы желоб раскрылся
		// и усовик сел на нитку.
		for i := 0; i <= 100; i++ {
			at := (length - r.FlareTo) * float64(i) / 100
			gap := math.Abs(math.Abs(planFaceAt(r, at)) - half)
			if gap < dt.FrogSet.Flangeway-1e-4 {
				t.Fatalf("усовик %s: желоб %.4f м в %.3f м от начала, норма %.4f м",
					r.ID, gap, at, dt.FrogSet.Flangeway)
			}
			checked++
		}
	}
	if checked == 0 {
		t.Fatal("половин усовика за горлом не нашлось — проверять нечего")
	}
}

// TestWingApproachBendsAreLocalized — ОТГИБЫ НЕ РАЗМАЗАНЫ ПО ВСЕМУ ПОДХОДУ.
//
// Одинаковые u с разными наклонами называют место гиба. Между такими местами
// наклон обязан равняться секущей на обоих концах: тогда кубика Эрмита вырождается
// точно в прямую, а не рисует длинную дугу перед крестовиной.
func TestWingApproachBendsAreLocalized(t *testing.T) {
	m := seedmap.Station()
	_, rg, err := Compile(m)
	if err != nil {
		t.Fatalf("компиляция: %v", err)
	}
	checked := 0
	for _, r := range rg.Rails {
		if r.Kind != FrogRailWing || r.ContinuousOrder != 1 {
			continue
		}
		bends, straights := 0, 0
		for i := 1; i < len(r.Plan); i++ {
			a, b := r.Plan[i-1], r.Plan[i]
			span := b.U - a.U
			if math.Abs(span) < 1e-12 {
				if math.Abs(b.Slope-a.Slope) > 1e-6 {
					bends++
				}
				continue
			}
			secant := (b.Face - a.Face) / span
			if math.Abs(a.Slope-secant) < 1e-9 && math.Abs(b.Slope-secant) < 1e-9 {
				straights++
			}
		}
		if bends < 1 {
			t.Fatalf("усовик %s: ни одного локального гиба в законе %+v", r.ID, r.Plan)
		}
		if straights < 2 {
			t.Fatalf("усовик %s: прямых участков %d, длинная кубика вернулась", r.ID, straights)
		}
		checked++
	}
	if checked != 2*len(m.Topology.Turnouts) {
		t.Fatalf("проверено подходов %d при %d стрелках", checked, len(m.Topology.Turnouts))
	}
}

// planFaceAt — вынос рабочей грани по присланному закону плана, кубикой Эрмита.
// Тот же закон читает клиент (track_build.gd::plan_face); проверка обязана
// смотреть на форму, которую ДЕЙСТВИТЕЛЬНО построят, а не на ту, что имелась в
// виду.
func planFaceAt(r RenderRailPart, at float64) float64 {
	if len(r.Plan) < 2 {
		return r.Face
	}
	for i := 1; i < len(r.Plan); i++ {
		a, b := r.Plan[i-1], r.Plan[i]
		if at > b.U && i < len(r.Plan)-1 {
			continue
		}
		h := b.U - a.U
		if h <= 0 {
			return b.Face
		}
		s := (at - a.U) / h
		return (2*s*s*s-3*s*s+1)*a.Face + (s*s*s-2*s*s+s)*h*a.Slope +
			(-2*s*s*s+3*s*s)*b.Face + (s*s*s-s*s)*h*b.Slope
	}
	return r.Face
}

func TestWingTailPlanIsMaterializedByServer(t *testing.T) {
	const (
		length   = 1.8
		start    = 1.55
		fromFace = 0.714
		toFace   = 0.760
	)
	plan := smoothTailPlan(length, 0, start, fromFace, fromFace, toFace, 0)
	if len(plan) < 20 {
		t.Fatalf("хвостовой отвод остался редкой ломаной: станций %d", len(plan))
	}
	if plan[0].U != 0 || plan[0].Face != fromFace || plan[0].Slope != 0 {
		t.Fatalf("начало прямой части изменилось: %+v", plan[0])
	}
	last := plan[len(plan)-1]
	if math.Abs(last.U-length) > 1e-12 || math.Abs(last.Face-toFace) > 1e-12 || math.Abs(last.Slope) > 1e-12 {
		t.Fatalf("конец отвода не совпал с ниткой касательно: %+v", last)
	}
	for i := 1; i < len(plan); i++ {
		if plan[i].U <= plan[i-1].U || plan[i].Face < plan[i-1].Face {
			t.Fatalf("станции хвостового отвода не монотонны: %d: %+v после %+v", i, plan[i], plan[i-1])
		}
	}
}

// TestWingsDoNotCrossEachOther — ДВА УСОВИКА НЕ ПЕРЕСЕКАЮТСЯ НИГДЕ.
//
// Прежняя раскладка — каждый усовик ниткой своего прохода — давала два рельса,
// проходящих друг сквозь друга: зазор между ними шёл +202 мм, +92 мм в горле,
// ноль на 22.83 м и −129 мм дальше. Проверка идёт по сечениям и требует, чтобы
// порядок ниток не менялся: усовик, лежащий выше, обязан лежать выше везде.
func TestWingsDoNotCrossEachOther(t *testing.T) {
	m := seedmap.Station()
	_, els, err := Propagate(m)
	if err != nil {
		t.Fatalf("развёртка: %v", err)
	}
	_, rg, err := Compile(m)
	if err != nil {
		t.Fatalf("компиляция: %v", err)
	}
	for _, turnout := range m.Topology.Turnouts {
		straight := turnout.ID + mapfmt.PassageStraight
		diverging := turnout.ID + mapfmt.PassageDiverging
		halves := map[string]RenderRailPart{}
		for _, r := range rg.Rails {
			if r.Owner != turnout.ID || r.Kind != FrogRailWing {
				continue
			}
			key := "in:" + r.Element
			if strings.Contains(r.ID, "|from_throat") {
				key = "out:" + r.Element
			}
			halves[key] = r
		}
		if len(halves) != 4 {
			t.Fatalf("стрелка %s: половин усовиков %d, ожидалось четыре", turnout.ID, len(halves))
		}
		// УСОВИК СОБИРАЕТСЯ КРЕСТ-НАКРЕСТ: приходящая половина одного прохода и
		// уходящая другого — это один рельс.
		wings := [2][2]RenderRailPart{
			{halves["in:"+diverging], halves["out:"+straight]},
			{halves["in:"+straight], halves["out:"+diverging]},
		}
		y := func(w [2]RenderRailPart, u float64) (float64, bool) {
			for _, h := range w {
				if u >= h.From && u <= h.To {
					return threadY(t, els[h.Element], h.Face, u), true
				}
			}
			return 0, false
		}
		from := math.Max(wings[0][0].From, wings[1][0].From)
		to := math.Min(wings[0][1].To, wings[1][1].To)
		var sign float64
		for step := 0; ; step++ {
			u := from + float64(step)*0.05
			if u > to {
				break
			}
			a, ok1 := y(wings[0], u)
			b, ok2 := y(wings[1], u)
			if !ok1 || !ok2 {
				continue
			}
			d := a - b
			if math.Abs(d) < 1e-9 {
				t.Fatalf("стрелка %s: усовики сошлись в точку в сечении %.3f м", turnout.ID, u)
			}
			if sign == 0 {
				sign = math.Copysign(1, d)
				continue
			}
			if math.Copysign(1, d) != sign {
				t.Fatalf("стрелка %s: усовики поменялись местами в сечении %.3f м (%.4f против %.4f)",
					turnout.ID, u, a, b)
			}
		}
		if sign == 0 {
			t.Fatalf("стрелка %s: ни одного сечения с двумя усовиками", turnout.ID)
		}
	}
}

// threadY — ордината нитки в сечении u.
func threadY(t *testing.T, el Element, face, u float64) float64 {
	t.Helper()
	d, err := units.MetersToDistance(u)
	if err != nil {
		t.Fatalf("координата %v: %v", u, err)
	}
	p, err := threadPoint(el, face, d)
	if err != nil {
		t.Fatalf("точка нитки на %v: %v", u, err)
	}
	return p.Y
}

// twoThreadDistance — расстояние между точками ДВУХ РАЗНЫХ ниток, каждая на своём
// элементе.
func twoThreadDistance(t *testing.T, a Element, faceA, uA float64, b Element, faceB, uB float64) float64 {
	t.Helper()
	da, err := units.MetersToDistance(uA)
	if err != nil {
		t.Fatalf("координата %v: %v", uA, err)
	}
	db, err := units.MetersToDistance(uB)
	if err != nil {
		t.Fatalf("координата %v: %v", uB, err)
	}
	pa, err := threadPoint(a, faceA, da)
	if err != nil {
		t.Fatalf("точка нитки: %v", err)
	}
	pb, err := threadPoint(b, faceB, db)
	if err != nil {
		t.Fatalf("точка нитки: %v", err)
	}
	return math.Hypot(pa.X-pb.X, pa.Y-pb.Y)
}
