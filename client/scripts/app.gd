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
## выбора.
##
## У машиниста с 2026-08-12 (вечер) это «откуда» — ЧЕЛОВЕК, а не камера: он ходит
## по той же поверхности, которую видит, и служит линейкой всему остальному в
## кадре (разбор — в шапке driver.gd). Поездов при этом по-прежнему нет:
## подвижного состава не существует в модели мира, и обещать его на экране
## нельзя. Плоской схемы ДСП тоже нет — она снесена вместе со старым клиентом и
## не воскрешена.
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
##   godot --path client -- … --reach=1000    # дальность взгляда, метры
##   godot --path client -- … --reach=all     # сколько сервер хранит
##   godot --path client -- … --role=driver --driver-view=third  # видно самого человека
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
## одна из причин, по которым тот клиент снесли (разбор — в шапке
## orbit_camera.gd). Фокус и масштаб считаются из габаритов того, что приехало с
## сервера.
## У МАШИНИСТА ЗДЕСЬ НЕТ НИ УГЛОВ, НИ ШИРИНЫ КАДРА, И ЭТО НЕ ПРОПУСК. Он —
## единственная роль, которая смотрит ИЗ мира, а не НА мир: в ней стоит ЧЕЛОВЕК, и
## куда наведена камера, решает его голова. Всё, что оболочка про это знает и
## обязана сказать миру, — что роль ПЕШАЯ; углы обзорной орбиты вокруг человека
## живут при нём (Driver.ORBIT_*), потому что это его орбита, а не станции.
const ROLE_CAMERA := {
	ShellUIScript.Role.DRIVER: {"on_foot": true},
	ShellUIScript.Role.BUILDER: {"azimuth": 205.0, "elevation": 45.0, "ortho": true, "frame_factor": 1.15},
	ShellUIScript.Role.DSP: {"azimuth": 180.0, "elevation": 70.0, "ortho": true, "frame_factor": 2.0},
}

## Виды пешей роли для ключа запуска. Список закрытый: ключ с опечаткой — отказ,
## а не тихое умолчание (то же правило, что у --reach).
const DRIVER_VIEWS := ["orbit", "eye", "third"]

## ДАЛЬНОСТЬ ВЗГЛЯДА — НАСТРОЙКА ИГРОКА, И МЕТРЫ ЗДЕСЬ ЗАКОННЫ.
##
## Это свойство ВИДА, ровно того же рода, что углы камеры в ROLE_CAMERA выше,
## плотность рассева травы (world.gd::GRASS_FAR — тоже метры) и дальность тени.
## Граница ClearAhead-sjq отдаёт клиенту вид целиком: «сколько мира я хочу
## держать в кадре» — вопрос к игроку, а не к серверу. Сервер отвечает на другой
## вопрос — «докуда мир засеян», — и его ответ приезжает манифестом потолком.
##
## ЧИСЛА КРУГЛЫЕ ЧЕЛОВЕЧЕСКИЕ, А НЕ РАДИУСЫ КОЛЕЦ, и это нарочно. Радиусы
## уровней у затравки ST_A — 512, 1024, 2048, 4096, 8192 м, и список из них
## выглядел бы копией чисел мира, зашитой в клиент (запрещено, и справедливо).
## 500, 1000, 2000 — это просьба игрока, а не адрес сервера; во что она
## обратилась на самом деле, переводит ChunkRule по радиусам ИЗ МАНИФЕСТА, и
## обе величины стоят рядом в HUD («просили 2000 м, видно на 2048 м»).
##
## REACH_ALL — «сколько сервер хранит»: единственный пункт, в котором даль
## называет не клиент.
const VIEW_REACH_M := [500.0, 1000.0, 2000.0, 4000.0, 8000.0, ChunkRule.REACH_ALL]

## Умолчание — 2000 м, и выбрано оно ЗАМЕРОМ ЦЕНЫ, а не вкусом. Цена кольца
## (2026-08-12, затравка ST_A, потолок 8192 м, одна машина; таблица целиком — в
## шапке ChunkRule): 512 м стоят 98 ответов и 4.4 с, 1024 — 140 и 6.4 с, 2048 —
## 184 и 8.4 с, 4096 — 224 и 10.4 с, весь мир — 260 и 12.1 с. Каждое следующее
## кольцо стоит примерно двух секунд загрузки.
##
## Почему остановились на третьем кольце, а не на первом: на снимке роли driver
## и на снимке всего приехавшего рельефа (frame=terrain) видно, что к 2 км
## СОБСТВЕННАЯ ДЫМКА клиента (world.gd, fog_density 0.00018) уже съедает цвет —
## дальний край читается как белёсая муть, и платить за него ещё четырьмя
## секундами не за что. Ближе двух километров край мира начинает попадать в кадр
## твёрдой кромкой, и это видно.
##
## Довод целиком О РИСУНКЕ — про свою дымку и свой кадр, — и потому число
## законно живёт в клиенте. Сменится дымка или разрешение — сменится и оно, и
## сервер об этом не узнает.
const VIEW_REACH_DEFAULT_INDEX := 2

## Строка управления на экране. Обязана называть ТО И ТОЛЬКО ТО, что камера
## действительно слушает: строка, обещающая колесо там, где колеса у игрока нет,
## — то же враньё, против которого заведён закон «чего сервер не прислал, того на
## экране нет», только про орган управления вместо факта о мире.
##
## Жесты трекпада названы явно, а не спрятаны под словом «колесо». Именно они
## спасают мак, где колеса не бывает вовсе (разбор — в шапке обработчика ввода
## OrbitCamera), и промолчать о них значило бы оставить игрока искать зум там,
## где его нет.
## Подсказка машиниста называет ТРИ ВИДА, потому что органы управления у них
## разные: в обзоре мышь обходит человека кругом, в двух других — поворачивает
## его голову, и WASD в первом случае двигают камеру, а во втором ноги. Одна
## строка на все три врала бы двум из них.
const ROLE_HINTS := {
	ShellUIScript.Role.DRIVER: "V — вид (обзор · от первого лица · от третьего) · в видах от лица: мышь — взгляд, WASD — идти, Shift — бежать, пробел — прыжок, F — вернуться · в обзоре: ЛКМ — орбита, WASD — панорама, колесо и двупальцевый скролл — зум · Esc — меню",
	ShellUIScript.Role.DSP: "ЛКМ — орбита · Shift+ЛКМ — панорама · WASD — панорама · колесо, щипок, двупальцевый скролл, +/− — зум · P — проекция · Esc — меню",
	ShellUIScript.Role.BUILDER: "ЛКМ — орбита · Shift+ЛКМ — панорама · WASD — панорама · колесо, щипок, двупальцевый скролл, +/− — зум · P — проекция · Esc — меню",
}

var _server_url := "http://127.0.0.1:8080"
var _region := ""
var _frame := "network"
## С какого вида открывается пешая роль. Ключ существует ОТДЕЛЬНО от клавиши V и
## не дублирует её: клавишей вид переключает игрок, ключом — снимок и зонд,
## которым нажимать некому. Та же пара, что --role рядом с экраном выбора роли.
var _driver_view := "eye"
var _driver_view_refusal := ""
var _shot_path := ""
var _quit_when_done := false
var _autostart_role := -1
## Дальность взгляда: метры либо ChunkRule.REACH_ALL. Ключ запуска её задаёт
## числом, пункт меню — перебором по VIEW_REACH_M.
var _view_reach_m: float = VIEW_REACH_M[VIEW_REACH_DEFAULT_INDEX]
## Отказ разбора ключа --reach. Держится строкой, а не показывается сразу:
## разбор идёт до того, как появился экран, и сказать некому.
var _view_reach_refusal := ""

var _world3d: Node3D
var _shell: ShellUI
## Оболочка спрашивает у мира ровно одно — каталог регионов, — и спрашивает его
## СЛОВОМ, а не адресом: транспорт живёт в WorldApi.
var _api: WorldApi

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

	_api = WorldApi.new()
	_api.base_url = _server_url
	add_child(_api)

	_shell = ShellUIScript.new()
	_shell.name = "Shell"
	add_child(_shell)
	_shell.new_game_requested.connect(_on_new_game)
	_shell.view_reach_next_requested.connect(_on_view_reach_next)
	_shell.region_chosen.connect(_on_region_chosen)
	_shell.role_chosen.connect(_enter_role)
	_shell.resume_requested.connect(_resume)
	_shell.back_requested.connect(_to_menu)
	_shell.to_menu_requested.connect(_to_menu)
	_shell.quit_requested.connect(_quit)

	_to_menu()
	# Отказ разбора ключа доезжает до ЭКРАНА, а не до лога: при --quit-when-done
	# он вдобавок гасит клиент ненулевым кодом, иначе снимок с опечаткой в ключе
	# молча вышел бы с умолчанием и выглядел бы исправным.
	if _view_reach_refusal != "":
		_autostart_failed(_view_reach_refusal)
		return
	if _driver_view_refusal != "":
		_autostart_failed(_driver_view_refusal)
		return
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
		elif a.begins_with("--driver-view="):
			_parse_driver_view(a.substr(14))
		elif a.begins_with("--reach="):
			_parse_reach(a.substr(8))
		elif a.begins_with("--shot="):
			_shot_path = a.substr(7)
		elif a == "--quit-when-done":
			_quit_when_done = true


## _parse_reach — ключ --reach=. Метры числом либо «all» («сколько сервер хранит»).
##
## ОТКАЗЫВАЕТ, А НЕ ЧИНИТ. «--reach=далеко», «--reach=0», «--reach=-5» — это
## опечатка, и подставить вместо неё умолчание значило бы отдать снимок или
## замер, снятый не с той дальностью, за исправный. Правило проекта здесь то же,
## что у валидатора карты: молчаливое умолчание запрещено.
##
## Ключ существует ОТДЕЛЬНО от пункта меню и не дублирует его: пунктом дальность
## выбирает игрок, ключом — снимок, проверка и замер, которым в меню нажимать
## некому. Это та же пара, что --role рядом с экраном выбора роли.
func _parse_reach(text: String) -> void:
	var t := text.strip_edges().to_lower()
	if t == "all" or t == "всё" or t == "все":
		_view_reach_m = ChunkRule.REACH_ALL
		return
	if not t.is_valid_float():
		_view_reach_refusal = "ключ --reach=%s: дальность взгляда — метры числом либо all («сколько сервер хранит»)" % text
		return
	var v := t.to_float()
	if v <= 0.0:
		_view_reach_refusal = "ключ --reach=%s: дальность взгляда должна быть положительной — мир без земли вокруг не рисуется, он не существует" % text
		return
	_view_reach_m = v


## _parse_driver_view — ключ --driver-view=. Один из DRIVER_VIEWS.
##
## ОТКАЗЫВАЕТ, А НЕ ЧИНИТ, по той же причине, что --reach: снимок, снятый не с
## того вида из-за опечатки, выглядит исправным и врёт ровно про то, ради чего его
## снимали.
func _parse_driver_view(text: String) -> void:
	var t := text.strip_edges().to_lower()
	if not DRIVER_VIEWS.has(t):
		_driver_view_refusal = "ключ --driver-view=%s: вид пешей роли — один из %s" % [
			text, ", ".join(DRIVER_VIEWS)]
		return
	_driver_view = t


## _view_reach_label — надпись пункта меню. Метры игрока, а не радиус кольца:
## во что просьба обратилась, знает только мир, спросивший манифест.
func _view_reach_label() -> String:
	if not is_finite(_view_reach_m):
		return "Дальность взгляда: сколько есть у сервера"
	return "Дальность взгляда: %.0f м" % _view_reach_m


## _on_view_reach_next — перебор дальности по кругу.
##
## Меняется настройка, а не построенный мир: мир строится из неё при входе в
## роль. Смена дальности на уже построенном мире означала бы догрузку или снос
## чанков на ходу — этого сегодня нет, и делать вид, что есть, нельзя.
func _on_view_reach_next() -> void:
	var i := VIEW_REACH_M.find(_view_reach_m)
	# Не нашлось — значит дальность пришла ключом запуска и в списке её нет:
	# перебор начинается с начала, а не подменяет ключ ближайшим пунктом.
	_view_reach_m = VIEW_REACH_M[0] if i < 0 else VIEW_REACH_M[(i + 1) % VIEW_REACH_M.size()]
	_shell.show_menu(_view_reach_label(), true)


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
	_shell.show_menu(_view_reach_label())


## Новая игра: сперва КАКОЙ регион, потом кем в нём играть.
##
## Каталог спрашивается каждый раз, а не кэшируется: миры живут на сервере, их
## там могло стать больше с прошлого захода, и показывать вчерашний список —
## это врать про содержимое сервера ради одного сэкономленного запроса.
func _on_new_game() -> void:
	_state = State.REGION
	_shell.say("Каталог регионов…")
	var cat: WorldApi.Catalog = await _api.regions()
	if cat.failed():
		# Отказ показывается ЧЕЛОВЕКУ НА ЭКРАНЕ, а не молчит в лог: он приходит
		# асинхронно и обрабатывается штатно, в логе при этом ни одной ошибки
		# (bd recall godot-client-check).
		_autostart_failed("Каталог регионов: %s" % cat.reason)
		return
	_regions = cat.regions
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
		"driver_view": _driver_view,
		# Дальность взгляда едет рядом с углами камеры и по той же причине: и то
		# и другое — про взгляд, и владеет ими оболочка, а не мир.
		"view_reach_m": _view_reach_m,
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
			_shell.show_menu(_view_reach_label())
			_state = State.MENU
		State.MENU:
			return  # из главного меню Esc не выходит: выход — пункт меню
	get_viewport().set_input_as_handled()
