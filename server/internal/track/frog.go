package track

import (
	"fmt"
	"math"
	"sort"

	"github.com/shady2k/ClearAhead/server/internal/geom"
	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/units"
)

// buildFrogFeatures считает точку крестовины каждой стрелки (спека §5) и кладёт
// её в rg.Features. Особенность — уровень 2: один канонический ответ на вопрос
// «где крестовина». Два клиента, независимо ищущие пересечение офсетных кривых,
// дали бы два разных ответа — считать обязан сервер.
//
// Пересекаются две конкретные нитки из четырёх: при hand="right" прямой проход
// −gauge/2 с боковым +gauge/2, при hand="left" наоборот. Нормали левые,
// смещение по ним. Gauge берётся из типа САМОГО устройства, а не типов
// примыкающих путей (спека §4).
//
// switch_toe и switch_heel не отдаются: конец остряка сервер вывести не может
// (спека §5), действует клиентская эвристика. Без блока construction типов нет —
// и крестовин нет: особенность не вычисляется.
func buildFrogFeatures(m *mapfmt.Map, els map[string]Element, rg *RenderGeometry) error {
	c := m.Construction
	if c == nil {
		return nil
	}
	types := make(map[string]mapfmt.TrackType, len(c.Types))
	for i := range c.Types {
		types[c.Types[i].ID] = c.Types[i]
	}
	for _, t := range m.Topology.Turnouts {
		f, err := frogFeature(els, types, c, t)
		if err != nil {
			return fmt.Errorf("track: стрелка %s: %w", mapfmt.Labeled(t.Name, t.ID), err)
		}
		if f != nil {
			rg.Features = append(rg.Features, *f)
		}
	}
	sort.Slice(rg.Features, func(i, j int) bool { return lessFeature(rg.Features[i], rg.Features[j]) })
	return nil
}

// frogFeature считает крестовину одной стрелки. Все поля особенности —
// point, u обоих адресов, касательные — строятся здесь одной функцией и
// проверяются на согласованность (спека §5).
func frogFeature(els map[string]Element, types map[string]mapfmt.TrackType,
	c *mapfmt.Construction, t mapfmt.Turnout) (*RenderFeature, error) {
	typ := t.Type
	if typ == "" {
		typ = c.DefaultType
	}
	tt, ok := types[typ]
	if !ok {
		return nil, fmt.Errorf("тип %q не разрешается — колею для крестовины взять неоткуда", typ)
	}
	half := tt.Gauge / 2

	straightID := t.ID + mapfmt.PassageStraight
	divergingID := t.ID + mapfmt.PassageDiverging
	sEl, okS := els[straightID]
	dEl, okD := els[divergingID]
	if !okS || !okD {
		return nil, fmt.Errorf("проходы %s и %s не скомпилированы", mapfmt.Labeled(t.Name, straightID), mapfmt.Labeled(t.Name, divergingID))
	}

	// Какие две нитки пересекать (спека §5): правая стрелка — прямой проход
	// −gauge/2 × боковой +gauge/2, левая — наоборот. Знак смещения — по левой
	// нормали: боковой путь правой стрелки уходит вправо, его левая нитка идёт
	// навстречу правой нитке прямого пути.
	var deltaS, deltaD float64
	switch t.Hand {
	case "right":
		deltaS, deltaD = -half, +half
	case "left":
		deltaS, deltaD = +half, -half
	default:
		return nil, fmt.Errorf("рукость %q, ожидается right или left", t.Hand)
	}

	ints, tangent, err := threadIntersections(sEl, deltaS, dEl, deltaD)
	if err != nil {
		return nil, err
	}
	if tangent {
		return nil, fmt.Errorf("нитки касаются, не пересекаясь — касание без пересечения это отказ")
	}
	if len(ints) != 1 {
		return nil, fmt.Errorf("пересечений офсетных ниток %d, ожидалось ровно одно", len(ints))
	}
	cand := ints[0]
	ts := tangentAt(sEl, cand.uS)
	td := tangentAt(dEl, cand.uD)

	// Согласованность point с адресами (спека §5): точка обязана лежать на
	// обеих нитках в своих u. По построению это так, проверка ловит баги.
	if x, y := threadPointAt(sEl, deltaS, cand.uS); dist(x, y, cand.x, cand.y) > 1e-6 {
		return nil, fmt.Errorf("point не согласуется с адресом прямого прохода (u=%g, расхождение %g м)", cand.uS, dist(x, y, cand.x, cand.y))
	}
	if x, y := threadPointAt(dEl, deltaD, cand.uD); dist(x, y, cand.x, cand.y) > 1e-6 {
		return nil, fmt.Errorf("point не согласуется с адресом бокового прохода (u=%g, расхождение %g м)", cand.uD, dist(x, y, cand.x, cand.y))
	}
	return &RenderFeature{
		Owner: t.ID,
		Kind:  "frog",
		Point: RenderPoint{X: cand.x, Y: cand.y},
		// Порядок адресов: прямой проход, затем боковой (спека §5).
		Addresses: []RenderAddress{
			{Element: straightID, U: cand.uS, Tangent: RenderVec{X: ts.X, Y: ts.Y}},
			{Element: divergingID, U: cand.uD, Tangent: RenderVec{X: td.X, Y: td.Y}},
		},
	}, nil
}

// lessFeature — канонический порядок особенностей: (owner, kind, addresses),
// адреса лексикографически по (element, u) (спека §10).
func lessFeature(a, b RenderFeature) bool {
	if a.Owner != b.Owner {
		return a.Owner < b.Owner
	}
	if a.Kind != b.Kind {
		return a.Kind < b.Kind
	}
	for i := 0; i < len(a.Addresses) && i < len(b.Addresses); i++ {
		if a.Addresses[i].Element != b.Addresses[i].Element {
			return a.Addresses[i].Element < b.Addresses[i].Element
		}
		if a.Addresses[i].U != b.Addresses[i].U {
			return a.Addresses[i].U < b.Addresses[i].U
		}
	}
	return len(a.Addresses) < len(b.Addresses)
}

// threadIntersections находит все пересечения ниток двух проходов внутри
// доменов [0, U] обоих. Касание возвращается отдельным флагом: касательное
// касание без пересечения — отказ валидации (спека §5).
func threadIntersections(a Element, deltaA float64, b Element, deltaB float64) ([]intersection, bool, error) {
	segsA, err := threadSegments(a, deltaA)
	if err != nil {
		return nil, false, err
	}
	segsB, err := threadSegments(b, deltaB)
	if err != nil {
		return nil, false, err
	}
	var pts []intersection
	tangent := false
	for i := range segsA {
		for j := range segsB {
			got, tan, err := segIntersections(&segsA[i], &segsB[j])
			if err != nil {
				return nil, false, err
			}
			tangent = tangent || tan
			pts = append(pts, got...)
		}
	}
	// Одно пересечение ровно на стыке примитивов найдётся дважды (по разу от
	// каждой пары сегментов) — дедупликация обязательна, иначе канонический
	// ответ станет «пересечений два».
	pts = dedupIntersections(pts)
	return pts, tangent, nil
}

type intersection struct {
	uS, uD float64 // u на прямом и боковом проходах (метры)
	x, y   float64
}

// threadSeg — офсетная нитка одного примитива: прямая даёт параллельную
// прямую, дуга — концентрическую окружность. Центр окружности тот же, что у
// осевой дуги, радиус меняется на |delta|: смещение по левой нормали для
// положительного угла дуги идёт наружу, для отрицательного — внутрь.
type threadSeg struct {
	uFrom, uTo float64 // домен сегмента в u прохода
	line       bool
	px, py     float64 // точка прямой
	dx, dy     float64 // единичное направление прямой
	cx, cy     float64 // центр окружности
	r          float64 // радиус окружности
	aFrom      float64 // угол начала дуги от центра
	aSweep     float64 // знаковый размах угла дуги
}

func threadSegments(e Element, delta float64) ([]threadSeg, error) {
	out := make([]threadSeg, 0, len(e.Plan))
	u := 0.0
	pos := e.Start.Plan
	for _, p := range e.Plan {
		uTo := u + p.Length.Meters()
		seg := threadSeg{uFrom: u, uTo: uTo}
		nx, ny := -math.Sin(pos.Heading), math.Cos(pos.Heading) // левая нормаль
		switch p.Kind {
		case geom.KindStraight:
			seg.line = true
			seg.px, seg.py = pos.X+delta*nx, pos.Y+delta*ny
			seg.dx, seg.dy = math.Cos(pos.Heading), math.Sin(pos.Heading)
		case geom.KindArc:
			sign := math.Copysign(1, p.Angle)
			seg.cx, seg.cy = pos.X+sign*p.Radius*nx, pos.Y+sign*p.Radius*ny
			seg.r = p.Radius - delta*sign
			if seg.r <= 0 {
				return nil, fmt.Errorf("нитка дуги вырождена: радиус %g при офсете %g", seg.r, delta)
			}
			tx, ty := pos.X+delta*nx, pos.Y+delta*ny
			seg.aFrom = math.Atan2(ty-seg.cy, tx-seg.cx)
			seg.aSweep = p.Angle
		default:
			return nil, fmt.Errorf("неизвестный примитив %d", p.Kind)
		}
		out = append(out, seg)
		pos = p.End(pos)
		u = uTo
	}
	return out, nil
}

// segIntersections ищет пересечения двух сегментов ниток. Домены обоих уже
// ограничены uFrom/uTo: решение вне [uFrom, uTo] отбрасывается.
func segIntersections(a, b *threadSeg) ([]intersection, bool, error) {
	if a.line && b.line {
		return lineLine(a, b)
	}
	if !a.line && !b.line {
		return circleCircle(a, b)
	}
	if a.line {
		return lineCircle(a, b)
	}
	return lineCircle(b, a)
}

// lineLine — пересечение двух прямых сегментов.
func lineLine(a, b *threadSeg) ([]intersection, bool, error) {
	denom := a.dx*b.dy - a.dy*b.dx
	if math.Abs(denom) < 1e-12 {
		// Параллельны. Совпадение — вырождение, канонического ответа нет.
		cross := (b.px-a.px)*a.dy - (b.py-a.py)*a.dx
		if math.Abs(cross) < 1e-9 {
			return nil, false, fmt.Errorf("нитки совпадают")
		}
		return nil, false, nil
	}
	dx, dy := b.px-a.px, b.py-a.py
	t := (dx*b.dy - dy*b.dx) / denom
	w := (dx*a.dy - dy*a.dx) / denom
	if t < -1e-6 || t > a.uTo-a.uFrom+1e-6 || w < -1e-6 || w > b.uTo-b.uFrom+1e-6 {
		return nil, false, nil
	}
	return []intersection{{uS: a.uFrom + t, uD: b.uFrom + w, x: a.px + t*a.dx, y: a.py + t*a.dy}}, false, nil
}

// lineCircle — пересечение прямой сегмента a и окружности сегмента b.
func lineCircle(a, b *threadSeg) ([]intersection, bool, error) {
	fx, fy := a.px-b.cx, a.py-b.cy
	bf := a.dx*fx + a.dy*fy
	c := fx*fx + fy*fy - b.r*b.r
	disc := 4 * (bf*bf - c)
	const discEps = 1e-10 // м² — касание на уровне микрометров неразличимо
	if disc < -discEps {
		return nil, false, nil
	}
	if math.Abs(disc) <= discEps {
		return nil, true, nil // касание
	}
	sqrt := math.Sqrt(disc) / 2
	var out []intersection
	for _, t := range []float64{-bf + sqrt, -bf - sqrt} {
		if t < -1e-6 || t > a.uTo-a.uFrom+1e-6 {
			continue
		}
		x, y := a.px+t*a.dx, a.py+t*a.dy
		if !onArc(b, x, y) {
			continue
		}
		out = append(out, intersection{uS: a.uFrom + t, uD: b.uFrom + arcOffset(b, x, y), x: x, y: y})
	}
	return out, false, nil
}

// circleCircle — пересечение двух дуговых сегментов.
func circleCircle(a, b *threadSeg) ([]intersection, bool, error) {
	dx, dy := b.cx-a.cx, b.cy-a.cy
	d := math.Hypot(dx, dy)
	const dEps = 1e-6 // метры
	if d > a.r+b.r+dEps || d < math.Abs(a.r-b.r)-dEps {
		return nil, false, nil // не пересекаются
	}
	if d <= dEps {
		return nil, false, fmt.Errorf("центры ниток совпадают — вырождение")
	}
	if math.Abs(d-(a.r+b.r)) <= dEps || math.Abs(d-math.Abs(a.r-b.r)) <= dEps {
		return nil, true, nil // внешнее или внутреннее касание
	}
	// По теореме косинусов: точка P на отрезке между центрами, затем перпендикуляр.
	along := (a.r*a.r - b.r*b.r + d*d) / (2 * d)
	h := math.Sqrt(a.r*a.r - along*along)
	ux, uy := dx/d, dy/d
	px, py := a.cx+along*ux, a.cy+along*uy
	nx, ny := -uy, ux
	var out []intersection
	for _, s := range []float64{1, -1} {
		x, y := px+s*h*nx, py+s*h*ny
		if !onArc(a, x, y) || !onArc(b, x, y) {
			continue
		}
		out = append(out, intersection{
			uS: a.uFrom + arcOffset(a, x, y),
			uD: b.uFrom + arcOffset(b, x, y),
			x:  x, y: y,
		})
	}
	return out, false, nil
}

// onArc проверяет, что точка лежит на дуговом сегменте: угловое расстояние от
// начала дуги не превышает размаха и направлено по нему.
func onArc(s *threadSeg, x, y float64) bool {
	alpha := math.Atan2(y-s.cy, x-s.cx)
	diff := alpha - s.aFrom
	for diff > math.Pi {
		diff -= 2 * math.Pi
	}
	for diff < -math.Pi {
		diff += 2 * math.Pi
	}
	return s.aSweep >= 0 && diff >= -1e-9 && diff <= s.aSweep+1e-9 ||
		s.aSweep < 0 && diff <= 1e-9 && diff >= s.aSweep-1e-9
}

// arcOffset — длина дуги сегмента от начала до точки: u смещение по окружности.
func arcOffset(s *threadSeg, x, y float64) float64 {
	alpha := math.Atan2(y-s.cy, x-s.cx)
	diff := alpha - s.aFrom
	for diff > math.Pi {
		diff -= 2 * math.Pi
	}
	for diff < -math.Pi {
		diff += 2 * math.Pi
	}
	return math.Abs(diff) * (s.uTo - s.uFrom) / math.Abs(s.aSweep)
}

// dedupIntersections склеивает совпадающие точки: одно пересечение на стыке
// примитивов приходит дважды.
func dedupIntersections(pts []intersection) []intersection {
	out := make([]intersection, 0, len(pts))
	for _, p := range pts {
		dup := false
		for _, q := range out {
			if math.Hypot(p.x-q.x, p.y-q.y) < 1e-6 {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, p)
		}
	}
	return out
}

// threadPointAt — точка нитки прохода на смещении u: осевая поза плюс офсет по
// левой нормали.
func threadPointAt(e Element, delta, u float64) (x, y float64) {
	d, err := units.MetersToDistance(u)
	if err != nil {
		return math.NaN(), math.NaN()
	}
	pos, err := e.Plan.PoseAt(e.Start.Plan, d)
	if err != nil {
		return math.NaN(), math.NaN()
	}
	return pos.X + delta*(-math.Sin(pos.Heading)), pos.Y + delta*math.Cos(pos.Heading)
}

// tangentAt — единичная касательная осевой линии прохода на смещении u, по
// ходу возрастания u.
func tangentAt(e Element, u float64) RenderVec {
	d, err := units.MetersToDistance(u)
	if err != nil {
		return RenderVec{}
	}
	pos, err := e.Plan.PoseAt(e.Start.Plan, d)
	if err != nil {
		return RenderVec{}
	}
	return RenderVec{X: math.Cos(pos.Heading), Y: math.Sin(pos.Heading)}
}

func dist(x, y, px, py float64) float64 {
	return math.Hypot(x-px, y-py)
}
