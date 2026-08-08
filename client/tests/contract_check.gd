extends SceneTree
## Проверка контракта со стороны клиента (обязательная по брифу W-J):
## грузит contract/render_geometry.golden.json, разбирает его БОЕВЫМ парсером
## (тем же, что в игре) и падает с кодом 1, если чего-то не хватает или
## встретился неизвестный kind. Go-сторона сверяет тот же файл со своим
## артефактом; вместе это ловит расхождение формы с обеих сторон.
##
## Запуск:
##   godot --headless --path client --script tests/contract_check.gd
##   [--golden ../contract/render_geometry.golden.json]

const Parser := preload("res://scripts/geometry_parser.gd")

var _failures := 0

func _initialize() -> void:
	var golden := _arg_value("--golden", "../contract/render_geometry.golden.json")
	var abs_path := _resolve(golden)
	if not FileAccess.file_exists(abs_path):
		printerr("CONTRACT CHECK: файл эталона не найден: %s" % abs_path)
		quit(1)
		return
	var text := FileAccess.get_file_as_string(abs_path)
	var res := Parser.parse(text)
	if not res.ok:
		printerr("CONTRACT CHECK FAIL: %s" % res.error)
		quit(1)
		return
	var geo: Dictionary = res.geometry

	# Явные ожидания по эталону — дополнительно к проверкам парсера.
	_check(geo.map_id == "ST_A", "map_id == ST_A, получили «%s»" % geo.map_id)
	_check(geo.map_revision == 1, "map_revision == 1, получили %s" % geo.map_revision)
	_check(geo.elements.size() == 31, "31 элемент, получили %d" % geo.elements.size())
	var arcs := 0
	var unknowns := 0
	for el in geo.elements:
		for p in el.primitives:
			if p.kind == Parser.KIND_STRAIGHT:
				_check(typeof(p.length) == TYPE_FLOAT, "%s: straight.length — число" % el.id)
			elif p.kind == Parser.KIND_ARC:
				arcs += 1
				_check(p.radius > 0.0, "%s: arc.radius > 0" % el.id)
				_check(typeof(p.angle) == TYPE_FLOAT, "%s: arc.angle — число" % el.id)
			else:
				unknowns += 1
	_check(unknowns == 0, "нет неизвестных kind (их %d)" % unknowns)
	_check(arcs >= 1, "есть хотя бы одна дуга (их %d) — ветка arc разбирается" % arcs)

	if _failures == 0:
		print("CONTRACT CHECK OK: %s · %s rev %d · %d элементов · %d дуг" % [
			golden, geo.map_id, geo.map_revision, geo.elements.size(), arcs])
		quit(0)
	else:
		printerr("CONTRACT CHECK FAIL: %d проверок не сошлось" % _failures)
		quit(1)

func _check(cond: bool, what: String) -> void:
	if cond:
		return
	_failures += 1
	printerr("CONTRACT CHECK: не сошлось: %s" % what)

func _arg_value(name: String, default: String) -> String:
	for arg in OS.get_cmdline_user_args():
		if arg.begins_with(name + "="):
			return arg.trim_prefix(name + "=")
	return default

func _resolve(path: String) -> String:
	if path.begins_with("res://") or path.is_absolute_path():
		return path
	return ProjectSettings.globalize_path("res://").path_join(path)
