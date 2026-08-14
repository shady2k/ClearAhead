package project

import (
	"fmt"
	"math"
	"sort"

	"github.com/shady2k/ClearAhead/server/internal/chunk"
)

// Point — точка в плане (x, z), метры в координатах региона. Высота замыканию
// не нужна: инвалидизация пространственная, а не по отметкам.
type Point struct {
	X, Z float64
}

// Extent — прямоугольный габарит влияния изменения, метры в координатах
// региона.
//
// Это ПОЛНЫЙ габарит: вызывающий обязан включить в него все зоны, которые
// исходник трогает, — для пути ось, земляной след откосов и полосу отчуждения
// покрова. Прямоугольник намеренно консервативен: коридор вдоль оси и круг
// вырубки покрываются своим габаритным прямоугольником, и лишний чанк на
// пересборку дешевле пропущенного шва.
type Extent struct {
	MinX, MinZ, MaxX, MaxZ float64
}

// valid сообщает, что габарит корректен. Отказ, а не подстановка: вырожденный
// или неконечный габарит — ошибка вызывающего.
func (e Extent) valid() bool {
	return finite(e.MinX) && finite(e.MinZ) && finite(e.MaxX) && finite(e.MaxZ) &&
		e.MinX <= e.MaxX && e.MinZ <= e.MaxZ
}

// inflate раздвигает габарит на r во все стороны. Для вырубки вокруг постройки:
// круг вырубки лежит ЗА габаритом постройки (terrain.go:317), и прямоугольник,
// раздвинутый на радиус, покрывает его целиком.
func (e Extent) inflate(r float64) Extent {
	return Extent{e.MinX - r, e.MinZ - r, e.MaxX + r, e.MaxZ + r}
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

// River — вид реки для замыкания: ось русла в плане и ширина охранной области.
//
// Высота точек оси и отметки уреза замыканию не нужны — только план. Это
// сознательно ОТДЕЛЬНЫЙ тип, а не mapfmt.River: пакет не тянет полный рецепт
// карты, потребитель (sqym.4) заполняет вид из mapfmt.Objects.Rivers.
type River struct {
	ID string
	// Axis — ось русла, две и более точек плана.
	Axis []Point
	// HalfWidthM, BankM, ValleyM — полуширина глади, берег и долина рецепта.
	HalfWidthM, BankM, ValleyM float64
}

// ReachM — радиус охранной области: reach из carveRiver (terrain.go:616), в
// котором река задаёт природную поверхность. Число взято из рецепта реки, а не
// придумано: это ответ координатора W1-D на вопрос «какой радиус охранной
// области брать».
func (r River) ReachM() float64 { return r.HalfWidthM + r.BankM + r.ValleyM }

// valid сообщает, что река пригодна для проверки пересечений.
func (r River) valid() bool {
	if len(r.Axis) < 2 {
		return false
	}
	for _, p := range r.Axis {
		if !finite(p.X) || !finite(p.Z) {
			return false
		}
	}
	return finite(r.HalfWidthM) && finite(r.BankM) && finite(r.ValleyM) &&
		r.HalfWidthM >= 0 && r.BankM >= 0 && r.ValleyM >= 0
}

// Region — факты рецепта региона, которые читает замыкание.
//
// Вид узкий намеренно: пакет отвечает на «что задето» и не несёт рецепт
// целиком. Потребитель заполняет его из mapfmt.Map: Extent и levels — из
// m.Terrain.Extent, реки — из m.Objects.Rivers, BuildingClearM — константа
// вырубки вокруг постройки (сегодня 30.0, terrain.go:139, поле не экспортируется
// из terrain и обязано быть названо вызывающим явно).
type Region struct {
	// Rule — правило подробности: охват уровня 0 и последний хранимый уровень.
	Rule chunk.Rule
	// Rivers — реки региона для отказа земляным работам.
	Rivers []River
	// BuildingClearM — на сколько метров за габарит постройки лес не растёт.
	// Радиус вырубки; отрицательное значение — отказ.
	BuildingClearM float64
}

// valid сообщает, что регион пригоден для замыкания. Отказ, а не подстановка:
// нулевое правило или неконечные числа означают забытую строку вызывающего.
func (r Region) valid() error {
	if !r.Rule.Known() {
		return fmt.Errorf("project: правило подробности не задано (Level0RadiusM=%v, MaxLevel=%d) — регион заполнен не полностью",
			r.Rule.Level0RadiusM, r.Rule.MaxLevel)
	}
	if !finite(r.BuildingClearM) || r.BuildingClearM < 0 {
		return fmt.Errorf("project: радиус вырубки вокруг постройки %v — требуется конечное неотрицательное число",
			r.BuildingClearM)
	}
	for i := range r.Rivers {
		if !r.Rivers[i].valid() {
			return fmt.Errorf("project: река %q непригодна: ось из двух и более конечных точек, ширины конечные неотрицательные",
				r.Rivers[i].ID)
		}
	}
	return nil
}

// Change — изменённый исходник и его пространственный габарит влияния.
type Change struct {
	Kind   SourceKind
	Extent Extent
}

// Address — адрес пересборки: чанк уровня Level с клеткой (CX, CZ) либо корень
// мира для проекций, компилируемых целиком.
//
// Регион в адрес не входит намеренно: замыкание регион-агностично, потребитель
// (sqym.4) подставляет свой регион в chunk.Address.
type Address = chunk.Address

// WorldRoot — адрес корня мира: единственный адрес Network.
var WorldRoot = Address{Level: -1}

// Entry — одна проекция замыкания и её адреса к пересборке.
type Entry struct {
	Projection Projection
	Addresses  []Address
}

// GroupPlan — группа согласованности: всё, что обязано опубликоваться одной
// версией. Публикация группы атомарна; между группами «нормальное окно»
// допускается (спека §6.3).
type GroupPlan struct {
	Group   Group
	Entries []Entry
}

// Result — полное замыкание инвалидизации: что пересобирать, разложенное по
// группам согласованности.
type Result struct {
	Groups []GroupPlan
}

// ChunkAddresses возвращает все адреса чанков замыкания (без корня мира) —
// счётчик для замеров: сколько адресов даёт правка одной стрелки.
func (res *Result) ChunkAddresses() []Address {
	var out []Address
	for _, g := range res.Groups {
		for _, e := range g.Entries {
			for _, a := range e.Addresses {
				if a.Level >= 0 {
					out = append(out, a)
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Level != out[j].Level {
			return out[i].Level < out[j].Level
		}
		if out[i].CX != out[j].CX {
			return out[i].CX < out[j].CX
		}
		return out[i].CZ < out[j].CZ
	})
	return out
}

// Closure считает ПОЛНОЕ замыкание инвалидизации: все (проекция, адреса) к
// пересборке, разложенные по группам согласованности, до того как пересобран
// хоть один чанк (§5.4 спеки).
//
// Порядок:
//
//  1. отказ неверному входу: неизвестный вид исходника, некорректный габарит,
//     неполный регион;
//  2. отказ у воды: земляной эффект, чей габарит пересекает охранную область
//     реки, отвергается с причиной — гидрологию первая версия не реализует;
//  3. прямые цели исходника (таблица declared) и их габариты;
//  4. транзитивное замыкание по рёбрам dependsOn: покров → лес, поверхность →
//     геометрия;
//  5. развёртка габаритов в адреса чанков по уровням проекции (замкнутые
//     клетки — шовный инвариант, полуоткрытые — площадные величины);
//  6. разложение по группам согласованности, детерминированный порядок.
func (r Region) Closure(ch Change) (*Result, error) {
	if err := r.valid(); err != nil {
		return nil, err
	}
	if !ch.Kind.Known() {
		return nil, fmt.Errorf("project: неизвестный вид исходника %d — пустое замыкание означало бы «ничего пересобирать не надо» и молча оставило бы старую землю",
			int(ch.Kind))
	}
	if !ch.Extent.valid() {
		return nil, fmt.Errorf("project: габарит изменения некорректен (%v..%v × %v..%v) — требуется конечный прямоугольник с min ≤ max",
			ch.Extent.MinX, ch.Extent.MaxX, ch.Extent.MinZ, ch.Extent.MaxZ)
	}
	if ch.Kind.surfaceAffecting() {
		if id, d, hit := r.riverZoneIntersect(ch.Extent); hit {
			return nil, fmt.Errorf(
				"project: %s пересекает охранную область реки %q: расстояние %v м при радиусе %v м — пересечение реки без сооружения — отказ (спека §5.4); гидрологию первая версия не реализует",
				ch.Kind, id, d, r.riverReach(id))
		}
	}

	// Прямые цели: проекция → габарит её пересборки.
	extents := make(map[Projection]Extent)
	switch ch.Kind {
	case SourcePath:
		extents[Network] = ch.Extent
		extents[Surface] = ch.Extent
		extents[Cover] = ch.Extent
		extents[Geometry] = ch.Extent
	case SourceGrading:
		// Прямой терраморфинг покров НЕ инвалидирует — безусловно (sqym.13).
		// «Свежая земля гола» флагом рецепта не заводится: форма без
		// потребителя. Покров строится из природных масок, реки, расстояния
		// до оси и областей вырубки и WorkedM не зовёт (terrain.go:987–1066),
		// то есть правка высот его не меняет; понадобится голая земля — она
		// станет отдельным земляным эффектом со своим исходником, а не
		// флагом рецепта (объявление Cover несёт это же правило).
		extents[Surface] = ch.Extent
		extents[Geometry] = ch.Extent
	case SourceStructure:
		// Вырубка вокруг постройки живёт ЗА габаритом пятна (terrain.go:317):
		// покров и лес меняются в круге, а не в пятне.
		cleared := ch.Extent.inflate(r.BuildingClearM)
		extents[Surface] = ch.Extent
		extents[Cover] = cleared
		extents[Geometry] = ch.Extent
	case SourceTrackType:
		// Тип меняет FormationToRailTop (mapfmt/construction.go:267–269):
		// земля под полотном и геометрия пути; сеть несёт типы (RenderTrackType)
		// и тоже меняется — «сеть физики — не обязательно» из §5.4 относится к
		// потребителю-физике, а не к сети как проекции.
		extents[Network] = ch.Extent
		extents[Surface] = ch.Extent
		extents[Geometry] = ch.Extent
	case SourceRiver:
		// Река меняет природную поверхность (carveRiver), урез и покров берега.
		extents[Surface] = ch.Extent
		extents[Water] = ch.Extent
		extents[Cover] = ch.Extent
	case SourceClearing:
		extents[Cover] = ch.Extent
	}

	// Транзитивное замыкание по рёбрам зависит.
	queue := sortedProjections(extents)
	for len(queue) > 0 {
		p := queue[0]
		queue = queue[1:]
		e := extents[p]
		for _, reader := range dependsOn[p] {
			if _, ok := extents[reader]; !ok {
				extents[reader] = e
				queue = append(queue, reader)
				sort.Slice(queue, func(i, j int) bool { return queue[i] < queue[j] })
			}
		}
	}

	// Развёртка в адреса и группы.
	res := &Result{}
	for _, g := range []Group{GroupNetwork, GroupSurface, GroupVegetation, GroupWater, GroupGeometry} {
		plan := GroupPlan{Group: g}
		for _, p := range sortedProjections(extents) {
			d := declaration(p)
			if d.Group != g || d.ClientDerived {
				continue
			}
			e := extents[p]
			var addrs []Address
			if p == Network {
				addrs = []Address{WorldRoot}
			} else {
				for _, level := range r.levels(d.Levels) {
					addrs = append(addrs, r.addresses(e, d.Cells, level)...)
				}
				sort.Slice(addrs, func(i, j int) bool {
					if addrs[i].Level != addrs[j].Level {
						return addrs[i].Level < addrs[j].Level
					}
					if addrs[i].CX != addrs[j].CX {
						return addrs[i].CX < addrs[j].CX
					}
					return addrs[i].CZ < addrs[j].CZ
				})
			}
			plan.Entries = append(plan.Entries, Entry{Projection: p, Addresses: addrs})
		}
		if len(plan.Entries) > 0 {
			res.Groups = append(res.Groups, plan)
		}
	}
	return res, nil
}

// riverReach возвращает радиус охранной области реки по имени (для сообщения
// об отказе). Имя заведомо из Region.Rivers — вызвано после riverZoneIntersect.
func (r Region) riverReach(id string) float64 {
	for _, rv := range r.Rivers {
		if rv.ID == id {
			return rv.ReachM()
		}
	}
	return 0
}

// riverZoneIntersect сообщает, пересекает ли габарит охранную область какой-
// либо реки: минимальное расстояние от прямоугольника до оси русла не больше
// reach. Ось — ЛИНИЯ, а не точка: река изгибается, и проверка «расстояние до
// ближайшей точки» пропустила бы пересечение в середине отрезка.
func (r Region) riverZoneIntersect(a Extent) (id string, dist float64, hit bool) {
	for _, rv := range r.Rivers {
		reach := rv.ReachM()
		for i := 1; i < len(rv.Axis); i++ {
			d := extentSegmentDist(a, rv.Axis[i-1], rv.Axis[i])
			if d <= reach {
				return rv.ID, d, true
			}
		}
	}
	return "", 0, false
}

// levels разворачивает спецификацию уровней проекции в конкретный список.
func (r Region) levels(spec levelsSpec) []int {
	switch spec {
	case levelsForest:
		return []int{chunk.ForestLevel}
	case levelsEvery:
		out := make([]int, 0, r.Rule.MaxLevel+1)
		for l := 0; l <= r.Rule.MaxLevel; l++ {
			out = append(out, l)
		}
		return out
	}
	return nil
}

// addresses режет габарит на клетки чанков уровня level.
//
// Замкнутые клетки (шовный инвариант): клетка уровня L покрывает
// [k·side, (k+1)·side] с общими границами, и отсчёт на границе принадлежит
// обоим соседям. Габарит [a, b] задевает клетки k, для которых k·side ≤ b и
// (k+1)·side ≥ a, то есть k от ceil(a/side)−1 до floor(b/side). Коснулся
// границы — пересобираются оба соседа.
//
// Полуоткрытые клетки (покров, лес): ячейка — площадь [k·side, (k+1)·side)
// без общих рядов, габарит задевает k от floor(a/side) до ceil(b/side)−1.
func (r Region) addresses(e Extent, cells cellsSpec, level int) []Address {
	side := float64(chunk.SideM0) * float64(int64(1)<<level)
	var c0x, c1x, c0z, c1z int
	if cells == cellsClosed {
		c0x = int(math.Ceil(e.MinX/side)) - 1
		c1x = int(math.Floor(e.MaxX / side))
		c0z = int(math.Ceil(e.MinZ/side)) - 1
		c1z = int(math.Floor(e.MaxZ / side))
	} else {
		c0x = int(math.Floor(e.MinX / side))
		c1x = int(math.Ceil(e.MaxX/side)) - 1
		c0z = int(math.Floor(e.MinZ / side))
		c1z = int(math.Ceil(e.MaxZ/side)) - 1
	}
	out := make([]Address, 0, (c1x-c0x+1)*(c1z-c0z+1))
	for cz := c0z; cz <= c1z; cz++ {
		for cx := c0x; cx <= c1x; cx++ {
			out = append(out, Address{Level: level, CX: cx, CZ: cz})
		}
	}
	return out
}

// sortedProjections — проекции карты в объявленном порядке: детерминизм
// результата, чтобы тесты и журнал сравнивали замыкания, а не догадывались о
// порядке карты.
func sortedProjections(m map[Projection]Extent) []Projection {
	out := make([]Projection, 0, len(m))
	for p := range m {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// extentSegmentDist — минимальное расстояние между прямоугольником и отрезком.
//
// Точность важна: отказ у реки решается сравнением с reach, и ошибка в
// расстояние на границе — пропущенный или ложный отказ. Сведение к расстоянию
// между отрезками полное: минимум расстояния между двумя выпуклыми множествами
// лежит либо на границе (конец одного против другого), либо в нуле пересечения;
// внутренняя точка обоих невозможна без пересечения.
func extentSegmentDist(a Extent, p, q Point) float64 {
	// Отрезок, целиком лежащий ВНУТРИ прямоугольника, ни с одним ребром не
	// пересекается, и расстояние до рёбер было бы положительным — ложный
	// пропуск отказа. Внутренность ловится концом.
	if pointInExtent(a, p) || pointInExtent(a, q) {
		return 0
	}
	edges := [4][2]Point{
		{{a.MinX, a.MinZ}, {a.MaxX, a.MinZ}},
		{{a.MaxX, a.MinZ}, {a.MaxX, a.MaxZ}},
		{{a.MaxX, a.MaxZ}, {a.MinX, a.MaxZ}},
		{{a.MinX, a.MaxZ}, {a.MinX, a.MinZ}},
	}
	d := math.Inf(1)
	for _, e := range edges {
		d = math.Min(d, segmentDist(e[0], e[1], p, q))
	}
	return d
}

// pointInExtent — лежит ли точка внутри или на границе прямоугольника.
func pointInExtent(a Extent, p Point) bool {
	return p.X >= a.MinX && p.X <= a.MaxX && p.Z >= a.MinZ && p.Z <= a.MaxZ
}

// segmentDist — минимальное расстояние между отрезками: 0 при пересечении,
// иначе минимум по четырём «конец против отрезка».
func segmentDist(a0, a1, b0, b1 Point) float64 {
	if segmentsIntersect(a0, a1, b0, b1) {
		return 0
	}
	d := pointSegmentDist(b0, a0, a1)
	d = math.Min(d, pointSegmentDist(b1, a0, a1))
	d = math.Min(d, pointSegmentDist(a0, b0, b1))
	return math.Min(d, pointSegmentDist(a1, b0, b1))
}

// segmentsIntersect — пересекаются ли отрезки, включая касание концом.
func segmentsIntersect(a0, a1, b0, b1 Point) bool {
	o1 := orient(a0, a1, b0)
	o2 := orient(a0, a1, b1)
	o3 := orient(b0, b1, a0)
	o4 := orient(b0, b1, a1)
	if o1 == 0 && onSegment(a0, a1, b0) {
		return true
	}
	if o2 == 0 && onSegment(a0, a1, b1) {
		return true
	}
	if o3 == 0 && onSegment(b0, b1, a0) {
		return true
	}
	if o4 == 0 && onSegment(b0, b1, a1) {
		return true
	}
	return o1*o2 < 0 && o3*o4 < 0
}

// orient — знак поворота тройки точек: > 0 против часовой, < 0 по часовой, 0 —
// коллинеарно.
func orient(a, b, c Point) float64 {
	return (b.X-a.X)*(c.Z-a.Z) - (b.Z-a.Z)*(c.X-a.X)
}

// onSegment — лежит ли точка c на отрезке [a, b] (коллинеарность
// предполагается проверенной).
func onSegment(a, b, c Point) bool {
	return c.X >= math.Min(a.X, b.X)-1e-12 && c.X <= math.Max(a.X, b.X)+1e-12 &&
		c.Z >= math.Min(a.Z, b.Z)-1e-12 && c.Z <= math.Max(a.Z, b.Z)+1e-12
}

// pointSegmentDist — расстояние от точки до отрезка: ортогональная проекция с
// зажатием в пределы.
func pointSegmentDist(p, a, b Point) float64 {
	abx, abz := b.X-a.X, b.Z-a.Z
	len2 := abx*abx + abz*abz
	if len2 == 0 {
		return math.Hypot(p.X-a.X, p.Z-a.Z)
	}
	t := ((p.X-a.X)*abx + (p.Z-a.Z)*abz) / len2
	t = math.Min(1, math.Max(0, t))
	qx, qz := a.X+t*abx, a.Z+t*abz
	return math.Hypot(p.X-qx, p.Z-qz)
}
