extends Node
## Боевой 2D-клиент: схема станции, камера в метрах, зум и панорама.
##
## ЧТО ЭТО ТАКОЕ ПОСЛЕ ПОЯВЛЕНИЯ ОБОЛОЧКИ. Игра запускается через app.tscn —
## меню, выбор роли, мир. Эта сцена осталась ОСНАСТКОЙ: ею проверяется контракт
## отрисовки (`make shot`, `make shot-offline`, `make dev`), и ей же владеет
## роль ДСП, которая показывает ту же схему внутри игры. Отдельной «игрой» она
## не является и стартового экрана не имеет — это и было причиной завести
## оболочку.
##
## Откуда берётся геометрия — не здесь: этим занят geometry_source.gd, общий с
## оболочкой. Здесь остались аргументы запуска, сборка World и статусы в UI.
##
## Запуск:
##   godot --path client scenes/main.tscn                   # с сервером
##   godot --path client scenes/main.tscn -- --server http://host:8080
##   godot --path client scenes/main.tscn -- --geometry-file ../contract/render_geometry.golden.json
##   godot --path client scenes/main.tscn -- --debug        # отладочный слой

const GM := preload("res://scripts/geometry_math.gd")
const GeometrySource := preload("res://scripts/geometry_source.gd")

@onready var world: Node2D = $World
@onready var camera: Camera2D = $Camera2D
@onready var ui: CanvasLayer = $UI
@onready var debug: Node2D = $World/Debug

var _server_url := "http://localhost:8080"
var _geometry_file := ""
var _source: GeometrySource

func _ready() -> void:
	debug.visible = ProjectSettings.get_setting("client/debug_layer", false)
	_parse_args()  # может включить --debug поверх настройки проекта
	camera.zoom_changed.connect(world.set_zoom)
	ui.retry_requested.connect(_reload)

	_source = GeometrySource.new()
	add_child(_source)
	_source.configure(_server_url, _geometry_file)
	_source.progress.connect(ui.set_status)
	_source.failed.connect(ui.set_error)
	_source.loaded.connect(_apply_geometry)
	_source.load_geometry()

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

func _reload() -> void:
	_source.load_geometry()

func _apply_geometry(geo: Dictionary, source: String) -> void:
	world.set_geometry(geo)
	# Тип пишется явно: узлы объявлены базовыми классами (Node2D и т.п.), скрипты
	# на них без class_name, поэтому возврат метода для парсера — Variant, и `:=`
	# вывести тип не может.
	var bounds: Rect2 = world.get_server_bounds()
	camera.fit_to(GM.server_rect_to_godot(bounds))
	debug.set_geometry(geo, bounds)
	ui.set_status("%s · rev %d · %d элементов (%s)" % [
		geo.map_id, geo.map_revision, geo.elements.size(), source])
