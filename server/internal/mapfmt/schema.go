package mapfmt

// Map — файл карты как он записан. Метры, радианы, промилле.
type Map struct {
	FormatVersion int               `json:"format_version"`
	MapID         string            `json:"map_id"`
	MapRevision   int               `json:"map_revision"`
	Georeference  *Georeference     `json:"georeference,omitempty"`
	Anchors       map[string]Anchor `json:"anchors"`
	Topology      Topology          `json:"topology"`
	Geometry      Geometry          `json:"geometry"`
	// Construction — рецепт путевой решётки (спека контракта отрисовки §3–4).
	// Блок соседствует с topology и geometry; отсутствие блока означает, что
	// авторинг ещё не породил решётку, и валидатор пропускает модуль (решение
	// зафиксировано в construction.go).
	Construction *Construction `json:"construction,omitempty"`
}

// Georeference — метаданные привязки. В В1 сохраняется и проверяется по форме,
// компилятором не используется (спека §4).
type Georeference struct {
	Datum            string            `json:"datum"`
	Origin           Origin            `json:"origin"`
	OriginHeightKind string            `json:"origin_height_kind"`
	XAxisAzimuthDeg  float64           `json:"x_axis_azimuth_deg"`
	GroundToGrid     float64           `json:"ground_to_grid"`
	Provenance       map[string]string `json:"provenance,omitempty"`
}

type Origin struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
	H   float64 `json:"h"`
}

// Anchor — единственная абсолютная поза связной компоненты. Позы остальных
// портов выводятся распространением (спека §7).
type Anchor struct {
	// Element — элемент, ВНУТРЬ которого смотрит Heading.
	//
	// Обязателен, когда в порту сходится больше одного конца: у общего порта
	// стрелки их три, и «направление порта» там не определено. На порту с одним
	// элементом поле избыточно и может отсутствовать.
	Element string  `json:"element,omitempty"`
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
	Z       float64 `json:"z"`
	Heading float64 `json:"heading"`
}

type Topology struct {
	Nodes     []Node      `json:"nodes"`
	Turnouts  []Turnout   `json:"turnouts"`
	Edges     []Edge      `json:"edges"`
	Trackside []Trackside `json:"trackside"`
}

type Node struct {
	ID    string `json:"id"`
	Ports []Port `json:"ports"`
}

// Port.Purpose пуст у обычного порта. "buffer_stop" и "map_boundary" делают
// висящее ребро законным (спека §10.3).
type Port struct {
	ID      string `json:"id"`
	Purpose string `json:"purpose,omitempty"`
}

type Turnout struct {
	ID   string `json:"id"`
	Hand string `json:"hand"`
	Frog string `json:"frog,omitempty"`
	// Type — тип путевой конструкции самого устройства (спека §4): опущен —
	// применяется construction.default_type, компилятор разрешает умолчание и в
	// проводе ссылка всегда явная. Крестовина (§5) считается по колее ИМЕННО
	// этого типа, а не типов примыкающих run'ов.
	Type  string       `json:"type,omitempty"`
	Ports TurnoutPorts `json:"ports"`
}

type TurnoutPorts struct {
	Common    string `json:"common"`
	Straight  string `json:"straight"`
	Diverging string `json:"diverging"`
}

type Edge struct {
	ID   string `json:"id"`
	From string `json:"from"`
	To   string `json:"to"`
}

type Trackside struct {
	ID     string         `json:"id"`
	Kind   string         `json:"kind"`
	Span   []SpanInterval `json:"span"`
	Side   string         `json:"side,omitempty"`
	Offset float64        `json:"offset,omitempty"`
	Width  float64        `json:"width,omitempty"`
}

// SpanInterval — интервал в координате u на одном элементе. Список интервалов
// позволяет объекту пересекать границы элементов и проходы стрелок.
type SpanInterval struct {
	Element string  `json:"element"`
	From    float64 `json:"from"`
	To      float64 `json:"to"`
}

type Geometry struct {
	Turnouts map[string]TurnoutGeometry `json:"turnouts"`
	Edges    map[string]Alignments      `json:"edges"`
}

type TurnoutGeometry struct {
	Straight  Alignments `json:"straight"`
	Diverging Alignments `json:"diverging"`
}

// Alignments — три функции над общим доменом [0, U]. Cant отложен целиком и
// поля под него не заводятся (спека §6).
type Alignments struct {
	Horizontal []HPrim `json:"horizontal"`
	Vertical   []VPrim `json:"vertical,omitempty"`
}

// HPrim — примитив плана. Kind: "straight" | "arc".
type HPrim struct {
	Kind   string  `json:"kind"`
	Length float64 `json:"length,omitempty"`
	Radius float64 `json:"radius,omitempty"`
	Angle  float64 `json:"angle,omitempty"`
}

// VPrim — примитив профиля. Kind: "grade" | "vertical_curve".
//
// У grade постоянный уклон SlopePermille. У vertical_curve уклон меняется по u
// линейно от уклона предыдущего элемента до EndSlopePermille — это парабола.
type VPrim struct {
	Kind             string  `json:"kind"`
	Length           float64 `json:"length"`
	SlopePermille    float64 `json:"slope_permille,omitempty"`
	EndSlopePermille float64 `json:"end_slope_permille,omitempty"`
}

// Construction — блок рецепта путевой решётки (спека контракта отрисовки §3–4).
//
// Типы, run'ы и умолчание живут в файле карты, а не отдельным ресурсом и не в
// коде сервера: привязка к ревизии получается по построению — правка типа
// меняет тело геометрии и обязана сменить ревизию карты.
type Construction struct {
	// DefaultType — тип по умолчанию: применяется к run'у с опущенным type и к
	// устройству с опущенным type. В проводе ссылка всегда явная — умолчание
	// разрешает компилятор, клиент скрытого умолчания не применяет никогда.
	DefaultType string            `json:"default_type"`
	Types       []TrackType       `json:"types"`
	Runs        []ConstructionRun `json:"runs"`
}

// TrackType — тип путевой конструкции (спека §3).
//
// Семантика чисел задана точно, иначе два рендерера реализуют разное:
// gauge — между внутренними рабочими гранями головок рельсов; символические
// нитки схемы ставятся на ±gauge/2 от осевой; sleeper.length — поперёк пути,
// sleeper.width — вдоль; ballast.half_width — по верху призмы.
// shoulder_slope и sleeper.type сознательно НЕ заводятся (спека §8).
type TrackType struct {
	ID      string       `json:"id"`
	Gauge   float64      `json:"gauge"`
	Sleeper TrackSleeper `json:"sleeper"`
	Ballast TrackBallast `json:"ballast"`
}

type TrackSleeper struct {
	Pitch  float64 `json:"pitch"`
	Length float64 `json:"length"`
	Width  float64 `json:"width"`
}

type TrackBallast struct {
	HalfWidth float64 `json:"half_width"`
}

// ConstructionRun — run размещения решётки (спека §4).
//
// Run — авторитетный факт о физической решётке, независимый от нарезки на
// RenderElement: смена внутренней сегментации карты не должна переставлять
// шпалы. Пишет run'ы строитель (инструмент), а не человек.
type ConstructionRun struct {
	// Type необязателен в карте: опущен — применяется Construction.DefaultType.
	// Компилятор обязан разрешить умолчание ДО провода.
	ID         string    `json:"id"`
	Type       string    `json:"type,omitempty"`
	Coordinate string    `json:"coordinate"`
	Phase      float64   `json:"phase"`
	Spans      []RunSpan `json:"spans"`
}

// RunSpan — участок run'а на одном элементе. Координата размещения — только u:
// клиент не имеет цепочки вертикального профиля и восстановить размещение по s
// не способен (спека §4). Спаны внутри run — в авторском порядке прохождения.
type RunSpan struct {
	Element   string  `json:"element"`
	From      float64 `json:"from"`
	To        float64 `json:"to"`
	Direction string  `json:"direction"` // "forward" | "reverse"
}
