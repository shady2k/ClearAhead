## Vegetation — эталонные меши растительности и их размещение экземплярами.
##
## # Почему это КЛИЕНТ целиком, и почему это не нарушение правила
##
## Правило владельца 2026-08-12: «сервер не должен отдавать ель; ель рисует
## клиент, сервер говорит ГДЕ она и КАКАЯ». Отсюда:
##
##   * ГДЕ — битовая карта леса, приезжает с сервера (chunk /forest);
##   * КАКАЯ — класс покрова той же ячейки, приезжает с сервера (/cover);
##   * КАКОЙ ВЫСОТЫ — ForestJitter, функция адреса, часть контракта;
##   * ИЗ КАКИХ ТРЕУГОЛЬНИКОВ — здесь. Ель из восьми сегментов и ель из
##     шестнадцати — одна и та же ель, и мир от выбора не меняется.
##
## ТРАВА — целиком здесь, включая посадку. У травинки нет тождества (контракт
## чанков §7): её нельзя срубить, посадить и передать. Единственное условие —
## сажать ПО ПОКРОВУ, а не по своему шуму, иначе клиент придумает, где растёт
## трава, а это уже факт о мире.
##
## Кусты — та же природа, что трава: заросль на месте следует из покрова, а
## отдельный куст тождества не имеет.
class_name Vegetation
extends RefCounted

## Пропорции елей и кустов — решения художника, взятые у снесённого спайка
## (разбор §1.3), чтобы не подбирать глазом второй раз.
const SPRUCE_SEGMENTS := 8
const BUSH_SEGMENTS := 6

const C_CONIFER := Color(0.12, 0.23, 0.11)
const C_CONIFER_LIT := Color(0.20, 0.32, 0.15)
const C_BROAD := Color(0.26, 0.36, 0.15)
const C_TRUNK := Color(0.20, 0.15, 0.11)
const C_BUSH := Color(0.20, 0.30, 0.13)
const C_BUSH_DRY := Color(0.31, 0.34, 0.17)
const C_GRASS := Color(0.38, 0.55, 0.20)
const C_GRASS_DRY := Color(0.50, 0.56, 0.26)


## _cone — конус радиуса r и высоты h с основанием на y0. Боковая поверхность
## без донышка: снизу его не видно ни у ели, ни у куста.
static func _cone(v: PackedVector3Array, n: PackedVector3Array, c: PackedColorArray,
		idx: PackedInt32Array, y0: float, r: float, h: float, col: Color, segs: int) -> void:
	var apex := Vector3(0, y0 + h, 0)
	for k in segs:
		var a0 := TAU * float(k) / float(segs)
		var a1 := TAU * float(k + 1) / float(segs)
		var p0 := Vector3(cos(a0) * r, y0, sin(a0) * r)
		var p1 := Vector3(cos(a1) * r, y0, sin(a1) * r)
		var nrm := (p1 - p0).cross(apex - p0).normalized()
		if nrm == Vector3.ZERO:
			nrm = Vector3.UP
		var b := v.size()
		v.append(p0); v.append(p1); v.append(apex)
		var lin := col.srgb_to_linear()
		for _i in 3:
			n.append(nrm)
			c.append(lin)
		idx.append_array([b, b + 1, b + 2])


## _cylinder — ствол. Боковая поверхность, без крышек.
static func _cylinder(v: PackedVector3Array, n: PackedVector3Array, c: PackedColorArray,
		idx: PackedInt32Array, y0: float, r: float, h: float, col: Color, segs: int) -> void:
	for k in segs:
		var a0 := TAU * float(k) / float(segs)
		var a1 := TAU * float(k + 1) / float(segs)
		var d0 := Vector3(cos(a0), 0, sin(a0))
		var d1 := Vector3(cos(a1), 0, sin(a1))
		var b := v.size()
		v.append(d0 * r + Vector3(0, y0, 0))
		v.append(d1 * r + Vector3(0, y0, 0))
		v.append(d1 * r + Vector3(0, y0 + h, 0))
		v.append(d0 * r + Vector3(0, y0 + h, 0))
		n.append(d0); n.append(d1); n.append(d1); n.append(d0)
		var lin := col.srgb_to_linear()
		for _i in 4:
			c.append(lin)
		idx.append_array([b, b + 1, b + 2, b, b + 2, b + 3])


static func _mesh(v: PackedVector3Array, n: PackedVector3Array, c: PackedColorArray, idx: PackedInt32Array) -> ArrayMesh:
	var arrays := []
	arrays.resize(Mesh.ARRAY_MAX)
	arrays[Mesh.ARRAY_VERTEX] = v
	arrays[Mesh.ARRAY_NORMAL] = n
	arrays[Mesh.ARRAY_COLOR] = c
	arrays[Mesh.ARRAY_INDEX] = idx
	var m := ArrayMesh.new()
	m.add_surface_from_arrays(Mesh.PRIMITIVE_TRIANGLES, arrays)
	return m


## spruce_mesh — ель ЕДИНИЧНОЙ высоты: два яруса кроны и ствол.
##
## Единичной затем, что высота приезжает функцией адреса и разная у каждого
## дерева: экземпляр масштабируется, а меш один на весь лес. Иначе на 726
## деревьев чанка пришлось бы 726 мешей.
static func spruce_mesh() -> ArrayMesh:
	var v := PackedVector3Array()
	var n := PackedVector3Array()
	var c := PackedColorArray()
	var idx := PackedInt32Array()
	_cylinder(v, n, c, idx, 0.0, 0.022, 0.12, C_TRUNK, 5)
	_cone(v, n, c, idx, 0.08, 0.21, 0.52, C_CONIFER, SPRUCE_SEGMENTS)
	_cone(v, n, c, idx, 0.40, 0.15, 0.60, C_CONIFER_LIT, SPRUCE_SEGMENTS)
	return _mesh(v, n, c, idx)


## broadleaf_mesh — лиственное: ствол выше, крона шаром из двух конусов
## навстречу. Форма другая нарочно: с двухсот метров порода читается силуэтом,
## а не цветом, и класс покрова, различающий хвойный и лиственный лес, иначе
## был бы прислан зря.
static func broadleaf_mesh() -> ArrayMesh:
	var v := PackedVector3Array()
	var n := PackedVector3Array()
	var c := PackedColorArray()
	var idx := PackedInt32Array()
	_cylinder(v, n, c, idx, 0.0, 0.026, 0.38, C_TRUNK, 5)
	_cone(v, n, c, idx, 0.34, 0.30, 0.50, C_BROAD, SPRUCE_SEGMENTS)
	# Нижний ярус кроны — конус остриём ВНИЗ: даёт округлый силуэт без сферы.
	var v2 := PackedVector3Array()
	var n2 := PackedVector3Array()
	var c2 := PackedColorArray()
	var i2 := PackedInt32Array()
	_cone(v2, n2, c2, i2, 0.0, 0.30, 0.26, C_BROAD, SPRUCE_SEGMENTS)
	for k in v2.size():
		v.append(Vector3(v2[k].x, 0.60 - v2[k].y, v2[k].z))
		n.append(Vector3(n2[k].x, -n2[k].y, n2[k].z))
		c.append(c2[k])
	var base := idx.size()
	for k in i2.size():
		idx.append(i2[k] + (v.size() - v2.size()))
	base = base  # порядок обхода перевёрнут вместе с нормалью, отдельной правки не нужно
	return _mesh(v, n, c, idx)


## bush_mesh — куст единичной высоты: приплюснутый конус.
static func bush_mesh(dry: bool) -> ArrayMesh:
	var v := PackedVector3Array()
	var n := PackedVector3Array()
	var c := PackedColorArray()
	var idx := PackedInt32Array()
	_cone(v, n, c, idx, 0.0, 0.55, 1.0, C_BUSH_DRY if dry else C_BUSH, BUSH_SEGMENTS)
	return _mesh(v, n, c, idx)


## grass_mesh — пучок травы: три скрещённых четырёхугольника единичной высоты.
##
## Квады, а не листья по одному: на пучок приходится шесть треугольников вместо
## сотни, и с двух метров разницы не видно. Это ровно та подробность, которая
## по границе владения принадлежит клиенту.
static func grass_mesh() -> ArrayMesh:
	var v := PackedVector3Array()
	var n := PackedVector3Array()
	var c := PackedColorArray()
	var idx := PackedInt32Array()
	# Полуширина 0.05, а не 0.16: при 0.16 пучок читается ПЛИТОЙ, а не травой —
	# видно на кадре с оси, где он занимает те же пиксели, что шпала. Число
	# подобрано снимком, а не рассуждением.
	for k in 3:
		var a := PI * float(k) / 3.0
		var d := Vector3(cos(a), 0, sin(a)) * 0.05
		var b := v.size()
		v.append(-d); v.append(d)
		v.append(d + Vector3(0, 1, 0)); v.append(-d + Vector3(0, 1, 0))
		var nrm := Vector3(-d.z, 0.6, d.x).normalized()
		for _i in 4:
			n.append(nrm)
		# Линейным, как и всюду: у корня сухой тон, у верхушки живой.
		var dry := C_GRASS_DRY.srgb_to_linear()
		var lush := C_GRASS.srgb_to_linear()
		c.append(dry); c.append(dry)
		c.append(lush); c.append(lush)
		idx.append_array([b, b + 1, b + 2, b, b + 2, b + 3])
	return _mesh(v, n, c, idx)


## material — общий материал растительности: цвет из вершин, свет обычный.
##
## cull_disabled нужен траве и лиственной кроне: у них есть грани, видимые с
## изнанки. Godot при этом САМ переворачивает нормаль для задней грани — это
## записано в памяти проекта (bd recall godot-cull-disabled-flips-normal), и
## поэтому отдельной правки нормалей здесь нет.
static func material() -> StandardMaterial3D:
	var m := StandardMaterial3D.new()
	m.vertex_color_use_as_albedo = true
	m.cull_mode = BaseMaterial3D.CULL_DISABLED
	m.roughness = 0.95
	return m
