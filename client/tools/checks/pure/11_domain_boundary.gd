## ГРАНИЦА ДОМЕНА — чистая проверка по ИСХОДНИКАМ: рендер не держит чисел о мире.
##
## Брат 10_transport_boundary. Тот сторожит «рендер не знает протокола», этот —
## «рендер не знает мира». Обе границы одинаково невидимы в кадре: клиент,
## заведший своё число про размер дерева, рисует правдоподобную картинку и
## проходит все прочие проверки до единой.
##
## # Чем это вызвано: три случая за неделю, и все найдены глазами
##
## • НАКАТ (ClearAhead-ax7m.10): ширина полосы жила в клиенте долей головки,
##   расходясь с серверной в полтора раза. Нашлось при разборе стрелки.
## • ПОРОГ ОСЫПИ (ClearAhead-s6v п. 6): решение требовало порог в контракте;
##   клиент держал его полтора месяца в ТРЁХ копиях и рядом хранил комментарий,
##   объясняющий, почему порогу место у него.
## • ПЕРЕЧЕНЬ yfu3: составлен вручную 2026-08-15, к 2026-08-18 устарел в двух
##   строках — работа эпика сняла две позиции, и заметить это было нечем.
##
## Цена каждого разбора — час, и начинается он с того, что кто-то посмотрел.
##
## # Почему ПЕРЕПИСЬ, а не умная проверка
##
## Отличить «размер детали мира» от «размера рисунка» текстом нельзя: 0.050 —
## это и щебёнка балласта, и толщина линии, и доля яркости. Всякая попытка
## судить по имени или по единице даст ложные тревоги на азимуте камеры 205° и
## пропустит долю, у которой в имени нет метров, — ровно та ошибка, от которой в
## 10_transport_boundary закрытый список кодов, а не «любое трёхзначное число».
##
## Поэтому проверка не судит. Она ПИННИТ ИНВЕНТАРЬ: всякое числовое const в
## файлах, где мир есть, обязано стоять в одном из двух списков ниже. Новое
## число, не попавшее ни в один, — отказ. Значит классифицировать его придётся
## тому, кто его завёл, и в тот день, когда он его завёл, а не через полтора
## месяца на разборе.
##
## СПИСОК ДОЛГА ОБЯЗАН УБЫВАТЬ, и это тоже проверяется: имя, исчезнувшее из
## клиента, но оставшееся в переписи, — отказ. Иначе перепись повторила бы
## судьбу перечня yfu3 и начала бы врать в приятную сторону.
##
## # Разделение — суждение, а не истина
##
## Кто в каком списке — решение 2026-08-18, и оно записано здесь ЗАТЕМ, ЧТОБЫ С
## НИМ МОЖНО БЫЛО СПОРИТЬ. Спор разрешается переносом строки из списка в список,
## а не отключением проверки: несогласие с классификацией и есть та самая мысль,
## ради которой перепись существует.
extends "res://tools/check_suite.gd"

## ФАЙЛЫ ПОКАЗА ЦЕЛИКОМ: в них мира нет ни в одном числе.
##
## Экран (панели, оболочка, шрифты), камера и мышь, транспорт и его коды,
## процедурные текстуры земли, кинематика показа (объявлена спекой t5h),
## очередь перестроек, разбор моделей, правила из манифеста.
##
## РИСК НАЗВАН: число о мире, заведённое в одном из этих файлов, проверка
## пропустит. Он принят сознательно — перепись всех 184 числовых констант
## клиента заставляла бы объяснять азимут камеры и отступ панели, то есть
## утонула бы в шуме, а тонущую проверку перестают читать. Файлы отобраны по
## тому, что в них НЕТ мира по самому их назначению; появится — это само по себе
## повод для разбора, а не для строки в списке.
const SHOW_ONLY_FILES := [
	"app.gd", "asset_cache.gd", "cab.gd", "cab_panel.gd", "chunk_rule.gd",
	"display_motion.gd", "ground_look.gd", "ground_rule.gd", "live_channel.gd",
	"model_build.gd", "net_channel.gd", "net_client.gd", "orbit_camera.gd",
	"port_debug.gd", "rebuild_queue.gd", "rolling_stock.gd", "shell_ui.gd",
	"switch_stand.gd", "track_geom.gd", "track_span.gd", "turnout_panel.gd",
	"version_rebuild.gd", "version_set.gd", "world_api.gd",
]

## ЧТО ПОКАЗУ ПРИНАДЛЕЖИТ ПО ПРАВУ — в файлах, где рядом живёт мир.
##
## Право это не общее, а по границе ClearAhead-sjq: клиенту принадлежат меш и
## LOD, тесселяция, рассев мелочи, цвет и материал, дальность взгляда, бюджет
## кадра и всё, что про глаз игрока, а не про место в мире.
const CLIENT_OWNS := [
	# Взгляд и режимы обзора: про глаз игрока, а не про мир.
	"MOUSE_SENS", "PITCH_LIMIT", "EYE_FOV", "TP_DIST", "TP_HEIGHT", "BOB_AMP",
	"ORBIT_FRAME_M", "ORBIT_AZIMUTH_DEG", "ORBIT_ELEV_DEG",
	"MODE_ORBIT", "MODE_EYE", "MODE_THIRD",
	# Приёмы показа, а не правила мира: доля намеченного пути, ниже которой
	# считаем «упёрся», и луч постановки на твердь (высота пуска и глубина
	# поиска). Ни одно из трёх не наблюдаемо в мире — это устройство кадра.
	"STEP_PROGRESS", "SETTLE_LIFT", "SETTLE_DROP",
	# Тесселяция: во сколько отрезков разбить дугу. Прямо названа клиентской в
	# границе sjq — «путь: примитивы и рецепты с сервера, тесселяция по зуму».
	"TESS_MAX_SEG_M", "TESS_MAX_ANG_RAD", "BLADE_STEP_M", "FROG_STEP_M",
	"BREAK_EPS_M", "STATION_EPS", "TOP_EPS",
	# Число сегментов и колец: подробность меша, то есть LOD.
	"CONE_SEGMENTS", "CROWN_SEGMENTS", "CROWN_RINGS", "TRUNK_SEGMENTS",
	# Рассев мелочи: по границе sjq сервер называет биом и плотность, а клиент
	# рассыпает как хочет. Шаг узора рассева — «как хочет».
	"GRASS_BASE", "TUFT_M", "BUSH_CLUMP_M", "TUFT_LOTS", "GRASS_TEX",
	# Дальность показа и бюджет кадра: сколько игрок видит и сколько кадр тянет.
	# Потолок хранения объявляет сервер, дальность взгляда — настройка клиента.
	"GRASS_FAR", "GRASS_CHUNK", "GRASS_BUDGET_US", "GRASS_WARM", "GRASS_REPLAN",
	"REBUILD_BUDGET_US", "FPS_PROBE_FRAMES", "CHANNEL_HELLO_WAIT_S",
	# Порядок слоёв в кадре: чем перекрывать что при совпадении отметок.
	"PRIO_BALLAST", "PRIO_PLATFORM", "PRIO_SLEEPER", "PRIO_RAIL", "PRIO_LINE",
	"PRIO_FROG",
	# УЗОР ТЕКСТУРЫ, А НЕ РАЗМЕР ДЕТАЛИ, и различие здесь настоящее: призма
	# балласта приезжает с сервера числами (полуширина, глубина, заложение
	# откоса), а щебёнка и волокно шпалы — рисунок на ней. Метры у них есть, но
	# это метры натуры, по которым выбран масштаб узора, а не размер вещи,
	# которую можно занять или переехать.
	"BALLAST_STONE", "BALLAST_TILE", "BALLAST_CELL_PX",
	"SLEEPER_GRAIN", "SLEEPER_RUN",
	# Цвет и подмешки цвета: решение художника, и второй клиент вправе взять
	# другие. Граница разобрана у правила земли (GroundRule): цвет осыпи —
	# показу, порог осыпи — серверу.
	"SCARP_TINT_MAX", "BARE_TINT", "SAND_TINT", "FOREST_TINT",
	"ROOF_TINT", "WALL_SHADE", "FOG_TARGET_AT_REACH",
	# Зеркало контракта чанков: коды классов покрова клиент ЧИТАЕТ, а не
	# назначает. Та же природа, что у кодов HTTP в транспортном слое — знание
	# договора там, где договор и разбирается.
	"SURFACE_MEADOW", "SURFACE_FOREST_CONIFER", "SURFACE_FOREST_BROAD",
	"SURFACE_SAND", "SURFACE_BARE_SOIL",
]

## ДОЛГ: числа о мире, которые ещё в клиенте. Имя → бида, в которой уедет.
##
## Это и есть отчёт «сколько осталось до тупого рендера». Пустой словарь значит,
## что вычистили.
const WORLD_DEBT := {
	# Тело человека: рост, глаз, плечи, шаг. Уедет паспортом, как машина.
	"BODY_H": "ClearAhead-yfu3", "EYE_H": "ClearAhead-yfu3",
	"HIP_H": "ClearAhead-yfu3", "SHOULDER_H": "ClearAhead-yfu3",
	"NECK_H": "ClearAhead-yfu3", "HEAD_R": "ClearAhead-yfu3",
	"BODY_HALF_W": "ClearAhead-yfu3", "BODY_HALF_D": "ClearAhead-yfu3",
	"CAPSULE_R": "ClearAhead-yfu3", "STRIDE": "ClearAhead-yfu3",
	# Как человек ходит и куда дотягивается руками. Не тело, а поведение и
	# правило игры: по дальности рук сервер обязан отказывать в команде.
	"WALK_SPEED": "ClearAhead-9r6g", "RUN_SPEED": "ClearAhead-9r6g",
	"ACCEL": "ClearAhead-9r6g", "JUMP_V": "ClearAhead-9r6g",
	"AIR_CONTROL": "ClearAhead-9r6g", "STEP_UP": "ClearAhead-9r6g",
	"BOARD_REACH_M": "ClearAhead-9r6g", "STAND_REACH_M": "ClearAhead-9r6g",
	"ALIGHT_CLEARANCE_M": "ClearAhead-9r6g",
	# Где человек появляется — факт о карте.
	"START_U_M": "ClearAhead-9r6g", "START_LAT_M": "ClearAhead-9r6g",
	"START_YAW_DEG": "ClearAhead-9r6g", "START_PITCH_DEG": "ClearAhead-9r6g",
	# Рост дерева и куста: не форма кроны, а размер вещи в мире.
	"TREE_H_MIN": "ClearAhead-yfu3", "TREE_H_MAX": "ClearAhead-yfu3",
	"BROAD_SCALE": "ClearAhead-yfu3", "TREE_MESH_H": "ClearAhead-yfu3",
	"BUSH_MESH_H": "ClearAhead-yfu3", "BUSH_LOW_MESH_H": "ClearAhead-yfu3",
	"SPRUCE_SPREAD_NARROW": "ClearAhead-yfu3",
	"SPRUCE_SPREAD_WIDE": "ClearAhead-yfu3",
	"BUSH_FIELD": "ClearAhead-yfu3", "BUSH_H_MIN": "ClearAhead-yfu3",
	"BUSH_H_MAX": "ClearAhead-yfu3", "BUSH_LOW_SHARE": "ClearAhead-yfu3",
	# Размер пучка травы: у пучка есть место в мире, в отличие от узора рассева.
	"GRASS_QUAD_W": "ClearAhead-yfu3", "GRASS_MESH_H": "ClearAhead-yfu3",
	# Река: насколько лента утоплена относительно уреза.
	"WATER_SINK": "ClearAhead-yfu3",
	# Клетка маски, которой трава вычитается под балластом. Правило «где трава не
	# растёт из-за пути» — того же класса, что порог сомкнутости, и чинится
	# вместе с поверхностью коридора.
	"BALLAST_MASK_CELL": "ClearAhead-ax7m.4",
}


func run() -> void:
	var decl := RegEx.new()
	# Числовое const: имя заглавными и голое число значением. Выражения
	# (маски, произведения, preload) не считаются: в них число само по себе
	# редко величина мира, а разбор их текстом дал бы ложные тревоги.
	var err := decl.compile("^const\\s+([A-Z][A-Z0-9_]*)\\s*:?=\\s*(-?[0-9]+(?:\\.[0-9]+)?(?:[eE]-?[0-9]+)?)\\s*(?:#.*)?$")
	_ok("выражение переписи собрано", err == OK)
	if err != OK:
		return

	var dir := DirAccess.open("res://scripts")
	if dir == null:
		_ok("исходники клиента читаются", false, "res://scripts не открылся")
		return

	var unlisted: Array[String] = []
	var seen: Dictionary = {}
	var scanned := 0
	var counted := 0

	for file_name in dir.get_files():
		if not file_name.ends_with(".gd") or file_name in SHOW_ONLY_FILES:
			continue
		scanned += 1
		var text := FileAccess.get_file_as_string("res://scripts/" + file_name)
		var line_no := 0
		for line in text.split("\n"):
			line_no += 1
			var m := decl.search(line.strip_edges())
			if m == null:
				continue
			counted += 1
			var name := m.get_string(1)
			seen[name] = true
			if name in CLIENT_OWNS or WORLD_DEBT.has(name):
				continue
			unlisted.append("%s:%d %s = %s" % [file_name, line_no, name, m.get_string(2)])

	_ok("файлы с миром просмотрены", scanned > 0, "файлов %d, числовых const %d" % [scanned, counted])

	# ГЛАВНЫЙ ПУНКТ: новое число обязано быть классифицировано тем, кто его завёл.
	_ok("нет чисел вне переписи", unlisted.is_empty(),
		"не объявлено: " + ", ".join(unlisted) if not unlisted.is_empty()
		else "все %d числа разнесены по спискам" % counted)

	# ПЕРЕПИСЬ НЕ ВРЁТ В ПРИЯТНУЮ СТОРОНУ: имя, ушедшее из клиента, обязано уйти
	# и отсюда. Иначе список долга останется прежним после того, как долг отдан,
	# — ровно то, что случилось с перечнем yfu3 за три дня.
	var stale: Array[String] = []
	for name in WORLD_DEBT.keys():
		if not seen.has(name):
			stale.append("%s (%s)" % [name, WORLD_DEBT[name]])
	for name in CLIENT_OWNS:
		if not seen.has(name):
			stale.append("%s (за показом)" % name)
	_ok("в переписи нет исчезнувших имён", stale.is_empty(),
		"вычеркнуть: " + ", ".join(stale) if not stale.is_empty() else "")

	# ОТЧЁТ ЧИСЛОМ, а не только зелёной галочкой: сколько осталось до тупого
	# рендера, видно в каждом прогоне, и видно, куда каждое из чисел уедет.
	var by_bead: Dictionary = {}
	for name in WORLD_DEBT.keys():
		var bead: String = WORLD_DEBT[name]
		by_bead[bead] = int(by_bead.get(bead, 0)) + 1
	var parts: Array[String] = []
	for bead in by_bead.keys():
		parts.append("%s: %d" % [bead, by_bead[bead]])
	parts.sort()
	_ok("долг клиента перед сервером посчитан", true,
		"чисел о мире %d — %s" % [WORLD_DEBT.size(), ", ".join(parts)])
