## check.gd — проверка того, что НЕ пиксели. Гоняется без окна:
##
##   godot --headless --path client --script res://tools/check.gd -- --server=…
##
## Зачем отдельно от рисунка: снимок экрана доказывает, что нарисовано верно, но
## не доказывает, что верно обработано НЕНАРИСОВАННОЕ. Счастливый путь на
## затравке ST_A не задевает ни 204, ни 404 — все 55 чанков приезжают. Значит
## обе ветки надо задеть нарочно, иначе они впервые сработают у игрока.
##
## Записанная грабля (bd recall godot-client-check) отсюда никуда не делась:
## этим файлом НЕЛЬЗЯ заменить снимок. Он проверяет коды и числа, а не картинку.
extends SceneTree

var _failures: Array[String] = []
var _checks := 0
var _started := false


## Работа начинается с ПЕРВОГО КАДРА, а не из _initialize: в _initialize дерево
## ещё не вместило узлы, и HTTPRequest отвечает «!is_inside_tree()». Это тот же
## урок, что записан про такты в bd recall godot-client-check: ждать надо
## события, а не удобного момента.
func _process(_delta: float) -> bool:
	if not _started:
		_started = true
		_run()
	return false


func _ok(name: String, cond: bool, detail: String = "") -> void:
	_checks += 1
	if cond:
		print("  ok   %s %s" % [name, detail])
	else:
		_failures.append("%s %s" % [name, detail])
		print("  ОТКАЗ %s %s" % [name, detail])


func _run() -> void:
	var server := "http://127.0.0.1:8080"
	var region := "ST_A"
	for a in OS.get_cmdline_user_args():
		if a.begins_with("--server="):
			server = a.substr(9).rstrip("/")
		elif a.begins_with("--region="):
			region = a.substr(9)

	var net := NetClient.new()
	net.base_url = server
	root.add_child(net)

	print("=== проверка клиента без окна: %s, регион %s ===" % [server, region])

	# 1. Манифест: без правила подробности клиент не знает, что спрашивать.
	var man_res: Dictionary = await net.fetch_json("/regions/%s" % region)
	_ok("манифест 200", man_res["ok"], String(man_res.get("error", "")))
	if not man_res["ok"]:
		_finish()
		return
	var rule := ChunkRule.from_manifest((man_res["data"] as Dictionary).get("chunks", {}) as Dictionary)
	_ok("правило подробности заполнено", rule.valid(), rule.rule_text())

	# 2. Правило уровня — на границах полос, а не в серединке.
	_ok("level_for(0)==0", rule.level_for(0.0) == 0)
	_ok("level_for(r0-ε)==0", rule.level_for(rule.level0_radius_m - 0.001) == 0)
	_ok("level_for(r0)==1", rule.level_for(rule.level0_radius_m) == 1)
	_ok("level_for(за последним уровнем)==-1",
		rule.level_for(rule.radius_of(rule.max_level) + 1.0) == -1)

	# 3. Сеть: рецепт разбирается, и длина по примитивам сходится с объявленной.
	var revision := int((man_res["data"] as Dictionary).get("revision", -1))
	var net_res: Dictionary = await net.fetch_json("/regions/%s/revisions/%d/network" % [region, revision])
	_ok("сеть 200", net_res["ok"], String(net_res.get("error", "")))
	if not net_res["ok"]:
		_finish()
		return
	var elements: Array[TrackGeom.Element] = []
	var total := 0.0
	var declared := 0.0
	for e_raw in ((net_res["data"] as Dictionary).get("elements", []) as Array):
		var el := TrackGeom.tessellate_element(e_raw as Dictionary, 5.0, 0.05)
		elements.append(el)
		total += el.length_m
		declared += el.length_declared_m
	_ok("элементов больше нуля", elements.size() > 0, "%d" % elements.size())
	_ok("длина по примитивам = объявленной сервером", absf(total - declared) < 1e-6,
		"%.6f против %.6f" % [total, declared])

	# Тесселяция обязана начинаться ровно в присланной позе: если клиент
	# «уточнит» начало, вся цепочка уедет, и на стыках появятся ступеньки.
	var first: Dictionary = ((net_res["data"] as Dictionary)["elements"] as Array)[0] as Dictionary
	var fp: Dictionary = (first["start"] as Dictionary)["plan"] as Dictionary
	var p0: TrackGeom.AxisPoint = elements[0].points[0]
	_ok("первая точка = присланной позе",
		absf(p0.x - float(fp["x"])) < 1e-9 and absf(p0.y - float(fp["y"])) < 1e-9)

	# 4. Чанк, который ЕСТЬ: размер тела, заголовок базы, разбор.
	var axis := TrackGeom.sample_axis(elements, 5.0)
	var addr: Dictionary = rule.candidates(axis, _bbox(axis))[0]
	var c: Dictionary = await net.fetch("/regions/%s/chunks/%d/%d/%d" % [region, addr["level"], addr["cx"], addr["cz"]])
	_ok("чанк 200", c["ok"] and c["code"] == 200, "код %s" % c.get("code"))
	if c["ok"] and c["code"] == 200:
		var blob: PackedByteArray = c["body"]
		_ok("тело = samples²·2 байт", blob.size() == rule.samples * rule.samples * 2,
			"%d байт" % blob.size())
		var base_hdr := NetClient.header_value(c["headers"], "X-Chunk-Base-Z-Mm")
		_ok("заголовок X-Chunk-Base-Z-Mm есть", base_hdr != "", base_hdr)
		var built := TerrainMesh.build(blob, float(base_hdr.to_int()) / 1000.0, addr["level"], addr["cx"], addr["cz"], rule)
		_ok("меш собран", built.get("ok", false), String(built.get("error", "")))
		if built.get("ok", false):
			_ok("вершин = samples²", int(built["vertices"]) == rule.samples * rule.samples,
				"%d" % built["vertices"])
			_ok("треугольников = (samples-1)²·2",
				int(built["triangles"]) == (rule.samples - 1) * (rule.samples - 1) * 2,
				"%d" % built["triangles"])
		# Тело неверной длины обязано быть отказом, а не половиной меша.
		var short := blob.slice(0, blob.size() - 2)
		_ok("короткое тело отвергнуто",
			not TerrainMesh.build(short, 0.0, 0, 0, 0, rule).get("ok", true))

	# 5. Пустота законна: 204, а не отказ. Адрес заведомо вне охвата.
	var far_cell := int(ceil(rule.radius_of(rule.max_level) * 4.0 / rule.side_of(0))) + 8
	var empty: Dictionary = await net.fetch("/regions/%s/chunks/0/%d/%d" % [region, far_cell, far_cell])
	_ok("далёкий чанк — 204", empty["ok"] and empty["code"] == 204, "код %s" % empty.get("code"))
	if empty["ok"] and empty["code"] == 204:
		_ok("у 204 пустое тело", (empty["body"] as PackedByteArray).size() == 0)

	# 6. Неверный адрес — 404, и это отказ, в отличие от 204.
	var bad: Dictionary = await net.fetch("/regions/%s/chunks/%d/0/0" % [region, rule.max_level + 5])
	_ok("уровень вне правила — 404", bad["ok"] and bad["code"] == 404, "код %s" % bad.get("code"))
	var bad_region: Dictionary = await net.fetch("/regions/НЕТ_ТАКОГО/chunks/0/0/0")
	_ok("несуществующий регион — 404", bad_region["ok"] and bad_region["code"] == 404,
		"код %s" % bad_region.get("code"))

	# 7. Цена decode_s16 против «сервер шлёт float32 вдвое большим объёмом».
	#    Открытый вопрос проекта: замер здесь, вывод — в отчёте.
	if c["ok"] and c["code"] == 200:
		_bench(c["body"], rule)

	_finish()


func _bench(blob: PackedByteArray, rule: ChunkRule) -> void:
	var n := rule.samples * rule.samples
	var rounds := 200

	var t0 := Time.get_ticks_usec()
	for _r in rounds:
		var out := PackedFloat32Array()
		out.resize(n)
		for k in n:
			out[k] = float(blob.decode_s16(k * 2))
	var t_s16 := float(Time.get_ticks_usec() - t0) / float(rounds)

	# Для сравнения — то, что было бы, шли сервер float32: вдвое больше байт,
	# зато один системный вызов вместо n.
	var wide := PackedByteArray()
	wide.resize(n * 4)
	t0 = Time.get_ticks_usec()
	for _r in rounds:
		var f := wide.to_float32_array()
		if f.size() != n:
			push_error("bench: to_float32_array дал %d" % f.size())
	var t_f32 := float(Time.get_ticks_usec() - t0) / float(rounds)

	print("=== цена разбора одного чанка (%d отсчётов, %d прогонов) ===" % [n, rounds])
	print("  decode_s16 в цикле:      %8.1f мкс  (%.1f нс на отсчёт, %d байт на проводе)" % [
		t_s16, t_s16 * 1000.0 / float(n), n * 2])
	print("  to_float32_array:        %8.1f мкс  (%.1f нс на отсчёт, %d байт на проводе)" % [
		t_f32, t_f32 * 1000.0 / float(n), n * 4])
	print("  разница:                 %8.1f мкс на чанк" % (t_s16 - t_f32))


func _bbox(axis: PackedVector2Array) -> Rect2:
	var mn := axis[0]
	var mx := axis[0]
	for p in axis:
		mn = Vector2(minf(mn.x, p.x), minf(mn.y, p.y))
		mx = Vector2(maxf(mx.x, p.x), maxf(mx.y, p.y))
	return Rect2(mn, mx - mn)


func _finish() -> void:
	print("=== проверок %d, отказов %d ===" % [_checks, _failures.size()])
	for f in _failures:
		print("  ОТКАЗ: %s" % f)
	quit(0 if _failures.is_empty() else 1)
