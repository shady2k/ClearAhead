extends SceneTree
## Тесты чистой геометрии (координаты сервера, Y вверх). Включая дуги с
## ЗНАЧЕНИЯМИ ИЗ ЭТАЛОНА: конец дуги SW1:diverging обязан совпасть со
## стартовой позой E_CROSS из contract/render_geometry.golden.json — это
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

	# --- ЗНАЧЕНИЕ ИЗ ЭТАЛОНА: конец SW1:diverging == старт E_CROSS ---
	# SW1:diverging: (120,0,h=0), дуга R=300, L=33.21, angle=-0.1107.
	# E_CROSS: старт (153.14221294597223, -1.8362971100542635, -0.1107).
	start = {"plan": {"x": 120.0, "y": 0.0, "heading": 0.0}}
	prims = [{"kind": "arc", "length": 33.21, "radius": 300.0, "angle": -0.1107}]
	pts = GM.sample_chain(start, prims)
	_approx(pts[pts.size() - 1], Vector2(153.14221294597223, -1.8362971100542635),
		"дуга SW1:diverging попадает в старт E_CROSS (эталон)")

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

	# --- pose_at: аналитическая поза (спека §4) ---
	start = {"plan": {"x": 10.0, "y": 20.0, "heading": 0.0}}
	prims = [{"kind": "straight", "length": 100.0}]
	var pose := GM.pose_at(start, prims, 30.0)
	_check(pose.ok, "pose_at: прямая внутри домена", pose)
	_approx(Vector2(pose.x, pose.y), Vector2(40, 20), "pose_at: точка на прямой")
	_approx_f(pose.heading, 0.0, "pose_at: заголовок прямой")
	pose = GM.pose_at(start, prims, 100.0)
	_approx(Vector2(pose.x, pose.y), Vector2(110, 20), "pose_at: u == конец цепочки")
	pose = GM.pose_at(start, prims, 150.0)
	_approx(Vector2(pose.x, pose.y), Vector2(110, 20), "pose_at: u за концом — последняя поза")
	pose = GM.pose_at(start, prims, -1.0)
	_check(not pose.ok, "pose_at: отрицательное u — ошибка", pose)

	# дуга: середина левой четверти R=100: s = половина дуги -> центральный
	# угол 45°, точка (R·sin45°, R·(1−cos45°)) = (70.71, 29.29) — НЕ середина
	# хорды (50,50); касательная повёрнута на 45°
	start = {"plan": {"x": 0.0, "y": 0.0, "heading": 0.0}}
	prims = [{"kind": "arc", "length": QUARTER_LEN, "radius": 100.0, "angle": HALF_PI}]
	pose = GM.pose_at(start, prims, QUARTER_LEN * 0.5)
	_approx(Vector2(pose.x, pose.y), Vector2(100.0 * sin(HALF_PI * 0.5), 100.0 * (1.0 - cos(HALF_PI * 0.5))),
		"pose_at: середина дуги (угол 45°)")
	_approx_f(pose.heading, HALF_PI * 0.5, "pose_at: касательная в середине дуги")
	# ЗНАЧЕНИЕ ИЗ ЭТАЛОНА: SW1:diverging в конце (u=33.21) == старт E_CROSS
	start = {"plan": {"x": 120.0, "y": 0.0, "heading": 0.0}}
	prims = [{"kind": "arc", "length": 33.21, "radius": 300.0, "angle": -0.1107}]
	pose = GM.pose_at(start, prims, 33.21)
	_approx(Vector2(pose.x, pose.y), Vector2(153.14221294597223, -1.8362971100542635),
		"pose_at: конец SW1:diverging попадает в старт E_CROSS (эталон)")
	# цепочка: прямой 50 + дуга — поза после границы
	start = {"plan": {"x": 0.0, "y": 0.0, "heading": 0.0}}
	prims = [
		{"kind": "straight", "length": 50.0},
		{"kind": "arc", "length": QUARTER_LEN, "radius": 100.0, "angle": HALF_PI},
	]
	pose = GM.pose_at(start, prims, 50.0 + QUARTER_LEN)
	_approx(Vector2(pose.x, pose.y), Vector2(150, 100), "pose_at: конец цепочки straight+дуга")
	# середина дуги цепочки: s=25 от начала дуги (R=100, полный угол 90°),
	# поворот 90°×25/157.08 = 28.65°; x = 50 + 100·sin(28.65°),
	# y = 100 − 100·cos(28.65°)
	pose = GM.pose_at(start, prims, 75.0)
	var mid_ang := HALF_PI * (25.0 / QUARTER_LEN)
	_approx(Vector2(pose.x, pose.y), Vector2(50 + 100.0 * sin(mid_ang), 100.0 - 100.0 * cos(mid_ang)),
		"pose_at: точка в середине дуги цепочки")

	# --- run_length / run_to_local (спека §4) ---
	var run := {
		"spans": [
			{"element": "A", "from": 5.0, "to": 25.0, "direction": "forward"},
			{"element": "B", "from": 0.0, "to": 40.0, "direction": "reverse"},
		],
	}
	_approx_f(GM.run_length(run), 60.0, "run_length: сумма спанов")
	var loc := GM.run_to_local(run, 0.0)
	_check(loc.ok and loc.element == "A" and absf(loc.u - 5.0) < 1e-9, "run_to_local: r=0 -> A.u=5", loc)
	loc = GM.run_to_local(run, 19.9)
	_check(loc.ok and loc.element == "A" and absf(loc.u - 24.9) < 1e-9, "run_to_local: forward u=from+(r-r0)", loc)
	loc = GM.run_to_local(run, 20.0)
	_check(loc.ok and loc.element == "B" and absf(loc.u - 40.0) < 1e-9, "run_to_local: граница спанов уходит следующему", loc)
	loc = GM.run_to_local(run, 50.0)
	_check(loc.ok and loc.element == "B" and absf(loc.u - 10.0) < 1e-9, "run_to_local: reverse u=to-(r-r0)", loc)
	loc = GM.run_to_local(run, 60.0)
	_check(not loc.ok, "run_to_local: r == run_length — вне (полуоткрыто)", loc)
	loc = GM.run_to_local(run, -0.1)
	_check(not loc.ok, "run_to_local: r < 0 — ошибка", loc)

	# --- run_sleeper_offsets: полуоткрытое правило (спека §4) ---
	var offs := GM.run_sleeper_offsets(0.0, 0.6, 47.941)
	_check(offs.size() == 80, "полуоткрытое: 47.941/0.6 -> 80 шпал (без конечной)", offs.size())
	_approx_f(offs[offs.size() - 1], 47.4, "полуоткрытое: последняя < длины")
	_check(not offs.has(47.941), "полуоткрытое: шпалы в конечной точке НЕТ", "")
	offs = GM.run_sleeper_offsets(0.15, 0.6, 3.0)
	_check(offs.size() == 5, "фаза: 0.15, 0.75, 1.35, 1.95, 2.55 -> 5 шпал", offs.size())
	_approx_f(offs[0], 0.15, "фаза: первый момент")
	offs = GM.run_sleeper_offsets(0.0, 0.0, 10.0)
	_check(offs.is_empty(), "шаг 0: пусто, не зацикливание", offs.size())
	offs = GM.run_sleeper_offsets(0.0, -0.6, 10.0)
	_check(offs.is_empty(), "шаг < 0: пусто", offs.size())
	offs = GM.run_sleeper_offsets(0.0, 0.6, 0.0)
	_check(offs.is_empty(), "длина 0: пусто", offs.size())

	# --- resample_uniform: неположительный шаг — отказ, не зацикливание ---
	_check(GM.resample_uniform(line, 0.0).is_empty(), "resample: шаг 0 -> пусто", "")
	_check(GM.resample_uniform(line, -0.6).is_empty(), "resample: шаг < 0 -> пусто", "")

func _approx(a: Vector2, b: Vector2, what: String) -> void:
	_total += 1
	if (a - b).length() < 1e-6:
		return
	_failures += 1
	printerr("FAIL [%s]: %s != %s (delta %s)" % [what, a, b, (a - b).length()])

func _approx_f(a: float, b: float, what: String) -> void:
	_total += 1
	if absf(a - b) < 1e-6:
		return
	_failures += 1
	printerr("FAIL [%s]: %f != %f" % [what, a, b])

func _check(cond: bool, what: String, got: Variant) -> void:
	_total += 1
	if cond:
		return
	_failures += 1
	printerr("FAIL [%s]: получено %s" % [what, got])
