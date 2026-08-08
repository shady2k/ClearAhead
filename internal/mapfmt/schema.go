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
	ID    string       `json:"id"`
	Hand  string       `json:"hand"`
	Frog  string       `json:"frog,omitempty"`
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
	ID   string         `json:"id"`
	Kind string         `json:"kind"`
	Span []SpanInterval `json:"span"`
	Side string         `json:"side,omitempty"`
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
