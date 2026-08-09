extends Node
## Сборка клиента: аргументы, асинхронная загрузка манифеста и геометрии
## (HTTP с ETag/304 или локальный файл-эталон), сборка World, первичный охват
## камеры, статусы в UI. HTTP только асинхронный — главный поток ничего не ждёт.
##
## map_id и ревизию клиент НЕ знает заранее: в сетевом режиме он сначала
## получает манифест с /manifest и только после этого запрашивает геометрию
## адресованной ревизии. Пара (map_id, ревизия) живёт в памяти от манифеста;
## литералов карты в коде нет.
##
## Запуск:
##   godot --path client                                  # с сервером (по умолчанию)
##   godot --path client -- --server http://host:8080
##   godot --path client -- --geometry-file ../contract/render_geometry.golden.json
##   godot --path client -- --debug                        # отладочный слой

const Parser := preload("res://scripts/geometry_parser.gd")
const GM := preload("res://scripts/geometry_math.gd")

@onready var world: Node2D = $World
@onready var camera: Camera2D = $Camera2D
@onready var ui: CanvasLayer = $UI
@onready var debug: Node2D = $World/Debug

var _server_url := "http://localhost:8080"
var _map_id := ""   # заполняется из манифеста, литерала карты нет
var _revision := 0  # заполняется из манифеста
var _geometry_file := ""
var _etag := ""
var _source := ""
var _http_manifest: HTTPRequest
var _http_geometry: HTTPRequest

func _ready() -> void:
	debug.visible = ProjectSettings.get_setting("client/debug_layer", false)
	_parse_args()  # может включить --debug поверх настройки проекта
	camera.zoom_changed.connect(world.set_zoom)
	ui.retry_requested.connect(_load_geometry)
	if _geometry_file != "":
		ui.set_status("Загрузка геометрии из файла…")
		_load_file(_geometry_file)
	else:
		ui.set_status("Загрузка манифеста с %s…" % _server_url)
		_load_manifest()

func _parse_args() -> void:
	_server_url = String(ProjectSettings.get_setting("client/server_url", _server_url)).trim_suffix("/")
	var args := OS.get_cmdline_user_args()
	# Принимаются обе формы — `--server=URL` и `--server URL`: в документации и в
	# smoke_screenshot.gd аргументы передаются через пробел.
	var i := 0
	while i < args.size():
		var arg: String = args[i]
		if arg.begins_with("--server="):
			_server_url = arg.trim_prefix("--server=").trim_suffix("/")
		elif arg == "--server" and i + 1 < args.size():
			_server_url = args[i + 1].trim_suffix("/")
			i += 1
		elif arg.begins_with("--geometry-file="):
			_geometry_file = arg.trim_prefix("--geometry-file=")
		elif arg == "--geometry-file" and i + 1 < args.size():
			_geometry_file = args[i + 1]
			i += 1
		elif arg == "--debug":
			debug.visible = true
		i += 1

func _load_geometry() -> void:
	# кнопка «Повторить»: манифест ещё не получен — начинаем с него, иначе
	# повторяем только запрос геометрии (пара карты уже известна).
	if _geometry_file != "":
		ui.set_status("Повторное чтение файла…")
		_load_file(_geometry_file)
	elif _map_id == "":
		ui.set_status("Повтор манифеста…")
		_load_manifest()
	else:
		ui.set_status("Повтор запроса…")
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
		ui.set_error("Не удалось начать запрос: %s" % error_string(err))

func _on_manifest_completed(result: int, response_code: int, _headers: PackedStringArray, body: PackedByteArray) -> void:
	if result != HTTPRequest.RESULT_SUCCESS:
		# HTTPRequest.Result — свой enum, error_string() по нему врёт (числа не совпадают с Error)
		ui.set_error("Сервер недоступен (код %d)" % result)
		return
	if response_code != 200:
		ui.set_error("Сервер ответил HTTP %d" % response_code)
		return
	var data: Variant = JSON.parse_string(body.get_string_from_utf8())
	if typeof(data) != TYPE_DICTIONARY:
		ui.set_error("Манифест не разобран: сервер вернул не JSON")
		return
	var man: Dictionary = data
	var id_v: Variant = man.get("map_id")
	if typeof(id_v) != TYPE_STRING or (id_v as String).is_empty():
		ui.set_error("Манифест без map_id")
		return
	var rev_v: Variant = man.get("map_revision")
	if typeof(rev_v) != TYPE_FLOAT and typeof(rev_v) != TYPE_INT:
		ui.set_error("Манифест без числовой map_revision")
		return
	_map_id = id_v as String
	_revision = int(rev_v)
	if _revision < 1:
		ui.set_error("Манифест с map_revision %d" % _revision)
		return
	ui.set_status("Загрузка геометрии %s rev %d с %s…" % [_map_id, _revision, _server_url])
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
		ui.set_error("Не удалось начать запрос: %s" % error_string(err))

func _on_geometry_completed(result: int, response_code: int, headers: PackedStringArray, body: PackedByteArray) -> void:
	if result != HTTPRequest.RESULT_SUCCESS:
		# HTTPRequest.Result — свой enum, error_string() по нему врёт (числа не совпадают с Error)
		ui.set_error("Сервер недоступен (код %d)" % result)
		return
	if response_code == 304:
		# 304 — без тела, перерисовка не нужна
		ui.set_status("304 — геометрия без изменений")
		return
	if response_code != 200:
		ui.set_error("Сервер ответил HTTP %d" % response_code)
		return
	for header in headers:
		var idx := header.find(":")
		if idx < 0:
			continue
		if header.substr(0, idx).strip_edges().to_lower() == "etag":
			_etag = header.substr(idx + 1).strip_edges()
			break
	_source = "сеть"
	_apply_geometry(body.get_string_from_utf8())

func _load_file(path: String) -> void:
	var abs_path := _resolve_path(path)
	if not FileAccess.file_exists(abs_path):
		ui.set_error("Файл не найден: %s" % abs_path)
		return
	var text := FileAccess.get_file_as_string(abs_path)
	if text.is_empty():
		ui.set_error("Файл пуст: %s" % abs_path)
		return
	_source = "файл"
	_apply_geometry(text)

func _resolve_path(path: String) -> String:
	if path.begins_with("res://") or path.begins_with("user://"):
		return path
	if path.is_absolute_path():
		return path
	return ProjectSettings.globalize_path("res://").path_join(path)

func _apply_geometry(text: String) -> void:
	var res := Parser.parse(text)
	if not res.ok:
		push_error(res.error)
		ui.set_error("Геометрия не разобрана: %s" % res.error)
		return
	var geo: Dictionary = res.geometry
	world.set_geometry(geo)
	# Тип пишется явно: узлы объявлены базовыми классами (Node2D и т.п.), скрипты
	# на них без class_name, поэтому возврат метода для парсера — Variant, и `:=`
	# вывести тип не может.
	var bounds: Rect2 = world.get_server_bounds()
	camera.fit_to(GM.server_rect_to_godot(bounds))
	debug.set_geometry(geo, bounds)
	ui.set_status("%s · rev %d · %d элементов (%s)" % [
		geo.map_id, geo.map_revision, geo.elements.size(), _source])
