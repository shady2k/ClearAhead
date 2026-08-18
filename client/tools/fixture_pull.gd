## fixture_pull.gd — СНЯТЬ ФИКСТУРУ СЕТИ с живого сервера. Одна команда:
##
##   make client-fixtures SERVER_URL=http://127.0.0.1:8080 REGION=ST_A
##
## Зачем инструмент, а не curl с jq в Makefile: обе программы у разработчика
## могут не стоять, а godot стоит по определению — без него ни одна цель client*
## не работает. Побочно снимок берётся ТЕМ ЖЕ СЛОЕМ, которым ходит игра, то есть
## фикстура — буквально то, что видит клиент, а не то, что видит утилита.
##
## # С КАКОГО АДРЕСА (и почему это половина дефекта u09k)
##
## С ВЕРСИОННОГО — /matches/{m}/worlds/{v}/network, — то есть с того, который
## читает игра. До 2026-08-18 снимок брался с /regions/{r}/revisions/{n}/network,
## и это ДРУГОЙ мир: ревизионный адрес отдаёт свежую геометрию прямо из карты в
## памяти, версионный — замороженную публикацию из базы. Пока публикация не
## зависела от кода геометрии, они расходились молча, и получалось худшее из
## возможного: make client-check зеленел на снимке свежего мира, а игрок смотрел
## на старый. Три коммита подряд прошли так, и один из них две ревизии не рисовал
## упоров вовсе.
##
## Снимок обязан цитировать ТО, ЧТО ВИДИТ ИГРОК. Расхождение адресов лечится на
## сервере (worldgen.republishStaleNetwork публикует новую версию, как только
## лежащее тело разошлось с построенным), но сторожить его должен снимок: сервер
## чинит, а проверка обязана мерить починенное.
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
	# АДРЕС ТОТ ЖЕ, ЧТО ЧИТАЕТ ИГРА, — версионный, а не ревизионный. Разбор в
	# шапке; здесь только порядок: версию называет манифест, матч — живое
	# состояние, и без любого из двух снимок не снимается, а не берётся с
	# запасного адреса. Запасной адрес и был дефектом.
	var head: Dictionary = man.data.get("projection_head", {}) as Dictionary
	var version := int(head.get("world_version", 0))
	if version <= 0:
		printerr("фикстура не снята: манифест не назвал projection_head.world_version")
		quit(1)
		return
	var live_res := await api.live(region)
	if live_res.failed():
		printerr("фикстура не снята: живое состояние не получено: %s" % live_res.reason)
		quit(1)
		return
	var match_id := String(live_res.data.get("match", ""))
	if match_id == "":
		printerr("фикстура не снята: живое состояние не назвало матч")
		quit(1)
		return
	var net_res := await api.world_network(match_id, version)
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
	print("снято: %s (версия мира %d, ревизия карты %d, матч %s, %s)"
		% [path, version, revision, match_id, server])
	quit(0)
