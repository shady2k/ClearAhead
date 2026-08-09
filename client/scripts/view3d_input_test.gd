extends SceneTree
## Доказательство интерактивной камеры синтетическими событиями через
## Input.parse_input_event — тот же путь, что и реальный ввод, до
## _unhandled_input. Снимки после каждого этапа показывают, что камера
## реально отреагировала:
##
##   DISPLAY=:1 godot --path client --script res://scripts/view3d_input_test.gd \
##       -- --out /tmp/v3d_it [--frames N]
##
## Пишет: <out>_1_baseline.png (горловина, стартовая конфигурация),
## <out>_2_orbited.png (после орбиты ЛКМ), <out>_3_zoomed.png (после зума
## колесом), <out>_4_panned.png (после панорамы СКМ), <out>_5_persp.png
## (после переключения в перспективу клавишей P).

const Sketch := preload("res://scripts/sketch3d.gd")

var _out := "/tmp/v3d_it"
var _frames := 10
var _elapsed := 0
var _stage := 0
var _pump := 0            # сколько событий ещё подать (счётчик вниз)
var _pump_total := 0      # всего событий в текущей серии (для номера шага)
var _pump_step := Vector2.ZERO
var _pump_mask := 0

func _initialize() -> void:
	_out = _arg("--out", _out)
	_frames = int(_arg("--frames", "10"))
	# стартовая конфигурация — горловина (SW1 x=120, SW2 x=173), как в брифе
	Sketch.shot_focus = Vector2(160.0, -3.5)
	Sketch.shot_size = 46.0
	Sketch.shot_azimuth = 45.0
	Sketch.shot_elev = 35.0
	root.add_child((load("res://scenes/sketch3d.tscn") as PackedScene).instantiate())

func _process(_delta: float) -> bool:
	if _pump > 0:
		_pump -= 1
		_pump_event(_pump_total - _pump)   # номер шага по возрастанию: 1..total
		return false
	_elapsed += 1
	if _elapsed < _frames:
		return false
	match _stage:
		0:
			_save("%s_1_baseline.png" % _out)
			_start_drag(Vector2(50.0, -30.0), MOUSE_BUTTON_MASK_LEFT, 12)
		1:
			_save("%s_2_orbited.png" % _out)
			_start_wheel(3)   # три «щелчка» колесом вверх = зум внутрь
		2:
			_save("%s_3_zoomed.png" % _out)
			_start_drag(Vector2(-160.0, 90.0), MOUSE_BUTTON_MASK_MIDDLE, 14)
		3:
			_save("%s_4_panned.png" % _out)
			_start_key(KEY_P)
		4:
			_save("%s_5_persp.png" % _out)
			print("VIEW3D INPUT TEST OK: орбита/зум/панорама/перспектива отработали")
			quit(0)
			return true
	_stage += 1
	_elapsed = 0
	return false

func _save(path: String) -> void:
	var img := root.get_texture().get_image()
	if img == null:
		printerr("V3DIT FAIL: окно не дало изображения на этапе %d" % _stage)
		quit(1)
		return
	var err := img.save_png(path)
	if err != OK:
		printerr("V3DIT FAIL: снимок не записан в %s (ошибка %d)" % [path, err])
		quit(1)
		return
	print("V3DIT OK: %s, %dx%d (этап %d)" % [path, img.get_width(), img.get_height(), _stage])

## --- синтетический ввод ---
func _start_drag(rel: Vector2, mask: int, steps: int) -> void:
	_pump = steps
	_pump_total = steps
	_pump_step = rel / float(steps)
	_pump_mask = mask

func _start_wheel(notches: int) -> void:
	# на каждый «щелчок» — press+release WHEEL_UP
	_pump = notches * 2
	_pump_total = notches * 2
	_pump_step = Vector2.ZERO
	_pump_mask = 0xFEED  # признак колеса

func _start_key(key: Key) -> void:
	_pump = 2
	_pump_total = 2
	_pump_step = Vector2.ZERO
	_pump_mask = int(key)

func _pump_event(step: int) -> void:
	if _pump_mask == MOUSE_BUTTON_MASK_LEFT or _pump_mask == MOUSE_BUTTON_MASK_MIDDLE \
			or _pump_mask == MOUSE_BUTTON_MASK_RIGHT:
		var e := InputEventMouseMotion.new()
		e.button_mask = _pump_mask
		e.relative = _pump_step
		e.position = Vector2(640.0, 360.0)
		Input.parse_input_event(e)
		return
	if _pump_mask == 0xFEED:
		var e := InputEventMouseButton.new()
		e.button_index = MOUSE_BUTTON_WHEEL_UP
		e.pressed = (step % 2) == 1
		e.position = Vector2(640.0, 360.0)
		Input.parse_input_event(e)
		return
	# клавиша: press, затем release
	var k := InputEventKey.new()
	k.keycode = _pump_mask as Key
	k.pressed = step == 1
	Input.parse_input_event(k)

func _arg(name: String, fallback: String) -> String:
	var args := OS.get_cmdline_user_args()
	var i := args.find(name)
	if i >= 0 and i + 1 < args.size():
		return args[i + 1]
	return fallback
