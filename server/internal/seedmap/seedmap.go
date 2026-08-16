// Package seedmap — фабрика карт для ТЕСТОВ, заготовка новой карты и источник
// первой выгрузки боевой.
//
// # Чем эта фабрика перестала быть 2026-08-12
//
// Боевой затравкой сервера. Мир засевается КАРТОЙ ИЗ ФАЙЛА (maps/st_a.json,
// ключ -map): карта мира есть данные, и держать её кодом значило, что править
// русло или посёлок может только тот, кто пересобирает сервер. Разбор того, что
// при этом отменено и что осталось, — в шапке mapfmt/decode.go.
//
// Station(WithTerrain()) — та самая карта, из которой файл выгружен
// (cmd/mapexport), и потому она осталась похожей на боевую. Похожей, а не той
// же: расхождение между ними законно, боевая правится в файле.
//
// # Зачем фабрика остаётся
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
	"fmt"
	"math"

	"github.com/shady2k/ClearAhead/server/internal/chunk"

	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/netloc"
)

// Option правит карту после сборки.
type Option func(*mapfmt.Map)

// edge — ребро фабрики. Единственное место, где фикстуры называют вид пути:
// фабрика порождает рельсовую сеть, и когда в перечень видов войдёт второй,
// «а какого вида фикстура» будет спрошено здесь, а не в пяти литералах.
func edge(id, name, from, to string) mapfmt.Edge {
	return mapfmt.Edge{ID: id, Name: name, Kind: mapfmt.KindRail, From: from, To: to}
}

// WithID задаёт идентификатор карты.
func WithID(id string) Option { return func(m *mapfmt.Map) { m.MapID = id } }

// WithRevision задаёт ревизию.
func WithRevision(n int) Option { return func(m *mapfmt.Map) { m.MapRevision = n } }

// WithoutConstruction убирает рецепт путевой решётки. Карта без него законна:
// это значит, что авторинг ещё не породил решётку.
func WithoutConstruction() Option {
	return func(m *mapfmt.Map) {
		// КАТАЛОГ ТИПОВ УСТРОЙСТВ ОСТАЁТСЯ, и это не оговорка. Убирается РЕШЁТКА
		// — типы пути и прогоны, — а проект перевода решёткой не является: он
		// отвечает на вопрос «какая здесь марка», и у стрелки этот вопрос есть
		// независимо от того, авторил ли кто-нибудь шпалы.
		//
		// Карта без решётки со стрелкой без проекта была бы картой, у которой
		// устройство есть, а какое — неизвестно. Это ровно то умолчание, которое
		// запрещено (см. Turnout.TurnoutType).
		if m.Construction == nil || len(m.Topology.Turnouts) == 0 {
			m.Construction = nil
			return
		}
		m.Construction = &mapfmt.Construction{TurnoutTypes: m.Construction.TurnoutTypes}
	}
}

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
			// Охват тот же, что у боевой карты репозитория: фикстура обязана
			// порождать мир той же формы, иначе замеры на ней говорили бы о
			// другом мире. Числа не «по умолчанию» — умолчания у охвата нет
			// вовсе, валидатор отвергает карту без блока extent.
			Extent: mapfmt.Extent{Level0RadiusM: 512, Levels: 5},
			// Домен — обязательное поле рецепта (W1-C): прямоугольник, из
			// которого мир прогревается чанками. Числа — тот же домен, что у
			// боевой карты: выведен из габарита оси плюс reach 8192 и округлён
			// НАРУЖУ до сетки самого грубого уровня (4096 м), разбор — в
			// worldgen (wave W1-C).
			Domain: mapfmt.Domain{MinX: -8192, MinZ: -12288, MaxX: 12288, MaxZ: 12288},
			// и пороги сняты с констант снесённого спайка (разбор §1.2, §1.3) —
			// маска леса с периодом около 312 м, низовой покров около 45 м,
			// порода около 180 м, вырубка 34 м вдоль оси. У спайка это были
			// решения художника; здесь они стали данными о мире, и подбирать их
			// придётся заново, когда появится, по чему подбирать.
			//
			// Парные пороги (2026-08-12): у спайка обе величины — и сомкнутость
			// покрова, и плотность посадки — были функциями с НАСЫЩЕНИЕМ, и
			// верхние их пороги при переносе потеряли. Восстановлены из тех же
			// строк спайка:
			//   ClosedThreshold −0.16 — это VEG_FULL 0.42 в шуме [0, 1],
			//     развёрнутое в [-1, 1]: 0.42·2 − 1;
			//   ForestDenseThreshold 0.22 — это множитель 5 в
			//     clamp((mask − 0.02)·5, 0, 0.96): 0.02 + 1/5.
			//
			// Октавы (2026-08-12) — оттуда же: `fractal_octaves` 3 / 2 / 2 у
			// трёх FastNoiseLite спайка. Одна октава давала не мозаику, а
			// материки: замер по классам показал плешины связными пятнами по
			// 100–150 м при длине волны 45 м.
			Cover: &mapfmt.Cover{
				Seed:                 20260812,
				ForestWavelengthM:    312,
				ForestOctaves:        3,
				ForestThreshold:      0.02,
				ForestDenseThreshold: 0.22,
				SpeciesWavelengthM:   180,
				SpeciesOctaves:       2,
				VegWavelengthM:       45,
				VegOctaves:           2,
				BareThreshold:        -0.48,
				ClosedThreshold:      -0.16,
				ClearHalfWidthM:      34,
			},
		}
	}
}

// Две опции на сооружения — ОБЩАЯ и ЧАСТНАЯ, и разница между ними ровно та же,
// что между двумя объёмами слова «сооружение» (разбор — в шапке mapfmt.Structure):
//
//   - WithStructure кладёт ЛЮБОЕ сооружение целиком, как его написал автор
//     теста. Это класс: платформа с размерами, упор, мост — что угодно;
//   - WithCarryingStructure объявляет участок пути НЕСОМЫМ — мостом или
//     тоннелем. Это подмножество {bridge, tunnel}, то самое «искусственное
//     сооружение», которое единственное меняет поведение рельефа.
//
// Частная опция оставлена отдельной, а не свёрнута в общую, ровно потому, что
// её вызов ЧИТАЕТСЯ как утверждение о рельефе: Line(WithTerrain(),
// WithCarryingStructure("bridge", ...)) говорит «здесь земля природная», а
// WithStructure(mapfmt.Structure{Kind: "bridge", ...}) говорит только «в карте
// лежит запись». Цена — два имени с общим корнем, и различает их причастие
// «несущее»; это дешевле, чем тест про земляные работы, из вызова которого не
// видно, что он про земляные работы.

// WithStructure кладёт в карту готовое сооружение любого вида.
func WithStructure(st mapfmt.Structure) Option {
	return func(m *mapfmt.Map) { m.Topology.Structures = append(m.Topology.Structures, st) }
}

// WithCarryingStructure объявляет участок пути несомым: мостом или тоннелем.
// Переданный id — это МЕТКА сооружения; UUID берётся из фиксированной таблицы
// (фабрика детерминирована). Неизвестная метка — паника: у фабрики один
// потребитель (тесты), и молча выдуманный UUID развёл бы фикстуру с эталоном.
func WithCarryingStructure(kind, id, element string, fromM, toM float64) Option {
	uuid, ok := carryingIDs[id]
	if !ok {
		panic("seedmap: нет UUID для несущего сооружения " + id)
	}
	return WithStructure(mapfmt.Structure{
		ID:   uuid,
		Name: id,
		Kind: kind,
		Span: netloc.LinearU{{Element: element, From: fromM, To: toM}},
	})
}

// carryingIDs — UUID'ы несущих сооружений тестов (метки MOST, TONNEL в name).
var carryingIDs = map[string]string{
	"MOST":   "018bcfe5-683b-7242-8242-00003b424242",
	"TONNEL": "018bcfe5-683c-7242-8242-00003c424242",
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

// Идентификаторы фикстур — UUIDv7 (решение владельца 2026-08-13 «UUIDv7
// везде»): тождество элемента — UUID, читаемая метка — отдельное поле name.
// Константы держат UUID'ы, прежние читаемые строки (E1, N_BOUNDARY, …) живут
// в name соответствующих элементов. Таблица фиксирована: фабрика
// детерминирована, эталоны тестов не зависят от времени и случайности.

// Идентификаторы перегона Line.
const (
	LineEdgeID   = "018bcfe5-6803-7242-8242-000003424242" // метка E1
	LineNodeWest = "018bcfe5-6801-7242-8242-000001424242" // метка NA
	LineNodeEast = "018bcfe5-6802-7242-8242-000002424242" // метка NB
	LineRunID    = "018bcfe5-6804-7242-8242-000004424242" // метка RUN_E1
)

// Line — минимальная валидная карта: прямой перегон между двумя границами.
//
// Годится всюду, где топология не важна: рельеф, чанки, кодеки, хранилище.
func Line(opts ...Option) *mapfmt.Map {
	m := &mapfmt.Map{
		FormatVersion: mapfmt.FormatVersion,
		MapID:         "LINE",
		MapRevision:   1,
		Anchors:       map[string]mapfmt.Anchor{LineNodeWest + ".P1": {X: 0, Y: 0, Z: 150, Heading: 0}},
		Topology: mapfmt.Topology{
			Nodes: []mapfmt.Node{
				{ID: LineNodeWest, Name: "NA", Ports: []mapfmt.Port{{ID: "P1", Purpose: "map_boundary"}}},
				{ID: LineNodeEast, Name: "NB", Ports: []mapfmt.Port{{ID: "P1", Purpose: "map_boundary"}}},
			},
			Edges: []mapfmt.Edge{edge(LineEdgeID, "E1", LineNodeWest+".P1", LineNodeEast+".P1")},
		},
		Geometry: mapfmt.Geometry{
			Turnouts: map[string]mapfmt.TurnoutGeometry{},
			Edges: map[string]mapfmt.Alignments{
				LineEdgeID: {Horizontal: []mapfmt.HPrim{{Kind: "straight", Length: LineLengthM}}},
			},
		},
		Construction: construction([]mapfmt.ConstructionRun{
			run(LineRunID, "RUN_E1", span(LineEdgeID, 0, LineLengthM)),
		}),
	}
	return apply(m, opts)
}

// Идентификаторы станции. Вынесены константами: тесты ссылаются на элементы по
// имени, и опечатка в строковом литерале дала бы отказ не по той причине.
const (
	StationApproach = "018bcfe5-680c-7242-8242-00000c424242" // метка E_APPROACH
	StationMain     = "018bcfe5-680d-7242-8242-00000d424242" // метка E_MAIN
	StationCross    = "018bcfe5-680e-7242-8242-00000e424242" // метка E_CROSS
	StationSiding   = "018bcfe5-680f-7242-8242-00000f424242" // метка E_SIDING
	StationStub     = "018bcfe5-6810-7242-8242-000010424242" // метка E_STUB
	StationSW1      = "018bcfe5-680a-7242-8242-00000a424242" // метка SW1
	StationSW2      = "018bcfe5-680b-7242-8242-00000b424242" // метка SW2

	// Узлы и сооружения станции.
	StationBoundaryNode     = "018bcfe5-6840-7242-8242-000040424242" // метка N_BOUNDARY
	StationStopMainNode     = "018bcfe5-6807-7242-8242-000007424242" // метка N_STOP_MAIN
	StationStopSidingNode   = "018bcfe5-6808-7242-8242-000008424242" // метка N_STOP_SIDING
	StationStopStubNode     = "018bcfe5-6809-7242-8242-000009424242" // метка N_STOP_STUB
	StationPlatformID       = "018bcfe5-6811-7242-8242-000011424242" // метка PLAT_MAIN
	StationBufferMainID     = "018bcfe5-6812-7242-8242-000012424242" // метка BS_MAIN
	StationBufferSidingID   = "018bcfe5-6813-7242-8242-000013424242" // метка BS_SIDING
	StationBufferStubID     = "018bcfe5-6814-7242-8242-000014424242" // метка BS_STUB
	StationRunApproachCross = "018bcfe5-6815-7242-8242-000015424242" // метка RUN_APPROACH_CROSS
	StationRunMain          = "018bcfe5-6816-7242-8242-000016424242" // метка RUN_MAIN
	StationRunSiding        = "018bcfe5-6817-7242-8242-000017424242" // метка RUN_SIDING
	StationRunStub          = "018bcfe5-6818-7242-8242-000018424242" // метка RUN_STUB
	StationRiverID          = "018bcfe5-6819-7242-8242-000019424242" // метка RIV_MAIN
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
		// РЕВИЗИЯ 3 — приезд сечения рельса (2026-08-15, ClearAhead-72p.8).
		//
		// Подняли её не для порядка. Ответ сети отдаётся с Cache-Control:
		// immutable, и честен он ровно потому, что «пара (регион, ревизия)
		// определяет один ответ» (network_test). Положи новое сечение в ту же
		// ревизию — и клиент с прежним кэшем никогда бы его не увидел, а
		// объяснить это было бы нечем: адрес обещал неизменность и не соврал.
		MapRevision: 3,
		// Отметка 142 — не украшение: рецепт рельефа объявляет базу 140, и путь
		// идёт по насыпи в два метра над ней. Ноль, стоявший здесь после
		// миграции JSON -> код, был потерей числа из st_a.json, а не решением:
		// земляные работы честно рыли вдоль всей оси траншею в 140 м с почти
		// отвесной стеной на границе досягаемости откоса (ClearAhead-27n).
		// Согласованность этой отметки с базой рельефа держится не соглашением,
		// а тестом земляных работ в internal/terrain.
		Anchors: map[string]mapfmt.Anchor{StationBoundaryNode + ".P1": {X: 0, Y: 0, Z: 142, Heading: 0}},
		Topology: mapfmt.Topology{
			Nodes: []mapfmt.Node{
				{ID: StationBoundaryNode, Name: "N_BOUNDARY", Ports: []mapfmt.Port{{ID: "P1", Purpose: "map_boundary"}}},
				{ID: StationStopMainNode, Name: "N_STOP_MAIN", Ports: []mapfmt.Port{{ID: "P1", Purpose: "buffer_stop"}}},
				{ID: StationStopSidingNode, Name: "N_STOP_SIDING", Ports: []mapfmt.Port{{ID: "P1", Purpose: "buffer_stop"}}},
				{ID: StationStopStubNode, Name: "N_STOP_STUB", Ports: []mapfmt.Port{{ID: "P1", Purpose: "buffer_stop"}}},
			},
			Turnouts: []mapfmt.Turnout{
				turnout(StationSW1, "SW1", mapfmt.DriveManual),
				turnout(StationSW2, "SW2", mapfmt.DriveElectric),
			},
			Edges: []mapfmt.Edge{
				edge(StationApproach, "E_APPROACH", StationBoundaryNode+".P1", StationSW1+".C"),
				edge(StationMain, "E_MAIN", StationSW1+".S", StationStopMainNode+".P1"),
				edge(StationCross, "E_CROSS", StationSW1+".D", StationSW2+".C"),
				edge(StationSiding, "E_SIDING", StationSW2+".S", StationStopSidingNode+".P1"),
				edge(StationStub, "E_STUB", StationSW2+".D", StationStopStubNode+".P1"),
			},
			Structures: []mapfmt.Structure{
				{
					ID:   StationPlatformID,
					Name: "PLAT_MAIN",
					Kind: "platform",
					Span: netloc.LinearU{{Element: StationMain, From: 40, To: 100}},
					Side: "right",
					// Отступ 1.745 — не округление прежних 1.75, а СЛЕДСТВИЕ:
					// это нормируемое расстояние от оси до кромки НИЗКОЙ
					// платформы (профиль норм 2). Высота 0.2 выбрана не вкусом,
					// а согласием с ним: высокая платформа при таком отступе
					// нарушила бы габарит и была бы отвергнута валидатором.
					// Прежние 1.75 стояли до появления высоты, когда отличить
					// низкую от высокой было нечем.
					Offset:        1.745,
					Width:         3,
					Height:        0.2,
					SlabThickness: 0.35,
				},
				// Упоры. Их не было НИ ОДНОГО до 2026-08-12, и это была дыра
				// затравки, а не контракта: валидатор принимал вид buffer_stop,
				// провод умел его везти, а создавать их было некому. Разбор
				// снесённого спайка назвал это дырой Д8 и заодно объяснил,
				// почему её не замечали: спайк выводил упоры ИЗ ТОПОЛОГИИ сам,
				// то есть рисовал то, чего ему не присылали.
				bufferStop(StationBufferMainID, "BS_MAIN", StationMain, 230),
				bufferStop(StationBufferSidingID, "BS_SIDING", StationSiding, 60),
				bufferStop(StationBufferStubID, "BS_STUB", StationStub, 30),
			},
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
		// Посёлок у станции с южной стороны. Числа и места взяты у снесённого
		// спайка (VILLAGE, 12 точек), и там же записан довод, который стоит
		// сохранить: «путь между посёлком и рекой — это и есть причина, по
		// которой станция здесь стоит». Размеры предварительные.
		Objects: &mapfmt.Objects{Buildings: village(), Rivers: river()},
		Construction: construction([]mapfmt.ConstructionRun{
			run(StationRunApproachCross, "RUN_APPROACH_CROSS", span(StationApproach, 0, 120), span(StationCross, 0, 20)),
			run(StationRunMain, "RUN_MAIN", span(StationMain, 0, 230)),
			run(StationRunSiding, "RUN_SIDING", span(StationSiding, 0, 60)),
			run(StationRunStub, "RUN_STUB", span(StationStub, 0, 30)),
		}),
	}
	return apply(m, opts)
}

// TrackTypeID — единственный тип решётки, порождаемый фабрикой.
//
// КОЛЕЯ 1520, а не 1435, и это решение владельца от 2026-08-12, а не правка
// опечатки. Прежнее число было европейским умолчанием, попавшим в фабрику по
// инерции: всё остальное в проекте русское — километраж и предельные столбики
// из ПТЭ, ВЛ80 в server/assets, ЧМЭ3 в снесённом спайке. Разошлось это молча,
// потому что колея ни с чем не сверялась: путь рисуется по своему же числу и
// выглядит исправным при любом.
//
// Обнаружилось при подгонке ассета: у ВЛ80 насадка колёс замерена по вершинам
// в 1400 мм, то есть машина колеи около 1480 — на пути 1435 её колёса стоят
// мимо рельсов, и в роли машиниста это видно. Бида ClearAhead-3ay.
const TrackTypeID = "018bcfe5-6805-7242-8242-000005424242" // метка TRACK_MAIN_1520

// village — двенадцать домов посёлка.
//
// Габариты выведены из адреса той же функцией, что высота дерева
// (chunk.ForestJitter): это не экономия байт — в карте они всё равно записаны
// явно, — а способ получить правдоподобный разброс, не выдумывая двенадцать
// троек чисел руками и не пряча выдумку за списком.
// buildingIDs — UUID'ы двенадцати домов посёлка (метки BLD_01…BLD_12 в name).
// Таблица фиксирована: фабрика детерминирована, и village() не зависит от
// времени и случайности.
var buildingIDs = []string{
	"018bcfe5-681a-7242-8242-00001a424242",
	"018bcfe5-681b-7242-8242-00001b424242",
	"018bcfe5-681c-7242-8242-00001c424242",
	"018bcfe5-681d-7242-8242-00001d424242",
	"018bcfe5-681e-7242-8242-00001e424242",
	"018bcfe5-681f-7242-8242-00001f424242",
	"018bcfe5-6820-7242-8242-000020424242",
	"018bcfe5-6821-7242-8242-000021424242",
	"018bcfe5-6822-7242-8242-000022424242",
	"018bcfe5-6823-7242-8242-000023424242",
	"018bcfe5-6824-7242-8242-000024424242",
	"018bcfe5-6825-7242-8242-000025424242",
}

func village() []mapfmt.Building {
	spots := [][2]float64{
		{70, -130}, {140, -112}, {206, -140}, {268, -116},
		{96, -196}, {172, -212}, {246, -190}, {318, -152},
		{330, -232}, {20, -178}, {396, -184}, {430, -124},
	}
	out := make([]mapfmt.Building, 0, len(spots))
	for i, p := range spots {
		w, d, h := chunk.ForestJitter(0, 0, i, 4096)
		out = append(out, mapfmt.Building{
			ID:      buildingIDs[i],
			Name:    fmt.Sprintf("BLD_%02d", i+1),
			X:       p[0],
			Y:       p[1],
			Heading: (d - 0.5) * 0.6,
			Width:   16 + w*14,
			Depth:   14 + d*10,
			Height:  7 + h*9,
		})
	}
	return out
}

// Размеры русла. Числа снесённого спайка (RIVER_HALF 30, RIVER_BANK 46) и
// разность двух его отметок: дно RIVER_BED −6.4 при урезе WATER_Y −3.0 даёт
// глубину 3.4 м. Песчаный пояс — SAND_BAND, переведённый из «метров над водой»
// в метры ОТ УРЕЗА: спайк мерил пляж высотой и сам записал, что ошибался —
// поясок вылезал на ровном месте, где воды нет.
const (
	riverHalfWidthM = 30.0
	riverBankM      = 46.0
	riverDepthM     = 3.4
	riverSandBandM  = 12.0
	// Бровка и долина числами спайка не подкреплены: у него их не было — воду
	// в русле держал клэмп низин (LAND_FLOOR), то есть свойство ВСЕГО рельефа,
	// а не реки. Здесь они предварительные и подобраны так, чтобы долина
	// перекрыла замеренный разброс природной земли вдоль меандра (127…157 м):
	// 260 м долины на перепад до 20 м дают склон около 8 %, обычный для речной
	// террасы и заведомо проходимый.
	riverRimM    = 1.2
	riverValleyM = 260.0
)

// river — река севернее станции.
//
// # Почему точками, а не формулой
//
// У спайка ось была функцией: `340 + 78·sin(0.0034x+0.4) + 26·cos(0.0079x−1.2)`.
// Формуле в карте не место — карта описывает ЭТУ реку, а не семейство рек, и
// правка русла должна быть правкой точек, а не подбором коэффициентов. Точки
// ниже ПОРОЖДЕНЫ той формулой: происхождение названо, а результат записан.
//
// Шаг 20 м — из разбора раскладки §3.3, где и посчитана цена: сто точек на два
// километра, порядка 800 байт на регион при 3604 байтах всей сети.
//
// # Почему она не пересекает путь
//
// Довод перенесён из спайка дословно: моста нет, а путь по воде — это не
// стилизация, это баг на картинке. Река идёт севернее станции, и её место —
// та самая причина, по которой станция стоит здесь: путь между посёлком (он
// южнее, y < 0) и рекой.
//
// # Уклон
//
// Урез падает на 3 м за два километра — 1.5 ‰, обычный равнинный уклон. Он не
// украшение: общий урез на всю реку дал бы либо затопленный верх, либо сухой
// низ, и ради этого отметка и лежит в каждой точке оси.
func river() []mapfmt.River {
	const (
		fromX, toX, stepX = -400.0, 1600.0, 20.0
		surfaceAtToX      = 146.0
		fallPerM          = 0.0105
	)
	axis := make([]mapfmt.RiverPoint, 0, int((toX-fromX)/stepX)+1)
	for x := fromX; x <= toX; x += stepX {
		y := 340.0 + 78.0*math.Sin(x*0.0034+0.4) + 26.0*math.Cos(x*0.0079-1.2)
		// Течение НА ЗАПАД: замер природной земли вдоль меандра даёт 157 м на
		// восточном конце и 127 на западном, и река, текущая в гору, была бы
		// видна на кадре сразу.
		axis = append(axis, mapfmt.RiverPoint{
			X: x, Y: y, Z: surfaceAtToX - (toX-x)*fallPerM,
		})
	}
	return []mapfmt.River{{
		ID:         StationRiverID,
		Name:       "RIV_MAIN",
		Axis:       axis,
		HalfWidthM: riverHalfWidthM,
		BankM:      riverBankM,
		DepthM:     riverDepthM,
		RimM:       riverRimM,
		ValleyM:    riverValleyM,
		SandBandM:  riverSandBandM,
	}}
}

// bufferStop — тупиковый упор в конце элемента.
//
// Интервал ТОЧЕЧНЫЙ (from == to): упор стоит на конце, а не занимает
// протяжённость. Валидатор это проверяет и заодно требует, чтобы порт в этом
// конце был объявлен тупиковым: сооружение тупик ПОДТВЕРЖДАЕТ, а объявляет его
// топология (mapfmt.checkBufferStopPort).
//
// Размеры предварительные, происхождение названо: 1.10 и поперечник около
// 1.8 м — константы снесённого спайка (BUFFER_H, gauge/2 + 0.20 на сторону),
// замеренные брифом разбора §1.5. Происхождение не есть норма, и когда источник
// норм появится, числа поменяются вместе с профилем, а не поодиночке.
func bufferStop(id, name, element string, atU float64) mapfmt.Structure {
	return mapfmt.Structure{
		ID:     id,
		Name:   name,
		Kind:   "buffer_stop",
		Span:   netloc.LinearU{{Element: element, From: atU, To: atU}},
		Height: 1.10,
		Width:  1.8,
	}
}

// turnout — стрелка затравки. Механизм ПАРАМЕТРОМ, а не константой: затравка
// повторяет карту ST_A, а там первая стрелка ручная, вторая с электроприводом,
// и ровно на этой паре проверяется, что перевод различает их.
func turnout(id, name, drive string) mapfmt.Turnout {
	return mapfmt.Turnout{
		ID:   id,
		Name: name,
		Kind: mapfmt.KindRail,
		Hand: "right",
		Frog: "1/9",
		// Ссылка на ПРОЕКТ перевода. Умолчания у неё нет: марка — решение автора
		// станции, и подставить её значило бы выбрать за него проект.
		TurnoutType: TurnoutTypeID,
		Drive:       drive,
		Ports:       mapfmt.TurnoutPorts{Common: "C", Straight: "S", Diverging: "D"},
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
		// Вертикальный стек. Числа ПРЕДВАРИТЕЛЬНЫЕ, и происхождение у них
		// честное: константы снесённого спайка, замеренные брифом разбора §1.5
		// (BALLAST_DEPTH 0.30, SLEEPER_H 0.20, CRIB_Y 0.10, откос 1:1,5), а
		// rail.height 0.18 — высота Р65. Происхождение не есть норма: источник
		// не назначен, и когда он появится, числа поменяются вместе с профилем.
		//
		// Сумма важнее слагаемых: 0.30 + 0.20 + 0.18 = 0.68 м — ровно на
		// столько земляные работы клали землю выше должного, пока z считался
		// отметкой земли, а не поверхности катания.
		//
		// ШПАЛА: У ЧИСЕЛ ПОЯВИЛСЯ ИСТОЧНИК, и это меняет их статус, а не только
		// значения. Источник назван владельцем 2026-08-13 (ClearAhead-u3t):
		//
		//   длина 2.75 м — одна на все типы шпал колеи 1520;
		//   сечение железобетонной в подрельсовой части (ГОСТ 33320-2015):
		//     высота 193–230 мм, ширина 250–300 мм;
		//   шаг задаётся ЭПЮРОЙ — числом шпал на километр, между ОСЯМИ:
		//     1840 шт/км (прямые главные) — 543–546 мм,
		//     2000 шт/км (кривые, мосты, тоннели) — 500–502 мм,
		//     1440 шт/км (малодеятельные и подъездные) — до 695 мм.
		//
		// Ширина 0.28 и высота 0.20 НЕ ТРОНУТЫ: они пришли константами спайка,
		// и норма их подтвердила — обе внутри вилки ГОСТ для железобетонной.
		// Так что у нас железобетонная шпала, и теперь это сказано.
		//
		// Шаг 0.6 был ничем: ни одной эпюре он не соответствует. Взято 0.543 —
		// прямые главные. ЭПЮРА ПО КЛАССАМ ПУТИ здесь НЕ РАЗВЕДЕНА: тупик
		// отстоя малодеятелен и просит 1440, но какой путь какого класса —
		// решение автора карты, а не моё (ClearAhead-u3t). Форма для этого уже
		// есть: у run'а есть поле type, и второй тип решётки заводится без
		// правки контракта.
		//
		// Сгущение шпал в стыках (42–44 см между осями) не моделируется вовсе:
		// стыков в контракте нет, нитка рисуется непрерывной.
		//
		// ПЛЕЧО БАЛЛАСТА уменьшилось само, и это надо было заметить: шпала
		// выросла с 2.5 до 2.75, а half_width остался 1.75, значит от торца
		// шпалы до бровки теперь 0.375 м вместо 0.5. Величина нормируемая
		// (порядок 0.25–0.45 в зависимости от класса), в вилку укладывается, и
		// потому число не тронуто — но тронуто оно НЕ БЫЛО СОЗНАТЕЛЬНО, а не по
		// недосмотру.
		Types: []mapfmt.TrackType{{
			ID:      TrackTypeID,
			Name:    "TRACK_MAIN_1520",
			Gauge:   1.520,
			Rail:    railP65(),
			Sleeper: mapfmt.TrackSleeper{Pitch: 0.543, Length: 2.75, Width: 0.28, Height: 0.20},
			Ballast: mapfmt.TrackBallast{
				HalfWidth: 1.75,
				Depth:     0.30,
				CribDepth: 0.10,
				SideSlope: 1.5,
			},
			// ПЕРЕВОДНЫЕ БРУСЬЯ. Числа порядка комплекта под марку 1/9 на Р65:
			// брус сечением 300 × 200 мм, эпюра перевода гуще путевой, самый
			// длинный брус комплекта 5.50 м.
			//
			// НАЗВАНЫ ПРЕДВАРИТЕЛЬНЫМИ, как и профиль норм, и по той же причине:
			// источник не назначен. Эпюра перевода — не одно число, а таблица на
			// марку и тип рельса, и брать её надо из проекта перевода, а не из
			// памяти. Сегодня они дают правдоподобную решётку, и это всё, что о
			// них можно честно сказать. Проверить их — работа автора карты, и
			// форма для этого уже есть: у стрелки своё поле type, второй тип
			// заводится без правки контракта.
			//
			// Длина каждого бруса ЗДЕСЬ НЕ ЗАДАНА и задана быть не может: она
			// считается сервером из расхождения осей (track/timbers.go). В карте
			// лежит только предел, за которым комплект кончается.
			Timber: &mapfmt.TrackTimber{
				Pitch:     0.50,
				LengthMax: 5.50,
				Width:     0.30,
				Height:    0.20,
			},
		}},
		Runs:         runs,
		TurnoutTypes: []mapfmt.TurnoutType{turnoutType()},
	}
}

// TurnoutTypeID — тип устройства затравки.
const TurnoutTypeID = "018bcfe5-6850-7242-8242-000050424242"

// turnoutType — ПРОЕКТ ПЕРЕВОДА затравки.
//
// # Что здесь переехало и откуда
//
// До 2026-08-16 длина остряка, ход и весь крестовинный комплект лежали у ТИПА
// ПУТИ, и долг был записан там же: «длина остряка на самом деле свойство МАРКИ,
// а здесь она свойство ТИПА ПУТИ… снимется, когда появится каталог типов
// устройств». Каталог появился, числа переехали.
//
// # Статус чисел, и он у них разный
//
//   - 0.152 м, 0.046 м, 0.044 м — НОРМЫ ПТЭ: ход остряка и желоба в крестовине и
//     у контррельса. Меняются только вместе с нормой.
//   - 6.500 и 6.515 м — ЭПЮРА проекта 2434 (Р65 1/9): прямой и кривой остряки.
//     Кривой длиннее, потому что мерится по дуге. До переезда здесь стояло одно
//     число 8.30 «проекта 2750», и оно шло на оба остряка сразу.
//   - остальное — ОЦЕНКИ порядка комплекта марки 1/9 на Р65, и норм за ними нет:
//     усовик около двух метров, контррельс четыре с половиной, отгиб четверть
//     метра, раструб 86 мм.
//
// Разница в статусе важнее значений: нормы поменяются с нормой, эпюра — с
// проектом, оценки — с первым же справочником комплекта.
//
// ГЕОМЕТРИИ ПРОХОДОВ ЗДЕСЬ ПОКА НЕТ: её по-прежнему задаёт автор в
// geometry.turnouts, и потому начальный угол остряка β0 и радиусы двух кривых
// эпюры в каталог не положены — их никто бы не прочёл. Приедут вместе с шагом,
// который начнёт строить проходы по типу (ClearAhead-ax7m.6).
// TurnoutTypeForTest отдаёт проект перевода затравки БЕЗ всей карты.
//
// Нужен проверкам устройства (крестовина, привод, брусья): они строят свою
// маленькую фикстуру из двух проходов и целой карты не заводят. Второй копии
// чисел в тестах при этом не появляется — это ровно тот же проект.
func TurnoutTypeForTest() mapfmt.TurnoutType { return turnoutType() }

func turnoutType() mapfmt.TurnoutType {
	return mapfmt.TurnoutType{
		ID:   TurnoutTypeID,
		Name: "Р65 1/9 пр. 2434",
		Frog: "1/9",
		Switch: mapfmt.TurnoutSwitch{
			BladeLengthStraight:  6.500,
			BladeLengthDiverging: 6.515,
			Throw:                0.152,
		},
		FrogSet: mapfmt.TrackFrog{
			Flangeway:      0.046,
			CheckFlangeway: 0.044,
			WingLength:     2.00,
			CastingLength:  0.90,
			CheckLength:    4.50,
			Flare:          0.25,
			FlareGap:       0.086,
		},
	}
}

func run(id, name string, spans ...netloc.IntervalU) mapfmt.ConstructionRun {
	return mapfmt.ConstructionRun{ID: id, Name: name, Coordinate: "u", Phase: 0, Spans: spans}
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
// Идентификаторы кольца.
const (
	RingNodeN1 = "018bcfe5-6826-7242-8242-000026424242" // метка N1
	RingNodeN2 = "018bcfe5-6827-7242-8242-000027424242" // метка N2
	RingNodeN3 = "018bcfe5-6828-7242-8242-000028424242" // метка N3
	RingNodeN4 = "018bcfe5-6829-7242-8242-000029424242" // метка N4
	RingEdge1  = "018bcfe5-682a-7242-8242-00002a424242" // метка E1
	RingEdge2  = "018bcfe5-682b-7242-8242-00002b424242" // метка E2
	RingEdge3  = "018bcfe5-682c-7242-8242-00002c424242" // метка E3
	RingEdge4  = "018bcfe5-682d-7242-8242-00002d424242" // метка E4
	RingRunID  = "018bcfe5-682e-7242-8242-00002e424242" // метка RUN_RING
)

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
		Anchors:       map[string]mapfmt.Anchor{RingNodeN1 + ".P1": {Element: RingEdge1}},
		Topology: mapfmt.Topology{
			Nodes: []mapfmt.Node{
				{ID: RingNodeN1, Name: "N1", Ports: []mapfmt.Port{{ID: "P1"}}},
				{ID: RingNodeN2, Name: "N2", Ports: []mapfmt.Port{{ID: "P1"}}},
				{ID: RingNodeN3, Name: "N3", Ports: []mapfmt.Port{{ID: "P1"}}},
				{ID: RingNodeN4, Name: "N4", Ports: []mapfmt.Port{{ID: "P1"}}},
			},
			Turnouts:   []mapfmt.Turnout{},
			Structures: []mapfmt.Structure{},
			Edges: []mapfmt.Edge{
				edge(RingEdge1, "E1", RingNodeN1+".P1", RingNodeN2+".P1"),
				edge(RingEdge2, "E2", RingNodeN2+".P1", RingNodeN3+".P1"),
				edge(RingEdge3, "E3", RingNodeN3+".P1", RingNodeN4+".P1"),
				edge(RingEdge4, "E4", RingNodeN4+".P1", RingNodeN1+".P1"),
			},
		},
		Geometry: mapfmt.Geometry{
			Turnouts: map[string]mapfmt.TurnoutGeometry{},
			Edges: map[string]mapfmt.Alignments{
				RingEdge1: arc(RingRadiusM), RingEdge2: arc(RingRadiusM),
				RingEdge3: arc(RingRadiusM), RingEdge4: arc(lastRadius),
			},
		},
	}
	return apply(m, opts)
}

// Идентификаторы перегона из двух рёбер.
const (
	CorridorFirst  = "018bcfe5-682f-7242-8242-00002f424242" // метка E1
	CorridorSecond = "018bcfe5-6830-7242-8242-000030424242" // метка E2
	// CorridorJoint — обычный порт, где сходятся два ребра.
	CorridorJoint = "018bcfe5-6833-7242-8242-000033424242" + ".P1" // метка N_MID
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
		Anchors:       map[string]mapfmt.Anchor{"018bcfe5-6831-7242-8242-000031424242" + ".P1": {X: 0, Y: 0, Z: 150, Heading: 0}},
		Topology: mapfmt.Topology{
			Nodes: []mapfmt.Node{
				{ID: "018bcfe5-6831-7242-8242-000031424242", Name: "NA", Ports: []mapfmt.Port{{ID: "P1", Purpose: "map_boundary"}}},
				{ID: "018bcfe5-6833-7242-8242-000033424242", Name: "N_MID", Ports: []mapfmt.Port{{ID: "P1"}}},
				{ID: "018bcfe5-6832-7242-8242-000032424242", Name: "NB", Ports: []mapfmt.Port{{ID: "P1", Purpose: "map_boundary"}}},
			},
			Edges: []mapfmt.Edge{
				edge(CorridorFirst, "E1", "018bcfe5-6831-7242-8242-000031424242"+".P1", CorridorJoint),
				edge(CorridorSecond, "E2", CorridorJoint, "018bcfe5-6832-7242-8242-000032424242"+".P1"),
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
			run("018bcfe5-6834-7242-8242-000034424242", "RUN_CORRIDOR", span(CorridorFirst, 0, halfM), span(CorridorSecond, 0, halfM)),
		}),
	}
	return apply(m, opts)
}

// Идентификаторы заготовки новой карты.
const (
	BlankEdgeID = "018bcfe5-6837-7242-8242-000037424242" // метка E_MAIN
	BlankWest   = "018bcfe5-6835-7242-8242-000035424242" // метка N_WEST
	BlankEast   = "018bcfe5-6836-7242-8242-000036424242" // метка N_EAST
	BlankRunID  = "018bcfe5-6838-7242-8242-000038424242" // метка RUN_E_MAIN
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
			Edges: []mapfmt.Edge{edge(BlankEdgeID, "E_MAIN", BlankWest+".P1", BlankEast+".P1")},
		},
		Geometry: mapfmt.Geometry{
			Turnouts: map[string]mapfmt.TurnoutGeometry{},
			Edges: map[string]mapfmt.Alignments{
				BlankEdgeID: {Horizontal: []mapfmt.HPrim{{Kind: "straight", Length: lengthM}}},
			},
		},
		Construction: construction([]mapfmt.ConstructionRun{
			run(BlankRunID, "RUN_E_MAIN", span(BlankEdgeID, 0, lengthM)),
		}),
	}
	return apply(m, opts)
}

// railP65 — рельс Р65 с сечением.
//
// # Происхождение чисел названо, и это не то же самое, что норма
//
// ГОСТ Р 51685-2013, Р65: высота 180 мм, ширина головки 75, ширина подошвы 150,
// толщина шейки 18, высота головки 45. Остальные пять чисел — РАЗБИВКА этой
// высоты по участкам (переход головка→шейка 15 мм, шейка 90, переход
// шейка→подошва 18, подошва 12) — из ГОСТ не взяты: они выбраны так, чтобы
// сумма дала объявленные 180 мм, и названы здесь оценкой.
//
// # Чего в сечении НЕТ и почему это не молчание, а объявленное упрощение
//
// Скруглений (у Р65 их полтора десятка, от R400 до R1.5), подуклонки 1/20 и
// переменной толщины подошвы. Сечение ПРЯМОЛИНЕЙНОЕ ПО УЧАСТКАМ: двенадцать
// вершин против нескольких сотен у оцифрованного ГОСТа. Это то же решение, что
// у крестовины-галочки, и цена у него та же: вблизи видно, что кромки острые.
//
// Обход — ПРОТИВ ЧАСОВОЙ в осях (x наружу от рабочей грани, y вверх от
// поверхности катания). Порядок здесь не оформление: по нему клиент строит
// нормали, и валидатор считает знак площади именно за этим.
func railP65() mapfmt.TrackRail {
	const (
		headW = 0.075  // головка поверху
		webHW = 0.009  // полутолщина шейки
		footH = 0.075  // полуширина подошвы
		yHead = -0.045 // низ головки
		yWeb1 = -0.060 // верх шейки (после перехода от головки)
		yWeb2 = -0.150 // низ шейки
		yFoot = -0.168 // верх подошвы у кромки
		yBot  = -0.180 // подошва, она же полная высота рельса
	)
	// Ось рельса лежит на половине ширины головки от рабочей грани: гранью
	// рельс касается колеи, а шейка и подошва симметричны относительно оси.
	const axis = headW / 2
	return mapfmt.TrackRail{
		Height:    0.18,
		HeadWidth: headW,
		Section: []mapfmt.SectionPoint{
			{0, 0},                // рабочая грань поверху — начало отсчёта
			{0, yHead},            // внутренняя щека головки
			{axis - webHW, yWeb1}, // переход к шейке, внутренняя сторона
			{axis - webHW, yWeb2}, // шейка, внутренняя сторона
			{axis - footH, yFoot}, // подошва, внутренняя кромка
			{axis - footH, yBot},
			{axis + footH, yBot}, // подошва, наружная кромка
			{axis + footH, yFoot},
			{axis + webHW, yWeb2}, // шейка, наружная сторона
			{axis + webHW, yWeb1},
			{headW, yHead}, // наружная щека головки
			{headW, 0},     // головка поверху, наружный край
		},
	}
}
