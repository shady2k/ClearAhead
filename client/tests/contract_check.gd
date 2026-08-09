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
	# Ревизию НЕ пиним числом: она растёт при каждой правке раскладки (карту
	# уже правили — ClearAhead-sn5), и прибитое число делает сверку контракта
	# красной по причине, к контракту отношения не имеющей.
	_check(geo.map_revision >= 1, "map_revision >= 1, получили %s" % geo.map_revision)
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

	# Роли ветвей стрелок: 8 стрелок × 2 ветви. Поле — единственный способ
	# отличить ветвь от пути, разбор ID запрещён.
	var roles := 0
	var role_bad := 0
	for el in geo.elements:
		if not el.has("role"):
			continue
		roles += 1
		var r: Dictionary = el.role
		if (r.turnout as String).is_empty() or (r.branch as String).is_empty() \
				or (r.hand as String).is_empty() or not r.has("frog"):
			role_bad += 1
		elif r.branch != Parser.BRANCH_STRAIGHT and r.branch != Parser.BRANCH_DIVERGING:
			role_bad += 1
		elif r.hand != "right" and r.hand != "left":
			role_bad += 1
	_check(role_bad == 0, "роли ветвей полные (битых %d)" % role_bad)
	_check(roles == 16, "16 ветвей стрелок с ролью, получили %d" % roles)

	# Путевые объекты: платформы 2 и 3, спаны в координате u как в карте.
	var platforms := 0
	for ts in geo.trackside:
		_check(ts.kind == Parser.KIND_PLATFORM, "%s: kind platform, получили «%s»" % [ts.id, ts.kind])
		_check(ts.has("side") and not (ts.side as String).is_empty(), "%s: сторона задана" % ts.id)
		_check(ts.spans.size() == 1, "%s: один спан, получили %d" % [ts.id, ts.spans.size()])
		var sp: Dictionary = ts.spans[0]
		_check(sp.element == "ST_A_E_T2" or sp.element == "ST_A_E_T3",
			"%s: спан на пути 2 или 3, получили %s" % [ts.id, sp.element])
		_check(sp.from == 100.0 and sp.to == 600.0,
			"%s: спан [100, 600] в u, получили [%s, %s]" % [ts.id, sp.from, sp.to])
		platforms += 1
	_check(platforms == 2, "2 платформы в контракте, получили %d" % platforms)

	if _failures == 0:
		print("CONTRACT CHECK OK: %s · %s rev %d · %d элементов · %d дуг · %d ролей · %d объектов" % [
			golden, geo.map_id, geo.map_revision, geo.elements.size(), arcs, roles, platforms])
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
