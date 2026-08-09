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
	_check(geo.map_id == "FIX_ST", "map_id == FIX_ST, получили «%s»" % geo.map_id)
	# Ревизию НЕ пиним числом: она растёт при каждой правке раскладки (карту
	# уже правили — ClearAhead-sn5), и прибитое число делает сверку контракта
	# красной по причине, к контракту отношения не имеющей.
	_check(geo.map_revision >= 1, "map_revision >= 1, получили %s" % geo.map_revision)
	_check(geo.elements.size() == 9, "9 элементов, получили %d" % geo.elements.size())
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
	_check(roles == 4, "4 ветви стрелок с ролью, получили %d" % roles)

	# Путевые объекты: платформа на главном пути, спан в координате u как в карте.
	var platforms := 0
	for ts in geo.trackside:
		_check(ts.kind == Parser.KIND_PLATFORM, "%s: kind platform, получили «%s»" % [ts.id, ts.kind])
		_check(ts.has("side") and not (ts.side as String).is_empty(), "%s: сторона задана" % ts.id)
		_check(ts.spans.size() == 1, "%s: один спан, получили %d" % [ts.id, ts.spans.size()])
		var sp: Dictionary = ts.spans[0]
		_check(sp.element == "E_MAIN",
			"%s: спан на главном пути E_MAIN, получили %s" % [ts.id, sp.element])
		_check(sp.from == 40.0 and sp.to == 100.0,
			"%s: спан [40, 100] в u, получили [%s, %s]" % [ts.id, sp.from, sp.to])
		platforms += 1
	_check(platforms == 1, "1 платформа в контракте, получили %d" % platforms)

	# Конструкция пути (спека §3–4): один тип, 4 run'а, полное покрытие
	# обычных рёбер, крестовины из features.
	_check(geo.track_types.size() == 1, "1 тип, получили %d" % geo.track_types.size())
	var tt: Dictionary = geo.track_types[0]
	_check(tt.id == "TRACK_MAIN_1435", "тип TRACK_MAIN_1435, получили «%s»" % tt.id)
	_approx(tt.gauge, 1.435, "gauge 1.435")
	_approx(tt.sleeper.pitch, 0.6, "шаг шпал 0.6")
	_approx(tt.sleeper.length, 2.5, "длина шпалы 2.5")
	_approx(tt.sleeper.width, 0.28, "ширина шпалы 0.28")
	_approx(tt.ballast.half_width, 1.75, "полуширина балласта 1.75")
	_check(geo.construction_runs.size() == 4, "4 run'а, получили %d" % geo.construction_runs.size())
	var covered := {}
	var joint_run: Dictionary = {}
	for run in geo.construction_runs:
		_check(run.has("type") and not (run.type as String).is_empty(), "%s: type явный" % run.id)
		_check(run.coordinate == "u", "%s: coordinate == u" % run.id)
		for span in run.spans:
			covered[span.element] = covered.get(span.element, 0) + 1
		if run.id == "RUN_APPROACH_CROSS":
			joint_run = run
	var uncovered := 0
	var overlapped := 0
	for el in geo.elements:
		if el.has("role"):
			continue
		var c: int = covered.get(el.id, 0)
		if c == 0:
			uncovered += 1
		elif c > 1:
			overlapped += 1
	_check(uncovered == 0, "обычные рёбра покрыты run'ами (непокрыто %d)" % uncovered)
	_check(overlapped == 0, "перекрытий покрытия нет (их %d)" % overlapped)
	_check(joint_run.spans.size() == 2, "run APPROACH/CROSS из двух спанов (их %d)" % joint_run.spans.size())
	_check(joint_run.spans[0].element == "E_APPROACH" and joint_run.spans[1].element == "E_CROSS",
		"run APPROACH/CROSS покрывает E_APPROACH и E_CROSS")
	_check(geo.placement_algorithm == "placement-v1", "placement_algorithm == placement-v1, получили «%s»" % geo.placement_algorithm)

	# Крестовины (спека §5): 2 стрелки, точка и адреса из эталона.
	var frogs := 0
	for f in geo.features:
		_check(f.kind == Parser.KIND_FEATURE_FROG, "%s: kind frog, получили «%s»" % [f.owner, f.kind])
		frogs += 1
	_check(frogs == 2, "2 крестовины, получили %d" % frogs)
	var sw1: Dictionary = {}
	for f in geo.features:
		if f.owner == "SW1":
			sw1 = f
	_check(not sw1.is_empty(), "есть крестовина SW1")
	if not sw1.is_empty():
		_approx(sw1.point.x, 149.34280150224168, "SW1: point.x")
		_approx(sw1.point.y, -0.7175, "SW1: point.y")
		_check(sw1.addresses.size() == 2, "SW1: два адреса (их %d)" % sw1.addresses.size())
		_check(sw1.addresses[0].element == "SW1:straight", "SW1: адрес 0 — прямой проход")
		_check(sw1.addresses[1].element == "SW1:diverging", "SW1: адрес 1 — боковой проход")
		_approx(sw1.addresses[0].u, 29.342801502241677, "SW1: u прямого прохода")
		_approx(sw1.addresses[1].u, 29.31944228015446, "SW1: u бокового прохода")
		_approx(sw1.addresses[0].tangent.x, 1.0, "SW1: касательная прямого прохода")
		_approx(sw1.addresses[0].tangent.y, 0.0, "SW1: касательная прямого прохода y")

	if _failures == 0:
		print("CONTRACT CHECK OK: %s · %s rev %d · %d элементов · %d дуг · %d ролей · %d объектов · %d run'ов · %d крестовин" % [
			golden, geo.map_id, geo.map_revision, geo.elements.size(), arcs, roles, platforms,
			geo.construction_runs.size(), geo.features.size()])
		quit(0)
	else:
		printerr("CONTRACT CHECK FAIL: %d проверок не сошлось" % _failures)
		quit(1)

func _approx(a: float, b: float, what: String) -> void:
	_check(absf(a - b) < 1e-6, "%s: %f != %f" % [what, a, b])

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
