extends SceneTree
## СТЕНД ПРЕДМЕТА — рисует ОДНУ вещь в пустой комнате, с нескольких сторон.
##
##   godot --path client --script res://tools/bench.gd -- --model=switch_stand_manual --out=/tmp/a.png
##   godot --path client --script res://tools/bench.gd -- --track --out=/tmp/rail.png
##   … --state=position=diverging,hand=left   состояние устройства
##
## # Зачем он есть
##
## Слово владельца 2026-08-16: «зачем мы поднимаем полный клиент godot для этого?
## Ещё потом боремся с тенями? Неужели нельзя написать простой тест, который
## рендерит ассет в разных плоскостях без теней и всего прочего?»
##
## До этого дня ответ на вопрос «куда смотрит щиток указателя» получали так:
## поднимали сервер, ждали пятнадцать секунд прогрева рельефа, ставили человека к
## приводу, снимали кадр с небом, травой и тенью — и щупали его пипеткой. На
## вопрос о ФОРМЕ ПРЕДМЕТА всё это не отвечает ничем; оно только мешает.
##
## # Чем стенд отличается от снимка мира
##
## У него НЕТ мира. Ни сервера, ни рельефа, ни чанков, ни канала. Есть предмет,
## объявленный свет и объявленный фон:
##
##   ТЕНЕЙ НЕТ — они прячут форму и меняются от времени суток;
##   НЕБА НЕТ — фон плоский, и цвет предмета есть цвет предмета, а не отражение
##     процедурного неба (та самая причина, по которой накат выходил синим);
##   КАМЕРА ОРТОГРАФИЧЕСКАЯ — у ортогонали нет перспективного вранья, и «эта
##     часть длиннее той» читается прямо с картинки;
##   ВИДЫ ФИКСИРОВАНЫ — четыре плоскости, всегда одни и те же, всегда в одном
##     порядке. Два прогона сравнимы попиксельно.
##
## # Чего стенд НЕ доказывает
##
## Что предмет верно ПОСТАВЛЕН в мире: место, поворот вдоль пути, отметку. Это
## по-прежнему дело чистых проверок (48_turnout_drive) и зонда. Стенд отвечает
## ровно на один вопрос — «как эта вещь выглядит и куда у неё что смотрит».
##
## # Почему не --headless
##
## По той же причине, что у снимка мира: headless подменяет растеризатор
## заглушкой, get_image() возвращает null, а frame_post_draw не наступает никогда.
## Окно настоящее, но крошечное и уведённое за экран.

## Сторона одного вида в пикселях. Четыре вида кладутся в ряд.
##
## Было 420 и хватало приводу: у него всё крупное. Крестовина потребовала больше
## — желоб в 46 мм на предмете в 5.7 м занимал полпикселя, то есть ровно то, чего
## на стенде не должно случаться.
const VIEW_PX := 640

## Отступ вокруг предмета, долей его габарита. Ноль прижал бы предмет к самой
## кромке, и кромка съела бы полсантиметра щитка.
const MARGIN := 0.12

## ВИДЫ. Имя — для строки отчёта, вектор — откуда смотрит камера в осях модели
## (x вправо, y вверх, z назад). Порядок фиксирован: слева направо в листе.
##
## «По ходу» — взгляд подъезжающего: ось −Z узла привода идёт по возрастанию u,
## значит подъезжающий смотрит из +Z. Это тот самый вид, для которого писан ИСИ,
## и потому он первый.
const VIEWS := [
	# СЛЕГКА СВЕРХУ, а не строго с торца, и это не вкус. Строго вдоль пути рельс
	# ИСЧЕЗАЕТ: торцов у него нет (объявлено в TrackView.rail_profile_mesh —
	# рельс уходит в стык со следующим), и с торца видна изнанка, которую
	# отсекает отбраковка. Заодно это ближе к правде: машинист смотрит на путь
	# сверху вниз, а не в упор вдоль головки.
	{"name": "по ходу", "from": Vector3(0, 0.25, 1)},
	{"name": "сбоку", "from": Vector3(1, 0, 0)},
	{"name": "сверху", "from": Vector3(0, 1, 0.001)},
	{"name": "вполоборота", "from": Vector3(1, 0.55, 1)},
]

## Свет и фон ОБЪЯВЛЕНЫ ЧИСЛАМИ, а не взяты у мира: стенд обязан давать один и
## тот же кадр завтра и на другой машине.
const BG := Color(0.14, 0.15, 0.17)
const LIGHT_ENERGY := 1.15
const AMBIENT := Color(0.42, 0.44, 0.47)
const AMBIENT_ENERGY := 0.55

## Полудлина окна вокруг точки крестовины, метры. Семь метров на весь предмет:
## усовик у затравки 3.6 м, контррельс 4 м, и окно обязано быть шире их, иначе
## стенд показывал бы обрезок вместо детали.
const FROG_WINDOW_M := 3.5

var _out := ""
var _model := ""
var _track := false
var _frog := false
var _states := {}
var _shots: Array[Image] = []
var _root: Node3D
var _cam: Camera3D
var _aabb := AABB()
var _started := false


func _initialize() -> void:
	for a in OS.get_cmdline_user_args():
		if a.begins_with("--out="):
			_out = a.substr(6)
		elif a.begins_with("--model="):
			_model = a.substr(8)
		elif a == "--track":
			_track = true
		elif a == "--frog":
			_frog = true
		elif a.begins_with("--state="):
			for pair in a.substr(8).split(",", false):
				var kv := pair.split("=", false)
				if kv.size() == 2:
					_states[kv[0]] = kv[1]
	if _out == "":
		print("стенд: нужен ключ --out=<файл.png>")
		quit(2)
		return
	if _model == "" and not _track and not _frog:
		print("стенд: нужен ключ --model=<имя>, --track либо --frog")
		quit(2)
		return


## _process — стенд собирается НА ПЕРВОМ КАДРЕ, а не в _initialize.
##
## В _initialize дерево ещё не живо: узел, добавленный там, отвечает
## is_inside_tree() = false, global_transform выходит единичным, и габарит
## предмета считается по несуществующему месту. Найдено первым же прогоном —
## «Condition "!is_inside_tree()" is true» три раза подряд.
func _process(_delta: float) -> bool:
	if _started:
		return false
	_started = true
	_build()
	return false


## _build — пустая комната и предмет в ней.
func _build() -> void:
	_root = Node3D.new()
	root.add_child(_root)

	var env := WorldEnvironment.new()
	var e := Environment.new()
	e.background_mode = Environment.BG_COLOR
	e.background_color = BG
	# Заливка ПЛОСКИМ ЦВЕТОМ, а не небом. Металл берёт весь свой цвет из карты
	# излучения, и небо в ней — это ровно тот случай, из-за которого накат выходил
	# синим. Здесь излучение объявлено серым, и цвет предмета есть его цвет.
	e.ambient_light_source = Environment.AMBIENT_SOURCE_COLOR
	e.ambient_light_color = AMBIENT
	e.ambient_light_energy = AMBIENT_ENERGY
	env.environment = e
	_root.add_child(env)

	# ДВА ИСТОЧНИКА И НИ ОДНОЙ ТЕНИ. Второй — заполняющий, с четверти силы: без
	# него теневая сторона предмета проваливается в фон, и форма читается хуже,
	# чем на снимке мира, ради ухода от которого стенд и заведён.
	for spec in [{"dir": Vector3(-0.5, -0.9, -0.6), "e": LIGHT_ENERGY},
			{"dir": Vector3(0.7, -0.35, 0.55), "e": LIGHT_ENERGY * 0.3}]:
		var l := DirectionalLight3D.new()
		l.shadow_enabled = false
		l.light_energy = float(spec["e"])
		l.look_at_from_position(Vector3.ZERO, spec["dir"] as Vector3, Vector3.UP)
		_root.add_child(l)

	_cam = Camera3D.new()
	_cam.projection = Camera3D.PROJECTION_ORTHOGONAL
	_cam.near = 0.01
	_cam.far = 200.0
	_cam.current = true
	_root.add_child(_cam)

	var subject: Node3D = null
	var title := _model
	if _frog:
		subject = _frog_subject()
		title = "крестовина"
	elif _track:
		subject = _track_subject()
		title = "участок пути"
	else:
		subject = _model_subject()
	if subject == null:
		quit(1)
		return
	_root.add_child(subject)
	_aabb = _bounds(subject)
	if _aabb.size == Vector3.ZERO:
		print("стенд: у предмета нулевой габарит — рисовать нечего")
		quit(1)
		return
	print("стенд: %s, габарит %.3f × %.3f × %.3f м" % [
		title, _aabb.size.x, _aabb.size.y, _aabb.size.z])
	_shoot_all()


## _model_subject — тело устройства из файла модели.
func _model_subject() -> Node3D:
	var path := "res://../server/assets/%s.model.json" % _model
	var text := FileAccess.get_file_as_string(path)
	if text == "":
		print("стенд: файла модели нет: %s" % path)
		return null
	var parsed: Variant = JSON.parse_string(text)
	if not (parsed is Dictionary):
		print("стенд: описание модели не разбирается")
		return null
	var built := ModelBuild.build(parsed as Dictionary, {"name": "12"})
	if built.failed():
		print("стенд: тело не собралось: %s" % built.reason)
		return null
	# СОСТОЯНИЯ ПРИМЕНЯЮТСЯ ЯВНО и печатаются: стенд, молча показавший предмет в
	# неизвестном состоянии, отвечает не на тот вопрос, который задали.
	for k in _states:
		var v := String(_states[k])
		var n := built.apply_state(k, v)
		if n == 0:
			n = built.apply_measure(k, float(v))
		print("  состояние %s = %s: частей тронуто %d" % [k, v, n])
	return built.root


## _track_subject — короткий прямой участок пути из ФИКСТУРЫ сети.
##
## Числа берутся у сервера (снимок сети), а не выдумываются здесь: стенд
## показывает то, что приедет игроку, иначе он показывает себя.
func _track_subject() -> Node3D:
	var text := FileAccess.get_file_as_string("res://tools/fixtures/network_ST_A.json")
	if text == "":
		print("стенд: нет снимка сети — переснимите его (make client-fixtures)")
		return null
	var net: Variant = JSON.parse_string(text)
	if not (net is Dictionary):
		return null
	var types := TrackBuild.types_by_id(net as Dictionary)
	if types.is_empty():
		print("стенд: в снимке сети нет ни одного типа пути")
		return null
	var span := TrackBuild.Span.new()
	span.element_id = "BENCH"
	TrackBuild._fill_type(span, types[types.keys()[0]] as Dictionary)
	# ОСЬ КЛАДЁТСЯ ВДОЛЬ −Z ДВИЖКА — туда же, куда смотрит ось привода
	# (SwitchStand.build). Иначе «вид по ходу» у пути и у механизма означал бы
	# разные стороны, и два листа стенда нельзя было бы сравнить.
	#
	# Плановый +y переходит в −Z (TerrainMesh.to_godot), значит курс π/2.
	var axis: Array[TrackGeom.AxisPoint] = []
	for k in 3:
		var u := float(k) * 1.5
		axis.append(TrackGeom.AxisPoint.new(0.0, u, 0.0, PI / 2.0, u))
	span.axis = axis
	var node := Node3D.new()
	node.name = "TrackSample"
	for pair in [
		{"mesh": TrackView.rail_body_mesh(span), "mat": TrackView.rail_material()},
		{"mesh": TrackView.railhead_mesh(span), "mat": TrackView.railhead_material()},
	]:
		var mesh := pair["mesh"] as ArrayMesh
		if mesh == null:
			continue
		var mi := MeshInstance3D.new()
		mi.mesh = mesh
		mi.material_override = pair["mat"] as Material
		node.add_child(mi)
	return node


## _frog_subject — КРЕСТОВИНА ВБЛИЗИ: окно вокруг точки пересечения со всем, что
## в него попало, — нитками проходов, усовиками, контррельсами и сердечником.
##
## Заведено 2026-08-16 по слову владельца «крестовина до сих пор выглядит плохо,
## там каша какая-то». Проверять это было нечем: обзорный кадр наводится на сеть,
## и желоб в 46 мм в нём мельче пикселя; зонд стрелки стоит у привода и смотрит
## вдоль пути, то есть мимо крестовины. Стенд отвечает ровно на вопрос о ФОРМЕ —
## без травы, тени и неба, четырьмя видами в один лист.
##
## Числа берутся у СЕРВЕРА (снимок сети), как и у участка пути: стенд показывает
## то, что приедет игроку.
func _frog_subject() -> Node3D:
	var text := FileAccess.get_file_as_string("res://tools/fixtures/network_ST_A.json")
	if text == "":
		print("стенд: нет снимка сети — переснимите его (make client-fixtures)")
		return null
	var parsed: Variant = JSON.parse_string(text)
	if not (parsed is Dictionary):
		return null
	var net: Dictionary = parsed as Dictionary
	var elements: Array[TrackGeom.Element] = []
	for e_raw in (net.get("elements", []) as Array):
		elements.append(TrackGeom.tessellate_element(e_raw as Dictionary, SEG_M, ANG_RAD))
	var by_id := TrackBuild.elements_by_id(elements)

	# Крестовина берётся ПЕРВАЯ по каноническому порядку — тому же, в котором их
	# отдаёт сервер: два прогона стенда обязаны показывать одну и ту же вещь.
	var owner := ""
	var window := {} # элемент -> (u от, u до)
	for f_raw in (net.get("features", []) as Array):
		var f: Dictionary = f_raw as Dictionary
		if String(f.get("kind", "")) != "frog":
			continue
		var this_owner := String(f.get("owner", ""))
		if owner != "" and this_owner >= owner:
			continue
		owner = this_owner
		window = {}
		for a_raw in (f.get("addresses", []) as Array):
			var a: Dictionary = a_raw as Dictionary
			var u := float(a.get("u", 0.0))
			window[String(a.get("element", ""))] = Vector2(u - FROG_WINDOW_M, u + FROG_WINDOW_M)
	if owner == "":
		print("стенд: в снимке сети нет ни одной крестовины")
		return null

	var node := Node3D.new()
	node.name = "Frog"
	var rail_mat := TrackView.rail_material()
	# НИТКИ ПРОХОДОВ — с их разрывами: ровно то, ради чего разрывы и заведены.
	# Обрезаются окном, а не рисуются целиком: перевод длиной 33 м на стенде дал
	# бы крестовину в двадцатую долю кадра.
	for sp in TrackBuild.covered_spans(net, by_id, SEG_M, ANG_RAD):
		if not window.has(sp.element_id) or not sp.has_rail_body():
			continue
		var win: Vector2 = window[sp.element_id]
		_add_mesh(node, TrackView.rail_body_mesh(_clip_span(sp, win)), rail_mat)
		_add_mesh(node, TrackView.railhead_mesh(_clip_span(sp, win)), TrackView.railhead_material())
		_add_mesh(node, TrackView.rail_fillet_mesh(_clip_span(sp, win)), TrackView.railfillet_material())
	# Усовики, контррельсы и сердечник — как в мире, без обрезки: они и так лежат
	# вокруг точки крестовины.
	var castings: Array = []
	for r_raw in (TrackBuild.frog_rails(net, by_id, SEG_M, ANG_RAD)["list"] as Array):
		var rail: TrackBuild.FrogRail = r_raw
		if rail.owner != owner:
			continue
		if rail.kind == TrackBuild.FROG_CASTING:
			castings.append(rail)
			continue
		_add_mesh(node, TrackView.frog_rail_mesh(rail), rail_mat)
		_add_mesh(node, TrackView.frog_railhead_mesh(rail), TrackView.railhead_material())
		_add_mesh(node, TrackView.frog_rail_fillet_mesh(rail), TrackView.railfillet_material())
	if castings.size() == 2:
		_add_mesh(node, TrackView.frog_casting_mesh(castings[0], castings[1]), rail_mat)
		_add_mesh(node, TrackView.frog_casting_head_mesh(castings[0], castings[1]),
			TrackView.railhead_material())
		_add_mesh(node, TrackView.frog_casting_fillet_mesh(castings[0], castings[1]),
			TrackView.railfillet_material())
	# Остряков в окне крестовины нет — они у острия, — но проверить, что их и
	# правда нет там, где их быть не должно, стоит: попади остряк в кадр, значит
	# сервер прислал его длиннее перевода.
	for b_raw in (TrackBuild.blades(net, by_id, SEG_M, ANG_RAD)["list"] as Array):
		var blade: TrackBuild.Blade = b_raw
		if blade.owner != owner or not window.has(blade.element_id):
			continue
		var win: Vector2 = window[blade.element_id]
		if blade.length_m <= win.x:
			continue
		_add_mesh(node, TrackView.frog_rail_mesh(blade), rail_mat)
		_add_mesh(node, TrackView.frog_railhead_mesh(blade), TrackView.railhead_material())
		_add_mesh(node, TrackView.frog_rail_fillet_mesh(blade), TrackView.railfillet_material())
	return node


## _clip_span — участок, обрезанный окном по u.
##
## Копией, а не правкой на месте: тот же участок спрашивают четыре вида подряд, и
## обрезанный дважды стал бы вдвое короче. Куски ниток (разрывы) обрезаются тем
## же окном — иначе рельс вылез бы за край предмета там, где разрывов нет.
func _clip_span(sp: TrackBuild.Span, win: Vector2) -> TrackBuild.Span:
	var out := TrackBuild.Span.new()
	out.element_id = sp.element_id
	out.type_id = sp.type_id
	out.gauge_m = sp.gauge_m
	out.rail_height_m = sp.rail_height_m
	out.rail_head_width_m = sp.rail_head_width_m
	out.rail_section = sp.rail_section
	out.axis = _clip_axis(sp.axis, win)
	out.rail_cut_left = sp.rail_cut_left
	out.rail_cut_right = sp.rail_cut_right
	for run in sp.rail_runs_left:
		var piece := _clip_axis(run as Array, win)
		if piece.size() >= 2:
			out.rail_runs_left.append(piece)
	for run in sp.rail_runs_right:
		var piece := _clip_axis(run as Array, win)
		if piece.size() >= 2:
			out.rail_runs_right.append(piece)
	return out


func _clip_axis(axis: Array, win: Vector2) -> Array[TrackGeom.AxisPoint]:
	var out: Array[TrackGeom.AxisPoint] = []
	for p_raw in axis:
		var p: TrackGeom.AxisPoint = p_raw
		if p.u >= win.x and p.u <= win.y:
			out.append(p)
	return out


func _add_mesh(parent: Node3D, mesh: ArrayMesh, mat: Material) -> void:
	if mesh == null:
		return
	var mi := MeshInstance3D.new()
	mi.mesh = mesh
	mi.material_override = mat
	parent.add_child(mi)


## Разбиение оси — ТО ЖЕ, чем пользуется мир (World.TESS_MAX_*): стенд обязан
## показывать ту же геометрию, а не более гладкую. Числа повторены, а не взяты у
## мира, потому что стенд мира не поднимает вовсе; разойдутся — стенд начнёт
## отвечать про свою крестовину, а не про ту, которую видит игрок.
const SEG_M := 5.0
const ANG_RAD := 0.05


## _bounds — габарит предмета по всем его мешам.
func _bounds(n: Node) -> AABB:
	var out := AABB()
	var first := true
	var stack: Array[Node] = [n]
	while not stack.is_empty():
		var cur: Node = stack.pop_back()
		var mi := cur as VisualInstance3D
		if mi != null:
			var box := mi.get_aabb()
			var world := AABB(mi.global_transform * box.position, Vector3.ZERO)
			for i in 8:
				world = world.expand(mi.global_transform * box.get_endpoint(i))
			out = world if first else out.merge(world)
			first = false
		for c in cur.get_children():
			stack.append(c)
	return out


func _shoot_all() -> void:
	# root у сценного дерева И ЕСТЬ окно: get_window() на нём отдаёт null, а
	# размер ставится прямо. Тоже найдено прогоном, а не чтением.
	root.size = Vector2i(VIEW_PX, VIEW_PX)
	for v in VIEWS:
		await process_frame
		_aim(v["from"] as Vector3)
		await RenderingServer.frame_post_draw
		await RenderingServer.frame_post_draw
		var img := root.get_texture().get_image()
		if img == null:
			print("стенд: кадр не снялся (headless не рисует)")
			quit(1)
			return
		_shots.append(img)
		print("  вид «%s» снят" % String(v["name"]))
	_sheet()
	quit(0)


## _aim — навести камеру на предмет с названной стороны.
##
## Габарит СЧИТАННЫЙ, а не заданный числом: стенд обязан одинаково хорошо
## показывать привод в метр и рельс в двадцать сантиметров.
func _aim(from: Vector3) -> void:
	var c := _aabb.get_center()
	var r := _aabb.size.length() * 0.5
	var dir := from.normalized()
	_cam.global_position = c + dir * (r * 4.0 + 1.0)
	var up := Vector3.UP if absf(dir.dot(Vector3.UP)) < 0.99 else Vector3.BACK
	_cam.look_at(c, up)
	# КАДР ПО ТОМУ, ЧТО ПОПЕРЁК ВЗГЛЯДА, а не по диагонали габарита.
	# По диагонали трёхметровый рельс, снятый С ТОРЦА, занимал бы пять процентов
	# кадра — то есть вид «по ходу» выходил пустым. Найдено первым же прогоном
	# стенда на пути.
	var b := _cam.global_transform.basis
	var half := 0.0
	for i in 8:
		var v := _aabb.get_endpoint(i) - c
		half = maxf(half, maxf(absf(v.dot(b.x)), absf(v.dot(b.y))))
	_cam.size = maxf(half * 2.0 * (1.0 + MARGIN), 0.05)


## _sheet — четыре вида в один лист, слева направо в порядке VIEWS.
func _sheet() -> void:
	if _shots.is_empty():
		return
	var w: int = _shots[0].get_width()
	var h: int = _shots[0].get_height()
	var sheet := Image.create(w * _shots.size(), h, false, _shots[0].get_format())
	for i in _shots.size():
		sheet.blit_rect(_shots[i], Rect2i(0, 0, w, h), Vector2i(i * w, 0))
	var err := sheet.save_png(_out)
	print("СТЕНД %s: %s (%dx%d, виды: %s)" % [
		"СОХРАНЁН" if err == OK else "НЕ СОХРАНЁН", _out,
		sheet.get_width(), sheet.get_height(),
		", ".join(VIEWS.map(func(v: Dictionary) -> String: return String(v["name"])))])
