extends Node2D

# ClearAhead — эскиз B0, Godot-половина.
# Та же станция, те же координаты, тот же цикл, что и в sketch/svg/station.html.
# Смысл файла — не «как надо писать на Godot», а «каково это писать».

# ── геометрия: координаты набиты руками ────────────────────────────────────
const Y1 := 200.0
const Y2 := 252.0
const Y3 := 304.0
const Y4 := 356.0
const YT := 444.0
const YS := 512.0

const C_BALLAST := Color("3a3733")
const C_SLEEPER := Color("4c473f")
const C_RAIL    := Color("c9c4b6")
const C_BUSY    := Color("4d2d27")
const C_ROUTE   := Color("2f4433")
const C_LOCO    := Color("d9a13a")
const C_LOCO_B  := Color("8a6520")
const C_CAR     := Color("79848d")
const C_CAR_B   := Color("4e565c")
const C_TEXT    := Color("cfd3d6")
const C_MUTED   := Color("8d949a")
const C_DIM     := Color("6f767c")
const C_RED     := Color("d9503a")
const C_GREEN   := Color("56a86b")
const C_AMBER   := Color("d9a13a")
const C_DARKRED := Color("7b2f24")

const CYCLE := 900.0
const T0 := 8 * 3600 + 35 * 60

var tracks := []          # {id, poly, cum, len, occ}
var route_a := {}
var route_b := {}
var train_a := []         # [{off, loco}]
var train_b := []

var model := 0.0
var rate := 15.0
var running := true
var font: Font

var clock_label: Label
var pause_btn: Button
var rate_btns := []
var slack_cells := []
var state_cells := []

var jobs := [
	{"n": 1, "what": "Подача 8 полувагонов",   "res": "лок. ЧМЭ3-401, 4 путь, подъездной", "due": 90,  "t0": 90,  "t1": 400},
	{"n": 2, "what": "Расстановка у фронта",   "res": "подъездной путь",                   "due": 560, "t0": 400, "t1": 560},
	{"n": 3, "what": "Уборка порожних",        "res": "лок. ЧМЭ3-401, 4 путь",             "due": 900, "t0": 560, "t1": 860},
	{"n": 4, "what": "Приём пригородного",     "res": "1 путь, зап. горловина, 2 путь",    "due": 250, "t0": 30,  "t1": 240},
	{"n": 5, "what": "Стоянка 2 мин",          "res": "2 путь",                            "due": 365, "t0": 240, "t1": 360},
	{"n": 6, "what": "Отправление 6412",       "res": "2 путь, вост. горловина",           "due": 545, "t0": 360, "t1": 560},
	{"n": 7, "what": "Формирование грузового", "res": "3 путь, вытяжка",                   "due": 780, "t0": 600, "t1": 800},
	{"n": 8, "what": "Отправление грузового",  "res": "вост. горловина, 1 путь",           "due": 880, "t0": 800, "t1": 900},
]

var signals_def := [
	{"x": 150.0,  "y": Y1 - 20.0, "id": "Ч"},
	{"x": 1480.0, "y": Y2 - 20.0, "id": "Н2"},
	{"x": 800.0,  "y": YT - 20.0, "id": "М3"},
	{"x": 1150.0, "y": YS - 20.0, "id": "М7"},
]
var signal_colors := [C_RED, C_RED, C_DARKRED, C_DARKRED]


func _ready() -> void:
	font = ThemeDB.fallback_font

	# схема занимает левые ~1360 px, справа панель
	scale = Vector2(0.755, 0.755)
	position = Vector2(0.0, 30.0)

	var raw := [
		["1", [Vector2(90, Y1), Vector2(1780, Y1)]],
		["2", [Vector2(300, Y1), Vector2(470, Y2), Vector2(1420, Y2), Vector2(1590, Y1)]],
		["3", [Vector2(480, Y2), Vector2(650, Y3), Vector2(1250, Y3), Vector2(1420, Y2)]],
		["4", [Vector2(660, Y3), Vector2(830, Y4), Vector2(1080, Y4), Vector2(1250, Y3)]],
		["T", [Vector2(860, Y4), Vector2(620, YT), Vector2(400, YT)]],
		["P", [Vector2(1050, Y4), Vector2(1480, YS), Vector2(1740, YS)]],
	]
	for r in raw:
		var m := measure(round_poly(r[1]))
		m["id"] = r[0]
		m["occ"] = C_BALLAST
		m["sleepers"] = build_sleepers(m)   # в SVG это один пунктирный штрих
		tracks.append(m)

	route_a = measure(round_poly([
		Vector2(90, Y1), Vector2(300, Y1), Vector2(470, Y2),
		Vector2(1420, Y2), Vector2(1590, Y1), Vector2(1780, Y1)]))
	route_b = measure(round_poly([
		Vector2(400, YT), Vector2(620, YT), Vector2(860, Y4),
		Vector2(1050, Y4), Vector2(1480, YS), Vector2(1740, YS)]))

	train_a = consist(4)
	train_b = consist(9)
	build_ui()


func consist(n: int) -> Array:
	var out := []
	for i in n:
		out.append({"off": i * 32.0, "loco": i == 0})
	return out


# ── скругление ломаной: углы превращаются в стрелочные кривые ──────────────
func round_poly(pts: Array, r := 46.0, steps := 14) -> PackedVector2Array:
	var out := PackedVector2Array([pts[0]])
	for i in range(1, pts.size() - 1):
		var p: Vector2 = pts[i - 1]
		var v: Vector2 = pts[i]
		var n: Vector2 = pts[i + 1]
		var d1 := v.distance_to(p)
		var d2 := n.distance_to(v)
		var rr: float = min(r, d1 / 2.0, d2 / 2.0)
		var a := v - (v - p).normalized() * rr
		var b := v + (n - v).normalized() * rr
		for k in range(steps + 1):
			var t := float(k) / steps
			out.append(a.lerp(v, t).lerp(v.lerp(b, t), t))
	out.append(pts[pts.size() - 1])
	return out


# ── интерполяция вдоль ломаной ─────────────────────────────────────────────
func measure(poly: PackedVector2Array) -> Dictionary:
	var cum := PackedFloat32Array([0.0])
	for i in range(1, poly.size()):
		cum.append(cum[i - 1] + poly[i].distance_to(poly[i - 1]))
	return {"poly": poly, "cum": cum, "len": cum[cum.size() - 1]}


func at(m: Dictionary, s: float) -> Dictionary:
	var cum: PackedFloat32Array = m["cum"]
	var poly: PackedVector2Array = m["poly"]
	var total: float = m["len"]
	s = clampf(s, 0.0, total)
	var i := 1
	while i < cum.size() - 1 and cum[i] < s:
		i += 1
	var span: float = cum[i] - cum[i - 1]
	var t: float = 0.0 if span <= 0.0 else (s - cum[i - 1]) / span
	var a: Vector2 = poly[i - 1]
	var b: Vector2 = poly[i]
	return {"pos": a.lerp(b, t), "ang": (b - a).angle()}


# ── движение: кусочно-линейное по модельному времени ───────────────────────
func leg(t: float, legs: Array) -> float:
	for L in legs:
		if t < L[0]:
			return L[2]
		if t <= L[1]:
			return L[2] + (L[3] - L[2]) * (t - L[0]) / (L[1] - L[0])
	return legs[legs.size() - 1][3]


func legs_a() -> Array:
	var stop: float = route_a["len"] * 0.62
	return [[30.0, 240.0, 0.0, stop],
			[240.0, 360.0, stop, stop],
			[360.0, 560.0, stop, route_a["len"] + 200.0]]


func legs_b() -> Array:
	var e: float = route_b["len"]
	return [[0.0, 90.0, 300.0, 300.0],
			[90.0, 400.0, 300.0, e],
			[400.0, 560.0, e, e],
			[560.0, 860.0, e, 300.0]]


func _process(delta: float) -> void:
	if running:
		model = fmod(model + delta * rate, CYCLE)
	update_occupancy(model)
	update_ui(model)
	queue_redraw()


# ── отрисовка ──────────────────────────────────────────────────────────────
func _draw() -> void:
	for t in tracks:
		draw_polyline(t["poly"], t["occ"], 26.0, true)
	for t in tracks:
		draw_multiline(t["sleepers"], C_SLEEPER, 2.5)
	for t in tracks:
		draw_polyline(t["poly"], C_RAIL, 9.0, true)
	for t in tracks:
		draw_polyline(t["poly"], t["occ"], 5.5, true)

	# упоры
	draw_line(Vector2(396, YT - 13), Vector2(396, YT + 13), C_RAIL, 4.0)
	draw_line(Vector2(1744, YS - 13), Vector2(1744, YS + 13), C_RAIL, 4.0)

	# предприятие
	draw_rect(Rect2(1570, YS + 28, 170, 42), Color("24282b"))
	draw_rect(Rect2(1570, YS + 28, 170, 42), Color("3a4044"), false, 1.0)
	draw_string(font, Vector2(1590, YS + 55), "предприятие",
		HORIZONTAL_ALIGNMENT_LEFT, -1, 15, C_MUTED)

	# подписи
	for l in [[110.0, Y1, "1 путь"], [490.0, Y2, "2 путь"],
			  [670.0, Y3, "3 путь"], [850.0, Y4, "4 путь"]]:
		draw_string(font, Vector2(l[0], l[1] - 20.0), l[2],
			HORIZONTAL_ALIGNMENT_LEFT, -1, 16, C_MUTED)
	draw_string(font, Vector2(420, YT + 32), "тупик отстоя",
		HORIZONTAL_ALIGNMENT_LEFT, -1, 15, C_DIM)
	draw_string(font, Vector2(1200, YS + 26), "подъездной путь",
		HORIZONTAL_ALIGNMENT_LEFT, -1, 15, C_DIM)

	# сигналы
	for i in signals_def.size():
		var s: Dictionary = signals_def[i]
		var p := Vector2(s["x"], s["y"])
		draw_line(p, p + Vector2(0, 13), C_DIM, 2.0)
		draw_circle(p, 5.0, signal_colors[i])
		draw_string(font, p + Vector2(9, 5), s["id"],
			HORIZONTAL_ALIGNMENT_LEFT, -1, 13, C_DIM)

	draw_consist(train_a, route_a, leg(model, legs_a()))
	draw_consist(train_b, route_b, leg(model, legs_b()))


func build_sleepers(t: Dictionary) -> PackedVector2Array:
	var out := PackedVector2Array()
	var total: float = t["len"]
	var s := 5.0
	while s < total:
		var q := at(t, s)
		var n := Vector2(-sin(q["ang"]), cos(q["ang"])) * 10.5
		out.append(q["pos"] - n)
		out.append(q["pos"] + n)
		s += 9.0
	return out


func draw_consist(cars: Array, m: Dictionary, head: float) -> void:
	for c in cars:
		var s: float = head - c["off"]
		if s < 0.0 or s > m["len"]:
			continue
		var q := at(m, s)
		var w: float = 30.0 if c["loco"] else 26.0
		draw_set_transform(q["pos"], q["ang"], Vector2.ONE)
		draw_rect(Rect2(-w / 2.0, -7.5, w, 15.0),
			C_LOCO if c["loco"] else C_CAR)
		draw_rect(Rect2(-w / 2.0, -7.5, w, 15.0),
			C_LOCO_B if c["loco"] else C_CAR_B, false, 1.5)
		draw_set_transform(Vector2.ZERO, 0.0, Vector2.ONE)


func update_occupancy(t: float) -> void:
	var st := {
		"1": C_ROUTE if (t > 10 and t < 60) or (t > 500 and t < 570) else C_BALLAST,
		"2": C_BUSY if t > 55 and t < 560 else C_BALLAST,
		"3": C_BUSY if t > 600 and t < 900 else C_BALLAST,
		"4": C_BUSY if (t > 60 and t < 200) or (t > 560 and t < 860) else C_BALLAST,
		"T": C_BUSY if t < 120 or t > 820 else C_BALLAST,
		"P": C_BUSY if t > 200 else C_BALLAST,
	}
	for tr in tracks:
		tr["occ"] = st[tr["id"]]
	signal_colors[0] = C_GREEN if t > 10 and t < 60 else C_RED
	signal_colors[1] = C_GREEN if t > 340 and t < 400 else C_RED
	signal_colors[2] = C_AMBER if t > 80 and t < 130 else C_DARKRED
	signal_colors[3] = C_AMBER if t > 150 and t < 420 else C_DARKRED


# ── интерфейс ──────────────────────────────────────────────────────────────
func mklabel(text: String, size: int, color: Color, align := HORIZONTAL_ALIGNMENT_LEFT) -> Label:
	var l := Label.new()
	l.text = text
	l.add_theme_font_size_override("font_size", size)
	l.add_theme_color_override("font_color", color)
	l.horizontal_alignment = align
	return l


func build_ui() -> void:
	var layer := CanvasLayer.new()
	add_child(layer)

	var top := HBoxContainer.new()
	top.position = Vector2(16, 10)
	top.add_theme_constant_override("separation", 12)
	layer.add_child(top)
	top.add_child(mklabel("ClearAhead", 14, C_TEXT))
	top.add_child(mklabel("эскиз B0 · Godot · станция Приречная", 14, C_DIM))

	pause_btn = Button.new()
	pause_btn.text = "пауза"
	pause_btn.pressed.connect(_on_pause)
	top.add_child(pause_btn)
	for r in [15.0, 60.0]:
		var b := Button.new()
		b.text = "×%d" % int(r)
		b.toggle_mode = true
		b.button_pressed = r == rate
		b.pressed.connect(_on_rate.bind(r))
		top.add_child(b)
		rate_btns.append({"btn": b, "rate": r})

	clock_label = mklabel("08:35:00", 22, C_TEXT)
	clock_label.position = Vector2(1180, 8)
	layer.add_child(clock_label)

	# правая панель — таблица заданий
	var panel := PanelContainer.new()
	panel.position = Vector2(1360, 44)
	panel.custom_minimum_size = Vector2(440, 716)
	panel.size = Vector2(440, 716)
	layer.add_child(panel)

	var box := VBoxContainer.new()
	box.add_theme_constant_override("separation", 8)
	panel.add_child(box)
	box.add_child(mklabel("ЗАДАНИЯ И РЕСУРСЫ", 12, C_DIM))

	var grid := GridContainer.new()
	grid.columns = 6
	grid.add_theme_constant_override("h_separation", 10)
	grid.add_theme_constant_override("v_separation", 6)
	box.add_child(grid)
	for h in ["#", "Что", "Ресурс", "Срок", "Запас", "Состояние"]:
		grid.add_child(mklabel(h, 12, C_DIM))
	for j in jobs:
		grid.add_child(mklabel(str(j["n"]), 12, C_DIM))
		grid.add_child(mklabel(j["what"], 12, C_TEXT))
		grid.add_child(mklabel(j["res"], 12, C_DIM))
		grid.add_child(mklabel(hhmm(T0 + j["due"]), 12, C_TEXT, HORIZONTAL_ALIGNMENT_RIGHT))
		var slack := mklabel("", 12, C_TEXT, HORIZONTAL_ALIGNMENT_RIGHT)
		var state := mklabel("", 12, C_TEXT)
		grid.add_child(slack)
		grid.add_child(state)
		slack_cells.append(slack)
		state_cells.append(state)


func _on_pause() -> void:
	running = not running
	pause_btn.text = "пауза" if running else "пуск"


func _on_rate(r: float) -> void:
	rate = r
	for e in rate_btns:
		e["btn"].button_pressed = e["rate"] == r


func hhmm(s: int) -> String:
	return "%02d:%02d" % [(s / 3600) % 24, (s / 60) % 60]


func hhmmss(s: float) -> String:
	var i := int(round(s))
	return "%02d:%02d:%02d" % [(i / 3600) % 24, (i / 60) % 60, i % 60]


func mmss(s: float) -> String:
	var i := int(abs(round(s)))
	return ("−" if s < 0 else "+") + "%02d:%02d" % [i / 60, i % 60]


func update_ui(t: float) -> void:
	clock_label.text = hhmmss(T0 + t)
	for i in jobs.size():
		var j: Dictionary = jobs[i]
		var done: bool = t >= j["t1"]
		var slack: float = (j["due"] - j["t1"]) if done else (j["due"] - max(t, float(j["t1"])))
		slack_cells[i].text = mmss(slack)
		slack_cells[i].add_theme_color_override("font_color",
			C_RED if slack < 0 else (C_AMBER if slack < 120 else C_TEXT))
		if t < j["t0"]:
			state_cells[i].text = "ожидает"
			state_cells[i].add_theme_color_override("font_color", C_DIM)
		elif not done:
			state_cells[i].text = "выполняется"
			state_cells[i].add_theme_color_override("font_color", C_TEXT)
		else:
			state_cells[i].text = "выполнено"
			state_cells[i].add_theme_color_override("font_color", C_GREEN)
