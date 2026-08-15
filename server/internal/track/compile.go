package track

import (
	"fmt"
	"math"
	"sort"

	"github.com/shady2k/ClearAhead/server/internal/geom"
	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/netloc"
	"github.com/shady2k/ClearAhead/server/internal/units"
)

// CompiledElement — линейный элемент после компиляции. Длины в s.
type CompiledElement struct {
	ID string
	// Kind — вид пути (mapfmt.KindRail). Входит в network_model_hash: это факт о
	// самой модели сети, а не украшение провода.
	Kind    string
	From    string
	To      string
	LengthU units.Distance
	LengthS units.Distance
	Prof    Profile
	// HProf — горизонтальное выравнивание: радиусы кривых элемента.
	HProf HProfile
}

// AlignmentAt — что под машиной в точке s: уклон и радиус кривой.
//
// # Почему один вызов, а не два
//
// Потому что оба ответа стоят на ОДНОМ переводе координаты. Физика стоит в s,
// план и профиль записаны в u, и перевод (Profile.SToU) — самая дорогая часть
// вопроса: двоичный поиск против двух проходов по цепочкам. Два раздельных
// метода заставили бы вызывающего либо платить за перевод дважды, либо тащить
// промежуточное u через свой код — то есть знать про две координаты там, где
// ему хватает одной.
//
// Заодно это единственное место, где перевод s → u вообще случается в горячем
// пути, и когда он подорожает или переедет в кэш, править придётся здесь.
//
// # Уклон отдаётся целым, и почему именно в этой шкале
//
// Профиль хранит уклон как float64 dz/du — это авторская запись, пришедшая из
// карты. Физике float в состояние отдавать нельзя (правило проекта, куплено
// потерей эталона контракта), поэтому перевод происходит ЗДЕСЬ, на границе, как
// и всякий другой перевод float → домен.
//
// Шкала — тысячные доли промилле, и она выбрана не произвольно: по ПТР удельное
// сопротивление от уклона ЧИСЛЕННО РАВНО уклону в промилле, а физика хранит
// удельные величины в тысячных Н/кН (physics.SpecificResistance). Совпадение
// шкал делает physics.GradeResistance тождеством — и это тождество проверяется
// тестом там же.
func (e CompiledElement) AlignmentAt(s units.Distance) (milliPermille int64, radius units.Distance, err error) {
	u, err := e.Prof.SToU(s)
	if err != nil {
		return 0, 0, fmt.Errorf("track: элемент %s: %w", e.ID, err)
	}
	_, slope, err := e.Prof.At(u)
	if err != nil {
		return 0, 0, fmt.Errorf("track: элемент %s: уклон: %w", e.ID, err)
	}
	radius, err = e.HProf.At(u)
	if err != nil {
		return 0, 0, fmt.Errorf("track: элемент %s: радиус: %w", e.ID, err)
	}
	return int64(math.Round(slope * 1e6)), radius, nil
}

// Протяжённость сооружения живёт в общем типе netloc: одна форма на файл,
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

// CompiledNetwork — вход физики и безопасности. Координат не содержит.
//
// Назывался CompiledTrack, и это то же имя вида вместо класса, за которое
// переименован ресурс: сюда лягут автомобильные дороги, а различает их поле Kind
// у элемента, а не тип-обёртка. Хеш этой модели зовётся network_model_hash — имя
// хеша обязано называть то, от чего он считается, иначе повисает в воздухе.
//
// Пакет при этом остался track, и это отдельное решение, а не забывчивость:
// переименование пакета — шум во всех импортах ради того же слова, и делать его
// стоит тогда, когда в пакет реально приедут дороги. Цена названа: до того дня
// тип читается как track.CompiledNetwork, где пакет говорит «рельсы», а тип —
// «сеть». CompiledElement и CompiledDevice трогать не пришлось: они и так
// называют класс.
type CompiledNetwork struct {
	MapID      string
	Revision   int
	Elements   map[string]CompiledElement
	Structures map[string]netloc.LinearS
	Devices    map[string]CompiledDevice
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
	// Type — тип путевой конструкции устройства, ВСЕГДА ЯВНЫЙ.
	//
	// Редакция 5 объявила это поле обязательным и назвала цену пропуска; поле не
	// доехало, и цена наступила. Проходы стрелок run'ами не покрываются по
	// правилу (их решётка нерегулярна), а типа у них не было — значит на ветвях
	// стрелки у клиента не было НИ ОДНОГО размера: ни колеи, ни шага шпал, ни
	// полуширины балласта. На кадре это видно: ветви рисовались ниткой.
	//
	// Отдельно вредно было то, что валидатор поле ПРОВЕРЯЛ (construction.go:
	// неразрешимая ссылка — отказ), а компилятор разрешал умолчание для себя,
	// чтобы взять gauge крестовины, и разрешённое значение выбрасывал. Проверка
	// стояла, польза терялась по дороге.
	//
	// Умолчание разрешает компилятор; клиент скрытого умолчания не применяет
	// никогда — то же правило, что у run'а (редакция 6 §6).
	Type string `json:"type"`
}

// RenderElement — элемент с абсолютной стартовой позой: клиент рисует цепочку
// от неё и ничего не пересчитывает.
type RenderElement struct {
	ID string `json:"id"`
	// Name — читаемая метка элемента (карта; решение владельца «UUIDv7
	// везде»): тождеством не является, в ссылках не участвует, но её клиент
	// показывает игроку вместо непроизносимого UUID. Отсутствует у элементов
	// без метки.
	Name string `json:"name,omitempty"`
	// Kind — вид пути: "rail" (mapfmt.KindRail). Ресурс network называет КЛАСС
	// содержимого, а не вид, и автомобильные дороги приедут в этот же ответ —
	// различать их клиенту нужно по полю, а не по адресу. Поле обязательное и
	// без omitempty: пустой вид в проводе означал бы, что клиент вправе
	// додумать его сам.
	Kind  string            `json:"kind"`
	Start PortPose          `json:"start"`
	Prims []RenderPrimitive `json:"primitives"`
	// Profile — цепочка вертикального профиля (редакция 6 §5).
	//
	// До неё в проводе была вертикаль из двух чисел на элемент — start.z и
	// start.slope, — и это называлось «контракт нейтрален к представлению только
	// в плане». Называлось неверно: цепочка ЕСТЬ в карте (mapfmt.VPrim),
	// компилятор её РАЗБИРАЕТ (profile.go), CompiledElement.Prof её НЕСЁТ — и в
	// провод не шло ничего. Это была не нейтральность, а потеря на последнем
	// шаге: данные доходили до компилятора и останавливались.
	//
	// start.slope при этом не стал избыточным: он задаёт уклон ДО первого
	// примитива и служит начальным условием параболы, если первый примитив —
	// vertical_curve. При пустой цепочке он один описывает элемент целиком, и
	// это ровно прежнее поведение — оно не сломано.
	Profile []RenderVPrim `json:"profile"`
	Role    *RenderRole   `json:"role,omitempty"`
}

// RenderVPrim — примитив вертикального профиля. Формы те же, что в карте:
// grade — постоянный уклон, vertical_curve — уклон, меняющийся по u линейно до
// EndSlopePermille, то есть парабола.
//
// Двух форм достаточно потому, что в карте их две. Изобретать третью форму для
// провода значило бы завести перевод между двумя языками профиля и место, где
// они разойдутся.
type RenderVPrim struct {
	Kind             string  `json:"kind"`
	LengthM          float64 `json:"length"`
	SlopePermille    float64 `json:"slope_permille,omitempty"`
	EndSlopePermille float64 `json:"end_slope_permille,omitempty"`
}

// RenderStructure — сооружение на плане (платформа и пр.).
//
// Размеры платформы — offset (от оси пути до ближней кромки) и width (поперёк)
// — едут на самом сооружении (спека §3): платформа — самостоятельное
// сооружение, тип решётки её размеры не определяет. Точечные виды (buffer_stop)
// размеров не несут.
type RenderStructure struct {
	ID string `json:"id"`
	// Name — читаемая метка сооружения: клиент показывает её игроку вместо
	// UUID. Тождеством не является, в ссылках не участвует.
	Name   string  `json:"name,omitempty"`
	Kind   string  `json:"kind"`
	Side   string  `json:"side,omitempty"`
	Offset float64 `json:"offset,omitempty"`
	Width  float64 `json:"width,omitempty"`
	// Height — верх сооружения над ПОВЕРХНОСТЬЮ КАТАНИЯ (редакция 6 §4).
	// У платформы величина нормируемая и проверяется профилем норм: высокая
	// платформа ближе габарита к оси — отказ.
	Height float64 `json:"height,omitempty"`
	// SlabThickness — толщина плиты платформы: платформа видна сбоку, и торец
	// плиты — то, чем она отличается от прямоугольника на земле.
	SlabThickness float64        `json:"slab_thickness,omitempty"`
	Spans         netloc.LinearU `json:"spans"`
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
	Region           string            `json:"region"`
	Revision         int               `json:"revision"`
	Elements         []RenderElement   `json:"elements"`
	Structures       []RenderStructure `json:"structures,omitempty"`
	TrackTypes       []RenderTrackType `json:"track_types"`
	ConstructionRuns []RenderRun       `json:"construction_runs"`
	// TurnoutGrids — решётка устройств: уровень 3 спеки §4. Отдельным списком, а
	// не прогонами, потому что проходы стрелок прогонами НЕ покрываются по самой
	// спеке, и потому что брус лежит поперёк ПРЯМОГО пути — ориентации, которой у
	// прогона нет (разбор — в шапке timbers.go).
	TurnoutGrids       []RenderTurnoutGrid `json:"turnout_grids"`
	Features           []RenderFeature     `json:"features"`
	PlacementAlgorithm string              `json:"placement_algorithm"`
}

// RenderTurnoutGrid — брусья одной стрелки. Все они лежат на осевой линии
// ПРЯМОГО прохода: Element называет её, U отсчитывается по ней, Offset — по её
// левой нормали. Одна опорная линия на всё устройство, а не своя у каждой ветви,
// и это и есть то, ради чего решётка отдана устройству.
type RenderTurnoutGrid struct {
	Owner   string  `json:"owner"`
	Element string  `json:"element"`
	Type    string  `json:"type"`
	Width   float64 `json:"width"`
	Height  float64 `json:"height"`
	// Timbers — в порядке возрастания U.
	Timbers []RenderTimber `json:"timbers"`
}

// RenderTimber — один брус. Длина СВОЯ у каждого: тем перевод и отличается от
// решётки, где длина взята из типа один раз на весь run.
type RenderTimber struct {
	U      float64 `json:"u"`
	Length float64 `json:"length"`
	// Offset — смещение ЦЕНТРА бруса от оси прямого прохода по левой нормали.
	// Нужно потому, что брус несимметричен относительно неё: он растёт в сторону
	// схода бокового пути.
	Offset float64 `json:"offset"`
}

// RenderTrackType — тип путевой конструкции в проводе (редакция 6 §3).
//
// Вертикальный стек отсчитывается ВНИЗ от поверхности катания: датум z — верх
// головки рельса (редакция 6 §2). Клиенту этого достаточно, чтобы построить
// путь телом и не выдумать ни одного числа.
type RenderTrackType struct {
	ID string `json:"id"`
	// Name — читаемая метка типа решётки (карта). Тождеством не является.
	Name    string        `json:"name,omitempty"`
	Gauge   float64       `json:"gauge"`
	Rail    RenderRail    `json:"rail"`
	Sleeper RenderSleeper `json:"sleeper"`
	Ballast RenderBallast `json:"ballast"`
	// FormationToRailTop — от верха основной площадки до поверхности катания.
	//
	// ПРОИЗВОДНОЕ поле: сумма ballast.depth + sleeper.height + rail.height. В
	// карте его нет намеренно (редакция 6 §3.2) — авторское поле рядом со
	// слагаемыми есть второй источник истины. Здесь оно есть потому, что провод
	// производен и разойтись с собой не может, а вот клиент и terrain, каждый
	// складывающий свои три числа, разойдутся округлением.
	FormationToRailTop float64 `json:"formation_to_rail_top"`
}

// RenderRail — рельс. Одно число: высота от подошвы до поверхности катания.
//
// Профиль рельса НЕ заводится (редакция 6 §8): одной высоты для честного рельса
// мало — нужны либо размеры головки, шейки и подошвы, либо идентификатор
// стандартного профиля. Клиент рисует ОБЪЯВЛЕННОЕ упрощение (прямоугольник), и
// разница между «упрощение объявлено» и «клиент выдумал» — вся разница, которую
// контракт защищает. Цена умолчания измерена: у снесённого спайка отсутствие
// ширины головки дало колею 1.335 вместо 1.435 — замер снят, когда мир объявлял
// 1.435, и не пересчитывается под нынешние 1.520 (ClearAhead-3ay): это факт о
// том дне, а промах в 10 см от смены колеи не изменился.
type RenderRail struct {
	Height float64 `json:"height"`
	// HeadWidth — ширина головки поверху. Единственное число профиля, которое
	// заведено, и заведено по условию: gauge задан между внутренними рабочими
	// гранями, и без ширины головки рельс нельзя поставить, не выдумав её.
	HeadWidth float64 `json:"head_width"`
}

type RenderSleeper struct {
	Pitch  float64 `json:"pitch"`
	Length float64 `json:"length"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

type RenderBallast struct {
	// HalfWidth — по верху призмы; нижняя полуширина выводится:
	// half_width + side_slope·(depth + crib_depth).
	HalfWidth float64 `json:"half_width"`
	// Depth — от верха основной площадки до низа шпалы.
	Depth float64 `json:"depth"`
	// CribDepth — засыпка шпального ящика выше низа шпалы. Верх призмы — это
	// низ шпалы плюс CribDepth, и видно именно его, а не постель.
	CribDepth float64 `json:"crib_depth"`
	// SideSlope — заложение откоса: метров по горизонтали на метр по вертикали.
	SideSlope float64 `json:"side_slope"`
}

// RenderRun — run размещения в проводе (спека §4). Type всегда явный:
// умолчание разрешил компилятор, клиент скрытого умолчания не применяет
// никогда. Спаны — в авторском порядке прохождения.
type RenderRun struct {
	ID string `json:"id"`
	// Name — читаемая метка run'а (карта). Тождеством не является.
	Name       string         `json:"name,omitempty"`
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

// renderProfile переводит скомпилированный профиль обратно в формы провода
// (редакция 6 §5).
//
// «Обратно» — точное слово: ProfileFrom свела обе формы карты к одной паре
// уклонов на звено, и здесь пара разворачивается в ту форму, из которой пришла.
// Так сделано затем, чтобы провод говорил на языке карты, а не на внутреннем
// языке компилятора: клиент, читающий контракт, и автор, пишущий карту, обязаны
// видеть одни и те же два вида.
//
// Пустая цепочка в карте даёт ОДНО звено нулевого уклона во всю длину элемента
// (ProfileFrom), и здесь оно превращается в grade 0‰. Это не подстановка
// вместо данных: плоский элемент и есть плоский элемент, а инвариант «сумма
// длин цепочки равна длине элемента» держится всегда — то есть z(u) определена
// всюду, а не почти всюду.
//
// Уклоны уезжают в ПРОМИЛЛЕ, как в карте: компилятор держит их безразмерными
// (dz/du), но менять единицу на границе провода значило бы завести третью
// единицу одного числа.
func renderProfile(p Profile) []RenderVPrim {
	out := make([]RenderVPrim, 0, len(p))
	for _, seg := range p {
		if seg.StartSlope == seg.EndSlope {
			out = append(out, RenderVPrim{
				Kind:          "grade",
				LengthM:       seg.LengthU.Meters(),
				SlopePermille: seg.StartSlope * 1000,
			})
			continue
		}
		out = append(out, RenderVPrim{
			Kind:             "vertical_curve",
			LengthM:          seg.LengthU.Meters(),
			EndSlopePermille: seg.EndSlope * 1000,
		})
	}
	return out
}

// Compile строит оба артефакта из одной карты.
func Compile(m *mapfmt.Map) (*CompiledNetwork, *RenderGeometry, error) {
	_, els, err := Propagate(m)
	if err != nil {
		return nil, nil, err
	}

	ids := make([]string, 0, len(els))
	for id := range els {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	cn := &CompiledNetwork{
		MapID:      m.MapID,
		Revision:   m.MapRevision,
		Elements:   make(map[string]CompiledElement, len(els)),
		Structures: map[string]netloc.LinearS{},
		Devices:    make(map[string]CompiledDevice, len(m.Topology.Turnouts)),
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
	// Читаемые метки элементов: у ребра своя, у прохода стрелки — метка
	// устройства. Идут в провод, чтобы клиент показывал игроку метку, а не
	// непроизносимый UUID.
	names := make(map[string]string, len(m.Topology.Edges)+2*len(m.Topology.Turnouts))
	for _, e := range m.Topology.Edges {
		names[e.ID] = e.Name
	}
	for _, t := range m.Topology.Turnouts {
		for _, ps := range t.Passages() {
			names[ps.ID] = t.Name
		}
	}
	// Тип по умолчанию — единственное, что нужно от блока construction ДО его
	// переноса в провод: им разрешается опущенный тип устройства (редакция 6
	// §6). Карта без блока законна, и тогда строка пуста — это «типа нет ни у
	// кого», а не «клиент подставит своё».
	deviceType := ""
	if m.Construction != nil {
		deviceType = m.Construction.DefaultType
	}

	rg := &RenderGeometry{
		// m.MapID кладётся в region без перевода: карта И ЕСТЬ регион
		// (world-storage §3, см. поле Region).
		Region:             m.MapID,
		Revision:           m.MapRevision,
		TrackTypes:         []RenderTrackType{},
		ConstructionRuns:   []RenderRun{},
		TurnoutGrids:       []RenderTurnoutGrid{},
		Features:           []RenderFeature{},
		PlacementAlgorithm: PlacementAlgorithm,
	}

	for _, id := range ids {
		e := els[id]
		lengthS, err := e.Prof.LengthS()
		if err != nil {
			return nil, nil, fmt.Errorf("track: %s: %w", mapfmt.Labeled(names[id], id), err)
		}
		cn.Elements[id] = CompiledElement{
			ID:      id,
			Kind:    e.Kind,
			From:    e.From,
			To:      e.To,
			LengthU: e.Plan.Length(),
			LengthS: lengthS,
			Prof:    e.Prof,
			HProf:   e.HProf,
		}
		re := RenderElement{
			ID:      id,
			Name:    names[id],
			Kind:    e.Kind,
			Start:   e.Start,
			Prims:   make([]RenderPrimitive, 0, len(e.Plan)),
			Profile: renderProfile(e.Prof),
		}
		if p, ok := passage[id]; ok {
			re.Role = &RenderRole{
				Turnout: p.turnout.ID,
				Branch:  p.branch,
				Hand:    p.turnout.Hand,
				Frog:    p.turnout.Frog,
				Type:    deviceType,
			}
			// Тип устройства разрешается тем же правилом, что у run'а, но
			// разрешить его надо было ДО этого цикла: карта без блока
			// construction законна, и тогда типа нет ни у кого. Пустая строка
			// здесь означает ровно это, а не «клиент подставит своё».
			if t := p.turnout.Type; t != "" {
				re.Role.Type = t
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
		cn.Devices[t.ID] = CompiledDevice{
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
	// Решётка устройств — уровень 3 той же спеки (§4). После крестовин, а не
	// вместо: крестовина отвечает на вопрос «где точка», брусья — «на чём лежит
	// путь», и одно другого не заменяет.
	if err := buildTurnoutGrids(m, els, rg); err != nil {
		return nil, nil, err
	}

	for _, st := range m.Topology.Structures {
		rt := RenderStructure{
			ID:            st.ID,
			Name:          st.Name,
			Kind:          st.Kind,
			Side:          st.Side,
			Offset:        st.Offset,
			Width:         st.Width,
			Height:        st.Height,
			SlabThickness: st.SlabThickness,
			Spans:         make(netloc.LinearU, 0, len(st.Span)),
		}
		spans := make(netloc.LinearS, 0, len(st.Span))
		for _, iv := range st.Span {
			e, ok := els[iv.Element]
			if !ok {
				return nil, nil, fmt.Errorf("track: объект %s ссылается на элемент %s, которого нет", mapfmt.Labeled(st.Name, st.ID), iv.Element)
			}
			// Спан в u уезжает клиенту как есть, из карты: план рисуется в u.
			rt.Spans = append(rt.Spans, iv)
			fromU, err := units.MetersToDistance(iv.From)
			if err != nil {
				return nil, nil, fmt.Errorf("track: объект %s: %w", mapfmt.Labeled(st.Name, st.ID), err)
			}
			toU, err := units.MetersToDistance(iv.To)
			if err != nil {
				return nil, nil, fmt.Errorf("track: объект %s: %w", mapfmt.Labeled(st.Name, st.ID), err)
			}
			fromS, err := e.Prof.UToS(fromU)
			if err != nil {
				return nil, nil, fmt.Errorf("track: объект %s: начало: %w", mapfmt.Labeled(st.Name, st.ID), err)
			}
			toS, err := e.Prof.UToS(toU)
			if err != nil {
				return nil, nil, fmt.Errorf("track: объект %s: конец: %w", mapfmt.Labeled(st.Name, st.ID), err)
			}
			spans = append(spans, netloc.IntervalS{
				Element:   iv.Element,
				From:      fromS,
				To:        toS,
				Direction: iv.Direction,
			})
		}
		rg.Structures = append(rg.Structures, rt)
		cn.Structures[st.ID] = spans
	}
	return cn, rg, nil
}
