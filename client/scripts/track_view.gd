## TrackView — путь на экране.
##
## Два вида отрисовки, и выбор между ними ОПРЕДЕЛЯЕТСЯ ДАННЫМИ, а не вкусом:
##
##   • ЛЕНТА шириной 2 × ballast.half_width — если сервер связал элемент со
##     строительным прогоном (construction_runs), а прогон — с типом пути
##     (track_types), у которого эта ширина есть;
##   • НИТЬ в один экранный пиксель — если такой связи нет. Ширины не прислали,
##     и лента любой ширины была бы выдумкой. Тонкая линия — это видимое «размер
##     неизвестен», а не украшение.
##
## Так на затравке ST_A ветви стрелок (SW1:*, SW2:*) рисуются нитью: ни один
## construction_run их не покрывает. Это не дефект клиента, а состояние данных,
## и его должно быть ВИДНО.
##
## Глубина: ось пути лежит ровно на обработанной поверхности рельефа (сервер
## сажает землю на отметку оси), поэтому лента и меш совпадают в точности и
## дерутся за z-буфер. Лечится это отключением ПРОВЕРКИ ГЛУБИНЫ у материала
## пути, а не подъёмом пути на «маленькую» высоту: подъём был бы сдвигом
## координаты, то есть данными, которых никто не присылал.
class_name TrackView
extends RefCounted


static func ribbon_mesh(el: TrackGeom.Element) -> ArrayMesh:
	var pts := el.points
	if pts.size() < 2 or el.ballast_half_width_m <= 0.0:
		return null

	var hw := el.ballast_half_width_m
	var verts := PackedVector3Array()
	var norms := PackedVector3Array()
	verts.resize(pts.size() * 2)
	norms.resize(pts.size() * 2)

	for k in pts.size():
		var p: TrackGeom.AxisPoint = pts[k]
		# Нормаль в плане — поворот курса на 90°. Ширина откладывается по ней,
		# высота у обоих краёв та же, что у оси: поперечный уклон отсыпки серверу
		# неизвестен, и придумать его значило бы повторить историю с восемью
		# промилле продольного профиля.
		var nx := -sin(p.heading)
		var ny := cos(p.heading)
		verts[k * 2] = TerrainMesh.to_godot(p.x + nx * hw, p.y + ny * hw, p.z)
		verts[k * 2 + 1] = TerrainMesh.to_godot(p.x - nx * hw, p.y - ny * hw, p.z)
		norms[k * 2] = Vector3.UP
		norms[k * 2 + 1] = Vector3.UP

	var idx := PackedInt32Array()
	for k in pts.size() - 1:
		var a := k * 2
		var b := k * 2 + 1
		var c := (k + 1) * 2
		var d := (k + 1) * 2 + 1
		idx.append_array([a, c, b, b, c, d])

	var arrays := []
	arrays.resize(Mesh.ARRAY_MAX)
	arrays[Mesh.ARRAY_VERTEX] = verts
	arrays[Mesh.ARRAY_NORMAL] = norms
	arrays[Mesh.ARRAY_INDEX] = idx

	var mesh := ArrayMesh.new()
	mesh.add_surface_from_arrays(Mesh.PRIMITIVE_TRIANGLES, arrays)
	return mesh


static func line_mesh(el: TrackGeom.Element) -> ImmediateMesh:
	if el.points.size() < 2:
		return null
	var mesh := ImmediateMesh.new()
	mesh.surface_begin(Mesh.PRIMITIVE_LINE_STRIP)
	for p_raw in el.points:
		var p: TrackGeom.AxisPoint = p_raw
		mesh.surface_add_vertex(TerrainMesh.to_godot(p.x, p.y, p.z))
	mesh.surface_end()
	return mesh


static func ribbon_material(colour: Color) -> StandardMaterial3D:
	var m := StandardMaterial3D.new()
	m.albedo_color = colour
	m.cull_mode = BaseMaterial3D.CULL_DISABLED
	m.no_depth_test = true
	# Заодно и не писать глубину: ленты соседних элементов у стрелки лежат в
	# одной плоскости (все на отметке оси), и запись глубины без её проверки
	# рвёт их на куски по порядку отрисовки.
	m.depth_draw_mode = BaseMaterial3D.DEPTH_DRAW_DISABLED
	m.render_priority = 1
	return m


static func line_material(colour: Color) -> StandardMaterial3D:
	var m := StandardMaterial3D.new()
	m.albedo_color = colour
	m.shading_mode = BaseMaterial3D.SHADING_MODE_UNSHADED
	m.no_depth_test = true
	# Заодно и не писать глубину: ленты соседних элементов у стрелки лежат в
	# одной плоскости (все на отметке оси), и запись глубины без её проверки
	# рвёт их на куски по порядку отрисовки.
	m.depth_draw_mode = BaseMaterial3D.DEPTH_DRAW_DISABLED
	m.render_priority = 2
	return m
