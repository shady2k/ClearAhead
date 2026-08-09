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

	# --- frog: марка ---
	var mark := Turnout.parse_frog_mark("1/9")
	_check(mark.ok, "марка 1/9 разбирается", mark)
	_approx_f(mark.value, atan(1.0 / 9.0), "1/9 -> atan(1/9)")
	mark = Turnout.parse_frog_mark(" 1 / 9 ")
	_check(mark.ok and absf(mark.value - atan(1.0 / 9.0)) < 1e-9, "пробелы вокруг дроби допустимы", mark)
	mark = Turnout.parse_frog_mark("1/12")
	_check(mark.ok and absf(mark.value - atan(1.0 / 12.0)) < 1e-9, "1/12 -> atan(1/12)", mark)
	for bad in ["1/0", "0/9", "1.5/9", "9", "1/", "/9", "abc", "", "1\\9"]:
		var r := Turnout.parse_frog_mark(bad)
		_check(not r.ok, "неразбираемая марка «%s» — ошибка" % bad, r)

	# --- frog: точка расхождения из эталона SW_1 (toe 300,0, hand=right) ---
	start = {"plan": {"x": 300.0, "y": 0.0, "heading": 0.0}}
	var straight_pts := PackedVector2Array([Vector2(300, 0), Vector2(333.5, 0)])
	prims = [{"kind": "arc", "length": 33.21, "radius": 300.0, "angle": -0.1107}]
	var div_pts := Trackside.sample_range(start, prims, 0.0, 33.21)
	var frog := Turnout._frog_point(straight_pts, div_pts, -1.0, 1.435, 0.7175)
	_check(frog.ok, "точка крестовины найдена", frog)
	if frog.ok:
		# крестовина — точка ВСТРЕЧИ ниток: на южном рамном рельсе прямой ветви
		_approx_f(frog.point.y, -0.7175, "крестовина SW_1: y на рамном рельсе")
		_approx_f(frog.point.x, 329.38, "крестовина SW_1: x в точке расхождения (s*~29.3)")

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
