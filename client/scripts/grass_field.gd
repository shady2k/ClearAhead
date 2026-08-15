## grass_field.gd — ПОЛЕ ТРАВЫ: КРУГ ПОСАДКИ ПОД БЮДЖЕТОМ КАДРА (sqym.7).
##
## Одно поколение травы — один экземпляр: корень, план, очередь и кэш чанков.
## Мир держит ЖИВОЕ поле (видимое) и, пока перестройка набора версии идёт, —
## СТРОЯЩЕЕСЯ поле (невидимое, корень не в дереве). Готовое строящееся поле
## становится видимым ОДНИМ переключением (commit): старый круг снят, новый
## поставлен, полукруга на экране не бывает.
##
## Всё состояние поколения живёт в экземпляре — у двух полей нет ни одной общей
## переменной, кроме самих мешей травы (они от поколения не зависят вовсе).
##
## Извлечено из world.gd без изменения устройства: план пересобирается по ходу,
## постройка идёт понемногу каждый кадр, задание продолжаемое. Что изменилось —
## только адресация: вместо глобальных переменных мира поле читает свои.
class_name GrassField
extends RefCounted

## Минимальная сомкнутость покрова, при которой ячейка засевается. Четыре из
## пятнадцати — густота разреженной опушки, ниже этого трава стоит редкой щёткой,
## а её цена та же (подробность — в шапке world.gd, у константы GRASS_MIN_CLOSURE,
## до выноса поля в отдельный файл).
const GRASS_MIN_CLOSURE := 4
const GRASS_FAR := 200.0        # м — радиус круга посадки, всегда один
const GRASS_BASE := 0.22        # м — базовая ячейка посадки: 20 ячеек на м²
## [радиус кольца, ШАГ по базовой сетке целым числом, доля занятых ячеек].
## Первые четыре строки спайковой таблицы; пятая (420 м) отпадает вместе с его
## радиусом. Плотность падает вчетверо на кольцо — шаг k режет её как k².
const GRASS_RINGS := [
	[45.0, 1, 1.00], [90.0, 2, 0.90], [160.0, 3, 0.80], [GRASS_FAR, 5, 0.70],
]
## Сторона чанка посадки. Единица постройки, единица кэша и единица отсечения по
## пирамиде разом: у одного большого MultiMesh поэкземплярного отсечения НЕТ, и
## трава уходила бы в отрисовку целиком даже вблизи.
const GRASS_CHUNK := 32.0
## Бюджет на кадр. Плотный чанк — это около 20 тысяч ячеек, то есть под сотню
## миллисекунд; очередь непрерываемых заданий дала бы те же рывки, только по
## сотне миллисекунд каждый.
const GRASS_BUDGET_US := 4000
## Во сколько раз дальше круга держим уже построенное: без тёплой полосы шаг
## назад заставлял бы строить только что выброшенное.
const GRASS_WARM := 1.30
## Насколько уедет камера, прежде чем ПЛАН пересчитывается. Порог — лечение
## устаревания, а не отсрочка работы: план дёшев, постройка идёт понемногу
## каждый кадр (разбор — в шапке world.gd у прежней GRASS_REPLAN).
const GRASS_REPLAN := 6.0
## Ячейка маски балласта. Проверять каждый пучок против всех точек оси — это
## сотня тысяч пучков на сотню точек, то есть десятки миллионов сравнений на
## пересадку. Маска строится один раз на ячейках этой стороны (разбор — в шапке
## world.gd у прежней BALLAST_MASK_CELL).
const BALLAST_MASK_CELL := 1.0

var root: Node3D = null
var focus := Vector2.INF
var meshes: Array[ArrayMesh] = []
var mats: Array[Material] = []
var plants := 0
var chunks := {}          # Vector3i -> Node3D
var want := {}            # Vector2i -> желаемый уровень
var queue: Array[Vector3i] = []
var queued := {}          # то же множеством, против дублей в очереди
var job := {}             # текущее ПРОДОЛЖАЕМОЕ задание
var cover_index := {}     # Vector2i(cx, cz) -> Dictionary яруса
var ground: Array = []   # якоря, из которых строится cover_index
var ballast := {}         # маска балласта: ссылка на словарь мира
var rule: ChunkRule = null
var camera_plan: Callable = Callable()
var multimesh: Callable = Callable()
var stats := {}


## setup — связать поле с миром. Всё, что поле читает извне, приходит сюда и
## только сюда: у строящегося поля (перестройка версии) — своя земля, своя маска
## и свои (пустые) счётчики, и оно не трогает ни одного числа живого мира.
func setup(p_rule: ChunkRule, p_ground: Array, p_ballast: Dictionary,
		p_camera_plan: Callable, p_multimesh: Callable, p_stats: Dictionary) -> void:
	rule = p_rule
	ground = p_ground
	ballast = p_ballast
	camera_plan = p_camera_plan
	multimesh = p_multimesh
	stats = p_stats


## plant_all — построить круг ЦЕЛИКОМ и прямо сейчас.
##
## Только для первой загрузки мира: снимок нельзя снимать, пока мир
## достраивается, и первый круг обязан стоять целиком до первого кадра. Дальше
## траву ведёт tick по бюджету кадра, и строящееся поле перестройки никогда не
## зовёт plant_all — оно копится кусочками, чтобы не съесть кадр.
func plant_all() -> void:
	if ground.is_empty() or rule == null or camera_plan.is_null():
		return
	var t0 := Time.get_ticks_usec()
	plan()
	while not (queue.is_empty() and job.is_empty()):
		work(1 << 30)
	show()
	plants += 1
	stats["grass_plants"] = plants
	# Цена постройки — ЗАМЕР, а не оценка: без числа «иногда дёргается»
	# невозможно ни подтвердить, ни опровергнуть.
	stats["grass_plant_ms"] = float(Time.get_ticks_usec() - t0) / 1000.0
	count()


## tick — план при сдвиге камеры и постройка в пределах бюджета. true, когда
## число готовых чанков изменилось: мир пересчитывает панель только тогда.
##
## ПЕРВЫЙ ВЫЗОВ ПЛАНИРУЕТ: у только что рождённого поля фокус ещё не задан
## (Vector2.INF), и план — не «камера уехала», а «круга ещё нет». Без этого
## строящееся поле перестройки версии прошло бы свою фазу пустым: очередь и
## задание пусты, pending() == 0, и коммит поставил бы в мир пустую траву.
func tick(budget_us: int) -> bool:
	if camera_plan.is_null():
		return false
	var p: Vector2 = camera_plan.call()
	if focus == Vector2.INF or p.distance_to(focus) >= GRASS_REPLAN:
		plan()
	var before := chunks.size()
	work(budget_us)
	if chunks.size() != before:
		count()
		return true
	return false
## pending — сколько работы осталось: очередь плюс задание в руках.
func pending() -> int:
	return queue.size() + (0 if job.is_empty() else 1)


## ensure_root — корень поколения. Заводится ОТДЕЛЬНО от постановки в дерево:
## чанки кладутся в него с первого же задания, а видимым он становится тогда,
## когда мир этого захочет (attach).
func ensure_root() -> void:
	if root != null:
		return
	root = Node3D.new()
	root.name = "Grass"


## attach — поставить корень поколения в мир. НЕ делается само собой, и это не
## церемония: строящееся поколение перестройки версии живёт корнем ВНЕ дерева
## (полукруга на экране не бывает), и в мир его ставит commit мира — тем же
## вызовом, что и здесь.
##
## Куплено дефектом. При выносе поля из world.gd (acccf3b) корень уехал сюда, а
## строка `world.add_child(_grass_root)` не уехала никуда: трава строилась,
## считалась панелью и не давала на экране НИ ОДНОГО пикселя. Замер, которым это
## доказано, — два снимка одной сцены, с травой и без неё: 0 различных пикселей
## из 1 440 000. Панель при этом честно показывала 118 762 пучка, потому что
## count() считает экземпляры, а не нарисованное (см. его шапку).
func attach(parent: Node3D) -> void:
	ensure_root()
	var had: Node = root.get_parent()
	if had == parent:
		return
	if had != null:
		had.remove_child(root)
	parent.add_child(root)


## free_all — освободить корень поля. Мир зовёт это, снимая поколение: корень
## уходит из дерева и освобождается вместе со всеми чанками.
func free_all() -> void:
	if root == null:
		return
	if root.get_parent() != null:
		root.get_parent().remove_child(root)
	root.queue_free()
	root = null


## plan — какие чанки и с какой подробностью нужны СЕЙЧАС.
##
## План пересчитывается, постройка не делается: здесь только бухгалтерия по трём
## сотням чанков. Очередь строится ПО БЛИЗОСТИ — под ногами трава нужна раньше,
## чем на горизонте.
func plan() -> void:
	var p: Vector2 = camera_plan.call()
	focus = p
	ensure_root()
	if meshes.is_empty():
		for k in Vegetation.GRASS_KINDS.size():
			meshes.append(Vegetation.grass_mesh(Vegetation.GRASS_KINDS[k]))
			mats.append(Vegetation.grass_material(Vegetation.GRASS_KINDS[k], k))
	if cover_index.is_empty():
		# Только уровень 0: на дальних уровнях ячейка покрова 64 м, и пучок в ней
		# означал бы одну травинку на гектар. Круг GRASS_FAR всё равно короче
		# радиуса нулевого уровня (на затравке 512 м), так что уровень 0 его
		# накрывает целиком.
		for g_raw in ground:
			var g: Dictionary = g_raw
			if int(g["level"]) == 0:
				cover_index[Vector2i(int(g["cx"]), int(g["cz"]))] = g
	want.clear()
	# ОЧЕРЕДЬ ПЕРЕСОБИРАЕТСЯ ЦЕЛИКОМ, А НЕ ДОПОЛНЯЕТСЯ. Довод спайка, там же и
	# замеренный: на непрерывной панораме бюджет уходил на чанки, которые камера
	# давно проехала. Устаревшее задание — не «немного лишней работы», оно
	# вытесняет нужное.
	queue.clear()
	queued.clear()
	var r := int(ceil(GRASS_FAR / GRASS_CHUNK)) + 1
	var c0 := int(floor(p.x / GRASS_CHUNK))
	var c1 := int(floor(p.y / GRASS_CHUNK))
	# Самый грубый уровень, который вообще применяется при радиусе GRASS_FAR:
	# последняя строка таблицы кончается ровно на нём.
	var coarse := GRASS_RINGS.size() - 1
	var rough := []      # чанки без единого готового уровня — им сперва грубый
	var fine := []       # и только потом желаемая подробность
	for gx in range(c0 - r, c0 + r + 1):
		for gz in range(c1 - r, c1 + r + 1):
			var d2 := chunk_d2(gx, gz, p)
			var level := level_for(sqrt(d2))
			if level < 0:
				continue
			var flat := Vector2i(gx, gz)
			want[flat] = level
			# ГРУБЫЙ УРОВЕНЬ ВПЕРЁД, и условие тут ровно одно: ЧАНК ГОЛЫЙ. Плотный
			# чанк — это двадцать тысяч ячеек, то есть десятки кадров бюджета; пока
			# он строится, под ногами была бы голая земля. Грубый — сотни ячеек:
			# покров появляется сразу, подробность приезжает следом.
			if not has_any(flat):
				rough.append([d2, Vector3i(gx, gz, coarse)])
			var key := Vector3i(gx, gz, level)
			if not chunks.has(key):
				fine.append([d2, key])
	rough.sort_custom(func(a, b): return a[0] < b[0])
	fine.sort_custom(func(a, b): return a[0] < b[0])
	for w in rough + fine:
		if queued.has(w[1]):
			continue
		queue.append(w[1])
		queued[w[1]] = true
	# Начатое задание, которое больше не нужно, бросаем: доделывать его — значит
	# занимать бюджет тем, чего никто не увидит.
	if not job.is_empty():
		var jk: Vector3i = job["key"]
		if int(want.get(Vector2i(jk.x, jk.y), -1)) < 0:
			job = {}
	evict(p)
	show()


## chunk_d2 — квадрат расстояния от взгляда до ЦЕНТРА чанка.
##
## По центру, а не по ближнему или дальнему углу, и это замер спайка: по ближнему
## углу чанк, едва задевший круг 45 м, получал плотность ближнего уровня целиком
## — густая область раздувалась на диагональ чанка и стоила в 1.67 раза больше
## работы за то, чего не видно. По дальнему была бы обратная ошибка: под ногами
## реже, чем нужно, а это как раз то, что видно. Центр даёт несмещённую.
func chunk_d2(gx: int, gz: int, focus_at: Vector2) -> float:
	var dx := (float(gx) + 0.5) * GRASS_CHUNK - focus_at.x
	var dz := (float(gz) + 0.5) * GRASS_CHUNK - focus_at.y
	return dx * dx + dz * dz


## level_for — номер кольца по удалению, или −1 за краем круга.
func level_for(d: float) -> int:
	if d > GRASS_FAR:
		return -1
	for k in GRASS_RINGS.size():
		if d <= float(GRASS_RINGS[k][0]):
			return k
	return GRASS_RINGS.size() - 1


## has_any — есть ли у квадрата земли хоть какой-то готовый уровень.
func has_any(flat: Vector2i) -> bool:
	for k in GRASS_RINGS.size():
		if chunks.has(Vector3i(flat.x, flat.y, k)):
			return true
	return false


## evict — выброс чанков за тёплой полосой. Полоса нужна, чтобы шаг назад
## не заставлял строить только что выброшенное.
func evict(focus_at: Vector2) -> void:
	var keep := GRASS_FAR * GRASS_WARM
	var keep2 := keep * keep
	for key in chunks.keys():
		if chunk_d2(key.x, key.y, focus_at) <= keep2:
			continue
		var node: Node3D = chunks[key]
		root.remove_child(node)
		node.queue_free()
		chunks.erase(key)


## show — показываем ТОТ УРОВЕНЬ, КОТОРЫЙ УЖЕ ЕСТЬ, ближайший к желаемому.
##
## Без этого вновь вошедший чанк стоял бы голой землёй, пока строится: у него нет
## ни одного готового уровня, и «покажем предыдущий» там не работает.
func show() -> void:
	var best := {}
	for key in chunks.keys():
		var flat := Vector2i(key.x, key.y)
		var want_level: int = int(want.get(flat, -1))
		if want_level < 0:
			continue
		var d: int = absi(key.z - want_level)
		if not best.has(flat) or d < int(best[flat][0]):
			best[flat] = [d, key]
	for key in chunks.keys():
		var flat := Vector2i(key.x, key.y)
		var node: Node3D = chunks[key]
		node.visible = best.has(flat) and best[flat][1] == key


## work — постройка в пределах бюджета. Задание переживает границу кадра:
## курсор по ячейкам покрова лежит в самом задании.
func work(budget_us: int) -> void:
	var t0 := Time.get_ticks_usec()
	while true:
		if job.is_empty():
			if queue.is_empty():
				return
			job = job_make(queue.pop_front())
			continue
		if job_step(job, t0, budget_us):
			commit(job)
			job = {}
			show()
		if Time.get_ticks_usec() - t0 >= budget_us:
			return


## job_make — задание на один чанк: список ячеек покрова, которые его накрывают,
## и курсор по ним.
##
## Список собирается заранее, а не вычисляется на ходу, потому что задание
## ПРОДОЛЖАЕМОЕ: всё, что переживает границу кадра, обязано лежать в нём самом.
func job_make(key: Vector3i) -> Dictionary:
	queued.erase(key)
	var ring: Array = GRASS_RINGS[key.z]
	var x0 := float(key.x) * GRASS_CHUNK
	var y0 := float(key.y) * GRASS_CHUNK
	var side := rule.side_of(0)
	var cells := rule.samples - 1
	var cstep := side / float(cells)
	var items := []
	# Чанк посадки может лечь на несколько ярусов: 32 м делит 256 м нацело только
	# пока правило подробности такое, каким приехало. Считаем по прямоугольнику,
	# а не по делимости, — тогда смена правила ничего не сломает молча.
	for cx in range(int(floor(x0 / side)), int(floor((x0 + GRASS_CHUNK - 0.001) / side)) + 1):
		for cz in range(int(floor(y0 / side)), int(floor((y0 + GRASS_CHUNK - 0.001) / side)) + 1):
			var g: Dictionary = cover_index.get(Vector2i(cx, cz), {})
			if g.is_empty():
				continue
			var ox := float(cx) * side
			var oz := float(cz) * side
			var i0: int = maxi(0, int(floor((x0 - ox) / cstep)))
			var i1: int = mini(cells - 1, int(ceil((x0 + GRASS_CHUNK - ox) / cstep)) - 1)
			var j0: int = maxi(0, int(floor((y0 - oz) / cstep)))
			var j1: int = mini(cells - 1, int(ceil((y0 + GRASS_CHUNK - oz) / cstep)) - 1)
			for j in range(j0, j1 + 1):
				for i in range(i0, i1 + 1):
					items.append([g, i, j])
	return {
		"key": key, "step": int(ring[1]), "fill": float(ring[2]),
		"items": items, "cursor": 0, "x0": x0, "y0": y0,
		"xf": [[], [], []],
		"cl": [PackedColorArray(), PackedColorArray(), PackedColorArray()],
	}


## job_step — true, когда задание закончено. Бюджет проверяется РАЗ В ЯЧЕЙКУ
## ПОКРОВА: ближняя ячейка это около трёхсот ячеек мировой сетки, то есть
## порядка миллисекунды — достаточно мелко для бюджета в четыре и достаточно
## крупно, чтобы сам Time.get_ticks_usec не стал статьёй расхода.
func job_step(job_at: Dictionary, t0: int, budget_us: int) -> bool:
	var items: Array = job_at["items"]
	while int(job_at["cursor"]) < items.size():
		var it: Array = items[int(job_at["cursor"])]
		job_at["cursor"] = int(job_at["cursor"]) + 1
		cover_cell(job_at, it[0], int(it[1]), int(it[2]))
		if Time.get_ticks_usec() - t0 >= budget_us:
			return int(job_at["cursor"]) >= items.size()
	return true


## on_ballast — попадает ли точка под подошву балластной призмы. Маска строится
## один раз (world.gd::_build_ballast_mask), и поле держит СВОЮ ссылку на неё:
## строящееся поле перестройки проверяет по маске нового набора, а живое — по
## маске видимого мира.
func on_ballast(x: float, y: float) -> bool:
	if ballast.is_empty():
		return false
	return ballast.has(mask_key(x, y))


func mask_key(x: float, y: float) -> int:
	return int(floor(x / BALLAST_MASK_CELL)) * 100003 + int(floor(y / BALLAST_MASK_CELL))


## cover_cell — одна ячейка ПРИСЛАННОГО покрова: сеет по мировой сетке внутри
## неё.
##
## ГРАНИЦА ВЛАДЕНИЯ ПРОХОДИТ ЗДЕСЬ. Сервер отвечает, ЕСТЬ ЛИ здесь трава и
## СКОЛЬКО её — классом и сомкнутостью ячейки 4 м; клиент отвечает, из скольких
## пучков это развернуть и куда каждый встал внутри своих 22 сантиметров.
func cover_cell(job_at: Dictionary, g: Dictionary, i: int, j: int) -> void:
	var samples: int = rule.samples
	var cells := samples - 1
	var k := j * cells + i
	var cover: PackedByteArray = g["cover"]
	var packed := cover[k]
	var cls := packed >> 4
	var closure := packed & 0x0f
	if closure < GRASS_MIN_CLOSURE:
		return
	if cls == TerrainMesh.SURFACE_SAND or cls == TerrainMesh.SURFACE_BARE_SOIL:
		return
	var forest: PackedByteArray = g["forest"]
	# Ячейка со стволом травой не засевается: под елью её не видно, а пучков она
	# стоит столько же.
	if forest.size() == cells * cells / 8 and (forest[k / 8] & (1 << (k % 8))) != 0:
		return

	var side := rule.side_of(0)
	var cstep := side / float(cells)
	var rx0 := float(g["cx"]) * side + float(i) * cstep
	var ry0 := float(g["cz"]) * side + float(j) * cstep
	# ВЫСОТА — БИЛИНЕЙНО ПО ЧЕТЫРЁМ УГЛАМ ЯЧЕЙКИ, а не отсчётом её угла на всех.
	# Раньше все пучки ячейки стояли на высоте её левого верхнего отсчёта: при
	# сотне пучков на ячейку это незаметно только на ровном, а на откосе 4 м
	# ячейки дают до метра расхождения — трава висит в воздухе выше по склону и
	# тонет ниже. При двадцати пучках на м² такая ступенька читается сразу.
	var heights: PackedFloat32Array = g["heights"]
	var base_z: float = g["base_z"]
	var h00 := base_z + float(heights[j * samples + i]) * 0.01
	var h10 := base_z + float(heights[j * samples + i + 1]) * 0.01
	var h01 := base_z + float(heights[(j + 1) * samples + i]) * 0.01
	var h11 := base_z + float(heights[(j + 1) * samples + i + 1]) * 0.01

	var step: int = job_at["step"]
	var fill: float = job_at["fill"]
	var cell := GRASS_BASE * float(step)
	var xf: Array = job_at["xf"]
	var cl: Array = job_at["cl"]
	# ЯЧЕЙКА ПРИНАДЛЕЖИТ ЧАНКУ ПО СВОЕЙ ОПОРНОЙ ТОЧКЕ ii*cell — владение
	# однозначное, поэтому на швах чанков нет ни дублей, ни щелей. Пересечение
	# берётся с прямоугольником чанка: ячейка покрова может торчать за его край.
	var ax0: float = maxf(rx0, float(job_at["x0"]))
	var ax1: float = minf(rx0 + cstep, float(job_at["x0"]) + GRASS_CHUNK)
	var ay0: float = maxf(ry0, float(job_at["y0"]))
	var ay1: float = minf(ry0 + cstep, float(job_at["y0"]) + GRASS_CHUNK)
	var lush := float(closure) / 15.0
	# Лесная ячейка БЕЗ ствола: полог над ней всё равно есть (стволы стоят в
	# соседних), и трава под ним темнее. Ячейка СО стволом отсеяна выше.
	var under_canopy := cls == TerrainMesh.SURFACE_FOREST_CONIFER \
		or cls == TerrainMesh.SURFACE_FOREST_BROAD
	for ii in range(int(ceil(ax0 / cell)), int(ceil(ax1 / cell))):
		var bi := ii * step
		var bx := float(bi) * GRASS_BASE
		for jj in range(int(ceil(ay0 / cell)), int(ceil(ay1 / cell))):
			var bj := jj * step
			# ДВА ДЕШЁВЫХ ОТСЕВА ВПЕРЁД, и порядок здесь — это цена. Занятость
			# ячейки (кольцо) и густота (ПРИСЛАННАЯ сомкнутость) отбрасывают
			# большинство кандидатов одним хешем каждый, а полный жребий пучка
			# стоит вчетверо дороже.
			var lot3 := Vegetation.hash01(bi, bj, 3)
			if lot3 > fill:
				continue
			# КУРТИНЫ: внутри одной дернины трава редеет и густеет пятнами по 11 м.
			# ПРИСЛАННАЯ сомкнутость решает, есть ли покров вообще, а куртинность —
			# густоту внутри него (разбор — у Vegetation.TUFT_M). Здесь стояла голая
			# `lush`, то есть независимый жребий по однородной вероятности в каждой
			# ячейке 22 см: у такого рассева нет ни одного масштаба крупнее ячейки, а
			# глаз читает именно масштабы — отсюда «ровный ворс» вместо покрова.
			# Множители 0.45 и 0.75 спайковы: в прогалине куртины стоит меньше
			# половины пучков, в самой гуще — вся сомкнутость и пятая часть сверху.
			var tuft := Vegetation.tuft(bx, float(bj) * GRASS_BASE)
			if Vegetation.hash01(bi, bj, 5) > lush * (0.45 + 0.75 * tuft):
				continue
			var lot := Vegetation.tuft_lot(bi, bj)
			# Сдвиг ВНУТРИ БАЗОВОЙ ячейки 0.22 м, а не внутри ячейки кольца:
			# оттого дальняя трава и стоит в тех же точках, что ближняя.
			var x := bx + float(lot[0]) * GRASS_BASE
			var y := float(bj) * GRASS_BASE + float(lot[1]) * GRASS_BASE
			# ПО БАЛЛАСТУ ТРАВА НЕ РАСТЁТ, и запрет здесь СЧИТАН, а не выдуман:
			# подошва призмы приезжает с сервера (полуширина плюс заложение откоса
			# на её высоту), и ею же он рисует саму призму. Спайк держал тут своё
			# число 2.9 м, потому что размеров не получал вовсе. На кадре без
			# запрета пучки стояли в шпальном ящике — трава сквозь щебень.
			if on_ballast(x, y):
				continue
			var z := lerpf(
				lerpf(h00, h10, (x - rx0) / cstep),
				lerpf(h01, h11, (x - rx0) / cstep),
				(y - ry0) / cstep)
			# ПОРОДА ОТ МЕСТА И ЖРЕБИЯ РАЗОМ, и это устройство спайка: метёлки идут
			# ТОЛЬКО в куртинах, кочки — вперемешку, и оба редко, иначе они перестают
			# быть исключением. Здесь пороги стояли по одному жребию, и метёлка от
			# этого росла посреди прогалины наравне с гущей.
			var kind := 0
			if tuft > 0.60 and lot[2] < 0.30:
				kind = 2
			elif lot[2] > 0.66:
				kind = 1
			var spec: Array = Vegetation.GRASS_KINDS[kind]
			# ВЫСОТА ИДЁТ ПЯТНАМИ, А НЕ ЖРЕБИЕМ. Жребий по всему полю даёт ровный ворс
			# средней высоты — глаз читает его ковром. Куртина ведёт высоту вместе с
			# густотой: где гуще, там и выше.
			var plush: float = clampf(lush * (0.30 + 0.90 * tuft), 0.0, 1.0)
			var h: float = lerpf(float(spec[3]), float(spec[4]), plush) * lerpf(0.62, 1.45, lot[3])
			var wide: float = float(spec[5]) * lerpf(0.75, 1.35, lot[4])
			var basis := Basis(Vector3.UP, float(lot[5]) * TAU)
			# НАКЛОН: ни один пучок не стоит по отвесу, и разнобой наклонов —
			# половина того, что отличает луг от щётки. До 30°: отвесный квад
			# сверху не виден вовсе, у него нет площади в плане.
			var ta := float(lot[6]) * TAU
			basis = Basis(Vector3(cos(ta), 0.0, sin(ta)), float(lot[7]) * 0.52) * basis
			var s := h / Vegetation.GRASS_MESH_H
			basis = basis.scaled(Vector3(s * wide, s, s * wide))
			xf[kind].append(Transform3D(basis, TerrainMesh.to_godot(x, y, z)))
			# ОТТЕНОК: МЕСТО ЗАДАЁТ СЕРЕДИНУ, ЖРЕБИЙ РАЗВОДИТ СОСЕДЕЙ.
			#
			# Светлота идёт от ЗАНЯТОСТИ ячейки (lot3), а не от отдельного жребия, и
			# это спайково: то же число, что решило «пучок здесь есть», задаёт и его
			# место в тоне, поэтому редеющий край куртины ещё и светлеет.
			#
			# Сухость берётся из СОМКНУТОСТИ (редкий покров и есть выгоревший) плюс
			# крупный тон луга — тот самый, которым покрашена земля под пучком, чтобы
			# трава и земля выгорали в одних и тех же местах, а не порознь.
			var dry: float = clampf(0.70 - 0.75 * lush + 0.30 * GroundLook.meadow_wave(x, y), 0.0, 1.0)
			var col: Color = Vegetation.C_GRASS_LUSH.lerp(Vegetation.C_GRASS_PALE,
				clampf(0.28 + 0.50 * (lot3 - 0.5), 0.0, 1.0))
			col = col.lerp(Vegetation.C_GRASS_DARK, float(lot[9]) * 0.34)
			col = col.lerp(Vegetation.C_GRASS_WITHER, dry * lerpf(0.20, 0.70, float(lot[10])))
			# ПОД ПОЛОГОМ ТРАВА ТЕМНЕЕ, и уводится она к тому же подлеску, которым
			# крашена земля под ней (FOREST_TINT 0.45 — число спайка). Без этого пучок
			# на лесной ячейке светился лугом посреди тени.
			if under_canopy:
				col = col.lerp(TerrainMesh.COVER_COLOURS[cls], TerrainMesh.FOREST_TINT)
			col *= lerpf(0.92, 1.18, float(lot[11]))
			# ЛИНЕЙНЫМ: цвет экземпляра MultiMesh движок берёт как есть, ровно как
			# ARRAY_COLOR (bd recall godot-vertex-color-linear).
			cl[kind].append(col.srgb_to_linear())


## commit — готовый чанк в дерево сцены. СВОЙ MultiMesh на чанк и на породу:
## у одного большого поэкземплярного отсечения по пирамиде НЕТ, и трава уходила
## бы в отрисовку целиком даже вблизи.
func commit(job_at: Dictionary) -> void:
	var key: Vector3i = job_at["key"]
	if chunks.has(key):
		var old: Node3D = chunks[key]
		root.remove_child(old)
		old.queue_free()
	var node := Node3D.new()
	node.name = "C%d_%d_L%d" % [key.x, key.y, key.z]
	# Невидимым до show: иначе новый уровень встал бы рядом со старым, и на кадр
	# между постройкой и показом трава удвоилась бы.
	node.visible = false
	root.add_child(node)
	for k in Vegetation.GRASS_KINDS.size():
		var kx: Array[Transform3D] = []
		for t in (job_at["xf"][k] as Array):
			kx.append(t)
		multimesh.call(node, "Grass%d" % k, meshes[k], kx, mats[k], job_at["cl"][k], false)
	chunks[key] = node


## count — сколько пучков СЕЙЧАС НА ЭКРАНЕ. Считается по видимым чанкам, а не по
## всем построенным: панель обязана называть то, что нарисовано, иначе число
## врёт ровно про то, ради чего его смотрят.
##
## КОРЕНЬ ВНЕ ДЕРЕВА — ЭТО НОЛЬ, и проверка здесь куплена дефектом. Флаг visible
## у чанка говорит лишь «этот уровень выбран среди своих»; узел, чей предок не
## подключён к сцене, не рисуется независимо от него. Пока условия не было,
## панель называла 118 762 пучка при пустом кадре — то самое враньё, против
## которого и написана строка выше.
func count() -> void:
	var tufts := 0
	var shown := 0
	if root == null or not root.is_inside_tree():
		stats["grass_drawn"] = 0
		stats["grass_chunks"] = 0
		stats["grass_chunks_kept"] = chunks.size()
		return
	for key in chunks.keys():
		var node: Node3D = chunks[key]
		if not node.visible:
			continue
		shown += 1
		for mm in node.get_children():
			tufts += (mm as MultiMeshInstance3D).multimesh.instance_count
	stats["grass_drawn"] = tufts
	stats["grass_chunks"] = shown
	stats["grass_chunks_kept"] = chunks.size()
