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
	for el in geometry.elements:
		_draw_element(el)

func _draw_element(el: Dictionary) -> void:
	var pts: PackedVector2Array = GM.sample_chain(el.start, el.primitives)
	if _lod == Lod.SIMPLE:
		_add_line(pts, SIMPLE_COLOR, SIMPLE_PX)
		return
	var ballast := Polygon2D.new()
	ballast.polygon = GM.offset_polygon(pts, BALLAST_HALF_W)
	ballast.color = BALLAST_COLOR
	add_child(ballast)
	for sign in [1.0, -1.0]:
		_add_line(GM.offset_polyline(pts, GAUGE_HALF * sign), RAIL_COLOR, RAIL_PX)
	if _lod == Lod.FULL:
		_add_sleepers(pts)

func _add_line(points: PackedVector2Array, color: Color, px: float) -> void:
	var line := Line2D.new()
	line.points = points
	line.default_color = color
	line.width = px / _zoom
	line.joint_mode = Line2D.LINE_JOINT_ROUND
	line.begin_cap_mode = Line2D.LINE_CAP_ROUND
	line.end_cap_mode = Line2D.LINE_CAP_ROUND
	add_child(line)
	_track_lines.append({ "line": line, "px": px })

func _add_sleepers(pts: PackedVector2Array) -> void:
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
	add_child(layer)

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
