## КАНАЛ КОМАНД — договор с сервером через настоящий сокет.
##
## Чистая проверка рядом (checks/pure/95_channel_contract.gd) сверяет РАЗБОР
## клиента с объявлением и обходится без сети. Здесь проверяется то, чего без
## сервера не проверить: что сокет открывается по объявленному адресу, что
## рукопожатие отвечает объявленной формой, что снапшот приходит САМ, что сессия
## переживает разрыв и что отказ несёт машинную причину из списка договора.
##
## Стоит после манифеста (00) и до всего, что стоит на рельефе: пролог сетевой
## половины уже прошёл, а порядок здесь — имя файла.
##
## ТРУБА ЗДЕСЬ ЗОВЁТСЯ НАРОЧНО. Слой (LiveChannel) сам никогда не пошлёт чужую
## версию конверта — он знает свою. Проверить отказ по версии можно только
## сказав в провод то, чего слой не говорит, и это ровно тот случай, ради
## которого проверкам позволено щупать провод: «единственное место клиента, где
## проверяется сам договор, а не его прочтение».
extends "res://tools/check_suite.gd"

const ContractDoc := preload("res://tools/contract_doc.gd")

## Сколько ждём события канала. Сервер шлёт биение не реже раза в секунду
## модельного времени; три секунды — это три пропущенных биения, то есть уже не
## «медленно», а «не работает».
const WAIT_S := 3.0


func run() -> void:
	var doc = ContractDoc.load_channel()
	_ok("договор канала прочитан", not doc.failed(), doc.reason)
	if doc.failed():
		return

	var ch := LiveChannel.new()
	ch.base_url = ctx.api.base_url
	ch.region = ctx.region
	var snaps: Array[LiveChannel.Snapshot] = []
	var breaks: Array[String] = []
	ch.snapshot.connect(func(s: LiveChannel.Snapshot) -> void: snaps.append(s))
	ch.broke.connect(func(why: String) -> void: breaks.append(why))
	ctx.tree.root.add_child(ch)
	ch.start()

	var greeted := await _until(func() -> bool: return ch.greeted)
	_ok("рукопожатие прошло", greeted, str(breaks))
	if not greeted:
		ch.stop()
		ch.queue_free()
		return

	_ok("сервер выдал сессию и актёра", ch.session_id != "" and ch.actor_id != "",
		"сессия %s, актёр %s" % [ch.session_id, ch.actor_id])
	# Рукопожатие обязано отдать ПОЛНЫЙ снапшот: иначе клиент ждал бы первого
	# изменения мира, чтобы узнать, что в нём стоит.
	_ok("рукопожатие отдало снапшот", snaps.size() == 1, str(snaps.size()))
	if snaps.is_empty():
		ch.stop()
		ch.queue_free()
		return
	var first := snaps[0]
	_ok("снапшот про наш регион", first.region == ctx.region, first.region)
	_ok("снапшот полный", first.full(), first.kind)
	_ok("номер первого снапшота — единица", first.snapshot_seq == 1, str(first.snapshot_seq))
	_ok("модельное время приехало числом микросекунд", first.time_us > 0, str(first.time_us))

	# СНАПШОТ ПРИХОДИТ САМ. Это главное отличие канала от ручки live: клиент ни
	# о чём не просил, а состояние приехало.
	var before := snaps.size()
	var came := await _until(func() -> bool: return snaps.size() > before)
	_ok("снапшот приходит без запроса", came, "снимков %d" % snaps.size())
	if came:
		var last: LiveChannel.Snapshot = snaps[snaps.size() - 1]
		_ok("номера снапшотов растут", last.snapshot_seq > first.snapshot_seq,
			"%d -> %d" % [first.snapshot_seq, last.snapshot_seq])
		# Биение идёт по МОДЕЛЬНОМУ времени, и между двумя снимками неподвижного
		# мира обязана пройти секунда модельного времени, а не кадр реального.
		_ok("между биениями прошло модельное время", last.time_us > first.time_us,
			"%d -> %d мкс" % [first.time_us, last.time_us])

	await _check_reconnect(ch, snaps, breaks)
	ch.stop()
	ch.queue_free()
	await _check_refusal(doc)


## СЕССИЯ ПЕРЕЖИВАЕТ РАЗРЫВ — то, ради чего session_id вообще существует.
func _check_reconnect(ch: LiveChannel, snaps: Array, breaks: Array) -> void:
	var was_session := ch.session_id
	var was_actor := ch.actor_id
	var was_seq := ch.last_snapshot_seq
	# Разрыв изображается уходом и возвратом: настоящий обрыв кабеля здесь
	# нечем устроить, а проверяется не он, а то, что клиент возвращается в ТУ ЖЕ
	# сессию, назвав её сервером выданный идентификатор.
	ch.stop()
	await ctx.tree.process_frame
	ch.start()
	var back := await _until(func() -> bool: return ch.greeted)
	_ok("клиент вернулся в канал", back, str(breaks))
	if not back:
		return
	_ok("сессия та же", ch.session_id == was_session, "%s -> %s" % [was_session, ch.session_id])
	_ok("актёр тот же", ch.actor_id == was_actor, "%s -> %s" % [was_actor, ch.actor_id])
	# Счётчик снапшотов принадлежит СЕССИИ и продолжается: обнуление означало бы,
	# что после каждого разрыва пропусков как будто не было.
	_ok("счётчик снапшотов продолжился", ch.last_snapshot_seq > was_seq,
		"%d -> %d" % [was_seq, ch.last_snapshot_seq])


## ОТКАЗ — тоже часть договора: клиент разбирает его машинно.
func _check_refusal(doc) -> void:
	var pipe := NetChannel.new()
	ctx.tree.root.add_child(pipe)
	var answers: Array[Dictionary] = []
	pipe.answered.connect(func(_id: int, _result: Variant, error: Dictionary) -> void:
		answers.append(error))
	# Флаг МАССИВОМ, а не переменной: лямбда в GDScript захватывает окружение по
	# ЗНАЧЕНИЮ, и присваивание внутри неё меняло бы копию — снаружи флаг остался
	# бы ложным навсегда. Массив передаётся ссылкой, поэтому запись видна обоим.
	var opened: Array[bool] = [false]
	pipe.opened.connect(func() -> void: opened[0] = true)
	pipe.open(_ws_url())
	var up := await _until(func() -> bool: return opened[0])
	_ok("сокет открылся по адресу договора", up, _ws_url())
	if not up:
		pipe.queue_free()
		return
	# Версия заведомо чужая: сервер обязан отказать явно, а не работать молча по
	# своим правилам с тем, кто говорит по чужим.
	pipe.send("hello", {"protocol_version": 9000})
	var got := await _until(func() -> bool: return not answers.is_empty())
	_ok("сервер ответил на чужую версию", got)
	if got:
		var err: Dictionary = answers[0]
		_ok("отказ пришёл кодом договора", int(err.get("code", 0)) == int(doc.errors.get("refused", 0)),
			"код %s, договор %s" % [str(err.get("code")), str(doc.errors.get("refused"))])
		var data: Variant = err.get("data", {})
		var bad: String = doc.validate(doc.refusal_data, data)
		_ok("форма причины сходится с договором", bad == "", bad)
		var reason := String((data as Dictionary).get("reason", ""))
		_ok("причина объявлена в договоре", doc.refusal_reasons.has(reason), reason)
		_ok("причина именно про версию", reason == LiveChannel.REASON_UNSUPPORTED_PROTOCOL, reason)
	pipe.close()
	pipe.queue_free()


func _ws_url() -> String:
	var base: String = ctx.api.base_url
	if base.begins_with("https://"):
		return "wss://" + base.substr(8) + (LiveChannel.CHANNEL_PATH % ctx.region)
	return "ws://" + base.substr(7) + (LiveChannel.CHANNEL_PATH % ctx.region)


## _until — ждать события кадрами, а не сном.
##
## Сон в проверке — это тайминг, то есть мигающая проверка. Кадры же обязаны
## идти в любом случае: без них не опрашивается сокет, и ожидание сном никогда
## бы ничего не дождалось.
func _until(cond: Callable) -> bool:
	var deadline := Time.get_ticks_msec() + int(WAIT_S * 1000.0)
	while Time.get_ticks_msec() < deadline:
		await ctx.tree.process_frame
		if cond.call():
			return true
	return false
