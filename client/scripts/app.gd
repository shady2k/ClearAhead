extends Node
## ОБОЛОЧКА ИГРЫ: меню -> карта -> роль -> игра. Главная сцена проекта.
##
## Зачем она есть. До неё клиент стартовал сразу в отрисовку того, что держит
## сервер, а 3D-мир жил отдельными сценами-спайками, которые читали эталон
## файлом и запускались каждая своей целью make. Это оснастка для проверки
## контракта, а не игра: у игры есть вход, выход и состояние, в котором она
## находится.
##
## ОДИН МИР, ТРИ РОЛИ. Мир — один и тот же для всех троих; роль решает две вещи:
## спавнить ли персонажа и откуда смотреть.
##
##   машинист  — персонаж есть, камера у него в голове;
##   строитель — персонажа нет, орто сверху под углом;
##   ДСП       — персонажа нет, то же место, плюс переключатель в плоскую схему.
##
## Схема ДСП — это БОЕВОЙ 2D-КЛИЕНТ, а не вторая его копия: те же world.gd,
## camera.gd и debug.gd, что и в scenes/main.tscn. Настоящее табло устроено
## именно так, и стрелки с занятостью (В2-В3) на схеме читаются несравнимо
## лучше, чем сверху в перспективе.
##
## ГЕОМЕТРИЮ ГРУЗИТ ОБОЛОЧКА, а не мир. Путь приходит с сервера один раз и
## отдаётся обоим потребителям — 3D-миру через geometry_override и 2D-схеме
## напрямую. Иначе об одной станции существовало бы два источника правды.
##
## Запуск:
##   godot --path client                              # меню
##   godot --path client -- --server http://host:8080
##   godot --path client -- --role driver             # сразу в роль, минуя меню
##   godot --path client -- --offline                 # эталон вместо сервера

const GM := preload("res://scripts/geometry_math.gd")
const GeometrySource := preload("res://scripts/geometry_source.gd")
const ShellUI := preload("res://scripts/shell_ui.gd")
const SpikeRelief := preload("res://scripts/spike_relief.gd")
const ChunkDebug := preload("res://scripts/chunk_debug.gd")

const WORLD_SCENE := "res://scenes/spike_world.tscn"   # мир без персонажа
const FPV_SCENE := "res://scenes/spike_fpv.tscn"       # тот же мир плюс человек

## Состояние оболочки. Переход между ними — единственный способ что-либо
## показать: экраны взаимоисключающи по построению.
##
## MAP появился, когда карт стало больше одной: до него оболочка знала, что мир
## ровно один, и это знание было зашито в переход «Новая игра -> роль».
enum State { MENU, MAP, ROLE, GAME, PAUSE }

## Как роль ставит камеру обзора. Числа — не украшение: SIZE в ортографии это
## ШИРИНА КАДРА в метрах, ELEV — наклон в градусах, AZ — азимут, FOCUS — точка
## взгляда в координатах пути. Строитель смотрит на станцию под углом, ДСП —
## почти сверху и шире: ему нужна вся горловина разом.
const ROLE_CAMERA := {
	ShellUI.Role.BUILDER: {"size": 300.0, "elev": 45.0, "az": 205.0, "focus": Vector2(240.0, 0.0)},
	ShellUI.Role.DSP: {"size": 520.0, "elev": 70.0, "az": 180.0, "focus": Vector2(240.0, 0.0)},
}

const ROLE_HINTS := {
	ShellUI.Role.DRIVER: "V — вид · WASD, Shift — идти и бежать · пробел — прыжок · мышь — взгляд · F — к локомотиву · F3 — чанки · Esc — меню",
	ShellUI.Role.DSP: "Tab — мир или схема · ЛКМ — орбита · WASD — панорама · колесо — зум · F3 — чанки · Esc — меню",
	ShellUI.Role.BUILDER: "ЛКМ — орбита · WASD — панорама · колесо — зум · P — проекция · F — к локомотиву · F3 — чанки · Esc — меню",
}

@onready var world3d: Node3D = $World3D
@onready var schematic: Node2D = $Schematic
@onready var schematic_camera: Camera2D = $SchematicCamera
@onready var schematic_back: CanvasLayer = $SchematicBack
@onready var schematic_debug: Node2D = $Schematic/Debug
@onready var shell: CanvasLayer = $Shell
@onready var hud: CanvasLayer = $Hud

var _server_url := "http://localhost:8080"
var _geometry_file := ""
var _autostart_role := -1

var _source: GeometrySource
var _maps: Array = []       # последний каталог с сервера; пуст до первого запроса
var _geometry := {}
var _state := State.MENU
var _role := -1
var _world: Node3D          # инстанс мира текущей роли; вне игры — null
var _chunks: Node3D         # отладочный слой чанков; живёт столько же, сколько мир
var _schematic_on := false

func _ready() -> void:
	_parse_args()
	shell.new_game_requested.connect(_on_new_game)
	shell.map_chosen.connect(_on_map_chosen)
	shell.role_chosen.connect(_enter_role)
	shell.resume_requested.connect(_resume)
	shell.back_requested.connect(_to_menu)
	shell.to_menu_requested.connect(_to_menu)
	shell.quit_requested.connect(_quit)

	schematic_camera.zoom_changed.connect(schematic.set_zoom)
	_show_schematic(false)

	_source = GeometrySource.new()
	add_child(_source)
	_source.configure(_server_url, _geometry_file)
	_source.progress.connect(shell.say)
	_source.failed.connect(_on_load_failed)
	_source.maps_listed.connect(_on_maps_listed)
	_source.loaded.connect(_on_loaded)

	_to_menu()
	# --role входит в игру ТОЙ ЖЕ дорогой, что и кнопка «Новая игра»: загрузка
	# начинается там, и без этого вызова оболочка молча стояла бы в меню, ожидая
	# геометрию, которую никто не заказал.
	if _autostart_role >= 0:
		_on_new_game()

func _parse_args() -> void:
	_server_url = String(ProjectSettings.get_setting("client/server_url", _server_url)).trim_suffix("/")
	var args := OS.get_cmdline_user_args()
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
		elif arg == "--offline":
			# Эталон вместо сервера: проверяется мир и роли, а не сеть.
			_geometry_file = GeometrySource.golden_path()
		elif arg.begins_with("--role="):
			_autostart_role = _role_by_name(arg.trim_prefix("--role="))
		elif arg == "--role" and i + 1 < args.size():
			_autostart_role = _role_by_name(args[i + 1])
			i += 1
		i += 1

func _role_by_name(name: String) -> int:
	match name.to_lower():
		"driver", "машинист": return ShellUI.Role.DRIVER
		"dsp", "дсп": return ShellUI.Role.DSP
		"builder", "строитель": return ShellUI.Role.BUILDER
	push_error("APP: неизвестная роль %s" % name)
	return -1

## --- переходы состояний ---

func _to_menu() -> void:
	_teardown_world()
	_state = State.MENU
	get_tree().paused = false
	Input.set_mouse_mode(Input.MOUSE_MODE_VISIBLE)
	hud.hide_role()
	shell.show_menu()

## Новая игра: сперва КАКАЯ карта, потом кем на ней играть.
##
## Каталог спрашивается каждый раз, а не кэшируется: карты живут на сервере, их
## там могло стать больше с прошлого захода, и показывать вчерашний список —
## это врать про содержимое сервера ради одного сэкономленного запроса.
func _on_new_game() -> void:
	_state = State.MAP
	shell.say("Каталог карт…")
	_source.list_maps()

## Каталог пришёл. При --role выбор делается за игрока — первой картой списка:
## автостарт существует для снимков и проверок, и останавливаться в нём на
## экране выбора значит не доехать до того, ради чего он запускался.
func _on_maps_listed(maps: Array) -> void:
	_maps = maps
	if _state != State.MAP:
		return  # каталог догнал уже ушедшего из выбора — показывать нечего
	if _autostart_role >= 0 and not maps.is_empty():
		_choose_first_map()
		return
	shell.show_maps(maps, "Карты живут на сервере. Клиент рисует ту, которую сервер отдал."
		if _geometry_file == ""
		else "Сервера нет: играем эталон контракта из файла (--offline).")

## Взять первую карту каталога. Дорога автостарта (--role) и снимков: у обоих
## выбор карты не проверяется, они едут дальше — в роль и в мир.
func _choose_first_map() -> void:
	if _maps.is_empty():
		shell.say("Каталог карт пуст — выбирать нечего.")
		return
	_on_map_chosen(String((_maps[0] as Dictionary)["name"]))

## Карта выбрана. Геометрия грузится ЗДЕСЬ, а не при входе в роль: она общая
## для всех трёх ролей, и переключение роли не должно ходить в сеть.
func _on_map_chosen(name: String) -> void:
	_state = State.ROLE
	_geometry = {}   # прошлая станция больше не действует: роль её не построит
	shell.say("Загрузка станции…")
	_source.select_map(name)

func _on_loaded(geo: Dictionary, source: String) -> void:
	_geometry = geo
	# Схема собирается сразу: она общая для всех ролей и стоит один раз.
	schematic.set_geometry(geo)
	var bounds: Rect2 = schematic.get_server_bounds()
	schematic_camera.fit_to(GM.server_rect_to_godot(bounds))
	schematic_debug.set_geometry(geo, bounds)
	print("APP: станция %s rev %d — %d элементов (%s)" % [
		geo.map_id, geo.map_revision, geo.elements.size(), source])
	if _autostart_role >= 0:
		var role := _autostart_role
		_autostart_role = -1
		_enter_role(role)
		return
	if _state == State.ROLE:
		shell.show_roles()

## Отказ показывается ЧЕЛОВЕКУ НА ЭКРАНЕ. Проверять это грепом лога нельзя:
## отказ приходит асинхронно и обрабатывается штатно, в логе ни одной ошибки.
func _on_load_failed(message: String) -> void:
	shell.say("%s — станция не загружена." % message)

func _enter_role(role: int) -> void:
	if _geometry.is_empty():
		shell.say("Станция ещё не загружена.")
		return
	# Снос ПЕРЕД назначением роли, а не после: _teardown_world сбрасывает _role,
	# и обратный порядок оставлял роль в -1 — мир строился, HUD рисовался, а
	# всё, что спрашивает «кто я сейчас» (Tab у ДСП), молча не работало.
	_teardown_world()
	_role = role
	_build_world(role)
	_state = State.GAME
	get_tree().paused = false
	shell.hide_all()
	hud.show_role(ShellUI.ROLE_NAMES[role], ROLE_HINTS[role])

func _pause() -> void:
	_state = State.PAUSE
	get_tree().paused = true
	Input.set_mouse_mode(Input.MOUSE_MODE_VISIBLE)
	shell.show_pause()

func _resume() -> void:
	_state = State.GAME
	get_tree().paused = false
	shell.hide_all()
	# Курсор возвращает МИР, а не оболочка: захвачен он или отпущен, зависит от
	# вида, а вид знает только он.
	if _world != null and _world.has_method("resume_input"):
		_world.call("resume_input")

func _quit() -> void:
	get_tree().quit()

## --- мир роли ---

## Мир строится ОДНОЙ И ТОЙ ЖЕ сценой для строителя и ДСП и другой — для
## машиниста. Разница между ними ровно одна: spike_fpv наследует spike_world и
## добавляет к нему твердь под ногами и фигуру. Персонаж — не режим камеры, а
## обязательство: коллизии на каждом меше стоят почти секунды на старте, и мир
## без человека платить за них не обязан.
func _build_world(role: int) -> void:
	SpikeRelief.geometry_override = _geometry
	var cam: Dictionary = ROLE_CAMERA.get(role, {})
	if not cam.is_empty():
		SpikeRelief.shot_focus = cam["focus"]
		SpikeRelief.shot_size = cam["size"]
		SpikeRelief.shot_azimuth = cam["az"]
		SpikeRelief.shot_elev = cam["elev"]
		SpikeRelief.shot_persp = false
	var path := FPV_SCENE if role == ShellUI.Role.DRIVER else WORLD_SCENE
	var packed: PackedScene = load(path)
	if packed == null:
		shell.say("Сцена мира не загрузилась: %s" % path)
		_to_menu()
		return
	_world = packed.instantiate() as Node3D
	world3d.add_child(_world)
	_add_chunk_debug()
	_show_schematic(false)

## Отладочный слой чанков живёт РЯДОМ с миром, а не внутри него: он про
## протокол, а мир про то, что видно. Высоту он спрашивает у мира — рельеф
## знает только тот, кто его построил.
func _add_chunk_debug() -> void:
	_chunks = ChunkDebug.new()
	_chunks.name = "ChunkDebug"
	world3d.add_child(_chunks)
	if _world != null and _world.has_method("_height_at"):
		_chunks.set_height_source(Callable(_world, "_height_at"))

func _toggle_chunk_debug() -> void:
	if _chunks == null:
		return
	_chunks.toggle()
	hud.show_note(_chunks.legend() if _chunks.visible else "")

func _teardown_world() -> void:
	if _chunks != null:
		world3d.remove_child(_chunks)
		_chunks.queue_free()
		_chunks = null
	if _world != null:
		world3d.remove_child(_world)
		_world.queue_free()
		_world = null
	_role = -1
	hud.show_note("")
	_show_schematic(false)

## --- схема ДСП ---

## Схема закрывает мир целиком, а не висит поверх него: 2D в Godot рисуется над
## 3D, и полупрозрачная схема на лесу нечитаема. Фон — отдельный слой ПОД схемой
## (layer = -1), потому что сама схема живёт на обычном холсте.
func _show_schematic(on: bool) -> void:
	_schematic_on = on
	schematic.visible = on
	schematic_debug.visible = on and ProjectSettings.get_setting("client/debug_layer", false)
	schematic_back.visible = on
	schematic_camera.enabled = on
	world3d.visible = not on

func _toggle_schematic() -> void:
	if _role != ShellUI.Role.DSP:
		return
	_show_schematic(not _schematic_on)

## --- ввод оболочки ---

## _input, а не _unhandled_input: Esc обязан дойти до оболочки РАНЬШЕ мира.
## У spike_fpv на Esc свой смысл (отпустить курсор), и без перехвата выйти в
## меню из вида от первого лица было бы нечем.
##
## УЗЕЛ App ОБЪЯВЛЕН PROCESS_MODE_ALWAYS в сцене, и это не украшение: пауза
## останавливает у обычного узла не только _process, но и ввод. Оболочка,
## поставленная на паузу вместе с миром, перестала бы слышать Esc — и выйти из
## паузы клавишей, которой в неё вошли, стало бы нечем. На снимке паузы это не
## видно: кадр правильный, меню нарисовано.
func _input(event: InputEvent) -> void:
	if not (event is InputEventKey) or not event.pressed or event.echo:
		return
	var key := event as InputEventKey
	match key.keycode:
		KEY_ESCAPE:
			match _state:
				State.GAME:
					_pause()
				State.PAUSE:
					_resume()
				State.MAP, State.ROLE:
					shell.show_menu()
					_state = State.MENU
				State.MENU:
					return  # из главного меню Esc не выходит: выход — пункт меню
			get_viewport().set_input_as_handled()
		KEY_TAB:
			if _state == State.GAME and _role == ShellUI.Role.DSP:
				_toggle_schematic()
				get_viewport().set_input_as_handled()
		KEY_F3:
			# Отладочный слой доступен всем трём ролям: разбиение на чанки —
			# свойство мира, а не роли, и смотреть на него из кабины так же
			# осмысленно, как сверху.
			if _state == State.GAME and not _schematic_on:
				_toggle_chunk_debug()
				get_viewport().set_input_as_handled()
