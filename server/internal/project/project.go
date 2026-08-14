// Package project объявляет граф инвалидизации проекций и считает полное
// замыкание пересборки ДО того, как пересобран хоть один чанк.
//
// # Модель
//
// Исходник — авторитетный факт (путь, терраморфинг, здание, тип решётки,
// река, вырубка). Проекция — материализованный результат компиляции (сеть,
// поверхность, покров, лес, вода, геометрия). Решённый контракт
// `.internal/specs/2026-08-13-sources-compilers-projections.md` §1 запрещает
// «правка высоты меняет всё»: один строительный исходник инвалидирует несколько
// НЕЗАВИСИМЫХ проекций, и для каждой объявлены пять полей (§5.4 спеки: входы,
// габарит влияния, уровни подробности, формат результата, группа
// согласованности). Объявления — ниже, в таблице declared; Closure считает по
// ним замыкание.
//
// # Почему стрелки проверены в коде, а не взяты из таблицы
//
// Редакция 2 спеки писала, что правка высоты перестраивает покров, лес, урез
// воды, маски и коллизию. Код показывает обратное, и это проверено по текущему
// дереву:
//
//   - покров (terrain.Field.CoverAt) WorkedM НЕ зовёт — он строится из масок
//     рецепта, расстояния до русла и до оси и пятен вырубки
//     (terrain.go:987–1066);
//   - лес (ChunkForest) читает класс покрова той же ячейки и маску леса
//     рецепта, не высоты (terrain.go:1119–1142);
//   - маска балласта строится на КЛИЕНТЕ из оси и подошвы призмы
//     (client/scripts/world.gd:832–837, track_view.gd:66–84);
//   - коллизия собирается из тех же мешей, что видны, включая путь и здания
//     (world.gd:1754–1788).
//
// Верно другое: исходник инвалидирует несколько независимых проекций, и
// замыкание — множество (проекция, адреса) к пересборке, разложенное по
// группам согласованности. Строительная транзакция (sqym.4) зовёт Closure до
// сборки и публикует каждую группу одной версией.
//
// # Шовный инвариант
//
// У соседних чанков общий ряд отсчётов (chunk.Samples = 65 отсчётов на 64
// интервала): отсчёт на границе принадлежит ОБОИМ. Патч, меняющий граничный
// отсчёт, обязан присутствовать в обоих соседних патчах, нести там одинаковую
// абсолютную отметку и публиковаться с ними одной версией — иначе вернутся
// трещины между чанками, которые контракт уже устранил абсолютными
// координатами (§5.1). Замыкание обеспечивает это по построению: проекции с
// общими граничными отсчётами (поверхность, геометрия, вода) считают клетки
// ЗАМКНУТЫМИ — габарит, коснувшийся границы чанка, включает соседа, и все
// патчи одной проекции падают в одну группу. Покров и лес — площадные
// величины по ячейкам без общих рядов (CoverCells = Samples − 1), им шов не
// нужен.
//
// # Отказ у воды
//
// Самый дорогой fan-out — вода, и первая версия вправе отказывать любому
// земляному эффекту, чей габарит пересекает охранную область реки (§5.4).
// Радиус области берётся из рецепта реки, а не придумывается: reach =
// HalfWidth + Bank + Valley — ровно та полоса, внутри которой carveRiver задаёт
// природную поверхность (terrain.go:610–633, локальная переменная reach на
// 616). Оговорка: механизм уреза (terrain.WaterEdge, terrain.go:655–664) читает
// WorkedM, то есть зависит от рабочей поверхности; вода остаётся функцией
// (река, рецепт) ТОЛЬКО потому, что земляные работы у реки отвергаются — снять
// отказ нельзя, не пересмотрев эту стрелку.
package project

// SourceKind — вид изменённого исходника. Значение вне объявленного ряда —
// отказ в Closure, а не пустое замыкание: пустое замыкание означало бы «ничего
// пересобирать не надо» и молча оставило бы старую землю.
type SourceKind int

const (
	// SourcePath — путь: узлы, рёбра, стрелки, геометрия, прогоны.
	SourcePath SourceKind = iota
	// SourceGrading — прямой терраморфинг: правка земли вручную.
	SourceGrading
	// SourceStructure — здание или площадка (GradedSite): пятно с жёстким
	// габаритом.
	SourceStructure
	// SourceTrackType — тип решётки: вертикальный стек пути.
	SourceTrackType
	// SourceRiver — река или дамба: русло и его размеры.
	SourceRiver
	// SourceClearing — вырубка: область, где лес не растёт.
	SourceClearing
)

// Known сообщает, что вид исходника объявлен. Неизвестное значение — ошибка
// вызывающего, а не легитимный «ничего не задето».
func (k SourceKind) Known() bool { return k >= SourcePath && k <= SourceClearing }

// surfaceAffecting — исходники с земляным эффектом: их габарит обязан не
// пересекать охранную область реки, иначе отказ. Вырубка земли не трогает, у
// реки своя правка.
func (k SourceKind) surfaceAffecting() bool {
	switch k {
	case SourcePath, SourceGrading, SourceStructure, SourceTrackType:
		return true
	}
	return false
}

func (k SourceKind) String() string {
	switch k {
	case SourcePath:
		return "путь"
	case SourceGrading:
		return "терраморфинг"
	case SourceStructure:
		return "здание, площадка"
	case SourceTrackType:
		return "тип решётки"
	case SourceRiver:
		return "река, дамба"
	case SourceClearing:
		return "вырубка"
	}
	return "неизвестный исходник"
}

// Projection — материализованная проекция: результат компиляции исходников.
type Projection int

const (
	// Network — скомпилированная сеть: одна компиляция на мир.
	Network Projection = iota
	// Surface — земля чанка: вечная база плюс патч по версии (§5.1).
	Surface
	// Cover — покров: класс поверхности и сомкнутость на ячейку.
	Cover
	// Vegetation — растительность: база (лес) плюс исключения (§7 спеки).
	Vegetation
	// Water — геометрия воды: гладь и урез.
	Water
	// Geometry — геометрия рендера: меши земли, пути, построек.
	Geometry
	// BallastMask — маска балласта. Живёт на клиенте: чистая функция оси и
	// подошвы призмы, адресов пересборки не порождает.
	BallastMask
	// Collision — коллизия. Живёт на клиенте: trimesh видимых мешей, адресов
	// пересборки не порождает.
	Collision
)

// Known сообщает, что проекция объявлена.
func (p Projection) Known() bool { return p >= Network && p <= Collision }

// Group — группа согласованности: что обязано публиковаться одной версией.
//
// Группы независимы нарочно: покров не зависит от поверхности (правка высоты
// его не перестраивает), поэтому новая земля со старым покровом в одном кадре
// — легитимное состояние, а не рассинхрон. «Нормальное окно» между разными
// версиями — спека §6.3.
type Group int

const (
	// GroupNetwork — скомпилированная сеть мира.
	GroupNetwork Group = iota
	// GroupSurface — патчи поверхности шовно замкнутого следа, все уровни.
	GroupSurface
	// GroupVegetation — покров и лес одного следа: лес читает покров, и версия
	// с новым покровом и старым лесом показала бы деревья на свежем лугу.
	GroupVegetation
	// GroupWater — геометрия воды.
	GroupWater
	// GroupGeometry — геометрия рендера следа.
	GroupGeometry
)

// Known сообщает, что группа объявлена.
func (g Group) Known() bool { return g >= GroupNetwork && g <= GroupGeometry }

// levelsSpec — как проекция раскладывается по уровням подробности.
type levelsSpec int

const (
	// levelsEvery — все уровни 0..MaxLevel правила региона.
	levelsEvery levelsSpec = iota
	// levelsForest — только уровень 0 (chunk.ForestLevel): дальше деревья
	// рассыпает клиент сам, рубить их некому (chunk.go:365–375).
	levelsForest
	// levelsWorld — уровней нет: одна компиляция на мир.
	levelsWorld
)

// cellsSpec — как габарит режется на клетки чанков уровня.
type cellsSpec int

const (
	// cellsClosed — замкнутые клетки: граничный отсчёт принадлежит обоим
	// соседям, и габарит, коснувшийся границы, включает обоих (шовный
	// инвариант). Для проекций с общими рядами отсчётов.
	cellsClosed cellsSpec = iota
	// cellsOpen — полуоткрытые клетки: значения площадные, общих рядов нет.
	cellsOpen
)

// Declared — объявление проекции: пять обязательных полей (§5.4 спеки).
//
// Комментарии на полях — не украшение: это контракт для sqym.4, и расхождение
// объявления с кодом, на которое оно ссылается, — баг объявления.
type Declared struct {
	// Projection — сама проекция: ключ строки таблицы.
	Projection Projection
	// Inputs — исходники, правка которых инвалидирует проекцию.
	Inputs []SourceKind
	// Recipe — части рецепта региона, от которых проекция зависит.
	Recipe string
	// Footprint — габарит влияния: на какую площадь вокруг изменения
	// распространяется пересборка.
	Footprint string
	// Levels — уровни подробности, на которых проекция существует.
	Levels levelsSpec
	// Cells — как габарит режется на клетки чанков.
	Cells cellsSpec
	// Format — формат результата.
	Format string
	// Group — группа согласованности.
	Group Group
	// ClientDerived — проекция живёт на клиенте как функция своей проекции-
	// входа и адресов пересборки не порождает.
	ClientDerived bool
}

// declared — таблица проекций. Один источник истины для объявлений и для
// Closure; тест Completeness проверяет заполненность всех пяти полей.
var declared = []Declared{
	{
		Projection: Network,
		Inputs:     []SourceKind{SourcePath, SourceTrackType},
		Recipe:     "нет — сеть чистая функция исходников пути",
		Footprint:  "весь мир: сеть компилируется целиком одной версией, адрес один (корень мира)",
		Levels:     levelsWorld,
		Cells:      cellsClosed,
		Format:     "скомпилированная сеть: элементы, профили, роли, типы, прогоны (track.RenderGeometry)",
		Group:      GroupNetwork,
	},
	{
		Projection: Surface,
		Inputs:     []SourceKind{SourcePath, SourceGrading, SourceStructure, SourceTrackType, SourceRiver},
		Recipe: "рецепт рельефа (BaseZ, октавы) и рецепт земляных работ (FormationHalfWidth, " +
			"SideSlope); природная поверхность, от которой эффекты считаются (§3 контракта)",
		Footprint: "габарит исходника с земляным следом: ось или пятно плюс откосы " +
			"(FormationHalfWidth + SideSlope×перепад; terrain.Field.reach)",
		Levels: levelsEvery,
		Cells:  cellsClosed,
		Format: "TerrainPatch: chunk_address, base_chunk_hash, affected_mask, absolute_height над вечной базой (§5.1)",
		Group:  GroupSurface,
	},
	{
		Projection: Cover,
		// Вход без SourceGrading — НАМЕРЕННО, и это решение координатора
		// (sqym.13): прямой терраморфинг покров НЕ инвалидирует, безусловно.
		// «Свежая земля гола» флагом рецепта не заводится — форма без
		// потребителя: покров строится из природных масок, реки, расстояния
		// до оси и областей вырубки и WorkedM не зовёт (terrain.go:987–1066),
		// то есть правка высот его не меняет и сегодня. Понадобится голая
		// земля — она станет отдельным земляным эффектом со своим исходником,
		// а не флагом рецепта. Closure несёт это же правило.
		Inputs: []SourceKind{SourcePath, SourceStructure, SourceRiver, SourceClearing},
		Recipe: "рецепт покрова (маски Veg/Forest/Species, пороги, полоса отчуждения ClearHalfWidth); " +
			"русло реки — песок и берег (terrain.go:996–1010, 1049–1054)",
		Footprint: "габарит исходника: для пути — полоса отчуждения вокруг оси, для постройки — круг " +
			"вырубки за габаритом (BuildingClear), для реки — берег и песчаный пояс",
		Levels: levelsEvery,
		Cells:  cellsOpen,
		Format: "блоб CoverBytes: байт (класс, сомкнутость) на ячейку сетки покрова (chunk.PackCover)",
		Group:  GroupVegetation,
	},
	{
		Projection: Vegetation,
		// Прямой исходник один — вырубка. Путь, здание и река достигают лес
		// ТРАНЗИТИВНО через покров (ребро Cover→Vegetation): их вырубка запечена
		// в класс ячейки покрова, а лес читает класс. Замыкание обязано
		// дотянуться до леса, а не остановиться на покрове.
		Inputs:    []SourceKind{SourceClearing},
		Recipe:    "рецепт покрова — маска леса для плотности (terrain.forestDensity)",
		Footprint: "тот же след, что у покрова: вырубка меняет класс ячейки, лес читает класс",
		Levels:    levelsForest,
		Cells:     cellsOpen,
		Format:    "блоб ForestBytes (бит на ячейку уровня 0) плюс исключения (§7 спеки)",
		Group:     GroupVegetation,
	},
	{
		Projection: Water,
		Inputs:     []SourceKind{SourceRiver},
		// ЗАВИСИМОСТЬ ОТ РАБОЧЕЙ ПОВЕРХНОСТИ записана явно (sqym.12), и это
		// не украшение: WaterEdge читает WorkedM (terrain.go:684–693), то есть
		// геометрия воды зависит от земляных работ НАПРЯМУЮ, а не только от
		// реки. Объявленная в §1.3 независимость воды держится единственной
		// подпоркой — отказом земляному эффекту, чей габарит пересекает
		// охранную область реки (Closure, surfaceAffecting; reach =
		// HalfWidth + Bank + Valley). Снять отказ нельзя, не пересмотрев эту
		// стрелку: правка высот у реки потянула бы самый дорогой fan-out
		// графа. Гидрологию первая версия не реализует — отказ честен.
		Recipe: "рецепт рельефа вокруг русла: урез живёт на природной поверхности (carveRiver), " +
			"но ЗАМЕРЯЕТСЯ по рабочей поверхности (WaterEdge → WorkedM, terrain.go:684–693)",
		Footprint: "габарит реки: русло плюс reach = HalfWidth + Bank + Valley (terrain.go:616); " +
			"независимость от земляных работ — отказ им в этой области (Closure), не свойство построения",
		Levels: levelsEvery,
		Cells:  cellsClosed,
		Format: "геометрия воды: гладь и урез по чанку (terrain.WaterEdge)",
		Group:  GroupWater,
	},
	{
		Projection: Geometry,
		Inputs:     []SourceKind{SourcePath, SourceGrading, SourceStructure, SourceTrackType},
		Recipe:     "нет — геометрия строится из проекций: поверхности, сети и построек",
		Footprint:  "габарит исходника плюс след: меш земли повторяет поверхность, меш пути — сеть",
		Levels:     levelsEvery,
		Cells:      cellsClosed,
		Format:     "меши: земля (TerrainMesh), путь (track_view), постройки",
		Group:      GroupGeometry,
	},
	{
		Projection:    BallastMask,
		Inputs:        []SourceKind{SourcePath, SourceTrackType},
		Recipe:        "тип решётки: полуширина призмы, откос, вертикальный стек (track.RenderTrackType)",
		Footprint:     "коридор оси: полуширина призмы плюс откос",
		Levels:        levelsEvery,
		Cells:         cellsOpen,
		Format:        "клиентская производная: ячейки, накрытые подошвой призмы (world.gd:_build_ballast_mask)",
		Group:         GroupNetwork,
		ClientDerived: true,
	},
	{
		Projection:    Collision,
		Inputs:        []SourceKind{SourcePath, SourceStructure},
		Recipe:        "нет — коллизия собирается из тех же мешей, что видны (world.gd:1754–1788)",
		Footprint:     "габарит видимых мешей следа",
		Levels:        levelsEvery,
		Cells:         cellsClosed,
		Format:        "клиентская производная: trimesh-коллизия видимых мешей (world.gd:_build_solid)",
		Group:         GroupGeometry,
		ClientDerived: true,
	},
}

// Declarations возвращает копии объявлений проекций: пять полей каждой (§5.4
// спеки). Копии намеренно: потребитель (sqym.4) читает объявление, но не
// правит таблицу, по которой считает Closure.
func Declarations() []Declared {
	out := make([]Declared, len(declared))
	copy(out, declared)
	// Копия структуры не защищает слайс Inputs: у копий он делит массив с
	// таблицей. Слайсы копируются отдельно, чтобы потребитель не правил
	// объявление сквозь копию.
	for i := range out {
		out[i].Inputs = append([]SourceKind(nil), out[i].Inputs...)
	}
	return out
}

// declaration возвращает объявление проекции.
func declaration(p Projection) *Declared {
	for i := range declared {
		if declared[i].Projection == p {
			return &declared[i]
		}
	}
	return nil
}

// dependsOn — рёбра между проекциями: какая проекция читает выход другой.
//
// Ребро — транзитивность замыкания: правка вырубки обязана дотянуться до леса
// через покров, правка реки — до геометрии через поверхность. Ребра сеть→
// геометрия и сеть→маска балласта намеренно НЕТ: сеть меняется только вместе с
// исходником, который уже задаёт локальный габарит геометрии, а маска —
// клиентская производная сети и пересобирается при получении новой версии.
var dependsOn = map[Projection][]Projection{
	Surface: {Geometry},
	Cover:   {Vegetation},
}
