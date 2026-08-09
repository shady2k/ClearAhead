extends SceneTree
## Структурная проверка отрисовки: грузит эталон, собирает World на полном
## LOD и считает узлы по слоям. Ожидания при 31 элементе (15 обычных, 16
## ветвей = 8 пар стрелок) и 2 путевых объектах:
##   ballast:   15 обычных + 8 общих (стрелки) = 23
##   sleepers:  15 + 8 = 23
##   platforms: 2 (по одному узлу на объект)
##   rails:     15*2 + 8*(4 нитки + 2 крыла крестовины) = 78
func _initialize() -> void:
	const Parser := preload("res://scripts/geometry_parser.gd")
	var text := FileAccess.get_file_as_string("../contract/render_geometry.golden.json")
	var res := Parser.parse(text)
	var world: Node2D = load("res://scripts/world.gd").new()
	root.add_child(world)
	world.set_geometry(res.geometry)
	world.set_zoom(12.0)
	var counts := {}
	for child in world.get_children():
		if child is Node2D and child.name in ["ballast", "sleepers", "platforms", "rails"]:
			counts[child.name] = child.get_child_count()
	print("LAYER COUNTS: %s" % [counts])
	quit(0)
