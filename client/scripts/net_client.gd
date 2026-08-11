## NetClient — единственное место, где клиент говорит с сервером.
##
## Транспорт — HTTP, как записано в ClearAhead-sjq: WebSocket придёт в В2 вместе
## с мутациями, раньше командовать нечем.
##
## Правило этого узла: он НИЧЕГО не подставляет вместо ответа. Ни эталона при
## отказе, ни умолчания при пустом теле. Отказ возвращается вызывающему как
## отказ, и вызывающий обязан показать его на экране, а не проглотить, — потому
## что записанная в проекте грабля (bd recall godot-client-check) звучит так:
## отказ приходит асинхронно и обрабатывается штатно, в логе ни одной ошибки, а
## на экране 404.
extends Node
class_name NetClient

var base_url: String = ""
var timeout_s: float = 15.0

## Счётчики видны снаружи: они попадают в HUD и в отчёт, а значит должны
## считаться там же, где происходит запрос, а не восстанавливаться потом.
var requests_made: int = 0
var code_counts: Dictionary = {}


func _count(code: int) -> void:
	code_counts[code] = int(code_counts.get(code, 0)) + 1


## fetch — один GET. Возвращает словарь:
##   ok      — дошло ли до ответа вообще (транспорт, не HTTP-код);
##   code    — код HTTP (0, если ответа не было);
##   body    — тело как PackedByteArray;
##   headers — заголовки как PackedStringArray;
##   error   — человеческая причина, если ok == false.
func fetch(path: String) -> Dictionary:
	var req := HTTPRequest.new()
	req.timeout = timeout_s
	add_child(req)
	requests_made += 1

	var err := req.request(base_url + path)
	if err != OK:
		req.queue_free()
		return {"ok": false, "code": 0, "error": "request(%s) вернул %d" % [path, err]}

	var res: Array = await req.request_completed
	req.queue_free()

	var result: int = res[0]
	var code: int = res[1]
	var headers: PackedStringArray = res[2]
	var body: PackedByteArray = res[3]

	if result != HTTPRequest.RESULT_SUCCESS:
		return {"ok": false, "code": code, "error": "%s: %s" % [path, result_text(result)]}

	_count(code)
	return {"ok": true, "code": code, "body": body, "headers": headers}


## fetch_json — GET, ожидающий 200 и разбираемый JSON. Всё остальное — отказ.
func fetch_json(path: String) -> Dictionary:
	var r := await fetch(path)
	if not r["ok"]:
		return {"ok": false, "error": r["error"]}
	if r["code"] != 200:
		return {"ok": false, "error": "%s: сервер ответил HTTP %d" % [path, r["code"]]}
	var text := (r["body"] as PackedByteArray).get_string_from_utf8()
	var parsed: Variant = JSON.parse_string(text)
	if typeof(parsed) != TYPE_DICTIONARY:
		return {"ok": false, "error": "%s: тело не JSON-объект" % path}
	return {"ok": true, "data": parsed as Dictionary}


## result_text — причина отказа словами, а не номером перечисления.
##
## Номер понятен тому, кто писал клиент, а читать его будет тот, у кого не
## запустилась игра. «Транспорт вернул 2» и «сервер не отвечает» — одно и то же
## событие и разные сообщения; на экран идёт второе.
static func result_text(result: int) -> String:
	match result:
		HTTPRequest.RESULT_CANT_CONNECT:
			return "не удалось соединиться с сервером"
		HTTPRequest.RESULT_CANT_RESOLVE:
			return "имя сервера не разрешается"
		HTTPRequest.RESULT_CONNECTION_ERROR:
			return "соединение оборвалось"
		HTTPRequest.RESULT_TIMEOUT:
			return "сервер не ответил вовремя"
		HTTPRequest.RESULT_NO_RESPONSE:
			return "сервер закрыл соединение без ответа"
		HTTPRequest.RESULT_BODY_SIZE_LIMIT_EXCEEDED:
			return "тело ответа больше допустимого"
		_:
			return "отказ транспорта (код %d)" % result


## header_value — значение заголовка без учёта регистра имени.
##
## Заголовки приходят строками «Имя: значение»; регистр имени по RFC 9110
## незначим, и полагаться на тот, что прислал сервер, значит поставить рендер в
## зависимость от библиотеки на другой стороне.
static func header_value(headers: PackedStringArray, name: String) -> String:
	var want := name.to_lower() + ":"
	for h in headers:
		if h.to_lower().begins_with(want):
			return h.substr(want.length()).strip_edges()
	return ""
