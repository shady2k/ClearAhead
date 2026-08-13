package track

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/shady2k/ClearAhead/server/internal/geom"
	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/units"
)

// Допуски замыкания, фиксированные версией формата (спека §7).
const (
	TolPosition = units.Millimeter
	TolHeading  = 1e-5
	TolSlope    = 1e-4
)

// Incidence — конец элемента в порту.
//
// Ключевая единица этого модуля. Хранить позу «на порт» нельзя: у общего порта
// стрелки сходятся три конца — внешнее ребро и два прохода, — и два прохода
// смотрят в одну сторону, а ребро в другую. Одного направления на порт для
// такого узла физически не существует.
//
// Первая редакция хранила позу порта плюс «владельца» и разворачивала всех
// остальных на π. Это давало верный результат только на портах степени два, а на
// стрелке зависело от того, какой элемент обошли первым, то есть от сортировки
// идентификаторов. Ошибка проявлялась не поворотом на π, а десятками метров
// расхождения ниже по цепи.
type Incidence struct {
	Port    string
	Element string
}

func (i Incidence) String() string { return i.Port + "@" + i.Element }

// PortPose — поза конца элемента, направленная ВНУТРЬ этого элемента.
//
// Z и Slope не входят в geom.Pose сознательно: geom считает план, вертикаль —
// отдельная одномерная функция.
// Теги обязательны: PortPose уезжает в RenderGeometry, то есть в контракт с
// клиентом на Godot. Без них наружу протекали бы имена полей Go, и клиент зашил
// бы их навсегда.
type PortPose struct {
	Plan  geom.Pose `json:"plan"`
	Z     float64   `json:"z"`
	Slope float64   `json:"slope"` // dz/du в направлении Plan
}

// PortRelation — связь двух концов в одном порту.
//
// Flip означает, что направления внутрь элементов различаются на π.
type PortRelation struct {
	To   Incidence
	Flip bool
}

// Junctions — отношения между концами, сходящимися в портах.
type Junctions map[Incidence][]PortRelation

// Element — линейный элемент: ребро или проход стрелки.
type Element struct {
	ID string
	// Kind — вид пути (mapfmt.KindRail). У ребра свой, у прохода — вид
	// устройства; правило записано один раз в mapfmt.ElementKinds.
	Kind  string
	From  string
	To    string
	Plan  geom.Chain
	Prof  Profile
	HProf HProfile // горизонтальное выравнивание: радиусы кривых
	Start PortPose // поза конца From, направленная внутрь элемента
}

// fromInc и toInc — концы элемента.
func (e Element) fromInc() Incidence { return Incidence{Port: e.From, Element: e.ID} }
func (e Element) toInc() Incidence   { return Incidence{Port: e.To, Element: e.ID} }

// Propagate выводит позы всех концов от якорей и проверяет замыкание.
//
// Результат не зависит от порядка обхода: settle сравнивает одну и ту же
// Incidence, поэтому выбора ориентации при сравнении не остаётся. Сортировки
// ниже определяют только порядок диагностических сообщений.
func Propagate(m *mapfmt.Map) (map[Incidence]PortPose, map[string]Element, error) {
	els, err := buildElements(m)
	if err != nil {
		return nil, nil, err
	}
	junc, err := buildJunctions(m, els)
	if err != nil {
		return nil, nil, err
	}

	byPort := map[string][]Incidence{}
	ids := make([]string, 0, len(els))
	for id := range els {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		e := els[id]
		byPort[e.From] = append(byPort[e.From], e.fromInc())
		byPort[e.To] = append(byPort[e.To], e.toInc())
	}

	poses := map[Incidence]PortPose{}
	visited := map[Incidence]bool{}

	anchorIDs := make([]string, 0, len(m.Anchors))
	for id := range m.Anchors {
		anchorIDs = append(anchorIDs, id)
	}
	sort.Strings(anchorIDs)

	for _, port := range anchorIDs {
		incs := byPort[port]
		if len(incs) == 0 {
			return nil, nil, fmt.Errorf("track: якорь %s не принадлежит ни одному элементу", port)
		}
		a := m.Anchors[port]

		// Якорь задаёт направление, а на порту с несколькими концами направление
		// неоднозначно: у общего порта стрелки их три. Снимается данными, а не
		// конвенцией — якорь называет свой элемент. Выводить его из сортировки
		// нельзя: переименование элемента развернуло бы всю компоненту на π.
		start := incs[0]
		if len(incs) > 1 || a.Element != "" {
			if a.Element == "" {
				names := make([]string, 0, len(incs))
				for _, in := range incs {
					names = append(names, in.Element)
				}
				sort.Strings(names)
				return nil, nil, fmt.Errorf(
					"track: якорь %s стоит на порту с %d концами (%s) — укажите element, "+
						"внутрь которого смотрит heading", port, len(incs), strings.Join(names, ", "))
			}
			found := false
			for _, in := range incs {
				if in.Element == a.Element {
					start, found = in, true
					break
				}
			}
			if !found {
				return nil, nil, fmt.Errorf("track: якорь %s называет элемент %s, которого в этом порту нет",
					port, a.Element)
			}
		}
		if _, seen := poses[start]; seen {
			return nil, nil, fmt.Errorf("track: в одной связной компоненте два якоря, второй — %s", port)
		}

		slope, err := endSlope(els[start.Element], start)
		if err != nil {
			return nil, nil, err
		}
		poses[start] = PortPose{
			Plan:  geom.Normalize(geom.Pose{X: a.X, Y: a.Y, Heading: a.Heading}),
			Z:     a.Z,
			Slope: slope,
		}

		queue := []Incidence{start}
		for len(queue) > 0 {
			at := queue[0]
			queue = queue[1:]
			if visited[at] {
				continue
			}
			visited[at] = true

			// Шаг первый: через элемент, на его другой конец.
			far, want, err := across(els[at.Element], at, poses[at])
			if err != nil {
				return nil, nil, err
			}
			if err := settle(poses, far, want); err != nil {
				return nil, nil, err
			}
			if !visited[far] {
				queue = append(queue, far)
			}

			// Шаг второй: на соседние концы в том же порту, по объявленному
			// отношению. Ориентация здесь утверждена buildJunctions, а не выведена
			// из порядка обхода.
			for _, rel := range junc[at] {
				want := relate(poses[at], rel.Flip)
				if err := settle(poses, rel.To, want); err != nil {
					return nil, nil, err
				}
				if !visited[rel.To] {
					queue = append(queue, rel.To)
				}
			}
		}
	}

	for id, e := range els {
		p, ok := poses[e.fromInc()]
		if !ok {
			return nil, nil, fmt.Errorf("track: элемент %s в компоненте без якоря", id)
		}
		e.Start = p
		els[id] = e
	}
	return poses, els, nil
}

// relate переносит позу на соседний конец в том же порту.
//
// При Flip разворачивается и план, и знак уклона: уклон измеряется в направлении
// Plan, поэтому разворот направления меняет его знак.
func relate(p PortPose, flip bool) PortPose {
	if !flip {
		return p
	}
	return PortPose{Plan: geom.Reverse(p.Plan), Z: p.Z, Slope: -p.Slope}
}

// buildJunctions утверждает отношения между концами в каждом порту.
//
// Это отдельная фаза сознательно: отношение «в одну сторону или в разные» есть
// свойство топологии, и его надо объявить один раз и проверить, а не выводить по
// ходу обхода из того, кто пришёл первым.
//
// Правила исчерпывают нынешний словарь элементов:
//
//	обычный порт, два ребра:  ребро1 --flip--> ребро2
//	стрелка, общий порт:      straight --same--> diverging
//	                          внешнее ребро --flip--> обоим проходам
//	стрелка, порт S:          внешнее ребро --flip--> straight
//	стрелка, порт D:          внешнее ребро --flip--> diverging
func buildJunctions(m *mapfmt.Map, els map[string]Element) (Junctions, error) {
	byPort := map[string][]Incidence{}
	ids := make([]string, 0, len(els))
	for id := range els {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		e := els[id]
		byPort[e.From] = append(byPort[e.From], e.fromInc())
		byPort[e.To] = append(byPort[e.To], e.toInc())
	}

	// Порты стрелок: какой стрелке принадлежит порт и какую роль играет.
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

	j := Junctions{}
	link := func(a, b Incidence, flip bool) {
		j[a] = append(j[a], PortRelation{To: b, Flip: flip})
		j[b] = append(j[b], PortRelation{To: a, Flip: flip})
	}

	ports := make([]string, 0, len(byPort))
	for p := range byPort {
		ports = append(ports, p)
	}
	sort.Strings(ports)

	for _, port := range ports {
		incs := byPort[port]
		var external, passages []Incidence
		for _, in := range incs {
			if isPassage(in.Element) {
				passages = append(passages, in)
			} else {
				external = append(external, in)
			}
		}

		tp, isSwitchPort := swPorts[port]
		if !isSwitchPort {
			if len(passages) > 0 {
				return nil, fmt.Errorf("track: порт %s не принадлежит стрелке, но в нём проход %s",
					port, passages[0].Element)
			}
			switch len(external) {
			case 0, 1:
			case 2:
				link(external[0], external[1], true)
			default:
				return nil, fmt.Errorf("track: порт %s обслуживает %d рёбер — развилку нужно оформить стрелкой",
					port, len(external))
			}
			continue
		}

		// Порт стрелки: ровно одно внешнее ребро. Два сделали бы правило
		// ориентации противоречивым, ноль оставил бы стрелку висящей.
		if len(external) != 1 {
			return nil, fmt.Errorf("track: порт %s стрелки %s обслуживает %d внешних рёбер, требуется ровно одно",
				port, tp.turnout, len(external))
		}
		want := 1
		if tp.role == "common" {
			want = 2
		}
		if len(passages) != want {
			return nil, fmt.Errorf("track: порт %s стрелки %s: проходов %d, ожидалось %d",
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

func isPassage(elementID string) bool {
	return strings.HasSuffix(elementID, mapfmt.PassageStraight) ||
		strings.HasSuffix(elementID, mapfmt.PassageDiverging)
}

// endSlope возвращает уклон на указанном конце элемента, измеренный ВНУТРЬ
// элемента: на From это уклон в начале, на To — уклон в конце со сменой знака,
// потому что поза там развёрнута.
func endSlope(e Element, at Incidence) (float64, error) {
	switch at.Port {
	case e.From:
		_, s, err := e.Prof.At(0)
		return s, err
	case e.To:
		_, s, err := e.Prof.At(e.Prof.LengthU())
		return -s, err
	default:
		return 0, fmt.Errorf("track: конец %s не принадлежит элементу %s", at, e.ID)
	}
}

// across переносит позу с одного конца элемента на другой.
//
// Обратный ход не разворачивает цепочку по звеньям: цепочка целиком есть одно
// жёсткое движение Δ, и начало восстанавливается как Compose(конец, Invert(Δ)).
func across(e Element, from Incidence, at PortPose) (Incidence, PortPose, error) {
	if from.Element != e.ID {
		return Incidence{}, PortPose{}, fmt.Errorf("track: конец %s не принадлежит элементу %s", from, e.ID)
	}
	dz, _, err := e.Prof.At(e.Prof.LengthU())
	if err != nil {
		return Incidence{}, PortPose{}, fmt.Errorf("track: %s: подъём по профилю: %w", e.ID, err)
	}
	delta := e.Plan.End(geom.Pose{})

	switch from.Port {
	case e.From:
		end := geom.Compose(at.Plan, delta)
		slope, err := endSlope(e, e.toInc())
		if err != nil {
			return Incidence{}, PortPose{}, err
		}
		return e.toInc(), PortPose{Plan: geom.Reverse(end), Z: at.Z + dz, Slope: slope}, nil
	case e.To:
		travelEnd := geom.Reverse(at.Plan)
		start := geom.Compose(travelEnd, geom.Invert(delta))
		slope, err := endSlope(e, e.fromInc())
		if err != nil {
			return Incidence{}, PortPose{}, err
		}
		return e.fromInc(), PortPose{Plan: start, Z: at.Z - dz, Slope: slope}, nil
	default:
		return Incidence{}, PortPose{}, fmt.Errorf("track: конец %s не принадлежит элементу %s", from, e.ID)
	}
}

// settle записывает позу конца или проверяет уже записанную на невязку.
//
// Сравниваются позы ОДНОГО И ТОГО ЖЕ конца, поэтому выбора ориентации здесь нет
// и результат не зависит от порядка обхода.
func settle(poses map[Incidence]PortPose, at Incidence, want PortPose) error {
	got, seen := poses[at]
	if !seen {
		poses[at] = want
		return nil
	}
	dxy := math.Hypot(got.Plan.X-want.Plan.X, got.Plan.Y-want.Plan.Y)
	dz := math.Abs(got.Z - want.Z)
	tol := TolPosition.Meters()
	if dxy > tol || dz > tol {
		return fmt.Errorf("track: невязка замыкания в %s: %.4f мм по плану, %.4f мм по высоте",
			at, dxy*1000, dz*1000)
	}
	dh := geom.Normalize(geom.Pose{Heading: got.Plan.Heading - want.Plan.Heading}).Heading
	if math.Abs(dh) > TolHeading {
		return fmt.Errorf("track: невязка замыкания в %s: %.3e рад по направлению", at, math.Abs(dh))
	}
	if ds := math.Abs(got.Slope - want.Slope); ds > TolSlope {
		return fmt.Errorf("track: невязка замыкания в %s: %.3e по уклону (%.4f‰ против %.4f‰)",
			at, ds, got.Slope*1000, want.Slope*1000)
	}
	return nil
}

// buildElements переводит выравнивания карты в цепочки geom и профили.
func buildElements(m *mapfmt.Map) (map[string]Element, error) {
	ends := m.ElementEnds()
	// Виды берутся той же таблицей, что и концы, и по тому же ключу: вид не
	// выводится здесь заново — правило «откуда он у прохода» живёт в mapfmt.
	kinds := m.ElementKinds()

	out := map[string]Element{}
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
				return nil, fmt.Errorf("track: %s[%d]: %w", id, i, err)
			}
			chain = append(chain, p)
		}
		prof, err := ProfileFrom(a, chain.Length())
		if err != nil {
			return nil, fmt.Errorf("track: %s: %w", id, err)
		}
		hprof, err := HProfileFrom(a.Horizontal)
		if err != nil {
			return nil, fmt.Errorf("track: %s: %w", id, err)
		}
		e, ok := ends[id]
		if !ok {
			return nil, fmt.Errorf("track: у элемента %s нет концов", id)
		}
		out[id] = Element{ID: id, Kind: kinds[id], From: e[0], To: e[1], Plan: chain, Prof: prof, HProf: hprof}
	}
	return out, nil
}
