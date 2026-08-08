package track

import (
	"fmt"
	"math"
	"sort"

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

// PortPose — поза порта: план из geom плюс высота.
//
// Z не входит в geom.Pose сознательно: geom считает план, вертикаль — отдельная
// одномерная функция. Смешивать их означало бы тащить профиль в модуль, который
// о нём ничего не знает.
type PortPose struct {
	Plan geom.Pose
	Z    float64
}

// Element — линейный элемент: ребро или проход стрелки.
type Element struct {
	ID    string
	From  string
	To    string
	Plan  geom.Chain
	Prof  Profile
	Start PortPose // поза порта From, направленная внутрь элемента
}

// Propagate выводит позы всех портов от якорей и проверяет замыкание циклов.
//
// Обход детерминированный: элементы сортируются по ID, потому что порядок обхода
// определяет, какая поза окажется вычисленной первой, а какая — проверенной на
// невязку, и от этого зависит текст ошибки.
func Propagate(m *mapfmt.Map) (map[string]PortPose, map[string]Element, error) {
	els, err := buildElements(m)
	if err != nil {
		return nil, nil, err
	}

	byPort := map[string][]string{}
	ids := make([]string, 0, len(els))
	for id := range els {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		e := els[id]
		byPort[e.From] = append(byPort[e.From], id)
		byPort[e.To] = append(byPort[e.To], id)
	}

	poses := map[string]portState{}
	visited := map[string]bool{}

	anchorIDs := make([]string, 0, len(m.Anchors))
	for id := range m.Anchors {
		anchorIDs = append(anchorIDs, id)
	}
	sort.Strings(anchorIDs)

	for _, aID := range anchorIDs {
		if visited[aID] {
			return nil, nil, fmt.Errorf("track: в одной связной компоненте два якоря, второй — %s", aID)
		}
		a := m.Anchors[aID]
		// Якорь принадлежит первому элементу порта в отсортированном порядке —
		// обход детерминирован, поэтому и владелец детерминирован.
		if len(byPort[aID]) == 0 {
			return nil, nil, fmt.Errorf("track: якорь %s не принадлежит ни одному элементу", aID)
		}
		poses[aID] = portState{
			Pose:  PortPose{Plan: geom.Normalize(geom.Pose{X: a.X, Y: a.Y, Heading: a.Heading}), Z: a.Z},
			Owner: byPort[aID][0],
		}

		queue := []string{aID}
		for len(queue) > 0 {
			port := queue[0]
			queue = queue[1:]
			if visited[port] {
				continue
			}
			visited[port] = true

			for _, eID := range byPort[port] {
				at := poses[port].as(eID)
				far, want, err := across(els[eID], port, at)
				if err != nil {
					return nil, nil, err
				}
				if err := settle(poses, far, want, eID); err != nil {
					return nil, nil, err
				}
				if !visited[far] {
					queue = append(queue, far)
				}
			}
		}
	}

	out := make(map[string]PortPose, len(poses))
	for id, st := range poses {
		out[id] = st.Pose
	}
	for id, e := range els {
		st, ok := poses[e.From]
		if !ok {
			return nil, nil, fmt.Errorf("track: элемент %s в компоненте без якоря", id)
		}
		e.Start = st.as(id)
		els[id] = e
	}
	return out, els, nil
}

// portState — поза порта и элемент, для которого она записана.
//
// Поза порта всегда смотрит ВНУТРЬ элемента. У стыка элементов два, и смотрят
// они в противоположные стороны, поэтому одного значения мало: надо помнить,
// чьё оно. Соседний элемент получает то же значение, развёрнутое на π.
type portState struct {
	Pose  PortPose
	Owner string
}

// as возвращает позу в системе указанного элемента.
func (s portState) as(element string) PortPose {
	if s.Owner == element {
		return s.Pose
	}
	return PortPose{Plan: geom.Reverse(s.Pose.Plan), Z: s.Pose.Z}
}

// across переносит позу с одного конца элемента на другой.
//
// Поза порта всегда смотрит ВНУТРЬ своего элемента. У From это совпадает с
// направлением движения по цепочке, у To — противоположно ему, отсюда Reverse
// в обе стороны.
//
// Обратный ход не разворачивает цепочку по звеньям: цепочка целиком есть одно
// жёсткое движение Δ, и начало восстанавливается как Compose(конец, Invert(Δ)).
func across(e Element, from string, at PortPose) (string, PortPose, error) {
	dz, _, err := e.Prof.At(e.Prof.LengthU())
	if err != nil {
		return "", PortPose{}, fmt.Errorf("track: %s: подъём по профилю: %w", e.ID, err)
	}
	delta := e.Plan.End(geom.Pose{})

	switch from {
	case e.From:
		end := geom.Compose(at.Plan, delta)
		return e.To, PortPose{Plan: geom.Reverse(end), Z: at.Z + dz}, nil
	case e.To:
		travelEnd := geom.Reverse(at.Plan)
		start := geom.Compose(travelEnd, geom.Invert(delta))
		return e.From, PortPose{Plan: start, Z: at.Z - dz}, nil
	default:
		return "", PortPose{}, fmt.Errorf("track: порт %s не принадлежит элементу %s", from, e.ID)
	}
}

// settle записывает позу порта или проверяет уже записанную на невязку.
//
// Второй элемент в порту — это стык, и именно здесь замыкание перестаёт быть
// декоративным: до введения стыков порт принадлежал ровно одному ребру, два
// пути никогда не сходились, и проверка не срабатывала ни разу.
func settle(poses map[string]portState, port string, want PortPose, via string) error {
	st, seen := poses[port]
	if !seen {
		poses[port] = portState{Pose: want, Owner: via}
		return nil
	}
	got := st.as(via)
	dx := got.Plan.X - want.Plan.X
	dy := got.Plan.Y - want.Plan.Y
	dz := got.Z - want.Z
	dxy := math.Hypot(dx, dy)
	tol := TolPosition.Meters()
	if dxy > tol || math.Abs(dz) > tol {
		return fmt.Errorf("track: невязка замыкания в порту %s через %s: %.4f мм по плану, %.4f мм по высоте",
			port, via, dxy*1000, math.Abs(dz)*1000)
	}
	dh := geom.Normalize(geom.Pose{Heading: got.Plan.Heading - want.Plan.Heading}).Heading
	if math.Abs(dh) > TolHeading {
		return fmt.Errorf("track: невязка замыкания в порту %s через %s: %.3e рад по направлению", port, via, math.Abs(dh))
	}
	return nil
}

// buildElements переводит выравнивания карты в цепочки geom и профили.
func buildElements(m *mapfmt.Map) (map[string]Element, error) {
	ends := map[string][2]string{}
	for _, e := range m.Topology.Edges {
		ends[e.ID] = [2]string{e.From, e.To}
	}
	for _, t := range m.Topology.Turnouts {
		ends[t.ID+mapfmt.PassageStraight] = [2]string{t.ID + "." + t.Ports.Common, t.ID + "." + t.Ports.Straight}
		ends[t.ID+mapfmt.PassageDiverging] = [2]string{t.ID + "." + t.Ports.Common, t.ID + "." + t.Ports.Diverging}
	}

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
		e, ok := ends[id]
		if !ok {
			return nil, fmt.Errorf("track: у элемента %s нет концов", id)
		}
		out[id] = Element{ID: id, From: e[0], To: e[1], Plan: chain, Prof: prof}
	}
	return out, nil
}
