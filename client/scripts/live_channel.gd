## LiveChannel — ЖИВОЕ СОСТОЯНИЕ ПАРТИИ, приходящее само.
##
## Слой смысла над трубой сокета (net_channel.gd), ровно так же, как WorldApi
## стоит над NetClient: труба знает кадры, этот файл знает договор — версию
## конверта, рукопожатие, сессию, снапшот и причины отказа. Игра не знает ни
## того, ни другого: она получает разобранный снимок и показывает его.
##
## Договор объявлен в contract/channel.v1.json и читается ОБЕИМИ сторонами:
## сервер сверяет с ним свой провод, клиент — свой разбор (проверки
## checks/pure/95_channel_contract.gd и checks/live/15_channel.gd). Прежний
## эталон контракта снесли за то, что он «остался без второй стороны»; эта
## сторона — вторая.
##
## # Чем это отличается от WorldApi.live()
##
## Ручка GET /regions/{region}/live отвечает на ВОПРОС и отвечает один раз.
## Канал ПРИСЫЛАЕТ САМ: сервер решает, когда состояние стоит показать (при
## изменении либо не реже раза в секунду модельного времени). Разница станет
## обязательной, когда локомотив поедет: спрашивать по кругу нельзя — темп
## опроса стал бы темпом мира.
##
## Ручка остаётся живой, пока по ней ходят проверки клиента. Одно состояние
## отдаётся двумя способами, и это названная временная цена, а не недосмотр.
##
## # Сессия хранится, потому что разрыв — не конец разговора
##
## session_id выдаёт сервер, а возвращает клиент при переподключении: в сессии
## живут ключи идемпотентности, и команда, повторённая после разрыва, обязана
## вернуть ТОТ ЖЕ ответ, а не примениться второй раз. Отсюда же обязанность
## честно забыть сессию, когда сервер её не узнал: unknown_session означает
## «сервер перезапустился», и держаться за старый ключ значило бы считать
## защищёнными повторы, которых никто не защищает.
##
## # Чего здесь нет
##
## Команд вверх. Канал их принимает, но первая доменная команда — органы
## управления кабины (ClearAhead-6ygr): контроллер тяги, реверсор, кран
## машиниста. Здесь клиент только здоровается и слушает.
class_name LiveChannel
extends Node

## Пришёл разобранный снимок партии.
signal snapshot(snap: Snapshot)

## Канал перестал быть живым и вот почему. Строка для человека: HUD показывает
## её как есть, а не переводит обратно в код.
signal broke(reason: String)

## MAJOR версия конверта, на которой говорит ЭТОТ клиент.
##
## Своя копия числа здесь законна и неизбежна: версия — это то, что клиент
## УМЕЕТ, а не то, что ему прислали. Расхождение с договором ловит проверка
## контракта, а расхождение с сервером — сам сервер, отказом на рукопожатии.
const PROTOCOL_VERSION := 1

## Метод уведомления сверху вниз.
const SNAPSHOT_METHOD := "snapshot"

## Причины отказа, НА КОТОРЫЕ КЛИЕНТ ДЕЙСТВУЕТ по-разному.
##
## Список неполон нарочно: сервер вправе прислать причину, которой клиент не
## знает, и такую он показывает как есть. Здесь только те две, где от причины
## зависит ПОВЕДЕНИЕ, — иначе таблица причин превратилась бы во вторую копию
## договора, которую надо помнить обновлять.
const REASON_UNKNOWN_SESSION := "unknown_session"
const REASON_UNSUPPORTED_PROTOCOL := "unsupported_protocol_version"

## Хвост адреса канала. Единственное место в клиенте, где он записан.
const CHANNEL_PATH := "/regions/%s/channel"

## Пауза перед повторным соединением, секунды.
##
## Число ОЦЕНОЧНОЕ и названо оценкой: разрыв на LAN длится доли секунды, а
## осмысленного замера сегодня взять неоткуда — сервер один и на той же машине.
## Секунда выбрана как «человек не успевает заметить, сервер успевает
## подняться». Нарастающей паузы нет: она нужна против общего сервера, которому
## наплыв повторов мешает встать, а здесь сервер — соседний процесс игрока.
const RETRY_DELAY_S := 1.0

## Snapshot — разобранный конверт. ИМЕНОВАННЫЕ ПОЛЯ, а не словарь наружу:
## словарь заставил бы каждого читателя помнить имена провода, и первое же
## переименование поля разошлось бы по рендеру молча.
class Snapshot extends RefCounted:
	var protocol_version := 0
	var session_id := ""
	var snapshot_seq := 0
	var kind := ""
	var region := ""
	var match_id := ""
	## time_us — модельное время партии, микросекунды. На проводе СТРОКОЙ:
	## JSON.parse_string разбирает числа как float, и микросекунды потеряли бы
	## точность молча.
	var time_us := 0
	## units — единицы как они приехали. Здесь словари нарочно: их разбирает
	## RollingStock, у которого свой договор с этими полями, и второй разбор
	## по дороге означал бы второе место, где живут их имена.
	var units: Array = []

	func full() -> bool:
		return kind == "full"


var base_url := ""
var region := ""

## Что известно про сессию. Публично ради HUD и проверок: они обязаны видеть,
## та же сессия продолжилась или завелась новая.
var session_id := ""
var actor_id := ""
var last_snapshot_seq := 0
var last_snapshot: Snapshot = null

## Живой ли канал прямо сейчас — для HUD.
var greeted := false

var _pipe: NetChannel = null
var _hello_id := -1
var _retry_left := 0.0
## _closed_by_us — признак «мы уходим сами»: тогда переподключаться не надо.
var _closed_by_us := false


## Труба заводится в _init, а В ДЕРЕВО попадает в _ready, и это не порядок ради
## порядка. Разбор конверта обязан работать ДО дерева: чистая проверка контракта
## (checks/pure/95_channel_contract.gd) кормит слой конвертом без всякого сокета,
## и труба, созданная только в _ready, была бы там пустой ссылкой — то есть
## проверка падала бы на устройстве вместо того, чтобы проверять разбор.
func _init() -> void:
	_pipe = NetChannel.new()
	_pipe.name = "NetChannel"
	_pipe.opened.connect(_on_opened)
	_pipe.closed.connect(_on_closed)
	_pipe.notified.connect(_on_notified)
	_pipe.answered.connect(_on_answered)


func _ready() -> void:
	# Без дерева труба не опрашивается, а без опроса WebSocketPeer не соединяется
	# вовсе — это первое, на чём здесь спотыкаются.
	add_child(_pipe)


## start — открыть канал к региону.
func start() -> void:
	_closed_by_us = false
	_pipe.open(_url())


## stop — уйти самим. Переподключения не будет.
func stop() -> void:
	_closed_by_us = true
	greeted = false
	_pipe.close()


## _url — адрес канала. http → ws, https → wss: схема сокета выводится из схемы
## сервера, а не задаётся второй настройкой, которая однажды разойдётся с первой.
func _url() -> String:
	var base := base_url
	if base.begins_with("https://"):
		base = "wss://" + base.substr(8)
	elif base.begins_with("http://"):
		base = "ws://" + base.substr(7)
	return base + (CHANNEL_PATH % region)


func _process(delta: float) -> void:
	if _retry_left <= 0.0:
		return
	_retry_left -= delta
	if _retry_left <= 0.0:
		start()


func _on_opened() -> void:
	# Рукопожатие ПЕРВЫМ кадром: до него сервер не шлёт ничего, потому что не
	# знает, той ли мы версии.
	var params := {"protocol_version": PROTOCOL_VERSION}
	if session_id != "":
		params["session_id"] = session_id
		params["last_snapshot_seq"] = last_snapshot_seq
	_hello_id = _pipe.send("hello", params)
	if _hello_id < 0:
		_fail("рукопожатие не отправилось")


func _on_closed(reason: String) -> void:
	greeted = false
	if _closed_by_us:
		return
	broke.emit(reason)
	# Сессия НЕ забывается при разрыве: она и заведена затем, чтобы его пережить.
	_retry_left = RETRY_DELAY_S


func _on_answered(id: int, result: Variant, error: Dictionary) -> void:
	if id != _hello_id:
		# Ответ не на наш запрос. Сегодня клиент шлёт единственный запрос, и
		# такой ответ означал бы расхождение с сервером, а не чужой разговор.
		_fail("пришёл ответ на неизвестный запрос %d" % id)
		return
	if not error.is_empty():
		_refused(error)
		return
	if typeof(result) != TYPE_DICTIONARY:
		_fail("ответ на рукопожатие не объект")
		return
	var res := result as Dictionary
	var version := int(res.get("protocol_version", 0))
	if version != PROTOCOL_VERSION:
		# Сервер ответил успехом, но назвал другую версию. Такого быть не
		# должно — но если случилось, продолжать нельзя: дальше мы читали бы
		# конверт по своим правилам, а он собран по чужим.
		_fail("сервер говорит на версии %d, клиент на %d" % [version, PROTOCOL_VERSION])
		return
	session_id = String(res.get("session_id", ""))
	actor_id = String(res.get("actor_id", ""))
	greeted = true
	var env: Variant = res.get("snapshot")
	if typeof(env) != TYPE_DICTIONARY:
		_fail("рукопожатие без снапшота")
		return
	_take_envelope(env as Dictionary)


func _on_notified(method: String, params: Dictionary) -> void:
	if method != SNAPSHOT_METHOD:
		# Неизвестный МЕТОД пропускается молча, в отличие от неизвестного
		# кадра: это ровно то же правило, что «неизвестные поля клиент
		# игнорирует». Сервер вправе завести новое уведомление, не поднимая
		# major-версию, и старый клиент обязан продолжать работать.
		return
	if not greeted:
		_fail("снапшот пришёл до рукопожатия")
		return
	_take_envelope(params)


## _take_envelope — разобрать конверт.
##
## НЕИЗВЕСТНЫЕ ПОЛЯ ИГНОРИРУЮТСЯ, и это часть договора, а не небрежность:
## приборы машиниста приедут новыми полями конверта и не должны поднимать
## major-версию. Строгость живёт в проверке контракта, а не в бою.
func _take_envelope(env: Dictionary) -> void:
	var version := int(env.get("protocol_version", 0))
	if version != PROTOCOL_VERSION:
		_fail("конверт версии %d, клиент читает %d" % [version, PROTOCOL_VERSION])
		return
	var snap := Snapshot.new()
	snap.protocol_version = version
	snap.session_id = String(env.get("session_id", ""))
	snap.snapshot_seq = int(env.get("snapshot_seq", 0))
	snap.kind = String(env.get("kind", ""))
	snap.region = String(env.get("region", ""))
	snap.match_id = String(env.get("match", ""))
	snap.time_us = String(env.get("time", "0")).to_int()
	var raw_units: Variant = env.get("units", [])
	snap.units = (raw_units as Array) if typeof(raw_units) == TYPE_ARRAY else []
	if not snap.full():
		# Разностных снапшотов не существует, и применить их клиент не умеет.
		# Молча принять неизвестный вид значило бы схлопнуть мир на экране до
		# того, что изменилось.
		_fail("снапшот вида %s: клиент умеет только full" % snap.kind)
		return
	last_snapshot = snap
	last_snapshot_seq = snap.snapshot_seq
	snapshot.emit(snap)


## _refused — отказ сервера с машинной причиной.
func _refused(error: Dictionary) -> void:
	var data: Variant = error.get("data", {})
	var reason := ""
	if typeof(data) == TYPE_DICTIONARY:
		reason = String((data as Dictionary).get("reason", ""))
	var text := String(error.get("message", "отказ без объяснения"))
	if reason == REASON_UNKNOWN_SESSION:
		# Сервер перезапустился. Сессия забывается ЧЕСТНО, вместе со счётчиком
		# снапшотов: держаться за ключи, которых на той стороне нет, значит
		# считать защищёнными повторы, которых никто не защищает.
		session_id = ""
		actor_id = ""
		last_snapshot_seq = 0
		broke.emit("сервер не знает нашу сессию — здороваемся заново")
		_pipe.close()
		_retry_left = RETRY_DELAY_S
		return
	if reason == REASON_UNSUPPORTED_PROTOCOL:
		# Повторять нечем: чинится это обновлением клиента, а не ожиданием.
		_closed_by_us = true
		_pipe.close()
		greeted = false
		broke.emit(text)
		return
	_fail(text)


## _fail — расхождение с договором. Повторяем: разойтись мог и один ответ.
func _fail(reason: String) -> void:
	greeted = false
	_pipe.close()
	broke.emit(reason)
	_retry_left = RETRY_DELAY_S
