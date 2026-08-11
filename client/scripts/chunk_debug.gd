extends Node3D
## ОТЛАДОЧНЫЙ СЛОЙ: как мир будет порезан на чанки. Включается F3.
##
## Зачем он есть до самих чанков. Контракт нарезки (сторона, уровни, радиус
## запроса) — это несколько чисел, и на бумаге они выглядят одинаково разумно
## при любом значении. На местности видно сразу: 256 м — это станция целиком
## или четверть горловины, попадает ли посёлок в один чанк, сколько их придётся
## тащить при панораме. Слой рисует ПРЕДЛОЖЕННУЮ сетку, а не полученную с
## сервера: чанков сервер пока не отдаёт, и в этом весь смысл — посмотреть на
## разбиение до того, как оно застынет в протоколе.
##
## Сетка кладётся ПО РЕЛЬЕФУ, а не плоскостью на нуле: плоская сетка над
## холмом уезжает под землю и врёт про то, какой чанк где кончается.

## Сторона чанка нулевого уровня. 256 = 8 x 32, то есть чанки травы
## (GRASS_CHUNK в spike_world.gd) ложатся внутрь без остатка — это не
## совпадение, а требование: две сетки с несоизмеримым шагом дают на стыке
## муар из полупустых ячеек.
const CHUNK := 256.0

## Радиусы уровней подробности, метры. Внутри первого — уровень 0 (шаг 5 м),
## дальше каждый следующий вдвое грубее. Последний — это и есть радиус, который
## клиент запрашивает у сервера; за ним мир не грузится вовсе.
const LEVEL_RADII := [512.0, 1024.0, 2048.0]

## Цвет уровня. Чем дальше, тем бледнее: ближний чанк должен читаться первым.
const LEVEL_COLORS := [
	Color(0.30, 0.85, 0.45, 0.90),   # 0 — полная подробность
	Color(0.95, 0.80, 0.25, 0.70),   # 1
	Color(0.95, 0.45, 0.20, 0.55),   # 2
	Color(0.55, 0.35, 0.65, 0.40),   # 3 и дальше
]

const RING_COLOR := Color(0.98, 0.98, 1.00, 0.85)
const LIFT := 0.6               # м — над рельефом, чтобы не спорить с землёй за пиксель
const DRAPE_STEP := 8.0         # м — шаг выборки высоты вдоль линии
const RING_SEGMENTS := 96       # отрезков на окружность радиуса
const REPLAN := 32.0            # м — насколько уедет камера до перестройки

## ЛЕНТЫ, А НЕ ЛИНИИ. PRIMITIVE_LINES в Godot рисует толщиной ровно в пиксель:
## line width не поддержан ни Vulkan/Metal, ни GLES3, и параметра для него нет.
## Сетка выходила ниткой, которую на траве не видно вовсе — проверено снимком.
## Поэтому каждый отрезок — плоская лента из двух треугольников, а ширина
## берётся от масштаба вида: на 300 м это метр, на двух километрах — десяток,
## и в обоих случаях линия читается примерно одинаково.
const WIDTH_FACTOR := 0.004
const WIDTH_MIN := 0.6
const WIDTH_MAX := 15.0

var _mesh: MeshInstance3D
var _im: ImmediateMesh
var _height: Callable            # (x, z) -> float; пустая — рисуем на нуле
var _center := Vector3.ZERO
var _width := 1.0
var _built := false

func _ready() -> void:
	visible = false
	_im = ImmediateMesh.new()
	_mesh = MeshInstance3D.new()
	_mesh.mesh = _im
	# Отладочный слой не отбрасывает тени и не принимает их: он не часть мира.
	_mesh.cast_shadow = GeometryInstance3D.SHADOW_CASTING_SETTING_OFF
	_mesh.material_override = _line_material()
	add_child(_mesh)

## Откуда брать высоту. Мир знает рельеф, слой — нет, и знать не должен:
## он про разбиение, а не про землю.
func set_height_source(f: Callable) -> void:
	_height = f
	_built = false

func toggle() -> void:
	visible = not visible
	if visible:
		_built = false   # пока был выключен, камера уехала

func _process(_delta: float) -> void:
	if not visible:
		return
	var cam := get_viewport().get_camera_3d()
	if cam == null:
		return
	var focus := _focus(cam)
	var w := _line_width(cam, focus)
	# Перестройка не только от сдвига, но и от зума: на приближении лента,
	# посчитанная для дальнего вида, закрывала бы полкадра.
	if _built and focus.distance_to(_center) < REPLAN and is_equal_approx(w, _width):
		return
	_center = focus
	_width = w
	_build(focus)
	_built = true

## Ширина ленты от масштаба вида. У ортографии масштаб — это size (ширина кадра
## в метрах), у перспективы — расстояние до точки взгляда; величины разные по
## смыслу, но обе пропорциональны тому, сколько метров приходится на пиксель.
func _line_width(cam: Camera3D, focus: Vector3) -> float:
	var span: float = cam.size if cam.projection == Camera3D.PROJECTION_ORTHOGONAL \
		else cam.global_position.distance_to(focus)
	return clampf(span * WIDTH_FACTOR, WIDTH_MIN, WIDTH_MAX)

## Центр разбиения — точка ПОД КАМЕРОЙ, а не сама камера: у вида сверху камера
## висит в сотнях метров над землёй, и круг уровней, отмеренный от неё, был бы
## меньше настоящего ровно на эту высоту.
func _focus(cam: Camera3D) -> Vector3:
	var p := cam.global_position
	return Vector3(p.x, 0.0, p.z)

func _y(x: float, z: float) -> float:
	if _height.is_valid():
		return float(_height.call(x, z)) + LIFT
	return LIFT

## --- построение -------------------------------------------------------------

func _build(focus: Vector3) -> void:
	_im.clear_surfaces()
	_im.surface_begin(Mesh.PRIMITIVE_TRIANGLES)

	var far: float = LEVEL_RADII[LEVEL_RADII.size() - 1]
	# Границы по сетке чанков, а не по кругу: линия обязана совпадать с реальным
	# краем чанка, иначе слой показывает свою собственную сетку, а не ту, что
	# поедет в протокол.
	var i0 := int(floor((focus.x - far) / CHUNK))
	var i1 := int(ceil((focus.x + far) / CHUNK))
	var j0 := int(floor((focus.z - far) / CHUNK))
	var j1 := int(ceil((focus.z + far) / CHUNK))

	for i in range(i0, i1 + 1):
		for j in range(j0, j1 + 1):
			var x := float(i) * CHUNK
			var z := float(j) * CHUNK
			# Рисуем две стороны ячейки из четырёх: соседняя ячейка нарисует
			# свои, и общее ребро не удваивается.
			_drape_line(Vector3(x, 0, z), Vector3(x + CHUNK, 0, z), focus)
			_drape_line(Vector3(x, 0, z), Vector3(x, 0, z + CHUNK), focus)

	for r: float in LEVEL_RADII:
		_ring(focus, r)

	_im.surface_end()

## Линия по рельефу: делится на шаги и каждый узел садится на землю. Цвет
## берётся ПОСЕГМЕНТНО по уровню — так видно, где проходит граница уровня,
## даже если она режет чанк пополам.
func _drape_line(a: Vector3, b: Vector3, focus: Vector3) -> void:
	var d := a.distance_to(b)
	var n: int = maxi(1, int(ceil(d / DRAPE_STEP)))
	var prev := Vector3.ZERO
	for k in range(n + 1):
		var t := float(k) / float(n)
		var p := a.lerp(b, t)
		p.y = _y(p.x, p.z)
		if k > 0:
			var mid := (prev + p) * 0.5
			var c := _color_at(mid, focus)
			if c.a > 0.0:
				_ribbon(prev, p, c)
		prev = p

## Окружность радиуса уровня — то, что клиент запрашивает у сервера.
func _ring(focus: Vector3, r: float) -> void:
	var prev := Vector3.ZERO
	for k in range(RING_SEGMENTS + 1):
		var a := TAU * float(k) / float(RING_SEGMENTS)
		var p := Vector3(focus.x + cos(a) * r, 0.0, focus.z + sin(a) * r)
		p.y = _y(p.x, p.z)
		if k > 0:
			_ribbon(prev, p, RING_COLOR)
		prev = p

## Отрезок лентой: два треугольника шириной _width, развёрнутых В ГОРИЗОНТ.
## Именно в горизонт, а не по нормали рельефа: лента на склоне должна лежать
## плашмя, а нормаль на шаге выборки 8 м скачет, и лента шла бы винтом.
func _ribbon(a: Vector3, b: Vector3, c: Color) -> void:
	var dir := Vector3(b.x - a.x, 0.0, b.z - a.z)
	if dir.length_squared() < 1e-9:
		return
	var side := Vector3(-dir.z, 0.0, dir.x).normalized() * (_width * 0.5)
	var a0 := a - side
	var a1 := a + side
	var b0 := b - side
	var b1 := b + side
	_im.surface_set_color(c)
	_im.surface_add_vertex(a0)
	_im.surface_set_color(c)
	_im.surface_add_vertex(b0)
	_im.surface_set_color(c)
	_im.surface_add_vertex(b1)
	_im.surface_set_color(c)
	_im.surface_add_vertex(a0)
	_im.surface_set_color(c)
	_im.surface_add_vertex(b1)
	_im.surface_set_color(c)
	_im.surface_add_vertex(a1)

func _color_at(p: Vector3, focus: Vector3) -> Color:
	var d := Vector2(p.x - focus.x, p.z - focus.z).length()
	for k in LEVEL_RADII.size():
		if d <= float(LEVEL_RADII[k]):
			return LEVEL_COLORS[k]
	return Color(0, 0, 0, 0)   # за последним радиусом мир не грузится — и не рисуется

func _line_material() -> StandardMaterial3D:
	var m := StandardMaterial3D.new()
	m.shading_mode = BaseMaterial3D.SHADING_MODE_UNSHADED
	m.vertex_color_use_as_albedo = true
	m.transparency = BaseMaterial3D.TRANSPARENCY_ALPHA
	# Обмотка ленты зависит от направления отрезка, а сетка идёт в обе стороны:
	# с отсечением половина линий пропала бы — и именно та половина, которая
	# смотрит от камеры.
	m.cull_mode = BaseMaterial3D.CULL_DISABLED
	# Отладочная сетка не спорит с рельефом за пиксель, но и не светит сквозь
	# холм: за холмом чанков не видно, и это правда про то, что грузится.
	m.no_depth_test = false
	return m

## Строка для человека: что он сейчас видит. Знать про неё должен тот, кто
## рисует HUD, — слой её только сочиняет.
func legend() -> String:
	return "чанки %d м · уровни %s м · зелёный=0, жёлтый=1, оранжевый=2" % [
		int(CHUNK), str(LEVEL_RADII)]
