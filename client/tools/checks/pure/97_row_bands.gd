## РЕЗКА МЕША И КОЛЛИЗИИ ПО СТРОКАМ СЕТКИ (sqym.16) — row-API TerrainMesh.
##
## 96_version_rebuild проверяет перестройку в сборе (очередь, бюджет, commit).
## Здесь проверяется САМА резка, без очереди: build_band + assemble — и три её
## свойства, каждое числом, а не глазом:
##
##   • ПОЛОСЫ ПОКРЫВАЮТ КУСОК ЦЕЛИКОМ: сборка полосами даёт те же вершины
##     (попозиционно) и те же треугольники, что сборка целиком, — ни щели
##     между полосами, ни повтора ряда;
##   • ШОВ МЕЖДУ ЧАНКАМИ НЕ ЕДЕТ: у соседей общий ряд отсчётов, и после
##     порезанной сборки ОБЕ стороны содержат один и тот же ряд вершин.
##     Ряд кривой (парабола), а не плоский: сдвинь или потеряй ряд — тест
##     краснеет, плоская линия ошибку бы спрятала;
##   • КОЛЛИЗИЯ СОБИРАЕТСЯ ТЕМИ ЖЕ ПОЛОСАМИ: треугольники faces в сумме дают
##     треугольники меша, последний элемент — юбка.
extends "res://tools/check_suite.gd"

const VersionRebuildScript := preload("res://scripts/version_rebuild.gd")

var _rule: ChunkRule = null


## Парабола по строке: та же, что в 96 — провис ненулевой, юбки ненулевые, а
## ряд в z = −256 у обеих соседних клеток несёт одно и то же кривое значение.
func _ramp(n: int) -> PackedFloat32Array:
	var a := PackedFloat32Array()
	a.resize(n * n)
	for k in n * n:
		a[k] = 0.01 * float(k % n) * float(k % n)
	return a


## _band_mesh — собрать целую клетку уровня 0 полосами, как перестройка набора:
## build_band по TERRAIN_BAND_ROWS рядов за вызов, затем assemble.
func _band_mesh(rule: ChunkRule, h: PackedFloat32Array, cx: int, cz: int,
		skirt: float) -> Dictionary:
	var acc := TerrainMesh.new_acc(rule.samples, -1)
	var j := int(acc["j0"])
	var j1 := int(acc["j1"])
	var band_rows := VersionRebuildScript.TERRAIN_BAND_ROWS
	while j < j1:
		var res := TerrainMesh.build_band(h, 100.0, 0, cx, cz, rule,
			PackedByteArray(), -1, j, mini(j + band_rows, j1), acc)
		if not res["ok"]:
			return res
		j = mini(j + band_rows, j1)
	return TerrainMesh.assemble(acc, skirt)


func _has_vertex(mesh: Mesh, p: Vector3, eps: float) -> bool:
	var am := mesh as ArrayMesh
	if am == null or am.get_surface_count() == 0:
		return false
	var vs: PackedVector3Array = am.surface_get_arrays(0)[Mesh.ARRAY_VERTEX]
	for v in vs:
		if v.distance_to(p) < eps:
			return true
	return false


func run() -> void:
	_rule = await ctx.rule()
	var n := _rule.samples
	var h := _ramp(n)

	# --- ПОЛОСЫ ПОКРЫВАЮТ КУСОК ЦЕЛИКОМ: КАК ЦЕЛЫЙ КУСОК -------------------
	var whole: Dictionary = TerrainMesh.build(h, 100.0, 0, 0, 0, _rule, PackedByteArray(), -1, 0.5)
	var banded: Dictionary = _band_mesh(_rule, h, 0, 0, 0.5)
	_ok("сборка полосами собралась", banded.get("ok", false),
		String(banded.get("error", "")))
	_ok("полосы дают те же треугольники, что целый кусок",
		int(banded["triangles"]) == int(whole["triangles"]),
		"целиком %d, полосами %d" % [int(whole["triangles"]), int(banded["triangles"])])
	_ok("полосы дают те же вершины, что целый кусок",
		int(banded["vertices"]) == int(whole["vertices"]),
		"целиком %d, полосами %d" % [int(whole["vertices"]), int(banded["vertices"])])
	# Вершины совпадают и ПОЗИЦИЯМИ, не только числом: ряды построены в одном
	# порядке (полоса добавляет только новые строки), и ни одна строка не
	# сдвинута и не потеряна. Допуск 1 мм — float32, а не байтовая сверка.
	var vs_b: PackedVector3Array = (banded["mesh"] as ArrayMesh).surface_get_arrays(0)[Mesh.ARRAY_VERTEX]
	var vs_w: PackedVector3Array = (whole["mesh"] as ArrayMesh).surface_get_arrays(0)[Mesh.ARRAY_VERTEX]
	var same := vs_b.size() == vs_w.size()
	if same:
		for k in vs_b.size():
			if vs_b[k].distance_to(vs_w[k]) > 0.001:
				same = false
				break
	_ok("вершины полос совпадают с целыми попозиционно (допуск 1 мм)", same)

	# --- ШОВ МЕЖДУ ЧАНКАМИ: ОБЩИЙ РЯД ОТСЧЁТОВ ------------------------------
	# Клетки (0,0) и (0,1) — соседи по z; их общий ряд (j=64 первой и j=0
	# второй) лежит в z = −256 и у обеих сторон несёт параболу 0.01·i². Резка
	# не вправе ни сдвинуть ряд, ни потерять его: обе стороны обязаны
	# содержать вершину в каждой точке ряда. Юбка висит на полметра ниже и в
	# допуск 1 см не попадает — проверяется именно поверхность.
	var m0: Dictionary = _band_mesh(_rule, h, 0, 0, 0.5)
	var m1: Dictionary = _band_mesh(_rule, h, 0, 1, 0.5)
	var step := _rule.step_of(0)
	var seam_fail := ""
	for i in n:
		var p := Vector3(float(i) * step, 100.0 + 0.0001 * float(i * i), -256.0)
		if not (_has_vertex(m0["mesh"], p, 0.01) and _has_vertex(m1["mesh"], p, 0.01)):
			seam_fail = "точка i=%d (%s)" % [i, str(p)]
			break
	_ok("общий ряд соседей после порезанной сборки одинаков в обоих мешах",
		seam_fail == "", seam_fail)

	# --- КОЛЛИЗИЯ ПОЛОС: ТРЕУГОЛЬНИКИ FACES = ТРЕУГОЛЬНИКИ МЕША --------------
	var faces: Array = banded["faces"]
	var face_tris := 0
	for f in faces:
		face_tris += (f as PackedVector3Array).size() / 3
	_ok("треугольники полос и юбки дают треугольники меша",
		face_tris == int(banded["triangles"]),
		"faces %d, в меше %d" % [face_tris, int(banded["triangles"])])
	_ok("юбка в faces последним элементом",
		not faces.is_empty() and (faces[faces.size() - 1] as PackedVector3Array).size() > 0)
