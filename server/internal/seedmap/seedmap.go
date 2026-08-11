// Package seedmap — фабрика карт: затравка для базы и фикстуры для тестов.
//
// # Зачем
//
// Тесты не читают боевую карту и не хранят JSON-фикстур. Карта строится кодом,
// и это даёт три вещи, которых файловые фикстуры не давали:
//
//  1. тест не ломается от правки боевой карты и не правит её ради себя;
//  2. намерение видно в вызове: Station(WithBridge(...)) читается, а diff
//     JSON-файла на двести строк — нет;
//  3. «сломанную» карту нельзя получить случайно: фабрика порождает валидную,
//     а порча делается точечной опцией, и в тесте написано, что именно
//     сломано.
//
// # Инвариант фабрики
//
// Все конструкторы БЕЗ опций порождают карту, проходящую mapfmt.Validate
// целиком. Это проверяется тестом самой фабрики: фикстура, которая перестала
// быть валидной, обесценивает каждый тест, который её берёт, и обнаруживаться
// это должно здесь, а не в чужом падении.
//
// Числа станции взяты согласованными: угол крестовины 1/9 — arctan(1/9) с
// округлением до 0,1107 рад, как в примере спеки формата §11; прямая вставка и
// радиусы подобраны так, чтобы замыкание сходилось в допуске.
package seedmap

import (
	"math"

	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/netloc"
)

// Option правит карту после сборки.
type Option func(*mapfmt.Map)

// edge — ребро фабрики. Единственное место, где фикстуры называют вид пути:
// фабрика порождает рельсовую сеть, и когда в перечень видов войдёт второй,
// «а какого вида фикстура» будет спрошено здесь, а не в пяти литералах.
func edge(id, from, to string) mapfmt.Edge {
	return mapfmt.Edge{ID: id, Kind: mapfmt.KindRail, From: from, To: to}
}

// WithID задаёт идентификатор карты.
func WithID(id string) Option { return func(m *mapfmt.Map) { m.MapID = id } }

// WithRevision задаёт ревизию.
func WithRevision(n int) Option { return func(m *mapfmt.Map) { m.MapRevision = n } }

// WithoutConstruction убирает рецепт путевой решётки. Карта без него законна:
// это значит, что авторинг ещё не породил решётку.
func WithoutConstruction() Option { return func(m *mapfmt.Map) { m.Construction = nil } }

// WithTerrain добавляет рецепт рельефа.
//
// Октавы записаны от крупной к мелкой — этого требует валидатор, чтобы рецепт
// был канонической записью одного рельефа, а не одной из перестановок.
func WithTerrain() Option {
	return func(m *mapfmt.Map) {
		m.Terrain = &mapfmt.Terrain{
			Seed:  20260811,
			BaseZ: 140,
			Octaves: []mapfmt.TerrainOctave{
				{WavelengthM: 400, AmplitudeM: 18},
				{WavelengthM: 90, AmplitudeM: 3},
			},
			Earthworks: mapfmt.Earthworks{FormationHalfWidth: 5, SideSlope: 1.5},
		}
	}
}

// WithStructure объявляет участок пути несомым сооружением: мостом или
// тоннелем. На его протяжении рельеф с осью не примиряется.
func WithStructure(kind, id, element string, fromM, toM float64) Option {
	return func(m *mapfmt.Map) {
		m.Topology.Trackside = append(m.Topology.Trackside, mapfmt.Trackside{
			ID:   id,
			Kind: kind,
			Span: netloc.LinearU{{Element: element, From: fromM, To: toM}},
		})
	}
}

// WithTrackside добавляет произвольный путевой объект.
func WithTrackside(ts mapfmt.Trackside) Option {
	return func(m *mapfmt.Map) { m.Topology.Trackside = append(m.Topology.Trackside, ts) }
}

// Mutate — точка для порчи карты в тестах валидатора. Отдельное имя нужно
// затем, чтобы намерение «здесь карта делается негодной» было видно в вызове.
func Mutate(f func(*mapfmt.Map)) Option { return Option(f) }

func apply(m *mapfmt.Map, opts []Option) *mapfmt.Map {
	for _, o := range opts {
		o(m)
	}
	return m
}

// LineLengthM — длина перегона, порождаемого Line.
const LineLengthM = 200.0

// LineEdgeID — единственный элемент перегона.
const LineEdgeID = "E1"

// Line — минимальная валидная карта: прямой перегон между двумя границами.
//
// Годится всюду, где топология не важна: рельеф, чанки, кодеки, хранилище.
func Line(opts ...Option) *mapfmt.Map {
	m := &mapfmt.Map{
		FormatVersion: mapfmt.FormatVersion,
		MapID:         "LINE",
		MapRevision:   1,
		Anchors:       map[string]mapfmt.Anchor{"NA.P1": {X: 0, Y: 0, Z: 150, Heading: 0}},
		Topology: mapfmt.Topology{
			Nodes: []mapfmt.Node{
				{ID: "NA", Ports: []mapfmt.Port{{ID: "P1", Purpose: "map_boundary"}}},
				{ID: "NB", Ports: []mapfmt.Port{{ID: "P1", Purpose: "map_boundary"}}},
			},
			Edges: []mapfmt.Edge{edge(LineEdgeID, "NA.P1", "NB.P1")},
		},
		Geometry: mapfmt.Geometry{
			Turnouts: map[string]mapfmt.TurnoutGeometry{},
			Edges: map[string]mapfmt.Alignments{
				LineEdgeID: {Horizontal: []mapfmt.HPrim{{Kind: "straight", Length: LineLengthM}}},
			},
		},
		Construction: construction([]mapfmt.ConstructionRun{
			run("RUN_E1", span(LineEdgeID, 0, LineLengthM)),
		}),
	}
	return apply(m, opts)
}

// Идентификаторы станции. Вынесены константами: тесты ссылаются на элементы по
// имени, и опечатка в строковом литерале дала бы отказ не по той причине.
const (
	StationApproach = "E_APPROACH"
	StationMain     = "E_MAIN"
	StationCross    = "E_CROSS"
	StationSiding   = "E_SIDING"
	StationStub     = "E_STUB"
	StationSW1      = "SW1"
	StationSW2      = "SW2"
)

// Station — горловина: подход, две стрелки, главный путь с кривой, боковой
// путь и тупик, плюс платформа.
//
// Годится всюду, где нужна настоящая топология: распространение поз, замыкание,
// компиляция, крестовины, контракт отрисовки.
func Station(opts ...Option) *mapfmt.Map {
	m := &mapfmt.Map{
		FormatVersion: mapfmt.FormatVersion,
		MapID:         "ST_A",
		MapRevision:   2,
		// Отметка 142 — не украшение: рецепт рельефа объявляет базу 140, и путь
		// идёт по насыпи в два метра над ней. Ноль, стоявший здесь после
		// миграции JSON -> код, был потерей числа из st_a.json, а не решением:
		// земляные работы честно рыли вдоль всей оси траншею в 140 м с почти
		// отвесной стеной на границе досягаемости откоса (ClearAhead-27n).
		// Согласованность этой отметки с базой рельефа держится не соглашением,
		// а тестом земляных работ в internal/terrain.
		Anchors: map[string]mapfmt.Anchor{"N_BOUNDARY.P1": {X: 0, Y: 0, Z: 142, Heading: 0}},
		Topology: mapfmt.Topology{
			Nodes: []mapfmt.Node{
				{ID: "N_BOUNDARY", Ports: []mapfmt.Port{{ID: "P1", Purpose: "map_boundary"}}},
				{ID: "N_STOP_MAIN", Ports: []mapfmt.Port{{ID: "P1", Purpose: "buffer_stop"}}},
				{ID: "N_STOP_SIDING", Ports: []mapfmt.Port{{ID: "P1", Purpose: "buffer_stop"}}},
				{ID: "N_STOP_STUB", Ports: []mapfmt.Port{{ID: "P1", Purpose: "buffer_stop"}}},
			},
			Turnouts: []mapfmt.Turnout{
				turnout(StationSW1), turnout(StationSW2),
			},
			Edges: []mapfmt.Edge{
				edge(StationApproach, "N_BOUNDARY.P1", StationSW1+".C"),
				edge(StationMain, StationSW1+".S", "N_STOP_MAIN.P1"),
				edge(StationCross, StationSW1+".D", StationSW2+".C"),
				edge(StationSiding, StationSW2+".S", "N_STOP_SIDING.P1"),
				edge(StationStub, StationSW2+".D", "N_STOP_STUB.P1"),
			},
			Trackside: []mapfmt.Trackside{{
				ID:     "PLAT_MAIN",
				Kind:   "platform",
				Span:   netloc.LinearU{{Element: StationMain, From: 40, To: 100}},
				Side:   "right",
				Offset: 1.75,
				Width:  3,
			}},
		},
		Geometry: mapfmt.Geometry{
			Turnouts: map[string]mapfmt.TurnoutGeometry{
				StationSW1: turnoutGeometry(),
				StationSW2: turnoutGeometry(),
			},
			Edges: map[string]mapfmt.Alignments{
				StationApproach: {Horizontal: []mapfmt.HPrim{{Kind: "straight", Length: 120}}},
				StationMain: {Horizontal: []mapfmt.HPrim{
					{Kind: "straight", Length: 50},
					{Kind: "arc", Radius: 500, Angle: 0.2},
					{Kind: "straight", Length: 80},
				}},
				StationCross:  {Horizontal: []mapfmt.HPrim{{Kind: "straight", Length: 20}}},
				StationSiding: {Horizontal: []mapfmt.HPrim{{Kind: "straight", Length: 60}}},
				StationStub:   {Horizontal: []mapfmt.HPrim{{Kind: "straight", Length: 30}}},
			},
		},
		// Длина главного пути — 50 + 500·0,2 + 80 = 230: дуга задана радиусом и
		// углом, длина выводится, а не записывается второй раз.
		Construction: construction([]mapfmt.ConstructionRun{
			run("RUN_APPROACH_CROSS", span(StationApproach, 0, 120), span(StationCross, 0, 20)),
			run("RUN_MAIN", span(StationMain, 0, 230)),
			run("RUN_SIDING", span(StationSiding, 0, 60)),
			run("RUN_STUB", span(StationStub, 0, 30)),
		}),
	}
	return apply(m, opts)
}

// TrackTypeID — единственный тип решётки, порождаемый фабрикой.
const TrackTypeID = "TRACK_MAIN_1435"

func turnout(id string) mapfmt.Turnout {
	return mapfmt.Turnout{
		ID:    id,
		Kind:  mapfmt.KindRail,
		Hand:  "right",
		Frog:  "1/9",
		Ports: mapfmt.TurnoutPorts{Common: "C", Straight: "S", Diverging: "D"},
	}
}

// turnoutGeometry — геометрия обыкновенного перевода 1/9 вправо.
// Угол −0,1107 рад — arctan(1/9), округлённый как в примере спеки §11.
func turnoutGeometry() mapfmt.TurnoutGeometry {
	return mapfmt.TurnoutGeometry{
		Straight:  mapfmt.Alignments{Horizontal: []mapfmt.HPrim{{Kind: "straight", Length: 33.5}}},
		Diverging: mapfmt.Alignments{Horizontal: []mapfmt.HPrim{{Kind: "arc", Radius: 300, Angle: -0.1107}}},
	}
}

func construction(runs []mapfmt.ConstructionRun) *mapfmt.Construction {
	return &mapfmt.Construction{
		DefaultType: TrackTypeID,
		Types: []mapfmt.TrackType{{
			ID:      TrackTypeID,
			Gauge:   1.435,
			Sleeper: mapfmt.TrackSleeper{Pitch: 0.6, Length: 2.5, Width: 0.28},
			Ballast: mapfmt.TrackBallast{HalfWidth: 1.75},
		}},
		Runs: runs,
	}
}

func run(id string, spans ...netloc.IntervalU) mapfmt.ConstructionRun {
	return mapfmt.ConstructionRun{ID: id, Coordinate: "u", Phase: 0, Spans: spans}
}

// span — направленный интервал. Направление у run'а решётки обязательно: она
// укладывается по ходу, и спан без него недоописан.
func span(element string, fromM, toM float64) netloc.IntervalU {
	return netloc.IntervalU{Element: element, From: fromM, To: toM, Direction: netloc.DirForward}
}

// RingRadiusM — радиус дуг кольца, порождаемого Ring.
const RingRadiusM = 300.0

// Ring — замкнутое кольцо из четырёх четвертей окружности.
//
// Единственная фикстура с ЦИКЛОМ. Ни перегон, ни станция цикла не содержат, а
// без цикла невязке замыкания взяться неоткуда: проверка сходимости на них
// холостая. lastRadius задаёт радиус последней четверти — отклонение от
// RingRadiusM даёт управляемую невязку, и это единственный способ проверить
// допуск с обеих сторон границы.
//
// Порт N1.P1 обслуживает два конца, поэтому якорь обязан назвать элемент,
// внутрь которого смотрит heading.
func Ring(lastRadius float64, opts ...Option) *mapfmt.Map {
	arc := func(radius float64) mapfmt.Alignments {
		return mapfmt.Alignments{Horizontal: []mapfmt.HPrim{
			{Kind: "arc", Radius: radius, Angle: math.Pi / 2},
		}}
	}
	m := &mapfmt.Map{
		FormatVersion: mapfmt.FormatVersion,
		MapID:         "RING",
		MapRevision:   1,
		Anchors:       map[string]mapfmt.Anchor{"N1.P1": {Element: "E1"}},
		Topology: mapfmt.Topology{
			Nodes: []mapfmt.Node{
				{ID: "N1", Ports: []mapfmt.Port{{ID: "P1"}}},
				{ID: "N2", Ports: []mapfmt.Port{{ID: "P1"}}},
				{ID: "N3", Ports: []mapfmt.Port{{ID: "P1"}}},
				{ID: "N4", Ports: []mapfmt.Port{{ID: "P1"}}},
			},
			Turnouts:  []mapfmt.Turnout{},
			Trackside: []mapfmt.Trackside{},
			Edges: []mapfmt.Edge{
				edge("E1", "N1.P1", "N2.P1"),
				edge("E2", "N2.P1", "N3.P1"),
				edge("E3", "N3.P1", "N4.P1"),
				edge("E4", "N4.P1", "N1.P1"),
			},
		},
		Geometry: mapfmt.Geometry{
			Turnouts: map[string]mapfmt.TurnoutGeometry{},
			Edges: map[string]mapfmt.Alignments{
				"E1": arc(RingRadiusM), "E2": arc(RingRadiusM),
				"E3": arc(RingRadiusM), "E4": arc(lastRadius),
			},
		},
	}
	return apply(m, opts)
}

// Идентификаторы перегона из двух рёбер.
const (
	CorridorFirst  = "E1"
	CorridorSecond = "E2"
	// CorridorJoint — обычный порт, где сходятся два ребра.
	CorridorJoint = "N_MID.P1"
)

// Corridor — перегон из ДВУХ рёбер, сходящихся на обычном порту.
//
// Нужен не для разнообразия: стык двух обычных рёбер — отдельная ветка
// переноса позы через порт, и ни перегон из одного ребра, ни станция её не
// задевают (на станции все сходящиеся порты принадлежат стрелкам). Без этой
// фикстуры ветка остаётся непокрытой, а её отсутствие в тестах незаметно.
func Corridor(opts ...Option) *mapfmt.Map {
	const halfM = 100.0
	m := &mapfmt.Map{
		FormatVersion: mapfmt.FormatVersion,
		MapID:         "CORRIDOR",
		MapRevision:   1,
		Anchors:       map[string]mapfmt.Anchor{"NA.P1": {X: 0, Y: 0, Z: 150, Heading: 0}},
		Topology: mapfmt.Topology{
			Nodes: []mapfmt.Node{
				{ID: "NA", Ports: []mapfmt.Port{{ID: "P1", Purpose: "map_boundary"}}},
				{ID: "N_MID", Ports: []mapfmt.Port{{ID: "P1"}}},
				{ID: "NB", Ports: []mapfmt.Port{{ID: "P1", Purpose: "map_boundary"}}},
			},
			Edges: []mapfmt.Edge{
				edge(CorridorFirst, "NA.P1", CorridorJoint),
				edge(CorridorSecond, CorridorJoint, "NB.P1"),
			},
		},
		Geometry: mapfmt.Geometry{
			Turnouts: map[string]mapfmt.TurnoutGeometry{},
			Edges: map[string]mapfmt.Alignments{
				CorridorFirst:  {Horizontal: []mapfmt.HPrim{{Kind: "straight", Length: halfM}}},
				CorridorSecond: {Horizontal: []mapfmt.HPrim{{Kind: "straight", Length: halfM}}},
			},
		},
		Construction: construction([]mapfmt.ConstructionRun{
			run("RUN_CORRIDOR", span(CorridorFirst, 0, halfM), span(CorridorSecond, 0, halfM)),
		}),
	}
	return apply(m, opts)
}

// Идентификаторы заготовки новой карты.
const (
	BlankEdgeID = "E_MAIN"
	BlankWest   = "N_WEST"
	BlankEast   = "N_EAST"
)

// Blank — заготовка НОВОЙ карты, с которой начинает автор.
//
// Отличается от Line не длиной, а смыслом концов: западный объявлен границей
// карты — оттуда придёт перегон, — а восточный тупиковым упором. Пустой карты
// не бывает: валидатор отвергает карту без якорей, а якорь ссылается на
// элемент, поэтому «пусто» выражается заготовкой, а не отсутствием элементов.
func Blank(opts ...Option) *mapfmt.Map {
	const lengthM = 500.0
	m := &mapfmt.Map{
		FormatVersion: mapfmt.FormatVersion,
		MapID:         "NEW",
		MapRevision:   1,
		Anchors:       map[string]mapfmt.Anchor{BlankWest + ".P1": {}},
		Topology: mapfmt.Topology{
			Nodes: []mapfmt.Node{
				{ID: BlankWest, Ports: []mapfmt.Port{{ID: "P1", Purpose: "map_boundary"}}},
				{ID: BlankEast, Ports: []mapfmt.Port{{ID: "P1", Purpose: "buffer_stop"}}},
			},
			Edges: []mapfmt.Edge{edge(BlankEdgeID, BlankWest+".P1", BlankEast+".P1")},
		},
		Geometry: mapfmt.Geometry{
			Turnouts: map[string]mapfmt.TurnoutGeometry{},
			Edges: map[string]mapfmt.Alignments{
				BlankEdgeID: {Horizontal: []mapfmt.HPrim{{Kind: "straight", Length: lengthM}}},
			},
		},
		Construction: construction([]mapfmt.ConstructionRun{
			run("RUN_"+BlankEdgeID, span(BlankEdgeID, 0, lengthM)),
		}),
	}
	return apply(m, opts)
}
