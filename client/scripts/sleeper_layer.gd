extends Node2D
## Слой шпал: одна пачка draw_multiline (пары точек = отдельные отрезки),
## перерисовка только по queue_redraw(). Координаты сервера (Y вверх) —
## переворот наследуется от World.

var _segments := PackedVector2Array()
var _color := Color(0.36, 0.36, 0.36)
var _width := 0.28

func setup(segments: PackedVector2Array, color: Color, width: float) -> void:
	_segments = segments
	_color = color
	_width = width
	queue_redraw()

func _draw() -> void:
	if _segments.size() >= 2:
		draw_multiline(_segments, _color, _width)
