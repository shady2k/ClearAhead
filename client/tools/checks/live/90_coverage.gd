## ПОКРЫТИЕ ЗЕМЛЁЙ — ЗАМЕР ТОГО, ЧТО ВИДНО: доля площади без земли и доля,
## накрытая больше чем одним слоем.
##
## Самая дорогая суита прогона, и стоит она последней нарочно: ей нужны все
## чанки региона, то есть весь провод. Фикстурой её не заменить — она и есть
## вопрос «привёз ли сервер землю всюду, где клиент её просит».
##
## # ЧТО ИЗМЕНИЛОСЬ 2026-08-12 (ВЕЧЕР) И ПОЧЕМУ ЗАМЕР ПРИШЛОСЬ ПЕРЕПИСАТЬ
##
## Прежде клиент резал чанк маской квадов, и «ровно один уровень в точке» было
## свойством ПОСТРОЕННОЙ ГЕОМЕТРИИ: суита брала индексы собранных мешей и
## считала слои под пробой. Теперь чанк строится целиком, все уровни лежат в
## сцене одновременно, и один из них выбирает ДВИЖОК по расстоянию от камеры
## (world.gd::_load_terrain). Считать слои по геометрии стало бессмысленно —
## их под каждой пробой столько, сколько уровней хранит сервер.
##
## Поэтому вопрос переформулирован, а не смягчён: «в точке ровно один слой»
## заменено на «при камере ЗДЕСЬ показан ровно один узел», и меряется это на
## СОБРАННЫХ мешах — их габариты (по ним движок и считает расстояние) берутся из
## ArrayMesh.get_aabb(), а не из арифметики адресов.
##
## ЦЕНА ПЕРЕПИСКИ НАЗВАНА ЧЕСТНО: правило порогов приходится ПОВТОРИТЬ здесь —
## суита не поднимает мир, у неё нет ни сцены, ни камеры. Повтор правила — это
## то, чего прежняя редакция избегала («спросить правило, согласно ли оно само с
## собой, не доказывает ничего»), и слабость эта настоящая. Не повторяются при
## этом две вещи, а они и есть суть: ГЕОМЕТРИЯ (из мешей) и НАБОР ПРИЕХАВШЕГО (из
## провода). Проверка «узлы связаны и порог стоит там, где задумано» живёт не
## здесь, а на глаз — в снимке границы уровней.
##
## # Две сетки проб, и обе нужны
##
## Вблизи пути шаг мелкий (там квад 4 м), по всему охвату — крупный: 8 км радиуса
## при шаге 10 м дали бы два миллиона проб и минуты счёта.
##
## ДЫРА И НАЛОЖЕНИЕ — РАЗНЫЕ БЕДЫ. Ноль узлов — дыра до неба, и её видно чёрным
## (или синим) провалом. Два узла и больше — z-fighting поверхностей разной
## подробности: мерцающая рябь, которая на снимке выглядит грязью.
extends "res://tools/check_suite.gd"


## Точки, из которых смотрят при замере «ровно один узел». Не одна: правило
## порогов — про РАССТОЯНИЕ, и камера в одном месте проверила бы одно кольцо.
##
## Высота 1.7 м — человек; 470 м — вид строителя (кадр 438 м даёт отступ камеры
## 470 м, замер world.gd::view); 2000 м — общий план. Сдвиг в сторону от оси
## нужен отдельной строкой: у клетки, чей центр дальше radius_of(L−1) от пути,
## подробных детей нет, и порога у её четверти не будет — а вот камера рядом с
## ней оказаться может, если игрок увёл взгляд с пути.
const CAMERA_PROBES := [
	{"note": "человек на оси", "up": 1.7, "off": 0.0},
	{"note": "строитель над станцией", "up": 330.0, "off": 330.0},
	{"note": "взгляд уведён с пути на 400 м", "up": 200.0, "off": 400.0},
	{"note": "общий план", "up": 1400.0, "off": 1400.0},
]


func run() -> void:
	var rule := await ctx.rule()
	var axis := await ctx.axis()
	var bbox := await ctx.bbox()

	var requested := 0
	var got := 0
	var empty := 0
	var hollow: Array[String] = []
	var tiles := {}
	var t0 := Time.get_ticks_usec()
	for level in range(0, rule.max_level + 1):
		for c_raw in rule.cells_for_level(axis, bbox, level):
			var a: Dictionary = c_raw
			requested += 1
			var r := await ctx.api.chunk(ctx.region, int(a["level"]), int(a["cx"]), int(a["cz"]))
			if r.failed():
				_ok("покрытие: чанк %d/%d/%d доехал" % [a["level"], a["cx"], a["cz"]], false,
					r.reason)
				continue
			if r.no_chunk():
				empty += 1
				hollow.append(ChunkRule.key_of(int(a["level"]), int(a["cx"]), int(a["cz"])))
				continue
			var dec := TerrainMesh.decode(r.blob, int(a["level"]), int(a["cx"]), int(a["cz"]), rule)
			if not dec.get("ok", false):
				_ok("покрытие: тело %d/%d/%d разобрано" % [a["level"], a["cx"], a["cz"]], false,
					String(dec.get("error", "")))
				continue
			got += 1
			tiles[ChunkRule.key_of(int(a["level"]), int(a["cx"]), int(a["cz"]))] = {
				"level": int(a["level"]), "cx": int(a["cx"]), "cz": int(a["cz"]),
				"h": dec["heights"], "base_z": r.base_z_m}
	var wall_ms := float(Time.get_ticks_usec() - t0) / 1000.0

	# ЮБКА ОБЯЗАНА ПЕРЕКРЫТЬ ХУДШУЮ ТРЕЩИНУ, и обе величины меряются здесь же.
	# Трещина — отклонение земли от прямой, проведённой через отсчёты вдвое (через
	# один) и вчетверо (через три) реже: ровно то, чем расходятся сетки соседних
	# уровней на общем шве.
	var sag2 := {}
	var sag4 := {}
	for key in tiles:
		var lv := int(tiles[key]["level"])
		sag2[lv] = maxf(float(sag2.get(lv, 0.0)), TerrainMesh.sag(tiles[key]["h"], rule.samples, 2))
		sag4[lv] = maxf(float(sag4.get(lv, 0.0)), TerrainMesh.sag(tiles[key]["h"], rule.samples, 4))
	var skirt := {}
	for lv in range(0, rule.max_level + 1):
		skirt[lv] = maxf(float(sag2.get(lv, 0.0)), float(sag2.get(lv - 1, 0.0)))

	var nodes := _build_nodes(rule, tiles, skirt)
	print("=== покрытие землёй (запрошено %d, получено %d, «чанка нет» %d, узлов %d, %.0f мс) ===" % [
		requested, got, empty, nodes.size(), wall_ms])
	var short_skirt: Array[String] = []
	for lv in range(0, rule.max_level + 1):
		print("  уровень %d: провис через один %.3f м, через три %.3f м; юбка %.3f м" % [
			lv, float(sag2.get(lv, 0.0)), float(sag4.get(lv, 0.0)), float(skirt[lv])])
		# Юбка узла уровня lv обязана перекрыть щель и со стороны подробного соседа
		# (его провис), и со стороны своей сетки.
		if float(skirt[lv]) < float(sag2.get(lv, 0.0)) - 1e-9 \
				or float(skirt[lv]) < float(sag2.get(lv - 1, 0.0)) - 1e-9:
			short_skirt.append("уровень %d" % lv)
	_ok("юбка перекрывает трещину на шве с соседом любого из двух уровней",
		short_skirt.is_empty(), str(short_skirt))
	_ok("каждая отобранная клетка либо приехала, либо честно объявлена отсутствующей",
		got + empty == requested, "получено %d, пусто %d, спрошено %d" % [got, empty, requested])
	if not hollow.is_empty():
		print("  «чанка нет» по адресам: %s" % str(hollow))

	var near := _grid(rule, nodes, tiles, axis, bbox, 10.0, rule.level0_radius_m)
	var wide := _grid(rule, nodes, tiles, axis, bbox, 128.0, rule.radius_of(rule.max_level))
	for g_raw in [near, wide]:
		var g: Dictionary = g_raw
		print("  проб %6d, шаг %5.0f м, радиус %6.0f м: без земли %.2f %%, слоёв в среднем %.2f" % [
			g["probes"], g["step"], g["radius"], 100.0 * float(g["bare_share"]), g["mean_layers"]])
		for c_raw in (g["cameras"] as Array):
			var c: Dictionary = c_raw
			print("    камера «%s»: показан ровно один узел на %.2f %% площади, ни одного на %.2f %%, "
				% [c["note"], 100.0 * float(c["one_share"]), 100.0 * float(c["none_share"])]
				+ "больше одного на %.2f %%; уровень скачком через один — %.2f %% пар соседей" % [
					100.0 * float(c["many_share"]), 100.0 * float(c["jump_share"])])
	_ok("вблизи пути земля есть всюду", float(near["bare_share"]) == 0.0,
		"без земли %.2f %% на %d пробах шага %.0f м" % [
			100.0 * float(near["bare_share"]), near["probes"], near["step"]])
	_ok("по всему охвату земля есть всюду", float(wide["bare_share"]) == 0.0,
		"без земли %.2f %% на %d пробах шага %.0f м" % [
			100.0 * float(wide["bare_share"]), wide["probes"], wide["step"]])
	for g_raw in [near, wide]:
		var g: Dictionary = g_raw
		for c_raw in (g["cameras"] as Array):
			var c: Dictionary = c_raw
			_ok("камера «%s», радиус %.0f м: в точке показан ровно один узел" % [
					c["note"], g["radius"]],
				float(c["one_share"]) == 1.0,
				"ни одного %.2f %%, больше одного %.2f %%" % [
					100.0 * float(c["none_share"]), 100.0 * float(c["many_share"])])
	_check_overlap(rule, tiles, axis, bbox)
	await _check_short_view(axis, bbox, requested)


## _build_nodes — УЗЛЫ ПОКАЗА, СОБРАННЫЕ ТАК ЖЕ, КАК ИХ СОБИРАЕТ МИР.
##
## Ключ узла — «уровень плюс клетка, которую он накрывает»: четверть клетки
## уровня L по площади в точности равна клетке уровня L−1, и другого имени у неё
## нет. Уровень 0 идёт целой клеткой — детей у него нет.
##
## Габарит берётся из СОБРАННОГО МЕША (ArrayMesh.get_aabb): именно до центра
## этого габарита движок и меряет расстояние, поэтому считать его арифметикой
## значило бы мерить не то. Юбка в габарит входит — она часть меша.
func _build_nodes(rule: ChunkRule, tiles: Dictionary, skirt: Dictionary) -> Dictionary:
	var out := {}
	for key in tiles:
		var t: Dictionary = tiles[key]
		var level := int(t["level"])
		var cx := int(t["cx"])
		var cz := int(t["cz"])
		# Сирота (родителя не приехало) не строится вовсе: у неё не было бы порога
		# ни у одного предка, и в столбце оказалось бы два всегда видимых узла.
		if level < rule.view_max_level and not tiles.has(ChunkRule.key_of(
				level + 1, cx >> 1, cz >> 1)):
			continue
		var quads: Array = [-1] if level == 0 else [0, 1, 2, 3]
		for q in quads:
			var b := TerrainMesh.build(t["h"], float(t["base_z"]), level, cx, cz, rule,
				PackedByteArray(), q, float(skirt.get(level, 0.0)))
			if not b.get("ok", false) or b["mesh"] == null:
				continue
			var begin := 0.0
			var ck: String = key
			if q >= 0:
				ck = ChunkRule.key_of(level - 1, cx * 2 + (q & 1), cz * 2 + ((q >> 1) & 1))
				if tiles.has(ck):
					begin = rule.radius_of(level - 1)
			out["%d|%s" % [level, ck]] = {
				"level": level, "begin": begin,
				"centre": (b["mesh"] as ArrayMesh).get_aabb().get_center()}
	return out


## _visible_level — какой уровень движок показал бы в точке при камере в cam.
##
## −1 значит «ни одного узла над этой точкой»: земли нет. Правило — то самое, из
## которого собран показ: узел уровня L виден ⟺ ¬A_L и A_k для всех k > L, где
## A_L = «камера ближе begin_L к центру узла». Отсюда виден ровно тот, у которого
## L = max{L : ¬A_L}, и такой всегда один — у самого подробного узла столбца
## begin = 0.
##
## Возвращается ПАРА: выбранный уровень и сколько узлов оказалось видимыми. Второе
## по построению всегда 0 или 1, и считается оно нарочно перебором, а не выводом:
## доказательство проверяется счётом, иначе оно проверяет само себя.
func _visible_level(rule: ChunkRule, nodes: Dictionary, x: float, z: float,
		cam: Vector3) -> Array:
	var chain: Array = []
	for level in range(0, rule.view_max_level + 1):
		var side := rule.side_of(maxi(level - 1, 0))
		var k := "%d|%s" % [level, ChunkRule.key_of(maxi(level - 1, 0),
			int(floor(x / side)), int(floor(z / side)))]
		chain.append(nodes.get(k, null))
	var seen := 0
	var chosen := -1
	for level in range(0, rule.view_max_level + 1):
		var n: Variant = chain[level]
		if n == null:
			continue
		var hidden := cam.distance_to(n["centre"]) < float(n["begin"])
		if hidden:
			continue
		var above_all_hidden := true
		for up in range(level + 1, rule.view_max_level + 1):
			var m: Variant = chain[up]
			if m == null:
				continue
			if cam.distance_to(m["centre"]) >= float(m["begin"]):
				above_all_hidden = false
				break
		if above_all_hidden:
			seen += 1
			chosen = level
	return [chosen, seen, chain]


## _grid — сколько уровней лежит под пробой и сколько из них ПОКАЗАНО.
##
## Проба берётся только внутри названного радиуса от оси: снаружи земли нет по
## правилу, и считать её отсутствие дырой значило бы мерить край мира.
func _grid(rule: ChunkRule, nodes: Dictionary, tiles: Dictionary, axis: PackedVector2Array,
		bbox: Rect2, step: float, radius: float) -> Dictionary:
	var x0 := bbox.position.x - radius
	var x1 := bbox.end.x + radius
	var z0 := bbox.position.y - radius
	var z1 := bbox.end.y + radius
	var cams: Array = []
	for c_raw in CAMERA_PROBES:
		var c: Dictionary = c_raw
		cams.append({"note": c["note"], "one": 0, "none": 0, "many": 0,
			"jumps": 0, "pairs": 0,
			"pos": TerrainMesh.to_godot(
				bbox.position.x + bbox.size.x * 0.5,
				bbox.position.y + bbox.size.y * 0.5 + float(c["off"]),
				float(c["up"]))})
	var probes := 0
	var bare := 0
	var layers_total := 0
	var x := x0
	while x <= x1:
		var z := z0
		# prev — уровень предыдущей пробы того же столбца по каждой камере: по нему
		# считаются скачки подробности через уровень.
		var prev: Array = []
		for c in cams:
			prev.append(-99)
		while z <= z1:
			if ChunkRule.nearest_axis_dist(axis, Vector2(x, z)) <= radius:
				probes += 1
				var layers := 0
				for level in range(0, rule.view_max_level + 1):
					var side := rule.side_of(level)
					if tiles.has(ChunkRule.key_of(level, int(floor(x / side)), int(floor(z / side)))):
						layers += 1
				layers_total += layers
				if layers == 0:
					bare += 1
				for ci in cams.size():
					var c: Dictionary = cams[ci]
					var v: Array = _visible_level(rule, nodes, x, z, c["pos"])
					if int(v[1]) == 1:
						c["one"] = int(c["one"]) + 1
					elif int(v[1]) == 0:
						c["none"] = int(c["none"]) + 1
					else:
						c["many"] = int(c["many"]) + 1
					if int(prev[ci]) > -99 and int(v[0]) >= 0:
						c["pairs"] = int(c["pairs"]) + 1
						if absi(int(v[0]) - int(prev[ci])) >= 2:
							c["jumps"] = int(c["jumps"]) + 1
					prev[ci] = int(v[0])
			else:
				for ci in cams.size():
					prev[ci] = -99
			z += step
		x += step
	var denom := maxf(1.0, float(probes))
	var out_cams: Array = []
	for c_raw in cams:
		var c: Dictionary = c_raw
		out_cams.append({"note": c["note"],
			"one_share": float(c["one"]) / denom,
			"none_share": float(c["none"]) / denom,
			"many_share": float(c["many"]) / denom,
			"jump_share": float(c["jumps"]) / maxf(1.0, float(c["pairs"]))})
	return {"probes": probes, "step": step, "radius": radius,
		"bare_share": float(bare) / denom,
		"mean_layers": float(layers_total) / denom,
		"cameras": out_cams}


## _check_overlap — НА СКОЛЬКО РАСХОДЯТСЯ ПОВЕРХНОСТИ ДВУХ УРОВНЕЙ ВНУТРИ
## ПЕРЕКРЫТИЯ. Это НЕ трещина и не то же число, что sag.
##
## Трещина живёт на шве и меряется вдоль линии сетки (sag); её закрывает юбка.
## Здесь меряется другое: насколько отметка уровня L отличается от отметки уровня
## L+1 в ОДНОЙ И ТОЙ ЖЕ точке площади. Это высота, на которую подпрыгнет земля
## под неподвижным деревом, когда камера отъедет и уровень сменится, — дерево
## посажено якорем и своего уровня показа не имеет.
##
## Прежняя редакция мерила то же самое, но только в узкой полосе вокруг
## окружности radius_of(L): там и только там проходил шов при выборе уровня по
## расстоянию ДО ПУТИ. Теперь шов проходит там, где его застала камера, поэтому
## полоса расширена на всё перекрытие.
func _check_overlap(rule: ChunkRule, tiles: Dictionary, axis: PackedVector2Array,
		bbox: Rect2) -> void:
	for level in range(0, rule.max_level):
		var r := rule.radius_of(level)
		var step := maxf(8.0, r / 96.0)
		var probes := 0
		var total := 0.0
		var worst := 0.0
		var x := bbox.position.x - r
		while x <= bbox.end.x + r:
			var z := bbox.position.y - r
			while z <= bbox.end.y + r:
				if ChunkRule.nearest_axis_dist(axis, Vector2(x, z)) <= r:
					var fine := _sample_height(rule, tiles, level, x, z)
					var coarse := _sample_height(rule, tiles, level + 1, x, z)
					if fine < INF and coarse < INF:
						probes += 1
						var d := absf(fine - coarse)
						total += d
						worst = maxf(worst, d)
				z += step
			x += step
		print("  перекрытие %d/%d внутри %6.0f м от оси: проб %5d, расхождение в среднем %.3f м, худшее %.3f м" % [
			level, level + 1, r, probes, total / maxf(1.0, float(probes)), worst])
		# БЛИЖНЕЕ ПЕРЕКРЫТИЕ ПРОВЕРЯЕТСЯ, дальние только меряются, и разница не в
		# строгости, а в том, что видно. Отсчёты грубой сетки совпадают в плане с
		# каждым вторым отсчётом подробной, поэтому расхождение рождается только
		# интерполяцией между ними. Порог 0.10 м — тридцатикратный запас к замеру
		# 2026-08-12 (0.003 м на шве 0/1); сработает он не на качании интерполяции,
		# а на расхождении САМИХ отсчётов между уровнями, то есть на том, что игрок
		# увидел бы прыжком земли под ногами.
		if level == 0:
			_ok("на ближнем перекрытии уровни дают одну и ту же землю", worst < 0.10,
				"худшее расхождение %.3f м внутри %.0f м от оси" % [worst, r])


## _sample_height — отметка земли уровня level в точке, билинейно по отсчётам.
## INF значит «этого чанка у клиента нет».
func _sample_height(rule: ChunkRule, tiles: Dictionary, level: int, x: float, z: float) -> float:
	var side := rule.side_of(level)
	var cx := int(floor(x / side))
	var cz := int(floor(z / side))
	var key := ChunkRule.key_of(level, cx, cz)
	if not tiles.has(key):
		return INF
	var rec: Dictionary = tiles[key]
	var h: PackedFloat32Array = rec["h"]
	var n := rule.samples
	var st := rule.step_of(level)
	var fx := (x - float(cx) * side) / st
	var fz := (z - float(cz) * side) / st
	var i := clampi(int(floor(fx)), 0, n - 2)
	var j := clampi(int(floor(fz)), 0, n - 2)
	var tx := clampf(fx - float(i), 0.0, 1.0)
	var tz := clampf(fz - float(j), 0.0, 1.0)
	var a := lerpf(h[j * n + i], h[j * n + i + 1], tx)
	var b := lerpf(h[(j + 1) * n + i], h[(j + 1) * n + i + 1], tx)
	return float(rec["base_z"]) + lerpf(a, b, tz) * 0.01


## СОКРАЩЁННЫЙ ВЗГЛЯД — ЧЕСТНО ЛИ КОНЧАЕТСЯ МИР.
##
## Клиент решает сам, как далеко смотреть (ChunkRule.set_view_reach), и уровни
## выше нужного не спрашивает вовсе. Арифметику этого проверяет checks/pure;
## здесь проверяется то, чего арифметикой не докажешь: что ВНУТРИ круга взгляда
## земля есть вся, хотя дальних уровней не спрашивали, — то есть мир кончается
## КРАЕМ, а не рвётся дырами у обрыва.
##
## Взгляд берётся В ДВА КОЛЬЦА (radius_of(1)), а не произвольным числом метров:
## одного кольца мало — при единственном уровне перекрытию неоткуда взяться, и
## половина замера стала бы бессодержательной. Число приходит из манифеста.
func _check_short_view(axis: PackedVector2Array, bbox: Rect2, full_requested: int) -> void:
	# Правило СВОЁ, а не ctx.rule(): дальность взгляда — изменяемое поле общего
	# правила прогона, и подкрутивший его файл менял бы соседние молча.
	var man := await ctx.manifest()
	var rule := ChunkRule.from_manifest(man.get("chunks", {}) as Dictionary)
	if rule.max_level < 2:
		_ok("у мира есть что сокращать: уровней больше двух", false,
			"манифест объявил %d — сокращать нечего, замер не про этот мир" % (rule.max_level + 1))
		return
	var view: Dictionary = rule.set_view_reach(rule.radius_of(1))
	_ok("сокращённый взгляд короче потолка", rule.view_max_level < rule.max_level,
		"видно на %.0f м из %.0f м" % [rule.view_reach_m(), rule.ceiling_reach_m()])

	var tiles := {}
	var requested := 0
	var empty := 0
	var above: Array[String] = []
	var t0 := Time.get_ticks_usec()
	for level in range(0, rule.view_max_level + 1):
		for c_raw in rule.cells_for_level(axis, bbox, level):
			var a: Dictionary = c_raw
			requested += 1
			if int(a["level"]) > rule.view_max_level:
				above.append(ChunkRule.key_of(int(a["level"]), int(a["cx"]), int(a["cz"])))
			var r := await ctx.api.chunk(ctx.region, int(a["level"]), int(a["cx"]), int(a["cz"]))
			if r.failed():
				_ok("сокращённый взгляд: чанк %d/%d/%d доехал" % [a["level"], a["cx"], a["cz"]],
					false, r.reason)
				continue
			if r.no_chunk():
				empty += 1
				continue
			var dec := TerrainMesh.decode(r.blob, int(a["level"]), int(a["cx"]), int(a["cz"]), rule)
			if not dec.get("ok", false):
				_ok("сокращённый взгляд: тело %d/%d/%d разобрано" % [a["level"], a["cx"], a["cz"]],
					false, String(dec.get("error", "")))
				continue
			tiles[ChunkRule.key_of(int(a["level"]), int(a["cx"]), int(a["cz"]))] = {
				"level": int(a["level"]), "cx": int(a["cx"]), "cz": int(a["cz"]),
				"h": dec["heights"], "base_z": r.base_z_m}
	var wall_ms := float(Time.get_ticks_usec() - t0) / 1000.0

	# ЭКОНОМИЯ — ЧИСЛОМ АДРЕСОВ НА ЖИВОМ ПРОВОДЕ, а не секундомером: уровни выше
	# спрошенного не появляются в списке вовсе.
	_ok("уровней выше спрошенного не запрошено ни одного", above.is_empty(), str(above))
	_ok("сокращённый взгляд стоит меньше адресов", requested < full_requested,
		"%d против %d" % [requested, full_requested])

	var nodes := _build_nodes(rule, tiles, {})
	var near := _grid(rule, nodes, tiles, axis, bbox, 10.0, rule.level0_radius_m)
	var edge := _grid(rule, nodes, tiles, axis, bbox, 24.0, rule.view_reach_m())
	print("=== сокращённый взгляд (%.0f м из %.0f м: запрошено %d, «чанка нет» %d, %.0f мс) ===" % [
		rule.view_reach_m(), rule.ceiling_reach_m(), requested, empty, wall_ms])
	for g_raw in [near, edge]:
		var g: Dictionary = g_raw
		print("  проб %6d, шаг %5.0f м, радиус %6.0f м: без земли %.2f %%, слоёв в среднем %.2f" % [
			g["probes"], g["step"], g["radius"], 100.0 * float(g["bare_share"]), g["mean_layers"]])
	_ok("внутри сокращённого взгляда земля есть всюду", float(edge["bare_share"]) == 0.0,
		"без земли %.2f %% на %d пробах шага %.0f м" % [
			100.0 * float(edge["bare_share"]), edge["probes"], edge["step"]])
	# Вблизи пути подробность НЕ ПАДАЕТ от того, что смотрят ближе: уровень 0
	# спрашивается тот же самый.
	_ok("вблизи пути сокращение взгляда ничего не отняло", float(near["bare_share"]) == 0.0,
		"без земли %.2f %% на %d пробах шага %.0f м" % [
			100.0 * float(near["bare_share"]), near["probes"], near["step"]])
	for c_raw in (edge["cameras"] as Array):
		var c: Dictionary = c_raw
		_ok("сокращённый взгляд, камера «%s»: в точке показан ровно один узел" % c["note"],
			float(c["one_share"]) == 1.0,
			"ни одного %.2f %%, больше одного %.2f %%" % [
				100.0 * float(c["none_share"]), 100.0 * float(c["many_share"])])
	# Урезание потолком проверяется тут же и без второго прохода по сети:
	# просьба вдвое дальше засеянного обязана дать ровно засеянное.
	var over: Dictionary = rule.set_view_reach(rule.ceiling_reach_m() * 2.0)
	_ok("просьба вдвое дальше засеянного упирается в потолок",
		bool(over["capped"]) and float(over["reach_m"]) == rule.ceiling_reach_m(),
		"видно %.0f м, хранится %.0f м" % [float(over["reach_m"]), rule.ceiling_reach_m()])
	# Правило возвращается на потолок: объект местный, но заблуждаться о нём
	# следующему читателю незачем — «что осталось в поле после проверки» уже
	# однажды стоило проекту разбора.
	rule.set_view_reach(ChunkRule.REACH_ALL)
	_ok("сокращённый взгляд не запросил ни одного уровня сверх потолка",
		int(view["levels"]) <= rule.max_level + 1,
		"уровней %d при потолке %d" % [int(view["levels"]), rule.max_level + 1])
