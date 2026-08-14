// Package terrain разворачивает рецепт рельефа в отсчёты высот.
//
// # Что здесь происходит и в каком порядке
//
//	рецепт (seed + октавы)  ->  природная поверхность
//	                                    |
//	              ось пути (x, y, z) ->  земляные работы
//	                                    |
//	                            рабочая поверхность
//	                                    |
//	                        квантование в целые сантиметры
//
// Земляные работы вычитаются ДО нарезки на чанки: чанку нечего досчитывать, он
// получает готовые отсчёты. Клиент рецепта не видит никогда.
//
// # Авторитет у пути
//
// Уклон пути — вход физики, высота земли — вход только рендера. Рельеф
// согласуется с проектной осью насыпью и выемкой; обратное направление
// запрещено, иначе сила от уклона начала бы зависеть от разрешения сетки высот
// (map-format-design §5).
//
// # Почему это переносимо между машинами
//
// Проект уже обжёгся на этом: эталон контракта падал при смене машины на
// расхождении около 1e-13 в восьми числах при неизменном счётном коде. Отсюда
// два правила, и оба обязательны:
//
//  1. в шуме нет трансцендентных функций — только целочисленный хеш и
//     сложение, вычитание, умножение над float64. Эти операции IEEE-754
//     задаёт побитово; синус и степень — нет;
//  2. результат КВАНТУЕТСЯ в целые сантиметры. Даже если предпоследний бит
//     разойдётся, сантиметр совпадёт. Квантование здесь не экономия места, а
//     условие воспроизводимости.
//
// Сантиметры — та же единица, в которой отсчёты едут на провод (int16
// относительно base_z), поэтому округление происходит один раз, а не дважды.
package terrain

import (
	"fmt"
	"math"

	"github.com/shady2k/ClearAhead/server/internal/chunk"
	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/netloc"
	"github.com/shady2k/ClearAhead/server/internal/track"
	"github.com/shady2k/ClearAhead/server/internal/units"
)

// AxisStepM — шаг выборки оси пути при построении опорных точек земляных
// работ. Пять метров совпадают с шагом сетки чанка уровня 0: делать выборку
// оси мельче сетки бессмысленно, крупнее — значит терять примыкание насыпи к
// пути между отсчётами.
//
// # Почему число экспортировано
//
// Этими же точками набивается lodGrid, а по нему считается DistanceToAxis — то
// самое расстояние, которым выбирается уровень подробности. Значит шаг выборки
// есть ЧИСЛО ПРАВИЛА, а не деталь рельефа: клиент выбирает уровень сам, меряет
// расстояние по своей выборке оси, и разойдись она с нашей — разойдутся и
// уровни. Число уезжает в манифест региона полем axis_step_m (httpapi:
// chunkRule) ОТСЮДА, а не копией в chunk: копия однажды разошлась бы с тем
// шагом, которым ось выбрана на самом деле.
//
// Цена расхождения замерена, а не оценена: клиент, ошибающийся на AxisStepM/2,
// земли не теряет вовсе, а подробность теряет на 0.53 % площади вблизи пути и
// на 0.04 % по всему охвату (worldgen,
// TestClientLevelChoiceToleratesDistanceError).
const AxisStepM = 5.0

// axisPoint — точка оси пути: план и отметка.
type axisPoint struct {
	X, Y, Z float64
}

// Field — рельеф карты: рецепт плюс ось пути, к которой он примирён.
type Field struct {
	recipe mapfmt.Terrain
	axis   []axisPoint
	// reach — расстояние от оси, дальше которого земляные работы заведомо не
	// достают. Считается из максимального перепада и заложения откоса: дальше
	// него можно не искать ближайшую точку оси вовсе.
	reach float64
	// rule — правило подробности ЭТОЙ карты: докуда порождать землю и сколько
	// уровней. Приезжает рецептом (mapfmt.Terrain.Extent), а не берётся из
	// констант пакета chunk, — разбор у типа chunk.Rule.
	rule chunk.Rule
	// grid — мелкий индекс для земляных работ, сторона ячейки reach.
	//
	// Перебор всех точек оси был бы O(отсчёты × точки), и обе величины растут
	// вместе с размером региона. Замер на станции: 6.9 мс на чанк при 1263
	// точках оси. На коридоре края точек оси около 400 тыс., чанков около 61
	// тыс. — это порядка 29 часов в один поток на регион. Индекс здесь не
	// оптимизация, а условие заявленного масштаба.
	grid pointGrid
	// lodGrid — грубый индекс для выбора уровня подробности. Он отдельный,
	// потому что вопросы разные: земляные работы спрашивают «есть ли ось
	// ближе тридцати метров», выбор уровня — «как далеко ось» на дистанции до
	// восьми километров. На мелкой сетке второй запрос обошёл бы сотни тысяч
	// пустых ячеек.
	lodGrid pointGrid
	// cleared — пятна, внутри которых леса нет: вокруг построек. Список, а не
	// индекс, намеренно: построек на регион единицы тысяч, а спрашивают их раз
	// на ячейку покрова, то есть на порядок реже, чем ось.
	cleared []clearedSpot
	// buildings — построенные объекты с земляным эффектом: жёсткая площадка
	// под габаритом и мягкий откос до природной поверхности (sqym.11).
	// Отдельный список от cleared намеренно: cleared — пятно ВЫРУБКИ (покров),
	// buildings — площадка и откос (высоты), и два вопроса к ним разные.
	buildings []buildingField
	// rivers — русла с готовым индексом оси.
	rivers []riverField
	// grading — правки высот (прямой терраморфинг): клетка уровня 0 ->
	// абсолютная отметка в сантиметрах от base_z. ИСХОДНИК, а не проекция:
	// правки не ложатся в базу чанков, переживают пересев и собираются в
	// рабочую поверхность вместе с эффектом оси (см. grading.go).
	grading map[gradeKey]int16
}

// riverField — река, приготовленная к запросам: размеры плюс индекс оси.
type riverField struct {
	halfWidthM, bankM, depthM float64
	rimM, valleyM             float64
	sandBandM                 float64
	grid                      pointGrid
}

// riverStepM — шаг, до которого сгущается ось русла перед индексированием.
//
// Индекс отвечает на «расстояние до ближайшей ТОЧКИ», а нужно «расстояние до
// ЛИНИИ». Разница на дуге равна стреле прогиба, и пять метров держат её ниже
// сантиметра при любом мыслимом радиусе меандра — то есть ниже единицы
// квантования высот. Считать расстояние до отрезков честнее, но это второй
// индекс и второй способ спросить одно и то же; сгущение переиспользует тот,
// что уже есть у оси пути.
//
// Пять метров, а не четыре: это тот же шаг, которым выбирается ось пути
// (AxisStepM), и совпадение здесь не случайно — оба индекса живут в одной
// сетке и должны стоить одинаково.
const riverStepM = 5.0

// clearedSpot — круг вырубки вокруг постройки.
type clearedSpot struct {
	x, y, r float64
}

// buildingClearM — на сколько метров ЗА габарит постройки лес не растёт.
const buildingClearM = 30.0

func riversOf(m *mapfmt.Map) []mapfmt.River {
	if m.Objects == nil {
		return nil
	}
	return m.Objects.Rivers
}

// densifyRiver сгущает ось русла до riverStepM, ЛИНЕЙНО интерполируя и план, и
// отметку уреза.
//
// Отметку — тоже линейно: между двумя точками река течёт равномерно, и всякое
// сглаживание профиля здесь означало бы, что сервер придумывает уклон, которого
// автор не задавал.
func densifyRiver(axis []mapfmt.RiverPoint) []axisPoint {
	if len(axis) == 0 {
		return nil
	}
	out := []axisPoint{{X: axis[0].X, Y: axis[0].Y, Z: axis[0].Z}}
	for i := 1; i < len(axis); i++ {
		a, b := axis[i-1], axis[i]
		dx, dy := b.X-a.X, b.Y-a.Y
		seg := math.Hypot(dx, dy)
		n := int(math.Ceil(seg / riverStepM))
		if n < 1 {
			n = 1
		}
		for k := 1; k <= n; k++ {
			t := float64(k) / float64(n)
			out = append(out, axisPoint{
				X: a.X + dx*t,
				Y: a.Y + dy*t,
				Z: a.Z + (b.Z-a.Z)*t,
			})
		}
	}
	return out
}

// pointGrid — равномерный индекс точек с расширяющимся поиском по кольцам.
type pointGrid struct {
	cell  float64
	cells map[cellKey][]axisPoint
}

func newPointGrid(cell float64, pts []axisPoint) pointGrid {
	g := pointGrid{cell: cell, cells: make(map[cellKey][]axisPoint, len(pts))}
	if cell <= 0 {
		return g
	}
	for _, p := range pts {
		k := g.keyOf(p.X, p.Y)
		g.cells[k] = append(g.cells[k], p)
	}
	return g
}

func (g pointGrid) keyOf(x, y float64) cellKey {
	return cellKey{int(math.Floor(x / g.cell)), int(math.Floor(y / g.cell))}
}

// nearest ищет ближайшую точку, расширяя кольца вокруг запроса.
//
// Поиск останавливается, когда найденное расстояние не превышает радиуса уже
// просмотренной области: всё, что осталось снаружи, заведомо дальше. Без этого
// условия ответ был бы приблизительным — ближайшая точка не обязана лежать в
// том же кольце, что первая найденная.
func (g pointGrid) nearest(x, y, maxDist float64) (dist, z float64, ok bool) {
	if g.cell <= 0 || len(g.cells) == 0 {
		return 0, 0, false
	}
	c := g.keyOf(x, y)
	best := math.Inf(1)
	var bestZ float64
	maxRing := int(math.Ceil(maxDist/g.cell)) + 1

	for r := 0; r <= maxRing; r++ {
		for dx := -r; dx <= r; dx++ {
			for dy := -r; dy <= r; dy++ {
				// Только внешняя рамка кольца: внутренность просмотрена ранее.
				if r > 0 && abs(dx) != r && abs(dy) != r {
					continue
				}
				for _, p := range g.cells[cellKey{c.ix + dx, c.iy + dy}] {
					ddx, ddy := x-p.X, y-p.Y
					if d2 := ddx*ddx + ddy*ddy; d2 < best {
						best, bestZ = d2, p.Z
					}
				}
			}
		}
		// Просмотрены все ячейки в радиусе r: любая точка вне их дальше, чем
		// r*cell от запроса.
		if d := math.Sqrt(best); d <= float64(r)*g.cell {
			if d > maxDist {
				return 0, 0, false
			}
			return d, bestZ, true
		}
	}
	if d := math.Sqrt(best); d <= maxDist {
		return d, bestZ, true
	}
	return 0, 0, false
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// cellKey — координаты ячейки индекса.
type cellKey struct{ ix, iy int }

// RuleOf — правило подробности карты: охват из рецепта рельефа.
//
// ЕДИНСТВЕННОЕ место, где рецепт превращается в правило. Вторая такая же
// строчка в worldgen (а она напрашивалась: Bootstrap знает карту, но не строит
// поля) означала бы два места, знающих, что Levels — это штуки, а MaxLevel — их
// номер, и однажды они разошлись бы на единицу.
//
// Согласованность самих чисел проверяет mapfmt.Validate, и второй проверки
// здесь НЕТ намеренно: путь входа зовёт валидацию до порождения (инвариант
// worldgen), а вторая копия порогов разошлась бы с первой. Отвергается только
// пустое правило — карта, дошедшая сюда мимо валидации.
func RuleOf(m *mapfmt.Map) (chunk.Rule, error) {
	if m.Terrain == nil {
		return chunk.Rule{}, fmt.Errorf("terrain: у карты %s нет рецепта рельефа; охват брать неоткуда", m.MapID)
	}
	e := m.Terrain.Extent
	r := chunk.Rule{Level0RadiusM: e.Level0RadiusM, MaxLevel: e.MaxLevel()}
	if !r.Known() {
		return chunk.Rule{}, fmt.Errorf("terrain: у рецепта карты %s нет охвата (terrain.extent): радиус уровня 0 %v, уровней %d — карта не прошла валидацию",
			m.MapID, e.Level0RadiusM, e.Levels)
	}
	return r, nil
}

// New строит поле из карты и её скомпилированных элементов.
//
// Элементы нужны целиком, а не их длины: земляные работы примиряют поверхность
// с ОТМЕТКОЙ оси, а она берётся из профиля, и с ПОЛОЖЕНИЕМ оси, а оно берётся
// из плана. Карта нужна ради искусственных сооружений — на протяжении моста и
// тоннеля ось землю не тянет.
func New(m *mapfmt.Map, els map[string]track.Element) (*Field, error) {
	recipe := m.Terrain
	if recipe == nil {
		return nil, fmt.Errorf("terrain: рецепта нет; карта без рельефа обрабатывается вызывающим")
	}
	rule, err := RuleOf(m)
	if err != nil {
		return nil, err
	}
	f := &Field{recipe: *recipe, rule: rule}
	carried := carriedSpans(m)

	total := 0.0
	for _, o := range recipe.Octaves {
		total += o.AmplitudeM
	}
	// Самый глубокий мыслимый перепад между осью и природной поверхностью —
	// полный размах шума. Откос от него уходит на total*SideSlope в стороны.
	f.reach = recipe.Earthworks.FormationHalfWidth + total*recipe.Earthworks.SideSlope

	// Постройки вырубают лес вокруг себя. Правило живёт на СЕРВЕРЕ и клиенту не
	// показывается — как и вырубка вдоль пути: это часть рецепта покрова, а не
	// факт, который надо возить. Полуширина взята у спайка (VILLAGE_CLEAR 30 м).
	for _, b := range buildingsOf(m) {
		f.cleared = append(f.cleared, clearedSpot{x: b.X, y: b.Y, r: buildingClearM + math.Max(b.Width, b.Depth)*0.5})
	}

	// Русла: ось сгущается и кладётся в такой же индекс, что у оси пути. Строится
	// ДО первого обращения к NaturalM — иначе рельеф успел бы посчитаться без
	// реки, и берег появился бы только у тех чанков, что породились позже.
	for _, r := range riversOf(m) {
		// Ячейка индекса — по САМОМУ ДАЛЬНЕМУ вопросу, который ему зададут:
		// долина шире песчаного пояса, и сетка, нарезанная по поясу, заставила
		// бы врезку обходить кольца.
		reach := r.HalfWidthM + r.BankM + math.Max(r.ValleyM, r.SandBandM)
		if reach <= 0 {
			continue
		}
		f.rivers = append(f.rivers, riverField{
			halfWidthM: r.HalfWidthM,
			bankM:      r.BankM,
			depthM:     r.DepthM,
			rimM:       r.RimM,
			valleyM:    r.ValleyM,
			sandBandM:  r.SandBandM,
			grid:       newPointGrid(reach, densifyRiver(r.Axis)),
		})
	}

	// Постройки: жёсткая площадка под габаритом и мягкий откос до природной
	// поверхности (sqym.11, building.go). Площадка стоит на отметке природной
	// поверхности в точке привязки, а NaturalM читает русла, поэтому список
	// строится ПОСЛЕ рек — иначе площадка встала бы на рельеф без врезки.
	for _, b := range buildingsOf(m) {
		f.buildings = append(f.buildings, buildingField{
			cx:        b.X,
			cy:        b.Y,
			heading:   b.Heading,
			halfW:     b.Width * 0.5,
			halfD:     b.Depth * 0.5,
			platformZ: f.NaturalM(b.X, b.Y),
		})
	}

	drops := formationDrops(m)
	for _, e := range els {
		pts, err := sampleAxis(e, carried[e.ID], drops[e.ID])
		if err != nil {
			return nil, fmt.Errorf("terrain: элемент %s: %w", e.ID, err)
		}
		f.axis = append(f.axis, pts...)
	}
	// Ни одной точки оси — законное состояние: карта, где весь путь идёт по
	// мостам и в тоннелях, земляных работ не имеет вовсе, и поверхность
	// остаётся природной.
	f.buildGrid()
	return f, nil
}

// NewGraded строит поле с правками высот (прямой терраморфинг).
//
// Правки — ИСХОДНИК рядом с картой, а не часть рецепта: New (без правок)
// остаётся точкой входа для мира без прямого терраморфинга (порождение по
// требованию, прогрев), а мир с правками идёт сюда — оба пути дают байт в байт
// одинаковые чанки там, где правок нет, и это тот же инвариант детерминизма,
// что держит prepare (спека §3).
func NewGraded(m *mapfmt.Map, els map[string]track.Element, g Grading) (*Field, error) {
	f, err := New(m, els)
	if err != nil {
		return nil, err
	}
	if err := f.addGrading(g); err != nil {
		return nil, err
	}
	return f, nil
}

// buildGrid строит оба индекса.
//
// Мелкий — со стороной reach: при ней любая точка, способная повлиять на
// земляные работы, лежит рядом, и поиск не зависит от размера сети. Грубый —
// со стороной радиуса уровня 0: на нём спрашивают удалённость до восьми
// километров, и мелкая сетка заставила бы обойти сотни тысяч пустых ячеек.
func (f *Field) buildGrid() {
	f.grid = newPointGrid(f.reach, f.axis)
	f.lodGrid = newPointGrid(f.rule.Level0RadiusM, f.axis)
}

// Rule — правило подробности этой карты. Отдаётся наружу затем, что порождение
// (worldgen) и запись региона обязаны спрашивать ОДНО правило, а не собирать
// его из рецепта каждый заново.
func (f *Field) Rule() chunk.Rule { return f.rule }

// Domain — прямоугольник домена региона. Отдаётся наружу затем, что порождение
// (worldgen) и запись региона обязаны спрашивать ОДИН домен, а не читать его из
// рецепта каждый заново. Согласованность домена с записью региона держит
// worldgen.sameDomain, как и для правила подробности.
func (f *Field) Domain() mapfmt.Domain { return f.recipe.Domain }

// carriedSpans собирает интервалы, на которых путь НЕСОМ искусственным
// сооружением — мостом или тоннелем, — сгруппированные по элементу.
//
// ЗДЕСЬ И ЖИВЁТ РАЗНИЦА МЕЖДУ ДВУМЯ СМЫСЛАМИ СЛОВА «СООРУЖЕНИЕ». Массив
// m.Topology.Structures — это КЛАСС: платформы, упоры, мосты, тоннели. Земляные
// работы касаются не класса, а подмножества {bridge, tunnel} — того, что путь
// НЕСЁТ. Платформа стоит рядом с путём, землю под ним не отменяет, и попади она
// в отбор — рельеф перестал бы примиряться с осью на длине платформы, то есть
// станция встала бы в природной канаве. Отбор по kind, а не по типу-обёртке,
// ровно потому, что обёртка называет класс (разбор — mapfmt.Structure).
//
// Это первый потребитель общего типа протяжённости за пределами платформ и
// решётки: сооружение занимает интервал, а не точку, и может пересекать
// границы элементов (netloc.LinearU, бида ClearAhead-xm7).
func carriedSpans(m *mapfmt.Map) map[string]netloc.LinearU {
	out := map[string]netloc.LinearU{}
	for _, st := range m.Topology.Structures {
		switch st.Kind {
		case "bridge", "tunnel":
		default:
			continue
		}
		for _, iv := range st.Span {
			out[iv.Element] = append(out[iv.Element], iv)
		}
	}
	return out
}

// sampleAxis выбирает точки оси элемента с шагом AxisStepM.
//
// Выборка идёт по авторской координате u, а не по s: план задан в u, и
// переводить туда-обратно ради равномерности по пространственной длине значило
// бы вносить лишнее округление в то, что и так приблизительно.
//
// Точки на протяжении моста или тоннеля НЕ попадают в выборку: там путь несёт
// сооружение, а земля остаётся природной. Без этого рельеф сравнял бы долину
// под мостом и прокопал траншею над тоннелем — то есть авторитет пути над
// землёй применился бы там, где путь земли не касается.
// dropSpan — участок элемента и высота конструкции пути на нём: сколько метров
// от поверхности катания вниз до верха основной площадки.
type dropSpan struct {
	from, to units.Distance
	drop     float64
}

// formationDrops собирает по элементам высоту конструкции пути.
//
// # Зачем это рельефу
//
// Датум z — ПОВЕРХНОСТЬ КАТАНИЯ (контракт отрисовки, редакция 6, §2), а земля
// лежит не под колесом, а под балластом. До объявления датума WorkedM клал землю
// ровно на отметку оси, то есть трактовал z как землю; на затравке ST_A это
// давало насыпь на 68 см выше должного (0.30 балласт + 0.20 шпала + 0.18 рельс).
// Ошибка не была видна ровно потому, что второй трактовки не существовало.
//
// # Откуда берётся тип в точке
//
// Оттуда же, откуда его берёт провод, и это не совпадение, а требование: два
// разных ответа на вопрос «какой тип пути здесь» развели бы землю и путь.
//
//   - ребро покрыто run'ами, и покрытие ПОЛНОЕ без перекрытий — это проверяет
//     валидатор (construction.go), поэтому здесь можно не разбирать пропуски;
//   - проход стрелки run'ами не покрывается по правилу, и тип берётся с самого
//     устройства (редакция 6 §6).
//
// Карта без блока construction даёт пустую раскладку и нулевую поправку. Это
// честно, а не умолчание: рельсошпальной решётки в такой карте нет вовсе, и
// вычитать нечего.
func formationDrops(m *mapfmt.Map) map[string][]dropSpan {
	c := m.Construction
	if c == nil {
		return nil
	}
	byID := make(map[string]mapfmt.TrackType, len(c.Types))
	for _, t := range c.Types {
		byID[t.ID] = t
	}
	resolve := func(id string) float64 {
		if id == "" {
			id = c.DefaultType
		}
		return byID[id].FormationToRailTop()
	}

	out := map[string][]dropSpan{}
	add := func(element string, from, to units.Distance, drop float64) {
		out[element] = append(out[element], dropSpan{from: from, to: to, drop: drop})
	}
	for _, r := range c.Runs {
		drop := resolve(r.Type)
		for _, iv := range r.Spans {
			from, err := units.MetersToDistance(iv.From)
			if err != nil {
				continue
			}
			to, err := units.MetersToDistance(iv.To)
			if err != nil {
				continue
			}
			// Спан run'а может идти reverse — направление есть смысл укладки, а
			// не порядок концов. Рельефу направление безразлично, поэтому концы
			// нормализуются здесь и только здесь.
			if from > to {
				from, to = to, from
			}
			add(iv.Element, from, to, drop)
		}
	}
	for _, t := range m.Topology.Turnouts {
		drop := resolve(t.Type)
		for _, ps := range t.Passages() {
			// Проход целиком: устройство — единая конструкция, делить её по u
			// нечем и незачем. units.Distance максимума хватает: домен прохода
			// заведомо короче.
			add(ps.ID, 0, units.Distance(1<<62), drop)
		}
	}
	return out
}

// formationDrop возвращает высоту конструкции в точке u.
//
// Ноль при непопадании — тот же честный ноль, что и у карты без construction:
// участок, не покрытый ни run'ом, ни устройством, конструкции не несёт.
func formationDrop(spans []dropSpan, u units.Distance) float64 {
	for _, s := range spans {
		if u >= s.from && u <= s.to {
			return s.drop
		}
	}
	return 0
}

func sampleAxis(e track.Element, carried netloc.LinearU, drops []dropSpan) ([]axisPoint, error) {
	lengthU := e.Prof.LengthU()
	if lengthU <= 0 {
		return nil, fmt.Errorf("нулевая длина")
	}
	step, err := units.MetersToDistance(AxisStepM)
	if err != nil {
		return nil, err
	}

	var out []axisPoint
	for u := units.Distance(0); ; u += step {
		last := false
		if u >= lengthU {
			u, last = lengthU, true
		}
		plan, err := e.Plan.PoseAt(e.Start.Plan, u)
		if err != nil {
			return nil, fmt.Errorf("план на %s: %w", u, err)
		}
		rise, _, err := e.Prof.At(u)
		if err != nil {
			return nil, fmt.Errorf("профиль на %s: %w", u, err)
		}
		if !insideAny(carried, u) {
			// Отметка ЗЕМЛИ, а не пути: z — поверхность катания, и высота
			// конструкции вычитается здесь, в единственном месте, где ось
			// превращается в точку рельефа.
			out = append(out, axisPoint{
				X: plan.X, Y: plan.Y,
				Z: e.Start.Z + rise - formationDrop(drops, u),
			})
		}
		if last {
			break
		}
	}
	return out, nil
}

// insideAny сообщает, что смещение попадает в один из интервалов.
func insideAny(spans netloc.LinearU, u units.Distance) bool {
	for _, iv := range spans {
		from, err := units.MetersToDistance(iv.From)
		if err != nil {
			continue
		}
		to, err := units.MetersToDistance(iv.To)
		if err != nil {
			continue
		}
		if u >= from && u <= to {
			return true
		}
	}
	return false
}

// NaturalM возвращает природную высоту в метрах — рельеф до земляных работ.
//
// Русло реки входит в ПРИРОДНУЮ поверхность, а не накладывается после земляных
// работ, и порядок здесь значим. Река — форма земли, существовавшая до дороги;
// земляные работы примиряют с осью пути ТО, ЧТО ЕСТЬ, включая берег. Обратный
// порядок означал бы, что река размывает готовую насыпь, и мост под собой
// прорезал бы полотно.
func (f *Field) NaturalM(x, y float64) float64 {
	h := f.recipe.BaseZ
	for _, o := range f.recipe.Octaves {
		h += o.AmplitudeM * valueNoise(f.recipe.Seed, x/o.WavelengthM, y/o.WavelengthM)
	}
	return f.carveRiver(x, y, h)
}

// carveRiver врезает русло и его долину в природную поверхность.
//
// Три пояса, считая от оси:
//
//	0 .. half            дно, плоское, на отметке «урез минус глубина»;
//	half .. +bank        берег: от дна вверх до БРОВКИ («урез плюс rim»);
//	+bank .. +valley     долина: от бровки к природной поверхности.
//
// Переходы сглажены тем же многочленом, что сглаживает шум: линейные давали бы
// видимый излом по бровке и по краю долины. У внешнего края долины значение
// равно натуре, поэтому шва с окружающим рельефом нет по построению.
//
// # ВНУТРИ СЛЕДА РЕКИ ФОРМА РЕКИ ПОБЕЖДАЕТ ШУМ, и это названо ценой
//
// Здесь НЕТ math.Min с натурой, хотя первая редакция его имела: «русло не
// насыпает грунт, оно его выносит». Звучит верно и неверно по существу. С
// ним река перестаёт держать воду: там, где шум опустил землю ниже уреза за
// бровкой, вода выходила в поле, и замер это подтвердил — урез упирался в
// потолок поиска на 76 м у трети точек оси.
//
// Поэтому в пределах half+bank+valley поверхность задаётся РЕКОЙ, а не шумом:
// где надо — срезается, где надо — поднимается до бровки. Цена: рецепт рельефа
// внутри этой полосы не действует, и рисунок шума там пропадает. Это ровно тот
// же размен, на который уже пошли земляные работы у пути, — и обоснование то
// же: форма, порождённая объектом, старше формы, порождённой шумом.
//
// Порт из спайка (`_carve_river`) по форме, но не по устройству: у спайка
// отметки дна и уреза были константами на всю карту и долины не было вовсе —
// её заменял клэмп низин (LAND_FLOOR). Здесь отметка берётся у ближайшей точки
// оси, потому что река течёт под уклон.
func (f *Field) carveRiver(x, y, h float64) float64 {
	if len(f.rivers) == 0 {
		return h
	}
	for i := range f.rivers {
		r := &f.rivers[i]
		reach := r.halfWidthM + r.bankM + r.valleyM
		d, surfaceZ, ok := r.grid.nearest(x, y, reach)
		if !ok {
			continue
		}
		bed := surfaceZ - r.depthM
		rim := surfaceZ + r.rimM
		switch {
		case d <= r.halfWidthM:
			return bed
		case d <= r.halfWidthM+r.bankM:
			return bed + (rim-bed)*smoothstep(r.halfWidthM, r.halfWidthM+r.bankM, d)
		default:
			return rim + (h-rim)*smoothstep(r.halfWidthM+r.bankM, reach, d)
		}
	}
	return h
}

// waterEdgeStepM — шаг поиска уреза. Полметра: вдвое мельче единицы, которой
// урез вообще имеет смысл на кадре, и вчетверо мельче шага сетки высот.
const waterEdgeStepM = 0.5

// WaterEdge — на каком расстоянии от оси русла вода встречает берег.
//
// # Почему это ЗАМЕР, а не формула
//
// Первая редакция везла клиенту полуширину глади одним числом на реку —
// «полуширина дна плюс берег». Это неправда, и неправда наглядная: урез там,
// где рабочая поверхность поднимается до отметки воды, а поверхность эта —
// шум плюс врезка плюс земляные работы. На пологом берегу вода заходит дальше,
// на крутом — ближе, и река постоянной ширины выдаёт себя каналом.
//
// Поэтому здесь идут ПО ЛУЧУ и смотрят, где земля вышла из воды. Отдельно
// влево и вправо: берега у реки разные, и симметричная лента — та же
// канава, только скруглённая.
//
// Считать это на клиенте нельзя: ему пришлось бы знать врезку, то есть кусок
// рецепта, который не показывают никогда.
func (f *Field) WaterEdge(x, y, nx, ny, surfaceZ, maxOut float64) float64 {
	last := 0.0
	for d := waterEdgeStepM; d <= maxOut; d += waterEdgeStepM {
		if f.WorkedM(x+nx*d, y+ny*d) >= surfaceZ {
			return last
		}
		last = d
	}
	return maxOut
}

// riverAt — расстояние до ближайшего русла и отметка его уреза.
//
// Радиус поиска шире берега на песчаный пояс: покров спрашивает про пляж,
// который лежит ЗА бровкой, и обрезанный по берегу поиск сообщал бы «реки
// рядом нет» ровно там, где начинается песок.
func (f *Field) riverAt(x, y float64) (dist, surfaceZ float64, r *riverField, ok bool) {
	for i := range f.rivers {
		rv := &f.rivers[i]
		d, z, hit := rv.grid.nearest(x, y, rv.halfWidthM+rv.bankM+rv.sandBandM)
		if hit {
			return d, z, rv, true
		}
	}
	return 0, 0, nil, false
}

// pathEffect — эффект ОДНОЙ оси пути: жёсткая площадка под основной площадкой
// и мягкий откос постоянного заложения до природной поверхности. Отделён от
// композиции нарочно: правка высот (прямой терраморфинг) обязана отличать
// жёсткую площадку (два жёстких габарита — отказ при расхождении) от мягкого
// откоса (жёсткое вправе перерезать мягкое), а без этой развилки сравнение
// сводилось бы к «правка поверх пути» в одном слове.
func (f *Field) pathEffect(natural, x, y float64) (worked float64, onPlatform bool, platformZ float64) {
	d, axisZ, ok := f.nearestAxis(x, y)
	if !ok {
		return natural, false, 0
	}
	half := f.recipe.Earthworks.FormationHalfWidth
	if d <= half {
		return axisZ, true, axisZ
	}
	// Перепад, который откос успевает набрать на расстоянии (d - half).
	drop := (d - half) / f.recipe.Earthworks.SideSlope
	if natural < axisZ {
		// Насыпь: спускаемся от площадки, но не ниже природной земли.
		return math.Max(natural, axisZ-drop), false, 0
	}
	// Выемка: поднимаемся от площадки, но не выше природной земли.
	return math.Min(natural, axisZ+drop), false, 0
}

// effectsAt — композиция земляных эффектов в точке: путь и постройки.
//
// Каждый эффект — чистая функция природной поверхности и своего исходника
// (§3), и собраны они по правилам §6.2:
//
//   - жёсткие площадки (основная площадка пути, площадки построек) в одной
//     точке обязаны совпасть в пределах квантования — две отметки одного
//     места несовместимы, отказ (§4.2), а не «последний победил»;
//   - мягкие области складываются огибающими: две насыпи — верхняя (max),
//     две выемки — нижняя (min);
//   - пересечение насыпи и выемки — отказ, пока не объявлено сооружение.
//
// Возвращает рабочую отметку, признак жёсткой площадки и её отметку. Ошибка —
// отказ композиции; канал без ошибки (WorkedM) ошибку гасит, потому что
// авторитетный канал компиляции — HeightCm.
func (f *Field) effectsAt(natural, x, y float64) (worked float64, onPlatform bool, platformZ float64, err error) {
	var (
		platSet bool
		platZ   float64
		hasFill bool
		fillMax float64
		hasCut  bool
		cutMin  float64
	)
	// addEffect кладёт один эффект в композицию. Жёсткие габариты в одной
	// точке сравниваются в округлённых сантиметрах — ровно в той единице, в
	// которой обе отметки попадут в провод; допуска нет (спека §8.5),
	// расхождение — отказ, а не компромисс (§4.2).
	addEffect := func(w float64, onP bool, pz float64) error {
		if onP {
			if !platSet {
				platSet, platZ = true, pz
				return nil
			}
			pc := math.Round((pz - f.recipe.BaseZ) * 100)
			pc0 := math.Round((platZ - f.recipe.BaseZ) * 100)
			if pc != pc0 {
				return fmt.Errorf(
					"terrain: в (%v, %v) две жёсткие площадки на разных отметках %d и %d см — два несовместимых жёстких габарита (спека §4.2); отказ, а не компромисс",
					x, y, int64(pc), int64(pc0))
			}
			return nil
		}
		switch {
		case w > natural:
			if !hasFill || w > fillMax {
				fillMax, hasFill = w, true
			}
		case w < natural:
			if !hasCut || w < cutMin {
				cutMin, hasCut = w, true
			}
		}
		return nil
	}
	pw, pon, pz := f.pathEffect(natural, x, y)
	if err := addEffect(pw, pon, pz); err != nil {
		// Первая найденная площадка — детерминированный ответ канала без
		// ошибки (WorkedM): путь обрабатывается раньше построек, порядок
		// фиксирован. Мир с конфликтом не компилируется — отказ в HeightCm.
		return platZ, true, platZ, err
	}
	for _, b := range f.buildings {
		bw, bon, bz := f.buildingEffect(natural, x, y, b)
		if err := addEffect(bw, bon, bz); err != nil {
			return platZ, true, platZ, err
		}
	}
	if platSet {
		return platZ, true, platZ, nil
	}
	if hasFill && hasCut {
		// Мир с таким конфликтом не компилируется (отказ в HeightCm), и канал
		// без ошибки отвечает детерминированно: верхняя огибающая.
		return math.Max(fillMax, cutMin), false, 0, fmt.Errorf(
			"terrain: в (%v, %v) насыпь %v м пересекается с выемкой %v м — пересечение насыпи и выемки — отказ (спека §6.2)",
			x, y, fillMax, cutMin)
	}
	if hasFill {
		return fillMax, false, 0, nil
	}
	if hasCut {
		return cutMin, false, 0, nil
	}
	return natural, false, 0, nil
}

// WorkedM возвращает рабочую высоту в метрах — после земляных работ и правок.
//
// Композиция эффектов — в effectsAt: жёсткие площадки пути и построек,
// мягкие откосы по огибающим §6.2. Правка высот (прямой терраморфинг)
// ложится ПОВЕРХ в покрытых клетках: она жёсткий габарит, и мягкий откос её
// не двигает (спека §4.1). Несовместимость правки с жёсткой площадкой (пути
// ИЛИ постройки) WorkedM молча не решает — это отказ, и живёт он в HeightCm,
// умеющем возвращать ошибку.
func (f *Field) WorkedM(x, y float64) float64 {
	natural := f.NaturalM(x, y)
	worked, _, _, _ := f.effectsAt(natural, x, y)
	if h, ok := f.gradingHeightM(x, y); ok {
		return h
	}
	return worked
}

// HeightCm возвращает рабочую высоту как целые сантиметры ОТНОСИТЕЛЬНО base_z.
//
// Это единица провода (int16 в чанке), и округление происходит здесь один раз.
// Переполнение — отказ, а не обрезание: молча приведённая к границе высота
// выглядела бы как плоскогорье ровно на краю диапазона.
func (f *Field) HeightCm(x, y float64) (int16, error) {
	natural := f.NaturalM(x, y)
	worked, onPlatform, platformZ, err := f.effectsAt(natural, x, y)
	if err != nil {
		return 0, err
	}
	if h, ok := f.gradingAt(x, y); ok {
		if onPlatform {
			// Два несовместимых жёстких габарита: правка на отметке h, а
			// жёсткая площадка (пути или постройки) лежит на платформе.
			// Сравнение в округлённых сантиметрах — ровно в той единице, в
			// которой обе отметки попадут в провод; допуска нет (спека
			// §8.5), расхождение — отказ, а не компромисс (§4.2).
			pc := math.Round((platformZ - f.recipe.BaseZ) * 100)
			if pc != float64(h) {
				return 0, fmt.Errorf(
					"terrain: в (%v, %v) правка высот %d см против жёсткой площадки %d см — два несовместимых жёстких габарита (спека §4.2); отказ, а не компромисс",
					x, y, h, int64(pc))
			}
		}
		worked = f.recipe.BaseZ + float64(h)/100
	}
	cm := math.Round((worked - f.recipe.BaseZ) * 100)
	if cm > math.MaxInt16 || cm < math.MinInt16 {
		return 0, fmt.Errorf("terrain: высота в (%v, %v) не помещается в int16 сантиметров относительно base_z: %v см", x, y, cm)
	}
	return int16(cm), nil
}

// nearestAxis ищет ближайшую точку оси и её отметку.
//
// Просматриваются девять ячеек вокруг запроса. Этого достаточно и это точно:
// сторона ячейки равна reach, поэтому любая точка ближе reach лежит внутри
// этой девятки, а всё, что вне её, заведомо дальше и не влияет на результат.
func (f *Field) nearestAxis(x, y float64) (dist, z float64, ok bool) {
	return f.grid.nearest(x, y, f.reach)
}

// DistanceToAxis — расстояние до ближайшей точки оси, без обрезания радиусом
// земляных работ. Нужно для выбора уровня подробности чанка.
//
// Предел поиска — охват мира: дальше него ответ никому не нужен, а стоит он
// кольцами пустых ячеек индекса.
func (f *Field) DistanceToAxis(x, y float64) (float64, bool) {
	return f.DistanceToAxisWithin(x, y, f.rule.ReachM())
}

// DistanceToAxisWithin — то же, но с ЯВНЫМ пределом поиска: false значит «ось
// дальше maxM», а не «оси нет».
//
// Предел приходит аргументом ради одного вопроса, которого нельзя задать
// оракулу с зашитым охватом: «клетка на краю мира — она внутри или снаружи?».
// Ответ на него нужен с запасом в полудиагональ клетки (worldgen.intersectsDisc):
// центр может лежать за охватом, а угол — внутри. Оракул, обрезанный ровно
// охватом, на такой вопрос отвечает «не знаю» и заставляет доказывать
// отдалённость перебором проб — 289 запросов вместо одного.
//
// Цена запроса зависит от предела, а не от того, нашлась ли ось: поиск идёт
// кольцами ячеек шириной Level0RadiusM, и «оси рядом нет» стоит обхода всех
// колец до maxM (замер BenchmarkDistanceToAxis: 0.28 мкс рядом с осью, 15 мкс
// мимо неё). Поэтому предел не задирается «на всякий случай».
func (f *Field) DistanceToAxisWithin(x, y, maxM float64) (float64, bool) {
	d, _, ok := f.lodGrid.nearest(x, y, maxM)
	return d, ok
}

// Bounds — габарит оси пути в плане. Считается по тем же точкам, по которым
// примиряется рельеф, поэтому вершины кривых не теряются.
func (f *Field) Bounds() (minX, minY, maxX, maxY float64, ok bool) {
	minX, minY = math.Inf(1), math.Inf(1)
	maxX, maxY = math.Inf(-1), math.Inf(-1)
	for _, p := range f.axis {
		minX, maxX = math.Min(minX, p.X), math.Max(maxX, p.X)
		minY, maxY = math.Min(minY, p.Y), math.Max(maxY, p.Y)
		ok = true
	}
	return
}

// valueNoise — значение шума в точке, из [-1, 1].
//
// Решётчатый шум с гладкой интерполяцией. Ни синусов, ни степеней: только
// целочисленный хеш и арифметика, которую IEEE-754 задаёт побитово.
func valueNoise(seed uint64, x, y float64) float64 {
	ix, iy := math.Floor(x), math.Floor(y)
	fx, fy := x-ix, y-iy

	// Сглаживание 6t⁵−15t⁴+10t³ — многочлен Перлина: первая и вторая
	// производные на концах нулевые, поэтому стыки ячеек не видны как гребни.
	// Записан по схеме Горнера: только умножения и сложения.
	sx := fx * fx * fx * (fx*(fx*6-15) + 10)
	sy := fy * fy * fy * (fy*(fy*6-15) + 10)

	i, j := int64(ix), int64(iy)
	n00 := latticeValue(seed, i, j)
	n10 := latticeValue(seed, i+1, j)
	n01 := latticeValue(seed, i, j+1)
	n11 := latticeValue(seed, i+1, j+1)

	a := n00 + sx*(n10-n00)
	b := n01 + sx*(n11-n01)
	return a + sy*(b-a)
}

// latticeValue — значение в узле решётки, из [-1, 1].
func latticeValue(seed uint64, i, j int64) float64 {
	h := mix(seed ^ mix(uint64(i)*0x9E3779B97F4A7C15^uint64(j)*0xC2B2AE3D27D4EB4F))
	// Берём 53 старших бита — ровно мантисса float64, поэтому деление точное.
	return float64(h>>11)/float64(uint64(1)<<53)*2 - 1
}

// mix — целочисленное перемешивание (splitmix64). Целые числа переносимы
// побитово по определению языка.
func mix(z uint64) uint64 {
	z += 0x9E3779B97F4A7C15
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	return z ^ (z >> 31)
}

// ChunkHeights разворачивает рельеф в отсчёты одного чанка.
//
// Отсчёты считаются от АБСОЛЮТНЫХ координат в регионе, а не от угла чанка:
// общий узел двух соседних чанков обязан получить бит в бит одно и то же
// значение, и обеспечивается это только тем, что аргумент у него один
// (контракт чанков §3).
func (f *Field) ChunkHeights(a chunk.Address) ([]int16, error) {
	out := make([]int16, chunk.Samples*chunk.Samples)
	for j := range chunk.Samples {
		for i := range chunk.Samples {
			x, z := a.SampleM(i, j)
			cm, err := f.HeightCm(x, z)
			if err != nil {
				return nil, fmt.Errorf("terrain: чанк %s/%d/%d/%d, отсчёт (%d, %d): %w",
					a.Region, a.Level, a.CX, a.CZ, i, j, err)
			}
			out[chunk.Index(i, j)] = cm
		}
	}
	return out, nil
}

// BaseZ — опорная высота рецепта в метрах. Отсчёты чанка отложены от неё.
func (f *Field) BaseZ() float64 { return f.recipe.BaseZ }

// Затухание и сгущение фрактального шума покрова.
//
// Это НЕ подобранные числа, а умолчания FastNoiseLite, которым считал спайк:
// каждая следующая октава вдвое мельче и вдвое слабее. Названы константами, а
// не полями рецепта, потому что крутить их никто не крутил, — разбор в шапке
// mapfmt.Cover.VegOctaves.
const (
	coverGain       = 0.5
	coverLacunarity = 2.0
)

// fractalNoise — сумма октав, нормированная на сумму размахов.
//
// Нормировка обязательна: без неё сумма трёх октав выходит за [-1, 1], а все
// пороги покрова заданы именно в этих границах и валидатором в них же заперты.
//
// Затравка октавы сдвигается фиксированным шагом, а не берётся той же: одна
// затравка на все октавы дала бы совпадающие решётки разного масштаба, то есть
// усиление рисунка в одних и тех же местах — сетку вместо фрактала.
func fractalNoise(seed uint64, x, y float64, octaves int) float64 {
	sum, amp, norm, freq := 0.0, 1.0, 0.0, 1.0
	for o := range octaves {
		sum += amp * valueNoise(seed+uint64(o)*0x9E3779B9, x*freq, y*freq)
		norm += amp
		amp *= coverGain
		freq *= coverLacunarity
	}
	if norm == 0 {
		return 0
	}
	return sum / norm
}

// smoothstep — доля продвижения x от a до b, сглаженная на обоих концах.
//
// Сглаженная, а не линейная, потому что переносится КОНКРЕТНАЯ функция спайка
// (`smoothstep(VEG_BARE, VEG_FULL, …)` в GDScript), а не идея плавного
// перехода: на границе плешины линейная рампа даёт видимый излом там, где
// покров перестаёт редеть.
func smoothstep(a, b, x float64) float64 {
	if b == a {
		return 0
	}
	t := (x - a) / (b - a)
	if t <= 0 {
		return 0
	}
	if t >= 1 {
		return 1
	}
	return t * t * (3 - 2*t)
}

// grade переводит долю [0, 1] в 16 градаций полубайта.
//
// Множитель 15.999, а не 16: доля, равная единице, обязана дать 15, а не 16 —
// шестнадцатое значение в полубайт не помещается и перевернулось бы в ноль,
// то есть полностью сомкнутый покров стал бы голой почвой.
func grade(v float64) int {
	k := int(v * 15.999)
	if k < 0 {
		return 0
	}
	if k > 15 {
		return 15
	}
	return k
}

// forestMaxDensity — доля занятых мест в самом сомкнутом массиве.
//
// Не единица, и это перенос числа спайка (`clamp(…, 0, 0.96)`) вместе с его
// доводом: в сомкнутом массиве всегда есть прогалы, а сплошная посадка по сетке
// 4 м читается не лесом, а частоколом. Число ПРЕДВАРИТЕЛЬНОЕ и живёт здесь, а
// не в рецепте, потому что потребителя у него ровно один — эта функция; в
// рецепт оно переедет, когда лес разных карт начнёт различаться прогалами.
const forestMaxDensity = 0.96

// forestDensity — доля занятых мест в ячейке: от нуля на границе массива до
// forestMaxDensity в его глубине.
//
// # Почему это отдельная функция, а не сомкнутость покрова
//
// До 2026-08-12 плотность посадки бралась из сомкнутости низового покрова, и
// это утверждало, что лес гуще там, где гуще трава. У спайка поля были разные:
// деревья сажались по МАСКЕ ЛЕСА, а низовой покров в посадке не участвовал
// вовсе. Смешение было не только неверным по существу, но и самоусиливающимся:
// починка сомкнутости (см. CoverAt) насытила бы её до полной на двух третях
// площади, и лес встал бы сплошняком.
func (f *Field) forestDensity(x, y float64) float64 {
	c := f.recipe.Cover
	mask := fractalNoise(c.Seed^0xF0E5, x/c.ForestWavelengthM, y/c.ForestWavelengthM, c.ForestOctaves)
	// Рампа ЛИНЕЙНАЯ, в отличие от сомкнутости: у спайка здесь стоял
	// clamp((mask − thr)·5, …), и опушка держится именно ею. Сглаженная рампа
	// сузила бы переход и вернула ту самую стену по границе массива, против
	// которой у спайка заведены одиночки в поле.
	t := (mask - c.ForestThreshold) / (c.ForestDenseThreshold - c.ForestThreshold)
	if t <= 0 {
		return 0
	}
	if t >= 1 {
		return forestMaxDensity
	}
	return t * forestMaxDensity
}

// CoverAt возвращает класс поверхности и сомкнутость низового покрова в точке.
//
// # Почему это считает сервер, а не клиент
//
// По той же причине, по которой сервер считает высоты: рецепт клиенту не
// показывают никогда (mapfmt.Terrain, шапка). Иначе шум пришлось бы
// реализовать дважды — в Go и на клиенте, — и две реализации разошлись бы на
// первой же смене движка, а «клиент ничего не придумывает» перестало бы быть
// правдой в самом заметном слое кадра.
//
// # Порядок правил, и он значим
//
// Правила применяются сверху вниз, и первое сработавшее выигрывает. Порядок —
// это и есть модель, а не деталь реализации:
//
//  1. ПОЛОСА ОТЧУЖДЕНИЯ. Ближе ClearHalfWidth к оси леса нет. Не потому, что
//     так красивее, а потому что её вырубают: ширина полосы — факт о дороге.
//  2. ГОЛАЯ ПОЧВА. Низовой покров ниже порога — плешина. Она бывает и в лесу, и
//     на лугу, поэтому проверяется раньше леса.
//  3. ЛЕС. Маска выше порога — массив; порода берётся второй маской, у которой
//     своя длина волны. Порода — свойство МЕСТА (роща берёзы за ельником), и
//     хешем по ячейке она не выражается: у хеша нет пространственной
//     корреляции (разбор §3.2).
//  4. ЛУГ — всё остальное.
//
// Песок (SurfaceSand) сегодня НЕ ПОРОЖДАЕТСЯ НИКОГДА, и это названо, а не
// умолчано: пояс песка привязан к урезу воды, а воды в контракте ещё нет.
// Класс объявлен заранее потому, что перечень классов — часть провода, и
// добавление значения потом стоит дороже, чем пустая ветка сейчас.
//
// # Сомкнутость — величина, а не её аргумент
//
// Сомкнутость возвращается в 16 градациях и считается НАСЫЩАЮЩЕЙСЯ функцией
// маски: ноль ниже BareThreshold, полная выше ClosedThreshold, плавный переход
// между ними. Под лесом она означает подлесок, на лугу — густоту травы, на
// плешине — ноль.
//
// До 2026-08-12 здесь стояла линейная развёртка самого шума в [0, 15], и это
// была подмена величины её аргументом. Цена подмены замерена на карте ST_A
// (полоса 1000 × 1000 м, шаг 4 м): средняя сомкнутость 0.49 от полной против
// 0.73 по правилу спайка, полная сомкнутость на 1.0 % площади против 64.8 %.
// Клиент смешивает цвет класса с голым грунтом ровно по этой доле — и весь луг
// выходил наполовину грунтовым, то есть коричневым.
func (f *Field) CoverAt(x, y float64) (class, closure int) {
	c := f.recipe.Cover
	if c == nil {
		return chunk.SurfaceMeadow, 0
	}

	veg := fractalNoise(c.Seed^0x7E6E, x/c.VegWavelengthM, y/c.VegWavelengthM, c.VegOctaves)
	closure = grade(smoothstep(c.BareThreshold, c.ClosedThreshold, veg))

	// ПЕСОК У УРЕЗА — первое правило, потому что оно самое сильное: русло и
	// пляж не бывают ни лугом, ни лесом, что бы ни говорили маски.
	//
	// Пояс привязан к РУСЛУ, а не к отметке, и это правка ошибки, которую спайк
	// нашёл у себя сам и записал: проверка песка была чисто по высоте, и на
	// ровном месте вылезал песчаный поясок там, где воды нет вовсе. Песок — там,
	// где река его намыла.
	//
	// Класс SurfaceSand объявлен в проводе с самого начала и до сегодня не
	// порождался НИ РАЗУ — в chunk.go рядом с ним записано, почему: «пояс песка
	// привязан к урезу воды, а воды в контракте ещё нет». Вода появилась.
	dRiver, _, rv, nearRiver := f.riverAt(x, y)
	if nearRiver && dRiver <= rv.halfWidthM+rv.sandBandM {
		return chunk.SurfaceSand, 0
	}

	if veg < c.BareThreshold {
		return chunk.SurfaceBareSoil, 0
	}

	cleared := false
	// Предел поиска — ПОЛУШИРИНА ВЫРУБКИ, а не охват мира. Ответ «ось дальше
	// вырубки» и ответ «ось дальше восьми километров» здесь равносильны, а стоят
	// по-разному: индекс ищет кольцами шириной Level0RadiusM, и предел в охват
	// заставлял обойти до семнадцати колец там, где хватает одного.
	//
	// Цена была не теоретической. Покров спрашивают в каждой из 64 × 64 = 4096
	// клеток чанка, поэтому лишние микросекунды здесь умножаются на четыре
	// тысячи. Замер (BenchmarkChunkLayers, M2 Pro), покров одного чанка уровня 0:
	//
	//	                      было      стало
	//	у оси                 2.78 мс   2.75 мс
	//	в километре           2.65 мс   2.76 мс
	//	в шести километрах   24.97 мс   1.94 мс
	//
	// То есть у оси не изменилось ничего — там ответ и так находился в первом
	// кольце, — а вдали покров стоил вдесятеро дороже высот того же чанка
	// (1.8 мс) и был главной статьёй расхода при порождении по требованию: живой
	// первый запрос чанка в шести километрах от оси стоил 39 мс, стал 4 мс.
	//
	// Содержимое при этом БАЙТ В БАЙТ прежнее, и это проверено, а не выведено:
	// хеши чанков (они же ETag) до и после правки совпали на четырёх адресах
	// уровня 0 от 1.3 до 6.3 км от оси, вместе с покровом и лесом.
	if d, ok := f.DistanceToAxisWithin(x, y, c.ClearHalfWidthM); ok && d <= c.ClearHalfWidthM {
		cleared = true
	}
	for _, sp := range f.cleared {
		dx, dy := x-sp.x, y-sp.y
		if dx*dx+dy*dy <= sp.r*sp.r {
			cleared = true
			break
		}
	}
	// Мокрый берег леса не держит: до бровки русла дерево не встаёт. Это не та
	// же вырубка, что вдоль пути — там лес СВЕЛИ, здесь он не растёт, — но
	// выражается тем же признаком, потому что следствие одно.
	if nearRiver && dRiver <= rv.halfWidthM+rv.bankM {
		cleared = true
	}
	if !cleared {
		forest := fractalNoise(c.Seed^0xF0E5, x/c.ForestWavelengthM, y/c.ForestWavelengthM, c.ForestOctaves)
		if forest >= c.ForestThreshold {
			species := fractalNoise(c.Seed^0x59EC, x/c.SpeciesWavelengthM, y/c.SpeciesWavelengthM, c.SpeciesOctaves)
			if species > 0 {
				return chunk.SurfaceForestBroad, closure
			}
			return chunk.SurfaceForestConifer, closure
		}
	}
	return chunk.SurfaceMeadow, closure
}

// ChunkCover разворачивает покров чанка в блоб.
//
// Ячеек 64 × 64, а не 65 × 65 отсчётов: покров площадной, и значение берётся в
// ЦЕНТРЕ ячейки (chunk.CoverCellCenterM). Разбор — в шапке констант покрова в
// пакете chunk.
func (f *Field) ChunkCover(a chunk.Address) ([]byte, error) {
	if f.recipe.Cover == nil {
		return nil, nil
	}
	out := make([]byte, chunk.CoverBytes)
	for j := 0; j < chunk.CoverCells; j++ {
		for i := 0; i < chunk.CoverCells; i++ {
			x, z := a.CoverCellCenterM(i, j)
			class, closure := f.CoverAt(x, z)
			out[chunk.CoverIndex(i, j)] = chunk.PackCover(class, closure)
		}
	}
	return out, nil
}

// HasCover сообщает, есть ли у карты рецепт покрова.
func (f *Field) HasCover() bool { return f.recipe.Cover != nil }

// ChunkForest разворачивает лес чанка в битовую карту занятости.
//
// # Инвариант, который сервер обязан держать
//
// Бит стоит ТОЛЬКО в ячейке лесного класса. Проверять это на выходе к рендереру
// незачем — отказ живёт на входе в мир: здесь лес и порождается, и здесь же
// единственное место, где он мог бы разойтись с покровом. Клиент, встретивший
// бит в ячейке песка, дерево не рисует, и это отказ, а не подстановка.
//
// # Плотность
//
// Вероятность дерева берётся из МАСКИ ЛЕСА той же ячейки (forestDensity):
// внутри массива лес гуще к середине и редеет к опушке. До 2026-08-12 здесь
// стояла сомкнутость низового покрова — то есть утверждалось, что лес гуще там,
// где гуще трава. Разбор — в шапке forestDensity.
//
// Жребий — та же ForestJitter, что даёт смещение и высоту, но взятая по
// ДРУГОМУ полю: пусти посадку и высоту от одного числа — и высокие деревья
// оказались бы систематически в одном углу ячейки.
//
// Лес существует только на уровне 0 (chunk.ForestLevel): за границей коридора
// деревья рассыпает клиент по покрову, потому что рубить их некому.
func (f *Field) ChunkForest(a chunk.Address, cover []byte) []byte {
	if a.Level != chunk.ForestLevel || len(cover) != chunk.CoverBytes {
		return nil
	}
	out := make([]byte, chunk.ForestBytes)
	for j := 0; j < chunk.CoverCells; j++ {
		for i := 0; i < chunk.CoverCells; i++ {
			class, _ := chunk.UnpackCover(cover[chunk.CoverIndex(i, j)])
			if class != chunk.SurfaceForestConifer && class != chunk.SurfaceForestBroad {
				continue
			}
			// Маска берётся в ЦЕНТРЕ той же ячейки, что дал класс: возьми её в
			// углу — и плотность разошлась бы с классом на полшага, отчего по
			// краю массива стояли бы деревья вероятности нуль.
			x, z := a.CoverCellCenterM(i, j)
			// Жребий по третьему числу: первые два уйдут на смещение, и общий
			// источник посадил бы высокие деревья в один угол ячейки.
			_, _, lot := chunk.ForestJitter(a.CX, a.CZ, i+chunk.CoverCells, j)
			if lot < f.forestDensity(x, z) {
				chunk.SetForestOccupied(out, i, j)
			}
		}
	}
	return out
}
