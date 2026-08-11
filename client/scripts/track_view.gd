## TrackView — путь на экране.
##
## Сюда приходит уже РАЗМЕЩЁННОЕ (TrackBuild) и превращается в меши. Ни одного
## решения о мире здесь нет: только про то, из каких треугольников это сложить.
##
## Два вида осевой отрисовки, и выбор между ними ОПРЕДЕЛЯЕТСЯ ДАННЫМИ, а не
## вкусом:
##
##   • ЛЕНТА шириной 2 × ballast.half_width — на участке, покрытом строительным
##     прогоном (construction_runs), у типа которого эта ширина есть;
##   • НИТЬ в один экранный пиксель — там, где такой связи нет. Ширины не
##     прислали, и лента любой ширины была бы выдумкой. Тонкая линия — это
##     видимое «размер неизвестен», а не украшение.
##
## Так на затравке ST_A ветви стрелок (SW1:*, SW2:*) остаются нитью: ни один
## construction_run их не покрывает. Это не дефект клиента, а состояние данных.
##
## ВСЁ ПЛОСКОЕ И НА ОТМЕТКЕ ОСИ. Ни балласт телом, ни шпала коробкой, ни рельс
## профилем, ни плита платформы толщиной не рисуются, и это не лень: в контракте
## не сказано, от чего отсчитывается `z` элемента — от головки рельса, от верха
## призмы или от бровки земляного полотна. Пока это не названо, неизвестно даже,
## вверх откладывать высоту или вниз, и любое вертикальное число было бы
## выдумкой того же рода, что снесённые восемь промилле продольного профиля.
##
## Глубина: ось пути лежит ровно на обработанной поверхности рельефа (сервер
## сажает землю на отметку оси), поэтому всё нарисованное и меш рельефа
## совпадают в точности и дерутся за z-буфер. Лечится это отключением ПРОВЕРКИ
## ГЛУБИНЫ у материалов пути, а не подъёмом пути на «маленькую» высоту: подъём
## был бы сдвигом координаты, то есть данными, которых никто не присылал.
## Порядок слоёв задаётся render_priority — он экранный и миру не принадлежит.
class_name TrackView
extends RefCounted

## Порядок слоёв. Числа экранные: что поверх чего, а не что выше чего.
const PRIO_BALLAST := 1
const PRIO_PLATFORM := 2
const PRIO_SLEEPER := 3
const PRIO_RAIL := 4
const PRIO_LINE := 5
const PRIO_FROG := 6


## ribbon_mesh — лента постоянной полуширины вдоль оси.
static func ribbon_mesh(axis: Array[TrackGeom.AxisPoint], half_width_m: float) -> ArrayMesh:
	if axis.size() < 2 or half_width_m <= 0.0:
		return null

	var verts := PackedVector3Array()
	var norms := PackedVector3Array()
	verts.resize(axis.size() * 2)
	norms.resize(axis.size() * 2)

	for k in axis.size():
		var p: TrackGeom.AxisPoint = axis[k]
		# Ширина откладывается по нормали в плане; высота у обоих краёв та же,
		# что у оси: поперечный уклон отсыпки серверу неизвестен, и придумать его
		# значило бы повторить историю с восемью промилле продольного профиля.
		var n := p.left()
		verts[k * 2] = TerrainMesh.to_godot(p.x + n.x * half_width_m, p.y + n.y * half_width_m, p.z)
		verts[k * 2 + 1] = TerrainMesh.to_godot(p.x - n.x * half_width_m, p.y - n.y * half_width_m, p.z)
		norms[k * 2] = Vector3.UP
		norms[k * 2 + 1] = Vector3.UP

	return _strip(verts, norms)


## strip_mesh — полоса между двумя присланными кромками.
##
## Отдельно от ribbon_mesh: у платформы кромки НЕ симметричны относительно оси
## (offset и offset + width с одной стороны), и подгонять её под ленту значило бы
## считать её шириной вдвое большей, чем прислано.
static func strip_mesh(near: PackedVector3Array, far: PackedVector3Array) -> ArrayMesh:
	var n := mini(near.size(), far.size())
	if n < 2:
		return null
	var verts := PackedVector3Array()
	var norms := PackedVector3Array()
	verts.resize(n * 2)
	norms.resize(n * 2)
	for k in n:
		verts[k * 2] = TerrainMesh.to_godot(near[k].x, near[k].y, near[k].z)
		verts[k * 2 + 1] = TerrainMesh.to_godot(far[k].x, far[k].y, far[k].z)
		norms[k * 2] = Vector3.UP
		norms[k * 2 + 1] = Vector3.UP
	return _strip(verts, norms)


## sleeper_mesh — вся решётка одним мешем.
##
## Одним, а не 768 узлами: шпала не имеет доменной идентичности (render-contract
## §4 — «порядковый номер n не является доменной идентичностью»), выделять ей
## узел сцены нечем и незачем. Прямоугольник плоский: длина поперёк пути,
## ширина вдоль, якорь в геометрическом центре — ровно как задано правилом
## ориентации в §4.
static func sleeper_mesh(list: Array[TrackBuild.Sleeper]) -> ArrayMesh:
	if list.is_empty():
		return null
	var verts := PackedVector3Array()
	var norms := PackedVector3Array()
	var idx := PackedInt32Array()
	verts.resize(list.size() * 4)
	norms.resize(list.size() * 4)
	idx.resize(list.size() * 6)
	for k in list.size():
		var s: TrackBuild.Sleeper = list[k]
		var p := s.pose
		var n := p.left()
		var f := p.forward()
		var hl := s.length_m * 0.5
		var hw := s.width_m * 0.5
		var b := k * 4
		verts[b] = TerrainMesh.to_godot(p.x + n.x * hl - f.x * hw, p.y + n.y * hl - f.y * hw, p.z)
		verts[b + 1] = TerrainMesh.to_godot(p.x + n.x * hl + f.x * hw, p.y + n.y * hl + f.y * hw, p.z)
		verts[b + 2] = TerrainMesh.to_godot(p.x - n.x * hl + f.x * hw, p.y - n.y * hl + f.y * hw, p.z)
		verts[b + 3] = TerrainMesh.to_godot(p.x - n.x * hl - f.x * hw, p.y - n.y * hl - f.y * hw, p.z)
		for c in 4:
			norms[b + c] = Vector3.UP
		var q := k * 6
		idx[q] = b
		idx[q + 1] = b + 1
		idx[q + 2] = b + 2
		idx[q + 3] = b
		idx[q + 4] = b + 2
		idx[q + 5] = b + 3
	return _mesh(verts, norms, idx)


## rail_mesh — нитки линиями.
##
## Линия, а не полоса в метрах: ширина нитки — величина ЭКРАННАЯ (render-contract
## §2, «толщина нитки 2 экранных px — по определению экранная»). Полоса в метрах
## заявляла бы ширину головки рельса, которой в контракте нет.
static func rail_mesh(threads: Array[PackedVector3Array]) -> ImmediateMesh:
	var any := false
	var mesh := ImmediateMesh.new()
	for line in threads:
		if line.size() < 2:
			continue
		any = true
		mesh.surface_begin(Mesh.PRIMITIVE_LINE_STRIP)
		for p in line:
			mesh.surface_add_vertex(TerrainMesh.to_godot(p.x, p.y, p.z))
		mesh.surface_end()
	return mesh if any else null


static func line_mesh(axis: Array[TrackGeom.AxisPoint]) -> ImmediateMesh:
	if axis.size() < 2:
		return null
	var mesh := ImmediateMesh.new()
	mesh.surface_begin(Mesh.PRIMITIVE_LINE_STRIP)
	for p_raw in axis:
		var p: TrackGeom.AxisPoint = p_raw
		mesh.surface_add_vertex(TerrainMesh.to_godot(p.x, p.y, p.z))
	mesh.surface_end()
	return mesh


## frog_mesh — крестовины галочками.
##
## Точка и обе касательные присланы; выдумана только ДЛИНА КРЫЛА и его ширина —
## и это прямо разрешено: «FROG_WING — длина галочки поверх стрелки — штрих для
## читаемости, ничем не измеряется на месте» (render-contract §2, таблица
## владения). Числа выбраны так, чтобы галочка читалась в виде на всю станцию;
## второй клиент вправе взять другие, и мир от этого не изменится.
static func frog_mesh(list: Array[TrackBuild.Frog], wing_m: float, half_w_m: float) -> ArrayMesh:
	if list.is_empty():
		return null
	var verts := PackedVector3Array()
	var norms := PackedVector3Array()
	var idx := PackedInt32Array()
	for f_raw in list:
		var f: TrackBuild.Frog = f_raw
		for t in f.tangents:
			var d := t.normalized()
			if d == Vector2.ZERO:
				continue
			var n := Vector2(-d.y, d.x) * half_w_m
			var b := verts.size()
			verts.append(TerrainMesh.to_godot(f.point.x + n.x, f.point.y + n.y, f.point.z))
			verts.append(TerrainMesh.to_godot(f.point.x - n.x, f.point.y - n.y, f.point.z))
			verts.append(TerrainMesh.to_godot(f.point.x + d.x * wing_m - n.x, f.point.y + d.y * wing_m - n.y, f.point.z))
			verts.append(TerrainMesh.to_godot(f.point.x + d.x * wing_m + n.x, f.point.y + d.y * wing_m + n.y, f.point.z))
			for c in 4:
				norms.append(Vector3.UP)
			idx.append_array([b, b + 1, b + 2, b, b + 2, b + 3])
	if idx.is_empty():
		return null
	return _mesh(verts, norms, idx)


static func _strip(verts: PackedVector3Array, norms: PackedVector3Array) -> ArrayMesh:
	var pairs := verts.size() / 2
	var idx := PackedInt32Array()
	for k in pairs - 1:
		var a := k * 2
		var b := k * 2 + 1
		var c := (k + 1) * 2
		var d := (k + 1) * 2 + 1
		idx.append_array([a, c, b, b, c, d])
	return _mesh(verts, norms, idx)


static func _mesh(verts: PackedVector3Array, norms: PackedVector3Array, idx: PackedInt32Array) -> ArrayMesh:
	var arrays := []
	arrays.resize(Mesh.ARRAY_MAX)
	arrays[Mesh.ARRAY_VERTEX] = verts
	arrays[Mesh.ARRAY_NORMAL] = norms
	arrays[Mesh.ARRAY_INDEX] = idx
	var mesh := ArrayMesh.new()
	mesh.add_surface_from_arrays(Mesh.PRIMITIVE_TRIANGLES, arrays)
	return mesh


## flat_material — материал плоского слоя пути.
##
## Проверка глубины выключена по причине из шапки, а запись глубины — заодно:
## слои лежат в одной плоскости (все на отметке оси), и запись без проверки
## рвала бы их на куски по порядку отрисовки.
static func flat_material(colour: Color, priority: int, unshaded: bool = false) -> StandardMaterial3D:
	var m := StandardMaterial3D.new()
	m.albedo_color = colour
	m.cull_mode = BaseMaterial3D.CULL_DISABLED
	m.no_depth_test = true
	m.depth_draw_mode = BaseMaterial3D.DEPTH_DRAW_DISABLED
	m.render_priority = priority
	if unshaded:
		m.shading_mode = BaseMaterial3D.SHADING_MODE_UNSHADED
	return m
