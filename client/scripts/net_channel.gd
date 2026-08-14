## NetChannel — ТРУБА СОКЕТА. Механика и ничего больше.
##
## Второй транспорт клиента рядом с NetClient, и деление между ними то же, что
## между NetClient и WorldApi: здесь соединение, кадры и корреляция «запрос —
## ответ», а смысл сообщений живёт слоем выше (live_channel.gd). Труба не знает
## ни одного имени метода, ни одного поля конверта и ни одной причины отказа.
##
## # Почему сокет вообще появился
##
## В шапке NetClient и WorldApi стояло «WebSocket придёт в В2 вместе с
## мутациями, раньше командовать нечем». Он пришёл раньше В2 и по другой
## причине: веха расщеплена (ClearAhead-wa51), и канал команд отделён от
## стрелок — потому что позиция контроллера тяги едет на сервер тем же путём,
## что перевод стрелки, а машинисту диспетчерская половина не нужна вовсе.
##
## WebSocketPeer в Godot первого класса — это и был довод, по которому транспорт
## выбран сокетом, а не парой «POST вверх + SSE вниз» (SSE пришлось бы собирать
## руками из HTTPClient).
##
## # Что здесь НЕ повторяется
##
## Разбор JSON-RPC на сервере живёт в барьере валидации, и там у него причина:
## сырой внешний вход обязан читать один пакет. У клиента такого барьера нет и
## быть не должно — сервер для клиента не «внешний недоверенный вход», а
## источник истины. Но правило «ничего не подставлять вместо ответа» общее:
## труба не чинит кадр, не додумывает поля и не глотает отказ. Испорченный кадр
## закрывает соединение с причиной, которую видно человеку.
class_name NetChannel
extends Node

## Кадр разобрался и оказался уведомлением сверху вниз.
signal notified(method: String, params: Dictionary)

## Пришёл ответ на наш запрос. error пуст, если ответ успешный.
signal answered(id: int, result: Variant, error: Dictionary)

## Соединение открылось (сокет готов принимать кадры).
signal opened

## Соединение кончилось. reason — человеческая причина, всегда непустая.
signal closed(reason: String)

## Предел входящего кадра. Сервер отвечает снапшотами, и их размер растёт
## вместе с числом единиц в партии; 256 КиБ — с запасом на порядок против
## сегодняшних килобайт. Свой предел нужен потому, что умолчание Godot (64 КиБ)
## обрезало бы снапшот молча, и выглядело бы это как «сервер прислал мусор».
const INBOUND_BUFFER_BYTES := 1 << 18

var _ws: WebSocketPeer = null
var _next_id := 0
## _open_seen — открытие сообщается ОДИН раз за соединение: состояние сокета
## опрашивается каждый кадр, и без этого сигнал шёл бы шестьдесят раз в секунду.
var _open_seen := false


## open — открыть сокет. Прежнее соединение закрывается.
func open(url: String) -> void:
	close("переоткрытие")
	_ws = WebSocketPeer.new()
	_ws.inbound_buffer_size = INBOUND_BUFFER_BYTES
	_open_seen = false
	var err := _ws.connect_to_url(url)
	if err != OK:
		_ws = null
		closed.emit("сокет не открылся: %s (%s)" % [error_string(err), url])


## close — закрыть сокет молча (сигнала не будет).
##
## Молча — потому что закрытие по своей воле не является событием для того, кто
## его и заказал. Разрыв со стороны сервера и испорченный кадр сообщаются
## сигналом; этот путь — «мы уходим сами».
func close(_why: String = "") -> void:
	if _ws == null:
		return
	_ws.close()
	_ws = null
	_open_seen = false


## connected — открыт ли сокет прямо сейчас.
func connected() -> bool:
	return _ws != null and _ws.get_ready_state() == WebSocketPeer.STATE_OPEN


## send — отправить запрос. Возвращает id запроса или -1, если сокет закрыт.
##
## id ВОЗВРАЩАЕТСЯ, а не прячется: ответ приходит сигналом, и вызывающему нужно
## чем-то отличить свой ответ от чужого. Числом, а не строкой, потому что
## строковый id пришлось бы ещё и порождать уникальным — а счётчик уникален по
## устройству.
func send(method: String, params: Dictionary) -> int:
	if not connected():
		return -1
	_next_id += 1
	var frame := {
		"jsonrpc": "2.0",
		"id": _next_id,
		"method": method,
		"params": params,
	}
	var err := _ws.send_text(JSON.stringify(frame))
	if err != OK:
		_break("кадр не отправился: %s" % error_string(err))
		return -1
	return _next_id


## Опрос сокета — каждый кадр. WebSocketPeer работает только пока его опрашивают:
## без poll() соединение не открывается вовсе, и это первое, на чём спотыкаются.
func _process(_delta: float) -> void:
	if _ws == null:
		return
	_ws.poll()
	match _ws.get_ready_state():
		WebSocketPeer.STATE_OPEN:
			if not _open_seen:
				_open_seen = true
				opened.emit()
			while _ws != null and _ws.get_available_packet_count() > 0:
				_take(_ws.get_packet())
		WebSocketPeer.STATE_CLOSED:
			var code := _ws.get_close_code()
			var why := _ws.get_close_reason()
			_ws = null
			_open_seen = false
			# Код −1 у Godot означает «закрылось без рукопожатия закрытия», то
			# есть оборванное соединение, а не отказ сервера. Разные события —
			# разные слова: клиент по этой строке решает, ждать ли и повторять.
			if code == -1:
				closed.emit("соединение оборвалось")
			elif why.is_empty():
				closed.emit("сервер закрыл канал (код %d)" % code)
			else:
				closed.emit("сервер закрыл канал: %s (код %d)" % [why, code])
		_:
			pass


## _take — разобрать один пришедший кадр.
func _take(packet: PackedByteArray) -> void:
	var text := packet.get_string_from_utf8()
	var parsed: Variant = JSON.parse_string(text)
	if typeof(parsed) != TYPE_DICTIONARY:
		_break("кадр не является объектом JSON")
		return
	var frame := parsed as Dictionary
	if String(frame.get("jsonrpc", "")) != "2.0":
		_break("кадр не помечен jsonrpc 2.0")
		return
	# Уведомление — кадр БЕЗ id. Признак берётся отсюда, а не из имени метода:
	# метод — это домен, а наличие ответа — свойство протокола.
	if not frame.has("id"):
		var params: Variant = frame.get("params", {})
		if typeof(params) != TYPE_DICTIONARY:
			_break("params уведомления не объект")
			return
		notified.emit(String(frame.get("method", "")), params as Dictionary)
		return
	# Ответ. id вернулся числом — тем же, каким уходил: сервер обязан вернуть
	# его как есть, и если он вернул что-то другое, соотнести ответ не с чем.
	var raw_id: Variant = frame.get("id")
	if typeof(raw_id) != TYPE_FLOAT and typeof(raw_id) != TYPE_INT:
		_break("id ответа не число")
		return
	var id := int(raw_id)
	var err_obj: Dictionary = {}
	if frame.has("error"):
		var e: Variant = frame.get("error")
		if typeof(e) != TYPE_DICTIONARY:
			_break("отказ не объект")
			return
		err_obj = e as Dictionary
	answered.emit(id, frame.get("result"), err_obj)


## _break — испорченный кадр: закрыть и назвать причину.
##
## Именно закрыть, а не пропустить кадр и жить дальше. Кадр, который клиент не
## понял, означает расхождение с договором, а не помеху на линии: продолжив, мы
## бы показывали игроку мир, собранный из половины сообщений.
func _break(reason: String) -> void:
	if _ws != null:
		_ws.close(1003, "клиент не разобрал кадр")
		_ws = null
	_open_seen = false
	closed.emit(reason)
