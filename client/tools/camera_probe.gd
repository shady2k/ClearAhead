extends SceneTree
## ЗОНД УПРАВЛЕНИЯ КАМЕРОЙ. Отвечает ЧИСЛОМ на вопрос «уводится ли точка
## взгляда», а не строкой «всё хорошо».
##
## Зачем отдельный зонд. Класс бага «жест есть в коде, но на этом вводе его не
## изобразить» в проекте уже случался дважды: зум был только на колесе мыши, и
## на трекпаде мака его не было вовсе; панорама была только на средней и правой
## кнопке — и на том же трекпаде камера оставалась привязанной к одной точке.
## Ни то, ни другое не видно ни в снимке (кадр правильный, просто его нельзя
## получить руками), ни в логе (ошибок нет). Видно только по состоянию камеры
## после нажатия, и вот его зонд и печатает.
##
## Клавиши подаются через Input.parse_input_event: это тот же путь, которым
## приходит настоящее нажатие, поэтому Input.is_key_pressed их видит — а именно
## его спрашивает _process камеры.
##
##   godot --path client --script res://tools/camera_probe.gd [-- --scene res://...]

const WARMUP := 3                # кадров на сборку сцены до первой пробы
const HOLD := 12                 # кадров держим клавишу

var _node: Node3D
var _frame := 0
var _stage := 0
var _before := Vector3.ZERO
var _fails := 0

func _initialize() -> void:
	var scene := _arg("--scene", "res://scenes/spike_relief.tscn")
	_node = (load(scene) as PackedScene).instantiate()
	root.add_child(_node)

func _process(_delta: float) -> bool:
	_frame += 1
	if _frame <= WARMUP:
		return false
	match _stage:
		0:
			_before = _node._cam_focus
			_press(KEY_W)
			_stage = 1
		HOLD:
			_release(KEY_W)
			_check("WASD: панорама вперёд", _before, _node._cam_focus)
			_before = _node._cam_focus
			_press(KEY_D)
		HOLD * 2:
			_release(KEY_D)
			_check("WASD: панорама вбок", _before, _node._cam_focus)
			_before = _node._cam_focus
			_node._focus_loco()
			_check("F: возврат на локомотив", _before, _node._cam_focus)
			_report()
			return true
	if _stage > 0:
		_stage += 1
	return false

## Сдвиг считается по горизонтали: панорама не трогает высоту точки взгляда.
func _check(what: String, before: Vector3, after: Vector3) -> void:
	var d := Vector2(after.x - before.x, after.z - before.z).length()
	var ok := d > 0.5
	if not ok:
		_fails += 1
	print("CAMERA PROBE: %-28s сдвиг %6.2f м  %s" % [what, d, "ok" if ok else "НЕ ДВИГАЕТСЯ"])

func _report() -> void:
	if _fails > 0:
		printerr("CAMERA PROBE FAIL: жестов без эффекта — %d" % _fails)
		quit(1)
		return
	print("CAMERA PROBE OK")
	quit(0)

## ПОСЛЕ КАЖДОГО СОБЫТИЯ ОЧЕРЕДЬ ПРОТАЛКИВАЕТСЯ. parse_input_event только кладёт
## событие в буфер, а разбирается буфер в начале кадра — и отпускание одной
## клавиши с нажатием следующей, стоящие подряд, попадают в один буфер. Порядок
## их разбора тогда решает исход пробы. Найдено на зонде ходьбы (walk_probe.gd):
## из двух одинаковых прогонов один показывал ходьбу, другой — полную
## неподвижность. Здесь та же пара стоит подряд (W отпускается, D нажимается).
func _press(key: Key) -> void:
	_key(key, true)

func _release(key: Key) -> void:
	_key(key, false)

func _key(key: Key, pressed: bool) -> void:
	var ev := InputEventKey.new()
	ev.keycode = key
	ev.physical_keycode = key
	ev.pressed = pressed
	Input.parse_input_event(ev)
	Input.flush_buffered_events()

func _arg(name: String, fallback: String) -> String:
	var args := OS.get_cmdline_user_args()
	var i := args.find(name)
	if i >= 0 and i + 1 < args.size():
		return args[i + 1]
	return fallback
