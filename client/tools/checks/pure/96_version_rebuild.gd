## ПЕРЕСТРОЙКА НАБОРА ПОД БЮДЖЕТОМ (sqym.7) — атомарность, коллизия, уровни.
##
## Версия, которую нельзя переоткрывать: набор готовится В СТОРОНЕ несколько
## кадров, видимым становится одним commit(). Проверяется на мини-сцене — два
## яруса земли уровня 0 и их родительский уровень 1 — без сети и без сервера:
##
##   • НАБОР НЕ ПОКАЗЫВАЕТСЯ ЧАСТИЧНО: пока перестройка идёт, сцена не видит ни
##     нового пути, ни новых мешей, ни новых тел — старая версия целиком;
##   • ПЕРЕСТРОЙКА РАСТЯНУТА НА КАДРЫ: работа делится бюджетом, а не делается
##     одним проходом;
##   • КОЛЛИЗИЯ ПЕРЕСТРАИВАЕТСЯ ВМЕСТЕ С МЕШЕМ: после commit() тело якоря
##     считает ровно треугольники НОВОГО меша, а не старого;
##   • РОДИТЕЛЬСКИЙ И ДОЧЕРНИЙ УРОВЕНЬ ТРОГАЮТСЯ ОБА: патч уровня 0 перекраивает
##     юбки уровня 1, и его меши перестраиваются, хотя их высоты не менялись;
##   • ШАГ ДЕЛИМ (sqym.16): меш и коллизия режутся ПО СТРОКАМ сетки, и на
##     полной перестройке ни один шаг не толще бюджета — превышений нет;
##   • ШОВ МЕЖДУ СОСЕДЯМИ: общий ряд отсчётов после порезанной сборки тот же
##     (геометрия резки проверена отдельно, 97_row_bands).
##
## Мини-сцена повторяет форму живой (world.gd): tiles, tile_nodes, якоря с
## коллизиями, старые узлы Track и Vegetation. Строители пути и растительности —
## заглушки-маркеры; земля, коллизии и трава — НАСТОЯЩИЕ (TerrainMesh.build,
## create_trimesh_collision, GrassField).
extends "res://tools/check_suite.gd"

const VersionRebuildScript := preload("res://scripts/version_rebuild.gd")
const GrassFieldScript := preload("res://scripts/grass_field.gd")

var _rule: ChunkRule = null


## Заглушка строителя пути: помечает отсоединённый узел и отдаёт ось с подошвой.
func _fake_track(_elements: Array, _network: Dictionary, parent: Node3D) -> Dictionary:
	var n := Node3D.new()
	n.name = "NewTrack"
	parent.add_child(n)
	return {"axis": PackedVector2Array([Vector2(0.0, 0.0), Vector2(10.0, 0.0)]), "toe": 3.0}


func _fake_veg_tile(_state: Dictionary, _g: Dictionary, _ballast: Dictionary) -> void:
	pass


func _fake_veg_final(state: Dictionary, parent: Node3D, _rule: ChunkRule) -> void:
	var n := Node3D.new()
	n.name = "NewVeg"
	parent.add_child(n)


## Настоящее поколение травы: земля и маска набора, корень отсоединён.
func _fake_grass_new(ground: Array, ballast: Dictionary):
	var gf := GrassFieldScript.new()
	gf.setup(_rule, ground, ballast, Callable(self, "_test_camera"),
		Callable(self, "_test_multimesh"), {})
	return gf


func _test_camera() -> Vector2:
	return Vector2(128.0, 128.0)


func _test_multimesh(_parent: Node3D, _name: String, _mesh: ArrayMesh,
		_xforms: Array, _mat: Material, _colours: PackedColorArray, _shadows: bool) -> void:
	pass


## Количество треугольников меша (как world.gd::_mesh_tris, без get_faces).
func _tris(mesh: Mesh) -> int:
	var am := mesh as ArrayMesh
	if am == null:
		return 0
	var n := 0
	for s in am.get_surface_count():
		var idx := am.surface_get_array_index_len(s)
		n += (idx if idx > 0 else am.surface_get_array_len(s)) / 3
	return n


func _zeros(n: int) -> PackedFloat32Array:
	var a := PackedFloat32Array()
	a.resize(n)
	return a


## ПАРАБОЛА по строке: провис (TerrainMesh.sag) у линейной рампы НОЛЬ — прямая,
## проведённая через отсчёты вдвое реже, ложится на неё вплотную. Кривизна даёт
## ненулевой провис, и юбки у мешей получаются НЕНУЛЕВЫМИ.
func _ramp(n: int) -> PackedFloat32Array:
	var a := PackedFloat32Array()
	a.resize(n)
	for i in n:
		a[i] = 0.01 * float(i % n) * float(i % n)
	return a


## Луг: класс 0 (медоу), сомкнутость 10 из 15.
func _meadow(cells: int) -> PackedByteArray:
	var c := PackedByteArray()
	c.resize(cells * cells)
	c.fill(0x0a)
	return c


func _bodies_of(mi: MeshInstance3D) -> Array:
	var out: Array = []
	for c in mi.get_children():
		var b := c as StaticBody3D
		if b != null:
			out.append(b)
	return out


## _shape_tris — сумма треугольников ВСЕХ фигур тела. Резка по строкам
## (sqym.16) собирает твердь куска из нескольких CollisionShape3D — по одной
## на полосу, — и счёт обязан складывать их все, а не читать первую.
func _shape_tris(body: StaticBody3D) -> int:
	var n := 0
	for c in body.get_children():
		var cs := c as CollisionShape3D
		if cs != null and cs.shape != null:
			n += cs.shape.get_faces().size() / 3
	return n


## _shape_count — сколько CollisionShape3D у тела. Число фигур — признак того,
## что коллизия резалась по полосам, а не собралась одним движковым вызовом.
func _shape_count(body: StaticBody3D) -> int:
	var out := 0
	for c in body.get_children():
		if c is CollisionShape3D and (c as CollisionShape3D).shape != null:
			out += 1
	return out


func run() -> void:
	_rule = await ctx.rule()
	var n := _rule.samples
	var cells := n - 1

	# --- МИНИ-СЦЕНА: старая версия мира -------------------------------
	var world := Node3D.new()
	var old_track := Node3D.new()
	old_track.name = "Track"
	var ot := Node3D.new()
	ot.name = "OldTrack"
	old_track.add_child(ot)
	world.add_child(old_track)
	var old_veg := Node3D.new()
	old_veg.name = "Vegetation"
	var ov := Node3D.new()
	ov.name = "OldVeg"
	old_veg.add_child(ov)
	world.add_child(old_veg)

	var tiles := {}
	var tile_nodes := {}
	var terrain_solid: Array[MeshInstance3D] = []

	var zeros := _zeros(n * n)
	var cover0 := _meadow(cells)
	# Ярус 0: два яруса-соседа, оба якоря (begin 0, один узел).
	var old0: Mesh = TerrainMesh.build(zeros, 100.0, 0, 0, 0, _rule, cover0, -1, 0.0)["mesh"]
	var old1: Mesh = TerrainMesh.build(zeros, 100.0, 0, 1, 0, _rule, cover0, -1, 0.0)["mesh"]
	tiles["0/0/0"] = {"level": 0, "cx": 0, "cz": 0, "h": zeros, "base_z": 100.0,
		"cover": cover0, "forest": PackedByteArray()}
	tiles["0/1/0"] = {"level": 0, "cx": 0, "cz": 1, "h": zeros, "base_z": 100.0,
		"cover": cover0, "forest": PackedByteArray()}
	# Родительский уровень 1: четыре четверти с порогом (не якоря).
	tiles["1/0/0"] = {"level": 1, "cx": 0, "cz": 0, "h": zeros, "base_z": 100.0,
		"cover": PackedByteArray(), "forest": PackedByteArray()}
	var old_level1 := {}
	for q in 4:
		var mi := MeshInstance3D.new()
		mi.mesh = TerrainMesh.build(zeros, 100.0, 1, 0, 0, _rule,
			PackedByteArray(), q, 0.0)["mesh"]
		old_level1[q] = mi.mesh
	for key in ["0/0/0", "0/1/0"]:
		var mi := MeshInstance3D.new()
		mi.mesh = old0 if key == "0/0/0" else old1
		mi.create_trimesh_collision()   # СТАРОЕ тело — то, что останется, если перестройку забыть
		terrain_solid.append(mi)
		tile_nodes[key] = [{"mi": mi, "q": -1, "begin": 0.0}]
	var level1_nodes: Array = []
	for q in 4:
		var mi := MeshInstance3D.new()
		mi.mesh = old_level1[q]
		level1_nodes.append({"mi": mi, "q": q, "begin": _rule.radius_of(0)})
	tile_nodes["1/0/0"] = level1_nodes

	# --- НАБОР ВЕРСИИ 2: патчи ТОЛЬКО уровня 0 -------------------------
	var ramp := _ramp(n * n)
	var set := {
		"version": 2,
		"network": {"elements": []},
		"heights": {
			"0/0/0": {"h": ramp, "base_z": 100.0},
			"0/1/0": {"h": zeros, "base_z": 100.0},
		},
		"clean": {},
	}
	var stats := {}
	var rb: VersionRebuildScript = VersionRebuildScript.new()
	rb.services = {
		"rule": _rule, "world": world, "tiles": tiles, "tile_nodes": tile_nodes,
		"terrain_solid": terrain_solid, "stats": stats,
		"track_builder": Callable(self, "_fake_track"),
		"veg_tile": Callable(self, "_fake_veg_tile"),
		"veg_finalize": Callable(self, "_fake_veg_final"),
		"grass_new": Callable(self, "_fake_grass_new"),
	}
	rb.begin(set, [])

	# --- ПОКА ПЕРЕСТРОЙКА ИДЁТ — СЦЕНА НЕ МЕНЯЕТСЯ ---------------------
	var frames := 0
	var res := {}
	while not rb.tick(8000)["done"] and frames < 400:
		frames += 1
		# Камера живёт: очередь переупорядочивается каждый кадр, как в _process.
		rb.reprioritize(Vector2(128.0, 128.0))
		if frames == 3:
			_ok("на третьем кадре набор ещё не показан: в сцене старый путь и старые меши",
				world.get_child_count() == 2
				and world.get_node_or_null("Track/OldTrack") != null
				and world.get_node_or_null("Vegetation/OldVeg") != null
				and (tile_nodes["0/0/0"][0]["mi"] as MeshInstance3D).mesh == old0
				and _bodies_of(tile_nodes["0/0/0"][0]["mi"]).size() == 1)
	res = rb.tick(8000)
	_ok("перестройка заняла несколько кадров, а не один проход",
		frames >= 2, "%d кадров" % frames)
	_ok("перестройка завершилась без отказа",
		not bool(res["failed"]) and bool(res["done"]), rb.failed_reason())
	# ПРИЁМОЧНЫЙ КРИТЕРИЙ sqym.16: меш уровня 0 и коллизия режутся по строкам
	# сетки, и ни один шаг не толще бюджета в 8 мс. Прежняя зернистость — меш
	# целиком 11.4 мс, коллизия 11.0 мс — называлась бы здесь каждым кадром;
	# теперь полосы впятеро тоньше, и перестройка проходит молча.
	var over: Array = res["over_budget"]
	_ok("ни один шаг перестройки не превысил бюджет: полосы по строкам сетки",
		over.is_empty(), "превысили: %s" % str(over))

	# --- COMMIT: ОДИН ШАГ, ВСЁ СРАЗУ ------------------------------------
	var hand: Dictionary = rb.commit()
	var costs: Dictionary = hand["costs"]
	_ok("commit дешёв: переключение — назначение ссылок, а не перестройка",
		float(costs["total_ms"]) < 100.0, "%s мс" % costs["total_ms"])
	_ok("сцена получила новый путь, старый снят",
		world.get_node_or_null("Track/NewTrack") != null
		and world.get_node_or_null("Track/OldTrack") == null)
	_ok("сцена получила новую растительность, старая снята",
		world.get_node_or_null("Vegetation/NewVeg") != null
		and world.get_node_or_null("Vegetation/OldVeg") == null)
	_ok("круг травы набора поставлен в мир",
		world.get_node_or_null("Grass") != null)

	# --- ВЫСОТЫ И МЕШИ: НАБОР В ЯВНОМ ВИДЕ ------------------------------
	_ok("высоты набора легли в живые ячейки",
		(tiles["0/0/0"]["h"] as PackedFloat32Array) == ramp
		and (tiles["0/1/0"]["h"] as PackedFloat32Array) == zeros)
	var new0: Mesh = (tile_nodes["0/0/0"][0]["mi"] as MeshInstance3D).mesh
	_ok("меш клетки с новыми высотами сменился",
		new0 != old0 and _tris(new0) > _tris(old0),
		"было %d треугольников, стало %d" % [_tris(old0), _tris(new0)])
	_ok("меш клетки с чистой базой перестроен по тем же высотам",
		(tile_nodes["0/1/0"][0]["mi"] as MeshInstance3D).mesh != old1)

	# --- КОЛЛИЗИЯ ПЕРЕСТРОЕНА ВМЕСТЕ С МЕШЕМ ----------------------------
	for key in ["0/0/0", "0/1/0"]:
		var mi: MeshInstance3D = tile_nodes[key][0]["mi"]
		var bodies := _bodies_of(mi)
		var expected := _tris(mi.mesh)
		_ok("коллизия якоря %s — ОДНО тело из фигур ПОЛОС, треугольники НОВОГО меша" % key,
			bodies.size() == 1
			and _shape_count(bodies[0]) >= 2
			and _shape_tris(bodies[0]) == expected
			and _shape_tris(bodies[0]) != 0,
			"тел %d, фигур %d, треугольников тела %d, в меше %d" % [bodies.size(),
				_shape_count(bodies[0]) if bodies.size() > 0 else -1,
				_shape_tris(bodies[0]) if bodies.size() > 0 else -1, expected])

	# --- РОДИТЕЛЬСКИЙ УРОВЕНЬ ПЕРЕСТРОЕН: ЮБКА ЗАВИСИТ ОТ ДЕТСКОГО САГА --
	var any_parent_rebuilt := false
	for q in 4:
		var mi: MeshInstance3D = tile_nodes["1/0/0"][q]["mi"]
		if mi.mesh != old_level1[q]:
			any_parent_rebuilt = true
			_ok("родительская четверть %d перестроена: юбка уровня 1 выросла от сага уровня 0" % q,
				_tris(mi.mesh) > _tris(old_level1[q]),
				"было %d, стало %d" % [_tris(old_level1[q]), _tris(mi.mesh)])
	_ok("хотя бы одна четверть родительского уровня перестроена", any_parent_rebuilt)

	# --- ЗЕМЛЯ ПОД РАСТИТЕЛЬНОСТЬЮ И МАСКА БАЛЛАСТА ----------------------
	_ok("земля набора отдана миру: два яруса-якоря",
		(hand["ground"] as Array).size() == 2)
	_ok("маска балласта набора отдана миру",
		not (hand["ballast"] as Dictionary).is_empty())
