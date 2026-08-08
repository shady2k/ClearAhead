extends Camera2D
## Камера в метрах. Панорама — перетаскиванием ЛКМ, зум — колесом К КУРСОРУ:
## точка под курсором остаётся под ним после зума. zoom — единственное место,
## где живёт масштаб. Камера живёт ВНЕ перевёрнутого поддерева World: её
## координаты — godot-экрана, минусов здесь нет (см. GeometryMath.server_to_godot).

signal zoom_changed(zoom: float)

const MIN_ZOOM := 0.02
const MAX_ZOOM := 50.0
const ZOOM_STEP := 1.15

var _panning := false

func _unhandled_input(event: InputEvent) -> void:
	if event is InputEventMouseButton:
		var mb := event as InputEventMouseButton
		if mb.button_index == MOUSE_BUTTON_WHEEL_UP and mb.pressed:
			_zoom_at(ZOOM_STEP)
		elif mb.button_index == MOUSE_BUTTON_WHEEL_DOWN and mb.pressed:
			_zoom_at(1.0 / ZOOM_STEP)
		elif mb.button_index == MOUSE_BUTTON_LEFT:
			_panning = mb.pressed
	elif event is InputEventMouseMotion and _panning:
		position -= (event as InputEventMouseMotion).relative / zoom

func _zoom_at(factor: float) -> void:
	var mouse_world := get_global_mouse_position()  # до смены zoom
	var new_zoom := clampf(zoom.x * factor, MIN_ZOOM, MAX_ZOOM)
	if is_equal_approx(new_zoom, zoom.x):
		return
	var old_zoom := zoom.x
	zoom = Vector2(new_zoom, new_zoom)
	# точка под курсором остаётся под курсором
	position = mouse_world - (mouse_world - position) * (old_zoom / new_zoom)
	zoom_changed.emit(new_zoom)

## Вписать прямоугольник (в координатах godot-экрана) в окно.
func fit_to(bounds: Rect2, margin_frac: float = 0.08) -> void:
	if bounds.size.x <= 0.0 or bounds.size.y <= 0.0:
		return
	var view := get_viewport_rect().size
	var z := minf(view.x / bounds.size.x, view.y / bounds.size.y) * (1.0 - margin_frac)
	zoom = Vector2.ONE * clampf(z, MIN_ZOOM, MAX_ZOOM)
	position = bounds.get_center()
	zoom_changed.emit(zoom.x)
