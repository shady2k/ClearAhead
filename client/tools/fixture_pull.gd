## fixture_pull.gd — СНЯТЬ ФИКСТУРУ СЕТИ с живого сервера. Одна команда:
##
##   make client-fixtures SERVER_URL=http://127.0.0.1:8080 REGION=ST_A
##
## Зачем инструмент, а не curl с jq в Makefile: обе программы у разработчика
## могут не стоять, а godot стоит по определению — без него ни одна цель client*
## не работает. Побочно снимок берётся ТЕМ ЖЕ СЛОЕМ, которым ходит игра, то есть
## фикстура — буквально то, что видит клиент, а не то, что видит утилита.
##
## Разница между «что прислал сервер» и «что увидел клиент» — не риторическая:
## сверка снимка с телом ответа (2026-08-12) даёт три значения из нескольких сот,
## разошедшихся на последний бит мантиссы (−9.532189389439237 против
## …235). Это разбор JSON движком, и он же работает в живом клиенте — значит
## фикстура права, а точнее была бы неправа.
##
## Раскладка по строкам (отступ в два пробела) обязательна: diff обновлённой
## фикстуры должен читаться глазами, иначе обновление проходит не глядя, и
## снимок перестаёт быть цитатой ответа — становится просто байтами в дереве.
##
## ЧТО СНИМКОМ НЕ БЕРЁТСЯ и почему: правило подробности и тело чанка. Первое —
## шесть чисел, и они стоят кодом в check_context.gd (там же разбор, почему);
## второе строится рампой у той проверки, которой нужно. Снимается только то,
## чего кодом честно не написать, — авторская сеть.
extends SceneTree

const DIR := "res://tools/fixtures"

var _started := false


func _process(_delta: float) -> bool:
	if not _started:
		_started = true
		_run()
	return false


func _run() -> void:
	var server := "http://127.0.0.1:8080"
	var region := "ST_A"
	for a in OS.get_cmdline_user_args():
		if a.begins_with("--server="):
			server = a.substr(9).rstrip("/")
		elif a.begins_with("--region="):
			region = a.substr(9)

	var api := WorldApi.new()
	api.base_url = server
	root.add_child(api)

	var man := await api.manifest(region)
	if man.failed():
		printerr("фикстура не снята: %s" % man.reason)
		quit(1)
		return
	var revision := int(man.data.get("revision", -1))
	var net_res := await api.network(region, revision)
	if net_res.failed():
		printerr("фикстура не снята: %s" % net_res.reason)
		quit(1)
		return

	DirAccess.make_dir_recursive_absolute(ProjectSettings.globalize_path(DIR))
	var path := "%s/network_%s.json" % [DIR, region]
	var f := FileAccess.open(path, FileAccess.WRITE)
	if f == null:
		printerr("фикстура не записана: %s" % path)
		quit(1)
		return
	# sort_keys = false: порядок полей остаётся серверным, иначе снимок перестаёт
	# быть цитатой ответа и становится его пересказом по алфавиту.
	# full_precision = true: без него JSON.stringify печатает 15 значащих цифр, и
	# заголовок элемента −0.11069999999999958 приезжает в файл как −0.1107.
	# Расхождение ничтожно (4e-16 на радиан), но фикстура обязана цитировать
	# точно: подправленное число невозможно отличить от правки карты.
	f.store_string(JSON.stringify(net_res.data, "  ", false, true) + "\n")
	f.close()
	print("снято: %s (ревизия %d, %s)" % [path, revision, server])
	quit(0)
