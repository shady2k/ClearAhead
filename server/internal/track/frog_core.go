package track

import (
	"fmt"
	"math"
	"sort"

	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
)

// frog_core.go — ТЕЛО СЕРДЕЧНИКА: форму отливки задаёт сервер, а не показ.
//
// # Почему это переехало сюда
//
// Последнее место, где клиент выбирал форму детали. До 2026-08-17 сердечник
// строился так: клиент брал две ГРАНЕВЫЕ линии, считал ширину между ними и по
// пяти своим константам (CASTING_SHOULDER, CASTING_NECK, CASTING_NECK_SHARE,
// CASTING_SHOULDER_STEP, CASTING_TIP_M) выводил четыре уровня по высоте. То есть
// решал, какой толщины отливка под головой, где у неё шейка и насколько шире
// подошва — доменные решения о ДЕТАЛИ, принятые рендером.
//
// Владелец закрыл вопрос одной фразой: «делай свой профиль на сервере, у клиента
// ничего не должно быть, он тупой рендер».
//
// # Что теперь едет на провод
//
// СЕЧЕНИЕ НА КАЖДОЙ СТАНЦИИ — многоугольник, как у рельса, только меняющийся по
// длине. Клиенту остаётся протянуть его между станциями: ни одного числа о форме
// у него больше нет.
//
// Станций столько, сколько нужно излому: сечение меняется по длине НЕЛИНЕЙНО
// (ширина верха растёт с расхождением граней, а нижние уровни держат свои
// минимумы), и на двух станциях протяжка соврала бы у самого острия, где отливка
// тоньше всего.
//
// # Что здесь модель, а что вывод
//
// ВЫВОД — ширина верхней площадки: она равна расхождению рабочих граней двух
// проходов, то есть считается из марки и колеи, а не назначается.
//
// МОДЕЛЬ — четыре уровня по высоте и их минимальные ширины (CA-1/9-R65-v1 §Б).
// Числа пришли с клиента как есть и названы моделью: чертежа крестовинного
// комплекта у нас нет, проверить их нечем. Разница с прежним положением дел не в
// числах, а в том, что они теперь ОБЪЯВЛЕНЫ в одном месте и уезжают деталью.
//
// Наименьшая ширина острия — тоже модель: у настоящего сердечника остриё срезано
// до 9–12 мм, острее не бывает по технологии литья. Ноль вместо неё дал бы
// вырожденные треугольники в протяжке — их проверка оболочки уже ловила.

// Уровни тела сердечника: доли высоты рельса и ширины его головки.
//
// Глубина головной части 45 мм из 180 — четверть; шейка кончается на 135 мм —
// три четверти. Ширины: шейка не у́же 30 мм (0.4 головки), подошва не у́же 150 мм
// (две головки), а от ширины площадки шейка отступает на 45 мм (0.6 головки).
const (
	CoreShoulderDepth = 0.25
	CoreNeckDepth     = 0.75
	CoreNeckShare     = 0.40
	CoreShoulderStep  = 0.60
	// CoreTipWidth — наименьшая ширина верхней площадки, метры.
	CoreTipWidth = 0.009
	// CoreStationStep — шаг станций сечения вдоль отливки, метры. Двадцать
	// сантиметров: вчетверо мельче самого короткого участка модели наката
	// (0.80 м) и вдевятеро — длины отливки.
	CoreStationStep = 0.20
)

// buildTurnoutFrogCores кладёт в геометрию тело сердечника каждой стрелки.
//
// Зовётся ПОСЛЕ buildTurnoutFrogRails и читает её грани: отливка лежит между
// ними, и второй раз считать, где они, незачем — разошлись бы.
func buildTurnoutFrogCores(m *mapfmt.Map, els map[string]Element, rg *RenderGeometry) error {
	if m.Construction == nil {
		return nil
	}
	types := make(map[string]mapfmt.TrackType, len(m.Construction.Types))
	for i := range m.Construction.Types {
		types[m.Construction.Types[i].ID] = m.Construction.Types[i]
	}
	for _, t := range m.Topology.Turnouts {
		faces := make([]RenderRailPart, 0, 2)
		// Половины усовиков ЗА ГОРЛОМ: по ним считается дно желоба — отливка
		// доходит до их рабочих граней, потому что желоб и есть то, что между.
		wings := make([]RenderRailPart, 0, 2)
		for _, r := range rg.Rails {
			if r.Owner != t.ID {
				continue
			}
			if r.Kind == FrogRailCasting {
				faces = append(faces, r)
			}
			if r.Kind == FrogRailWing && r.ContinuousOrder == 2 {
				wings = append(wings, r)
			}
		}
		if len(faces) == 0 {
			// Крестовины нет — и тела у неё нет. Молчание то же, что у ниток
			// крестовины: чего измерить нечем, того не отдаём.
			continue
		}
		if len(faces) != 2 {
			return fmt.Errorf("track: стрелка %s: граней сердечника %d, а отливка лежит между двумя",
				mapfmt.Labeled(t.Name, t.ID), len(faces))
		}
		typ := t.Type
		if typ == "" {
			typ = m.Construction.DefaultType
		}
		tt, ok := types[typ]
		if !ok {
			return fmt.Errorf("track: стрелка %s: тип %q не разрешается — сердечник отмерить нечем",
				mapfmt.Labeled(t.Name, t.ID), typ)
		}
		if len(wings) != 2 {
			return fmt.Errorf("track: стрелка %s: половин усовика за горлом %d, а желоб образуют две",
				mapfmt.Labeled(t.Name, t.ID), len(wings))
		}
		project, err := mapfmt.TurnoutProjectByID(t.TurnoutType)
		if err != nil {
			return fmt.Errorf("track: стрелка %s: %w", mapfmt.Labeled(t.Name, t.ID), err)
		}
		core, err := frogCore(els, t, faces, wings, tt, project.FrogSet.FlangewayDepth)
		if err != nil {
			return fmt.Errorf("track: стрелка %s: %w", mapfmt.Labeled(t.Name, t.ID), err)
		}
		rg.FrogCores = append(rg.FrogCores, core)
	}
	return nil
}

// frogCore считает станции сечения одной отливки.
func frogCore(els map[string]Element, t mapfmt.Turnout, faces, wings []RenderRailPart,
	tt mapfmt.TrackType, flangewayDepth float64) (RenderFrogCore, error) {
	a, b := faces[0], faces[1]
	// Усовики называются по тем же проходам, что и грани: дно желоба меряется
	// поперёк, и перепутать стороны значило бы мерить наискось.
	wa, wb := wings[0], wings[1]
	if wa.Element != a.Element {
		wa, wb = wb, wa
	}
	if wa.Element != a.Element || wb.Element != b.Element {
		return RenderFrogCore{}, fmt.Errorf("усовики лежат на проходах %s и %s, а грани отливки на %s и %s",
			wings[0].Element, wings[1].Element, a.Element, b.Element)
	}
	spanA := a.To - a.From
	spanB := b.To - b.From
	if spanA <= 0 || spanB <= 0 {
		return RenderFrogCore{}, fmt.Errorf("грани сердечника вырождены: %.3f и %.3f м", spanA, spanB)
	}
	elA, okA := els[a.Element]
	elB, okB := els[b.Element]
	if !okA || !okB {
		return RenderFrogCore{}, fmt.Errorf("проходы %s и %s не скомпилированы", a.Element, b.Element)
	}
	head := tt.Rail.HeadWidth
	height := tt.Rail.Height
	if head <= 0 || height <= 0 {
		return RenderFrogCore{}, fmt.Errorf("у типа %s головка %.4f и высота %.4f", tt.ID, head, height)
	}
	// ОТЛИВКА НАЧИНАЕТСЯ В ГОРЛЕ, А НЕ В ОСТРИЕ (2026-08-18).
	//
	// До этой правки тело шло от острия, и между горлом и остриём оставалась
	// сквозная дыра шириной 62…103 мм — та самая выемка, о которую споткнулся
	// владелец: «как по ней поедет поезд?». Ехать есть по чему, колесо там идёт
	// по усовикам, но металла под желобом не было вовсе, и это видно.
	//
	// Длина берётся у УСОВИКОВ: их половины за горлом начинаются ровно в шве и
	// кончаются в корне, то есть очерчивают отливку с обоих концов. Второго ответа
	// на вопрос «докуда» не заводится — он уже дан там.
	frontA := wa.From
	frontB := wb.From
	spanWA := wa.To - frontA
	spanWB := wb.To - frontB
	if spanWA <= 0 || spanWB <= 0 {
		return RenderFrogCore{}, fmt.Errorf("усовики за горлом вырождены: %.3f и %.3f м", spanWA, spanWB)
	}
	length := math.Min(spanWA, spanWB)
	// Где на этой длине встаёт остриё: до него верхней площадки нет вовсе — там
	// желоб непрерывен поперёк, и колесо держат усовики.
	tip := a.From - frontA
	steps := int(math.Ceil(length/CoreStationStep)) + 1
	core := RenderFrogCore{
		Owner:    t.ID,
		Length:   length,
		Stations: make([]RenderCoreStation, 0, steps+2),
	}
	stations := make([]float64, 0, steps+2)
	for i := 0; i <= steps; i++ {
		stations = append(stations, length*float64(i)/float64(steps))
	}
	// Остриё — своя станция: там сечение меняется скачком (появляется площадка
	// катания), и попасть в него шагом сетки нельзя.
	if tip > 0 && tip < length {
		stations = append(stations, tip, tip-1e-4)
		sort.Float64s(stations)
	}
	for _, u := range stations {
		if u < 0 {
			continue
		}
		// ШИРИНА ВЕРХА — РАСХОЖДЕНИЕ РАБОЧИХ ГРАНЕЙ, то есть сама марка. Меряется
		// между точками ОБЕИХ граней: у отливки нет своей оси, она и есть то, что
		// между ними.
		width := 0.0
		hasTop := u >= tip
		if hasTop {
			du := u - tip
			ax, ay := threadPointAt(elA, a.Face, a.From+du*spanA/(length-tip))
			bx, by := threadPointAt(elB, b.Face, b.From+du*spanB/(length-tip))
			if math.IsNaN(ax) || math.IsNaN(bx) {
				return RenderFrogCore{}, fmt.Errorf("грань сердечника не встаёт на проход в %.3f м от острия", du)
			}
			width = math.Hypot(ax-bx, ay-by)
		}
		// ШИРИНА ДНА — просвет между рабочими гранями УСОВИКОВ: желоба кончаются
		// на них, и дно доходит ровно дотуда. Выводится, а не объявляется, — по
		// тому же доводу, что и ширина верха.
		wax, way := threadPointAt(elA, planFace(wa, u*spanWA/length), frontA+u*spanWA/length)
		wbx, wby := threadPointAt(elB, planFace(wb, u*spanWB/length), frontB+u*spanWB/length)
		if math.IsNaN(wax) || math.IsNaN(wbx) {
			return RenderFrogCore{}, fmt.Errorf("грань усовика не встаёт на проход в %.3f м от горла", u)
		}
		floor := math.Hypot(wax-wbx, way-wby)
		core.Stations = append(core.Stations, RenderCoreStation{
			U:       u,
			Section: coreSection(width, floor, head, height, flangewayDepth, hasTop),
		})
	}
	return core, nil
}

// planFace — вынос рабочей грани детали по присланному ей закону плана.
// Тот же закон читает клиент; отливка обязана мерить дно по той же грани, по
// которой построен усовик, иначе дно и желоб разъедутся.
func planFace(r RenderRailPart, at float64) float64 {
	if len(r.Plan) < 2 {
		return r.Face
	}
	for i := 1; i < len(r.Plan); i++ {
		p, q := r.Plan[i-1], r.Plan[i]
		if at > q.U && i < len(r.Plan)-1 {
			continue
		}
		h := q.U - p.U
		if h <= 0 {
			return q.Face
		}
		s := (at - p.U) / h
		return (2*s*s*s-3*s*s+1)*p.Face + (s*s*s-2*s*s+s)*h*p.Slope +
			(-2*s*s*s+3*s*s)*q.Face + (s*s*s-s*s)*h*q.Slope
	}
	return r.Face
}

// coreSection — сечение отливки при ширине верхней площадки width.
//
// Обход и оси те же, что у рельса (mapfmt.TrackRail.Section): x поперёк пути, y
// от поверхности катания вниз. Начало отсчёта x — СРЕДНЯЯ ЛИНИЯ отливки, потому
// что своей рабочей грани у неё две, и от какой из них мерить — вопрос без
// ответа. Сечение симметрично относительно неё.
func coreSection(width, floor, head, height, flangewayDepth float64, hasTop bool) [][2]float64 {
	top := math.Max(width, CoreTipWidth)
	// ДНО ЖЕЛОБА — уровень отливки, доходящий до граней усовиков. Оно не может
	// быть у́же самой отливки на этом уровне: металл не исчезает оттого, что
	// желоб узок.
	shoulder := math.Max(math.Max(head, top), floor)
	half := []float64{
		top / 2,
		shoulder / 2,
		math.Max(head*CoreNeckShare, top-head*CoreShoulderStep) / 2,
		math.Max(head*2, top+head) / 2,
	}
	depth := []float64{0, flangewayDepth, height * CoreNeckDepth, height}
	if !hasTop {
		// ВПЕРЕДИ ОСТРИЯ ПЛОЩАДКИ КАТАНИЯ НЕТ: там желоб идёт поперёк без разрыва,
		// колесо держат усовики, и верх отливки — само дно. Оставь мы здесь
		// площадку хотя бы наименьшей ширины, впереди острия выросло бы лезвие,
		// которого у крестовины не бывает.
		half = half[1:]
		depth = depth[1:]
	}
	// Обход: вниз по одной стороне, вверх по другой. Порядок точек — тот же, что
	// у рельса, и клиент протягивает сечение тем же способом, что рельсовое.
	out := make([][2]float64, 0, 2*len(half))
	for i := range half {
		out = append(out, [2]float64{half[i], -depth[i]})
	}
	for i := len(half) - 1; i >= 0; i-- {
		out = append(out, [2]float64{-half[i], -depth[i]})
	}
	return out
}
