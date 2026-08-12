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

## Галочка крестовины. Длина крыла и его полуширина — ШТРИХ ДЛЯ ЧИТАЕМОСТИ,
## прямо отнесённый к клиенту таблицей владения render-contract §2: «ничем не
## измеряется на месте». Точка и обе касательные приезжают с сервера.
## Крыло длиннее и тоньше, чем «на глаз»: при марке 1/9 касательные расходятся на
## 6.4°, и два широких крыла сливаются в одну полосу — галочка перестаёт быть
## галочкой. 8 м на 0.15 м разводит концы почти на метр, и V читается.
## Длина упора вдоль пути как доля его ширины. Ширина и высота присланы, длина
## НЕТ — и это решение художника того же рода, что длина крыла крестовины: упор,
## нарисованный без длины, не читался бы как препятствие. Названо здесь, а не
## спрятано в функции, чтобы выдумка была видна списком.
const BUFFER_STOP_LENGTH_RATIO := 0.33
const FROG_WING_M := 8.0
const FROG_HALF_W_M := 0.15

## Корневые поля, которые клиент умеет читать. Список нужен не для порядка:
## поле `trackside` переименовали в `structures` (3637504), клиент остался на
## старом имени и писал в HUD «получено и НЕ рисуется: trackside 0» — враньё
## дважды. Теперь расхождение имён — ОТКАЗ на экране, а не тихий ноль:
## пропавшее известное поле кричит, а незнакомое новое называется отдельно.
const MANIFEST_FIELDS := ["region", "epoch", "revision", "network_model_hash", "network_hash", "chunks"]
const NETWORK_FIELDS := ["region", "revision", "elements", "structures", "track_types",
	"construction_runs", "features", "placement_algorithm"]
const ELEMENT_FIELDS := ["id", "kind", "start", "primitives", "profile", "role"]

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
	ui_label.custom_minimum_size = Vector2(700, 0)
	ui_label.size = Vector2(700, 0)
	# Мельче, чем было: числа выросли (шпалы, нитки, устройства), а панель не
	# должна закрывать то, о чём отчитывается. 12 пунктов ещё читаются на 1600×900.
	ui_label.add_theme_font_size_override("normal_font_size", 12)
	ui_label.add_theme_font_size_override("bold_font_size", 12)
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


## _check_fields — сверка имён с ТЕМ, ЧТО ПРИСЛАЛИ, а не с памятью.
##
## Пропавшее известное поле — отказ: значит, контракт переименовали, а клиент
## читает мимо и молча показывает ноль. Незнакомое поле отказом не является
## (сервер вправе прирастать), но называется вслух: пара «пропало A, появилось
## B» — это и есть переименование, увиденное с клиентской стороны.
func _check_fields(d: Dictionary, known: Array, where: String) -> void:
	var missing: Array[String] = []
	for k in known:
		if not d.has(k):
			missing.append(String(k))
	if not missing.is_empty():
		_fail("%s: нет полей %s — клиент читает по именам, которых сервер больше не шлёт" % [
			where, ", ".join(missing)])
	var unknown: Array[String] = []
	for k in d.keys():
		if not known.has(k):
			unknown.append(String(k))
	if not unknown.is_empty():
		var seen: Array = stats.get("unknown_fields", []) as Array
		seen.append("%s: %s" % [where, ", ".join(unknown)])
		stats["unknown_fields"] = seen


## _check_fields_element — то же по элементу, но `role` НЕОБЯЗАТЕЛЕН.
##
## Отсутствие роли значит «элемент не часть устройства», а не «данные потеряны»:
## у пяти из девяти элементов затравки роли нет по построению.
func _check_fields_element(e: Dictionary) -> void:
	var missing: Array[String] = []
	for k in ["id", "kind", "start", "primitives"]:
		if not e.has(k):
			missing.append(k)
	if not missing.is_empty():
		_fail("элемент %s: нет полей %s" % [String(e.get("id", "(без id)")), ", ".join(missing)])
	var unknown: Array[String] = []
	for k in e.keys():
		if not ELEMENT_FIELDS.has(k):
			unknown.append(String(k))
	if not unknown.is_empty():
		var seen: Array = stats.get("unknown_fields", []) as Array
		var line := "элемент %s: %s" % [String(e.get("id", "")), ", ".join(unknown)]
		if not seen.has(line):
			seen.append(line)
		stats["unknown_fields"] = seen


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
	_check_fields(man, MANIFEST_FIELDS, "манифест региона")
	stats["epoch"] = man.get("epoch", null)
	stats["revision"] = man.get("revision", null)
	# Хеш модели сети называется network_model_hash: `track_hash` — имя, которое
	# пережило свой ресурс (geometry снесён вместе с JSON) и держалось в клиенте
	# только потому, что String(get(…, "")) на пропавшем поле даёт пустую строку,
	# а пустая строка на экране выглядит как «просто короткий хеш».
	stats["network_model_hash"] = String(man.get("network_model_hash", "")).substr(0, 12)
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
	_check_fields(network, NETWORK_FIELDS, "сеть региона")
	stats["placement_algorithm"] = String(network.get("placement_algorithm", ""))

	var elements := _parse_elements(network)
	if elements.is_empty():
		_fail("сеть региона пуста: ни одного элемента — рисовать нечего")
		_refresh_ui()
		return

	_apply_track_types(network, elements)
	_draw_track(elements, network)

	# 3. Рельеф. Адреса чанков клиент выводит САМ, из правила манифеста и
	#    собственной оси.
	var axis := TrackGeom.sample_axis(elements, AXIS_SAMPLE_STEP_M)
	var bbox := _axis_bbox(axis)
	stats["axis_points"] = axis.size()
	stats["axis_bbox"] = "x %.1f…%.1f, y %.1f…%.1f" % [bbox.position.x, bbox.end.x, bbox.position.y, bbox.end.y]

	await _load_terrain(rule, axis, bbox)

	_place_camera(bbox, elements)
	_refresh_ui()


func _parse_elements(network: Dictionary) -> Array[TrackGeom.Element]:
	var out: Array[TrackGeom.Element] = []
	var total := 0.0
	var declared := 0.0
	var raw: Array = network.get("elements", []) as Array
	for e_raw in raw:
		_check_fields_element(e_raw as Dictionary)
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

	# Проходы стрелок: run'ами не покрыты по правилу, но с 2026-08-12 несут
	# СВОЙ тип в role.type (контракт редакции 6 §6). До того у ветвей не было ни
	# одного размера, и это было видно на кадре — они рисовались ниткой. Правило
	# «нет цепочки до типа — нет и размера» не ослаблено: цепочка просто стала
	# доходить туда, куда раньше обрывалась.
	var by_role := 0
	for el in elements:
		if el.ballast_half_width_m >= 0.0 or el.role.is_empty():
			continue
		var dev_type := String(el.role.get("type", ""))
		if not types.has(dev_type):
			continue
		var dt: Dictionary = types[dev_type]
		var db: Dictionary = dt.get("ballast", {}) as Dictionary
		if not db.has("half_width"):
			continue
		el.ballast_half_width_m = float(db["half_width"])
		el.type_id = dev_type
		by_role += 1

	stats["elements_with_width"] = covered + by_role
	stats["elements_width_from_device_type"] = by_role
	stats["elements_without_width"] = elements.size() - covered - by_role
	var bare: Array[String] = []
	for el in elements:
		if el.ballast_half_width_m < 0.0:
			bare.append(el.id)
	stats["elements_without_width_ids"] = bare

	# Сколько чего ПРИСЛАНО. Числа названы отдельно от «сколько нарисовано»,
	# чтобы «не нарисовано» не путали с «не прислано». Поле называется
	# `structures`: имя `trackside` умерло в 3637504, а клиент читал его и дальше,
	# показывая «получено и НЕ рисуется: trackside 0» — враньё дважды.
	stats["structures_received"] = (network.get("structures", []) as Array).size()
	stats["features_received"] = (network.get("features", []) as Array).size()
	stats["runs_received"] = (network.get("construction_runs", []) as Array).size()
	stats["track_types_received"] = (network.get("track_types", []) as Array).size()


## _draw_track — всё, что приехало про путь, слоями.
##
## Порядок слоёв снизу вверх: отсыпка, платформа, шпалы, нитки, галочки
## крестовин. Вертикаль отсчитывается ВНИЗ от головки рельса — причина в шапке
## track_view.gd, и
## она же написана на экране.
func _draw_track(elements: Array[TrackGeom.Element], network: Dictionary) -> void:
	var node := Node3D.new()
	node.name = "Track"
	world.add_child(node)

	var by_id := TrackBuild.elements_by_id(elements)
	var spans := TrackBuild.covered_spans(network, by_id, TESS_MAX_SEG_M, TESS_MAX_ANG_RAD)

	# 1. Балластная призма — ТЕЛОМ, если прислан весь вертикальный стек, и лентой
	#    на отметке оси, если прислана только полуширина. Второй случай оставлен
	#    не для красоты: так выглядит ответ сервера, не знающего редакции 6, и
	#    клиент обязан показать его честно, а не исчезнуть.
	#
	#    По УЧАСТКАМ прогонов, а не по элементам целиком: спан вправе покрывать
	#    часть элемента, и лента во всю его длину была бы отсыпкой там, где её не
	#    объявляли.
	var ballast := Node3D.new()
	ballast.name = "Ballast"
	node.add_child(ballast)
	var prism_mat := TrackView.solid_material(Color(0.46, 0.44, 0.41))
	var flat_ballast_mat := TrackView.flat_material(Color(0.40, 0.38, 0.36), TrackView.PRIO_BALLAST)
	var covered := {}
	var prisms := 0
	var ribbons := 0
	for sp in spans:
		covered[sp.element_id] = true
		var mi := MeshInstance3D.new()
		mi.name = "%s@%s" % [sp.element_id, sp.run_id if sp.from_run else sp.type_id]
		if sp.has_prism():
			mi.mesh = TrackView.prism_mesh(sp)
			mi.material_override = prism_mat
			prisms += 1
		elif sp.ballast_half_width_m > 0.0:
			mi.mesh = TrackView.ribbon_mesh(sp.axis, sp.ballast_half_width_m)
			mi.material_override = flat_ballast_mat
			ribbons += 1
		if mi.mesh == null:
			continue
		ballast.add_child(mi)
	stats["ballast_prisms_drawn"] = prisms
	stats["ballast_ribbons_drawn"] = ribbons

	# 2. Платформа. Полоса от offset до offset + width со стороны side на
	#    протяжении spans — все четыре числа присланы, ни одного своего.
	var plat_res := TrackBuild.platforms(network, by_id, TESS_MAX_SEG_M, TESS_MAX_ANG_RAD)
	var platforms: Array[TrackBuild.PlatformStrip] = plat_res["list"]
	var plat_node := Node3D.new()
	plat_node.name = "Platforms"
	node.add_child(plat_node)
	var slab_mat := TrackView.solid_material(Color(0.78, 0.77, 0.74))
	var plat_mat := TrackView.flat_material(Color(0.74, 0.73, 0.70), TrackView.PRIO_PLATFORM)
	var plats_drawn := 0
	var slabs := 0
	for p in platforms:
		var mi := MeshInstance3D.new()
		mi.name = p.id
		if p.has_slab():
			mi.mesh = TrackView.slab_mesh(p)
			mi.material_override = slab_mat
			slabs += 1
		else:
			mi.mesh = TrackView.strip_mesh(p.near, p.far)
			mi.material_override = plat_mat
		if mi.mesh == null:
			continue
		plat_node.add_child(mi)
		plats_drawn += 1
		var caption := "%s  %s  %.2f…%.2f м от оси" % [p.id, p.side, p.offset_m, p.offset_m + p.width_m]
		if p.has_slab():
			caption += "  +%.2f м над УГР" % p.height_m
		plat_node.add_child(_label(p.far[p.far.size() / 2], caption, Color(0.98, 0.98, 0.92)))
	stats["platforms_drawn"] = plats_drawn
	stats["platform_slabs_drawn"] = slabs
	stats["platforms_skipped"] = plat_res["skipped"]

	# 2б. Упоры. Габарит присланный: height над поверхностью катания, width
	#     поперёк. До 2026-08-12 их не отдавали вовсе, и снесённый спайк выводил
	#     их из топологии сам — этому клиенту такое запрещено.
	var bs_res := TrackBuild.buffer_stops(network, by_id)
	var stops: Array[TrackBuild.BufferStop] = bs_res["list"]
	var bs_mi := MeshInstance3D.new()
	bs_mi.name = "BufferStops"
	bs_mi.mesh = TrackView.buffer_stop_mesh(stops, BUFFER_STOP_LENGTH_RATIO)
	if bs_mi.mesh != null:
		bs_mi.material_override = TrackView.solid_material(Color(0.72, 0.16, 0.14))
		node.add_child(bs_mi)
	stats["buffer_stops_drawn"] = stops.size()
	stats["buffer_stops_skipped"] = bs_res["skipped"]

	# 3. Шпалы. Раскладка целиком из рецепта: phase + n·pitch по полуоткрытому
	#    правилу, поза аналитическая. Коробкой, если прислана sleeper.height, и
	#    плоским прямоугольником, если нет.
	var sl := TrackBuild.sleepers(network, by_id)
	var sleepers: Array[TrackBuild.Sleeper] = sl["list"]
	var sleeper_mi := MeshInstance3D.new()
	sleeper_mi.name = "Sleepers"
	sleeper_mi.mesh = TrackView.sleeper_mesh(sleepers)
	if sleeper_mi.mesh != null:
		sleeper_mi.material_override = TrackView.solid_material(Color(0.26, 0.20, 0.16))
		node.add_child(sleeper_mi)
	stats["sleepers_drawn"] = sleepers.size()
	stats["sleeper_runs"] = sl["runs"]
	stats["sleepers_skipped"] = sl["skipped"]

	# 4. Рельсы. Телом — объявленным упрощением (прямоугольник head_width ×
	#    rail.height, внутренней гранью на ±gauge/2), если ширина головки
	#    прислана. Символической ниткой в один пиксель, если нет: ширина нитки
	#    ЭКРАННАЯ, и полоса в метрах заявляла бы ширину головки, которой не
	#    прислали.
	var rails_node := Node3D.new()
	rails_node.name = "Rails"
	node.add_child(rails_node)
	var rail_mat := TrackView.solid_material(Color(0.62, 0.63, 0.66))
	var thread_mat := TrackView.flat_material(Color(0.90, 0.91, 0.95), TrackView.PRIO_RAIL, true)
	var threads: Array[PackedVector3Array] = []
	var rail_bodies := 0
	for sp in spans:
		if sp.has_rail_body():
			var rmi := MeshInstance3D.new()
			rmi.name = "%s@rail" % sp.element_id
			rmi.mesh = TrackView.rail_body_mesh(sp)
			if rmi.mesh != null:
				rmi.material_override = rail_mat
				rails_node.add_child(rmi)
				rail_bodies += 1
			continue
		threads.append_array(sp.threads())
	if not threads.is_empty():
		var thread_mi := MeshInstance3D.new()
		thread_mi.name = "Threads"
		thread_mi.mesh = TrackView.rail_mesh(threads)
		if thread_mi.mesh != null:
			thread_mi.material_override = thread_mat
			rails_node.add_child(thread_mi)
	stats["rail_bodies_drawn"] = rail_bodies
	stats["rail_threads_drawn"] = threads.size()
	stats["rail_spans_drawn"] = spans.size()

	# 5. Элементы, не покрытые НИ ОДНИМ прогоном, — нитью. Ни колеи, ни шпал, ни
	#    ширины у них нет, и взять их у соседа запрещено: «размер неизвестен»
	#    обязано быть видно.
	var bare_node := Node3D.new()
	bare_node.name = "Bare"
	node.add_child(bare_node)
	var line_mat := TrackView.flat_material(Color(1.0, 0.85, 0.25), TrackView.PRIO_LINE, true)
	var bare_drawn := 0
	for el in elements:
		if covered.has(el.id):
			continue
		var mi := MeshInstance3D.new()
		mi.name = el.id
		mi.mesh = TrackView.line_mesh(el.points)
		if mi.mesh == null:
			continue
		mi.material_override = line_mat
		bare_node.add_child(mi)
		bare_drawn += 1
	stats["bare_lines_drawn"] = bare_drawn

	# 6. Крестовины — галочкой по обеим присланным касательным.
	var fr := TrackBuild.frogs(network, by_id)
	var frogs: Array[TrackBuild.Frog] = fr["list"]
	var frog_mi := MeshInstance3D.new()
	frog_mi.name = "Frogs"
	frog_mi.mesh = TrackView.frog_mesh(frogs, FROG_WING_M, FROG_HALF_W_M)
	if frog_mi.mesh != null:
		frog_mi.material_override = TrackView.flat_material(Color(0.96, 0.28, 0.28), TrackView.PRIO_FROG, true)
		node.add_child(frog_mi)
	stats["frogs_drawn"] = frogs.size()
	stats["frogs_skipped"] = fr["skipped"]

	# 7. Стрелка — ОДНО устройство, а не две независимые ветви. Подпись несёт то,
	#    что прислано ролью: марку и рукость. Выводить сторону из геометрии не
	#    требуется — `hand` для того и есть, — и потому не делается.
	var devices := TrackBuild.devices(elements)
	var frog_by_owner := {}
	for f in frogs:
		frog_by_owner[f.owner] = f
	var dev_node := Node3D.new()
	dev_node.name = "Devices"
	node.add_child(dev_node)
	var dev_lines: Array[String] = []
	for d in devices:
		var mark := d.mark if d.mark != "" else "марки нет"
		var hand := d.hand if d.hand != "" else "рукости нет"
		dev_lines.append("%s (%s, %s): ветви %s — %s" % [
			d.id, mark, hand, "+".join(d.branches),
			"тип есть" if d.typed else "не покрыты ни одним construction_run, оттого без колеи и решётки"])
		if frog_by_owner.has(d.id):
			var f: TrackBuild.Frog = frog_by_owner[d.id]
			dev_node.add_child(_label(f.point, "%s  %s  %s" % [d.id, mark, hand], Color(1.0, 0.68, 0.68)))
	stats["devices"] = devices.size()
	stats["device_lines"] = dev_lines


## _label — подпись в мире.
##
## Экранная мебель: сообщает ИДЕНТИЧНОСТЬ присланного объекта (id, марка,
## рукость) и ничего к ней не добавляет. fixed_size — чтобы подпись читалась
## одинаково с любого удаления: её читают, а ею не меряют.
func _label(at: Vector3, text: String, colour: Color) -> Label3D:
	var l := Label3D.new()
	l.text = text
	l.position = TerrainMesh.to_godot(at.x, at.y, at.z)
	l.billboard = BaseMaterial3D.BILLBOARD_ENABLED
	l.fixed_size = true
	l.pixel_size = 0.0007
	l.font_size = 26
	l.outline_size = 8
	l.modulate = colour
	# Подпись поднята над точкой: иначе она ложится ровно на галочку крестовины,
	# которую и называет, и обе становятся нечитаемыми.
	l.offset = Vector2(0.0, -34.0)
	l.no_depth_test = true
	l.render_priority = 8
	return l


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
	# Цвет берётся ИЗ ВЕРШИНЫ, а вершина красится по крутизне (правило и его
	# обоснование — в шапке terrain_mesh.gd). Зелени по-прежнему нет: она
	# означала бы траву, которой в контракте нет. Есть ровно два тона — ровное и
	# крутое, — и оба выведены из присланных высот, как нормаль.
	mat.albedo_color = Color.WHITE
	mat.vertex_color_use_as_albedo = true
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
	var steep := 0
	var cover_got := 0
	var cover_empty := 0
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

		# Покров — второй запрос по ТОМУ ЖЕ адресу с хвостом /cover. Отдельным
		# телом, а не полем в первом: блобы разной природы и разной длины (8450
		# против 4096), и склеенные они заставили бы клиента без покрова возить
		# покров, а клиента без рельефа — разбирать заголовок длины.
		#
		# 204 значит «у карты нет рецепта покрова» — законное состояние, а не
		# сбой: земля тогда рисуется серой, как рисовалась до его появления.
		var cover := PackedByteArray()
		var cr: Dictionary = await net.fetch(path + "/cover")
		if cr["ok"] and int(cr["code"]) == 200:
			cover = cr["body"]
			cover_got += 1
		elif cr["ok"] and int(cr["code"]) == 204:
			cover_empty += 1
		elif cr["ok"]:
			_fail("покров %d/%d/%d: сервер ответил HTTP %d" % [c["level"], c["cx"], c["cz"], int(cr["code"])])
		else:
			_fail("покров %d/%d/%d: %s" % [c["level"], c["cx"], c["cz"], cr["error"]])

		var built := TerrainMesh.build(r["body"], base_z, c["level"], c["cx"], c["cz"], rule, cover)
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
		steep += int(built["steep_vertices"])

		var mi := MeshInstance3D.new()
		mi.name = "C%d_%d_%d" % [c["level"], c["cx"], c["cz"]]
		mi.mesh = built["mesh"]
		mi.material_override = mat
		terrain.add_child(mi)

	stats["chunks_requested"] = candidates.size()
	stats["chunks_200"] = got
	stats["chunks_204"] = empty
	stats["cover_200"] = cover_got
	stats["cover_204"] = cover_empty
	stats["chunks_by_level"] = by_level
	stats["vertices"] = verts
	stats["triangles"] = tris
	stats["z_min"] = z_min
	stats["z_max"] = z_max
	# Замер, а не оценка: сколько вершин попало в полосу «круче порога» — то есть
	# сколько земли покрасил уклон. Ноль значил бы, что правило не сработало
	# вовсе, и это надо видеть числом, а не искать глазами на снимке.
	stats["steep_vertices"] = steep
	stats["steep_share"] = float(steep) / maxf(1.0, float(verts))
	stats["steep_rule"] = "уклон %.2f…%.2f (решение художника)" % [
		TerrainMesh.SCARP_SLOPE_LO, TerrainMesh.SCARP_SLOPE_HI]
	stats["decode_usec_total"] = decode_usec
	stats["decode_usec_per_chunk"] = float(decode_usec) / maxf(1.0, float(got))
	stats["decode_ns_per_sample"] = float(decode_usec) * 1000.0 / maxf(1.0, float(verts))
	stats["mesh_build_usec_total"] = build_usec
	stats["terrain_wall_ms"] = float(Time.get_ticks_usec() - t0) / 1000.0
	# Охват рельефа — радиус последнего уровня ИЗ МАНИФЕСТА, а не «сколько-то
	# километров»: за ним чанков не хранится вовсе.
	stats["terrain_radius_m"] = rule.radius_of(rule.max_level)


func _place_camera(bbox: Rect2, elements: Array[TrackGeom.Element]) -> void:
	# Камера наводится по ГАБАРИТАМ ДАННЫХ: центр пути и его размер, высота —
	# середина диапазона приехавших отсчётов. Ни одного числа о мире здесь нет.
	var view_box := bbox
	if shot_view == "throat":
		# Горловина — не координата и не константа: это габарит ТЕХ ЭЛЕМЕНТОВ,
		# у которых сервер прислал role.turnout. Не прислал ни у одного — вида
		# нет, и это сказано вслух, а не подменено станцией.
		var throat := _role_bbox(elements)
		if throat.size == Vector2.ZERO:
			_fail("вид «горловина»: ни у одного элемента нет role.turnout — наводиться не на что")
		else:
			view_box = throat
	var cx := view_box.position.x + view_box.size.x * 0.5
	var cy := view_box.position.y + view_box.size.y * 0.5
	var mid_z := 0.0
	if stats.has("z_min") and float(stats["z_min"]) < INF:
		mid_z = (float(stats["z_min"]) + float(stats["z_max"])) * 0.5
	var centre := TerrainMesh.to_godot(cx, cy, mid_z)
	var radius := maxf(view_box.size.length() * 0.5, 1.0)
	stats["view"] = "%s: центр (%.1f, %.1f), радиус %.1f м" % [shot_view, cx, cy, radius]
	if shot_view == "wide":
		# Весь приехавший рельеф: полоса вокруг оси шириной в радиус последнего
		# уровня — число из манифеста, а не из головы.
		radius = maxf(radius, float(stats.get("terrain_radius_m", radius)))
		camera.frame_bounds(centre, radius, 48.0, 1.5)
	elif shot_view == "throat":
		# Круче наклон и ближе: решётку и галочку крестовины видно только сверху.
		camera.frame_bounds(centre, radius, 74.0, 1.35)
	else:
		# Наклон 55°, а не 30°: при пологом взгляде плоскость станции вырождается
		# в полоску, и решётка, платформа и крестовины сливаются в одну линию.
		# Наклон — свойство ВЗГЛЯДА, менять его закон разрешает.
		camera.frame_bounds(centre, radius, 70.0, 1.25)


## _role_bbox — габарит элементов, входящих в устройства.
func _role_bbox(elements: Array[TrackGeom.Element]) -> Rect2:
	var mn := Vector2(INF, INF)
	var mx := Vector2(-INF, -INF)
	var any := false
	for el in elements:
		if el.role.is_empty():
			continue
		for p_raw in el.points:
			var p: TrackGeom.AxisPoint = p_raw
			mn = Vector2(minf(mn.x, p.x), minf(mn.y, p.y))
			mx = Vector2(maxf(mx.x, p.x), maxf(mx.y, p.y))
			any = true
	if not any:
		return Rect2()
	return Rect2(mn, mx - mn)


func _refresh_ui() -> void:
	ui_label.text = _hud_text()


func _hud_text() -> String:
	var l: Array[String] = []
	l.append("[b]ClearAhead — клиент В1[/b]   %s   регион %s" % [server_url, region])
	if stats.has("revision"):
		l.append("эпоха %s, ревизия %s, network_model %s…, network %s…, раскладка %s" % [
			stats.get("epoch"), stats.get("revision"), stats.get("network_model_hash"),
			stats.get("network_hash"), stats.get("placement_algorithm")])
	if stats.has("rule"):
		l.append("правило подробности (из манифеста): %s" % stats["rule"])
	l.append("")
	if stats.has("elements"):
		l.append("[b]путь[/b]: элементов %d, длина по примитивам %.2f м (объявлено %.2f м, расхождение %.4f м)" % [
			stats["elements"], stats["length_total_m"], stats["length_declared_m"], stats["length_mismatch_m"]])
		l.append("  призма телом на %d участках, лентой на %d, нитью (без размеров) %d элемента: %s" % [
			stats.get("ballast_prisms_drawn", 0), stats.get("ballast_ribbons_drawn", 0),
			stats.get("bare_lines_drawn", 0), ", ".join(stats["elements_without_width_ids"])])
		l.append("  шпал %d (рецепт: phase + n·pitch, полуоткрыто), рельсов телом %d, ниток %d, участков %d" % [
			stats.get("sleepers_drawn", 0), stats.get("rail_bodies_drawn", 0),
			stats.get("rail_threads_drawn", 0), stats.get("rail_spans_drawn", 0)])
		l.append("  платформ %d (плитой %d), упоров %d, крестовин %d, стрелок %d" % [
			stats.get("platforms_drawn", 0), stats.get("platform_slabs_drawn", 0),
			stats.get("buffer_stops_drawn", 0), stats.get("frogs_drawn", 0), stats.get("devices", 0)])
		if int(stats.get("elements_width_from_device_type", 0)) > 0:
			l.append("  из них %d ветви стрелок: размеры по role.type, run'ами они не покрыты" % [
				stats.get("elements_width_from_device_type", 0)])
		for d in (stats.get("device_lines", []) as Array):
			l.append("    %s" % d)
	if stats.has("chunks_requested"):
		l.append("[b]рельеф[/b]: запрошено %d, 200 → %d, 204 (чанка нет) → %d" % [
			stats["chunks_requested"], stats["chunks_200"], stats["chunks_204"]])
		l.append("  по уровням: %s" % str(stats["chunks_by_level"]))
		l.append("  покров: 200 → %d, 204 (рецепта покрова нет) → %d; класс и сомкнутость на ячейку 64×64" % [
			stats.get("cover_200", 0), stats.get("cover_204", 0)])
		l.append("  вершин %d, треугольников %d, высоты %.2f…%.2f м" % [
			stats["vertices"], stats["triangles"], stats["z_min"], stats["z_max"]])
		l.append("  крутизной покрашено %d вершин (%.1f %%), порог %s" % [
			stats.get("steep_vertices", 0), 100.0 * float(stats.get("steep_share", 0.0)),
			stats.get("steep_rule", "")])
		l.append("  decode_s16: %d мкс всего, %.0f мкс на чанк, %.1f нс на отсчёт" % [
			stats["decode_usec_total"], stats["decode_usec_per_chunk"], stats["decode_ns_per_sample"]])
	l.append("")
	l.append("[b]ВЕРТИКАЛЬ ОТ ГОЛОВКИ РЕЛЬСА.[/b] z элемента — поверхность катания")
	l.append("(контракт отрисовки, редакция 6). Вниз от неё: рельс, шпала, призма. Земля — на")
	l.append("formation_to_rail_top ниже, и это число сервер считает тем же, чем земляные работы.")
	l.append("[i]объявленное упрощение: рельс — прямоугольник head_width × height; профиля")
	l.append("рельса в контракте нет. Длина упора вдоль пути и длина крыла крестовины — стиль.[/i]")
	l.append("[i]ЦВЕТ КЛАССА — единственное, что этот клиент ещё держит сам. По границе")
	l.append("владения он серверный и приедет каталогом ассетов; класс поверхности прислан.[/i]")
	l.append("[i]не рисуется, потому что сервер не отдаёт вовсе: трава, деревья, вода,")
	l.append("здания, небо, решётка стрелки (переводные брусья).[/i]")
	l.append("[i]204 (чанка нет) оставлено дырой: base_z региона в манифест не приезжает.[/i]")
	for s in (stats.get("sleepers_skipped", []) as Array):
		l.append("[color=#ffc060]решётка пропущена — %s[/color]" % s)
	for s in (stats.get("platforms_skipped", []) as Array):
		l.append("[color=#ffc060]сооружение пропущено — %s[/color]" % s)
	for s in (stats.get("buffer_stops_skipped", []) as Array):
		l.append("[color=#ffc060]упор пропущен — %s[/color]" % s)
	for s in (stats.get("frogs_skipped", []) as Array):
		l.append("[color=#ffc060]особенность пропущена — %s[/color]" % s)
	for s in (stats.get("unknown_fields", []) as Array):
		l.append("[color=#ffc060]прислано, но клиент такого поля не знает — %s[/color]" % s)
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
	for k in ["epoch", "revision", "network_model_hash", "network_hash", "placement_algorithm", "rule",
			"elements", "length_total_m", "length_declared_m", "length_mismatch_m",
			"elements_with_width", "elements_without_width", "elements_without_width_ids",
			"axis_points", "axis_bbox", "view",
			"structures_received", "features_received", "runs_received", "track_types_received",
			"ballast_ribbons_drawn", "bare_lines_drawn", "sleepers_drawn", "sleeper_runs",
			"sleepers_skipped", "rail_threads_drawn", "rail_spans_drawn",
			"platforms_drawn", "platforms_skipped", "frogs_drawn", "frogs_skipped",
			"devices", "device_lines", "unknown_fields",
			"chunks_requested", "chunks_200", "chunks_204", "chunks_by_level",
			"vertices", "triangles", "z_min", "z_max",
			"steep_vertices", "steep_share", "steep_rule",
			"decode_usec_total", "decode_usec_per_chunk", "decode_ns_per_sample",
			"mesh_build_usec_total", "terrain_wall_ms"]:
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
