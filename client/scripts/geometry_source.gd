extends Node
## Источник геометрии: откуда клиент берёт контракт отрисовки и как сообщает о
## неудаче. Вынесен из main.gd, когда потребителей стало двое — боевой клиент и
## оболочка игры; логика не менялась, менялось только место.
##
## Два входа, и они не равноправны:
##
##   сеть  — сначала GET /manifest (какая карта и какая ревизия), и только потом
##           GET /maps/{id}/revisions/{n}/geometry. Пара (map_id, ревизия) живёт
##           в памяти от манифеста; литералов карты в коде нет;
##   файл  — эталон contract/render_geometry.golden.json напрямую, без сервера.
##
## HTTP ТОЛЬКО АСИНХРОННЫЙ — главный поток ничего не ждёт.
##
## ОТКАЗ — ЭТО СИГНАЛ, А НЕ ЗАПИСЬ В ЛОГ. Проверять клиента грепом лога нельзя:
## отказ HTTP приходит асинхронно и обрабатывается штатно, в логе при этом НИ
## ОДНОЙ ошибки. Потребитель обязан показать `failed` человеку на экране.

const Parser := preload("res://scripts/geometry_parser.gd")

## Геометрия разобрана и готова к постройке. source — «сеть» или «файл»,
## строкой для показа человеку.
signal loaded(geometry: Dictionary, source: String)
## Не получилось. Текст — для человека на экране, не для лога.
signal failed(message: String)
## Промежуточный шаг: что происходит прямо сейчас.
signal progress(message: String)
## Каталог карт сервера. Элемент — {name, map_id, map_revision}; именно `name`
## адресует карту в POST /maps/load/{name}, а map_id адресует геометрию. Их
## путать нельзя: имя — файл в каталоге, map_id — тождество карты.
signal maps_listed(maps: Array)

## Имя единственной «карты» в офлайне. Экран выбора существует и без сервера:
## иначе --offline ходил бы другой дорогой, чем сеть, и проверял бы не то.
const OFFLINE_MAP := "эталон контракта"

var _server_url := "http://localhost:8080"
var _geometry_file := ""
var _map_id := ""   # заполняется из манифеста или из ответа maps.load
var _revision := 0  # заполняется оттуда же
var _etag := ""
var _http_manifest: HTTPRequest
var _http_geometry: HTTPRequest
var _http_maps: HTTPRequest
var _http_load: HTTPRequest

## Откуда брать. Непустой geometry_file выигрывает у сервера: это режим
## «проверяется отрисовка, а не сеть».
func configure(server_url: String, geometry_file: String = "") -> void:
	_server_url = server_url.trim_suffix("/")
	_geometry_file = geometry_file

## --- каталог карт --------------------------------------------------------
##
## Выбор карты — ДВА запроса, и порядок неслучаен:
##
##   GET  /maps              каталог: имя файла, map_id, ревизия;
##   POST /maps/load/{name}  сервер делает карту текущей и отдаёт манифест.
##
## Второй обязателен: geometry адресуется парой (map_id, ревизия), а её знает
## только сервер, прочитавший файл. Собрать URL геометрии из строки каталога
## соблазнительно и неверно — ревизия в каталоге взята из файла на диске, а
## отдавать сервер будет ту карту, которую он загрузил и провалидировал.

## Спросить каталог. В офлайне сервера нет, но экран выбора обязан быть тем же.
func list_maps() -> void:
	if _geometry_file != "":
		maps_listed.emit([{
			"name": OFFLINE_MAP,
			"map_id": "файл",
			"map_revision": 0,
		}])
		return
	if _http_maps == null:
		_http_maps = HTTPRequest.new()
		add_child(_http_maps)
		_http_maps.request_completed.connect(_on_maps_completed)
	progress.emit("Каталог карт с %s…" % _server_url)
	var err := _http_maps.request("%s/maps" % _server_url)
	if err != OK:
		failed.emit("Не удалось начать запрос каталога: %s" % error_string(err))

func _on_maps_completed(result: int, response_code: int, _headers: PackedStringArray, body: PackedByteArray) -> void:
	if result != HTTPRequest.RESULT_SUCCESS:
		failed.emit("Сервер недоступен (код %d)" % result)
		return
	if response_code != 200:
		failed.emit("Каталог карт: сервер ответил HTTP %d" % response_code)
		return
	var data: Variant = JSON.parse_string(body.get_string_from_utf8())
	if typeof(data) != TYPE_ARRAY:
		failed.emit("Каталог карт не разобран: сервер вернул не список")
		return
	var out: Array = []
	for item: Variant in data as Array:
		if typeof(item) != TYPE_DICTIONARY:
			continue
		var row: Dictionary = item
		var name_v: Variant = row.get("name")
		if typeof(name_v) != TYPE_STRING or (name_v as String).is_empty():
			continue  # строка без имени неадресуема — показывать её нечестно
		out.append({
			"name": name_v as String,
			"map_id": str(row.get("map_id", "")),
			"map_revision": int(row.get("map_revision", 0)),
		})
	if out.is_empty():
		failed.emit("На сервере нет ни одной карты")
		return
	maps_listed.emit(out)

## Выбрать карту по имени файла из каталога и загрузить её геометрию.
func select_map(name: String) -> void:
	if _geometry_file != "":
		progress.emit("Чтение геометрии из файла…")
		_load_file(_geometry_file)
		return
	if _http_load == null:
		_http_load = HTTPRequest.new()
		add_child(_http_load)
		_http_load.request_completed.connect(_on_load_completed)
	progress.emit("Загрузка карты %s…" % name)
	# Ревизия меняется вместе с картой, а ETag геометрии был от прошлой: не
	# сбросить его — значит получить 304 на чужой ресурс и остаться со старой
	# станцией на экране, без единой ошибки в логе.
	_etag = ""
	# Порядок сегментов /maps/load/{name}, а не /maps/{name}/load: операция
	# вторая, имя третье (httpapi/maps.go, mapNamePath).
	var err := _http_load.request(
		"%s/maps/load/%s" % [_server_url, name.uri_encode()],
		PackedStringArray(), HTTPClient.METHOD_POST, "")
	if err != OK:
		failed.emit("Не удалось начать запрос карты: %s" % error_string(err))

func _on_load_completed(result: int, response_code: int, _headers: PackedStringArray, body: PackedByteArray) -> void:
	if result != HTTPRequest.RESULT_SUCCESS:
		failed.emit("Сервер недоступен (код %d)" % result)
		return
	if response_code != 200:
		failed.emit("Карта не загружена: сервер ответил HTTP %d" % response_code)
		return
	var data: Variant = JSON.parse_string(body.get_string_from_utf8())
	if typeof(data) != TYPE_DICTIONARY:
		failed.emit("Ответ на загрузку карты не разобран: не JSON")
		return
	var man_v: Variant = (data as Dictionary).get("manifest")
	if typeof(man_v) != TYPE_DICTIONARY:
		failed.emit("Ответ на загрузку карты без манифеста")
		return
	if not _take_manifest(man_v as Dictionary):
		return
	progress.emit("Загрузка геометрии %s rev %d…" % [_map_id, _revision])
	_load_http()

## Разобрать манифест в пару (map_id, ревизия). Общее место двух дорог —
## GET /manifest и ответа maps.load: манифест у них один и тот же тип.
## Возвращает false, отправив failed: у вызывающего работа кончена.
func _take_manifest(man: Dictionary) -> bool:
	var id_v: Variant = man.get("map_id")
	if typeof(id_v) != TYPE_STRING or (id_v as String).is_empty():
		failed.emit("Манифест без map_id")
		return false
	var rev_v: Variant = man.get("map_revision")
	if typeof(rev_v) != TYPE_FLOAT and typeof(rev_v) != TYPE_INT:
		failed.emit("Манифест без числовой map_revision")
		return false
	var rev := int(rev_v)
	if rev < 1:
		failed.emit("Манифест с map_revision %d" % rev)
		return false
	_map_id = id_v as String
	_revision = rev
	return true

## --- геометрия -----------------------------------------------------------

## Начать (или повторить) загрузку. Повтор с уже полученным манифестом не
## перезапрашивает его: пара карты известна, спрашивать нечего.
func load_geometry() -> void:
	if _geometry_file != "":
		progress.emit("Чтение геометрии из файла…")
		_load_file(_geometry_file)
	elif _map_id == "":
		progress.emit("Загрузка манифеста с %s…" % _server_url)
		_load_manifest()
	else:
		progress.emit("Повтор запроса геометрии…")
		_load_http()

func _manifest_url() -> String:
	return "%s/manifest" % _server_url

func _geometry_url() -> String:
	return "%s/maps/%s/revisions/%d/geometry" % [_server_url, _map_id, _revision]

func _load_manifest() -> void:
	if _http_manifest == null:
		_http_manifest = HTTPRequest.new()
		add_child(_http_manifest)
		_http_manifest.request_completed.connect(_on_manifest_completed)
	var err := _http_manifest.request(_manifest_url())
	if err != OK:
		failed.emit("Не удалось начать запрос: %s" % error_string(err))

func _on_manifest_completed(result: int, response_code: int, _headers: PackedStringArray, body: PackedByteArray) -> void:
	if result != HTTPRequest.RESULT_SUCCESS:
		# HTTPRequest.Result — свой enum, error_string() по нему врёт (числа не совпадают с Error)
		failed.emit("Сервер недоступен (код %d)" % result)
		return
	if response_code != 200:
		failed.emit("Сервер ответил HTTP %d" % response_code)
		return
	var data: Variant = JSON.parse_string(body.get_string_from_utf8())
	if typeof(data) != TYPE_DICTIONARY:
		failed.emit("Манифест не разобран: сервер вернул не JSON")
		return
	if not _take_manifest(data as Dictionary):
		return
	progress.emit("Загрузка геометрии %s rev %d с %s…" % [_map_id, _revision, _server_url])
	_load_http()

func _load_http() -> void:
	if _http_geometry == null:
		_http_geometry = HTTPRequest.new()
		add_child(_http_geometry)
		_http_geometry.request_completed.connect(_on_geometry_completed)
	var headers := PackedStringArray()
	if _etag != "":
		headers.append("If-None-Match: %s" % _etag)
	var err := _http_geometry.request(_geometry_url(), headers)
	if err != OK:
		failed.emit("Не удалось начать запрос: %s" % error_string(err))

func _on_geometry_completed(result: int, response_code: int, headers: PackedStringArray, body: PackedByteArray) -> void:
	if result != HTTPRequest.RESULT_SUCCESS:
		# HTTPRequest.Result — свой enum, error_string() по нему врёт (числа не совпадают с Error)
		failed.emit("Сервер недоступен (код %d)" % result)
		return
	if response_code == 304:
		# 304 — без тела, перерисовка не нужна
		progress.emit("304 — геометрия без изменений")
		return
	if response_code != 200:
		failed.emit("Сервер ответил HTTP %d" % response_code)
		return
	for header in headers:
		var idx := header.find(":")
		if idx < 0:
			continue
		if header.substr(0, idx).strip_edges().to_lower() == "etag":
			_etag = header.substr(idx + 1).strip_edges()
			break
	_emit_parsed(body.get_string_from_utf8(), "сеть")

func _load_file(path: String) -> void:
	var abs_path := _resolve_path(path)
	if not FileAccess.file_exists(abs_path):
		failed.emit("Файл не найден: %s" % abs_path)
		return
	var text := FileAccess.get_file_as_string(abs_path)
	if text.is_empty():
		failed.emit("Файл пуст: %s" % abs_path)
		return
	_emit_parsed(text, "файл")

func _resolve_path(path: String) -> String:
	if path.begins_with("res://") or path.begins_with("user://"):
		return path
	if path.is_absolute_path():
		return path
	return ProjectSettings.globalize_path("res://").path_join(path)

func _emit_parsed(text: String, source: String) -> void:
	var res := Parser.parse(text)
	if not res.ok:
		push_error(res.error)
		failed.emit("Геометрия не разобрана: %s" % res.error)
		return
	loaded.emit(res.geometry, source)

## Путь к эталону контракта — тому же файлу, который читает серверный тест.
## Живёт здесь, потому что спрашивают его двое: режим `--geometry-file` без
## аргумента и оболочка, когда сервера нет.
static func golden_path() -> String:
	var client_dir := ProjectSettings.globalize_path("res://").trim_suffix("/")
	return client_dir.get_base_dir().path_join("contract/render_geometry.golden.json")
