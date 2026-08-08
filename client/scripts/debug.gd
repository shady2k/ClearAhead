extends Node2D
## Отладочный слой (виден по флагу client/debug_layer или --debug):
## id элементов в стартовых позах, охват станции.
## Дочерний узел World: рисует в координатах сервера (Y вверх) и наследует
## единственный переворот World.scale.y = -1 — собственного минуса здесь нет.

var _labels: Array[Label] = []
var _bounds := Rect2()

func set_geometry(geo: Dictionary, server_bounds: Rect2) -> void:
	for label in _labels:
		label.queue_free()
	_labels.clear()
	_bounds = server_bounds
	for el in geo.elements:
		var label := Label.new()
		label.text = "%s · %d прим." % [el.id, el.primitives.size()]
		label.add_theme_font_size_override("font_size", 11)
		label.modulate = Color(1.0, 0.35, 0.35)
		label.position = Vector2(el.start.plan.x, el.start.plan.y) + Vector2(0, 8)
		label.scale = Vector2(1.0, -1.0)  # текст под переворотом World читается зеркально; это глифы, а не геометрия
		add_child(label)
		_labels.append(label)
	queue_redraw()

func _draw() -> void:
	if _bounds.size.x > 0.0:
		draw_rect(_bounds, Color(1.0, 0.35, 0.35, 0.7), false, 1.0)
