package track

import (
	"fmt"
	"math"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/seedmap"
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
	// ШЕСТЬ ЗАПИСЕЙ НА СТРЕЛКУ: два усовика, два контррельса и две грани
	// сердечника. Грани — не нитки, но записаны тем же видом, потому что
	// описываются тем же: отрезок вдоль прохода с выносом от его оси.
	if len(rg.TurnoutRails) != 6*len(m.Topology.Turnouts) {
		t.Fatalf("записей крестовины %d при %d стрелках — у каждой их шесть",
			len(rg.TurnoutRails), len(m.Topology.Turnouts))
	}
	tt := m.Construction.Types[0]
	dt := m.Construction.TurnoutTypes[0]
	half := tt.Gauge / 2
	hands := map[string]string{}
	for _, x := range m.Topology.Turnouts {
		hands[x.ID] = x.Hand
	}
	for _, r := range rg.TurnoutRails {
		// Внутренняя нитка прохода — та же, что у остряка и у крестовины.
		inner := -half
		straight := r.Element == r.Owner+mapfmt.PassageStraight
		if (hands[r.Owner] == mapfmt.HandRight) != straight {
			inner = +half
		}
		switch r.Kind {
		case FrogRailWing:
			// СНАРУЖИ внутренней нитки и дальше от оси, чем она.
			if math.Abs(r.Face) <= math.Abs(inner) {
				t.Fatalf("усовик %s: рабочая грань на %+.4f, нитка на %+.4f — усовик обязан лежать снаружи",
					r.Element, r.Face, inner)
			}
			if d := math.Abs(r.Face) - math.Abs(inner); math.Abs(d-dt.FrogSet.Flangeway) > 1e-9 {
				t.Fatalf("усовик %s: желоб %.4f, а норма %.4f", r.Element, d, dt.FrogSet.Flangeway)
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
			if r.Flare != 0 {
				t.Fatalf("у сердечника отгиб %g — отливка сплошная, раструба у неё нет", r.Flare)
			}
			continue
		default:
			t.Fatalf("нитка неизвестного вида %q", r.Kind)
		}
		switch r.Kind {
		case FrogRailWing:
			// УСОВИК САДИТСЯ КОНЦАМИ РОВНО НА НИТКУ. Он и есть она, отведённая
			// наружу перед сердечником: отдельной рейкой рядом усовик выглядел
			// двумя рельсами в ладони друг от друга (2026-08-16).
			if math.Abs(r.EndFace-inner) > 1e-9 {
				t.Fatalf("усовик %s: конец отгиба на %+.4f, а нитка на %+.4f — он обязан выходить из неё",
					r.Element, r.EndFace, inner)
			}
			if r.Flare <= 0 {
				t.Fatalf("усовик %s без отгиба: он не может начаться от нитки скачком", r.Element)
			}
		case FrogRailCheck:
			// У КОНТРРЕЛЬСА РАСТРУБ ШИРЕ РАБОЧЕГО ЖЕЛОБА, и раскрывается он ПРОЧЬ
			// от своей нитки: колесо входит в желоб с любой стороны.
			neighbour := -inner
			mid := math.Abs(r.Face - neighbour)
			end := math.Abs(r.EndFace - neighbour)
			if end <= mid {
				t.Fatalf("контррельс %s: желоб в раструбе %.4f не больше рабочего %.4f",
					r.Element, end, mid)
			}
		}
		if r.To <= r.From {
			t.Fatalf("нитка %s %s вырождена: [%g, %g]", r.Kind, r.Element, r.From, r.To)
		}
		if 2*r.Flare > r.To-r.From+1e-9 {
			t.Fatalf("нитка %s %s: два отгиба по %g не помещаются в %g",
				r.Kind, r.Element, r.Flare, r.To-r.From)
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
	m := seedmap.Station(seedmap.Mutate(func(m *mapfmt.Map) {
		m.Construction.TurnoutTypes[0].FrogSet.CheckLength = 100
		m.Construction.TurnoutTypes[0].FrogSet.WingLength = 100
	}))
	els, rg, err := Compile(m)
	if err != nil {
		t.Fatalf("компиляция: %v", err)
	}
	for _, r := range rg.TurnoutRails {
		el, ok := els.Elements[r.Element]
		if !ok {
			t.Fatalf("элемент %s не скомпилирован", r.Element)
		}
		length := el.LengthU.Meters()
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
	castings := map[string]RenderTurnoutRail{}
	wings := map[string]RenderTurnoutRail{}
	for _, r := range rg.TurnoutRails {
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
	for _, g := range rg.TurnoutRailGaps {
		if g.Kind != GapCasting {
			continue
		}
		c, ok := castings[g.Element]
		if !ok {
			t.Fatalf("разрыв под сердечником на %s, где грани сердечника нет", g.Element)
		}
		// РАЗРЫВ КОРОЧЕ ОТЛИВКИ РОВНО НА НАХЛЁСТ с каждого конца. Встык торец
		// нитки остался бы открытым и виден срезом; довод и число — у
		// CastingOverlap.
		// Разрыв идёт от начала усовика до хвоста отливки: на всём протяжении
		// нитку заменяет сперва отведённая она же, потом сердечник. Конец
		// сверяется с отливкой, начало — с усовиком.
		w, okw := wings[g.Element]
		if !okw {
			t.Fatalf("у прохода %s нет усовика, а разрыв под сердечник есть", g.Element)
		}
		// Встык: разрыв начинается ровно там, где начинается усовик, и кончается
		// там, где кончается сердечник. Нахлёста нет — он означал бы два полных
		// рельса в одном месте (модель CA-1/9-R65-v1, §Д).
		if math.Abs(g.From-w.From) > 1e-9 || math.Abs(g.To-c.To) > 1e-9 {
			t.Fatalf("проход %s: усовик от %.3f, сердечник до %.3f, а разрыв [%.3f, %.3f]",
				g.Element, w.From, c.To, g.From, g.To)
		}
		// ПЕРЕД ОСТРИЁМ СЕРДЕЧНИКА ЕГО МЕТАЛЛА НЕТ. Отливка клалась симметрично
		// точке пересечения и стояла там, где должно быть горло между усовиками.
		if math.Abs((c.From-frogU[g.Element])-FrogPointOffset) > 1e-9 {
			t.Fatalf("проход %s: сердечник начинается на %.3f при точке крестовины %.3f — остриё обязано отстоять на %.3f м",
				g.Element, c.From, frogU[g.Element], FrogPointOffset)
		}
		// Разрывается ВНУТРЕННЯЯ нитка — та, что образует крестовину и по которой
		// лежит грань отливки. Наружная под сердечник не попадает вовсе.
		if math.Abs(g.Offset-c.Face) > 1e-9 {
			t.Fatalf("проход %s: разорвана нитка на %+.4f, а сердечник смыкается с ниткой на %+.4f",
				g.Element, g.Offset, c.Face)
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
