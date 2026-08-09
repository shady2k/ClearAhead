extends Node2D
## World — всё, что в метрах и зумится: балласт, рельсы, шпалы.
## Дети рисуются в координатах СЕРВЕРА (Y вверх); станцию переворачивает
## ОДИН минус — transform этого узла:
##
##     scale.y = -1
##
## Больше нигде в отрисовке знака y нет: ни в тесселяции дуг (geometry_math.gd),
## ни в смещениях, ни в углах. Второй минус в любом месте сделает станцию
## зеркальной, и это незаметно. Debug — дочерний узел World и наследует тот же
## переворот; отдельного scale.y = -1 нигде больше нет.

const GM := preload("res://scripts/geometry_math.gd")
const SleeperLayer := preload("res://scripts/sleeper_layer.gd")

enum Lod { SIMPLE = 0, MID = 1, FULL = 2 }

const LOD_MID_ZOOM := 1.5    # px/м — ниже: одна линия на путь
const LOD_FULL_ZOOM := 5.0   # px/м — выше: добавляются шпалы

const BALLAST_HALF_W := 1.75  # м — полоса балласта
const GAUGE_HALF := 0.7175    # м — половина колеи 1435 мм
const SLEEPER_HALF := 1.25    # м — шпала поперёк пути
const SLEEPER_STEP := 0.6     # м — шаг шпал
const SLEEPER_WIDTH := 0.28   # м — толщина шпалы

const RAIL_PX := 2.0          # экранные px — толщина нитки (не метры!)
const SIMPLE_PX := 2.5        # экранные px — упрощённая линия

const BALLAST_COLOR := Color(0.58, 0.58, 0.58)
const RAIL_COLOR := Color(0.95, 0.95, 0.95)
const SIMPLE_COLOR := Color(0.80, 0.80, 0.80)
const SLEEPER_COLOR := Color(0.36, 0.36, 0.36)

## Порядок слоёв СНИЗУ ВВЕРХ: дети рисуются в порядке добавления, поэтому
## порядок имён здесь и есть порядок отрисовки. Новые слои (платформы,
## стрелки) добавляются в список и получают свой проход в rebuild().
const LAYER_ORDER: Array[String] = ["ballast", "sleepers", "rails"]

var geometry: Dictionary = { "elements": [] }  # пустая станция до загрузки: зум до прихода геометрии не падает
var _zoom := 1.0
var _lod := -1
var _track_lines: Array = []  # [{ "line": Line2D, "px": float }] — ширины при зуме

@onready var _debug: Node2D = $Debug

func _ready() -> void:
	scale = Vector2(1.0, -1.0)  # ЕДИНСТВЕННЫЙ переворот станции (Y вверх -> Y вниз)

func set_geometry(geo: Dictionary) -> void:
	geometry = geo
	_lod = -1
	rebuild()

## zoom камеры (px/м). Толщина линий — в экранных px: делим на zoom.
## При смене уровня LOD сцена пересобирается.
func set_zoom(z: float) -> void:
	_zoom = z
	var lod := _lod_for(z)
	if lod != _lod:
		rebuild()
		return
	for entry in _track_lines:
		entry.line.width = entry.px / z

func _lod_for(z: float) -> int:
	if z >= LOD_FULL_ZOOM:
		return Lod.FULL
	if z >= LOD_MID_ZOOM:
		return Lod.MID
	return Lod.SIMPLE

func rebuild() -> void:
	for child in get_children():
		if child != _debug:
			child.queue_free()
	_track_lines.clear()
	_lod = _lod_for(_zoom)
	if _lod == Lod.SIMPLE:
		# Упрощённый уровень — одна линия на путь, слои не нужны.
		for el in geometry.elements:
			_add_line(self, GM.sample_chain(el.start, el.primitives), SIMPLE_COLOR, SIMPLE_PX)
		return
	# Рисуем СЛОЯМИ, а не поэлементно: сперва балласт всех элементов, затем
	# шпалы всех, затем нитки всех. Поэлементная отрисовка красила балласт
	# стрелок (их id ST_A_SW_* идут после путей ST_A_E_*) поверх ниток и шпал
	# уже нарисованных путей.
	var layers := _make_layers()
	for el in geometry.elements:
		_draw_ballast(layers.ballast, el)
	if _lod == Lod.FULL:
		for el in geometry.elements:
			_draw_sleepers(layers.sleepers, el)
	for el in geometry.elements:
		_draw_rails(layers.rails, el)

## Создаёт пустые контейнеры слоёв под World в порядке LAYER_ORDER.
func _make_layers() -> Dictionary:
	var layers := {}
	for name in LAYER_ORDER:
		var layer := Node2D.new()
		layer.name = name
		add_child(layer)
		layers[name] = layer
	return layers

## Балласт элемента — один Polygon2D в слой балласта.
func _draw_ballast(parent: Node2D, el: Dictionary) -> void:
	var pts: PackedVector2Array = GM.sample_chain(el.start, el.primitives)
	var ballast := Polygon2D.new()
	ballast.polygon = GM.offset_polygon(pts, BALLAST_HALF_W)
	ballast.color = BALLAST_COLOR
	parent.add_child(ballast)

## Две нитки элемента — Line2D в слой ниток (ширины живут в _track_lines).
func _draw_rails(parent: Node2D, el: Dictionary) -> void:
	var pts: PackedVector2Array = GM.sample_chain(el.start, el.primitives)
	for sign in [1.0, -1.0]:
		_add_line(parent, GM.offset_polyline(pts, GAUGE_HALF * sign), RAIL_COLOR, RAIL_PX)

func _add_line(parent: Node2D, points: PackedVector2Array, color: Color, px: float) -> void:
	var line := Line2D.new()
	line.points = points
	line.default_color = color
	line.width = px / _zoom
	line.joint_mode = Line2D.LINE_JOINT_ROUND
	line.begin_cap_mode = Line2D.LINE_CAP_ROUND
	line.end_cap_mode = Line2D.LINE_CAP_ROUND
	parent.add_child(line)
	_track_lines.append({ "line": line, "px": px })

## Шпалы элемента — один SleeperLayer (пачка draw_multiline) в слой шпал.
func _draw_sleepers(parent: Node2D, el: Dictionary) -> void:
	var pts: PackedVector2Array = GM.sample_chain(el.start, el.primitives)
	var resampled := GM.resample_uniform(pts, SLEEPER_STEP)
	var ns := GM.normals(resampled)
	var segs := PackedVector2Array()
	for i in resampled.size():
		var p: Vector2 = resampled[i]
		var n: Vector2 = ns[i]
		segs.append(p - n * SLEEPER_HALF)
		segs.append(p + n * SLEEPER_HALF)
	var layer := SleeperLayer.new()
	layer.setup(segs, SLEEPER_COLOR, SLEEPER_WIDTH)
	parent.add_child(layer)

## Охват станции в координатах СЕРВЕРА. Камера живёт вне перевёрнутого
## поддерева, поэтому main переводит границы GM.server_rect_to_godot().
func get_server_bounds() -> Rect2:
	var minv := Vector2(INF, INF)
	var maxv := Vector2(-INF, -INF)
	for el in geometry.elements:
		for p in GM.sample_chain(el.start, el.primitives):
			minv = minv.min(p)
			maxv = maxv.max(p)
	if minv.x == INF:
		return Rect2()
	return Rect2(minv, maxv - minv)
