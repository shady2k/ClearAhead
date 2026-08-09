extends Node
## Сборка клиента: аргументы, асинхронная загрузка геометрии (HTTP с ETag/304
## или локальный файл-эталон), сборка World, первичный охват камеры, статусы
## в UI. HTTP только асинхронный — главный поток ничего не ждёт.
##
## Запуск:
##   godot --path client                                  # с сервером (по умолчанию)
##   godot --path client -- --server http://host:8080 --map ST_A --revision 1
##   godot --path client -- --geometry-file ../contract/render_geometry.golden.json
##   godot --path client -- --debug                        # отладочный слой

const Parser := preload("res://scripts/geometry_parser.gd")
const GM := preload("res://scripts/geometry_math.gd")

@onready var world: Node2D = $World
@onready var camera: Camera2D = $Camera2D
@onready var ui: CanvasLayer = $UI
@onready var debug: Node2D = $World/Debug

var _server_url := "http://localhost:8080"
var _map_id := "ST_A"
var _revision := 1
var _geometry_file := ""
var _etag := ""
var _source := ""
var _http: HTTPRequest

func _ready() -> void:
	debug.visible = ProjectSettings.get_setting("client/debug_layer", false)
	_parse_args()  # может включить --debug поверх настройки проекта
	camera.zoom_changed.connect(world.set_zoom)
	ui.retry_requested.connect(_load_geometry)
	if _geometry_file != "":
		ui.set_status("Загрузка геометрии из файла…")
		_load_file(_geometry_file)
	else:
		ui.set_status("Загрузка геометрии с %s…" % _server_url)
		_load_http()

func _parse_args() -> void:
	_server_url = String(ProjectSettings.get_setting("client/server_url", _server_url)).trim_suffix("/")
	for arg in OS.get_cmdline_user_args():
		if arg.begins_with("--server="):
			_server_url = arg.trim_prefix("--server=").trim_suffix("/")
		elif arg.begins_with("--map="):
			_map_id = arg.trim_prefix("--map=")
		elif arg.begins_with("--revision="):
			var rev := arg.trim_prefix("--revision=")
			if rev.is_valid_int():
				_revision = int(rev)
		elif arg.begins_with("--geometry-file="):
			_geometry_file = arg.trim_prefix("--geometry-file=")
		elif arg == "--debug":
			debug.visible = true

func _load_geometry() -> void:
	# кнопка «Повторить»
	if _geometry_file != "":
		ui.set_status("Повторное чтение файла…")
		_load_file(_geometry_file)
	else:
		ui.set_status("Повтор запроса…")
		_load_http()

func _geometry_url() -> String:
	return "%s/maps/%s/revisions/%d/geometry" % [_server_url, _map_id, _revision]

func _load_http() -> void:
	if _http == null:
		_http = HTTPRequest.new()
		add_child(_http)
		_http.request_completed.connect(_on_http_completed)
	var headers := PackedStringArray()
	if _etag != "":
		headers.append("If-None-Match: %s" % _etag)
	var err := _http.request(_geometry_url(), headers)
	if err != OK:
		ui.set_error("Не удалось начать запрос: %s" % error_string(err))

func _on_http_completed(result: int, response_code: int, headers: PackedStringArray, body: PackedByteArray) -> void:
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
