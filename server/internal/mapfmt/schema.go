package mapfmt

import "github.com/shady2k/ClearAhead/server/internal/netloc"

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
	// Terrain — РЕЦЕПТ рельефа, а не отсчёты (бида ClearAhead-9k7).
	//
	// Клиент рецепта не видит никогда: сервер разворачивает его в отсчёты,
	// вычитает земляные работы и отдаёт готовые высоты. Двойной реализации
	// шума в Go и на клиенте не нужно, а «клиент ничего не придумывает»
	// остаётся в силе — придумывает сервер, одинаково для всех и один раз.
	//
	// Отсутствие блока означает карту без рельефа: это законно, отсчётов
	// просто нет. Базовую поверхность рисует клиент.
	Terrain *Terrain `json:"terrain,omitempty"`
}

// Terrain — рецепт рельефа карты.
//
// Высота земли — вход ТОЛЬКО рендера; уклон пути — вход физики. Авторитет у
// пути: рельеф согласуется с проектной осью насыпью и выемкой, обратное
// направление запрещено, иначе сила от уклона начнёт зависеть от разрешения
// сетки высот (map-format-design §5).
type Terrain struct {
	// Seed — затравка шума. Одна затравка даёт один и тот же рельеф на любой
	// машине: шум считается целочисленным хешем и арифметикой без
	// трансцендентных функций, а результат квантуется в сантиметры.
	Seed uint64 `json:"seed"`
	// BaseZ — опорная высота региона в метрах. Отсчёты хранятся и едут как
	// целые сантиметры ОТНОСИТЕЛЬНО неё: int16 покрывает ±327 м вокруг базы,
	// чего хватает на чанк 256 м в любых горах.
	BaseZ float64 `json:"base_z"`
	// Octaves — слои шума, от крупного к мелкому. Порядок значим: сумма
	// считается в записанном порядке, и перестановка меняет последний бит.
	Octaves []TerrainOctave `json:"octaves"`
	// Earthworks — как рельеф примиряется с осью пути.
	Earthworks Earthworks `json:"earthworks"`
}

// TerrainOctave — один слой шума: длина волны и размах.
type TerrainOctave struct {
	WavelengthM float64 `json:"wavelength"`
	AmplitudeM  float64 `json:"amplitude"`
}

// Earthworks — земляные работы: основная площадка и откосы.
//
// Модель намеренно грубая и named honestly: площадка постоянной полуширины
// плюс откос постоянного заложения. Реальные насыпи имеют бермы, кюветы и
// переменное заложение по грунтам; всё это — содержимое отдельного слоя, когда
// у него появится потребитель. Здесь важно одно: под путём земля лежит на
// отметке оси, а не там, где её нарисовал шум.
type Earthworks struct {
	// FormationHalfWidth — полуширина основной площадки, метры.
	FormationHalfWidth float64 `json:"formation_half_width"`
	// SideSlope — заложение откоса: сколько метров по горизонтали на метр по
	// вертикали. Значение 1.5 означает откос 1:1,5.
	SideSlope float64 `json:"side_slope"`
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
	ID string `json:"id"`
	// Kind — вид пути устройства, тот же перечень, что у ребра (KindRail).
	// Обязателен по той же причине и, сверх того, потому что проходы стрелки
	// берут вид ОТСЮДА: их порождает компилятор, автор их не пишет и приписать
	// им вид больше негде (см. Map.ElementKinds).
	Kind string `json:"kind"`
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

// Edge — ребро: линейный элемент сети между двумя портами.
type Edge struct {
	ID string `json:"id"`
	// Kind — ВИД пути: чем этот элемент является физически. Обязателен, см.
	// KindRail.
	Kind string `json:"kind"`
	From string `json:"from"`
	To   string `json:"to"`
}

// KindRail — единственный вид элемента сети, который принимается сегодня.
//
// # Почему поле заведено раньше, чем появился второй вид
//
// Ресурс называется network, а не track, потому что он называет КЛАСС
// содержимого: решением владельца автомобильные дороги приедут В ТОТ ЖЕ ответ.
// Отсюда элементу нужен различитель вида, и вопрос только в том, когда его
// заводить.
//
// Возражение «объявленная форма без потребителя окажется неверной»
// (map-format-design §8) сюда НЕ применяется, и это стоит запомнить, потому что
// здесь сталкиваются два правила проекта:
//
//   - §8 — правило про ВОЗМОЖНОСТИ: не строй механизм, которым никто не
//     пользуется. Механизма тут и нет: kind ничего не включает и ни на что не
//     влияет, компилятор его только переносит;
//   - различитель в ПЕРСИСТЕНТНЫХ и контрактных данных подчиняется обратному
//     правилу, уже записанному про пространство имён выше по файлу (id.go) и
//     про шов 6 (ClearAhead-4he): дешевле всего сделать вслепую, дороже всего
//     мигрировать. Пока карт мало и они свои, поле бесплатно; после импорта и
//     пользовательских карт каждая такая карта станет миграцией.
//
// # Поле — это миграция, значение — нет
//
// Отсюда объём: поле обязательное с сегодня, перечень значений — из одного.
// Добавить "road" потом — строка в switch ниже. Добавить САМО ПОЛЕ потом —
// разбирать, что значит его отсутствие в старых картах, и жить с неявным
// умолчанием «без kind значит рельсы», то есть с молчаливым враньём про каждую
// карту, где автор просто забыл поле.
const KindRail = "rail"

type Trackside struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	// Span — протяжённость объекта. Отдельные SpanInterval и RunSpan
	// упразднены 2026-08-11: они различались ровно наличием направления, и
	// различие было случайным (map-content-design §3, бида ClearAhead-xm7).
	// У платформы направления нет, и поле у её интервалов пустое.
	Span   netloc.LinearU `json:"span"`
	Side   string         `json:"side,omitempty"`
	Offset float64        `json:"offset,omitempty"`
	Width  float64        `json:"width,omitempty"`
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
	ID         string  `json:"id"`
	Type       string  `json:"type,omitempty"`
	Coordinate string  `json:"coordinate"`
	Phase      float64 `json:"phase"`
	// Spans — координата размещения только u: клиент не имеет цепочки
	// вертикального профиля и восстановить размещение по s не способен
	// (спека §4). Спаны — в авторском порядке прохождения, направление у
	// каждого ОБЯЗАТЕЛЬНО: run укладывается по ходу, и спан без направления в
	// нём недоописан, а не «без направления».
	Spans netloc.LinearU `json:"spans"`
}
