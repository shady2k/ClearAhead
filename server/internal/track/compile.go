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

// RenderRole — роль элемента в схеме. У обычного пути отсутствует; у ветви
// стрелки несёт всё, что клиенту нужно для остряков и крестовины: к какой
// стрелке относится ветвь, какая она, рука и марка крестовины. Марка
// необязательна и в карте, и в проводе (спека §7): крестовина строится из
// особенности frog (§5), марка показывается подписью, если есть.
type RenderRole struct {
	Turnout string `json:"turnout"`        // ID стрелки, напр. ST_A_SW_1
	Branch  string `json:"branch"`         // "straight" | "diverging"
	Hand    string `json:"hand"`           // "right" | "left"
	Frog    string `json:"frog,omitempty"` // марка крестовины, напр. "1/9"
}

// RenderElement — элемент с абсолютной стартовой позой: клиент рисует цепочку
// от неё и ничего не пересчитывает.
type RenderElement struct {
	ID    string            `json:"id"`
	Start PortPose          `json:"start"`
	Prims []RenderPrimitive `json:"primitives"`
	Role  *RenderRole       `json:"role,omitempty"`
}

// RenderSpan — интервал путевого объекта в координате u на одном элементе.
// Метры u берутся из карты как есть: клиент рисует план, а s — координата
// симуляции, из неё в u конвертировать нельзя.
type RenderSpan struct {
	Element string  `json:"element"`
	FromM   float64 `json:"from"`
	ToM     float64 `json:"to"`
}

// RenderTrackside — путевой объект на плане (платформа и пр.).
//
// Размеры платформы — offset (от оси пути до ближней кромки) и width (поперёк)
// — едут на самом объекте (спека §3): платформа — самостоятельный путевой
// объект, тип решётки её размеры не определяет. Точечные объекты (buffer_stop)
// размеров не несут.
type RenderTrackside struct {
	ID     string       `json:"id"`
	Kind   string       `json:"kind"`
	Side   string       `json:"side,omitempty"`
	Offset float64      `json:"offset,omitempty"`
	Width  float64      `json:"width,omitempty"`
	Spans  []RenderSpan `json:"spans"`
}

// RenderGeometry — вход клиента и инструментов.
//
// Четыре новых корневых поля — типы, run'ы, особенности и версия алгоритма
// размещения (спека §4): клиент больше не выдумывает решётку, а рисует по
// рецепту. Массивы непустые даже для карты без блока construction: форма
// контракта — «[]», а не null.
type RenderGeometry struct {
	MapID              string            `json:"map_id"`
	Revision           int               `json:"map_revision"`
	Elements           []RenderElement   `json:"elements"`
	Trackside          []RenderTrackside `json:"trackside,omitempty"`
	TrackTypes         []RenderTrackType `json:"track_types"`
	ConstructionRuns   []RenderRun       `json:"construction_runs"`
	Features           []RenderFeature   `json:"features"`
	PlacementAlgorithm string            `json:"placement_algorithm"`
}

// RenderTrackType — тип путевой конструкции в проводе (спека §3).
type RenderTrackType struct {
	ID      string        `json:"id"`
	Gauge   float64       `json:"gauge"`
	Sleeper RenderSleeper `json:"sleeper"`
	Ballast RenderBallast `json:"ballast"`
}

type RenderSleeper struct {
	Pitch  float64 `json:"pitch"`
	Length float64 `json:"length"`
	Width  float64 `json:"width"`
}

type RenderBallast struct {
	HalfWidth float64 `json:"half_width"`
}

// RenderRun — run размещения в проводе (спека §4). Type всегда явный:
// умолчание разрешил компилятор, клиент скрытого умолчания не применяет
// никогда. Спаны — в авторском порядке прохождения.
type RenderRun struct {
	ID         string          `json:"id"`
	Type       string          `json:"type"`
	Coordinate string          `json:"coordinate"`
	Phase      float64         `json:"phase"`
	Spans      []RenderRunSpan `json:"spans"`
}

type RenderRunSpan struct {
	Element   string  `json:"element"`
	From      float64 `json:"from"`
	To        float64 `json:"to"`
	Direction string  `json:"direction"`
}

// RenderFeature — особенность уровня 2 (спека §5): один канонический ответ на
// вопрос «где именно». Сейчас единственный вид — крестовина (frog).
type RenderFeature struct {
	Owner     string          `json:"owner"`
	Kind      string          `json:"kind"`
	Point     RenderPoint     `json:"point"`
	Addresses []RenderAddress `json:"addresses"`
}

// RenderPoint — физическая точка особенности. Для крестовины это пересечение
// офсетных ниток, а не поза ни одной из осевых линий (спека §5).
type RenderPoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// RenderAddress — адрес особенности на осевой линии прохода: u — позиция,
// tangent — единичный, по ходу возрастания u, направленный внутрь адреса.
type RenderAddress struct {
	Element string    `json:"element"`
	U       float64   `json:"u"`
	Tangent RenderVec `json:"tangent"`
}

type RenderVec struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// PlacementAlgorithm — версия алгоритма размещения решётки, часть артефакта:
// смена алгоритма не должна молча менять старую ревизию (спека §4).
const PlacementAlgorithm = "placement-v1"

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
	// Проходы стрелок: элемент {ID}:straight или {ID}:diverging получает роль
	// ветви. Обычные рёбра роли не несут.
	type passageRole struct {
		turnout mapfmt.Turnout
		branch  string
	}
	passage := map[string]passageRole{}
	for _, t := range m.Topology.Turnouts {
		passage[t.ID+mapfmt.PassageStraight] = passageRole{t, "straight"}
		passage[t.ID+mapfmt.PassageDiverging] = passageRole{t, "diverging"}
	}
	rg := &RenderGeometry{
		MapID:              m.MapID,
		Revision:           m.MapRevision,
		TrackTypes:         []RenderTrackType{},
		ConstructionRuns:   []RenderRun{},
		Features:           []RenderFeature{},
		PlacementAlgorithm: PlacementAlgorithm,
	}

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
		if p, ok := passage[id]; ok {
			re.Role = &RenderRole{
				Turnout: p.turnout.ID,
				Branch:  p.branch,
				Hand:    p.turnout.Hand,
				Frog:    p.turnout.Frog,
			}
		}
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

	// Рецепт решётки и особенности устройств (спека §3–5): типы, run'ы и
	// крестовины уезжают в контракт. Компилятор разрешает умолчание типа —
	// в проводе у каждого run ссылка всегда явная.
	if err := fillConstruction(m, rg); err != nil {
		return nil, nil, err
	}
	if err := buildFrogFeatures(m, els, rg); err != nil {
		return nil, nil, err
	}

	for _, ts := range m.Topology.Trackside {
		rt := RenderTrackside{
			ID:     ts.ID,
			Kind:   ts.Kind,
			Side:   ts.Side,
			Offset: ts.Offset,
			Width:  ts.Width,
			Spans:  make([]RenderSpan, 0, len(ts.Span)),
		}
		spans := make([]TrackSpanS, 0, len(ts.Span))
		for _, iv := range ts.Span {
			e, ok := els[iv.Element]
			if !ok {
				return nil, nil, fmt.Errorf("track: объект %s ссылается на элемент %s, которого нет", ts.ID, iv.Element)
			}
			// Спан в u уезжает клиенту как есть, из карты: план рисуется в u.
			rt.Spans = append(rt.Spans, RenderSpan{Element: iv.Element, FromM: iv.From, ToM: iv.To})
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
		rg.Trackside = append(rg.Trackside, rt)
		ct.Trackside[ts.ID] = spans
	}
	return ct, rg, nil
}
