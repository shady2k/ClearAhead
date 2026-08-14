extends SceneTree
## ЗОНД СЕТИ КЛИЕНТА. Отвечает числами на вопрос «на каком этапе тормоза».
##
##   godot --headless --path client --script res://tools/net_probe.gd -- --server=…
##
## # Зачем он появился
##
## Загрузка рельефа стоит 18 секунд из 25 (замер зонда кабины), и бида
## ClearAhead-s42 винит в этом последовательность запросов: «260 запросов подряд
## по ~90 мс на петле, где round-trip — десятки микросекунд». Первое число
## замерено, второе — нет, и между ними лежит вопрос, который решает, что чинить:
## тормозит сервер, сеть или клиент.
##
## Серверную половину уже померили отдельно (Go, тот же адрес, 60 запросов):
## последовательно 13 мс на все, медиана 218 мкс на запрос, параллель ничего не
## даёт. Значит пол задержки — доли миллисекунды, и остальное добавляет клиент.
##
## Этот зонд меряет клиентскую половину и отвечает на три вопроса:
##
##   1. сколько стоит ОДИН запрос из Godot;
##   2. сколько при этом проходит ИТЕРАЦИЙ ГЛАВНОГО ЦИКЛА — если запрос стоит
##      целые итерации, дело не в сети, а в том, что HTTPRequest продвигает свой
##      автомат раз в кадр;
##   3. что даёт параллель — несколько HTTPRequest в полёте разом.
##
## # ОТВЕТ, ПОЛУЧЕННЫЙ 2026-08-15 (машина владельца, сервер на петле)
##
##   сервер (Go, 60 запросов)      13 мс на все, медиана 218 мкс на запрос
##   клиент последовательно        2070 мс на 60, то есть 34.5 мс на запрос
##   клиент по 8 в полёте           282 мс на те же 60 — в 7.3 раза быстрее
##   холостой главный цикл          6.90 мс на итерацию
##   итераций на один запрос        5.00
##
## Пять итераций по 6.90 мс и дают ровно 34.5 мс. Сеть тут ни при чём, сервер
## тоже: запрос стоит ПЯТЬ ОБОРОТОВ ГЛАВНОГО ЦИКЛА, потому что HTTPRequest
## продвигает свой автомат (соединиться, послать, опросить, принять, закрыть) по
## шагу за кадр.
##
## Отсюда же объясняются «~90 мс», записанные в s42 без разбора: замер там шёл в
## ИГРЕ, где кадр 16–21 мс, а не в headless с его 6.9 мс. Пять кадров по
## двадцать — восемьдесят с лишним миллисекунд. Проверка модели на боевом
## прогоне: 80 чанков — это ~191 запрос, при кадре 21.1 мс модель даёт 20.1 с,
## замер показал 18.2 с.
##
## СЛЕДСТВИЕ ДЛЯ ПОЧИНКИ: уменьшать надо не мир и не число ресурсов на чанк, а
## ЧИСЛО ОБОРОТОВ ЦИКЛА НА ЗАПРОС — то есть держать несколько запросов в полёте.
## Замер даёт 7.3 раза на восьми.
##
## --headless здесь ЗАКОННЕН, в отличие от зондов ходьбы и кабины: мир не
## строится, сцены нет, меряется только сеть.

const COUNT := 60   ## сколько адресов берём в замер
const IN_FLIGHT := 8 ## сколько запросов держим в полёте в параллельном замере

var _server := "http://127.0.0.1:8080"
var _region := "ST_A"
var _started := false


func _process(_delta: float) -> bool:
	if not _started:
		_started = true
		_run()
	return false


func _run() -> void:
	for a in OS.get_cmdline_user_args():
		if a.begins_with("--server="):
			_server = a.substr(9).rstrip("/")
		elif a.begins_with("--region="):
			_region = a.substr(9)

	var addrs := await _addresses()
	print("=== ЗОНД СЕТИ: сервер %s, адресов с телом %d ===" % [_server, addrs.size()])
	if addrs.is_empty():
		quit(1)
		return

	# 1. ПОСЛЕДОВАТЕЛЬНО — так, как это делает клиент сегодня.
	var t0 := Time.get_ticks_usec()
	var f0 := Engine.get_process_frames()
	var worst := 0
	for p in addrs:
		var one := Time.get_ticks_usec()
		await _fetch(p)
		worst = maxi(worst, Time.get_ticks_usec() - one)
	var seq_us := Time.get_ticks_usec() - t0
	var seq_frames := Engine.get_process_frames() - f0
	print("последовательно: %d запросов за %.0f мс, на запрос %.1f мс (худший %.1f мс)" % [
		addrs.size(), seq_us / 1000.0, seq_us / 1000.0 / addrs.size(), worst / 1000.0])
	print("  итераций главного цикла за замер %d — это %.2f на запрос, по %.2f мс на итерацию" % [
		seq_frames, float(seq_frames) / addrs.size(),
		(seq_us / 1000.0) / maxf(1.0, float(seq_frames))])

	# 2. ПАРАЛЛЕЛЬНО — несколько узлов HTTPRequest в полёте разом.
	t0 = Time.get_ticks_usec()
	f0 = Engine.get_process_frames()
	var done := [0]
	var next := 0
	while next < addrs.size():
		var batch := mini(IN_FLIGHT, addrs.size() - next)
		for i in range(batch):
			_fetch_async(addrs[next + i], done)
		next += batch
		while done[0] < next:
			await process_frame
	var par_us := Time.get_ticks_usec() - t0
	var par_frames := Engine.get_process_frames() - f0
	print("по %d в полёте: те же %d запросов за %.0f мс (в %.1f раза быстрее)" % [
		IN_FLIGHT, addrs.size(), par_us / 1000.0, float(seq_us) / float(par_us)])
	print("  итераций главного цикла за замер %d — это %.2f на запрос, по %.2f мс на итерацию" % [
		par_frames, float(par_frames) / addrs.size(),
		(par_us / 1000.0) / maxf(1.0, float(par_frames))])

	# 2а. ХОЛОСТОЙ ТЕМП главного цикла: с чем сравнивать цену запроса.
	t0 = Time.get_ticks_usec()
	for i in range(200):
		await process_frame
	var idle_us := Time.get_ticks_usec() - t0
	print("холостой цикл: 200 итераций за %.0f мс — по %.2f мс на итерацию" % [
		idle_us / 1000.0, idle_us / 1000.0 / 200.0])

	# 3. ЧТО ГОВОРИТ ДВИЖОК О КАДРЕ. Если запрос стоит кадр, а кадр в headless
	# идёт по своему темпу, то узкое место — не сеть, а темп опроса.
	print("предел кадров движка: %d (0 — без предела)" % Engine.max_fps)
	quit(0)


## _addresses — существующие адреса уровня 0: 200 есть, 204 пусто.
func _addresses() -> Array[String]:
	var out: Array[String] = []
	for cx in range(-6, 7):
		for cz in range(-6, 7):
			if out.size() >= COUNT:
				return out
			var p := "%s/regions/%s/chunks/0/%d/%d" % [_server, _region, cx, cz]
			if await _code(p) == 200:
				out.append(p)
	return out


func _code(url: String) -> int:
	var req := HTTPRequest.new()
	root.add_child(req)
	if req.request(url) != OK:
		req.queue_free()
		return 0
	var res: Array = await req.request_completed
	req.queue_free()
	return int(res[1])


## _fetch — один запрос СВОИМ узлом, как это делает NetClient.
##
## Узел заводится и сносится на каждый запрос — ровно так написан NetClient
## (fetch создаёт HTTPRequest, ждёт и зовёт queue_free). Если цена окажется в
## этом, замер покажет её здесь, а не спрячет за переиспользованием.
func _fetch(url: String) -> void:
	var req := HTTPRequest.new()
	root.add_child(req)
	if req.request(url) != OK:
		req.queue_free()
		return
	await req.request_completed
	req.queue_free()


func _fetch_async(url: String, done: Array) -> void:
	var req := HTTPRequest.new()
	root.add_child(req)
	req.request_completed.connect(func(_r: int, _c: int, _h: PackedStringArray, _b: PackedByteArray) -> void:
		done[0] += 1
		req.queue_free())
	if req.request(url) != OK:
		done[0] += 1
		req.queue_free()
