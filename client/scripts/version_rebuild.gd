## version_rebuild.gd — ПЕРЕСТРОЙКА НАБОРА ПОД БЮДЖЕТОМ КАДРА (sqym.7).
##
## Набор версии собран (сеть + патчи всех клеток в памяти, sqym.6) — теперь его
## надо ПЕРЕСТРОИТЬ: меши земли, коллизии, растительность, траву. Раньше это
## делал ОДИН синхронный проход (_apply_version_set), и кадр на нём замирал
## (замер W5-A: 281–288 мс на одной пересадке травы). Теперь перестройка идёт
## В СТОРОНЕ, кусочками по кадрам, под объявленным бюджетом, и видимым набор
## становится одним commit() — пока он не готов, на экране старая версия
## целиком, и полунабора не бывает.
##
## Сцены перестройка не касается: всё, что она строит, живёт в её собственных
## словарях и отсоединённых узлах. Сцену трогает только commit() — и это ровно
## тот шаг, который обязан быть дешёвым: тяжёлое уже сделано.
##
## Статьи бюджета, каждая со своим замером в отчёте:
##   track — путь, ось и маска балласта (один шаг: сеть мала);
##   terrain — ArrayMesh каждой клетки уровня с юбками (ПОЛОСА по строкам
##     сетки на задание: кусок целиком 11.4 мс замером — толще бюджета в 8 мс,
##     полоса делит его; сборка полос и юбки — последним заданием куска);
##   vegetation — лес и кусты (по ярусу-якорю на задание + сборка MultiMesh);
##   grass — круг травы нового набора (поле GrassField, своя очередь);
##   solid — StaticBody3D + CollisionShape3D по ПОЛОСАМ (полоса на задание:
##     фигура из треугольников полосы, тело куска собирается из них и
##     переключается вместе с мешем одним commit());
##
## Родительский и дочерний уровень трогаются оба: юбка узла уровня L равна
## максимуму провиса уровней L и L−1 (TerrainMesh.sag), значит патч уровня L
## перекраивает юбки уровня L+1, а патч уровня L−1 — юбки уровня L. Перестройка
## поэтому заказывает меши для уровней с новыми высотами И для уровня над ними.
class_name VersionRebuild
extends RefCounted

const RebuildQueueScript := preload("res://scripts/rebuild_queue.gd")
const GrassFieldScript := preload("res://scripts/grass_field.gd")

## TERRAIN_BAND_ROWS — рядов сетки в одной полосе меша и коллизии (sqym.16).
##
## ЗАМЕР: меш уровня 0 целиком — 11.4 мс, коллизия — 11.0 мс, бюджет кадра
## 8 мс: шаг обязан быть делимым, иначе очередь честно назовёт превышение, но
## раздробить его не сможет. Полоса в 8 рядов — восьмая часть чанка, ~1.4 мс
## на полосу (коллизия — теми же полосами), то есть впятеро тоньше бюджета.
## Мельче резать — плодить задания ради плодовитости; крупнее — подойти к
## бюджету вплотную на машине вдвое медленнее замера.
const TERRAIN_BAND_ROWS := 8



## services — что мир даёт перестройке. Всё, что трогает сцену, приходит
## Callable'ами от мира; данные (tiles, tile_nodes) — ЖИВЫМИ ССЫЛКАМИ на словари
## мира, но читаются они только на commit() и в подготовке, а строящееся поле
## держит свои копии высот.
var services := {}

var _set := {}                  # набор версии: {version, network, heights, clean}
var _elements: Array = []
var _q := RebuildQueueScript.new()
var _phase := "track"           # track -> terrain -> vegetation -> grass -> solid -> done
var _staging := {}              # результаты подготовки (см. begin)
var _veg_state := {}            # накопленные экземпляры растительности
var _results := {}              # ключ клетки -> {q: ArrayMesh}
var _solid_faces := {}          # ключ клетки -> {q: [PackedVector3Array]} — треугольники полос
var _bodies := {}               # ключ клетки -> {q: StaticBody3D}
var _over: Array[String] = []   # превышения бюджета за всю перестройку
var _costs := {}                # статья -> мкс
var _failed_reason := ""


## begin — начать перестройку набора. Сразу считается всё, что дёшево и нужно
## любому заданию: высоты набора, провисы и юбки уровней, список клеток и якорей.
func begin(set: Dictionary, elements: Array) -> void:
	_set = set
	_elements = elements
	var rule: ChunkRule = services["rule"]
	var tiles: Dictionary = services["tiles"]
	# Высоты набора отдельно от живых: строящееся поле не трогает _tiles ни
	# одним чтением, которое изменило бы видимый мир.
	var staging_heights := {}
	var sag2 := {}
	var sag4 := {}
	for key in set["heights"]:
		var h: Dictionary = set["heights"][key]
		staging_heights[key] = {"h": h["h"], "base_z": h["base_z"]}
		var lv := int((tiles[key] as Dictionary)["level"])
		sag2[lv] = maxf(float(sag2.get(lv, 0.0)), TerrainMesh.sag(h["h"], rule.samples, 2))
		sag4[lv] = maxf(float(sag4.get(lv, 0.0)), TerrainMesh.sag(h["h"], rule.samples, 4))
	var skirt_of := {}
	for lv in range(0, rule.view_max_level + 1):
		skirt_of[lv] = maxf(float(sag2.get(lv, 0.0)), float(sag2.get(lv - 1, 0.0)))
	# Уровни перестройки: уровни с новыми высотами ПЛЮС уровень над ними —
	# юбка уровня L+1 считается по провису уровня L, и патч перекраивает обе.
	var changed := {}
	for key in staging_heights:
		changed[int((tiles[key] as Dictionary)["level"])] = true
	var rebuild_levels := {}
	for lv in changed:
		rebuild_levels[lv] = true
		if lv + 1 <= rule.view_max_level:
			rebuild_levels[lv + 1] = true
	var rebuild_keys := {}
	for key in services["tile_nodes"]:
		if rebuild_levels.has(int((tiles[key] as Dictionary)["level"])):
			rebuild_keys[key] = true
	_staging = {
		"heights": staging_heights, "skirt": skirt_of, "sag2": sag2, "sag4": sag4,
		"rebuild_keys": rebuild_keys,
		"ground": assemble_ground(tiles, services["tile_nodes"], rule, staging_heights),
		"ballast": {}, "track": null, "veg": null, "grass": null,
	}
	_phase = "track"
	_q.clear()
	_q.push("track", Callable(self, "_step_track"), 0.0, {})
	_results = {}
	_solid_faces = {}
	_bodies = {}
	_over = []
	_costs = {}
	_failed_reason = ""


## failed — перестройка наткнулась на отказ (меш не собрался, коллизия не
## создалась). Видимой остаётся старая версия; мир показывает причину.
func failed() -> bool:
	return _failed_reason != ""


## failed_reason — причина отказа, если перестройка упала.
func failed_reason() -> String:
	return _failed_reason


## source_set — набор, который перестраивается. commit() мира берёт его отсюда,
## а не перечитывает у накопителя: у набора один момент фиксации (begin), дальше
## он живёт в перестройке.
func source_set() -> Dictionary:
	return _set


## pending — сколько работы осталось (включая траву и фазы впереди).
func pending() -> int:
	if _phase == "done":
		return 0
	if _phase == "grass":
		return (_staging["grass"] as GrassFieldScript).pending()
	return _q.pending()


## reprioritize — камера двинулась: работа для клетки за спиной уступает
## видимой. Пересчёт по расстоянию от фокуса до ЦЕНТРА клетки задания.
func reprioritize(focus: Vector2) -> void:
	if _phase == "done":
		return
	var rule: ChunkRule = services["rule"]
	_q.reprioritize_all(func(job):
		var st: Dictionary = job["state"]
		if not st.has("cx"):
			return 1e18
		var side := rule.side_of(int(st["level"]))
		var dx := (float(int(st["cx"])) + 0.5) * side - focus.x
		var dz := (float(int(st["cz"])) + 0.5) * side - focus.y
		return dx * dx + dz * dz)


## tick — один кадр перестройки: работа в пределах бюджета, фазы по порядку.
##
## Возвращает {phase, pending, over_budget, failed, done}. Превышение бюджета
## копится за всю перестройку: одно грубое задание в одном кадре — это число в
## отчёте, а не молчаливый проступок.
func tick(budget_us: int) -> Dictionary:
	var t0 := Time.get_ticks_usec()
	while not failed():
		var remaining := budget_us - int(Time.get_ticks_usec() - t0)
		if remaining <= 0:
			break
		match _phase:
			"track":
				_absorb(_q.work(remaining))
				if _q.pending() == 0:
					_phase = "terrain"
					_fill_terrain()
					continue
			"terrain":
				_absorb(_q.work(remaining))
				if _q.pending() == 0:
					_phase = "vegetation"
					_fill_vegetation()
					continue
			"vegetation":
				_absorb(_q.work(remaining))
				if _q.pending() == 0:
					_phase = "grass"
					continue
			"grass":
				var g: GrassFieldScript = _staging["grass"]
				g.tick(remaining)
				if g.pending() == 0:
					_phase = "solid"
					_fill_solid()
					continue
			"solid":
				_absorb(_q.work(remaining))
				if _q.pending() == 0:
					_phase = "done"
					continue
			"done":
				break
	return {
		"phase": _phase, "pending": pending(), "over_budget": _over.duplicate(),
		"failed": failed(), "done": _phase == "done",
	}


func _absorb(res: Dictionary) -> void:
	for id in (res["over_budget"] as Array):
		_over.append(String(id))


## --- ФАЗА TRACK --------------------------------------------------------------

## _step_track — путь, ось и маска балласта ОДНИМ заданием. Сеть мала (десятки
## элементов), и резать её мельче значило бы плодить задания ради плодовитости;
## если шаг превысит бюджет, очередь назовёт его, и статью разрежут.
func _step_track(_budget_us: int, _t0: int, _state: Dictionary) -> bool:
	var t0 := Time.get_ticks_usec()
	var track := Node3D.new()
	track.name = "Track"
	var res: Dictionary = services["track_builder"].call(_elements, _set["network"], track)
	_staging["track"] = track
	_staging["axis"] = res["axis"]
	_staging["ballast"] = ballast_mask(res["axis"], float(res["toe"]))
	_costs["track_us"] = int(Time.get_ticks_usec() - t0)
	# Трава и кусты нового набора проверяют балласт по НОВОЙ маске: поле травы
	# заводится здесь, с землёй и маской набора.
	var gf = services["grass_new"].call(_staging["ground"], _staging["ballast"])
	_staging["grass"] = gf
	return true


## --- ФАЗА TERRAIN ------------------------------------------------------------
func _fill_terrain() -> void:
	var rule: ChunkRule = services["rule"]
	var tiles: Dictionary = services["tiles"]
	_q.clear()
	var p := 0.0
	for key in _staging["rebuild_keys"]:
		var t: Dictionary = tiles[key]
		var h: PackedFloat32Array = t["h"]
		var base_z: float = t["base_z"]
		if (_staging["heights"] as Dictionary).has(key):
			h = (_staging["heights"] as Dictionary)[key]["h"]
			base_z = (_staging["heights"] as Dictionary)[key]["base_z"]
		var level := int(t["level"])
		var skirt := float((_staging["skirt"] as Dictionary)[level])
		for n_raw in (services["tile_nodes"][key] as Array):
			var n: Dictionary = n_raw
			var q := int(n["q"])
			if q >= 0 and (rule.samples - 1) % 2 != 0:
				_fail("чанк %s: samples−1 = %d нечётно, четверть невыразима" % [key, rule.samples - 1])
				return
			# ОДНО задание на кусок с КУРСОРОМ, а не задание на полосу: шаг
			# строит одну полосу за вызов и возвращает false, пока курсор не
			# дойдёт до конца, — тогда собирает меш и завершается. Порядок
			# полос задаёт курсор, а не приоритеты: переупорядочивание очереди
			# (reprioritize_all, нестабильная sort_custom) не может поставить
			# сборку раньше полос, и общий ряд между полосами (одна строка
			# вершин в накопителе) не поедет.
			var acc := TerrainMesh.new_acc(rule.samples, q)
			_q.push("t:%s:q%d" % [key, q], Callable(self, "_step_terrain"), p, {
				"key": key, "q": q, "h": h, "base_z": base_z,
				"level": level, "cx": int(t["cx"]), "cz": int(t["cz"]),
				"cover": t["cover"], "skirt": skirt, "acc": acc,
				"j": int(acc["j0"]), "j1": int(acc["j1"]),
			})
			p += 1.0


## _step_terrain — ОДИН продолжаемый шаг куска: полоса за вызов (sqym.16).
##
## Меш уровня 0 целиком стоит 11.4 мс замером, бюджет кадра 8 мс — шаг обязан
## быть делимым. Полоса в TERRAIN_BAND_ROWS рядов — восьмая часть чанка,
## ~1.4 мс; очередь зовёт шаг снова, пока не выйдет бюджет, и курсор (j)
## продолжается со следующего кадра. Дошёл до конца — собрать меш и юбку.
func _step_terrain(_budget_us: int, _t0: int, state: Dictionary) -> bool:
	if int(state["j"]) < int(state["j1"]):
		var j_hi := mini(int(state["j"]) + TERRAIN_BAND_ROWS, int(state["j1"]))
		var rule: ChunkRule = services["rule"]
		var res: Dictionary = TerrainMesh.build_band(state["h"], float(state["base_z"]),
			int(state["level"]), int(state["cx"]), int(state["cz"]), rule,
			state["cover"], int(state["q"]), int(state["j"]), j_hi, state["acc"])
		if not res["ok"]:
			_fail(String(res["error"]))
			return true
		state["j"] = j_hi
		return false
	# Курсор на конце: полосы готовы, собрать меш. Треугольники полос (faces)
	# уходят в _solid_faces — фаза solid соберёт твердь ТЕМИ ЖЕ полосами.
	var built: Dictionary = TerrainMesh.assemble(state["acc"], float(state["skirt"]))
	if not built["ok"]:
		_fail(String(built["error"]))
		return true
	var mesh: ArrayMesh = built["mesh"]
	if not _results.has(state["key"]):
		_results[state["key"]] = {}
	(_results[state["key"]] as Dictionary)[state["q"]] = mesh
	if mesh != null:
		if not _solid_faces.has(state["key"]):
			_solid_faces[state["key"]] = {}
		(_solid_faces[state["key"]] as Dictionary)[state["q"]] = built["faces"]
	return true


## --- ФАЗА VEGETATION ---------------------------------------------------------

## _fill_vegetation — по ярусу-якорю на задание: земля набора, маска набора.
## Экземпляры копятся в _veg_state, сборка MultiMesh — последним заданием.
func _fill_vegetation() -> void:
	var st: Dictionary = _staging
	var veg := Node3D.new()
	veg.name = "Vegetation"
	st["veg"] = veg
	_veg_state = _new_veg_state()
	_q.clear()
	var p := 0.0
	for g_raw in st["ground"]:
		var g: Dictionary = g_raw
		_q.push("v:%d/%d" % [int(g["cx"]), int(g["cz"])],
			Callable(self, "_step_veg_tile"), p,
			{"g": g, "cx": int(g["cx"]), "cz": int(g["cz"]), "level": int(g["level"])})
		p += 1.0
	_q.push("v:finalize", Callable(self, "_step_veg_final"), p, {})


func _step_veg_tile(_budget_us: int, _t0: int, state: Dictionary) -> bool:
	services["veg_tile"].call(_veg_state, state["g"], _staging["ballast"])
	return true


func _step_veg_final(_budget_us: int, _t0: int, _state: Dictionary) -> bool:
	var t0 := Time.get_ticks_usec()
	services["veg_finalize"].call(_veg_state, _staging["veg"], services["rule"])
	_costs["vegetation_us"] = int(Time.get_ticks_usec() - t0)
	return true


func _new_veg_state() -> Dictionary:
	return {
		"spruce_narrow": [] as Array[Transform3D], "spruce_wide": [] as Array[Transform3D],
		"broad": [] as Array[Transform3D], "bushes": [] as Array[Transform3D],
		"bushes_low": [] as Array[Transform3D],
		"spruce_narrow_c": PackedColorArray(), "spruce_wide_c": PackedColorArray(),
		"broad_c": PackedColorArray(), "bushes_c": PackedColorArray(),
		"bushes_low_c": PackedColorArray(),
	}


## --- ФАЗА SOLID --------------------------------------------------------------

## _fill_solid — коллизии якорей, у которых СМЕНИЛСЯ меш, ПОЛОСАМИ (sqym.16):
## по заданию на фигуру полосы. Тело куска (StaticBody3D) живёт в общем
## состоянии полос и собирается из CollisionShape3D по одной за задание —
## коллизия стоит 11.0 мс замером, и одна фигура на весь кусок снова была бы
## неделимым шагом толще бюджета. В _bodies тело попадает ОТДЕЛЬНЫМ последним
## заданием: только ГОТОВЫЕ тела, и переключаются они вместе с мешем одним
## commit() — пока набор в стороне, ни одна фигура в сцене не появляется.
func _fill_solid() -> void:
	_q.clear()
	var p := 0.0
	for key in _solid_faces:
		var t: Dictionary = services["tiles"][key]
		for n_raw in (services["tile_nodes"][key] as Array):
			var n: Dictionary = n_raw
			if float(n["begin"]) != 0.0:
				continue
			var q := int(n["q"])
			if not (_solid_faces[key] as Dictionary).has(q):
				continue
			# ОДНО задание на кусок с курсором (fi): вызов создаёт ОДНУ фигуру
			# полосы и возвращает false, пока полосы не кончатся, — тогда тело
			# отдаётся в _bodies и задание завершается. Коллизия целиком стоит
			# 11.0 мс замером, фигура полосы ~1.4 мс: шаг делится тем же
			# резаком, что и меш, а в _bodies попадает только ГОТОВОЕ тело.
			_q.push("s:%s:q%d" % [key, q], Callable(self, "_step_solid"), p, {
				"key": key, "q": q, "level": int(t["level"]),
				"cx": int(t["cx"]), "cz": int(t["cz"]),
				"faces": (_solid_faces[key] as Dictionary)[q],
				"fi": 0, "body": null,
			})
			p += 1.0


## _step_solid — ОДИН продолжаемый шаг куска: фигура полосы за вызов (sqym.16).
## Тело собирается ОТСОЕДИНЁННЫМ: у якоря в сцене коллизия нового набора
## появилась бы раньше переключения — игрок упирался бы в будущее. В _bodies
## тело попадает только когда все полосы собраны, и commit() переключает его
## вместе с мешем — полунабора на экране не бывает.
func _step_solid(_budget_us: int, _t0: int, state: Dictionary) -> bool:
	var faces: Array = state["faces"]
	if int(state["fi"]) < faces.size():
		var t0 := Time.get_ticks_usec()
		var band_faces: PackedVector3Array = faces[int(state["fi"])]
		if not band_faces.is_empty():
			var body: StaticBody3D = state["body"]
			if body == null:
				body = StaticBody3D.new()
				state["body"] = body
			var shape := ConcavePolygonShape3D.new()
			shape.set_faces(band_faces)
			var cs := CollisionShape3D.new()
			cs.shape = shape
			body.add_child(cs)
			_costs["solid_us"] = int(_costs.get("solid_us", 0)) + int(Time.get_ticks_usec() - t0)
		state["fi"] = int(state["fi"]) + 1
		return false
	if state["body"] != null:
		if not _bodies.has(state["key"]):
			_bodies[state["key"]] = {}
		(_bodies[state["key"]] as Dictionary)[state["q"]] = state["body"]
	return true

## --- COMMIT: ЕДИНСТВЕННЫЙ ШАГ, КОТОРЫЙ ТРОГАЕТ СЦЕНУ --------------------------

## commit — показать набор ЦЕЛИКОМ: снять старое, поставить новое, одним
## проходом между двумя кадрами. Всё тяжёлое уже сделано, здесь только
## назначение ссылок и перестановка узлов — потому переключение и не замирает.
##
## Возвращает {costs, ground, ballast, grass}: мир забирает землю и маску в свои
## поля и меняет живое поле травы на новое.
func commit() -> Dictionary:
	var t0 := Time.get_ticks_usec()
	var st: Dictionary = _staging
	var world: Node3D = services["world"]
	var costs := {}
	# ПУТЬ: старый снят целиком, новый поставлен целиком.
	var t_track := Time.get_ticks_usec()
	_remove_child_named(world, "Track")
	if st["track"] != null:
		world.add_child(st["track"])
	costs["track_ms"] = float(Time.get_ticks_usec() - t_track) / 1000.0
	# ЗЕМЛЯ: высоты набора в уже стоящие узлы, меши из результатов перестройки.
	# Узел без результата (клетка 204) остаётся тем, чем был, — и это честно:
	# строки хранилища аддитивны, и у клетки без строки до новой версии не было
	# её и до старой.
	var t_terrain := Time.get_ticks_usec()
	for key in _results:
		var t: Dictionary = services["tiles"][key]
		if (st["heights"] as Dictionary).has(key):
			t["h"] = (st["heights"] as Dictionary)[key]["h"]
			t["base_z"] = (st["heights"] as Dictionary)[key]["base_z"]
		for n_raw in (services["tile_nodes"][key] as Array):
			var n: Dictionary = n_raw
			var q := int(n["q"])
			if ((_results[key] as Dictionary).has(q)):
				(n["mi"] as MeshInstance3D).mesh = (_results[key] as Dictionary)[q]
	costs["terrain_ms"] = float(Time.get_ticks_usec() - t_terrain) / 1000.0
	# РАСТИТЕЛЬНОСТЬ И ТРАВА: старые узлы сняты, новые поставлены.
	var t_veg := Time.get_ticks_usec()
	_remove_child_named(world, "Vegetation")
	if st["veg"] != null:
		world.add_child(st["veg"])
	if st["grass"] != null and (st["grass"] as GrassFieldScript).root != null:
		world.add_child((st["grass"] as GrassFieldScript).root)
	costs["vegetation_ms"] = float(Time.get_ticks_usec() - t_veg) / 1000.0
	# ТВЕРДЬ: старые тела сняты явно, новые поставлены.
	var t_solid := Time.get_ticks_usec()
	for key in _bodies:
		for n_raw in (services["tile_nodes"][key] as Array):
			var n: Dictionary = n_raw
			var q := int(n["q"])
			if not (_bodies[key] as Dictionary).has(q):
				continue
			var mi: MeshInstance3D = n["mi"]
			for c in mi.get_children():
				var b := c as StaticBody3D
				if b != null:
					mi.remove_child(b)
					b.free()
			mi.add_child((_bodies[key] as Dictionary)[q])
	costs["solid_ms"] = float(Time.get_ticks_usec() - t_solid) / 1000.0
	costs["total_ms"] = float(costs["track_ms"] + costs["terrain_ms"]
		+ costs["vegetation_ms"] + costs["solid_ms"])
	return {
		"costs": costs, "ground": st["ground"], "ballast": st["ballast"],
		"grass": st["grass"], "build_costs_us": _costs.duplicate(),
		"over_budget": _over.duplicate(),
	}


func _fail(why: String) -> void:
	_failed_reason = why


func _remove_child_named(parent: Node, name: String) -> void:
	for c in parent.get_children():
		if c.name == name:
			c.free()
			return


## ballast_mask — маска подошвы балластной призмы на ячейках BALLAST_MASK_CELL.
##
## Чистая функция оси и подошвы; мир пользуется ею же при первой загрузке, а
## перестройка — для маски нового набора. Одна реализация на оба случая: две
## копии арифметики маски разошлись бы при первой правке шага досэмплирования.
static func ballast_mask(axis: PackedVector2Array, toe_m: float) -> Dictionary:
	var out := {}
	if axis.size() < 2 or toe_m <= 0.0:
		return out
	var r := toe_m + GrassFieldScript.BALLAST_MASK_CELL
	for k in axis.size() - 1:
		var a := axis[k]
		var b := axis[k + 1]
		var seg := a.distance_to(b)
		var steps := maxi(1, int(ceil(seg / (GrassFieldScript.BALLAST_MASK_CELL * 0.5))))
		for s in steps + 1:
			var p := a.lerp(b, float(s) / float(steps))
			var i0 := int(floor((p.x - r) / GrassFieldScript.BALLAST_MASK_CELL))
			var i1 := int(floor((p.x + r) / GrassFieldScript.BALLAST_MASK_CELL))
			var j0 := int(floor((p.y - r) / GrassFieldScript.BALLAST_MASK_CELL))
			var j1 := int(floor((p.y + r) / GrassFieldScript.BALLAST_MASK_CELL))
			for i in range(i0, i1 + 1):
				for j in range(j0, j1 + 1):
					var cx := (float(i) + 0.5) * GrassFieldScript.BALLAST_MASK_CELL
					var cy := (float(j) + 0.5) * GrassFieldScript.BALLAST_MASK_CELL
					if Vector2(cx - p.x, cy - p.y).length() <= toe_m:
						out[i * 100003 + j] = true
	return out


## assemble_ground — земля под растительностью из отсчётов набора (те же якоря).
##
## Якорь — узел без порога видимости: его показывают, когда камера рядом, и
## площади якорей покрывают плоскость ровно один раз. На них садится
## растительность и из них строится твердь.
static func assemble_ground(tiles: Dictionary, tile_nodes: Dictionary, rule: ChunkRule,
		staging_heights: Dictionary) -> Array[Dictionary]:
	var out: Array[Dictionary] = []
	for key in tile_nodes:
		var t: Dictionary = tiles[key]
		var h: PackedFloat32Array = t["h"]
		var base_z: float = t["base_z"]
		if staging_heights.has(key):
			h = staging_heights[key]["h"]
			base_z = staging_heights[key]["base_z"]
		var level := int(t["level"])
		var cx := int(t["cx"])
		var cz := int(t["cz"])
		for n_raw in (tile_nodes[key] as Array):
			var n: Dictionary = n_raw
			if float(n["begin"]) != 0.0:
				continue
			var q := int(n["q"])
			var i0 := 0
			var j0 := 0
			var span := rule.cells()
			if q >= 0:
				var half := rule.cells() >> 1
				i0 = (q & 1) * half
				j0 = ((q >> 1) & 1) * half
				span = half
			if not (t["cover"] as PackedByteArray).is_empty():
				out.append({"cover": t["cover"], "forest": t["forest"],
					"heights": h, "base_z": base_z,
					"level": level, "cx": cx, "cz": cz,
					"i0": i0, "j0": j0, "span": span})
	return out
