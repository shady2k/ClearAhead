package mapfmt

// Проверка пересечений осей в плане.
//
// Инвариант: любая общая точка двух осей обязана быть объяснена топологией.
// Глухого пересечения формат карты не выражает (в topology только nodes,
// turnouts, edges, trackside), значит пересечение осей — не недомоделированный
// объект, а ошибка карты, и Validate обязан её отвергать.
//
// Что объяснено топологией:
//
//   - совпадение назначенных концов двух элементов, подключённых к одному
//     порту (обычный стык); проверяется не «близость к узлу», а что общая
//     точка пришлась на конец ОБОИХ элементов;
//   - у стрелки — общее остриё: прямой и отклонённый проходы выходят из
//     общего порта в одну сторону, общая точка разрешена только там (t=0 у
//     обоих). Повторное пересечение ветвей после расхождения — ошибка;
//   - стык соседних примитивов одного элемента: это непрерывность одной оси,
//     объяснённая построением цепочки.
//
// Всё остальное — ошибка: пересечение в середине, конец пути, упирающийся в
// середину другого без топологической связи, касание вне разрешённого порта,
// коллинеарное наложение и дублированные рёбра, самопересечение элемента.
//
// Пересечения считаются аналитически по примитивам straight/arc, а не по
// тесселированным полилиниям: тесселяция у острия сама рождает ложные
// пересечения, и порог на них — лечение симптома. Допуски здесь — для
// устойчивости счёта, а не для определения физического смысла.
//
// По высотам исключение не делается: у нас нет объекта, который объявляет
// разноуровневость, а случайное расхождение z замаскировало бы ошибку уклонов.
// Оси сравниваются в плане всегда.

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/shady2k/ClearAhead/server/internal/geom"
	"github.com/shady2k/ClearAhead/server/internal/units"
)

// Допуски плановой проверки.
//
// axisTolPosition совпадает с допуском замыкания компилятора (спека §7,
// track.TolPosition): концы, сошедшиеся в порту, в расчёте могут разойтись на
// миллиметр, и классификация «общая точка — это стык» обязана это выдержать.
// Совпадение кривых (наложение, дубликат ребра) распознаётся на микронном
// масштабе: сближение на миллиметры — это междупутье, а оно здесь НЕ
// проверяется, это другая задача с другими допусками.
const (
	axisTolPosition = units.Millimeter // «точка на конце элемента»
	axisTolCoincide = units.Micrometer // «кривые совпадают» (наложение)
	axisTolHeading  = 1e-5             // схождение направлений, как в замыкании
	axisEps         = 1e-9             // относительная точность решения систем
)

// ---- Плановая проекция топологии ----
//
// Проверка работает в мировых координатах, значит карте нужны позы, а их
// выводит распространение. Это план-проекция track.Propagate: вертикаль здесь
// не нужна, оси сравниваются в плане.

// axisElement — линейный элемент плана: ребро или проход стрелки.
type axisElement struct {
	ID    string
	From  string
	To    string
	Plan  geom.Chain
	Start geom.Pose // поза конца From, направленная внутрь элемента
	posed bool      // элемент в компоненте с якорем — у него есть поза
}

// axisIncidence — конец элемента в порту.
type axisIncidence struct {
	port    string
	element string
}

func (a axisIncidence) String() string { return a.port + "@" + a.element }

// axisRelation — связь двух концов в одном порту. Flip означает, что
// направления внутрь элементов различаются на π.
type axisRelation struct {
	to   axisIncidence
	flip bool
}

func buildAxisElements(m *Map) (map[string]axisElement, error) {
	ends := m.ElementEnds()

	out := map[string]axisElement{}
	for id, a := range m.AllAlignments() {
		chain := make(geom.Chain, 0, len(a.Horizontal))
		for i, hp := range a.Horizontal {
			var (
				p   geom.Primitive
				err error
			)
			switch hp.Kind {
			case "straight":
				var d units.Distance
				if d, err = units.MetersToDistance(hp.Length); err == nil {
					p, err = geom.Straight(d)
				}
			case "arc":
				p, err = geom.Arc(hp.Radius, hp.Angle)
			default:
				err = fmt.Errorf("неизвестный примитив %q", hp.Kind)
			}
			if err != nil {
				return nil, fmt.Errorf("mapfmt: %s[%d]: %w", id, i, err)
			}
			chain = append(chain, p)
		}
		e, ok := ends[id]
		if !ok {
			return nil, fmt.Errorf("mapfmt: у элемента %s нет концов", id)
		}
		out[id] = axisElement{ID: id, From: e[0], To: e[1], Plan: chain}
	}
	return out, nil
}

// buildAxisJunctions утверждает отношения между концами в каждом порту —
// те же правила, что у компилятора (track.buildJunctions).
func buildAxisJunctions(m *Map, els map[string]axisElement) (map[axisIncidence][]axisRelation, error) {
	byPort := map[string][]axisIncidence{}
	ids := make([]string, 0, len(els))
	for id := range els {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		e := els[id]
		byPort[e.From] = append(byPort[e.From], axisIncidence{e.From, id})
		byPort[e.To] = append(byPort[e.To], axisIncidence{e.To, id})
	}

	type turnoutPort struct {
		turnout string
		role    string // "common" | "straight" | "diverging"
	}
	swPorts := map[string]turnoutPort{}
	for _, t := range m.Topology.Turnouts {
		swPorts[t.ID+"."+t.Ports.Common] = turnoutPort{t.ID, "common"}
		swPorts[t.ID+"."+t.Ports.Straight] = turnoutPort{t.ID, "straight"}
		swPorts[t.ID+"."+t.Ports.Diverging] = turnoutPort{t.ID, "diverging"}
	}

	j := map[axisIncidence][]axisRelation{}
	link := func(a, b axisIncidence, flip bool) {
		j[a] = append(j[a], axisRelation{to: b, flip: flip})
		j[b] = append(j[b], axisRelation{to: a, flip: flip})
	}

	ports := make([]string, 0, len(byPort))
	for p := range byPort {
		ports = append(ports, p)
	}
	sort.Strings(ports)

	isPassage := func(id string) bool {
		return strings.HasSuffix(id, PassageStraight) || strings.HasSuffix(id, PassageDiverging)
	}

	for _, port := range ports {
		incs := byPort[port]
		var external, passages []axisIncidence
		for _, in := range incs {
			if isPassage(in.element) {
				passages = append(passages, in)
			} else {
				external = append(external, in)
			}
		}

		tp, isSwitchPort := swPorts[port]
		if !isSwitchPort {
			if len(passages) > 0 {
				return nil, fmt.Errorf("mapfmt: порт %s не принадлежит стрелке, но в нём проход %s",
					port, passages[0].element)
			}
			switch len(external) {
			case 0, 1:
			case 2:
				link(external[0], external[1], true)
			default:
				return nil, fmt.Errorf("mapfmt: порт %s обслуживает %d рёбер — развилку нужно оформить стрелкой",
					port, len(external))
			}
			continue
		}

		if len(external) != 1 {
			return nil, fmt.Errorf("mapfmt: порт %s стрелки %s обслуживает %d внешних рёбер, требуется ровно одно",
				port, tp.turnout, len(external))
		}
		want := 1
		if tp.role == "common" {
			want = 2
		}
		if len(passages) != want {
			return nil, fmt.Errorf("mapfmt: порт %s стрелки %s: проходов %d, ожидалось %d",
				port, tp.turnout, len(passages), want)
		}
		if tp.role == "common" {
			// Оба прохода выходят из остряка в одну сторону.
			link(passages[0], passages[1], false)
		}
		for _, p := range passages {
			link(external[0], p, true)
		}
	}
	return j, nil
}

// axisPoses выводит плановые позы начал элементов от якорей и проверяет
// замыкание плана.
//
// Замыкание проверяется, но не карается: разошедшийся план отвергает
// компилятор (track.Propagate), а здесь несошедшаяся геометрия просто лишает
// проверку пересечений смысла — такую карту всё равно не скомпилировать, и
// отчёт о пересечениях на ней неважен. Поэтому settle берёт первую позу, а
// расхождение больше допуска помечает план как разошедшийся (broken), и
// проверка пропускается целиком.
//
// Элементы в компонентах без якоря позы не получают (posed=false): их
// положение не определено, и пересечения такой компоненты не проверяются —
// компоненту без якоря отвергает компилятор, а валидатор связность не держит
// (см. validateAnchors).
func axisPoses(m *Map) (els map[string]axisElement, broken bool, err error) {
	els, err = buildAxisElements(m)
	if err != nil {
		return nil, false, err
	}
	junc, err := buildAxisJunctions(m, els)
	if err != nil {
		return nil, false, err
	}

	byPort := map[string][]axisIncidence{}
	ids := make([]string, 0, len(els))
	for id := range els {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		e := els[id]
		byPort[e.From] = append(byPort[e.From], axisIncidence{e.From, id})
		byPort[e.To] = append(byPort[e.To], axisIncidence{e.To, id})
	}

	poses := map[axisIncidence]geom.Pose{}
	visited := map[axisIncidence]bool{}

	anchorIDs := make([]string, 0, len(m.Anchors))
	for id := range m.Anchors {
		anchorIDs = append(anchorIDs, id)
	}
	sort.Strings(anchorIDs)

	for _, port := range anchorIDs {
		incs := byPort[port]
		if len(incs) == 0 {
			return nil, false, fmt.Errorf("mapfmt: якорь %s не принадлежит ни одному элементу", port)
		}
		a := m.Anchors[port]

		// Якорь задаёт направление, а на порту с несколькими концами оно
		// неоднозначно: у общего порта стрелки концов три. Снимается данными,
		// а не конвенцией — якорь называет свой элемент.
		start := incs[0]
		if len(incs) > 1 || a.Element != "" {
			if a.Element == "" {
				names := make([]string, 0, len(incs))
				for _, in := range incs {
					names = append(names, in.element)
				}
				sort.Strings(names)
				return nil, false, fmt.Errorf(
					"mapfmt: якорь %s стоит на порту с %d концами (%s) — укажите element, "+
						"внутрь которого смотрит heading", port, len(incs), strings.Join(names, ", "))
			}
			found := false
			for _, in := range incs {
				if in.element == a.Element {
					start, found = in, true
					break
				}
			}
			if !found {
				return nil, false, fmt.Errorf("mapfmt: якорь %s называет элемент %s, которого в этом порту нет",
					port, a.Element)
			}
		}
		if _, seen := poses[start]; seen {
			return nil, false, fmt.Errorf("mapfmt: в одной связной компоненте два якоря, второй — %s", port)
		}
		poses[start] = geom.Normalize(geom.Pose{X: a.X, Y: a.Y, Heading: a.Heading})

		queue := []axisIncidence{start}
		for len(queue) > 0 {
			at := queue[0]
			queue = queue[1:]
			if visited[at] {
				continue
			}
			visited[at] = true

			// Шаг первый: через элемент, на его другой конец.
			far, want, err := axisAcross(els[at.element], at, poses[at])
			if err != nil {
				return nil, false, err
			}
			axisSettle(poses, far, want, &broken)
			if !visited[far] {
				queue = append(queue, far)
			}

			// Шаг второй: на соседние концы в том же порту, по объявленному
			// отношению. Ориентация утверждена buildAxisJunctions, а не выведена
			// из порядка обхода.
			for _, rel := range junc[at] {
				want := poses[at]
				if rel.flip {
					want = geom.Reverse(want)
				}
				axisSettle(poses, rel.to, want, &broken)
				if !visited[rel.to] {
					queue = append(queue, rel.to)
				}
			}
		}
	}

	for id := range els {
		if p, ok := poses[axisIncidence{els[id].From, id}]; ok {
			e := els[id]
			e.Start = p
			e.posed = true
			els[id] = e
		}
	}
	return els, broken, nil
}

// axisSettle записывает позу конца или, если она уже записана, сравнивает с
// записанной. Расхождение больше допуска не роняет проверку — оно помечает
// план как разошедшийся, и проверка пересечений пропускается целиком.
func axisSettle(poses map[axisIncidence]geom.Pose, at axisIncidence, want geom.Pose, broken *bool) {
	got, seen := poses[at]
	if !seen {
		poses[at] = want
		return
	}
	dxy := math.Hypot(got.X-want.X, got.Y-want.Y)
	dh := geom.Normalize(geom.Pose{Heading: got.Heading - want.Heading}).Heading
	if dxy > axisTolPosition.Meters() || math.Abs(dh) > axisTolHeading {
		*broken = true
	}
}

// axisAcross переносит позу с одного конца элемента на другой.
// Обратный ход не разворачивает цепочку по звеньям: цепочка целиком есть одно
// жёсткое движение Δ, и начало восстанавливается как Compose(конец, Invert(Δ)).
func axisAcross(e axisElement, from axisIncidence, at geom.Pose) (axisIncidence, geom.Pose, error) {
	if from.element != e.ID {
		return axisIncidence{}, geom.Pose{}, fmt.Errorf("mapfmt: конец %s не принадлежит элементу %s", from, e.ID)
	}
	delta := e.Plan.End(geom.Pose{})
	switch from.port {
	case e.From:
		end := geom.Compose(at, delta)
		return axisIncidence{e.To, e.ID}, geom.Reverse(end), nil
	case e.To:
		travelEnd := geom.Reverse(at)
		start := geom.Compose(travelEnd, geom.Invert(delta))
		return axisIncidence{e.From, e.ID}, start, nil
	default:
		return axisIncidence{}, geom.Pose{}, fmt.Errorf("mapfmt: конец %s не принадлежит элементу %s", from, e.ID)
	}
}

// ---- Пересечение примитивов ----

// axisPrim — один примитив плана в мировых координатах.
type axisPrim struct {
	start  geom.Pose
	end    geom.Pose
	kind   geom.Kind
	length float64 // метры
	radius float64 // метры; только для дуги
	angle  float64 // радианы со знаком; только для дуги
	uStart float64 // метры от начала элемента
}

func elementPrims(e axisElement) []axisPrim {
	prims := make([]axisPrim, 0, len(e.Plan))
	cur := e.Start
	var u float64
	for _, p := range e.Plan {
		ap := axisPrim{
			start:  cur,
			kind:   p.Kind,
			length: p.Length.Meters(),
			radius: p.Radius,
			angle:  p.Angle,
			uStart: u,
		}
		ap.end = p.End(cur)
		prims = append(prims, ap)
		u += ap.length
		cur = ap.end
	}
	return prims
}

// primHit — общая точка двух примитивов. Параметры — метры от начала
// примитива. overlap=true означает, что общих точек бесконечно много
// (коллинеарное наложение, совпадающие дуги): физически это одна ось,
// записанная дважды.
type primHit struct {
	uP, vP  float64
	x, y    float64
	overlap bool
}

// axisHit — общая точка двух элементов: параметры метрах от начала элементов.
type axisHit struct {
	uP, vP  float64 // метры вдоль примитивов (для стыков соседних примитивов)
	uA, uB  float64 // метры вдоль элементов
	x, y    float64
	overlap bool
}

// primIntersections возвращает все общие точки двух примитивов.
func primIntersections(a, b axisPrim) []primHit {
	if a.kind == geom.KindStraight && b.kind == geom.KindStraight {
		return interStraightStraight(a, b)
	}
	if a.kind == geom.KindStraight {
		return interStraightArc(a, b)
	}
	if b.kind == geom.KindStraight {
		// симметричная пара: параметры меняются местами
		hits := interStraightArc(b, a)
		for i := range hits {
			hits[i].uP, hits[i].vP = hits[i].vP, hits[i].uP
		}
		return hits
	}
	return interArcArc(a, b)
}

func interStraightStraight(a, b axisPrim) []primHit {
	ax, ay := math.Cos(a.start.Heading), math.Sin(a.start.Heading)
	bx, by := math.Cos(b.start.Heading), math.Sin(b.start.Heading)
	dx, dy := b.start.X-a.start.X, b.start.Y-a.start.Y

	det := ax*by - ay*bx
	if math.Abs(det) > axisEps {
		u := (dx*by - dy*bx) / det
		v := (ay*dx - ax*dy) / det
		tol := axisTolPosition.Meters()
		if u < -tol || u > a.length+tol || v < -tol || v > b.length+tol {
			return nil
		}
		return []primHit{{uP: u, vP: v, x: a.start.X + ax*u, y: a.start.Y + ay*u}}
	}

	// Параллельные: общая прямая — только если концы b лежат на прямой a
	// (микронный допуск: сближение на миллиметры — это междупутье, не наложение).
	if math.Abs(dx*ay-dy*ax) > axisTolCoincide.Meters() ||
		math.Abs((dx+bx*b.length)*ay-(dy+by*b.length)*ax) > axisTolCoincide.Meters() {
		return nil
	}
	// Проекции интервалов на прямую a: a — [0, lenA], b — [p, p + lenB*(b̂·â)].
	s := bx*ax + by*ay
	p := dx*ax + dy*ay
	lo := math.Max(0, math.Min(p, p+s*b.length))
	hi := math.Min(a.length, math.Max(p, p+s*b.length))
	// Наложение: интервалы перекрываются на положительную длину. Порог —
	// точность счёта, а не микрон: касание концами даёт ровно ноль, а
	// микронный зазор между несостыкованными отрезками — это не общая точка.
	rel := axisEps * (a.length + b.length + math.Abs(p) + 1)
	if hi-lo > rel {
		u := (lo + hi) / 2
		v := (u - p) / s
		return []primHit{{uP: u, vP: v, x: a.start.X + ax*u, y: a.start.Y + ay*u, overlap: true}}
	}
	if hi-lo >= -rel {
		u := (lo + hi) / 2
		v := (u - p) / s
		return []primHit{{uP: u, vP: v, x: a.start.X + ax*u, y: a.start.Y + ay*u}}
	}
	return nil
}

func interStraightArc(a, b axisPrim) []primHit {
	// a — прямая, b — дуга. |S + u·â − C|² = R² — квадратное уравнение по u.
	ax, ay := math.Cos(a.start.Heading), math.Sin(a.start.Heading)
	cx, cy := arcCenter(b)
	mx, my := a.start.X-cx, a.start.Y-cy
	mm := mx*mx + my*my - b.radius*b.radius
	ma := mx*ax + my*ay
	disc := ma*ma - mm
	if disc < -axisEps*axisEps*b.radius*b.radius {
		return nil
	}
	if disc < 0 {
		disc = 0
	}
	sqrt := math.Sqrt(disc)
	tol := axisTolPosition.Meters()
	var out []primHit
	for _, u := range []float64{-ma - sqrt, -ma + sqrt} {
		if u < -tol || u > a.length+tol {
			continue
		}
		x, y := a.start.X+ax*u, a.start.Y+ay*u
		v, ok := arcParam(b, cx, cy, x, y)
		if !ok {
			continue
		}
		out = append(out, primHit{uP: u, vP: v, x: x, y: y})
	}
	return out
}

func interArcArc(a, b axisPrim) []primHit {
	c1x, c1y := arcCenter(a)
	c2x, c2y := arcCenter(b)
	dx, dy := c2x-c1x, c2y-c1y
	d := math.Hypot(dx, dy)
	scale := math.Max(a.radius, b.radius)

	if d < axisEps*scale {
		// Концентричные: пересечение есть только на одной окружности.
		if math.Abs(a.radius-b.radius) > axisTolCoincide.Meters() {
			return nil
		}
		return coincidentArcs(a, b, c1x, c1y)
	}

	r1, r2 := a.radius, b.radius
	// Не пересекаются: зазор больше точности счёта (не больше «физического»
	// допуска — касание и его отсутствие обязаны различаться).
	if d > r1+r2+axisEps*scale || d < math.Abs(r1-r2)-axisEps*scale {
		return nil
	}
	// Точки пересечения окружностей.
	a2 := (d*d + r1*r1 - r2*r2) / (2 * d)
	h2 := r1*r1 - a2*a2
	if h2 < 0 {
		h2 = 0 // касание окружностей
	}
	ux, uy := dx/d, dy/d
	px, py := c1x+ux*a2, c1y+uy*a2
	h := math.Sqrt(h2)
	var out []primHit
	for _, s := range []float64{1, -1} {
		if h == 0 && s == -1 {
			break
		}
		x, y := px-s*uy*h, py+s*ux*h
		ua, ok1 := arcParam(a, c1x, c1y, x, y)
		ub, ok2 := arcParam(b, c2x, c2y, x, y)
		if ok1 && ok2 {
			out = append(out, primHit{uP: ua, vP: ub, x: x, y: y})
		}
	}
	return out
}

// arcCenter возвращает центр дуги в мировых координатах.
func arcCenter(a axisPrim) (float64, float64) {
	rs := math.Copysign(a.radius, a.angle)
	h := a.start.Heading
	return a.start.X - rs*math.Sin(h), a.start.Y + rs*math.Cos(h)
}

// arcParam возвращает параметр точки на дуге — метры от начала дуги — и
// признак того, что точка лежит на дуге.
func arcParam(a axisPrim, cx, cy, x, y float64) (float64, bool) {
	phi := math.Atan2(y-cy, x-cx)
	psi := math.Atan2(a.start.Y-cy, a.start.X-cx)
	swept, ok := sweptAngle(a, psi, phi)
	if !ok {
		return 0, false
	}
	return math.Abs(swept) * a.radius, true
}

// sweptAngle — угол точки phi (от центра дуги) относительно начала дуги psi,
// измеренный в направлении хода дуги, и признак того, что точка на дуге.
func sweptAngle(a axisPrim, psi, phi float64) (float64, bool) {
	swept := phi - psi
	if a.angle > 0 {
		if swept < 0 {
			swept += 2 * math.Pi
		}
		return swept, swept <= a.angle+axisEps
	}
	if swept > 0 {
		swept -= 2 * math.Pi
	}
	return swept, swept >= a.angle-axisEps
}

// coincidentArcs — две дуги одной окружности. Общие точки — пересечение
// угловых диапазонов: интервал — наложение (одна ось записана дважды),
// касание диапазонов в одной точке — точечная общая точка.
func coincidentArcs(a, b axisPrim, cx, cy float64) []primHit {
	psiA := math.Atan2(a.start.Y-cy, a.start.X-cx)
	psiB := math.Atan2(b.start.Y-cy, b.start.X-cx)
	isFull := func(angle float64) bool { return math.Abs(angle) >= 2*math.Pi-axisEps }

	mid := func(psi float64, angle float64) (float64, float64, float64) {
		phi := psi + angle/2
		ua, _ := sweptAngle(a, psiA, phi)
		ub, _ := sweptAngle(b, psiB, phi)
		return math.Abs(ua) * a.radius, math.Abs(ub) * b.radius, phi
	}

	switch {
	case isFull(a.angle) && isFull(b.angle):
		ua, ub, phi := mid(psiA, a.angle)
		return []primHit{{uP: ua, vP: ub, x: cx + a.radius*math.Cos(phi), y: cy + a.radius*math.Sin(phi), overlap: true}}
	case isFull(a.angle):
		ua, ub, phi := mid(psiB, b.angle)
		return []primHit{{uP: ua, vP: ub, x: cx + a.radius*math.Cos(phi), y: cy + a.radius*math.Sin(phi), overlap: true}}
	case isFull(b.angle):
		ua, ub, phi := mid(psiA, a.angle)
		return []primHit{{uP: ua, vP: ub, x: cx + a.radius*math.Cos(phi), y: cy + a.radius*math.Sin(phi), overlap: true}}
	}

	// Границы обеих дуг — кандидаты в концы общего интервала. Сырые углы
	// сохраняются: нормировка в [0, 2π) сломала бы sweptAngle — параметр u
	// на дуге считается от её начала, а не от нормированного угла.
	var inside []float64
	for _, phi := range []float64{psiA, psiA + a.angle, psiB, psiB + b.angle} {
		_, okA := sweptAngle(a, psiA, phi)
		_, okB := sweptAngle(b, psiB, phi)
		if okA && okB {
			dup := false
			for _, in := range inside {
				d := math.Abs(normAngle(in) - normAngle(phi))
				if d > math.Pi {
					d = 2*math.Pi - d
				}
				if d <= axisEps {
					dup = true
					break
				}
			}
			if !dup {
				inside = append(inside, phi)
			}
		}
	}
	if len(inside) == 0 {
		return nil
	}
	if len(inside) == 1 {
		// Диапазоны касаются в одной точке.
		phi := inside[0]
		ua, _ := sweptAngle(a, psiA, phi)
		ub, _ := sweptAngle(b, psiB, phi)
		return []primHit{{uP: math.Abs(ua) * a.radius, vP: math.Abs(ub) * b.radius,
			x: cx + a.radius*math.Cos(phi), y: cy + a.radius*math.Sin(phi)}}
	}

	// Интервал: дуга между двумя границами, середина которой лежит в обеих.
	// Из двух дуг между точками верна та, чья середина внутри пересечения.
	lo, hi := inside[0], inside[1]
	for _, midPhi := range []float64{lo + (hi-lo)/2, hi + (lo+2*math.Pi-hi)/2} {
		_, okA := sweptAngle(a, psiA, midPhi)
		_, okB := sweptAngle(b, psiB, midPhi)
		if okA && okB {
			ua, _ := sweptAngle(a, psiA, midPhi)
			ub, _ := sweptAngle(b, psiB, midPhi)
			return []primHit{{uP: math.Abs(ua) * a.radius, vP: math.Abs(ub) * b.radius,
				x: cx + a.radius*math.Cos(midPhi), y: cy + a.radius*math.Sin(midPhi), overlap: true}}
		}
	}
	return nil
}

func normAngle(phi float64) float64 {
	phi = math.Mod(phi, 2*math.Pi)
	if phi < 0 {
		phi += 2 * math.Pi
	}
	return phi
}

// ---- Общий перебор ----

// aabb — ограничивающий прямоугольник элемента: дешёвый отсев пар, которые
// физически не могут пересечься.
type aabb struct{ minX, minY, maxX, maxY float64 }

func (b aabb) overlaps(o aabb) bool {
	return b.minX <= o.maxX && o.minX <= b.maxX && b.minY <= o.maxY && o.minY <= b.maxY
}

func primsAABB(prims []axisPrim) aabb {
	b := aabb{minX: math.Inf(1), minY: math.Inf(1), maxX: math.Inf(-1), maxY: math.Inf(-1)}
	extend := func(x, y float64) {
		if x < b.minX {
			b.minX = x
		}
		if y < b.minY {
			b.minY = y
		}
		if x > b.maxX {
			b.maxX = x
		}
		if y > b.maxY {
			b.maxY = y
		}
	}
	for _, p := range prims {
		extend(p.start.X, p.start.Y)
		extend(p.end.X, p.end.Y)
		if p.kind != geom.KindArc {
			continue
		}
		cx, cy := arcCenter(p)
		psi := math.Atan2(p.start.Y-cy, p.start.X-cx)
		// Экстремумы дуги — в кардинальных направлениях, если они в размахе.
		for _, dir := range []float64{0, math.Pi / 2, math.Pi, 3 * math.Pi / 2} {
			if _, ok := sweptAngle(p, psi, dir); ok {
				extend(cx+math.Cos(dir)*p.radius, cy+math.Sin(dir)*p.radius)
			}
		}
	}
	return b
}

// axisViolation — одна непростительная общая точка.
type axisViolation struct {
	aID, bID string
	x, y     float64
	overlap  bool
	self     bool
}

func (m *Map) validateAxisIntersections() error {
	els, broken, err := axisPoses(m)
	if err != nil {
		return err
	}
	if broken {
		// План не сошёлся: пересечения на разъехавшейся геометрии не считаем —
		// такую карту отвергает компилятор по невязке замыкания.
		return nil
	}

	ids := make([]string, 0, len(els))
	for id, e := range els {
		if e.posed {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)

	prims := make(map[string][]axisPrim, len(ids))
	boxes := make(map[string]aabb, len(ids))
	for _, id := range ids {
		p := elementPrims(els[id])
		prims[id] = p
		boxes[id] = primsAABB(p)
	}

	var viols []axisViolation

	// Пары элементов. Общая точка разрешена только на назначенных концах,
	// подключённых к одному порту.
	for i := range ids {
		aID := ids[i]
		ea := els[aID]
		lenA := ea.Plan.Length().Meters()
		for j := i + 1; j < len(ids); j++ {
			bID := ids[j]
			eb := els[bID]
			if !boxes[aID].overlaps(boxes[bID]) {
				continue
			}
			shared := sharedEnds(ea, eb, lenA, eb.Plan.Length().Meters())
			for _, h := range intersectElements(prims[aID], prims[bID]) {
				if h.overlap {
					viols = append(viols, axisViolation{aID: aID, bID: bID, x: h.x, y: h.y, overlap: true})
					continue
				}
				if !jointAllowed(shared, h.uA, h.uB) {
					viols = append(viols, axisViolation{aID: aID, bID: bID, x: h.x, y: h.y})
				}
			}
		}
	}

	// Самопересечения: цепочка одного элемента не должна пересекать саму себя.
	// Стык соседних примитивов — непрерывность оси, объяснённая построением.
	for _, id := range ids {
		p := prims[id]
		for i := range p {
			for j := i + 1; j < len(p); j++ {
				adjacent := j == i+1
				for _, h := range primIntersections(p[i], p[j]) {
					if adjacent && hAtJoint(p[i], h) {
						continue
					}
					viols = append(viols, axisViolation{aID: id, bID: id, x: h.x, y: h.y, overlap: h.overlap, self: true})
				}
			}
		}
	}

	if len(viols) == 0 {
		return nil
	}
	viols = dedupeViolations(viols)
	parts := make([]string, 0, len(viols))
	for _, v := range viols {
		switch {
		case v.self && v.overlap:
			parts = append(parts, fmt.Sprintf("%s налагается сам на себя около (%.1f, %.1f)", v.aID, v.x, v.y))
		case v.overlap:
			parts = append(parts, fmt.Sprintf("%s x %s налагаются (одна ось записана дважды) около (%.1f, %.1f)",
				v.aID, v.bID, v.x, v.y))
		case v.self:
			parts = append(parts, fmt.Sprintf("%s самопересекается в (%.1f, %.1f)", v.aID, v.x, v.y))
		default:
			parts = append(parts, fmt.Sprintf("%s x %s в (%.1f, %.1f)", v.aID, v.bID, v.x, v.y))
		}
	}
	return fmt.Errorf("mapfmt: оси пересекаются в плане: %s", strings.Join(parts, "; "))
}

// sharedEnds возвращает порты, к которым подключены оба элемента, с параметром
// конца каждого элемента в этом порту (0 — From, длина — To).
func sharedEnds(a, b axisElement, lenA, lenB float64) map[string][2]float64 {
	out := map[string][2]float64{}
	portA := map[string]float64{a.From: 0, a.To: lenA}
	portB := map[string]float64{b.From: 0, b.To: lenB}
	for p, ua := range portA {
		if ub, ok := portB[p]; ok {
			out[p] = [2]float64{ua, ub}
		}
	}
	return out
}

// jointAllowed — общая точка (uA, uB) объяснена топологией: оба элемента
// подключены к одному порту своими концами, и точка пришлась на эти концы.
func jointAllowed(shared map[string][2]float64, uA, uB float64) bool {
	tol := axisTolPosition.Meters()
	for _, ends := range shared {
		if math.Abs(uA-ends[0]) <= tol && math.Abs(uB-ends[1]) <= tol {
			return true
		}
	}
	return false
}

// hAtJoint — общая точка соседних примитивов пришлась на их общий стык:
// конец первого совпал с началом второго.
func hAtJoint(a axisPrim, h primHit) bool {
	tol := axisTolPosition.Meters()
	return math.Abs(h.uP-a.length) <= tol && math.Abs(h.vP) <= tol
}

// intersectElements возвращает общие точки двух элементов.
func intersectElements(pA, pB []axisPrim) []axisHit {
	var out []axisHit
	for _, a := range pA {
		for _, b := range pB {
			for _, h := range primIntersections(a, b) {
				out = append(out, axisHit{
					uP: h.uP, vP: h.vP,
					uA: a.uStart + h.uP, uB: b.uStart + h.vP,
					x: h.x, y: h.y, overlap: h.overlap,
				})
			}
		}
	}
	return out
}

// dedupeViolations — общая точка на границе примитивов находится дважды
// (парой примитивов слева и парой справа); в отчёте она должна быть один раз.
func dedupeViolations(v []axisViolation) []axisViolation {
	if len(v) < 2 {
		return v
	}
	sort.Slice(v, func(i, j int) bool {
		if v[i].aID != v[j].aID {
			return v[i].aID < v[j].aID
		}
		if v[i].bID != v[j].bID {
			return v[i].bID < v[j].bID
		}
		if v[i].x != v[j].x {
			return v[i].x < v[j].x
		}
		return v[i].y < v[j].y
	})
	out := v[:1]
	for _, cur := range v[1:] {
		prev := out[len(out)-1]
		if cur.aID == prev.aID && cur.bID == prev.bID &&
			math.Hypot(cur.x-prev.x, cur.y-prev.y) <= axisTolCoincide.Meters() {
			continue
		}
		out = append(out, cur)
	}
	return out
}
