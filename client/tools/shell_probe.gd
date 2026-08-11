extends SceneTree
## ДЕЙСТВУЮТ ЛИ ПЕРЕХОДЫ ОБОЛОЧКИ. Подаёт клавиши тем же путём, каким приходит
## настоящее нажатие, и печатает состояние после каждой.
##
## ЗАЧЕМ ОТДЕЛЬНО ОТ app_shot.gd. Снимок паузы доказывает, что меню НАРИСОВАНО,
## и ничего не говорит о том, можно ли из неё выйти: кадр в обоих случаях
## правильный, ошибок в логе нет. Дефект этого класса тут уже был — узел App без
## PROCESS_MODE_ALWAYS перестаёт слышать ввод вместе с миром, и Esc, которым в
## паузу вошли, из неё не выводит. Тот же класс, что зум только на колесе мыши и
## панорама только на средней кнопке (см. tools/camera_probe.gd).
##
##   godot --path client --script res://tools/shell_probe.gd -- --offline --role dsp
##
## Роль и источник геометрии разбирает сама оболочка — аргументы после `--`
## видны обоим. Проверки идут от состояния «игрок в мире», поэтому --role
## обязателен.

## Состояния app.gd: MENU, ROLE, GAME, PAUSE — порядок enum'а.
const STATE_NAMES := ["меню", "выбор роли", "игра", "пауза"]

## Сколько кадров ждать между шагами. Ввод через parse_input_event разбирается
## не в тот же кадр, а мир роли строится почти секунду.
const SETTLE := 4
const WARMUP := 90

var _app: Node
var _step := 0
var _wait := WARMUP
var _fails := 0

func _initialize() -> void:
	_app = load("res://scenes/app.tscn").instantiate()
	root.add_child(_app)

func _process(_delta: float) -> bool:
	if _wait > 0:
		_wait -= 1
		return false
	_wait = SETTLE
	_step += 1
	match _step:
		1:
			_expect("до нажатий", 2, "в мире")   # GAME
			_press(KEY_ESCAPE)
		2:
			_expect("Esc из игры", 3, "пауза")   # PAUSE
			_press(KEY_ESCAPE)
		3:
			_expect("Esc из паузы", 2, "снова в мире")
			_press(KEY_TAB)
		4:
			_expect_schema("Tab в роли ДСП", true)
			_press(KEY_TAB)
		5:
			_expect_schema("Tab обратно", false)
		_:
			if _fails == 0:
				print("SHELL PROBE OK: переходы действуют")
				quit(0)
			else:
				printerr("SHELL PROBE FAIL: несовпадений %d" % _fails)
				quit(1)
			return true
	return false

func _expect(what: String, want: int, human: String) -> void:
	var got: int = _app.get("_state")
	var ok := got == want
	if not ok:
		_fails += 1
	print("%s %-16s -> %s (ждали: %s)" % [
		"OK  " if ok else "FAIL", what, _name_of(got), human])

func _expect_schema(what: String, want: bool) -> void:
	var got: bool = _app.get("_schematic_on")
	var ok := got == want
	if not ok:
		_fails += 1
	print("%s %-16s -> схема %s (ждали: %s)" % [
		"OK  " if ok else "FAIL", what,
		"включена" if got else "выключена",
		"включена" if want else "выключена"])

func _name_of(state: int) -> String:
	if state < 0 or state >= STATE_NAMES.size():
		return "неизвестно (%d)" % state
	return STATE_NAMES[state]

## Нажатие подаётся ЦЕЛИКОМ — нажали и отпустили. Оболочка слушает только
## pressed, но незакрытое нажатие оставляет клавишу «зажатой» для всего
## остального ввода, и следующий шаг зонда пришёл бы в изменённый мир.
func _press(key: Key) -> void:
	var down := InputEventKey.new()
	down.keycode = key
	down.physical_keycode = key
	down.pressed = true
	Input.parse_input_event(down)
	var up := InputEventKey.new()
	up.keycode = key
	up.physical_keycode = key
	up.pressed = false
	Input.parse_input_event(up)
