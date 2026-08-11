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

// axisStepM — шаг выборки оси пути при построении опорных точек земляных
// работ. Пять метров совпадают с шагом сетки чанка уровня 0: делать выборку
// оси мельче сетки бессмысленно, крупнее — значит терять примыкание насыпи к
// пути между отсчётами.
const axisStepM = 5.0

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
	// grid — равномерный индекс точек оси со стороной ячейки reach.
	//
	// Перебор всех точек оси был бы O(отсчёты × точки), и обе величины растут
	// вместе с размером региона. Замер на станции: 6.9 мс на чанк при 1263
	// точках оси. На коридоре края точек оси около 400 тыс., чанков около 61
	// тыс. — это порядка 29 часов в один поток на регион. Индекс здесь не
	// оптимизация, а условие заявленного масштаба.
	grid map[cellKey][]axisPoint
}

// cellKey — координаты ячейки индекса.
type cellKey struct{ ix, iy int }

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
	f := &Field{recipe: *recipe}
	carried := carriedSpans(m)

	total := 0.0
	for _, o := range recipe.Octaves {
		total += o.AmplitudeM
	}
	// Самый глубокий мыслимый перепад между осью и природной поверхностью —
	// полный размах шума. Откос от него уходит на total*SideSlope в стороны.
	f.reach = recipe.Earthworks.FormationHalfWidth + total*recipe.Earthworks.SideSlope

	for _, e := range els {
		pts, err := sampleAxis(e, carried[e.ID])
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

// buildGrid раскладывает точки оси по ячейкам со стороной reach.
//
// Сторона выбрана равной reach не из удобства: при ней любая точка, способная
// повлиять на запрос, лежит в одной из девяти соседних ячеек, и поиск
// становится постоянным по времени независимо от размера сети.
func (f *Field) buildGrid() {
	f.grid = make(map[cellKey][]axisPoint, len(f.axis))
	if f.reach <= 0 {
		// Земляных работ нет вовсе: индекс пуст, поиск сразу вернёт «далеко».
		return
	}
	for _, p := range f.axis {
		k := f.cellOf(p.X, p.Y)
		f.grid[k] = append(f.grid[k], p)
	}
}

func (f *Field) cellOf(x, y float64) cellKey {
	return cellKey{int(math.Floor(x / f.reach)), int(math.Floor(y / f.reach))}
}

// carriedSpans собирает интервалы, на которых путь НЕСОМ сооружением — мостом
// или тоннелем, — сгруппированные по элементу.
//
// Это первый потребитель общего типа протяжённости за пределами платформ и
// решётки: сооружение занимает интервал, а не точку, и может пересекать
// границы элементов (netloc.LinearU, бида ClearAhead-xm7).
func carriedSpans(m *mapfmt.Map) map[string]netloc.LinearU {
	out := map[string]netloc.LinearU{}
	for _, ts := range m.Topology.Trackside {
		switch ts.Kind {
		case "bridge", "tunnel":
		default:
			continue
		}
		for _, iv := range ts.Span {
			out[iv.Element] = append(out[iv.Element], iv)
		}
	}
	return out
}

// sampleAxis выбирает точки оси элемента с шагом axisStepM.
//
// Выборка идёт по авторской координате u, а не по s: план задан в u, и
// переводить туда-обратно ради равномерности по пространственной длине значило
// бы вносить лишнее округление в то, что и так приблизительно.
//
// Точки на протяжении моста или тоннеля НЕ попадают в выборку: там путь несёт
// сооружение, а земля остаётся природной. Без этого рельеф сравнял бы долину
// под мостом и прокопал траншею над тоннелем — то есть авторитет пути над
// землёй применился бы там, где путь земли не касается.
func sampleAxis(e track.Element, carried netloc.LinearU) ([]axisPoint, error) {
	lengthU := e.Prof.LengthU()
	if lengthU <= 0 {
		return nil, fmt.Errorf("нулевая длина")
	}
	step, err := units.MetersToDistance(axisStepM)
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
			out = append(out, axisPoint{X: plan.X, Y: plan.Y, Z: e.Start.Z + rise})
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
func (f *Field) NaturalM(x, y float64) float64 {
	h := f.recipe.BaseZ
	for _, o := range f.recipe.Octaves {
		h += o.AmplitudeM * valueNoise(f.recipe.Seed, x/o.WavelengthM, y/o.WavelengthM)
	}
	return h
}

// WorkedM возвращает рабочую высоту в метрах — после земляных работ.
//
// Правило: под основной площадкой земля лежит на отметке оси; за её кромкой
// уходит откосом постоянного заложения до природной поверхности. Насыпь
// поднимает землю, выемка опускает, и ни то ни другое не «проваливается»
// сквозь природную поверхность — за это отвечают max и min.
func (f *Field) WorkedM(x, y float64) float64 {
	natural := f.NaturalM(x, y)

	d, axisZ, ok := f.nearestAxis(x, y)
	if !ok {
		return natural
	}
	half := f.recipe.Earthworks.FormationHalfWidth
	if d <= half {
		return axisZ
	}
	// Перепад, который откос успевает набрать на расстоянии (d - half).
	drop := (d - half) / f.recipe.Earthworks.SideSlope
	if natural < axisZ {
		// Насыпь: спускаемся от площадки, но не ниже природной земли.
		return math.Max(natural, axisZ-drop)
	}
	// Выемка: поднимаемся от площадки, но не выше природной земли.
	return math.Min(natural, axisZ+drop)
}

// HeightCm возвращает рабочую высоту как целые сантиметры ОТНОСИТЕЛЬНО base_z.
//
// Это единица провода (int16 в чанке), и округление происходит здесь один раз.
// Переполнение — отказ, а не обрезание: молча приведённая к границе высота
// выглядела бы как плоскогорье ровно на краю диапазона.
func (f *Field) HeightCm(x, y float64) (int16, error) {
	cm := math.Round((f.WorkedM(x, y) - f.recipe.BaseZ) * 100)
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
	if f.reach <= 0 {
		return 0, 0, false
	}
	c := f.cellOf(x, y)
	best := math.Inf(1)
	var bestZ float64
	for dx := -1; dx <= 1; dx++ {
		for dy := -1; dy <= 1; dy++ {
			for _, p := range f.grid[cellKey{c.ix + dx, c.iy + dy}] {
				ddx, ddy := x-p.X, y-p.Y
				if d2 := ddx*ddx + ddy*ddy; d2 < best {
					best, bestZ = d2, p.Z
				}
			}
		}
	}
	d := math.Sqrt(best)
	if d > f.reach {
		return 0, 0, false
	}
	return d, bestZ, true
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
