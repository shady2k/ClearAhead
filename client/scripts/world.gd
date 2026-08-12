## World — сборка сцены из того, и только из того, что прислал сервер.
##
## Строится ОБОЛОЧКОЙ (app.gd) и живёт ровно столько, сколько игрок находится в
## роли. До 2026-08-12 этот файл звался main.gd и был приложением целиком: сам
## разбирал ключи запуска, сам решал, какой регион показать, и выйти из него было
## некуда. Разделение не косметическое — у мира появился владелец, который умеет
## его снести и построить заново другой ролью, не ходя в сеть повторно.
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
## Рассев растительности — решения художника. Названы константами и здесь,
## чтобы выдумка была видна списком, а не растворялась в коде.
## Порог сомкнутости: ниже него трава не появляется вовсе (голая земля).
## Высота глаза и дальность взгляда для вида «track». Решения художника: у
## человека глаз около 1.7 м, а смотреть надо туда, где путь ещё читается.
const EYE_HEIGHT_M := 1.75
const EYE_LOOK_AHEAD_M := 120.0

const GRASS_MIN_CLOSURE := 4
const GRASS_MAX_PER_CELL := 14
const GRASS_H_MIN := 0.22
const GRASS_H_MAX := 0.55
const BUSH_CHANCE := 0.055
const BUSH_H_MIN := 0.9
const BUSH_H_MAX := 2.6

## Постройки: всё это решения художника, названные списком.
const BUILDING_SINK := 1.5
const ROOF_OVERHANG := 0.7
const ROOF_THICKNESS := 0.6
## Насколько кровля САДИТСЯ НА СТЕНУ, а не встаёт над ней. Порт из спайка вместе
## с доводом: плита, поставленная ровно на срез стены, оставляет щель, и дом
## читается висящим на ножках.
const ROOF_SEAT := 0.3
## C_ROOF — не цвет кровли, а то, КУДА цвет кровли уводится от цвета стен.
## Разница существенная, и она куплена ошибкой: до 2026-08-12 крыша красилась
## прямо в C_ROOF, и посёлок с высоты строителя читался серыми плитами — все
## двенадцать домов одинаковыми. Спайк записал причину рядом со своим кодом:
## СВЕРХУ ВИДНО КРЫШУ, значит цвет несёт она, а стены только оттеняют.
const C_ROOF := Color(0.38, 0.39, 0.41)
const ROOF_TINT := 0.18   # доля увода кровли к C_ROOF
const WALL_SHADE := 0.22  # насколько стена темнее своего цвета
const HOUSE_COLORS := [
	Color(0.84, 0.83, 0.79), Color(0.74, 0.62, 0.64), Color(0.66, 0.72, 0.63),
	Color(0.55, 0.50, 0.62), Color(0.82, 0.75, 0.60), Color(0.60, 0.66, 0.72),
]

const BUFFER_STOP_LENGTH_RATIO := 0.33
## Крыло галочки крестовины. Восемь метров годились СХЕМЕ СВЕРХУ, где галочка
## была единственным способом показать особенность; на виде с оси она стала
## красной полосой во весь кадр. Число уменьшено до порядка настоящего крыла
## крестовины — это по-прежнему решение художника, но теперь оно хотя бы не
## спорит с масштабом того, поверх чего лежит.
const FROG_WING_M := 1.4
const FROG_HALF_W_M := 0.07

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
var quit_when_done := false

## Роль и её камера. Приходят от оболочки: КТО смотрит — не свойство мира.
var role := -1
var role_name := ""
var role_hints := ""
var role_camera := {}
## На что наводиться: network — вся сеть, throat — только устройства, terrain —
## весь приехавший рельеф. Это ГАБАРИТ, а не взгляд, и берётся он из данных.
var frame := "network"

var world: Node3D
var ui_label: RichTextLabel
var camera: Camera3D
var net: NetClient

var stats := {}
var errors: Array[String] = []
var report_lines: Array[String] = []


## configure — всё, что мир получает СНАРУЖИ, одним вызовом и до входа в дерево.
##
## До входа — потому что загрузка начинается в _ready: настроенный после этого
## мир успел бы сходить в сеть не за тем регионом.
func configure(cfg: Dictionary) -> void:
	server_url = String(cfg.get("server_url", server_url)).rstrip("/")
	region = String(cfg.get("region", region))
	role = int(cfg.get("role", -1))
	role_name = String(cfg.get("role_name", ""))
	role_hints = String(cfg.get("role_hints", ""))
	role_camera = cfg.get("camera", {}) as Dictionary
	frame = String(cfg.get("frame", frame))
	shot_path = String(cfg.get("shot_path", ""))
	quit_when_done = bool(cfg.get("quit_when_done", false))


func _ready() -> void:
	_build_scene()
	await _load_world()
	_print_report()
	if shot_path != "":
		await _save_shot()
	if quit_when_done:
		get_tree().quit(1 if not errors.is_empty() else 0)


## set_input_enabled — отдать или отобрать у камеры жесты. Зовёт оболочка,
## когда поверх мира появляется меню.
func set_input_enabled(on: bool) -> void:
	if camera == null:
		return
	if camera.has_method("set_active"):
		camera.call("set_active", on)
	else:
		camera.set_process_unhandled_input(on)
		camera.set_process(on)


func _build_scene() -> void:
	world = Node3D.new()
	world.name = "World"
	add_child(world)

	# НЕБО, СВЕТ И ВОЗДУХ — РЕШЕНИЕ ХУДОЖНИКА ЦЕЛИКОМ, и это записано в границе
	# владения: пока в мире нет времени суток и погоды, азимут солнца и цвет
	# неба не являются фактами о месте. Как появятся широта и час — азимут и
	# высота солнца станут миром, а палитра неба останется здесь.
	#
	# До 2026-08-12 фоном стоял тёмно-серый цвет, и это была не заглушка, а
	# следствие прежнего закона: земля была серой, потому что покрова не
	# присылали, и небо цвета неба над серой землёй выглядело бы враньём.
	# Покров приехал — врать больше нечем.
	var env := WorldEnvironment.new()
	var e := Environment.new()
	e.background_mode = Environment.BG_SKY
	var sky := Sky.new()
	var pmat := ProceduralSkyMaterial.new()
	pmat.sky_top_color = Color(0.30, 0.50, 0.78)
	pmat.sky_horizon_color = Color(0.78, 0.86, 0.93)
	pmat.ground_horizon_color = Color(0.74, 0.81, 0.87)
	pmat.ground_bottom_color = Color(0.52, 0.60, 0.57)
	pmat.sun_angle_max = 12.0
	sky.sky_material = pmat
	e.sky = sky
	# Свет неба, а не выдуманный ambient: цвет заливки берётся из того же неба,
	# и небо с землёй перестают спорить о том, какого цвета воздух.
	e.ambient_light_source = Environment.AMBIENT_SOURCE_SKY
	e.ambient_light_energy = 1.0
	# Дымка. Числа спайка (разбор §1.6): на 500 м даёт заметное смягчение, на
	# 2 км — сплошную завесу, отчего дальний край рельефа перестаёт резать глаз
	# ступенькой уровня подробности.
	e.fog_enabled = true
	e.fog_density = 0.00018
	e.fog_aerial_perspective = 0.30
	e.fog_sky_affect = 0.0
	# SSAO НЕ включается: проект собран мобильным рендерером, и там его нет —
	# движок отвечает предупреждением и молча игнорирует. Оставлять включённым
	# значило бы держать в коде настройку, которая ничего не делает, и объяснять
	# следующему, почему её не видно на кадре.
	env.environment = e
	add_child(env)

	var sun := DirectionalLight3D.new()
	sun.name = "Sun"
	sun.rotation = Vector3(deg_to_rad(-48.0), deg_to_rad(-128.0), 0.0)
	sun.light_energy = 1.05
	sun.light_color = Color(1.0, 0.97, 0.90)
	sun.shadow_enabled = true
	sun.directional_shadow_max_distance = 900.0
	sun.light_angular_distance = 1.6
	# Смещения теней — не вкус, а лечение САМОЗАТЕНЕНИЯ: без них решётка и
	# призма покрывались собственной тенью от каждой шпалы и уходили в чёрное.
	# Видно только на виде с оси: сверху тень падала мимо камеры.
	sun.shadow_bias = 0.05
	sun.shadow_normal_bias = 2.0
	add_child(sun)

	# КАМЕРА ПО РОЛИ. Орбитальная там, где смотрят НА место (строитель, ДСП), и
	# свободная там, где смотрят ИЗ места (машинист). Разница не в удобстве: у
	# первой мышь обходит станцию кругом, у второй — поворачивает голову.
	#
	# Роль называет тип и углы; куда навести и в каком масштабе — считается из
	# габаритов приехавших данных (_place_camera).
	camera = Camera3D.new()
	camera.name = "Camera"
	camera.set_script(load("res://scripts/orbit_camera.gd" if not role_camera.is_empty()
		else "res://scripts/free_camera.gd"))
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
	var prism_mat := TrackView.ballast_material()
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
		sleeper_mi.material_override = TrackView.sleeper_material()
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
	# Рельс: тело ржавое (диэлектрик, шероховатое), накат металлический. Числа и
	# довод — из спайка: 0.55/0.35 были «полуметалл, какого не бывает».
	var rail_mat := TrackView.solid_material(Color(0.42, 0.40, 0.40))
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

	# ЗЕМЛЯ РИСУЕТСЯ ШЕЙДЕРОМ, а не плоским цветом вершины: цвет из покрова даёт
	# верный ОБЩИЙ план и ровно ничего с высоты человека — кадр заполняет один
	# ровный зелёный. Шейдер добавляет зерно ближнего плана (дернина и грунт),
	# и биом ему задаёт СЕРВЕР: класс и сомкнутость лежат в вершине, цвет в RGB,
	# травянистость в альфе. Собственного шума у шейдера нет.
	var mat := GroundLook.material()

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
	# Ярусы уровня 0 копятся для рассева растительности: покров говорит, ГДЕ
	# что растёт, высоты — на какой отметке, битовая карта леса — где ствол.
	var ground: Array[Dictionary] = []
	var terrain_box := Rect2()
	var cover_got := 0
	var forest_got := 0
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

		# Лес — только уровень 0: за коридором деревья рассыпает клиент по покрову,
		# потому что рубить их некому (контракт чанков §5а, разбор раскладки).
		var forest := PackedByteArray()
		if int(c["level"]) == 0:
			var fr2: Dictionary = await net.fetch(path + "/forest")
			if fr2["ok"] and int(fr2["code"]) == 200:
				forest = fr2["body"]
				forest_got += 1
			elif not fr2["ok"]:
				_fail("лес %d/%d/%d: %s" % [c["level"], c["cx"], c["cz"], fr2["error"]])
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

		if not cover.is_empty():
			ground.append({"cover": cover, "forest": forest, "heights": built["heights"],
				"base_z": base_z, "level": int(c["level"]),
				"cx": int(c["cx"]), "cz": int(c["cz"])})

		# Габарит ПРИЕХАВШЕГО рельефа: угол чанка плюс его сторона. Радиус
		# последнего уровня для этого не годится — он объявляет, докуда чанки
		# МОГУТ храниться (8192 м), а не где они есть, и кадр по нему уходил в
		# поле, где нет ничего, кроме дымки.
		var side_m := rule.side_of(int(c["level"]))
		var cell := Rect2(Vector2(float(c["cx"]) * side_m, float(c["cz"]) * side_m),
			Vector2(side_m, side_m))
		terrain_box = cell if got == 1 else terrain_box.merge(cell)

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
	stats["forest_200"] = forest_got
	await _draw_buildings()
	_draw_vegetation(ground, rule)
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
	stats["terrain_box"] = terrain_box


## _place_camera — навести камеру роли на габарит, названный `frame`.
##
## Здесь сходятся две вещи, и они РАЗНОЙ природы. Углы и проекция приходят от
## РОЛИ (свойство взгляда). Точка и масштаб считаются из ГАБАРИТОВ ДАННЫХ —
## ни одного числа о мире тут нет и быть не может: снесённый клиент держал фокус
## `Vector2(240.0, 0.0)`, и это одна из причин, по которым его снесли.
func _place_camera(bbox: Rect2, elements: Array[TrackGeom.Element]) -> void:
	# МАШИНИСТ смотрит ГОРИЗОНТАЛЬНО, и потому он единственный, на чьём кадре
	# видно небо, дымку и силуэт леса. Остальные роли смотрят сверху, и по ним
	# нельзя судить, похоже ли это на железную дорогу.
	#
	# Точка берётся из ДАННЫХ: начало самого длинного элемента, отметка его оси
	# плюс высота глаза. Высота глаза — решение художника (у человека она около
	# 1.7 м), и потому названа константой.
	if role_camera.is_empty():
		var longest: TrackGeom.Element = null
		for el in elements:
			if longest == null or el.length_m > longest.length_m:
				longest = el
		if longest == null:
			_fail("роль «машинист»: ни одного элемента не приехало — вставать некуда")
			return
		var a0 := longest.pose_at(0.0)
		var a1 := longest.pose_at(minf(EYE_LOOK_AHEAD_M, longest.length_m))
		var eye := TerrainMesh.to_godot(a0.x, a0.y, a0.z + EYE_HEIGHT_M)
		var target := TerrainMesh.to_godot(a1.x, a1.y, a1.z + EYE_HEIGHT_M)
		camera.global_position = eye
		camera.look_at(target, Vector3.UP)
		stats["view"] = "с оси %s, глаз на %.2f м над головкой рельса" % [longest.id, EYE_HEIGHT_M]
		return

	var view_box := _frame_bbox(bbox, elements)
	var cx := view_box.position.x + view_box.size.x * 0.5
	var cy := view_box.position.y + view_box.size.y * 0.5
	var mid_z := 0.0
	if stats.has("z_min") and float(stats["z_min"]) < INF:
		mid_z = (float(stats["z_min"]) + float(stats["z_max"])) * 0.5
	var centre := TerrainMesh.to_godot(cx, cy, mid_z)
	# Ширина кадра — по БОЛЬШЕЙ стороне габарита: по диагонали станция ушла бы в
	# половину экрана, по меньшей — не влезла бы вдоль.
	var span := maxf(maxf(view_box.size.x, view_box.size.y), 1.0)
	var factor := float(role_camera.get("frame_factor", 1.15))
	camera.configure(centre, span * factor,
		float(role_camera.get("azimuth", 205.0)),
		float(role_camera.get("elevation", 45.0)),
		bool(role_camera.get("ortho", true)))
	stats["view"] = "%s, габарит «%s»: центр (%.1f, %.1f), кадр %.0f м, %s" % [
		role_name, frame, cx, cy, span * factor, camera.projection_name()]


## _frame_bbox — габарит, названный `frame`. Из данных, а не из координат.
func _frame_bbox(bbox: Rect2, elements: Array[TrackGeom.Element]) -> Rect2:
	match frame:
		"throat":
			# Горловина — не координата и не константа: это габарит ТЕХ
			# элементов, у которых сервер прислал role.turnout. Не прислал ни у
			# одного — габарита нет, и это сказано вслух, а не подменено сетью.
			var throat := _role_bbox(elements)
			if throat.size == Vector2.ZERO:
				_fail("габарит «горловина»: ни у одного элемента нет role.turnout — наводиться не на что")
				return bbox
			return throat
		"terrain":
			# Весь ПРИЕХАВШИЙ рельеф — габарит полученных чанков, а не радиус
			# последнего уровня: тот говорит, докуда чанки МОГУТ храниться.
			var tb: Rect2 = stats.get("terrain_box", Rect2())
			if tb.size == Vector2.ZERO:
				return bbox
			return tb
		"network":
			return bbox
	_fail("габарит «%s» неизвестен — знаю network, throat, terrain" % frame)
	return bbox


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
	# Роль первой строкой: игрок обязан видеть, кем он сейчас смотрит, не открывая
	# меню. Это то же место, где спайк держал имя роли и подсказку управления.
	if role_name != "":
		l.append("[b]%s[/b]   %s   регион %s" % [role_name, server_url, region])
	else:
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
		l.append("  растительность: деревьев %d (хвойных %d, лиственных %d), кустов %d, пучков травы %d" % [
			stats.get("trees_drawn", 0), stats.get("trees_conifer", 0), stats.get("trees_broadleaf", 0),
			stats.get("bushes_drawn", 0), stats.get("grass_drawn", 0)])
		l.append("  лес: битовых карт получено %d (только уровень 0)" % stats.get("forest_200", 0))
		l.append("  построек %d (место, габарит и ОТМЕТКА — с сервера; форма крыши и цвет — здесь)" % stats.get("buildings_drawn", 0))
		l.append("  рек %d, лент %d (ось, урез и ширина — с сервера; цвет и блеск — здесь)" % [
			stats.get("rivers_drawn", 0), stats.get("river_quads", 0)])
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
	l.append("[i]класс поверхности прислан; ЦВЕТ класса — законно клиентский, как меш ели:")
	l.append("сервер говорит что и где, как это выглядит — дело рендерера.[/i]")
	l.append("[i]где растёт лес и какой породы — прислано; МЕШ ели, куста и пучка,")
	l.append("плотность рассева, небо, свет и дымка — клиентские, как и цвет класса.[/i]")
	l.append("[i]не рисуется, потому что сервер не отдаёт вовсе: локомотив,")
	l.append("решётка стрелки (переводные брусья).[/i]")
	l.append("[i]русло врезано в ВЫСОТЫ: берег и долина приехали отсчётами чанка и")
	l.append("не стоили ни одного лишнего байта. Лентой рисуется только гладь.[/i]")
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
	# Подсказка управления приходит ОТ РОЛИ: у орбитальной камеры и свободной
	# разные жесты, и одна строка на обе врала бы одной из них.
	l.append("[i]%s[/i]" % (role_hints if role_hints != ""
		else "мышь (правая) — обзор, WASD — движение, Q/E — вниз/вверх, колесо — скорость"))
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


## _draw_vegetation — деревья, кусты и трава по ярусам уровня 0.
##
## # Что здесь чьё
##
## ГДЕ РАСТЁТ — сервер: класс покрова и битовая карта леса. Ни одна из трёх
## посадок не берёт собственного шума — иначе клиент придумал бы, где растёт
## трава, а это факт о месте.
##
## КАК ВЫГЛЯДИТ — клиент: меши, пропорции, число сегментов, плотность рассева,
## порог сомкнутости, с которого трава появляется. Второй рендерер вправе взять
## другие, и мир не изменится.
##
## Экземплярами (MultiMesh), а не узлами: у травинки нет тождества, выделять ей
## узел сцены нечем и незачем.
func _draw_vegetation(ground: Array[Dictionary], rule: ChunkRule) -> void:
	if ground.is_empty():
		return
	var node := Node3D.new()
	node.name = "Vegetation"
	world.add_child(node)

	var cells := rule.samples - 1

	var spruce: Array[Transform3D] = []
	var broad: Array[Transform3D] = []
	var bushes: Array[Transform3D] = []
	var grass: Array[Transform3D] = []

	for g_raw in ground:
		var g: Dictionary = g_raw
		var cover: PackedByteArray = g["cover"]
		var forest: PackedByteArray = g["forest"]
		var heights: PackedFloat32Array = g["heights"]
		var base_z: float = g["base_z"]
		var cx: int = g["cx"]
		var cz: int = g["cz"]
		var level: int = g["level"]
		# Сторона и шаг — СВОИ У КАЖДОГО УРОВНЯ. Брать их у нулевого значило бы
		# посадить дальний лес в шестнадцать раз плотнее и в шестнадцать раз
		# ближе к оси, чем говорит покров.
		var side := rule.side_of(level)
		var step := side / float(cells)
		var has_forest := forest.size() == cells * cells / 8

		# ЛЕС ЗА КОРИДОРОМ РАССЕВАЕТ КЛИЕНТ САМ, и это не отступление от правила,
		# а его прямое следствие (разбор раскладки леса, §«грубые уровни»).
		#
		# Тождество нужно дереву затем, что его РУБЯТ; рубят там, куда
		# дотянулись, — в коридоре, и полоса рубки совпадает с полосой уровня 0.
		# За её границей у дерева тождества нет, значит нет и повода возить его
		# битом: оно рассевается по покрову ровно так же, как трава, и по тому
		# же основанию — «сажать ПО ПОКРОВУ, а не по своему шуму».
		#
		# Цена названа: срубить такое дерево нельзя, и на границе коридора при
		# рубке появится шов. Он невидим, пока рубки нет.
		if has_forest:
			var res := Forest.trees(forest, cover, heights, base_z, rule.samples, cx, cz, side)
			for st_raw in (res["list"] as Array):
				var st: Forest.Stem = st_raw
				var t := Transform3D(Basis.IDENTITY.scaled(Vector3(st.height_m, st.height_m, st.height_m)),
					TerrainMesh.to_godot(st.x, st.y, st.z))
				if st.species == TerrainMesh.SURFACE_FOREST_BROAD:
					broad.append(t)
				else:
					spruce.append(t)

		# Трава и кусты — по ячейкам покрова. Порог сомкнутости и плотность
		# рассева клиентские: сервер сказал «здесь луг густоты 11», во сколько
		# пучков это развернуть — вопрос кадра, а не мира.
		var ox := float(cx) * side
		var oz := float(cz) * side
		for j in cells:
			for i in cells:
				var k := j * cells + i
				var packed := cover[k]
				var cls := packed >> 4
				var closure := packed & 0x0f
				if closure < GRASS_MIN_CLOSURE:
					continue
				if cls == TerrainMesh.SURFACE_SAND or cls == TerrainMesh.SURFACE_BARE_SOIL:
					continue
				# Ячейка со стволом травой не засевается: под елью её не видно, а
				# пучков она стоит столько же.
				if has_forest and (forest[k / 8] & (1 << (k % 8))) != 0:
					continue
				var z0 := base_z + float(heights[j * rule.samples + i]) * 0.01
				# Лесная ячейка БЕЗ битовой карты — дальний уровень. Дерево
				# ставится хешем той же функции: она уже часть контракта, и
				# заводить второй жребий значило бы иметь два ответа на вопрос
				# «где стоит дерево».
				if not has_forest and (cls == TerrainMesh.SURFACE_FOREST_CONIFER
						or cls == TerrainMesh.SURFACE_FOREST_BROAD):
					var fj := Forest.jitter(cx, cz, i + cells, j)
					if fj[2] < float(closure) / 15.0:
						var fh := Forest.TREE_H_MIN + float(fj[2]) * (Forest.TREE_H_MAX - Forest.TREE_H_MIN)
						var ft := Transform3D(
							Basis.IDENTITY.scaled(Vector3(fh, fh, fh)),
							TerrainMesh.to_godot(ox + (float(i) + fj[0]) * step,
								oz + (float(j) + fj[1]) * step, z0))
						if cls == TerrainMesh.SURFACE_FOREST_BROAD:
							ft = ft.scaled_local(Vector3(Forest.BROAD_SCALE, Forest.BROAD_SCALE, Forest.BROAD_SCALE))
							broad.append(ft)
						else:
							spruce.append(ft)
					continue
				# Трава — только вблизи: на дальних уровнях ячейка 64 м, и пучок
				# в ней означал бы одну травинку на гектар. Порог по уровню, а не
				# по расстоянию до камеры: рассев считается один раз при загрузке.
				if level > 0:
					continue
				var z := z0
				var per_cell := int(round(float(closure) / 15.0 * float(GRASS_MAX_PER_CELL)))
				for n in per_cell:
					var jt := Forest.jitter(cx, cz, i + n * 97, j + n * 31)
					var gx := ox + (float(i) + jt[0]) * step
					var gy := oz + (float(j) + jt[1]) * step
					var h := GRASS_H_MIN + float(jt[2]) * (GRASS_H_MAX - GRASS_H_MIN)
					grass.append(Transform3D(
						Basis.IDENTITY.rotated(Vector3.UP, float(jt[2]) * TAU).scaled(Vector3(h, h, h)),
						TerrainMesh.to_godot(gx, gy, z)))
				# Куст — редко и по той же сомкнутости.
				var bj := Forest.jitter(cx, cz, i + 7919, j + 6271)
				if bj[2] < BUSH_CHANCE * float(closure) / 15.0:
					var bh := BUSH_H_MIN + float(bj[0]) * (BUSH_H_MAX - BUSH_H_MIN)
					bushes.append(Transform3D(
						Basis.IDENTITY.rotated(Vector3.UP, float(bj[1]) * TAU).scaled(Vector3(bh * 1.4, bh, bh * 1.4)),
						TerrainMesh.to_godot(ox + (float(i) + bj[0]) * step, oz + (float(j) + bj[1]) * step, z)))

	var mat := Vegetation.material()
	_multimesh(node, "Spruce", Vegetation.spruce_mesh(), spruce, mat)
	_multimesh(node, "Broadleaf", Vegetation.broadleaf_mesh(), broad, mat)
	_multimesh(node, "Bushes", Vegetation.bush_mesh(false), bushes, mat)
	_multimesh(node, "Grass", Vegetation.grass_mesh(), grass, mat)

	stats["trees_drawn"] = spruce.size() + broad.size()
	stats["trees_conifer"] = spruce.size()
	stats["trees_broadleaf"] = broad.size()
	stats["bushes_drawn"] = bushes.size()
	stats["grass_drawn"] = grass.size()


func _multimesh(parent: Node3D, name_: String, mesh: ArrayMesh, xforms: Array[Transform3D],
		mat: StandardMaterial3D) -> void:
	if xforms.is_empty() or mesh == null:
		return
	var mm := MultiMesh.new()
	mm.transform_format = MultiMesh.TRANSFORM_3D
	mm.mesh = mesh
	mm.instance_count = xforms.size()
	for k in xforms.size():
		mm.set_instance_transform(k, xforms[k])
	var mi := MultiMeshInstance3D.new()
	mi.name = name_
	mi.multimesh = mm
	mi.material_override = mat
	parent.add_child(mi)


## _draw_buildings — постройки из третьего ресурса региона.
##
## # Что прислано и что решено здесь
##
## Место, поворот, габарит и ОТМЕТКА — сервер. Отметку он считает сам и везёт
## явно: направление авторитета тут обратное пути — путь диктует отметку земле,
## а дом её принимает. Считать её на клиенте нельзя, и это замерено: расхождение
## отметки между уровнем 0 и уровнем 4 в среднем 0.39 м и до 2.30 м, то есть дом
## висел бы или тонул в зависимости от того, какой чанк успел загрузиться.
##
## Форма крыши, цвет стен и напуск карниза — РЕНДЕРЕР. По той же границе, по
## которой клиент строит ель: сервер сказал «здесь дом 18 × 21 × 14», из каких
## треугольников он сложен — не факт о мире.
func _draw_buildings() -> void:
	# Ревизия берётся ИЗ МАНИФЕСТА, а не из stats: в stats она лежит значением
	# JSON (float), и int() от null дал бы ноль, то есть запрос к
	# /revisions/0/objects и честный 404 не по той причине.
	var rev := int(float(stats.get("revision", -1.0)))
	if rev <= 0:
		_fail("объекты региона: манифест не назвал ревизию — спрашивать нечего")
		return
	var r: Dictionary = await net.fetch_json("/regions/%s/revisions/%d/objects" % [region, rev])
	if not r["ok"]:
		_fail("объекты региона: %s" % r["error"])
		return
	var body: Dictionary = r["data"]
	_draw_water(body.get("rivers", []) as Array)
	var list: Array = body.get("buildings", []) as Array
	if list.is_empty():
		stats["buildings_drawn"] = 0
		return

	var node := Node3D.new()
	node.name = "Buildings"
	world.add_child(node)

	var verts := PackedVector3Array()
	var norms := PackedVector3Array()
	var cols := PackedColorArray()
	var idx := PackedInt32Array()
	var drawn := 0
	for b_raw in list:
		var b: Dictionary = b_raw as Dictionary
		var w := float(b.get("width", 0.0)) * 0.5
		var d := float(b.get("depth", 0.0)) * 0.5
		var h := float(b.get("height", 0.0))
		if w <= 0.0 or d <= 0.0 or h <= 0.0:
			continue
		var x := float(b.get("x", 0.0))
		var y := float(b.get("y", 0.0))
		var z := float(b.get("z", 0.0))
		var a := float(b.get("heading", 0.0))
		var fwd := Vector2(cos(a), sin(a))
		# Цвет стены — из хеша ИМЕНИ дома, а не из порядка в списке: порядок
		# может измениться, а дом останется тем же, и перекрашивать его при
		# пересортировке было бы враньём про постоянство места.
		var col: Color = HOUSE_COLORS[abs(hash(String(b.get("id", "")))) % HOUSE_COLORS.size()]
		# Заглубление: дом стоит НА земле, но её отметка взята в одной точке, а
		# пятно у дома двадцать метров. Опустить коробку на BUILDING_SINK — то
		# же лечение, что у спайка, и названо оно там же: дешевле, чем сажать
		# каждый угол отдельно, и незаметно, пока участок не на склоне.
		_house(verts, norms, cols, idx, x, y, z - BUILDING_SINK, fwd, w, d, h, col)
		drawn += 1

	if idx.is_empty():
		return
	var arrays := []
	arrays.resize(Mesh.ARRAY_MAX)
	arrays[Mesh.ARRAY_VERTEX] = verts
	arrays[Mesh.ARRAY_NORMAL] = norms
	arrays[Mesh.ARRAY_COLOR] = cols
	arrays[Mesh.ARRAY_INDEX] = idx
	var mesh := ArrayMesh.new()
	mesh.add_surface_from_arrays(Mesh.PRIMITIVE_TRIANGLES, arrays)
	var mi := MeshInstance3D.new()
	mi.name = "Houses"
	mi.mesh = mesh
	var mat := StandardMaterial3D.new()
	mat.vertex_color_use_as_albedo = true
	mat.roughness = 0.9
	mi.material_override = mat
	node.add_child(mi)
	stats["buildings_drawn"] = drawn


## _draw_water — водная гладь лентой по оси русла.
##
## # Что прислано и что решено здесь
##
## ОСЬ, ОТМЕТКА УРЕЗА И ДОКУДА ДОХОДИТ ВОДА — сервер, до каждой точки. Клиент не
## считает ни ширины, ни уровня: берег вырезан в высотах, и где именно вода
## встречает землю, знает только тот, кто эти высоты породил.
##
## ЦВЕТ, БЛЕСК И ПРОЗРАЧНОСТЬ — рендерер, по той же границе, по которой он
## выбирает цвет луга и меш ели. Числа взяты у снесённого спайка, чтобы не
## подбирать глазом второй раз уже подобранное.
##
## Лента чуть НИЖЕ присланного уреза: гладь и берег сходятся на одной отметке по
## построению, и при точном совпадении они дерутся за z-буфер — вдоль всего
## берега идёт мерцающая кромка. Опускание на сантиметр дешевле, чем отключение
## проверки глубины, и в отличие от него не пускает воду поверх моста.
const WATER_SINK := 0.01
const C_WATER := Color(0.24, 0.44, 0.56)


func _draw_water(rivers: Array) -> void:
	if rivers.is_empty():
		stats["rivers_drawn"] = 0
		return
	var verts := PackedVector3Array()
	var norms := PackedVector3Array()
	var idx := PackedInt32Array()
	var drawn := 0
	var points := 0
	for r_raw in rivers:
		var r: Dictionary = r_raw as Dictionary
		var axis: Array = r.get("axis", []) as Array
		if axis.size() < 2:
			# Река из одной точки — не река. Сервер такую не отдаёт (валидатор
			# отказывает), и молча дорисовывать её здесь значило бы прятать
			# расхождение версий контракта.
			_fail("река %s: точек оси %d — ленту не построить" % [String(r.get("id", "")), axis.size()])
			continue
		for k in axis.size() - 1:
			var a: Dictionary = axis[k] as Dictionary
			var b: Dictionary = axis[k + 1] as Dictionary
			var ax := float(a.get("x", 0.0))
			var ay := float(a.get("y", 0.0))
			var bx := float(b.get("x", 0.0))
			var by := float(b.get("y", 0.0))
			var t := Vector2(bx - ax, by - ay)
			if t.length() < 1e-6:
				continue
			t = t.normalized()
			# Левая нормаль к ходу оси — та же, по которой сервер мерил урез.
			var n := Vector2(-t.y, t.x)
			var az := float(a.get("z", 0.0)) - WATER_SINK
			var bz := float(b.get("z", 0.0)) - WATER_SINK
			var al := float(a.get("half_left", 0.0))
			var ar := float(a.get("half_right", 0.0))
			var bl := float(b.get("half_left", 0.0))
			var br := float(b.get("half_right", 0.0))
			var base := verts.size()
			verts.append(TerrainMesh.to_godot(ax + n.x * al, ay + n.y * al, az))
			verts.append(TerrainMesh.to_godot(bx + n.x * bl, by + n.y * bl, bz))
			verts.append(TerrainMesh.to_godot(bx - n.x * br, by - n.y * br, bz))
			verts.append(TerrainMesh.to_godot(ax - n.x * ar, ay - n.y * ar, az))
			for _i in 4:
				norms.append(Vector3.UP)
			idx.append_array([base, base + 1, base + 2, base, base + 2, base + 3])
			points += 1
		drawn += 1

	if idx.is_empty():
		stats["rivers_drawn"] = 0
		return
	var arrays := []
	arrays.resize(Mesh.ARRAY_MAX)
	arrays[Mesh.ARRAY_VERTEX] = verts
	arrays[Mesh.ARRAY_NORMAL] = norms
	arrays[Mesh.ARRAY_INDEX] = idx
	var mesh := ArrayMesh.new()
	mesh.add_surface_from_arrays(Mesh.PRIMITIVE_TRIANGLES, arrays)
	var mi := MeshInstance3D.new()
	mi.name = "Water"
	mi.mesh = mesh
	var mat := StandardMaterial3D.new()
	# albedo_color, а не цвет вершины: движок переводит его из sRGB сам, и
	# правило «в вершину класть линейный» сюда не относится (bd recall
	# godot-vertex-color-linear).
	mat.albedo_color = C_WATER
	mat.roughness = 0.06
	mat.metallic = 0.25
	mi.material_override = mat
	world.add_child(mi)
	stats["rivers_drawn"] = drawn
	stats["river_quads"] = points


## _house — коробка стен плюс плита крыши с напуском.
##
## Цвет дома несёт КРОВЛЯ, а стены его оттеняют: с высоты строителя и ДСП стен
## почти не видно, и посёлок, у которого цветные стены под серой крышей,
## читается двенадцатью одинаковыми плитами. Разбор — у объявления C_ROOF.
func _house(v: PackedVector3Array, n: PackedVector3Array, c: PackedColorArray, idx: PackedInt32Array,
		x: float, y: float, z: float, fwd: Vector2, hw: float, hd: float, h: float, wall: Color) -> void:
	TrackView.box_into(v, n, c, idx, x, y, fwd, hd, hw, z, z + h, wall.darkened(WALL_SHADE))
	TrackView.box_into(v, n, c, idx, x, y, fwd, hd + ROOF_OVERHANG, hw + ROOF_OVERHANG,
		z + h - ROOF_SEAT, z + h - ROOF_SEAT + ROOF_THICKNESS, wall.lerp(C_ROOF, ROOF_TINT))
