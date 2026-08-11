package track

import (
	"fmt"
	"sort"

	"github.com/shady2k/ClearAhead/server/internal/geom"
	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/netloc"
	"github.com/shady2k/ClearAhead/server/internal/units"
)

// CompiledElement — линейный элемент после компиляции. Длины в s.
type CompiledElement struct {
	ID string
	// Kind — вид пути (mapfmt.KindRail). Входит в track_hash: это факт о самой
	// модели пути, а не украшение провода.
	Kind    string
	From    string
	To      string
	LengthU units.Distance
	LengthS units.Distance
	Prof    Profile
}

// Протяжённость путевого объекта живёт в общем типе netloc: одна форма на файл,
// компиляцию и провод, координата — параметр типа (бида ClearAhead-xm7).

// Traversal — направленный переход устройства: проход от порта к порту.
type Traversal struct {
	Passage string
	From    string
	To      string
}

// CompiledDevice — путевое устройство после компиляции.
//
// Число портов и набор переходов НЕ зашиты: глухое пересечение с четырьмя
// портами и двумя непересекающимися проходами описывается той же формой, что
// обыкновенная стрелка (map-content-design §4). В файле карты стрелка при этом
// остаётся трёхпортовой записью — обобщена форма, которую видит код, а не та,
// которую пишет автор.
//
// Все проходы делят ОДИН ресурс: положение остряка определяет разрешённый
// переход, занятость живёт на сегментах.
//
// Состояний и конфликтов переходов здесь нет намеренно: их потребитель —
// замыкание маршрута, а централизации в проекте ещё нет, и объявленная сейчас
// форма оказалась бы неверной (map-format-design §8).
type CompiledDevice struct {
	ID string
	// Hand — рукость. Свойство стрелки; у устройства без ветвления пусто.
	Hand       string
	Ports      []string
	Traversals []Traversal
	Resource   string
}

// CompiledTrack — вход физики и безопасности. Координат не содержит.
type CompiledTrack struct {
	MapID     string
	Revision  int
	Elements  map[string]CompiledElement
	Trackside map[string]netloc.LinearS
	Devices   map[string]CompiledDevice
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
	ID string `json:"id"`
	// Kind — вид пути: "rail" (mapfmt.KindRail). Ресурс network называет КЛАСС
	// содержимого, а не вид, и автомобильные дороги приедут в этот же ответ —
	// различать их клиенту нужно по полю, а не по адресу. Поле обязательное и
	// без omitempty: пустой вид в проводе означал бы, что клиент вправе
	// додумать его сам.
	Kind  string            `json:"kind"`
	Start PortPose          `json:"start"`
	Prims []RenderPrimitive `json:"primitives"`
	Role  *RenderRole       `json:"role,omitempty"`
}

// RenderTrackside — путевой объект на плане (платформа и пр.).
//
// Размеры платформы — offset (от оси пути до ближней кромки) и width (поперёк)
// — едут на самом объекте (спека §3): платформа — самостоятельный путевой
// объект, тип решётки её размеры не определяет. Точечные объекты (buffer_stop)
// размеров не несут.
type RenderTrackside struct {
	ID     string         `json:"id"`
	Kind   string         `json:"kind"`
	Side   string         `json:"side,omitempty"`
	Offset float64        `json:"offset,omitempty"`
	Width  float64        `json:"width,omitempty"`
	Spans  netloc.LinearU `json:"spans"`
}

// RenderGeometry — вход клиента и инструментов.
//
// Четыре новых корневых поля — типы, run'ы, особенности и версия алгоритма
// размещения (спека §4): клиент больше не выдумывает решётку, а рисует по
// рецепту. Массивы непустые даже для карты без блока construction: форма
// контракта — «[]», а не null.
type RenderGeometry struct {
	// Region и Revision называют ТОТ РЕСУРС, КОТОРЫЙ СПРОСИЛИ:
	// GET /regions/{region}/revisions/{n}/network. До ClearAhead-z4u корневые
	// поля звались map_id и map_revision, а манифест региона рядом отдавал
	// region и revision, — клиент видел две системы имён в соседних ответах.
	//
	// Регион и карта — ОДНО И ТО ЖЕ, и это принятое решение, а не совпадение:
	// world-storage-and-zones-design §3 — «map_id обозначает РЕГИОН, а не
	// станцию; станция — именованная область внутри региона». Строка
	// `region := m.MapID` в worldgen.Bootstrap — реализация этого решения.
	//
	// Оговорка нужна потому, что в коде живут оба слова: «карта» (mapstore,
	// MapID, /maps — авторская сторона) и «регион» (/regions — сторона мира), и
	// ниоткуда не видно, что они называют одну сущность.
	//
	// Регион существует по ГЕОДЕЗИЧЕСКОЙ причине, а не по складской: у него свой
	// frame (датум, origin, азимут оси X, ground_to_grid), и сторона до ~50 км
	// выбрана так, чтобы плоское приближение оставалось честным — 50 км дают
	// отклонение около 13 см, 100 км уже неприемлемо. Сеть Краснодарского края
	// 400×400 км — это не одна карта на многих регионах, а МНОГО РЕГИОНОВ,
	// сшитых явно: стык — отдельная сущность (пара frame'ов плюс преобразование),
	// а пересекающий его путь — два элемента со связью, а не один длинный.
	Region             string            `json:"region"`
	Revision           int               `json:"revision"`
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
	ID         string         `json:"id"`
	Type       string         `json:"type"`
	Coordinate string         `json:"coordinate"`
	Phase      float64        `json:"phase"`
	Spans      netloc.LinearU `json:"spans"`
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
		Trackside: map[string]netloc.LinearS{},
		Devices:   make(map[string]CompiledDevice, len(m.Topology.Turnouts)),
	}
	// Проходы стрелок: элемент {ID}:straight или {ID}:diverging получает роль
	// ветви. Обычные рёбра роли не несут.
	type passageRole struct {
		turnout mapfmt.Turnout
		branch  string
	}
	passage := map[string]passageRole{}
	for _, t := range m.Topology.Turnouts {
		for _, ps := range t.Passages() {
			passage[ps.ID] = passageRole{t, ps.Branch}
		}
	}
	rg := &RenderGeometry{
		// m.MapID кладётся в region без перевода: карта И ЕСТЬ регион
		// (world-storage §3, см. поле Region).
		Region:             m.MapID,
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
			Kind:    e.Kind,
			From:    e.From,
			To:      e.To,
			LengthU: e.Plan.Length(),
			LengthS: lengthS,
			Prof:    e.Prof,
		}
		re := RenderElement{ID: id, Kind: e.Kind, Start: e.Start, Prims: make([]RenderPrimitive, 0, len(e.Plan))}
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
		ps := t.Passages()
		tr := make([]Traversal, 0, len(ps))
		for _, p := range ps {
			tr = append(tr, Traversal{Passage: p.ID, From: p.From, To: p.To})
		}
		ct.Devices[t.ID] = CompiledDevice{
			ID:         t.ID,
			Hand:       t.Hand,
			Ports:      t.PortIDs(),
			Traversals: tr,
			Resource:   "RES_" + t.ID,
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
			Spans:  make(netloc.LinearU, 0, len(ts.Span)),
		}
		spans := make(netloc.LinearS, 0, len(ts.Span))
		for _, iv := range ts.Span {
			e, ok := els[iv.Element]
			if !ok {
				return nil, nil, fmt.Errorf("track: объект %s ссылается на элемент %s, которого нет", ts.ID, iv.Element)
			}
			// Спан в u уезжает клиенту как есть, из карты: план рисуется в u.
			rt.Spans = append(rt.Spans, iv)
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
			spans = append(spans, netloc.IntervalS{
				Element:   iv.Element,
				From:      fromS,
				To:        toS,
				Direction: iv.Direction,
			})
		}
		rg.Trackside = append(rg.Trackside, rt)
		ct.Trackside[ts.ID] = spans
	}
	return ct, rg, nil
}
