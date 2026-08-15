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
	w._driver.put(at + Vector3(1.4, 0.0, 0.0), 0.0, 0.0)
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
	var moved: bool = await _until(func() -> bool:
		return String((w._turnouts[stand.owner_id] as Dictionary).get("position", "")) == want)
	_ok("клавиша T перевела стрелку на сервере", moved,
		"%s -> %s" % [was, (w._turnouts[stand.owner_id] as Dictionary).get("position", "")])
	# И УКАЗАТЕЛЬ В МИРЕ ПОВЕРНУЛСЯ. Это второе звено, и без него сервер знал бы
	# одно, а игрок видел другое.
	_ok("указатель в мире показывает новое положение", stand.shown == want,
		"указатель %s, сервер %s" % [stand.shown, want])

	# 4. И ОБРАТНО. Та же клавиша: положение считает клиент от нынешнего, и если
	#    бы он слал одно и то же, второй перевод ничего бы не изменил.
	_press(KEY_T)
	var back: bool = await _until(func() -> bool:
		return String((w._turnouts[stand.owner_id] as Dictionary).get("position", "")) == was)
	_ok("вторая T вернула стрелку обратно", back,
		"%s" % (w._turnouts[stand.owner_id] as Dictionary).get("position", ""))
	_ok("указатель вернулся вместе с ней", stand.shown == was, stand.shown)

	print("TURNOUT PROBE %s" % ("OK" if _fails == 0 else "ОТКАЗОВ %d" % _fails))
	quit(_fails)


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
