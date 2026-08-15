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
	await _check_controls(ch, doc, snaps)
	await _check_turnout(ch, doc, snaps)
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


## РУКОЯТКИ — первая доменная команда, через настоящий сокет.
##
## Проверяется вся дорога: команда ушла, положение встало на сервере, ответ
## разобран клиентом, и следом САМ пришёл снапшот с новым положением — потому
## что состояние изменилось, а не потому что настал срок биения.
func _check_controls(ch: LiveChannel, doc, snaps: Array) -> void:
	var unit_id := ""
	var limits := {}
	var last: LiveChannel.Snapshot = snaps[snaps.size() - 1]
	for raw in last.units:
		var u := raw as Dictionary
		if (u.get("controls", null)) != null:
			unit_id = String(u.get("id", ""))
			break
	_ok("в партии есть машина с органами управления", unit_id != "")
	if unit_id == "":
		return

	# ПРЕДЕЛЫ — ИЗ ПАСПОРТА, а не из головы проверки: клиент их тоже берёт
	# оттуда, и проверка, зашившая своё число, разошлась бы с обоими.
	var set_res := await ctx.api.content()
	_ok("набор контента получен", not set_res.failed(), set_res.reason)
	if set_res.failed():
		return
	var type_id := ""
	for raw in last.units:
		var u := raw as Dictionary
		if String(u.get("id", "")) == unit_id:
			type_id = String(u.get("type", ""))
	for raw in (set_res.data.get("stock", []) as Array):
		var t := raw as Dictionary
		if String(t.get("id", "")) == type_id:
			limits = (t.get("controls", {})) as Dictionary
	_ok("паспорт машины называет ступени", not limits.is_empty(), type_id)
	if limits.is_empty():
		return
	var notches := int(limits.get("traction_notches", 0))
	_ok("ступеней тяги больше нуля", notches > 0, str(notches))

	var accepted: Array[Dictionary] = []
	var refused: Array[String] = []
	ch.controls_set.connect(func(_u: String, c: Dictionary) -> void: accepted.append(c))
	ch.controls_refused.connect(func(reason: String, _t: String) -> void: refused.append(reason))

	var before := snaps.size()
	# КРАН МАШИНИСТА В КОМАНДЕ: у машины с тормозной магистралью он обязателен —
	# команда ставит положение ВСЕХ органов разом. Берётся ПРИСЛАННЫЙ, а не
	# выдуманный: какое положение у машины сейчас, знает снапшот.
	var handle := String(_controls_now(ch, unit_id).get("handle", ""))
	ch.set_controls(unit_id, 7, 0, "forward", handle)
	var answered := await _until(func() -> bool: return not accepted.is_empty() or not refused.is_empty())
	# ПРИНЯТА, А НЕ ПРОСТО ОТВЕЧЕНА, и разница здесь куплена дефектом: отказ —
	# тоже ответ, и пока проверка спрашивала «ответил ли», она проходила зелёной,
	# а семь проверок ниже молча не выполнялись вовсе. Так и случилось в день,
	# когда у машины появилась магистраль и команда без крана стала отказной.
	_ok("команда органов ПРИНЯТА, а не отказана", not accepted.is_empty(),
		"отказы: %s" % str(refused))
	if accepted.is_empty():
		return
	var stood: Dictionary = accepted[0]
	var bad: String = doc.validate("controls", stood)
	_ok("положение в ответе сходится с договором", bad == "", bad)
	_ok("встало то, что просили",
		int(stood.get("traction", -1)) == 7 and String(stood.get("reverser", "")) == "forward",
		str(stood))

	# СНАПШОТ ПРИХОДИТ ОТ КОМАНДЫ: состояние изменилось — рассылка не ждёт биения.
	var came := await _until(func() -> bool: return snaps.size() > before)
	_ok("снапшот принёс новое положение сам", came)
	if came:
		var fresh: LiveChannel.Snapshot = snaps[snaps.size() - 1]
		var seen := {}
		for raw in fresh.units:
			var u := raw as Dictionary
			if String(u.get("id", "")) == unit_id:
				seen = (u.get("controls", {})) as Dictionary
		_ok("в снапшоте органы той же машины", int(seen.get("traction", -1)) == 7, str(seen))

	# СТУПЕНЬ ЗА ПРЕДЕЛОМ ПАСПОРТА — отказ с объявленной причиной.
	ch.set_controls(unit_id, notches + 1, 0, "forward", handle)
	var said := await _until(func() -> bool: return not refused.is_empty())
	_ok("ступень за пределом отказана", said, str(accepted.size()))
	if said:
		_ok("причина отказа объявлена договором", doc.refusal_reasons.has(refused[0]), refused[0])
		_ok("причина именно про ступень", refused[0] == "notch_out_of_range", refused[0])

	# И ВОЗВРАЩАЕМ МАШИНУ В НОЛЬ: проверка не оставляет мир в тяге, иначе
	# следующий прогон начинался бы не с того состояния, о котором он думает.
	ch.set_controls(unit_id, 0, 0, "neutral", handle)
	await _until(func() -> bool: return accepted.size() > 1)


## СТРЕЛКА — вторая доменная команда, тем же сокетом.
##
## Проверяется та же дорога, что у рукояток, плюс своё: положение стрелки едет в
## КОНВЕРТЕ (а не у единицы), команда называет положение явно, и снапшот с новым
## положением приходит САМ — то есть перевод попал в канонический хеш состояния.
## Последнее не мелочь: трижды до этого забытое в хеше состояние доезжало до
## клиента только секундным биением.
func _check_turnout(ch: LiveChannel, doc, snaps: Array) -> void:
	var last: LiveChannel.Snapshot = snaps[snaps.size() - 1]
	_ok("в конверте приехали стрелки", not last.turnouts.is_empty(), str(last.turnouts.size()))
	if last.turnouts.is_empty():
		return
	var sw := last.turnouts[0] as Dictionary
	var bad: String = doc.validate("turnout", sw)
	_ok("стрелка в конверте сходится с договором", bad == "", bad)
	var sw_id := String(sw.get("id", ""))
	var was := String(sw.get("position", ""))
	_ok("положение остряка названо известным словом",
		was == LiveChannel.TURNOUT_STRAIGHT or was == LiveChannel.TURNOUT_DIVERGING, was)
	# МЕХАНИЗМ ЕДЕТ ВМЕСТЕ С ПОЛОЖЕНИЕМ: пульт узнаёт вид стрелки из снапшота, а
	# не вторым запросом за геометрией.
	_ok("механизм стрелки назван", String(sw.get("drive", "")) != "", str(sw))

	var set_ok: Array[Dictionary] = []
	var refused: Array[String] = []
	ch.turnout_set.connect(func(t: String, p: String) -> void: set_ok.append({"t": t, "p": p}))
	ch.turnout_refused.connect(func(reason: String, _t: String) -> void: refused.append(reason))

	var want := LiveChannel.TURNOUT_DIVERGING if was == LiveChannel.TURNOUT_STRAIGHT \
		else LiveChannel.TURNOUT_STRAIGHT
	var before := snaps.size()
	ch.set_turnout(sw_id, want)
	var answered := await _until(func() -> bool: return not set_ok.is_empty() or not refused.is_empty())
	_ok("команда перевода ПРИНЯТА, а не отказана", not set_ok.is_empty() and answered,
		"отказы: %s" % str(refused))
	if set_ok.is_empty():
		return
	_ok("ответ несёт положение, которое встало",
		String((set_ok[0] as Dictionary)["p"]) == want, str(set_ok[0]))

	var came := await _until(func() -> bool: return snaps.size() > before)
	_ok("снапшот принёс новое положение стрелки сам", came)
	if came:
		var fresh: LiveChannel.Snapshot = snaps[snaps.size() - 1]
		var seen := ""
		for raw in fresh.turnouts:
			var t := raw as Dictionary
			if String(t.get("id", "")) == sw_id:
				seen = String(t.get("position", ""))
		_ok("в снапшоте та же стрелка переведена", seen == want, "%s -> %s" % [was, seen])

	# НЕИЗВЕСТНОЕ ПОЛОЖЕНИЕ — отказ с объявленной причиной, а не тихий ноль.
	refused.clear()
	ch.set_turnout(sw_id, "боком")
	var said := await _until(func() -> bool: return not refused.is_empty())
	_ok("неизвестное положение отказано", said, str(set_ok.size()))
	if said:
		_ok("причина отказа объявлена договором", doc.refusal_reasons.has(refused[0]), refused[0])
		_ok("причина именно про положение", refused[0] == "unknown_turnout_position", refused[0])

	# И ВОЗВРАЩАЕМ СТРЕЛКУ КАК БЫЛА: проверка не оставляет мир переведённым,
	# иначе следующий прогон начинался бы не с того состояния, о котором думает.
	ch.set_turnout(sw_id, was)
	await _until(func() -> bool: return set_ok.size() > 1)


## _controls_now — органы машины по последнему снапшоту. Нужен, чтобы команда
## несла ТО положение крана, которое у машины есть: подставить своё значило бы
## проверять договор против собственной догадки о машине.
func _controls_now(ch: LiveChannel, unit_id: String) -> Dictionary:
	if ch.last_snapshot == null:
		return {}
	for raw in ch.last_snapshot.units:
		var u := raw as Dictionary
		if String(u.get("id", "")) == unit_id:
			var c: Variant = u.get("controls", {})
			if typeof(c) == TYPE_DICTIONARY:
				return c as Dictionary
	return {}


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
