extends Node3D
## Эскиз W3D (не прототип): наш параметрический путь в 3D под ортографической
## камерой, без единого художественного ассета. Данные — напрямую из
## contract/render_geometry.golden.json (сервер не нужен), разбор — готовым
## geometry_parser.gd, чистая геометрия — готовым geometry_math.gd.
##
## Плоскость пути — XZ, Y вверх: серверные координаты плана (x, y) ->
## (x, 0, y). Переворот 2D-мира (World.scale.y = -1) здесь НЕ нужен: 3D и так
## Y-up, станция не зеркальна.
##
## Меши: балласт — призма с откосом вдоль оси; шпалы — коробки поперёк оси по
## рецепту run'ов (шаг, фаза, полуоткрытое правило); рельсы — прямоугольные
## нитки на ±gauge/2. Высоты слоёв в контракте отсутствуют (z=0 везде) —
## стилевые константы ниже, они же — «выдуманные числа» в отчёте.

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

const COL_BALLAST := Color(0.62, 0.60, 0.56)
const COL_SLEEPER := Color(0.30, 0.26, 0.22)
const COL_RAIL := Color(0.72, 0.73, 0.76)
const COL_FROG := Color(0.80, 0.60, 0.45)   # тёплая сталь — заглушка различима

## Путь к эталону; снимок-скрипт может переопределить аргументом --geometry.
static var geometry_path := ""
static var shot_focus := Vector2.INF
static var shot_size := 0.0
static var shot_azimuth := 45.0
static var shot_elev := 35.0

var _camera: Camera3D
var _aabb := AABB()
var _geometry := { "elements": [] }
var _elements := {}

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

## --- сборка: один ArrayMesh на слой (балласт/шпалы/рельсы) ---
func _rebuild() -> void:
	var ballast := SurfaceTool.new()
	var sleepers := SurfaceTool.new()
	var rails := SurfaceTool.new()
	ballast.begin(Mesh.PRIMITIVE_TRIANGLES)
	sleepers.begin(Mesh.PRIMITIVE_TRIANGLES)
	rails.begin(Mesh.PRIMITIVE_TRIANGLES)
	var pitch := _sleeper_pitch()
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
			_add_frog(rails, f)
	_commit(ballast, COL_BALLAST)
	_commit(sleepers, COL_SLEEPER)
	_commit(rails, COL_RAIL)

func _commit(tool: SurfaceTool, color: Color) -> void:
	var mesh := tool.commit()
	if mesh == null:
		return
	var mi := MeshInstance3D.new()
	mi.mesh = mesh
	var mat := StandardMaterial3D.new()
	mat.albedo_color = color
	mat.roughness = 0.85
	mi.material_override = mat
	add_child(mi)
	_aabb = _aabb.merge(mesh.get_aabb())

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
	_extrude(tool, _chain3d(el), section)

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
		_extrude(tool, _offset_chain3d(el, g * sign), section)

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
		Vector3(c0.x, 0.0, c0.y), Vector3(c1.x, 0.0, c1.y)]), section)

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

## Экструзия сечения (точки в осях lat×up, порядок: мин-lat-низ, мин-lat-верх,
## макс-lat-верх, макс-lat-низ) вдоль цепочки: по грани на пару точек сечения,
## включая замыкающую (низ). Направление обхода выбрано так, что нормали
## наружу на верхних и боковых гранях (проверено алгеброй и снимками).
func _extrude(tool: SurfaceTool, chain: PackedVector3Array, section: PackedVector3Array) -> void:
	var n := section.size()
	for i in chain.size() - 1:
		var a := chain[i]
		var b := chain[i + 1]
		var dir := b - a
		if dir.length() < 1e-9:
			continue
		dir = dir.normalized()
		var lat := dir.cross(Vector3.UP)
		for j in n:
			var k := (j + 1) % n
			var p0 := a + lat * section[j].x + Vector3.UP * section[j].y
			var p1 := a + lat * section[k].x + Vector3.UP * section[k].y
			var p2 := b + lat * section[k].x + Vector3.UP * section[k].y
			var p3 := b + lat * section[j].x + Vector3.UP * section[j].y
			_quad(tool, p0, p1, p2, p3)

func _quad(tool: SurfaceTool, p0: Vector3, p1: Vector3, p2: Vector3, p3: Vector3) -> void:
	var n := (p1 - p0).cross(p2 - p0).normalized()
	tool.set_normal(n)
	tool.add_vertex(p0)
	tool.add_vertex(p1)
	tool.add_vertex(p2)
	tool.add_vertex(p0)
	tool.add_vertex(p2)
	tool.add_vertex(p3)

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

## --- свет и фон (GL Compatibility) ---
func _add_environment() -> void:
	var env := Environment.new()
	env.background_mode = Environment.BG_COLOR
	env.background_color = Color(0.78, 0.80, 0.84)
	env.ambient_light_source = Environment.AMBIENT_SOURCE_COLOR
	env.ambient_light_color = Color.WHITE
	env.ambient_light_energy = 0.35
	var we := WorldEnvironment.new()
	we.environment = env
	add_child(we)
	var light := DirectionalLight3D.new()
	light.rotation_degrees = Vector3(50.0, -35.0, 0.0)
	light.light_energy = 0.75
	add_child(light)

## --- ортографическая камера ---
## focus — центр в метрах плана (Vector2.INF — автоцентр по AABB);
## size — половина видимой высоты в метрах (0 — автофит);
## azimuth — азимут камеры от +X в плоскости пути, градусы;
## elev — угол над плоскостью пути, градусы (89 ≈ сверху).
func configure_camera(focus: Vector2, size: float, azimuth_deg: float, elev_deg: float) -> void:
	var az := deg_to_rad(azimuth_deg)
	var el := deg_to_rad(clampf(elev_deg, 0.0, 89.0))
	var center := _aabb.get_center()
	var target := Vector3(center.x, 0.0, center.z)
	if focus != Vector2.INF:
		target = Vector3(focus.x, 0.0, focus.y)
	var dist := maxf(_aabb.size.length() * 3.0, 50.0)
	var horiz := Vector3(cos(az), 0.0, sin(az))
	_camera.position = target + (horiz * cos(el) + Vector3.UP * sin(el)) * dist
	_camera.look_at(target, Vector3.UP)
	_camera.projection = Camera3D.PROJECTION_ORTHOGONAL
	_camera.current = true
	if size > 0.0:
		_camera.size = size
	else:
		_camera.size = _fit_size()

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
