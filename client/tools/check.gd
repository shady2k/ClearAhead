## check.gd — БЕГУН проверок клиента. Того, что НЕ пиксели.
##
##   godot --headless --path client --script res://tools/check.gd -- --only=pure
##   godot --headless --path client --script res://tools/check.gd -- --only=live --server=…
##
## Зачем отдельно от рисунка: снимок экрана доказывает, что нарисовано верно, но
## не доказывает, что верно обработано НЕНАРИСОВАННОЕ. Счастливый путь на
## затравке ST_A почти не задевает ни 204, ни 404: из 115 адресов рельефа 113
## отвечают 200, а два пустых приходят на самом краю охвата. Значит обе ветки
## надо задеть нарочно, иначе они впервые сработают у игрока.
##
## Записанная грабля (bd recall godot-client-check) отсюда никуда не делась:
## этим бегуном НЕЛЬЗЯ заменить снимок. Он проверяет коды и числа, а не картинку.
##
## # ДВА КАТАЛОГА, И ДЕЛЕНИЕ ПО ПРИРОДЕ, А НЕ ПО РАЗМЕРУ
##
##   checks/pure/ — вычисление над данными. Сети не касается ВОВСЕ: контекст в
##     этом режиме не имеет ни WorldApi, ни NetClient (оба null), и потянувшаяся
##     к ним проверка падает громко. Гоняется на голой машине.
##   checks/live/ — договор с сервером: коды, заголовки, имена полей, провод.
##     Без поднятого сервера не имеет смысла и потому вынесена.
##
## Замер, ради которого разнос сделан (2026-08-12, до него): один файл в 1263
## строки, выросший за сутки на 722 строки правками четырёх агентов подряд, и
## 114 проверок из 132 требовали живого сервера — при том что серверу в
## арифметике границ уровней, раскладке шпал и тесселяции делать нечего.
##
## # ОБНАРУЖЕНИЕ, А НЕ СПИСОК
##
## Файлы не перечислены нигде: бегун обходит каталог и берёт всякий *.gd.
## Добавить проверку — положить файл, а не править общий. Именно правка общего
## файла и была причиной, по которой он разросся: другого места не было.
##
## Порядок — по имени файла, отсюда числовые приставки. Для checks/pure он не
## значит ничего (проверки независимы), для checks/live значит: сначала пролог
## (манифест, сеть), потом всё, что на них стоит.
extends SceneTree

const CheckReport := preload("res://tools/check_report.gd")
const CheckContext := preload("res://tools/check_context.gd")

const PURE_DIR := "res://tools/checks/pure"
const LIVE_DIR := "res://tools/checks/live"

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


func _run() -> void:
	var server := "http://127.0.0.1:8080"
	var region := "ST_A"
	var only := "all"
	for a in OS.get_cmdline_user_args():
		if a.begins_with("--server="):
			server = a.substr(9).rstrip("/")
		elif a.begins_with("--region="):
			region = a.substr(9)
		elif a.begins_with("--only="):
			only = a.substr(7)

	var report := CheckReport.new()
	var t0 := Time.get_ticks_usec()
	var title := ""

	if only == "pure" or only == "all":
		print("=== чистые проверки: вычисление над фикстурами, сети нет ===")
		var pure_ctx := CheckContext.new()
		pure_ctx.tree = self
		pure_ctx.report = report
		pure_ctx.offline = true
		await _run_dir(PURE_DIR, pure_ctx, report)
		title = "чистые"

	if only == "live" or only == "all":
		print("=== проверки с сервером: %s, регион %s ===" % [server, region])
		# СЛОЙ, А НЕ ТРУБА. Проверка ходит той же дорогой, что игра: спроси она
		# провод напрямую, она перестала бы покрывать настоящий путь ответа — а
		# именно там живут разбор заголовка, перевод единиц и имена исходов.
		#
		# Труба остаётся под рукой (ctx.net) ровно для тех проверок, которые
		# щупают ПРОВОД нарочно: «далёкий чанк — 204», «чужой регион — 404», «у
		# покрова нет заголовка базы». Они и обязаны говорить кодами: это
		# единственное место клиента, где проверяется сам договор с сервером, а
		# не его прочтение.
		var api := WorldApi.new()
		api.base_url = server
		root.add_child(api)
		var live_ctx := CheckContext.new()
		live_ctx.tree = self
		live_ctx.report = report
		live_ctx.region = region
		live_ctx.api = api
		live_ctx.net = api.net
		await _run_dir(LIVE_DIR, live_ctx, report)
		title = "с сервером" if only == "live" else "чистые и с сервером"

	if title == "":
		report.infra("неизвестный ключ --only=%s (pure, live, all)" % only)
		title = only

	var ms := float(Time.get_ticks_usec() - t0) / 1000.0
	quit(report.finish(title, ms))


## _run_dir — обход каталога проверок.
##
## Пустой каталог и незагрузившийся файл — СБОЙ УСТРОЙСТВА, а не пропуск.
## Обнаружение по каталогу покупает удобство ценой новой беды: проверка, тихо
## исчезнувшая вместе с опечаткой в имени файла, выглядит как зелёный прогон.
## Поэтому молчание здесь запрещено так же, как молчаливое умолчание в
## валидаторе карты.
func _run_dir(dir_path: String, ctx: CheckContext, report: CheckReport) -> void:
	var names := _suite_files(dir_path)
	if names.is_empty():
		report.infra("%s: ни одного файла проверки" % dir_path)
		return
	for n in names:
		var path := "%s/%s" % [dir_path, n]
		var scr: Script = load(path)
		if scr == null:
			report.infra("%s: скрипт не загрузился" % path)
			continue
		var suite = scr.new()
		suite.ctx = ctx
		report.begin(path)
		await suite.run()
		report.end()
		# Прогон прекращается, только когда дальше проверять нечем: без
		# манифеста нет правила, без правила нет ни одного адреса чанка, и
		# полсотни отказов подряд сказали бы одно и то же. Отказ ОТДЕЛЬНОЙ
		# проверки прогон не останавливает — это свойство было до разноса и
		# сохранено.
		if ctx.broken:
			report.infra("прогон прерван: %s" % ctx.broken_reason)
			return


func _suite_files(dir_path: String) -> Array[String]:
	var out: Array[String] = []
	var dir := DirAccess.open(dir_path)
	if dir == null:
		return out
	for file_name in dir.get_files():
		if file_name.ends_with(".gd"):
			out.append(file_name)
	out.sort()
	return out
