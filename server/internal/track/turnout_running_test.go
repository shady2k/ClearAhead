package track

import (
	"math"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/seedmap"
	"github.com/shady2k/ClearAhead/server/internal/units"
)

// TestSwitchPanelHasExactlyFourRunningRails — ЧЕТЫРЕ НИТКИ В ЛЮБОМ СЕЧЕНИИ ДО
// КРЕСТОВИНЫ, и ни одна из них не появляется и не исчезает по дороге.
//
// Состав меняется, число — нет, и это и есть схема перевода:
//
//	до корня остряков   — два рамных рельса и два остряка между ними;
//	за корнем           — те же два рамных плюс два соединительных, начавшихся
//	                      в корнях;
//	за концом рамных    — четыре соединительных: каждый продолжает свой рамный.
//
// Проверка написана 2026-08-17 взамен прежней, которая ждала в сечении 7 м ТРИ
// нитки: у бокового маршрута наружной нитки там будто бы не было вовсе. Схема
// перевода это опровергла — нижний рамный рельс отгибается и сам ею становится,
// — а замер назвал цену ошибки: колея бокового сходила с 1520 мм до 1165 мм.
func TestSwitchPanelHasExactlyFourRunningRails(t *testing.T) {
	m := seedmap.Station()
	_, rg, err := Compile(m)
	if err != nil {
		t.Fatalf("компиляция: %v", err)
	}
	for _, turnout := range m.Topology.Turnouts {
		// Сечения выбраны по составу, а не через равные промежутки: 1 и 4 м — в
		// пределах остряков, 7 м — за корнем и до конца рамных, 12 и 18 м — за
		// рамными и до крестовины.
		for _, u := range []float64{1, 4, 7, 12, 18} {
			if got := runningRailCountAt(rg, turnout.ID, u); got != 4 {
				t.Fatalf("стрелка %s, сечение %.1f м: физических ходовых рельсов %d, ожидается 4",
					turnout.ID, u, got)
			}
		}
	}
}

func runningRailCountAt(rg *RenderGeometry, owner string, u float64) int {
	count := 0
	for _, b := range rg.TurnoutBlades {
		if b.Owner == owner && u >= 0 && u < b.Length {
			count++
		}
	}
	for _, r := range rg.Rails {
		if r.Owner != owner || (r.Kind != TurnoutRailStock && r.Kind != TurnoutRailClosure) {
			continue
		}
		if u >= r.From && u < r.To {
			count++
		}
	}
	return count
}

// TestStockRailsAreStraightAndCurved — РАМНЫХ РЕЛЬСА ДВА, И ОНИ РАЗНЫЕ: прямой
// лежит вдоль прямого прохода, криволинейный — вдоль бокового.
//
// Это и есть то, что видно на схеме: верхний рамный рельс идёт прямо во всю
// длину, нижний отгибается за корнем остряков и уходит наружной ниткой бокового
// пути. Прежняя редакция клала оба вдоль прямого прохода, и наружной нитке
// бокового приходилось начинаться в пустоте.
func TestStockRailsAreStraightAndCurved(t *testing.T) {
	m := seedmap.Station()
	_, rg, err := Compile(m)
	if err != nil {
		t.Fatalf("компиляция: %v", err)
	}
	for _, turnout := range m.Topology.Turnouts {
		straight := turnout.ID + mapfmt.PassageStraight
		diverging := turnout.ID + mapfmt.PassageDiverging
		ends := map[string]float64{}
		for _, r := range rg.Rails {
			if r.Owner != turnout.ID || r.Kind != TurnoutRailStock {
				continue
			}
			if r.From != 0 {
				t.Fatalf("стрелка %s: рамный рельс начинается в %.3f м, а не в острие", turnout.ID, r.From)
			}
			if _, seen := ends[r.Element]; seen {
				t.Fatalf("стрелка %s: на элементе %s два рамных рельса", turnout.ID, r.Element)
			}
			ends[r.Element] = r.To
		}
		if len(ends) != 2 || math.IsNaN(ends[straight]) {
			t.Fatalf("стрелка %s: рамные рельсы лежат на %v, ожидались прямой и боковой проходы",
				turnout.ID, ends)
		}
		if _, ok := ends[straight]; !ok {
			t.Fatalf("стрелка %s: прямого рамного рельса нет", turnout.ID)
		}
		if _, ok := ends[diverging]; !ok {
			t.Fatalf("стрелка %s: криволинейного рамного рельса нет", turnout.ID)
		}
		// ДЛИНА У НИХ ОДНА, И ОДИНАКОВОЕ u ЭТО И ЗНАЧИТ: координата прохода есть
		// длина дуги, поэтому рельс длиной 12.5 м кончается в одном и том же u на
		// любом из двух проходов. Прежняя редакция пересчитывала конец кривого
		// рельса пропорцией длин прохода — и промахивалась на 31 мм вдоль и на
		// 355 мм поперёк.
		if math.Abs(ends[straight]-ends[diverging]) > 1e-9 {
			t.Fatalf("стрелка %s: рамные рельсы кончаются в %.6f и %.6f м — это разные длины",
				turnout.ID, ends[straight], ends[diverging])
		}
	}
}

// TestCurvedStockRailMeetsItsClosureRail — СТЫК КРИВОГО РАМНОГО РЕЛЬСА С
// СОЕДИНИТЕЛЬНЫМ СХОДИТСЯ В ТОЧКУ.
//
// Тот самый стык, на который владелец показал 2026-08-17 словами «вот этот рельс
// — продолжение этого, а они у тебя разорваны». Тогда между ними было 0.3546 м:
// рамный рельс кончался на ПРЯМОМ проходе, а соединительный начинался на боковом,
// и общей точки у них не было по построению.
//
// Меряется не равенство u, а РАССТОЯНИЕ МЕЖДУ ТОЧКАМИ РАБОЧЕЙ ГРАНИ: два конца
// на разных элементах могут иметь одинаковое u и лежать в разных местах — ровно
// это и было дефектом.
func TestCurvedStockRailMeetsItsClosureRail(t *testing.T) {
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
		diverging := turnout.ID + mapfmt.PassageDiverging
		var stock, closure *RenderRailPart
		for i, r := range rg.Rails {
			if r.Owner != turnout.ID || r.Element != diverging {
				continue
			}
			switch {
			case r.Kind == TurnoutRailStock:
				stock = &rg.Rails[i]
			case r.Kind == TurnoutRailClosure && r.ID == turnout.ID+"|closure|diverging_outer":
				closure = &rg.Rails[i]
			}
		}
		if stock == nil || closure == nil {
			t.Fatalf("стрелка %s: на боковом проходе нет пары «рамный + соединительный»", turnout.ID)
		}
		if math.Abs(stock.Face-closure.Face) > 1e-9 {
			t.Fatalf("стрелка %s: рамный на выносе %.3f, соединительный на %.3f — это разные нитки",
				turnout.ID, stock.Face, closure.Face)
		}
		gap := threadDistance(t, els[diverging], stock.Face, stock.To, closure.From)
		// Допуск — микрометр: числа приходят счётом, и «ровно ноль» здесь
		// означало бы сравнение float64 байт в байт, чего проект не делает.
		if gap > 1e-6 {
			t.Fatalf("стрелка %s: между концом рамного рельса и началом соединительного %.4f м",
				turnout.ID, gap)
		}
	}
}

// threadDistance — расстояние между двумя точками ОДНОЙ нитки, взятыми по u.
func threadDistance(t *testing.T, el Element, face, u1, u2 float64) float64 {
	t.Helper()
	at := func(u float64) (float64, float64) {
		d, err := units.MetersToDistance(u)
		if err != nil {
			t.Fatalf("координата %v: %v", u, err)
		}
		p, err := threadPoint(el, face, d)
		if err != nil {
			t.Fatalf("точка нитки на %v: %v", u, err)
		}
		return p.X, p.Y
	}
	x1, y1 := at(u1)
	x2, y2 := at(u2)
	return math.Hypot(x2-x1, y2-y1)
}

// TestTurnoutDeclaresNoGaps — В СТРЕЛКЕ НЕТ ОБЪЯВЛЕННЫХ РАЗРЫВОВ.
//
// Единственный, который здесь заводился, говорил «до конца рамных рельсов
// наружную нитку бокового ведёт рамный рельс прямого пути». Замер его
// опроверг: колея бокового маршрута при этом сходила с 1520 мм до 1165 мм, а
// нитка начиналась в 0.3546 м вбок от металла. Разрыва нет, потому что нет
// пустоты: наружную нитку бокового ведёт СВОЙ рамный рельс, криволинейный.
//
// Проверка стоит не ради нуля, а ради ЗАПРЕТА НА ГЛУШИТЕЛЬ: объявленный разрыв
// снимает порт с проверки смычки (assembly.coveredByGap), и заведись он тут
// снова — сборка перестала бы видеть ровно то место, из-за которого всё это
// разбиралось.
func TestTurnoutDeclaresNoGaps(t *testing.T) {
	m := seedmap.Station()
	_, rg, err := Compile(m)
	if err != nil {
		t.Fatalf("компиляция: %v", err)
	}
	for _, g := range rg.RailGaps {
		for _, turnout := range m.Topology.Turnouts {
			if g.Owner == turnout.ID {
				t.Fatalf("стрелка %s объявила разрыв %s на [%.3f, %.3f]: %s",
					turnout.ID, g.Kind, g.From, g.To, g.Why)
			}
		}
	}
}

// TestClosureAndWingShareContinuousRail закрепляет физическую деталь, а не
// только совпадение координат: внутренний соединительный рельс переходит в
// передний отвод усовика и затем в его половину вдоль сердечника. Три записи
// нужны адресации, но три отдельных меша дают два ложных поперечных шва.
func TestClosureAndWingShareContinuousRail(t *testing.T) {
	m := seedmap.Station()
	_, rg, err := Compile(m)
	if err != nil {
		t.Fatalf("компиляция: %v", err)
	}
	for _, turnout := range m.Topology.Turnouts {
		groups := map[string]map[int]string{}
		for _, r := range rg.Rails {
			if r.Owner != turnout.ID || r.ContinuousID == "" {
				continue
			}
			if groups[r.ContinuousID] == nil {
				groups[r.ContinuousID] = map[int]string{}
			}
			if old := groups[r.ContinuousID][r.ContinuousOrder]; old != "" {
				t.Fatalf("стрелка %s: у непрерывного рельса %s порядок %d занят %s и %s",
					turnout.ID, r.ContinuousID, r.ContinuousOrder, old, r.ID)
			}
			groups[r.ContinuousID][r.ContinuousOrder] = r.Kind
		}
		if len(groups) != 2 {
			t.Fatalf("стрелка %s: непрерывных усовиков %d, ожидалось два", turnout.ID, len(groups))
		}
		for id, parts := range groups {
			if len(parts) != 3 || parts[0] != TurnoutRailClosure ||
				parts[1] != FrogRailWing || parts[2] != FrogRailWing {
				t.Fatalf("стрелка %s: непрерывный усовик %s собран как %v, ожидалось closure, wing, wing",
					turnout.ID, id, parts)
			}
		}
	}
}
