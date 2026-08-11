## Main — сборка сцены из того, и только из того, что прислал сервер.
##
## ЗАКОН: чего сервер не прислал, того на экране нет. Ни одной зашитой
## константы, описывающей мир. Если чего-то не хватает — оно показывается как
## отсутствующее (нить вместо ленты, дыра вместо чанка, красная строка вместо
## картинки) и попадает в отчёт. Пустой экран честнее выдуманного.
##
## Что здесь ЕСТЬ константами и почему это законно: подробность тесселяции, шаг
## выборки оси, цвета, наклон камеры. Это свойства РИСУНКА. Граница из
## ClearAhead-sjq разрешает клиенту меш, LOD, материалы и тесселяцию — и
## запрещает данные.
##
## Строение сцены (ClearAhead-0xc, перенесено на 3D):
##   World  — всё, что в метрах: рельеф и путь;
##   Camera — свободная, наведена по габаритам данных;
##   UI     — то, что не в метрах: числа и отказы.
extends Node3D

## Подробность рисунка. Не мир.
const TESS_MAX_SEG_M := 5.0
const TESS_MAX_ANG_RAD := 0.05

## Шаг выборки оси для выбора уровня чанка. В манифесте его НЕТ: сервер
## объявляет радиусы, но не то, по каким точкам он мерит расстояние. Расхождение
## шага даёт лишние 204 у границы полосы — не отказ, но повод их считать.
const AXIS_SAMPLE_STEP_M := 5.0

var server_url := "http://127.0.0.1:8080"
var region := "ST_A"
var shot_path := ""
var shot_view := "station"
var quit_when_done := false

var world: Node3D
var ui_label: RichTextLabel
var camera: Camera3D
var net: NetClient

var stats := {}
var errors: Array[String] = []
var report_lines: Array[String] = []


func _ready() -> void:
	_parse_args()
	_build_scene()
	await _load_world()
	_print_report()
	if shot_path != "":
		await _save_shot()
	if quit_when_done:
		get_tree().quit(1 if not errors.is_empty() else 0)


func _parse_args() -> void:
	var args: PackedStringArray = OS.get_cmdline_user_args()
	if args.is_empty():
		args = OS.get_cmdline_args()
	for a in args:
		if a.begins_with("--server="):
			server_url = a.substr(9).rstrip("/")
		elif a.begins_with("--region="):
			region = a.substr(9)
		elif a.begins_with("--shot="):
			shot_path = a.substr(7)
		elif a.begins_with("--view="):
			shot_view = a.substr(7)
		elif a == "--quit-when-done":
			quit_when_done = true


func _build_scene() -> void:
	world = Node3D.new()
	world.name = "World"
	add_child(world)

	var env := WorldEnvironment.new()
	var e := Environment.new()
	e.background_mode = Environment.BG_COLOR
	e.background_color = Color(0.07, 0.09, 0.12)
	e.ambient_light_source = Environment.AMBIENT_SOURCE_COLOR
	e.ambient_light_color = Color(0.45, 0.5, 0.58)
	e.ambient_light_energy = 0.6
	env.environment = e
	add_child(env)

	var sun := DirectionalLight3D.new()
	sun.name = "Sun"
	sun.rotation = Vector3(deg_to_rad(-50.0), deg_to_rad(-40.0), 0.0)
	sun.light_energy = 1.1
	add_child(sun)

	camera = Camera3D.new()
	camera.name = "Camera"
	camera.set_script(load("res://scripts/free_camera.gd"))
	add_child(camera)

	var ui := CanvasLayer.new()
	ui.name = "UI"
	add_child(ui)
	ui_label = RichTextLabel.new()
	ui_label.bbcode_enabled = true
	ui_label.scroll_active = false
	ui_label.fit_content = true
	ui_label.set_anchors_preset(Control.PRESET_TOP_LEFT)
	ui_label.position = Vector2(14, 10)
	ui_label.custom_minimum_size = Vector2(770, 0)
	ui_label.size = Vector2(770, 0)
	ui_label.add_theme_font_size_override("normal_font_size", 14)
	ui_label.add_theme_font_size_override("bold_font_size", 14)
	# Подложка: числа обязаны читаться поверх любого рельефа. Без неё белое по
	# светло-серому пропадает — а числа здесь и есть доказательство.
	var box := StyleBoxFlat.new()
	box.bg_color = Color(0.04, 0.05, 0.07, 0.82)
	box.set_content_margin_all(10.0)
	box.set_corner_radius_all(4)
	ui_label.add_theme_stylebox_override("normal", box)
	ui.add_child(ui_label)


func _fail(msg: String) -> void:
	errors.append(msg)
	push_error(msg)


func _load_world() -> void:
	net = NetClient.new()
	net.base_url = server_url
	add_child(net)

	stats["server"] = server_url
	stats["region"] = region

	# 1. Манифест региона. Без него нечего подставить ни в сеть, ни в правило
	#    подробности, поэтому отказ здесь — конец загрузки, а не полумир.
	var man_res: Dictionary = await net.fetch_json("/regions/%s" % region)
	if not man_res["ok"]:
		_fail("манифест региона: %s" % man_res["error"])
		_refresh_ui()
		return
	var man: Dictionary = man_res["data"]
	stats["epoch"] = man.get("epoch", null)
	stats["revision"] = man.get("revision", null)
	stats["track_hash"] = String(man.get("track_hash", "")).substr(0, 12)
	stats["network_hash"] = String(man.get("network_hash", "")).substr(0, 12)

	var rule := ChunkRule.from_manifest(man.get("chunks", {}) as Dictionary)
	if not rule.valid():
		_fail("манифест не содержит правила подробности — какой уровень чанка спрашивать, неизвестно")
		_refresh_ui()
		return
	stats["rule"] = "side=%d м step=%d м samples=%d r0=%.0f м max_level=%d" % [
		rule.side_m, rule.step_m, rule.samples, rule.level0_radius_m, rule.max_level
	]

	# 2. Сеть региона. Путь нужен ДО рельефа: уровень чанка выбирается по
	#    расстоянию до оси, и без оси спрашивать нечего.
	var revision := int(man.get("revision", -1))
	var net_res: Dictionary = await net.fetch_json("/regions/%s/revisions/%d/network" % [region, revision])
	if not net_res["ok"]:
		_fail("сеть региона: %s" % net_res["error"])
		_refresh_ui()
		return
	var network: Dictionary = net_res["data"]

	var elements := _parse_elements(network)
	if elements.is_empty():
		_fail("сеть региона пуста: ни одного элемента — рисовать нечего")
		_refresh_ui()
		return

	_apply_track_types(network, elements)
	_draw_track(elements)

	# 3. Рельеф. Адреса чанков клиент выводит САМ, из правила манифеста и
	#    собственной оси.
	var axis := TrackGeom.sample_axis(elements, AXIS_SAMPLE_STEP_M)
	var bbox := _axis_bbox(axis)
	stats["axis_points"] = axis.size()
	stats["axis_bbox"] = "x %.1f…%.1f, y %.1f…%.1f" % [bbox.position.x, bbox.end.x, bbox.position.y, bbox.end.y]

	await _load_terrain(rule, axis, bbox)

	_place_camera(bbox)
	_refresh_ui()


func _parse_elements(network: Dictionary) -> Array[TrackGeom.Element]:
	var out: Array[TrackGeom.Element] = []
	var total := 0.0
	var declared := 0.0
	var raw: Array = network.get("elements", []) as Array
	for e_raw in raw:
		var el := TrackGeom.tessellate_element(e_raw as Dictionary, TESS_MAX_SEG_M, TESS_MAX_ANG_RAD)
		if el.points.size() < 2:
			_fail("элемент %s: тесселяция дала меньше двух точек" % el.id)
			continue
		out.append(el)
		total += el.length_m
		declared += el.length_declared_m
	stats["elements"] = out.size()
	stats["length_total_m"] = total
	stats["length_declared_m"] = declared
	# Сходимость рецепта: длина, выведенная из радиуса и угла, против длины,
	# объявленной сервером. Расхождение значило бы, что клиент понимает примитив
	# иначе, чем сервер его писал, — и это важнее любой картинки.
	stats["length_mismatch_m"] = absf(total - declared)
	return out


## _apply_track_types — ширина отсыпки берётся ТОЛЬКО по цепочке
## construction_runs -> track_types. Элемент, до которого цепочка не дошла,
## остаётся без ширины и рисуется нитью.
func _apply_track_types(network: Dictionary, elements: Array[TrackGeom.Element]) -> void:
	var types := {}
	for t_raw in (network.get("track_types", []) as Array):
		var t: Dictionary = t_raw as Dictionary
		types[String(t.get("id", ""))] = t

	var by_id := {}
	for el in elements:
		by_id[el.id] = el

	var covered := 0
	for r_raw in (network.get("construction_runs", []) as Array):
		var run: Dictionary = r_raw as Dictionary
		var type_id := String(run.get("type", ""))
		if not types.has(type_id):
			continue
		var t: Dictionary = types[type_id]
		var ballast: Dictionary = t.get("ballast", {}) as Dictionary
		if not ballast.has("half_width"):
			continue
		var hw := float(ballast["half_width"])
		for s_raw in (run.get("spans", []) as Array):
			var span: Dictionary = s_raw as Dictionary
			var eid := String(span.get("element", ""))
			if not by_id.has(eid):
				continue
			var el: TrackGeom.Element = by_id[eid]
			if el.ballast_half_width_m < 0.0:
				el.ballast_half_width_m = hw
				el.type_id = type_id
				covered += 1

	stats["elements_with_width"] = covered
	stats["elements_without_width"] = elements.size() - covered
	var bare: Array[String] = []
	for el in elements:
		if el.ballast_half_width_m < 0.0:
			bare.append(el.id)
	stats["elements_without_width_ids"] = bare

	# Прочее, что сеть прислала и что эта веха НЕ рисует. Числа названы, чтобы
	# «не нарисовано» не путали с «не прислано».
	stats["trackside_received"] = (network.get("trackside", []) as Array).size()
	stats["features_received"] = (network.get("features", []) as Array).size()
	stats["runs_received"] = (network.get("construction_runs", []) as Array).size()
	stats["track_types_received"] = (network.get("track_types", []) as Array).size()


func _draw_track(elements: Array[TrackGeom.Element]) -> void:
	var ribbon_mat := TrackView.ribbon_material(Color(0.92, 0.45, 0.18))
	var line_mat := TrackView.line_material(Color(1.0, 0.85, 0.25))
	var node := Node3D.new()
	node.name = "Track"
	world.add_child(node)

	for el in elements:
		var mi := MeshInstance3D.new()
		mi.name = el.id
		if el.ballast_half_width_m > 0.0:
			mi.mesh = TrackView.ribbon_mesh(el)
			mi.material_override = ribbon_mat
		else:
			mi.mesh = TrackView.line_mesh(el)
			mi.material_override = line_mat
		if mi.mesh == null:
			continue
		node.add_child(mi)


func _axis_bbox(axis: PackedVector2Array) -> Rect2:
	if axis.is_empty():
		return Rect2()
	var mn := axis[0]
	var mx := axis[0]
	for p in axis:
		mn = Vector2(minf(mn.x, p.x), minf(mn.y, p.y))
		mx = Vector2(maxf(mx.x, p.x), maxf(mx.y, p.y))
	return Rect2(mn, mx - mn)


func _load_terrain(rule: ChunkRule, axis: PackedVector2Array, bbox: Rect2) -> void:
	var terrain := Node3D.new()
	terrain.name = "Terrain"
	world.add_child(terrain)

	var mat := StandardMaterial3D.new()
	# Серый, а не зелёный: покров сервер не отдаёт вовсе. Зелень означала бы
	# траву, которой в контракте нет. Форму показывает освещение, а не цвет.
	mat.albedo_color = Color(0.62, 0.62, 0.60)
	mat.roughness = 0.95
	mat.cull_mode = BaseMaterial3D.CULL_DISABLED

	var candidates := rule.candidates(axis, bbox)
	var got := 0
	var empty := 0
	var verts := 0
	var tris := 0
	var z_min := INF
	var z_max := -INF
	var decode_usec := 0
	var build_usec := 0
	var by_level := {}
	var t0 := Time.get_ticks_usec()

	for c_raw in candidates:
		var c: Dictionary = c_raw
		var path := "/regions/%s/chunks/%d/%d/%d" % [region, c["level"], c["cx"], c["cz"]]
		var r: Dictionary = await net.fetch(path)
		if not r["ok"]:
			_fail("чанк %d/%d/%d: %s" % [c["level"], c["cx"], c["cz"], r["error"]])
			continue
		var code: int = r["code"]
		if code == 204:
			# Законная пустота: чанка здесь нет. Не отказ, и рисовать нечего —
			# базовой поверхности сервер не описывает, значит её и не будет.
			empty += 1
			continue
		if code != 200:
			_fail("чанк %d/%d/%d: сервер ответил HTTP %d" % [c["level"], c["cx"], c["cz"], code])
			continue

		var base_hdr := NetClient.header_value(r["headers"], "X-Chunk-Base-Z-Mm")
		if base_hdr == "":
			# Без базы отсчёты не значат ничего. Подставить ноль значило бы
			# нарисовать чанк на неизвестной высоте — ровно тот случай, ради
			# которого писан закон.
			_fail("чанк %d/%d/%d: нет заголовка X-Chunk-Base-Z-Mm — высоты не к чему отложить" % [c["level"], c["cx"], c["cz"]])
			continue
		var base_z := float(base_hdr.to_int()) / 1000.0

		var built := TerrainMesh.build(r["body"], base_z, c["level"], c["cx"], c["cz"], rule)
		if not built["ok"]:
			_fail(String(built["error"]))
			continue

		got += 1
		by_level[c["level"]] = int(by_level.get(c["level"], 0)) + 1
		verts += int(built["vertices"])
		tris += int(built["triangles"])
		z_min = minf(z_min, float(built["z_min"]))
		z_max = maxf(z_max, float(built["z_max"]))
		decode_usec += int(built["decode_usec"])
		build_usec += int(built["build_usec"])

		var mi := MeshInstance3D.new()
		mi.name = "C%d_%d_%d" % [c["level"], c["cx"], c["cz"]]
		mi.mesh = built["mesh"]
		mi.material_override = mat
		terrain.add_child(mi)

	stats["chunks_requested"] = candidates.size()
	stats["chunks_200"] = got
	stats["chunks_204"] = empty
	stats["chunks_by_level"] = by_level
	stats["vertices"] = verts
	stats["triangles"] = tris
	stats["z_min"] = z_min
	stats["z_max"] = z_max
	stats["decode_usec_total"] = decode_usec
	stats["decode_usec_per_chunk"] = float(decode_usec) / maxf(1.0, float(got))
	stats["decode_ns_per_sample"] = float(decode_usec) * 1000.0 / maxf(1.0, float(verts))
	stats["mesh_build_usec_total"] = build_usec
	stats["terrain_wall_ms"] = float(Time.get_ticks_usec() - t0) / 1000.0
	# Охват рельефа — радиус последнего уровня ИЗ МАНИФЕСТА, а не «сколько-то
	# километров»: за ним чанков не хранится вовсе.
	stats["terrain_radius_m"] = rule.radius_of(rule.max_level)


func _place_camera(bbox: Rect2) -> void:
	# Камера наводится по ГАБАРИТАМ ДАННЫХ: центр пути и его размер, высота —
	# середина диапазона приехавших отсчётов. Ни одного числа о мире здесь нет.
	var cx := bbox.position.x + bbox.size.x * 0.5
	var cy := bbox.position.y + bbox.size.y * 0.5
	var mid_z := 0.0
	if stats.has("z_min") and float(stats["z_min"]) < INF:
		mid_z = (float(stats["z_min"]) + float(stats["z_max"])) * 0.5
	var centre := TerrainMesh.to_godot(cx, cy, mid_z)
	var radius := maxf(bbox.size.length() * 0.5, 1.0)
	if shot_view == "wide":
		# Весь приехавший рельеф: полоса вокруг оси шириной в радиус последнего
		# уровня — число из манифеста, а не из головы.
		radius = maxf(radius, float(stats.get("terrain_radius_m", radius)))
		camera.frame_bounds(centre, radius, 48.0, 1.5)
	else:
		camera.frame_bounds(centre, radius, 30.0, 1.25)


func _refresh_ui() -> void:
	ui_label.text = _hud_text()


func _hud_text() -> String:
	var l: Array[String] = []
	l.append("[b]ClearAhead — клиент В1[/b]   %s   регион %s" % [server_url, region])
	if stats.has("revision"):
		l.append("эпоха %s, ревизия %s, track %s…, network %s…" % [
			stats.get("epoch"), stats.get("revision"), stats.get("track_hash"), stats.get("network_hash")])
	if stats.has("rule"):
		l.append("правило подробности (из манифеста): %s" % stats["rule"])
	l.append("")
	if stats.has("elements"):
		l.append("[b]путь[/b]: элементов %d, длина по примитивам %.2f м (объявлено %.2f м, расхождение %.4f м)" % [
			stats["elements"], stats["length_total_m"], stats["length_declared_m"], stats["length_mismatch_m"]])
		l.append("  лентой (ширина из track_types) %d, нитью — ширины не прислали %d: %s" % [
			stats["elements_with_width"], stats["elements_without_width"],
			", ".join(stats["elements_without_width_ids"])])
	if stats.has("chunks_requested"):
		l.append("[b]рельеф[/b]: запрошено %d, 200 → %d, 204 (чанка нет) → %d" % [
			stats["chunks_requested"], stats["chunks_200"], stats["chunks_204"]])
		l.append("  по уровням: %s" % str(stats["chunks_by_level"]))
		l.append("  вершин %d, треугольников %d, высоты %.2f…%.2f м" % [
			stats["vertices"], stats["triangles"], stats["z_min"], stats["z_max"]])
		l.append("  decode_s16: %d мкс всего, %.0f мкс на чанк, %.1f нс на отсчёт" % [
			stats["decode_usec_total"], stats["decode_usec_per_chunk"], stats["decode_ns_per_sample"]])
	l.append("")
	l.append("[i]получено и НЕ рисуется этой вехой: trackside %s, features %s[/i]" % [
		stats.get("trackside_received", 0), stats.get("features_received", 0)])
	l.append("[i]не рисуется, потому что сервер не отдаёт: покров, растительность, здания, небо[/i]")
	if errors.is_empty():
		l.append("[color=#7fd97f]отказов нет[/color]")
	else:
		l.append("[color=#ff6060][b]ОТКАЗЫ (%d):[/b][/color]" % errors.size())
		for e in errors:
			l.append("[color=#ff6060]  %s[/color]" % e)
	l.append("")
	l.append("[i]мышь (правая) — обзор, WASD — движение, Q/E — вниз/вверх, колесо — скорость[/i]")
	return "\n".join(l)


func _print_report() -> void:
	print("=== ClearAhead client, регион %s, сервер %s ===" % [region, server_url])
	for k in ["epoch", "revision", "track_hash", "network_hash", "rule",
			"elements", "length_total_m", "length_declared_m", "length_mismatch_m",
			"elements_with_width", "elements_without_width", "elements_without_width_ids",
			"axis_points", "axis_bbox",
			"chunks_requested", "chunks_200", "chunks_204", "chunks_by_level",
			"vertices", "triangles", "z_min", "z_max",
			"decode_usec_total", "decode_usec_per_chunk", "decode_ns_per_sample",
			"mesh_build_usec_total", "terrain_wall_ms",
			"trackside_received", "features_received", "runs_received", "track_types_received"]:
		if stats.has(k):
			print("%-24s %s" % [k, stats[k]])
	print("%-24s %s" % ["http_codes", net.code_counts if net != null else {}])
	print("%-24s %d" % ["errors", errors.size()])
	for e in errors:
		print("  ОТКАЗ: %s" % e)


func _save_shot() -> void:
	# Кадр ДОЖДАТЬСЯ, а не форсировать: force_draw сразу после сборки сцены даёт
	# пустую картинку. Два кадра, не один.
	await RenderingServer.frame_post_draw
	await RenderingServer.frame_post_draw
	var tex := get_viewport().get_texture()
	if tex == null:
		print("СНИМОК НЕ СДЕЛАН: у окна нет текстуры (headless не рисует)")
		return
	var img := tex.get_image()
	if img == null:
		print("СНИМОК НЕ СДЕЛАН: get_image() вернул null (headless не рисует)")
		return
	var err := img.save_png(shot_path)
	print("СНИМОК %s: %s (%dx%d, save_png=%d)" % [
		"СОХРАНЁН" if err == OK else "НЕ СОХРАНЁН", shot_path, img.get_width(), img.get_height(), err])
