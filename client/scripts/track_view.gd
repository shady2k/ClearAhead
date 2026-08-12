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
## ПУТЬ СТАЛ ТЕЛОМ 2026-08-12. До того здесь всё было плоским и на отметке оси —
## не по лени, а потому что контракт не говорил, от чего эта отметка считается.
## Редакция 6 §2 назвала её ПОВЕРХНОСТЬЮ КАТАНИЯ, и вертикаль стала считаемой.
##
## Глубина: раньше ось пути лежала ровно на обработанной поверхности рельефа
## (сервер сажал землю на отметку оси), всё дралось за z-буфер, и лечилось это
## отключением ПРОВЕРКИ ГЛУБИНЫ. Теперь земля лежит на `formation_to_rail_top`
## ниже — на затравке это 0.68 м, — и телам драться не с чем: они занимают ровно
## тот объём, который раньше был щелью. Поэтому solid_material проверку глубины
## НЕ отключает.
##
## Отключённой она осталась у ЛИНИЙ — нитей и галочек крестовин: те по-прежнему
## лежат на отметке оси, потому что у них нет толщины ни в каком смысле, и
## поднимать их некуда.
##
## Порядок слоёв (render_priority) сохранён для плоских слоёв; телам он не нужен
## и не назначается — их разводит сама геометрия.
class_name TrackView
extends RefCounted

## Порядок слоёв. Числа экранные: что поверх чего, а не что выше чего.
const PRIO_BALLAST := 1
const PRIO_PLATFORM := 2
const PRIO_SLEEPER := 3
const PRIO_RAIL := 4
const PRIO_LINE := 5
const PRIO_FROG := 6


## prism_mesh — балластная призма ТЕЛОМ: трапеция, протянутая вдоль оси.
##
## Все пять чисел приезжают с сервера, ни одно не выведено:
##
##   верх призмы     z − rail_height − sleeper_height + crib_depth
##   низ призмы      z − formation_to_rail_top
##   полуширина верха   ballast.half_width
##   полуширина низа    half_width + side_slope · высота призмы
##
## Заложение откоса — «метров по горизонтали на метр по вертикали», ровно как у
## земляных работ: одна единица на два откоса в одной карте заведена нарочно
## (контракт §3), потому что две единицы у одноимённых величин — заготовленная
## ошибка.
##
## Строится четырьмя полосами (верх, два откоса, низ), а не замкнутым объёмом с
## торцами: торцы на стыке соседних участков смотрели бы внутрь друг друга, и
## их пришлось бы гасить по признаку смежности, которого у клиента нет. Цена
## названа: у одиночного участка призма открыта с концов, и в виде вплотную это
## видно.
static func prism_mesh(span: TrackBuild.Span) -> ArrayMesh:
	if not span.has_prism() or span.axis.size() < 2:
		return null
	var n := span.axis.size()
	var top_hw := span.ballast_half_width_m
	var prism_h := span.ballast_depth_m + span.ballast_crib_depth_m
	var bot_hw := top_hw + span.ballast_side_slope * prism_h

	var top_l := PackedVector3Array(); top_l.resize(n)
	var top_r := PackedVector3Array(); top_r.resize(n)
	var bot_l := PackedVector3Array(); bot_l.resize(n)
	var bot_r := PackedVector3Array(); bot_r.resize(n)
	for k in n:
		var p: TrackGeom.AxisPoint = span.axis[k]
		var nl := p.left()
		var z_top := p.z - span.rail_height_m - span.sleeper_height_m + span.ballast_crib_depth_m
		var z_bot := p.z - span.formation_to_rail_top_m
		top_l[k] = TerrainMesh.to_godot(p.x + nl.x * top_hw, p.y + nl.y * top_hw, z_top)
		top_r[k] = TerrainMesh.to_godot(p.x - nl.x * top_hw, p.y - nl.y * top_hw, z_top)
		bot_l[k] = TerrainMesh.to_godot(p.x + nl.x * bot_hw, p.y + nl.y * bot_hw, z_bot)
		bot_r[k] = TerrainMesh.to_godot(p.x - nl.x * bot_hw, p.y - nl.y * bot_hw, z_bot)

	var verts := PackedVector3Array()
	var norms := PackedVector3Array()
	var idx := PackedInt32Array()
	_quad_strip(verts, norms, idx, top_r, top_l)
	_quad_strip(verts, norms, idx, top_l, bot_l)
	_quad_strip(verts, norms, idx, bot_r, top_r)
	_quad_strip(verts, norms, idx, bot_l, bot_r)
	return _mesh(verts, norms, idx)


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
	for s_raw in list:
		var s: TrackBuild.Sleeper = s_raw
		var p := s.pose
		# Шпала — коробка, если высоту прислали, и плоский прямоугольник, если
		# нет. Второй случай остаётся не для красоты: карта без sleeper.height
		# сегодня отвергается валидатором, но ответ СТАРОГО сервера её не несёт,
		# и клиент обязан показать решётку без толщины, а не исчезнуть.
		_box(verts, norms, idx, p.x, p.y, p.forward(),
			s.width_m * 0.5, s.length_m * 0.5, s.bottom_z(), s.top_z())
	return _mesh(verts, norms, idx)


## rail_body_mesh — рельсы телом, ОБЪЯВЛЕННЫМ УПРОЩЕНИЕМ.
##
## Прямоугольник `head_width × rail.height`, внутренней гранью на ±gauge/2.
## Это не профиль рельса и не выдаётся за него: контракт (редакция 6 §8) держит
## профиль отложенным и прямо называет прямоугольник упрощением. Заведена ровно
## одна величина формы — ширина головки, — и заведена по условию: gauge задан
## между ВНУТРЕННИМИ РАБОЧИМИ ГРАНЯМИ, и без ширины головки рельс некуда
## поставить, не выдумав её.
##
## Цена умолчания измерена на снесённом спайке: без ширины головки нитки
## поставили по осям головок вместо рабочих граней, и колея вышла 1.335 вместо
## 1.435.
##
## Рисуются три грани — верх и два бока. Низ не рисуется: он лежит на шпале и не
## виден ниоткуда, а треугольники стоят денег.
static func rail_body_mesh(span: TrackBuild.Span) -> ArrayMesh:
	if not span.has_rail_body() or span.axis.size() < 2:
		return null
	var n := span.axis.size()
	var inner := span.gauge_m * 0.5
	var outer := inner + span.rail_head_width_m
	var verts := PackedVector3Array()
	var norms := PackedVector3Array()
	var idx := PackedInt32Array()

	var sides: Array[float] = [1.0, -1.0]
	for sgn in sides:
		var top_i := PackedVector3Array(); top_i.resize(n)
		var top_o := PackedVector3Array(); top_o.resize(n)
		var bot_i := PackedVector3Array(); bot_i.resize(n)
		var bot_o := PackedVector3Array(); bot_o.resize(n)
		for k in n:
			var p: TrackGeom.AxisPoint = span.axis[k]
			var nl := p.left()
			var z_bot := p.z - span.rail_height_m
			top_i[k] = TerrainMesh.to_godot(p.x + nl.x * inner * sgn, p.y + nl.y * inner * sgn, p.z)
			top_o[k] = TerrainMesh.to_godot(p.x + nl.x * outer * sgn, p.y + nl.y * outer * sgn, p.z)
			bot_i[k] = TerrainMesh.to_godot(p.x + nl.x * inner * sgn, p.y + nl.y * inner * sgn, z_bot)
			bot_o[k] = TerrainMesh.to_godot(p.x + nl.x * outer * sgn, p.y + nl.y * outer * sgn, z_bot)
		if sgn > 0.0:
			_quad_strip(verts, norms, idx, top_i, top_o)
			_quad_strip(verts, norms, idx, top_o, bot_o)
			_quad_strip(verts, norms, idx, bot_i, top_i)
		else:
			_quad_strip(verts, norms, idx, top_o, top_i)
			_quad_strip(verts, norms, idx, bot_o, top_o)
			_quad_strip(verts, norms, idx, top_i, bot_i)
	if idx.is_empty():
		return null
	return _mesh(verts, norms, idx)


## slab_mesh — плита платформы: верх, торец у пути и дальний торец.
##
## Толщина нужна не для объёма ради объёма: платформа видна СБОКУ, и торец плиты
## с просветом под ним — то, чем она отличается от прямоугольника, нарисованного
## на земле. Низ плиты не рисуется — он лежит на насыпи и не виден.
static func slab_mesh(strip: TrackBuild.PlatformStrip) -> ArrayMesh:
	if not strip.has_slab():
		return null
	var n := mini(strip.near.size(), strip.far.size())
	if n < 2:
		return null
	var t := strip.slab_thickness_m
	var top_n := PackedVector3Array(); top_n.resize(n)
	var top_f := PackedVector3Array(); top_f.resize(n)
	var bot_n := PackedVector3Array(); bot_n.resize(n)
	var bot_f := PackedVector3Array(); bot_f.resize(n)
	for k in n:
		var a := strip.near[k]
		var b := strip.far[k]
		top_n[k] = TerrainMesh.to_godot(a.x, a.y, a.z)
		top_f[k] = TerrainMesh.to_godot(b.x, b.y, b.z)
		bot_n[k] = TerrainMesh.to_godot(a.x, a.y, a.z - t)
		bot_f[k] = TerrainMesh.to_godot(b.x, b.y, b.z - t)
	var verts := PackedVector3Array()
	var norms := PackedVector3Array()
	var idx := PackedInt32Array()
	_quad_strip(verts, norms, idx, top_n, top_f)
	_quad_strip(verts, norms, idx, bot_n, top_n)
	_quad_strip(verts, norms, idx, top_f, bot_f)
	return _mesh(verts, norms, idx)


## buffer_stop_mesh — упоры коробками.
##
## Габарит присланный: height — над поверхностью катания, width — поперёк пути.
## Длина вдоль пути НЕ прислана и здесь взята равной трети ширины — это решение
## художника того же рода, что длина крыла крестовины, и оно названо: упор,
## нарисованный плоским прямоугольником, не читался бы как препятствие. Второй
## клиент вправе взять другую пропорцию, и мир от этого не изменится.
static func buffer_stop_mesh(list: Array[TrackBuild.BufferStop], length_ratio: float) -> ArrayMesh:
	if list.is_empty():
		return null
	var verts := PackedVector3Array()
	var norms := PackedVector3Array()
	var idx := PackedInt32Array()
	for b_raw in list:
		var b: TrackBuild.BufferStop = b_raw
		var p := b.pose
		_box(verts, norms, idx, p.x, p.y, p.forward(),
			b.width_m * length_ratio * 0.5, b.width_m * 0.5, p.z, p.z + b.height_m)
	if idx.is_empty():
		return null
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


## _quad_strip — полоса между двумя направляющими, дописанная в общие буферы.
##
## Нормаль считается из самой геометрии квада, а не задаётся Vector3.UP: у
## откоса призмы она наклонная, и UP сделал бы откос неотличимым по свету от
## верха. Порядок направляющих задаёт сторону: (a, b) даёт нормаль наружу при
## обходе против часовой в плане.
static func _quad_strip(verts: PackedVector3Array, norms: PackedVector3Array,
		idx: PackedInt32Array, a: PackedVector3Array, b: PackedVector3Array) -> void:
	var n := mini(a.size(), b.size())
	if n < 2:
		return
	for k in n - 1:
		var p0 := a[k]
		var p1 := b[k]
		var p2 := a[k + 1]
		var p3 := b[k + 1]
		var nrm := (p2 - p0).cross(p1 - p0).normalized()
		if nrm == Vector3.ZERO:
			nrm = Vector3.UP
		var base := verts.size()
		verts.append(p0); verts.append(p1); verts.append(p2); verts.append(p3)
		for c in 4:
			norms.append(nrm)
		idx.append_array([base, base + 1, base + 2, base + 1, base + 3, base + 2])


## _box — прямоугольный параллелепипед по оси пути: длина вдоль, ширина поперёк,
## от z_bot до z_top. Шесть граней; торцы нужны, потому что коробка одиночная.
## box_into — та же коробка, что _box, но с цветом вершины. Публичная: постройки
## строятся ею же, и второй экземпляр той же геометрии разошёлся бы с первым.
static func box_into(verts: PackedVector3Array, norms: PackedVector3Array, cols: PackedColorArray,
		idx: PackedInt32Array, cx: float, cy: float, fwd: Vector2, half_len: float, half_wid: float,
		z_bot: float, z_top: float, col: Color) -> void:
	var before := verts.size()
	_box(verts, norms, idx, cx, cy, fwd, half_len, half_wid, z_bot, z_top)
	# ЦВЕТ КЛАДЁТСЯ ЛИНЕЙНЫМ, а принимается sRGB — в нём записаны константы и в
	# нём же их читает человек. Перевод живёт ЗДЕСЬ, в единственной двери в меш,
	# а не у каждого вызывающего: правило уже записано (bd recall
	# godot-vertex-color-linear), и терялось оно ровно тем, что было в одном
	# месте (terrain_mesh.gd) и отсутствовало в двух других.
	#
	# Цена пропуска замерена спайком и видна на кадре: albedo_color движок
	# переводит сам, а ARRAY_COLOR берёт как есть, и 0.41 выходит на экран как
	# 0.67. Посёлок из шести разных цветов читался двенадцатью одинаковыми
	# светло-серыми плитами — не потому, что цвета одинаковы, а потому, что все
	# они выбелены к одному пределу.
	var lin := col.srgb_to_linear()
	for _k in range(before, verts.size()):
		cols.append(lin)


static func _box(verts: PackedVector3Array, norms: PackedVector3Array, idx: PackedInt32Array,
		cx: float, cy: float, fwd: Vector2, half_len: float, half_wid: float,
		z_bot: float, z_top: float) -> void:
	var f := fwd.normalized()
	if f == Vector2.ZERO:
		return
	var l := Vector2(-f.y, f.x)
	# Массивы типизированы явно: у нетипизированного литерала элемент цикла
	# приезжает Variant, и sgn.x не выводится — GDScript отказывается разбирать
	# файл целиком. Поймано снимком, а не проверками: check.gd не трогает
	# track_view.gd вовсе и потому дал 78 «ok» при неразбираемом рисующем коде.
	var levels: Array[float] = [z_bot, z_top]
	var signs: Array[Vector2] = [Vector2(-1, -1), Vector2(1, -1), Vector2(1, 1), Vector2(-1, 1)]
	var corners := PackedVector3Array()
	for sz in levels:
		for sgn in signs:
			var px := cx + f.x * half_len * sgn.x + l.x * half_wid * sgn.y
			var py := cy + f.y * half_len * sgn.x + l.y * half_wid * sgn.y
			corners.append(TerrainMesh.to_godot(px, py, sz))
	# Грани перечислены явно: 0..3 — низ, 4..7 — верх, порядок обхода задаёт
	# наружную нормаль.
	var faces: Array[PackedInt32Array] = [
		PackedInt32Array([4, 5, 6, 7]), PackedInt32Array([3, 2, 1, 0]),
		PackedInt32Array([0, 1, 5, 4]), PackedInt32Array([1, 2, 6, 5]),
		PackedInt32Array([2, 3, 7, 6]), PackedInt32Array([3, 0, 4, 7]),
	]
	for face in faces:
		var p0: Vector3 = corners[face[0]]
		var p1: Vector3 = corners[face[1]]
		var p2: Vector3 = corners[face[2]]
		var p3: Vector3 = corners[face[3]]
		var nrm := (p1 - p0).cross(p2 - p0).normalized()
		if nrm == Vector3.ZERO:
			nrm = Vector3.UP
		var base := verts.size()
		verts.append(p0); verts.append(p1); verts.append(p2); verts.append(p3)
		for c in 4:
			norms.append(nrm)
		idx.append_array([base, base + 1, base + 2, base, base + 2, base + 3])


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


## solid_material — материал ТЕЛА: обычный, с проверкой глубины.
##
## Отличается от flat_material ровно тем, ради чего писалась вся редакция 6:
## тело занимает объём, у него есть верх и бок под разным углом к свету, и
## отключать ему проверку глубины больше не за что. Раньше отключали потому, что
## путь и земля лежали на одной отметке и дрались за z-буфер; теперь земля на
## `formation_to_rail_top` ниже, и щель между ними заполнена призмой.
static func solid_material(colour: Color) -> StandardMaterial3D:
	var m := StandardMaterial3D.new()
	m.albedo_color = colour
	return m


## ФАКТУРА ПУТИ — ПОРТ ИЗ СНЕСЁННОГО СПАЙКА, а не новое сочинение.
##
## Материалы щебня и шпалы были написаны в `spike_world.gd` (коммит 809280f^) и
## доведены там снимками; вместе с кодом там записаны три ошибки, которые стоили
## заходов, и переписывать их заново значило бы наступить на них второй раз:
##
##  1. КЛЕТОЧНЫЙ ШУМ, А НЕ ПЕРЛИН. У перлина нет высоких частот: вблизи балласт
##     читается серой лентой с разводами. Щебень — это ЯЧЕЙКИ С ГРАНИЦАМИ, и
##     даёт их F2−F1: ноль в середине зерна, максимум на шве с соседним.
##  2. SEAMLESS ЗДЕСЬ ВРЕДЕН. NoiseTexture2D делает бесшовность кросс-фейдом
##     тайла с самим собой; клеточный шум от наложения двух сеток теряет ровно
##     то, ради чего взят, — границу зерна. Первый заход спайка с seamless дал
##     наждак вместо щебня. Шов тайла на узкой полосе не виден.
##  3. РАМПА ТОЛЬКО ГАСИТ, ПОДНЯТЬ ОНА НЕ МОЖЕТ: печётся в RGBA8, всё выше
##     единицы обрезается. У F2−F1 бо́льшая часть площади — середина зерна, то
##     есть малые значения; рампа 0.38…1.0 съедала две трети альбедо, и «сухой
##     светлый щебень» выходил почти чёрным. Светлоту задаёт ЦВЕТ, рампа
##     оставляет себе только тень в шве.
##
## Трипланар вместо UV — оттуда же: у мешей пути развёртки нет, и заводить её
## означало бы третью систему координат рядом с (x, y) и u.
const BALLAST_STONE := 0.050   # м — характерный размер щебёнки (натура 3–6 см)
const BALLAST_TILE := 512      # px — сторона тайла
const BALLAST_CELL_PX := 32.0  # px — сторона ячейки, она же 1/frequency
const C_BALLAST := Color(0.50, 0.48, 0.45)  # сухой щебень на солнце
const C_SLEEPER := Color(0.22, 0.17, 0.13)  # пропитанная шпала
## Направление волокна берётся анизотропным uv1_scale, то есть в МИРОВЫХ осях, и
## ограничение названо честно ещё спайком: на дуге и на путях под углом волокно
## поедет. Правильное лечение — свои UV на коробке шпалы; это отдельная работа.
const SLEEPER_GRAIN := 0.045   # м — период волокна ПОПЕРЁК бруса
const SLEEPER_RUN := 2.2       # м — период рисунка ВДОЛЬ бруса


static func ballast_material() -> StandardMaterial3D:
	var period := BALLAST_STONE * (float(BALLAST_TILE) / BALLAST_CELL_PX)
	var m := StandardMaterial3D.new()
	m.albedo_color = C_BALLAST
	m.roughness = 1.0
	m.albedo_texture = _stone_texture(false)
	m.normal_enabled = true
	m.normal_texture = _stone_texture(true)
	m.normal_scale = 1.5
	m.uv1_triplanar = true
	m.uv1_scale = Vector3.ONE / period
	return m


static func _stone_texture(as_normal: bool) -> NoiseTexture2D:
	var noise := FastNoiseLite.new()
	noise.seed = 0xB0115
	noise.noise_type = FastNoiseLite.TYPE_CELLULAR
	noise.cellular_return_type = FastNoiseLite.RETURN_DISTANCE2_SUB
	noise.cellular_distance_function = FastNoiseLite.DISTANCE_EUCLIDEAN
	noise.cellular_jitter = 1.0
	noise.frequency = 1.0 / BALLAST_CELL_PX
	noise.fractal_type = FastNoiseLite.FRACTAL_NONE
	var nt := NoiseTexture2D.new()
	nt.width = BALLAST_TILE
	nt.height = BALLAST_TILE
	nt.seamless = false
	nt.noise = noise
	if as_normal:
		nt.as_normal_map = true
		nt.bump_strength = 24.0
	else:
		var ramp := Gradient.new()
		ramp.offsets = PackedFloat32Array([0.0, 0.30, 0.75, 1.0])
		ramp.colors = PackedColorArray([
			Color(0.62, 0.62, 0.62), Color(0.82, 0.82, 0.82),
			Color(1.00, 1.00, 1.00), Color(1.00, 1.00, 1.00)])
		nt.color_ramp = ramp
	return nt


static func sleeper_material() -> StandardMaterial3D:
	var m := StandardMaterial3D.new()
	m.albedo_color = C_SLEEPER
	m.roughness = 0.82
	m.albedo_texture = _wood_texture(false)
	m.normal_enabled = true
	m.normal_texture = _wood_texture(true)
	# Дерево почти плоское: рельеф даёт трещина, а не волокно.
	m.normal_scale = 0.7
	m.uv1_triplanar = true
	m.uv1_scale = Vector3(1.0 / SLEEPER_GRAIN, 1.0 / SLEEPER_RUN, 1.0 / SLEEPER_RUN)
	return m


static func _wood_texture(as_normal: bool) -> NoiseTexture2D:
	var noise := FastNoiseLite.new()
	noise.seed = 0x0D06
	noise.noise_type = FastNoiseLite.TYPE_PERLIN
	noise.frequency = 0.09
	noise.fractal_octaves = 4
	var nt := NoiseTexture2D.new()
	nt.width = 256
	nt.height = 256
	nt.seamless = false
	nt.noise = noise
	if as_normal:
		nt.as_normal_map = true
		nt.bump_strength = 6.0
	else:
		# Резкий тёмный край — это и есть трещина: рампа с уступом, а не плавный
		# градиент, иначе выходит та же серая муть.
		var ramp := Gradient.new()
		ramp.offsets = PackedFloat32Array([0.0, 0.16, 0.30, 1.0])
		ramp.colors = PackedColorArray([
			Color(0.34, 0.34, 0.34), Color(0.62, 0.62, 0.62),
			Color(1.00, 1.00, 1.00), Color(1.00, 1.00, 1.00)])
		nt.color_ramp = ramp
	return nt


## flat_material — материал плоского слоя пути.
##
## Проверка глубины выключена по причине из шапки, а запись глубины — заодно:
## слои лежат в одной плоскости (все на отметке оси), и запись без проверки
## рвала бы их на куски по порядку отрисовки.
## ПРОВЕРКА ГЛУБИНЫ БОЛЬШЕ НЕ ОТКЛЮЧАЕТСЯ, и это отмена лечения, а не новая
## настройка. Отключали её потому, что путь и земля лежали на ОДНОЙ отметке и
## дрались за z-буфер: сервер сажал землю на отметку оси. С 2026-08-12 земля
## лежит на formation_to_rail_top ниже (на затравке 0.68 м), драться не с чем.
##
## Оставленное отключение стало вредным ровно тогда, когда появился вид с оси:
## галочки крестовин и нити рисовались ПОВЕРХ ВСЕГО и висели сквозь рельеф
## красными полосами на горизонте. Плоскому виду сверху это было незаметно.
##
## render_priority сохранён: он разводит слои, лежащие в одной плоскости, а
## таких ещё хватает — нити и галочки толщины не имеют ни в каком смысле.
static func flat_material(colour: Color, priority: int, unshaded: bool = false) -> StandardMaterial3D:
	var m := StandardMaterial3D.new()
	m.albedo_color = colour
	m.cull_mode = BaseMaterial3D.CULL_DISABLED
	m.render_priority = priority
	if unshaded:
		m.shading_mode = BaseMaterial3D.SHADING_MODE_UNSHADED
	return m
