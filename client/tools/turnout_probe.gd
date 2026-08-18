extends SceneTree
## ЗОНД СТРЕЛКИ. Отвечает числами на вопрос, который иначе проверяется только
## глазами: доходит ли клавиша до остряка, а остряк — обратно до указателя.
##
##   godot --path client --script res://tools/turnout_probe.gd -- --server=… --region=ST_A
##
## # Зачем зонд, если есть проверки
##
## Их две, и между ними дыра ровно в одном месте — том же, что у кабины. Чистая
## проверка (checks/pure/48_turnout_drive.gd) меряет разбор привода и поворот
## указателя, не зная ни про клавиши, ни про сервер. Живая
## (checks/live/15_channel.gd) меряет команду, вызывая set_turnout напрямую, —
## клавиш там тоже нет.
##
## Не проверено ничем оказывается САМОЕ ГЛАВНОЕ: подошёл — нажал — стрелка
## перевелась — указатель в мире повернулся. Каждое звено по отдельности зелёное,
## а цепочки нет.
##
## Зонд закрывает эту дыру и только её. Он ставит человека к приводу, нажимает
## КЛАВИШУ и читает ПРИСЛАННОЕ СЕРВЕРОМ положение, а не то, что клиент себе
## показал.
##
## --headless НЕ ГОДИТСЯ по той же причине, что у остальных зондов: мир строится
## сценой, а сцене нужен вьюпорт. Окно уводится за экран.

const SETTLE_WAIT := 60.0 ## с — сколько ждём мир, путь и канал
const REPLY_WAIT := 8.0   ## с — сколько ждём, пока перевод доедет обратно

var _app: Node
var _t := 0.0
var _fails := 0
var _running := false
var _said := 0.0

## Как часто зонд говорит, что он жив, пока ждёт мир.
const SAY_EVERY_S := 3.0


func _initialize() -> void:
	_app = (load("res://scenes/main.tscn") as PackedScene).instantiate()
	root.add_child(_app)


func _physics_process(delta: float) -> bool:
	_t += delta
	var w := _world()
	# ЖДЁМ НЕ «МИР ПОСТРОЕН», А «ЕСТЬ ЧТО ПЕРЕВОДИТЬ»: приводы приезжают
	# геометрией, положения — каналом, и до прихода обоих зонд мерил бы пустоту.
	if w == null or w._stands.is_empty() or w._turnouts.is_empty() or w._driver == null:
		if _t > SETTLE_WAIT:
			print("зонд: за %.0f с не дождались приводов и положений (нужен ключ --role=driver)" % SETTLE_WAIT)
			quit(1)
			return true
		# ПРИЗНАК ЖИЗНИ, пока греется мир. Молчащее окно неотличимо от
		# зависшего: рельеф едет секундами, и владелец закрывал окно, не дождавшись
		# («тест зависает, я окно закрываю», 2026-08-16).
		if _t - _said > SAY_EVERY_S:
			_said = _t
			print("зонд: ждём мир — %.0f с из %.0f (приводов %d, положений %d)" % [
				_t, SETTLE_WAIT,
				0 if w == null else w._stands.size(),
				0 if w == null else w._turnouts.size()])
		return false
	if not _running:
		_running = true
		_run(w)
	return false


func _world() -> Node:
	for n in root.get_children():
		var found := _find(n)
		if found != null:
			return found
	return null


func _find(n: Node) -> Node:
	if n.get_script() != null and n.has_method("_load_world"):
		return n
	for c in n.get_children():
		var f := _find(c)
		if f != null:
			return f
	return null


func _run(w: Node) -> void:
	var stand: SwitchStand = null
	for id in w._stands:
		stand = w._stands[id]
		break
	print("=== ЗОНД СТРЕЛКИ: приводов %d, положений %d ===" % [
		w._stands.size(), w._turnouts.size()])
	_report_indicators(w)

	# 1. ВДАЛИ КЛАВИША МОЛЧИТ. Человек стоит там, где его поставил мир, и до
	#    привода ему далеко: пульт пуст, T ничего не переводит.
	var far: Dictionary = w._turnout_target()
	_ok("вдали от привода пульту нечего показывать", far.is_empty(), str(far))
	var was := String((w._turnouts[stand.owner_id] as Dictionary).get("position", ""))
	_press(KEY_T)
	await _wait(0.5)
	_ok("вдали T не перевела ничего",
		String((w._turnouts[stand.owner_id] as Dictionary).get("position", "")) == was, was)

	# 2. ПОДОШЁЛ — ПОЯВИЛОСЬ ПРЕДЛОЖЕНИЕ. Человек ставится рядом с приводом; его
	#    ставит ЗОНД, а не ходьба: дойти до горловины пешком — это минуты, а
	#    проверяется здесь не ходьба (её меряет walk_probe).
	var at: Vector3 = stand.plan_point()
	# ЧЕЛОВЕК СТАВИТСЯ ПЕРЕД ПРИВОДОМ ПО ХОДУ ПРОХОДА и смотрит через него к
	# крестовине, а не просто лицом в точку привода.
	#
	# В первой редакции человек смотрел НА привод. С какой стороны он при этом
	# оказывался, задавал мировой вектор (2.2, 0, 0.6), не связанный с путём. На
	# ST_A камера встала со стороны крестовины и смотрела назад, на общий прямой
	# участок. В кадре закономерно были только параллельные рамные рельсы и
	# острия; по этому кадру несколько раз подряд пытались «чинить стрелку»,
	# которая целиком находилась за спиной.
	#
	# Ось −Z привода уже поставлена вдоль возрастания u (SwitchStand.build), то
	# есть от общего порта к двум выходам. Берём ЕЁ, а не повторяем преобразование
	# координат пути. Курс по-прежнему считает сам человек (yaw_for) — второго
	# правила о том, как направление связано со взглядом, зонд не заводит.
	#
	# ВЗГЛЯД ОПУЩЕН, А ЧЕЛОВЕК ОСТАЁТСЯ ВПЛОТНУЮ. С почти горизонтальным взглядом
	# в кадр не попадало главное: тяга лежит ниже головки рельса, то есть за
	# нижней кромкой. Отойти нельзя — досягаемость привода коротка (STAND_REACH_M),
	# и человек, отошедший ради вида, перестаёт дотягиваться до стрелки: зонд
	# после такой правки отказал семь раз подряд.
	var along := -stand.global_transform.basis.z
	along.y = 0.0
	along = along.normalized()
	var stood := at - along * 2.2
	var toward_frog := at + along * 12.0
	w._driver.put(stood, w._driver.yaw_for(toward_frog - stood), -30.0)
	await physics_frame
	await process_frame
	var near: Dictionary = w._turnout_target()
	_ok("у привода пульт показывает ЕГО стрелку",
		String(near.get("id", "")) == stand.owner_id, str(near))
	_ok("подсказка у ног предлагает перевод",
		w._driver.prompt().contains("перевести"), w._driver.prompt())
	_ok("пульт показывает то же положение, что пришло с сервера",
		String(near.get("position", "")) == was, "%s против %s" % [near.get("position", ""), was])

	# 3. НАЖАЛ — ПЕРЕВЕЛАСЬ. Читается ПРИСЛАННОЕ сервером положение, а не то, что
	#    клиент себе показал: показать можно что угодно.
	var want := "diverging" if was == "straight" else "straight"
	_press(KEY_T)
	# 3а. ОСТРЯК ИДЁТ, А НЕ ПРЫГАЕТ. Ловится ПОСРЕДИ хода: сначала ждём, пока
	#     сервер объявит перевод, потом смотрим долю и отвод остряка. Без этого
	#     зонд видел бы только начало и конец — то есть ровно то, что было до
	#     2026-08-16, когда положение менялось в тот же тик.
	var started: bool = await _until(func() -> bool:
		return bool((w._turnouts[stand.owner_id] as Dictionary).get("moving", false)))
	_ok("сервер объявил перевод идущим", started,
		str(w._turnouts.get(stand.owner_id, {})))
	if started:
		var sw: Dictionary = w._turnouts[stand.owner_id] as Dictionary
		_ok("в переводе стрелка не стоит нигде", String(sw.get("position", "")) == "",
			"положение «%s»" % String(sw.get("position", "")))
		_ok("перевод идёт к запрошенной ветви", String(sw.get("to", "")) == want,
			"идёт к %s" % sw.get("to", ""))
		var mid := _blade_of(w, stand.owner_id, want)
		if not mid.is_empty():
			var mbl: TrackBuild.Blade = mid["blade"]
			# Остряк цели ЗАКРЫВАЕТСЯ: отвод строго между «отведён» и «прижат».
			_ok("остряк на середине перевода отведён не до конца и не прижат",
				mbl.open > 0.0 and mbl.open < 1.0,
				"отвод %.3f при доле перевода %.3f" % [mbl.open, sw.get("progress", 0.0)])
	var moved: bool = await _until(func() -> bool:
		return String((w._turnouts[stand.owner_id] as Dictionary).get("position", "")) == want)
	_ok("клавиша T перевела стрелку на сервере", moved,
		"%s -> %s" % [was, (w._turnouts[stand.owner_id] as Dictionary).get("position", "")])
	# И УКАЗАТЕЛЬ В МИРЕ ПОВЕРНУЛСЯ. Это второе звено, и без него сервер знал бы
	# одно, а игрок видел другое.
	_ok("указатель в мире показывает новое положение", stand.shown == want,
		"указатель %s, сервер %s" % [stand.shown, want])
	# И ОСТРЯК ОТОШЁЛ. Третье звено, и до 2026-08-15 его не было вовсе: сервер
	# знал, указатель показывал, а рельсы стояли (ClearAhead-86mb). Проверяется
	# ПОПЕРЕЧНОЕ ПОЛОЖЕНИЕ подвижной нитки в кадре, а не поле состояния: поле
	# можно выставить, ничего не нарисовав.
	var blade_rec := _blade_of(w, stand.owner_id, want)
	_ok("у переведённой стрелки есть подвижный проход", not blade_rec.is_empty(),
		"проходов с остряком: %d" % w._blades.size())
	if not blade_rec.is_empty():
		var bl: TrackBuild.Blade = blade_rec["blade"]
		_ok("остряк прохода, по которому стрелка стоит, прижат", not bl.open,
			"ветвь %s, положение %s, отведён %s" % [bl.branch, want, bl.open])
		var other := _blade_of(w, stand.owner_id, was)
		if not other.is_empty():
			var obl: TrackBuild.Blade = other["blade"]
			_ok("остряк другого прохода отведён", obl.open,
				"ветвь %s, отведён %s" % [obl.branch, obl.open])
			var mesh := (other["node"] as MeshInstance3D).mesh
			_ok("отведённый остряк перестроен в кадре", mesh != null)

	# 4. И ОБРАТНО. Та же клавиша: положение считает клиент от нынешнего, и если
	#    бы он слал одно и то же, второй перевод ничего бы не изменил.
	_press(KEY_T)
	var back: bool = await _until(func() -> bool:
		return String((w._turnouts[stand.owner_id] as Dictionary).get("position", "")) == was)
	_ok("вторая T вернула стрелку обратно", back,
		"%s" % (w._turnouts[stand.owner_id] as Dictionary).get("position", ""))
	_ok("указатель вернулся вместе с ней", stand.shown == was, stand.shown)

	# СНИМОК ПОСЛЕДНИМ ДЕЛОМ, если о нём попросили. Окно зонда — единственное
	# место, где стрелку видно вблизи: обзорные кадры (make dev-shot) наводятся
	# на сеть целиком, и остряк с желобом в них меньше пикселя.
	await _shoot(w)
	print("TURNOUT PROBE %s" % ("OK" if _fails == 0 else "ОТКАЗОВ %d" % _fails))
	quit(_fails)


## _shoot — снимок кадра зонда. Без ключа --shot= не делает ничего.
##
## Кадр ДОЖДАТЬСЯ, а не форсировать — та же грабля, что у мира: force_draw сразу
## после правки сцены даёт прошлый кадр.
func _shoot(w: Node) -> void:
	var path := ""
	for a in OS.get_cmdline_user_args():
		if a.begins_with("--shot="):
			path = a.substr(7)
	if path == "":
		return
	w.ui_label.visible = false
	await RenderingServer.frame_post_draw
	await RenderingServer.frame_post_draw
	var tex := root.get_texture()
	if tex == null:
		print("СНИМОК НЕ СДЕЛАН: у окна нет текстуры")
		return
	var img := tex.get_image()
	if img == null:
		print("СНИМОК НЕ СДЕЛАН: get_image() вернул null")
		return
	var err := img.save_png(path)
	print("СНИМОК %s: %s (%dx%d)" % [
		"СОХРАНЁН" if err == OK else "НЕ СОХРАНЁН", path, img.get_width(), img.get_height()])


## _press — клавиша идёт ТЕМ ЖЕ ПУТЁМ, что от игрока: через ввод движка, а не
## вызовом обработчика. Зонд, зовущий метод напрямую, доказал бы, что метод
## работает, и ничего — про клавишу.
func _press(key: int) -> void:
	var ev := InputEventKey.new()
	ev.keycode = key
	ev.physical_keycode = key
	ev.pressed = true
	Input.parse_input_event(ev)
	var up := InputEventKey.new()
	up.keycode = key
	up.physical_keycode = key
	up.pressed = false
	Input.parse_input_event(up)


func _wait(seconds: float) -> void:
	var until := Time.get_ticks_msec() + int(seconds * 1000.0)
	while Time.get_ticks_msec() < until:
		await process_frame


func _until(cond: Callable) -> bool:
	var until := Time.get_ticks_msec() + int(REPLY_WAIT * 1000.0)
	while Time.get_ticks_msec() < until:
		if cond.call():
			return true
		await process_frame
	return false


func _ok(name: String, cond: bool, detail: String = "") -> void:
	if not cond:
		_fails += 1
	print("  %s %s%s" % ["ok  " if cond else "ОТКАЗ", name, "" if detail == "" else "  " + detail])


## _blade_of — подвижный участок стрелки по её ветви. Пусто, если такого нет:
## ответ старого сервера остряков не несёт, и зонд обязан сказать это словами, а
## не упасть на пустом словаре.
func _blade_of(w: Node, owner: String, branch: String) -> Dictionary:
	for eid in w._blades:
		var rec: Dictionary = w._blades[eid]
		var bl: TrackBuild.Blade = rec["blade"]
		if bl.owner == owner and bl.branch == branch:
			return rec
	return {}


## _report_indicators — КУДА СМОТРИТ КАЖДЫЙ УКАЗАТЕЛЬ, числами.
##
## Чистая проверка меряет то же, но на СВОЁМ приводе, собранном из фикстуры. Тут
## меряется ЖИВОЙ мир: те же узлы, что видит игрок, с тем положением, что прислал
## сервер. Расхождение между этими двумя замерами и есть ответ на «указатель
## смотрит не туда» — оно скажет, врёт описание тела или что-то по дороге.
func _report_indicators(w: Node) -> void:
	for id in w._stands:
		var st: SwitchStand = w._stands[id]
		var vane := _named(st, "vane")
		if vane == null:
			print("  указатель %s: щитка нет" % st.label)
			continue
		var sw: Dictionary = (w._turnouts.get(id, {}) as Dictionary)
		# Направление прохода в осях движка: ось −Z узла привода идёт по
		# возрастанию u (SwitchStand.build), и второго правила зонд не заводит.
		var along := -st.global_transform.basis.z
		var left := st.global_transform.basis.x * -1.0
		var n: Vector3 = vane.global_transform.basis.z.normalized()
		var tip: Vector3 = vane.global_transform.basis.x.normalized()
		print("  указатель %s: положение %s, рука %s | лицо·ход %+.3f (ребром ≈0) | стрела·влево %+.3f" % [
			st.label, String(sw.get("position", "?")), String(sw.get("hand", "?")),
			n.dot(along), tip.dot(left)])


## _named — узел по имени в поддереве. Своя копия, потому что зонд не занимает
## у проверок: у него другой процесс и другая жизнь.
func _named(root: Node, want: String) -> Node3D:
	if root.name == want and root is Node3D:
		return root as Node3D
	for c in root.get_children():
		var f := _named(c, want)
		if f != null:
			return f
	return null
