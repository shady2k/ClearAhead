extends SceneTree
## Тесты чистых помощников отрисовки: sample_range (спаны платформ в u) и
## марка крестовины (frog). Запуск:
##   godot --headless --path client --script tests/test_draw_helpers.gd
const Trackside := preload("res://scripts/trackside_layer.gd")
const Turnout := preload("res://scripts/turnout_layer.gd")

const HALF_PI := 1.5707963267948966
const QUARTER_LEN := 157.07963267948966  # R=100, четверть круга

var _total := 0
var _failures := 0

func _initialize() -> void:
	_run()
	if _failures == 0:
		print("DRAW HELPER TESTS OK: %d проверок" % _total)
		quit(0)
	else:
		printerr("DRAW HELPER TESTS FAIL: %d из %d не прошло" % [_failures, _total])
		quit(1)

func _run() -> void:
	# --- sample_range: прямая ---
	var start := {"plan": {"x": 0.0, "y": 0.0, "heading": 0.0}}
	var prims: Array[Dictionary] = [{"kind": "straight", "length": 100.0}]
	var pts := Trackside.sample_range(start, prims, 10.0, 30.0)
	_check(pts.size() == 2, "прямая [10,30]: две точки", pts.size())
	_approx(pts[0], Vector2(10, 0), "прямая: начало интервала")
	_approx(pts[1], Vector2(30, 0), "прямая: конец интервала")

	pts = Trackside.sample_range(start, prims, 0.0, 100.0)
	_approx(pts[0], Vector2(0, 0), "прямая от 0: начало")
	_approx(pts[pts.size() - 1], Vector2(100, 0), "прямая до конца")

	# --- вырожденные интервалы ---
	_check(Trackside.sample_range(start, prims, 30.0, 10.0).is_empty(), "to < from: пусто", "")
	_check(Trackside.sample_range(start, prims, 0.0, 0.0).is_empty(), "from == to: пусто", "")
	_check(Trackside.sample_range(start, prims, 150.0, 200.0).is_empty(), "интервал за концом: пусто", "")
	_check(Trackside.sample_range(start, prims, 0.0, -5.0).is_empty(), "to <= 0: пусто", "")

	# --- стык примитивов: дуга после прямой, интервал пересекает границу ---
	prims = [
		{"kind": "straight", "length": 50.0},
		{"kind": "arc", "length": QUARTER_LEN, "radius": 100.0, "angle": HALF_PI},
	]
	pts = Trackside.sample_range(start, prims, 25.0, 50.0 + QUARTER_LEN)
	_approx(pts[0], Vector2(25, 0), "стык: начало до границы")
	_approx(pts[pts.size() - 1], Vector2(150, 100), "стык: конец после дуги (четверть круга)")
	for i in range(1, pts.size()):
		_check(pts[i].distance_to(pts[i - 1]) > 1e-6, "стык: нет дублей точек (i=%d)" % i, pts[i])

	# --- дуга из эталона: SW_1:diverging [0, конец] == старт W12 ---
	start = {"plan": {"x": 300.0, "y": 0.0, "heading": 0.0}}
	prims = [{"kind": "arc", "length": 33.21, "radius": 300.0, "angle": -0.1107}]
	pts = Trackside.sample_range(start, prims, 0.0, 33.21)
	_approx(pts[pts.size() - 1], Vector2(333.14221294597223, -1.8362971100542635),
		"дуга из эталона попадает в старт W12")

	# --- frog: крылья из особенности контракта (спека §5) ---
	var feature := {
		"owner": "SW_1",
		"kind": "frog",
		"point": {"x": 329.34, "y": -0.7175},
		"addresses": [
			{"element": "SW_1:straight", "u": 29.34, "tangent": {"x": 1.0, "y": 0.0}},
			{"element": "SW_1:diverging", "u": 29.32, "tangent": {"x": 0.995, "y": -0.110}},
		],
	}
	var wings := Turnout.frog_wings(feature, 1.5)
	_check(wings.size() == 2, "frog_wings: два крыла", wings.size())
	_approx(wings[0][0], Vector2(329.34, -0.7175), "frog: крыло 1 начинается в point")
	_approx(wings[0][1], Vector2(329.34 + 1.5, -0.7175), "frog: крыло 1 по касательной прямого прохода")
	_approx(wings[1][1], Vector2(329.34 + 1.5 * 0.995, -0.7175 + 1.5 * (-0.110)),
		"frog: крыло 2 по касательной бокового прохода")
	# карта без особенности — рисование пропускается до frog_wings (is_empty
	# в _draw_frog); крылья строятся только из валидной особенности
	# точка из эталона SW_1: (329.3428, -0.7175); адреса — прямой, затем боковой
	var golden := {
		"owner": "ST_A_SW_1",
		"kind": "frog",
		"point": {"x": 329.3428015022417, "y": -0.7175},
		"addresses": [
			{"element": "ST_A_SW_1:straight", "u": 29.342801502241677, "tangent": {"x": 1.0, "y": 0.0}},
			{"element": "ST_A_SW_1:diverging", "u": 29.31944228015446, "tangent": {"x": 0.9952280796009604, "y": -0.09756897454695994}},
		],
	}
	var gwings := Turnout.frog_wings(golden, 1.5)
	_approx(gwings[0][0], Vector2(329.3428015022417, -0.7175), "frog: точка из эталона")
	_approx(gwings[0][1], Vector2(330.8428015022417, -0.7175), "frog: крыло прямого прохода по эталону")
	_approx(gwings[1][1], Vector2(329.3428015022417 + 1.5 * 0.9952280796009604, -0.7175 + 1.5 * (-0.09756897454695994)),
		"frog: крыло бокового прохода по эталону")

func _approx(a: Vector2, b: Vector2, what: String) -> void:
	_total += 1
	if (a - b).length() < 2e-2:
		return
	_failures += 1
	printerr("FAIL [%s]: %s != %s (delta %s)" % [what, a, b, (a - b).length()])

func _approx_f(a: float, b: float, what: String) -> void:
	_total += 1
	if absf(a - b) < 3e-2:
		return
	_failures += 1
	printerr("FAIL [%s]: %v != %v" % [what, a, b])

func _check(cond: bool, what: String, got: Variant) -> void:
	_total += 1
	if cond:
		return
	_failures += 1
	printerr("FAIL [%s]: получено %s" % [what, got])
