## App — ОБОЛОЧКА ИГРЫ: меню → регион → роль → игра. Главная сцена проекта.
##
## Порт из снесённого спайка (`app.gd`, 809280f^), приведённый к сегодняшнему
## клиенту.
##
## # Зачем она есть
##
## До неё клиент стартовал прямо в отрисовку того, что держит сервер: имя региона
## приходило ключом запуска, ракурс — ключом `--view`, и выйти из мира было
## некуда, потому что вне мира ничего не было. Это оснастка для проверки
## контракта, а не игра: у игры есть вход, выход и состояние, в котором она
## находится.
##
## # Один мир, три роли
##
## Мир — один и тот же для всех троих. Роль решает ровно одно: ОТКУДА на него
## смотрят. Больше она сегодня не решает ничего, и это сказано вслух на экране
## выбора: персонажа машиниста и плоской схемы ДСП не существует — они снесены
## вместе со старым клиентом и не воскрешены.
##
## # Роль и габарит — РАЗНЫЕ вещи, и потому это разные ключи
##
## `--role` отвечает на вопрос «кто смотрит»: тип камеры, углы, проекция. Это
## свойство ВЗГЛЯДА, и граница ClearAhead-sjq отдаёт его клиенту целиком.
## `--frame` отвечает на вопрос «на что навести»: габарит берётся ИЗ ДАННЫХ —
## вся сеть, только устройства, весь приехавший рельеф. Прежний `--view` мешал
## эти два вопроса в один список (`station`, `throat`, `track`, `wide`), отчего
## «вид с оси» и «вид горловины» стояли рядом, будучи разного рода.
##
## Запуск:
##   godot --path client -- --server=http://127.0.0.1:8080          # меню
##   godot --path client -- --server=… --region=ST_A --role=builder # сразу в роль
##   godot --path client -- … --role=builder --frame=throat --shot=/tmp/a.png
extends Node

const ShellUIScript := preload("res://scripts/shell_ui.gd")

const WORLD_SCENE := "res://scenes/world.tscn"

## Состояние оболочки. Переход между ними — единственный способ что-либо
## показать: экраны взаимоисключающи по построению.
enum State { MENU, REGION, ROLE, GAME, PAUSE }

## Как роль ставит камеру.
##
## Числа — свойства ВЗГЛЯДА и потому законно здесь: azimuth — азимут от +X,
## elevation — угол над плоскостью, ortho — проекция. Строитель смотрит на
## станцию под углом, ДСП — почти сверху: ему нужна вся горловина разом, и
## потому у него шире кадр (`frame_factor` — во сколько раз кадр больше габарита
## данных).
##
## ЧЕГО ЗДЕСЬ НЕТ: точки фокуса и ширины кадра в метрах. У спайка стояло
## `focus: Vector2(240.0, 0.0)` и `size: 300.0` — числа, взятые ниоткуда, и это
## одна из причин, по которым тот клиент снесли (шапка free_camera.gd). Фокус и
## масштаб считаются из габаритов того, что приехало с сервера.
const ROLE_CAMERA := {
	ShellUIScript.Role.BUILDER: {"azimuth": 205.0, "elevation": 45.0, "ortho": true, "frame_factor": 1.15},
	ShellUIScript.Role.DSP: {"azimuth": 180.0, "elevation": 70.0, "ortho": true, "frame_factor": 2.0},
}

const ROLE_HINTS := {
	ShellUIScript.Role.DRIVER: "мышь (правая) — обзор · WASD — движение · Q/E — вниз/вверх · колесо — скорость · Esc — меню",
	ShellUIScript.Role.DSP: "ЛКМ — орбита · Shift+ЛКМ — панорама · WASD — панорама · колесо — зум · P — проекция · Esc — меню",
	ShellUIScript.Role.BUILDER: "ЛКМ — орбита · Shift+ЛКМ — панорама · WASD — панорама · колесо — зум · P — проекция · Esc — меню",
}

var _server_url := "http://127.0.0.1:8080"
var _region := ""
var _frame := "network"
var _shot_path := ""
var _quit_when_done := false
var _autostart_role := -1

var _world3d: Node3D
var _shell: ShellUI
var _net: NetClient

var _state := State.MENU
var _role := -1
var _world: Node3D
var _regions: Array = []


func _ready() -> void:
	# Оболочка слышит ввод и на паузе: узел, поставленный на паузу вместе с
	# миром, перестал бы получать Esc — и выйти из паузы клавишей, которой в неё
	# вошли, стало бы нечем.
	process_mode = Node.PROCESS_MODE_ALWAYS
	_parse_args()

	_world3d = Node3D.new()
	_world3d.name = "World3D"
	add_child(_world3d)

	_net = NetClient.new()
	_net.base_url = _server_url
	add_child(_net)

	_shell = ShellUIScript.new()
	_shell.name = "Shell"
	add_child(_shell)
	_shell.new_game_requested.connect(_on_new_game)
	_shell.region_chosen.connect(_on_region_chosen)
	_shell.role_chosen.connect(_enter_role)
	_shell.resume_requested.connect(_resume)
	_shell.back_requested.connect(_to_menu)
	_shell.to_menu_requested.connect(_to_menu)
	_shell.quit_requested.connect(_quit)

	_to_menu()
	# --role входит в игру ТОЙ ЖЕ дорогой, что и кнопка «Новая игра»: каталог
	# спрашивается там, и без этого вызова оболочка молча стояла бы в меню,
	# ожидая регион, которого никто не заказал.
	if _autostart_role >= 0:
		_on_new_game()


func _parse_args() -> void:
	var args: PackedStringArray = OS.get_cmdline_user_args()
	if args.is_empty():
		args = OS.get_cmdline_args()
	for a in args:
		if a.begins_with("--server="):
			_server_url = a.substr(9).rstrip("/")
		elif a.begins_with("--region="):
			_region = a.substr(9)
		elif a.begins_with("--role="):
			_autostart_role = _role_by_name(a.substr(7))
		elif a.begins_with("--frame="):
			_frame = a.substr(8)
		elif a.begins_with("--shot="):
			_shot_path = a.substr(7)
		elif a == "--quit-when-done":
			_quit_when_done = true


func _role_by_name(name: String) -> int:
	match name.to_lower():
		"driver", "машинист": return ShellUIScript.Role.DRIVER
		"dsp", "дсп": return ShellUIScript.Role.DSP
		"builder", "строитель": return ShellUIScript.Role.BUILDER
	push_error("APP: неизвестная роль %s — знаю driver, dsp, builder" % name)
	return -1


## --- переходы состояний ---

func _to_menu() -> void:
	_teardown_world()
	_state = State.MENU
	get_tree().paused = false
	Input.set_mouse_mode(Input.MOUSE_MODE_VISIBLE)
	_shell.show_menu()


## Новая игра: сперва КАКОЙ регион, потом кем в нём играть.
##
## Каталог спрашивается каждый раз, а не кэшируется: миры живут на сервере, их
## там могло стать больше с прошлого захода, и показывать вчерашний список —
## это врать про содержимое сервера ради одного сэкономленного запроса.
func _on_new_game() -> void:
	_state = State.REGION
	_shell.say("Каталог регионов…")
	var r: Dictionary = await _net.fetch_json("/regions")
	if not r["ok"]:
		# Отказ показывается ЧЕЛОВЕКУ НА ЭКРАНЕ, а не молчит в лог: он приходит
		# асинхронно и обрабатывается штатно, в логе при этом ни одной ошибки
		# (bd recall godot-client-check).
		_autostart_failed("Каталог регионов: %s" % r["error"])
		return
	_regions = (r["data"] as Dictionary).get("regions", []) as Array
	if _state != State.REGION:
		return  # каталог догнал уже ушедшего из выбора — показывать нечего
	if _regions.is_empty():
		_autostart_failed("Сервер не держит ни одного региона — играть не во что.")
		return
	# При автостарте выбор делается за игрока: он существует для снимков и
	# проверок, и останавливаться в нём на экране выбора значит не доехать до
	# того, ради чего он запускался.
	if _autostart_role >= 0:
		_choose_autostart_region()
		return
	_shell.show_regions(_regions, "Регионы живут на сервере. Клиент рисует тот, который сервер отдал.")


## Регион автостарта: названный ключом, иначе первый играбельный.
##
## Названный ключом НЕ проверяется на присутствие в каталоге: если сервер о нём
## не знает, отказ придёт с манифеста и будет назван причиной, а не подменён
## тихой подстановкой соседнего региона.
func _choose_autostart_region() -> void:
	if _region != "":
		_on_region_chosen(_region)
		return
	for r_raw in _regions:
		var r: Dictionary = r_raw as Dictionary
		if bool(r.get("playable", false)):
			_on_region_chosen(String(r["region"]))
			return
	_autostart_failed("Ни одного играбельного региона: у всех есть рельеф, но нет сети.")


## Регион выбран. Мир НЕ строится здесь: он общий для всех трёх ролей, и
## переключение роли не должно ходить в сеть заново.
func _on_region_chosen(region: String) -> void:
	_region = region
	_state = State.ROLE
	if _autostart_role >= 0:
		var role := _autostart_role
		_autostart_role = -1
		_enter_role(role)
		return
	_shell.show_roles()


func _enter_role(role: int) -> void:
	# Снос ПЕРЕД назначением роли, а не после: _teardown_world сбрасывает _role,
	# и обратный порядок оставлял бы роль в -1 при построенном мире.
	_teardown_world()
	_role = role
	_state = State.GAME
	get_tree().paused = false
	_shell.hide_all()
	_build_world(role)


func _pause() -> void:
	_state = State.PAUSE
	get_tree().paused = true
	Input.set_mouse_mode(Input.MOUSE_MODE_VISIBLE)
	_set_world_input(false)
	_shell.show_pause()


func _resume() -> void:
	_state = State.GAME
	get_tree().paused = false
	_shell.hide_all()
	_set_world_input(true)


func _quit() -> void:
	get_tree().quit()


## _autostart_failed — автостарт не доехал до роли.
##
## Отказ говорится на экране, как всякий другой. Но при --quit-when-done экрана
## никто не читает: это снимок или проверка, и ждать там некому. Поймано на
## первом же запуске — клиент, не нашедший каталога, простоял в меню, пока его не
## убили по таймауту, и выглядело это зависанием клиента, а не отказом сервера.
func _autostart_failed(why: String) -> void:
	_shell.say(why)
	if _autostart_role < 0 and _role >= 0:
		return
	push_error("APP: автостарт не состоялся — %s" % why)
	if _quit_when_done:
		print("АВТОСТАРТ НЕ СОСТОЯЛСЯ: %s" % why)
		get_tree().quit(1)


## --- мир роли ---

func _build_world(role: int) -> void:
	var packed: PackedScene = load(WORLD_SCENE)
	if packed == null:
		_autostart_failed("Сцена мира не загрузилась: %s" % WORLD_SCENE)
		_to_menu()
		return
	_world = packed.instantiate() as Node3D
	# configure ДО добавления в дерево: мир начинает грузиться в _ready, и
	# настроенный после этого он успел бы сходить в сеть не за тем регионом.
	_world.call("configure", {
		"server_url": _server_url,
		"region": _region,
		"role": role,
		"role_name": ShellUIScript.ROLE_NAMES[role],
		"role_hints": ROLE_HINTS[role],
		"camera": ROLE_CAMERA.get(role, {}),
		"frame": _frame,
		"shot_path": _shot_path,
		"quit_when_done": _quit_when_done,
	})
	_world3d.add_child(_world)


func _teardown_world() -> void:
	if _world != null:
		_world3d.remove_child(_world)
		_world.queue_free()
		_world = null
	_role = -1


## _set_world_input — отдать или отобрать у мира жесты.
##
## Пауза останавливает _process и _unhandled_input у обычных узлов, но камера
## успевает получить событие того же кадра, в котором нажат Esc, и станция
## дёргается под уже нарисованным меню. Явный выключатель дешевле, чем разбор
## порядка обработки.
func _set_world_input(on: bool) -> void:
	if _world != null and _world.has_method("set_input_enabled"):
		_world.call("set_input_enabled", on)


## --- ввод оболочки ---

## _input, а не _unhandled_input: Esc обязан дойти до оболочки РАНЬШЕ мира. У
## камеры на Esc свой смысл (отпустить курсор), и без перехвата выйти в меню из
## мира было бы нечем.
func _input(event: InputEvent) -> void:
	if not (event is InputEventKey):
		return
	var key := event as InputEventKey
	if not key.pressed or key.echo:
		return
	if key.keycode != KEY_ESCAPE:
		return
	match _state:
		State.GAME:
			_pause()
		State.PAUSE:
			_resume()
		State.REGION, State.ROLE:
			_shell.show_menu()
			_state = State.MENU
		State.MENU:
			return  # из главного меню Esc не выходит: выход — пункт меню
	get_viewport().set_input_as_handled()
