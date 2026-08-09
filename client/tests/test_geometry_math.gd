extends SceneTree
## Тесты чистой геометрии (координаты сервера, Y вверх). Включая дуги с
## ЗНАЧЕНИЯМИ ИЗ ЭТАЛОНА: конец дуги ST_A_SW_1:diverging обязан совпасть с
## стартовой позой ST_A_E_W12 из contract/render_geometry.golden.json — это
## проверяет формулу дуги и знак угла против данных настоящего сервера.
## Запуск:
##   godot --headless --path client --script tests/test_geometry_math.gd

const GM := preload("res://scripts/geometry_math.gd")

const HALF_PI := 1.5707963267948966
const QUARTER_LEN := 157.07963267948966  # R=100, четверть круга

var _total := 0
var _failures := 0

func _initialize() -> void:
	_run()
	if _failures == 0:
		print("MATH TESTS OK: %d проверок" % _total)
		quit(0)
	else:
		printerr("MATH TESTS FAIL: %d из %d не прошло" % [_failures, _total])
		quit(1)

func _run() -> void:
	# --- straight ---
	var start := {"plan": {"x": 0.0, "y": 0.0, "heading": 0.0}}
	var prims: Array[Dictionary] = [{"kind": "straight", "length": 100.0}]
	var pts := GM.sample_chain(start, prims)
	_check(pts.size() == 2, "straight: две точки (нормали постоянны)", pts.size())
	_approx(pts[1], Vector2(100, 0), "straight вдоль +X")

	start = {"plan": {"x": 0.0, "y": 0.0, "heading": HALF_PI}}
	prims = [{"kind": "straight", "length": 100.0}]
	pts = GM.sample_chain(start, prims)
	_approx(pts[1], Vector2(0, 100), "straight на север (heading +90°)")

	# --- дуга влево: четверть круга R=100 из (0,0,h=0) -> (100,100) ---
	start = {"plan": {"x": 0.0, "y": 0.0, "heading": 0.0}}
	prims = [{"kind": "arc", "length": QUARTER_LEN, "radius": 100.0, "angle": HALF_PI}]
	pts = GM.sample_chain(start, prims)
	_approx(pts[pts.size() - 1], Vector2(100, 100), "дуга влево на четверть круга")

	# --- дуга вправо -> (100,-100) ---
	prims = [{"kind": "arc", "length": QUARTER_LEN, "radius": 100.0, "angle": -HALF_PI}]
	pts = GM.sample_chain(start, prims)
	_approx(pts[pts.size() - 1], Vector2(100, -100), "дуга вправо на четверть круга")

	# --- направление в конце дуги: касательная (0,1) для левой четверти ---
	# Хорда последнего шага тесселяции касательной НЕ равна: она отстаёт ровно
	# на половину углового шага (при R=100 и шаге 0.5 м это 0.0025 рад — и
	# именно столько показывала эта проверка, когда мерила хорду). Накопленный
	# heading читается прямой, подставленной следом: её единственный отрезок
	# рисуется точно по нему.
	prims = [
		{"kind": "arc", "length": QUARTER_LEN, "radius": 100.0, "angle": HALF_PI},
		{"kind": "straight", "length": 10.0},
	]
	pts = GM.sample_chain(start, prims)
	var last_dir := (pts[pts.size() - 1] - pts[pts.size() - 2]).normalized()
	_approx(last_dir, Vector2(0, 1), "заголовок после левой четверти = 90° (на север)")

	# --- цепочка straight + arc продолжается от конца предыдущего ---
	prims = [
		{"kind": "straight", "length": 50.0},
		{"kind": "arc", "length": QUARTER_LEN, "radius": 100.0, "angle": HALF_PI},
	]
	pts = GM.sample_chain(start, prims)
	_approx(pts[pts.size() - 1], Vector2(150, 100), "straight 50 + дуга: конец (150,100)")

	# --- ЗНАЧЕНИЕ ИЗ ЭТАЛОНА: конец SW_1:diverging == старт W12 ---
	# SW_1:diverging: (300,0,h=0), дуга R=300, L=33.21, angle=-0.1107.
	# W12: старт (333.14221294597223, -1.8362971100542635, -0.1107).
	start = {"plan": {"x": 300.0, "y": 0.0, "heading": 0.0}}
	prims = [{"kind": "arc", "length": 33.21, "radius": 300.0, "angle": -0.1107}]
	pts = GM.sample_chain(start, prims)
	_approx(pts[pts.size() - 1], Vector2(333.14221294597223, -1.8362971100542635),
		"дуга SW_1:diverging попадает в старт W12 (эталон)")

	# --- normals / offset / polygon ---
	var line := PackedVector2Array([Vector2(0, 0), Vector2(10, 0)])
	var ns := GM.normals(line)
	_approx(ns[0], Vector2(0, 1), "нормаль в начале прямой — влево от движения")
	_approx(ns[1], Vector2(0, 1), "нормаль в конце прямой")

	var off := GM.offset_polyline(line, 2.0)
	_approx(off[0], Vector2(0, 2), "смещение влево на 2 м")
	_approx(off[1], Vector2(10, 2), "смещение влево на 2 м (конец)")

	var poly := GM.offset_polygon(line, 1.5)
	_check(poly.size() == 4, "полигон балласта: 4 угла", poly.size())
	_approx(poly[0], Vector2(0, 1.5), "левая кромка начало")
	_approx(poly[2], Vector2(10, -1.5), "правая кромка конец")

	# --- resample_uniform ---
	var resampled := GM.resample_uniform(line, 0.6)
	_check(resampled.size() == 18, "ресемпл 10 м шагом 0.6: 18 точек", resampled.size())
	_approx(resampled[0], Vector2(0, 0), "ресемпл: первая точка")
	_approx(resampled[1], Vector2(0.6, 0), "ресемпл: шаг 0.6")
	_approx(resampled[resampled.size() - 1], Vector2(10, 0), "ресемпл: точный конец")

	# --- переворот только в одной функции ---
	_approx(GM.server_to_godot(Vector2(3, -7)), Vector2(3, 7), "server_to_godot переворачивает y")
	var sr := Rect2(Vector2(10, 20), Vector2(50, 60))
	var gr := GM.server_rect_to_godot(sr)
	_check(gr.position == Vector2(10, -80), "server_rect: верх сервера -> верх экрана", gr.position)
	_check(gr.size == Vector2(50, 60), "server_rect: размер не меняется", gr.size)

func _approx(a: Vector2, b: Vector2, what: String) -> void:
	_total += 1
	if (a - b).length() < 1e-6:
		return
	_failures += 1
	printerr("FAIL [%s]: %s != %s (delta %s)" % [what, a, b, (a - b).length()])

func _check(cond: bool, what: String, got: Variant) -> void:
	_total += 1
	if cond:
		return
	_failures += 1
	printerr("FAIL [%s]: получено %s" % [what, got])
