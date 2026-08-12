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
			// Охват тот же, что у боевой карты репозитория: фикстура обязана
			// порождать мир той же формы, иначе замеры на ней говорили бы о
			// другом мире. Числа не «по умолчанию» — умолчания у охвата нет
			// вовсе, валидатор отвергает карту без блока extent.
			Extent: mapfmt.Extent{Level0RadiusM: 512, Levels: 5},
			// Покров. Числа ПРЕДВАРИТЕЛЬНЫЕ, происхождение названо: длины волн
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

// WithCarryingStructure объявляет участок пути несомым: мостом или тоннелем. На
// его протяжении рельеф с осью не примиряется (terrain.carriedSpans).
func WithCarryingStructure(kind, id, element string, fromM, toM float64) Option {
	return WithStructure(mapfmt.Structure{
		ID:   id,
		Kind: kind,
		Span: netloc.LinearU{{Element: element, From: fromM, To: toM}},
	})
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
			Structures: []mapfmt.Structure{
				{
					ID:   "PLAT_MAIN",
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
				bufferStop("BS_MAIN", StationMain, 230),
				bufferStop("BS_SIDING", StationSiding, 60),
				bufferStop("BS_STUB", StationStub, 30),
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

// village — двенадцать домов посёлка.
//
// Габариты выведены из адреса той же функцией, что высота дерева
// (chunk.ForestJitter): это не экономия байт — в карте они всё равно записаны
// явно, — а способ получить правдоподобный разброс, не выдумывая двенадцать
// троек чисел руками и не пряча выдумку за списком.
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
			ID:      fmt.Sprintf("BLD_%02d", i+1),
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
		ID:         "RIV_MAIN",
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
func bufferStop(id, element string, atU float64) mapfmt.Structure {
	return mapfmt.Structure{
		ID:     id,
		Kind:   "buffer_stop",
		Span:   netloc.LinearU{{Element: element, From: atU, To: atU}},
		Height: 1.10,
		Width:  1.8,
	}
}

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
		// Вертикальный стек. Числа ПРЕДВАРИТЕЛЬНЫЕ, и происхождение у них
		// честное: константы снесённого спайка, замеренные брифом разбора §1.5
		// (BALLAST_DEPTH 0.30, SLEEPER_H 0.20, CRIB_Y 0.10, откос 1:1,5), а
		// rail.height 0.18 — высота Р65. Происхождение не есть норма: источник
		// не назначен, и когда он появится, числа поменяются вместе с профилем.
		//
		// Сумма важнее слагаемых: 0.30 + 0.20 + 0.18 = 0.68 м — ровно на
		// столько земляные работы клали землю выше должного, пока z считался
		// отметкой земли, а не поверхности катания.
		Types: []mapfmt.TrackType{{
			ID:      TrackTypeID,
			Gauge:   1.435,
			Rail:    mapfmt.TrackRail{Height: 0.18, HeadWidth: 0.075},
			Sleeper: mapfmt.TrackSleeper{Pitch: 0.6, Length: 2.5, Width: 0.28, Height: 0.20},
			Ballast: mapfmt.TrackBallast{
				HalfWidth: 1.75,
				Depth:     0.30,
				CribDepth: 0.10,
				SideSlope: 1.5,
			},
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
			Turnouts:   []mapfmt.Turnout{},
			Structures: []mapfmt.Structure{},
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
