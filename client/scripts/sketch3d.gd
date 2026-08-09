extends Node3D
## Вид W7 — 3D-вид станции, на который можно смотреть (развитие эскиза W3D).
## Параметрический путь в 3D без единого художественного ассета. Данные —
## напрямую из contract/render_geometry.golden.json (сервер не нужен), разбор —
## готовым geometry_parser.gd, чистая геометрия — готовым geometry_math.gd.
##
## Плоскость пути — XZ, Y вверх: серверные координаты плана (x, y) ->
## (x, 0, y). Переворот 2D-мира (World.scale.y = -1) здесь НЕ нужен.
##
## Что добавлено к эскизу W3D (это и есть вид «на который можно смотреть»):
##   - земля: большая плоскость с процедурной текстурой под путём (рельеф
##     Terrain3D не используется: проект рендерится в GL Compatibility, а
##     Terrain3D требует Vulkan/Forward+; на llvmpipe его клипмапы не считаются);
##   - платформа PLAT_MAIN (trackside, E_MAIN 40..100) с жёлтой кромкой и
##     тупиковые упоры (buffer_stop) на концах элементов без продолжения;
##   - заглушки торцов экструзий (балласт, рельсы, крестовины, платформа);
##   - материалы и свет: небо, теневой ключевой источник, металлические рельсы,
##     шум на земле — балласт/шпалы/рельсы различимы и сверху, и под углом;
##   - интерактивная камера: ЛКМ — орбита, СКМ/ПКМ — панорама, колесо — зум,
##     P — орто/перспектива, L — эталонная коробка локомотива (16×3 м).
##
## Меши: балласт — призма с откосом вдоль оси; шпалы — коробки поперёк оси по
## рецепту run'ов (шаг, фаза, полуоткрытое правило); рельсы — прямоугольные
## нитки на ±gauge/2. Высоты слоёв в контракте отсутствуют (z=0 везде) —
## стилевые константы ниже, они же — «выдуманные числа» в отчёте эскиза.

const GM := preload("res://scripts/geometry_math.gd")
const Parser := preload("res://scripts/geometry_parser.gd")

## --- стилевые константы (в контракте их нет: вертикальный профиль нулевой) ---
const BALLAST_DEPTH := 0.30      # м — толщина балласта
const BALLAST_ADD := 0.45        # м — откос 1:1.5: низ шире верха на 0.45 на борт
const SLEEPER_H := 0.20          # м — высота шпалы
const SLEEPER_LIFT := 0.01       # м — зазор над балластом (анти z-fight)
const RAIL_H := 0.16             # м — высота рельса (прямоугольный профиль)
const RAIL_W := 0.10             # м — ширина рельса
const RAIL_LIFT := 0.01          # м — зазор над шпалой (анти z-fight)
const BRANCH_LIFT := 0.01        # м — подъём геометрии ветвей стрелки (анти z-fight)
const FROG_LEN := 1.2            # м — длина заглушки крестовины вдоль касательной
const FROG_W := 0.12             # м — ширина заглушки крестовины

## --- W7: земля, платформа, упоры, эталон ---
const GROUND_Y := -0.32          # м — уровень земли (низ балласта -0.30)
const GROUND_HALF := 500.0       # м — полуразмер плоскости земли
const PLATFORM_OFFSET := 1.75    # м — ближняя кромка платформы от оси пути
const PLATFORM_WIDTH := 3.0      # м — ширина платформы (как в world.gd: 2D-клиент)
const PLATFORM_H_RAIL := 0.55    # м — высота верха платформы над головкой рельса
const PLATFORM_T := 0.35         # м — толщина плиты платформы
const PLATFORM_EDGE_W := 0.15    # м — ширина жёлтой кромки у ближнего края
const BUFFER_W := 0.30           # м — глубина упора вдоль пути
const BUFFER_H := 1.10           # м — высота упора (над низом балласта)
const BUFFER_SINK := 0.10        # м — заглубление низа упора в балласт
const LOCO_L := 16.0             # м — длина эталонной коробки (локомотив)
const LOCO_W := 3.0              # м — ширина
const LOCO_H := 3.6              # м — высота кузова
const LOCO_ROOF := 0.35          # м — толщина крыши
const LOCO_LAT := 5.0            # м — боковое смещение от оси пути (левая сторона)
const LOCO_U := 172.0            # м — координата u на E_MAIN (прямая после дуги)

const COL_BALLAST := Color(0.62, 0.60, 0.56)
const COL_SLEEPER := Color(0.30, 0.26, 0.22)
const COL_RAIL := Color(0.72, 0.73, 0.76)
const COL_FROG := Color(0.80, 0.60, 0.45)   # тёплая сталь — заглушка различима
const COL_GROUND := Color(0.46, 0.42, 0.34) # грязь/дерн с шумовой текстурой
const COL_PLATFORM := Color(0.80, 0.80, 0.78)
const COL_PLATFORM_EDGE := Color(0.85, 0.72, 0.15)
const COL_BUFFER := Color(0.78, 0.16, 0.10) # упорный красный
const COL_LOCO := Color(0.16, 0.20, 0.30)
const COL_LOCO_ROOF := Color(0.62, 0.64, 0.68)

## Путь к эталону; снимок-скрипт может переопределить аргументом --geometry.
static var geometry_path := ""
static var shot_focus := Vector2.INF
static var shot_size := 0.0
static var shot_azimuth := 45.0
static var shot_elev := 35.0
static var hide_loco := false

const CAM_FOV := 50.0            # градусы — перспективная проекция

var _camera: Camera3D
var _aabb := AABB()
var _geometry := { "elements": [] }
var _elements := {}

## Состояние камеры (интерактив): фокус на земле, азимут/высота, размер орто.
var _cam_focus := Vector3.ZERO
var _cam_az := 45.0
var _cam_elev := 35.0
var _cam_size := 100.0
var _cam_dist := 200.0
var _cam_ortho := true

var _loco_node: Node3D

func _ready() -> void:
	_camera = get_node("Camera3D")
	var parsed := Parser.parse(_load_golden())
	if not parsed.ok:
		push_error("W3D: %s" % parsed.error)
		return
	_geometry = parsed.geometry
	for el in _geometry.elements:
		_elements[el.id] = el
	_rebuild()
	_add_environment()
	configure_camera(shot_focus, shot_size, shot_azimuth, shot_elev)

func _load_golden() -> String:
	var path := geometry_path
	if path == "":
		var client_dir := ProjectSettings.globalize_path("res://").trim_suffix("/")
		path = client_dir.get_base_dir().path_join("contract/render_geometry.golden.json")
	var f := FileAccess.open(path, FileAccess.READ)
	if f == null:
		push_error("W3D: не открыть %s" % path)
		return "{}"
	return f.get_as_text()

## --- сборка: один ArrayMesh на слой ---
func _rebuild() -> void:
	var ballast := SurfaceTool.new()
	var sleepers := SurfaceTool.new()
	var rails := SurfaceTool.new()
	var frog := SurfaceTool.new()
	var platform := SurfaceTool.new()
	var platform_edge := SurfaceTool.new()
	var buffers := SurfaceTool.new()
	ballast.begin(Mesh.PRIMITIVE_TRIANGLES)
	sleepers.begin(Mesh.PRIMITIVE_TRIANGLES)
	rails.begin(Mesh.PRIMITIVE_TRIANGLES)
	frog.begin(Mesh.PRIMITIVE_TRIANGLES)
	platform.begin(Mesh.PRIMITIVE_TRIANGLES)
	platform_edge.begin(Mesh.PRIMITIVE_TRIANGLES)
	buffers.begin(Mesh.PRIMITIVE_TRIANGLES)
	var pitch := _sleeper_pitch()
	_add_ground()
	for el in _geometry.elements:
		var lift := BRANCH_LIFT if el.has("role") else 0.0
		_add_ballast(ballast, el, lift)
		_add_rails(rails, el, lift)
		if el.has("role"):
			_add_branch_sleepers(sleepers, el, pitch, lift)
	for run in _geometry.get("construction_runs", []):
		_add_run_sleepers(sleepers, run)
	for f in _geometry.get("features", []):
		if f.kind == "frog":
			_add_frog(frog, f)
	_add_platforms(platform, platform_edge)
	_add_buffer_stops(buffers)
	_add_loco_scale()
	_commit(ballast, _mat(COL_BALLAST, 0.95))
	_commit(sleepers, _mat(COL_SLEEPER, 0.90))
	_commit(rails, _mat(COL_RAIL, 0.35, 0.6))
	_commit(frog, _mat(COL_FROG, 0.40, 0.5))
	_commit(platform, _mat(COL_PLATFORM, 0.85))
	_commit(platform_edge, _mat(COL_PLATFORM_EDGE, 0.70))
	_commit(buffers, _mat(COL_BUFFER, 0.60))

## --- материалы ---
func _mat(albedo: Color, roughness: float, metallic: float = 0.0, noisy: bool = false) -> StandardMaterial3D:
	var m := StandardMaterial3D.new()
	m.albedo_color = albedo
	m.roughness = roughness
	m.metallic = metallic
	if noisy:
		var noise := FastNoiseLite.new()
		noise.seed = 0xC1EA
		noise.frequency = 0.05
		var nt := NoiseTexture2D.new()
		nt.width = 256
		nt.height = 256
		nt.seamless = true
		nt.noise = noise
		var ramp := Gradient.new()
		ramp.offsets = PackedFloat32Array([0.0, 1.0])
		ramp.colors = PackedColorArray([Color(0.85, 0.85, 0.85), Color(1.0, 1.0, 1.0)])
		nt.color_ramp = ramp
		m.albedo_texture = nt
		m.uv1_scale = Vector3(16.0, 16.0, 1.0)
	return m

func _commit(tool: SurfaceTool, mat: StandardMaterial3D, parent: Node = null) -> MeshInstance3D:
	var mesh := tool.commit()
	if mesh == null:
		return null
	var mi := MeshInstance3D.new()
	mi.mesh = mesh
	mi.material_override = mat
	if parent == null:
		parent = self
	parent.add_child(mi)
	_aabb = _aabb.merge(mesh.get_aabb())
	return mi

func _add_ballast(tool: SurfaceTool, el: Dictionary, lift: float) -> void:
	var typ := _type()
	if typ.is_empty():
		return
	var hw := float(typ.ballast.half_width)
	var section := PackedVector3Array([
		Vector3(-hw - BALLAST_ADD, -BALLAST_DEPTH + lift, 0.0),
		Vector3(-hw, lift, 0.0),
		Vector3(hw, lift, 0.0),
		Vector3(hw + BALLAST_ADD, -BALLAST_DEPTH + lift, 0.0),
	])
	_extrude(tool, _chain3d(el), section, true)

func _add_rails(tool: SurfaceTool, el: Dictionary, lift: float) -> void:
	var typ := _type()
	if typ.is_empty():
		return
	var g := float(typ.gauge) * 0.5
	var y0 := RAIL_LIFT + SLEEPER_H + lift
	var section := PackedVector3Array([
		Vector3(-RAIL_W * 0.5, y0, 0.0),
		Vector3(-RAIL_W * 0.5, y0 + RAIL_H, 0.0),
		Vector3(RAIL_W * 0.5, y0 + RAIL_H, 0.0),
		Vector3(RAIL_W * 0.5, y0, 0.0),
	])
	for sign in [1.0, -1.0]:
		_extrude(tool, _offset_chain3d(el, g * sign), section, true)

func _add_frog(tool: SurfaceTool, f: Dictionary) -> void:
	var addr: Dictionary = f.addresses[0]
	var tangent: Dictionary = addr.tangent
	var dir := Vector2(float(tangent.x), float(tangent.y)).normalized()
	var c := Vector2(float(f.point.x), float(f.point.y))
	var c0 := c - dir * (FROG_LEN * 0.5)
	var c1 := c + dir * (FROG_LEN * 0.5)
	var y0 := RAIL_LIFT + SLEEPER_H
	var section := PackedVector3Array([
		Vector3(-FROG_W * 0.5, y0, 0.0),
		Vector3(-FROG_W * 0.5, y0 + RAIL_H, 0.0),
		Vector3(FROG_W * 0.5, y0 + RAIL_H, 0.0),
		Vector3(FROG_W * 0.5, y0, 0.0),
	])
	_extrude(tool, PackedVector3Array([
		Vector3(c0.x, 0.0, c0.y), Vector3(c1.x, 0.0, c1.y)]), section, true)

## Шпалы по рецепту run'а (спека §4): моменты phase + n*pitch из run_length,
## аналитическая pose(u) — как в 2D-мире (world.gd._draw_run_sleepers).
func _add_run_sleepers(tool: SurfaceTool, run: Dictionary) -> void:
	var typ := _type()
	if typ.is_empty():
		return
	var pitch := float(typ.sleeper.pitch)
	var length := GM.run_length(run)
	var half := float(typ.sleeper.length) * 0.5
	var width := float(typ.sleeper.width)
	for r in GM.run_sleeper_offsets(float(run.phase), pitch, length):
		var local := GM.run_to_local(run, r)
		if not local.ok:
			continue
		var el: Dictionary = _elements.get(local.element, {})
		if el.is_empty():
			continue
		var pose := GM.pose_at(el.start, el.primitives, local.u)
		if not pose.ok:
			continue
		_add_sleeper_box(tool, Vector2(pose.x, pose.y), pose.heading, half, width, 0.0)

## Шпалы на ветвях стрелки: run'ы их не покрывают (спека §4 — проходы устройств
## не регулярная решётка), контракт решётки стрелки не даёт. ВЫДУМАНО: тот же
## шаг/фаза, что у run'ов, вдоль каждой ветви от острия. Шпала в u=0 у
## отклонения пропущена — она совпадает с общей шпалой прямого пути.
func _add_branch_sleepers(tool: SurfaceTool, el: Dictionary, pitch: float, lift: float) -> void:
	var typ := _type()
	if typ.is_empty():
		return
	var half := float(typ.sleeper.length) * 0.5
	var width := float(typ.sleeper.width)
	var is_diverging: bool = el.role.branch == "diverging"
	for u in GM.run_sleeper_offsets(0.0, pitch, _element_length(el)):
		if is_diverging and u < 0.001:
			continue
		var pose := GM.pose_at(el.start, el.primitives, u)
		if not pose.ok:
			continue
		_add_sleeper_box(tool, Vector2(pose.x, pose.y), pose.heading, half, width, lift)

func _add_sleeper_box(tool: SurfaceTool, center: Vector2, heading: float,
		half_len: float, width: float, lift: float) -> void:
	var d := Vector2(cos(heading), sin(heading))
	var l := Vector2(-d.y, d.x)
	var c := Vector3(center.x, SLEEPER_LIFT + lift, center.y)
	var u := Vector3(l.x, 0.0, l.y) * half_len          # вдоль шпалы
	var v := Vector3(d.x, 0.0, d.y) * (width * 0.5)     # вдоль пути
	var w := Vector3(0.0, SLEEPER_H, 0.0)
	var top := [
		c + u + v + w, c - u + v + w, c - u - v + w, c + u - v + w,
	]
	var bot := [
		c + u + v, c - u + v, c - u - v, c + u - v,
	]
	_quad(tool, top[0], top[1], top[2], top[3])   # верх
	_quad(tool, bot[3], bot[2], top[2], top[3])   # борт -v
	_quad(tool, bot[2], bot[1], top[1], top[2])   # торец -u
	_quad(tool, bot[1], bot[0], top[0], top[1])   # борт +v
	_quad(tool, bot[0], bot[3], top[3], top[0])   # торец +u

## Параллелепипед по ортонормированным осям (a — вдоль, b — поперёк, up — вверх).
## Обмотка совпадает с проверенной обмоткой _add_sleeper_box (верх/борта) плюс
## низ.
func _add_obox(tool: SurfaceTool, c: Vector3, a: Vector3, b: Vector3, up: Vector3,
		ha: float, hb: float, hu: float) -> void:
	var t := [
		c + a * ha + b * hb + up * hu, c - a * ha + b * hb + up * hu,
		c - a * ha - b * hb + up * hu, c + a * ha - b * hb + up * hu,
	]
	var bo := [
		c + a * ha + b * hb - up * hu, c - a * ha + b * hb - up * hu,
		c - a * ha - b * hb - up * hu, c + a * ha - b * hb - up * hu,
	]
	_quad(tool, t[0], t[1], t[2], t[3])          # верх
	_quad(tool, bo[3], bo[2], t[2], t[3])        # борт -b
	_quad(tool, bo[2], bo[1], t[1], t[2])        # торец -a
	_quad(tool, bo[1], bo[0], t[0], t[1])        # борт +b
	_quad(tool, bo[0], bo[3], t[3], t[0])        # торец +a
	_quad(tool, bo[3], bo[2], bo[1], bo[0])      # низ

## Экструзия сечения (точки в осях lat×up, порядок: мин-lat-низ, мин-lat-верх,
## макс-lat-верх, макс-lat-низ) вдоль цепочки: по грани на пару точек сечения,
## включая замыкающую (низ). caps=true — торцевые заглушки (закрытые концы).
## Обмотка выбрана так, что геометрическая нормаль (p1-p0)×(p2-p0) смотрит
## наружу на всех гранях; освещение поправляет _quad (см. там).
func _extrude(tool: SurfaceTool, chain: PackedVector3Array, section: PackedVector3Array, caps: bool = false) -> void:
	var n := section.size()
	var first_lat := Vector3.ZERO
	var last_lat := Vector3.ZERO
	for i in chain.size() - 1:
		var a := chain[i]
		var b := chain[i + 1]
		var dir := b - a
		if dir.length() < 1e-9:
			continue
		dir = dir.normalized()
		var lat := dir.cross(Vector3.UP)
		if first_lat == Vector3.ZERO:
			first_lat = lat
		last_lat = lat
		for j in n:
			var k := (j + 1) % n
			var p0 := a + lat * section[j].x + Vector3.UP * section[j].y
			var p1 := a + lat * section[k].x + Vector3.UP * section[k].y
			var p2 := b + lat * section[k].x + Vector3.UP * section[k].y
			var p3 := b + lat * section[j].x + Vector3.UP * section[j].y
			_quad(tool, p0, p1, p2, p3)
	if caps and n >= 3 and chain.size() >= 2:
		_cap(tool, chain[0], first_lat, section, true)
		_cap(tool, chain[chain.size() - 1], last_lat, section, false)

func _cap(tool: SurfaceTool, a: Vector3, lat: Vector3, section: PackedVector3Array, flip: bool) -> void:
	var pts := PackedVector3Array()
	for j in section.size():
		pts.append(a + lat * section[j].x + Vector3.UP * section[j].y)
	if flip:
		_quad(tool, pts[0], pts[3], pts[2], pts[1])
	else:
		_quad(tool, pts[0], pts[1], pts[2], pts[3])

## ОБХОД БАГА РЕНДЕРА: на этом стеке (Godot 4.7.1 nixpkgs + llvmpipe, GL
## Compatibility) нормали мешей SurfaceTool рендерятся ИНВЕРТИРОВАННЫМИ —
## верхние грани (нормаль +Y) ведут себя как нижние: гаснут под светом сверху
## и загораются под светом снизу. PlaneMesh (земля) рендерится правильно.
## Эмпирически проверено изолированным тестом: нормаль надо класть
## ПРОТИВОПОЛОЖНУЮ геометрической (set_normal(-n)), тогда освещение и
## обмотка согласуются (верх светлый под солнцем сверху). Касается только
## освещения: обмотка треугольников при этом остаётся фронтальной.
func _quad(tool: SurfaceTool, p0: Vector3, p1: Vector3, p2: Vector3, p3: Vector3) -> void:
	var n := (p1 - p0).cross(p2 - p0).normalized()
	tool.set_normal(-n)
	tool.add_vertex(p0)
	tool.add_vertex(p1)
	tool.add_vertex(p2)
	tool.add_vertex(p0)
	tool.add_vertex(p2)
	tool.add_vertex(p3)

## --- земля ---
func _add_ground() -> void:
	var plane := PlaneMesh.new()
	plane.size = Vector2(GROUND_HALF * 2.0, GROUND_HALF * 2.0)
	var mi := MeshInstance3D.new()
	mi.mesh = plane
	mi.position = Vector3(190.0, GROUND_Y, 6.0)
	mi.material_override = _mat(COL_GROUND, 1.0, 0.0, true)
	mi.cast_shadow = GeometryInstance3D.SHADOW_CASTING_SETTING_OFF
	add_child(mi)

## --- платформы (trackside kind=platform) ---
## Полоса вдоль спана элемента: от ближней кромки (offset) до дальней
## (offset + width) со стороны side. Сторона «right» — отрицательная латераль
## (2D-клиент рисует её тем же знаком), у восточного хода это сторона двора.
func _add_platforms(slab: SurfaceTool, edge: SurfaceTool) -> void:
	for obj in _geometry.get("trackside", []):
		if obj.kind != "platform":
			printerr("VIEW3D: путевой объект «%s» вида «%s» не рисуется (умею platform)" % [obj.id, obj.kind])
			continue
		var side: String = obj.get("side", "")
		var side_sign := 1.0
		if side == "left":
			side_sign = 1.0
		elif side == "right":
			side_sign = -1.0
		else:
			printerr("VIEW3D: платформа «%s»: неизвестная сторона «%s»" % [obj.id, side])
			continue
		var yb := _rail_top() + PLATFORM_H_RAIL - PLATFORM_T
		var yt := _rail_top() + PLATFORM_H_RAIL
		for span in obj.spans:
			var el: Dictionary = _elements.get(span.element, {})
			if el.is_empty():
				printerr("VIEW3D: платформа «%s»: неизвестный элемент «%s»" % [obj.id, span.element])
				continue
			var chain := _range_chain(el, float(span.from), float(span.to))
			if chain.size() < 2:
				continue
			var lat_in := side_sign * PLATFORM_OFFSET
			var lat_out := side_sign * (PLATFORM_OFFSET + PLATFORM_WIDTH)
			var lo := minf(lat_in, lat_out)
			var hi := maxf(lat_in, lat_out)
			_extrude(slab, chain, PackedVector3Array([
				Vector3(lo, yb, 0.0), Vector3(lo, yt, 0.0),
				Vector3(hi, yt, 0.0), Vector3(hi, yb, 0.0)]), true)
			var e_in := lat_in
			var e_out := lat_in + side_sign * PLATFORM_EDGE_W
			var elo := minf(e_in, e_out)
			var ehi := maxf(e_in, e_out)
			_extrude(edge, chain, PackedVector3Array([
				Vector3(elo, yb, 0.0), Vector3(elo, yt, 0.0),
				Vector3(ehi, yt, 0.0), Vector3(ehi, yb, 0.0)]), true)

## --- тупиковые упоры (buffer_stop) ---
## Контрактная trackside вида buffer_stop в эталоне отсутствует (фикстура
## FIX_ST их в render-geometry не шлёт), но топология их подразумевает: конец
## элемента, не соединённый с началом другого элемента, — тупик. По
## fixture_station.json это N_STOP_MAIN/N_STOP_SIDING/N_STOP_STUB (буферы),
## N_BOUNDARY — граница карты. Упор — красная коробка поперёк пути в конечной
## точке элемента, от балласта до высоты сцепки.
func _add_buffer_stops(tool: SurfaceTool) -> void:
	var typ := _type()
	if typ.is_empty():
		return
	var half_across := float(typ.gauge) * 0.5 + 0.20
	for el in _geometry.elements:
		if _element_connected_at_end(el):
			continue
		var pose := GM.pose_at(el.start, el.primitives, _element_length(el))
		if not pose.ok:
			continue
		var dir := Vector3(cos(pose.heading), 0.0, sin(pose.heading))
		var lat := Vector3(-dir.z, 0.0, dir.x)
		var c := Vector3(pose.x, 0.0, pose.y) + dir * (BUFFER_W * 0.5)
		var cy := -BUFFER_SINK + BUFFER_H * 0.5
		_add_obox(tool, c + Vector3(0.0, cy, 0.0), dir, lat, Vector3.UP,
			BUFFER_W * 0.5, half_across, BUFFER_H * 0.5)

func _element_connected_at_end(el: Dictionary) -> bool:
	var pose := GM.pose_at(el.start, el.primitives, _element_length(el))
	if not pose.ok:
		return true
	var end2 := Vector2(pose.x, pose.y)
	for other in _geometry.elements:
		if other.id == el.id:
			continue
		var s := Vector2(float(other.start.plan.x), float(other.start.plan.y))
		if s.distance_to(end2) < 0.05:
			return true
	return false

## --- эталонная коробка локомотива (16×3 м), отключаемая клавишей L ---
func _add_loco_scale() -> void:
	var root_node := Node3D.new()
	root_node.name = "LocoScale"
	_loco_node = root_node
	var body := SurfaceTool.new()
	var roof := SurfaceTool.new()
	body.begin(Mesh.PRIMITIVE_TRIANGLES)
	roof.begin(Mesh.PRIMITIVE_TRIANGLES)
	var el: Dictionary = _elements.get("E_MAIN", {})
	if not el.is_empty():
		var pose := GM.pose_at(el.start, el.primitives, LOCO_U)
		if pose.ok:
			var dir := Vector3(cos(pose.heading), 0.0, sin(pose.heading))
			var lat := Vector3(-dir.z, 0.0, dir.x)
			var base := Vector3(pose.x, GROUND_Y, pose.y) + lat * LOCO_LAT
			_add_obox(body, base + Vector3(0.0, LOCO_H * 0.5, 0.0), dir, lat, Vector3.UP,
				LOCO_L * 0.5, LOCO_W * 0.5, LOCO_H * 0.5)
			_add_obox(roof, base + Vector3(0.0, LOCO_H + LOCO_ROOF * 0.5, 0.0), dir, lat, Vector3.UP,
				LOCO_L * 0.5, LOCO_W * 0.5, LOCO_ROOF * 0.5)
	_commit(body, _mat(COL_LOCO, 0.50, 0.2), root_node)
	_commit(roof, _mat(COL_LOCO_ROOF, 0.40, 0.3), root_node)
	root_node.visible = not hide_loco
	add_child(root_node)

## --- цепочки ---
func _chain3d(el: Dictionary) -> PackedVector3Array:
	return _chain3d_from(GM.sample_chain(el.start, el.primitives))

func _offset_chain3d(el: Dictionary, offset: float) -> PackedVector3Array:
	return _chain3d_from(GM.offset_polyline(GM.sample_chain(el.start, el.primitives), offset))

func _chain3d_from(pts: PackedVector2Array) -> PackedVector3Array:
	var out := PackedVector3Array()
	for p in pts:
		out.append(Vector3(p.x, 0.0, p.y))
	return out

## Осевая цепочка элемента на [from..to] координаты u, шаг 0.5 м, по
## аналитической pose(u) (не тесселяции) — платформа лежит точно на пути.
func _range_chain(el: Dictionary, from: float, to: float) -> PackedVector3Array:
	var out := PackedVector3Array()
	if to <= from:
		return out
	var u := from
	while u <= to + 1e-6:
		var pose := GM.pose_at(el.start, el.primitives, u)
		if not pose.ok:
			break
		out.append(Vector3(pose.x, 0.0, pose.y))
		u += 0.5
	var last := out[out.size() - 1] if not out.is_empty() else Vector3.INF
	var fin := GM.pose_at(el.start, el.primitives, to)
	if fin.ok:
		var q := Vector3(fin.x, 0.0, fin.y)
		if last.distance_to(q) > 1e-6:
			out.append(q)
	return out

## --- контрактные индексы ---
func _type() -> Dictionary:
	var tts: Array = _geometry.get("track_types", [])
	if tts.is_empty():
		return {}
	return tts[0]

func _sleeper_pitch() -> float:
	var typ := _type()
	if typ.is_empty():
		return 0.6
	return float(typ.sleeper.pitch)

func _element_length(el: Dictionary) -> float:
	var total := 0.0
	for p in el.primitives:
		total += float(p.length)
	return total

func _rail_top() -> float:
	return RAIL_LIFT + SLEEPER_H + RAIL_H

## --- свет и фон ---
## Небо (процедурный градиент) с амбиентом от него, теневой ключевой источник
## с северо-востока и слабая заливка с юго-запада. Оба направленных света
## повёрнуты ОТРИЦАТЕЛЬНЫМ X-наклоном — это даёт лучи ВНИЗ (проверено
## эмпирически на llvmpipe; положительный X-наклон уводит лучи вверх и гасит
## верхние грани).
func _add_environment() -> void:
	var env := Environment.new()
	var sky_mat := ProceduralSkyMaterial.new()
	sky_mat.sky_top_color = Color(0.30, 0.42, 0.58)
	sky_mat.sky_horizon_color = Color(0.72, 0.76, 0.82)
	sky_mat.ground_horizon_color = Color(0.62, 0.62, 0.60)
	sky_mat.ground_bottom_color = Color(0.25, 0.25, 0.24)
	var sky := Sky.new()
	sky.sky_material = sky_mat
	env.background_mode = Environment.BG_SKY
	env.sky = sky
	env.ambient_light_source = Environment.AMBIENT_SOURCE_SKY
	env.ambient_light_energy = 0.5
	var we := WorldEnvironment.new()
	we.environment = env
	add_child(we)
	var light := DirectionalLight3D.new()
	light.rotation_degrees = Vector3(-50.0, -35.0, 0.0)
	light.light_energy = 1.0
	light.shadow_enabled = true
	light.directional_shadow_max_distance = 250.0
	add_child(light)
	var fill := DirectionalLight3D.new()
	fill.rotation_degrees = Vector3(-45.0, 145.0, 0.0)
	fill.light_energy = 0.3
	add_child(fill)

## --- камера ---
## focus — центр в метрах плана (Vector2.INF — автоцентр по AABB);
## size — видимая высота орто в метрах (0 — автофит по всей фикстуре);
## azimuth — азимут камеры от +X в плоскости пути, градусы;
## elev — угол над плоскостью пути, градусы (89 ≈ сверху).
func configure_camera(focus: Vector2, size: float, azimuth_deg: float, elev_deg: float) -> void:
	var center := _aabb.get_center()
	_cam_focus = Vector3(center.x, 0.0, center.z)
	if focus != Vector2.INF:
		_cam_focus = Vector3(focus.x, 0.0, focus.y)
	_cam_az = azimuth_deg
	_cam_elev = clampf(elev_deg, 2.0, 89.0)
	_cam_ortho = true
	_cam_size = size if size > 0.0 else 1.0
	_apply_camera()
	if size <= 0.0:
		_cam_size = _fit_size()
		_apply_camera()

func _apply_camera() -> void:
	if _cam_ortho:
		# орто: размер = полная видимая высота; дистанция на вид не влияет,
		# но держим её согласованной, чтобы переключение в перспективу не
		# прыгало по масштабу
		_cam_dist = _cam_size / (2.0 * tan(deg_to_rad(CAM_FOV) * 0.5))
	var az := deg_to_rad(_cam_az)
	var el := deg_to_rad(_cam_elev)
	var horiz := Vector3(cos(az), 0.0, sin(az))
	_camera.position = _cam_focus + (horiz * cos(el) + Vector3.UP * sin(el)) * _cam_dist
	_camera.look_at(_cam_focus, Vector3.UP)
	_camera.projection = Camera3D.PROJECTION_ORTHOGONAL if _cam_ortho else Camera3D.PROJECTION_PERSPECTIVE
	_camera.size = _cam_size
	_camera.fov = CAM_FOV
	_camera.current = true

## Автофит: ортографический размер по проекции AABB на плоскость камеры.
## Использует РЕАЛЬНЫЙ transform камеры (get_camera_transform) — дублировать
## базис вручную нельзя: Transform3D().looking_at() строит базис из начала
## координат, а не из позиции камеры, и фит врёт.
func _fit_size() -> float:
	var inv := _camera.get_camera_transform().affine_inverse()
	var minx := INF
	var maxx := -INF
	var miny := INF
	var maxy := -INF
	for i in 8:
		var q: Vector3 = inv * _aabb.get_endpoint(i)
		minx = minf(minx, q.x)
		maxx = maxf(maxx, q.x)
		miny = minf(miny, q.y)
		maxy = maxf(maxy, q.y)
	var size := Vector2(ProjectSettings.get_setting("display/window/size/viewport_width"),
			ProjectSettings.get_setting("display/window/size/viewport_height"))
	var aspect := size.x / size.y
	return maxf(maxy - miny, (maxx - minx) / aspect) * 1.12

## --- интерактив: орбита (ЛКМ), панорама (СКМ/ПКМ), зум (колесо),
## --- P — орто/перспектива, L — эталонная коробка ---
func _unhandled_input(event: InputEvent) -> void:
	if event is InputEventMouseMotion and (event.button_mask & (MOUSE_BUTTON_MASK_LEFT | MOUSE_BUTTON_MASK_MIDDLE | MOUSE_BUTTON_MASK_RIGHT)) != 0:
		if event.button_mask & MOUSE_BUTTON_MASK_LEFT and not (event.button_mask & (MOUSE_BUTTON_MASK_MIDDLE | MOUSE_BUTTON_MASK_RIGHT)):
			_orbit(event.relative)
		else:
			_pan(event.relative)
		_apply_camera()
		get_viewport().set_input_as_handled()
	elif event is InputEventMouseButton and event.pressed:
		if event.button_index == MOUSE_BUTTON_WHEEL_UP:
			_zoom(-1.0)
			_apply_camera()
			get_viewport().set_input_as_handled()
		elif event.button_index == MOUSE_BUTTON_WHEEL_DOWN:
			_zoom(1.0)
			_apply_camera()
			get_viewport().set_input_as_handled()
	elif event is InputEventKey and event.pressed and not event.echo:
		if event.keycode == KEY_P:
			_toggle_projection()
		elif event.keycode == KEY_L:
			_toggle_loco()
		get_viewport().set_input_as_handled()

func _orbit(rel: Vector2) -> void:
	_cam_az -= rel.x * 0.30
	_cam_elev = clampf(_cam_elev + rel.y * 0.30, 2.0, 89.0)

func _pan(rel: Vector2) -> void:
	var vp := _camera.get_viewport().get_visible_rect().size
	var wpp: float
	if _cam_ortho:
		wpp = _cam_size / vp.y
	else:
		wpp = 2.0 * _cam_dist * tan(deg_to_rad(CAM_FOV) * 0.5) / vp.y
	var basis := _camera.global_transform.basis
	var right := Vector3(basis.x.x, 0.0, basis.x.z).normalized()
	var fwd := Vector3(-basis.z.x, 0.0, -basis.z.z).normalized()
	_cam_focus -= right * rel.x * wpp
	_cam_focus += fwd * rel.y * wpp

func _zoom(dir: float) -> void:
	var f := 0.88 if dir < 0.0 else 1.0 / 0.88
	if _cam_ortho:
		_cam_size = clampf(_cam_size * f, 2.0, 800.0)
	else:
		_cam_dist = clampf(_cam_dist * f, 8.0, 4000.0)

func _toggle_projection() -> void:
	_cam_ortho = not _cam_ortho
	if _cam_ortho:
		_cam_size = clampf(2.0 * _cam_dist * tan(deg_to_rad(CAM_FOV) * 0.5), 2.0, 800.0)
	else:
		_cam_dist = clampf(_cam_size / (2.0 * tan(deg_to_rad(CAM_FOV) * 0.5)), 8.0, 4000.0)
	_apply_camera()
	print("VIEW3D: проекция %s" % ("ортографическая" if _cam_ortho else "перспективная"))

func _toggle_loco() -> void:
	if _loco_node == null:
		return
	_loco_node.visible = not _loco_node.visible
	print("VIEW3D: эталонная коробка %s" % ("показана" if _loco_node.visible else "скрыта"))
