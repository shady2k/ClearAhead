package track

import (
	"fmt"
	"sort"

	"github.com/shady2k/ClearAhead/server/internal/geom"
	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/units"
)

// CompiledElement — линейный элемент после компиляции. Длины в s.
type CompiledElement struct {
	ID      string
	From    string
	To      string
	LengthU units.Distance
	LengthS units.Distance
	Prof    Profile
}

// TrackSpanS — интервал путевого объекта в пространственной координате.
type TrackSpanS struct {
	Element string
	FromS   units.Distance
	ToS     units.Distance
}

// CompiledTurnout — стрелка после компиляции. Оба прохода делят один ресурс:
// положение остряка определяет разрешённый переход, занятость живёт на сегментах.
type CompiledTurnout struct {
	ID        string
	Hand      string
	Common    string
	Straight  string
	Diverging string
	Resource  string
}

// CompiledTrack — вход физики и безопасности. Координат не содержит.
type CompiledTrack struct {
	MapID     string
	Revision  int
	Elements  map[string]CompiledElement
	Trackside map[string][]TrackSpanS
	Turnouts  map[string]CompiledTurnout
}

// RenderPrimitive — примитив плана для клиента. Метры и радианы.
type RenderPrimitive struct {
	Kind    string  `json:"kind"`
	LengthM float64 `json:"length"`
	Radius  float64 `json:"radius,omitempty"`
	Angle   float64 `json:"angle,omitempty"`
}

// RenderElement — элемент с абсолютной стартовой позой: клиент рисует цепочку
// от неё и ничего не пересчитывает.
type RenderElement struct {
	ID    string            `json:"id"`
	Start PortPose          `json:"start"`
	Prims []RenderPrimitive `json:"primitives"`
}

// RenderGeometry — вход клиента и инструментов.
type RenderGeometry struct {
	MapID    string          `json:"map_id"`
	Revision int             `json:"map_revision"`
	Elements []RenderElement `json:"elements"`
}

// Compile строит оба артефакта из одной карты.
func Compile(m *mapfmt.Map) (*CompiledTrack, *RenderGeometry, error) {
	_, els, err := Propagate(m)
	if err != nil {
		return nil, nil, err
	}

	ids := make([]string, 0, len(els))
	for id := range els {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	ct := &CompiledTrack{
		MapID:     m.MapID,
		Revision:  m.MapRevision,
		Elements:  make(map[string]CompiledElement, len(els)),
		Trackside: map[string][]TrackSpanS{},
		Turnouts:  make(map[string]CompiledTurnout, len(m.Topology.Turnouts)),
	}
	rg := &RenderGeometry{MapID: m.MapID, Revision: m.MapRevision}

	for _, id := range ids {
		e := els[id]
		lengthS, err := e.Prof.LengthS()
		if err != nil {
			return nil, nil, fmt.Errorf("track: %s: %w", id, err)
		}
		ct.Elements[id] = CompiledElement{
			ID:      id,
			From:    e.From,
			To:      e.To,
			LengthU: e.Plan.Length(),
			LengthS: lengthS,
			Prof:    e.Prof,
		}
		re := RenderElement{ID: id, Start: e.Start, Prims: make([]RenderPrimitive, 0, len(e.Plan))}
		for _, p := range e.Plan {
			switch p.Kind {
			case geom.KindStraight:
				re.Prims = append(re.Prims, RenderPrimitive{Kind: "straight", LengthM: p.Length.Meters()})
			default:
				re.Prims = append(re.Prims, RenderPrimitive{
					Kind: "arc", LengthM: p.Length.Meters(), Radius: p.Radius, Angle: p.Angle,
				})
			}
		}
		rg.Elements = append(rg.Elements, re)
	}

	for _, t := range m.Topology.Turnouts {
		ct.Turnouts[t.ID] = CompiledTurnout{
			ID:        t.ID,
			Hand:      t.Hand,
			Common:    t.ID + "." + t.Ports.Common,
			Straight:  t.ID + "." + t.Ports.Straight,
			Diverging: t.ID + "." + t.Ports.Diverging,
			Resource:  "RES_" + t.ID,
		}
	}

	for _, ts := range m.Topology.Trackside {
		spans := make([]TrackSpanS, 0, len(ts.Span))
		for _, iv := range ts.Span {
			e, ok := els[iv.Element]
			if !ok {
				return nil, nil, fmt.Errorf("track: объект %s ссылается на элемент %s, которого нет", ts.ID, iv.Element)
			}
			fromU, err := units.MetersToDistance(iv.From)
			if err != nil {
				return nil, nil, fmt.Errorf("track: объект %s: %w", ts.ID, err)
			}
			toU, err := units.MetersToDistance(iv.To)
			if err != nil {
				return nil, nil, fmt.Errorf("track: объект %s: %w", ts.ID, err)
			}
			fromS, err := e.Prof.UToS(fromU)
			if err != nil {
				return nil, nil, fmt.Errorf("track: объект %s: начало: %w", ts.ID, err)
			}
			toS, err := e.Prof.UToS(toU)
			if err != nil {
				return nil, nil, fmt.Errorf("track: объект %s: конец: %w", ts.ID, err)
			}
			spans = append(spans, TrackSpanS{Element: iv.Element, FromS: fromS, ToS: toS})
		}
		ct.Trackside[ts.ID] = spans
	}
	return ct, rg, nil
}
