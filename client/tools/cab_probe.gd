extends SceneTree
## ЗОНД КАБИНЫ. Отвечает числами на вопрос, который иначе проверяется только
## глазами: доходит ли клавиша до рукоятки, а рукоятка — до сервера.
##
##   godot --path client --script res://tools/cab_probe.gd -- --server=… --region=ST_A
##
## # Зачем зонд, если есть проверки
##
## Их две, и между ними дыра ровно в одном месте. Чистая проверка
## (checks/pure/96_cab.gd) меряет арифметику рукояток, не зная про клавиши.
## Живая (checks/live/15_channel.gd) меряет команду, вызывая set_controls
## напрямую, — клавиш там тоже нет. Само сопоставление «клавиша 2 — ступень
## вверх» не проверялось ничем, кроме снимка, а на снимке «тяга 7» от «тяги 1»
## глазами отличается плохо.
##
## Зонд закрывает эту дыру и только её: он нажимает КЛАВИШИ и читает, что встало
## НА СЕРВЕРЕ (cab.set_*), а не то, что клиент себе показал.
##
## --headless НЕ ГОДИТСЯ по той же причине, что у walk_probe и stock_probe: мир
## строится сценой, а сцене нужен вьюпорт. Окно уводится за экран.

const SETTLE_WAIT := 60.0 ## с — сколько ждём мир, кабину и канал
const REPLY_WAIT := 5.0   ## с — сколько ждём ответ сервера на команду

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
	if w == null or not w.cab.aboard():
		if _t > SETTLE_WAIT:
			print("зонд: в кабину так и не сели за %.0f с (нужен ключ --board)" % SETTLE_WAIT)
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
	var cab: Cab = w.cab
	print("=== ЗОНД КАБИНЫ: машина %s, ступеней тяги %d, торможения %d ===" % [
		cab.unit_id, cab.traction_notches, cab.brake_notches])

	# ИСХОДНОЕ ПОЛОЖЕНИЕ — нулевое. Зонд не полагается на то, что мир свежий:
	# прошлый прогон мог оставить рукоятки набранными, и тогда все числа ниже
	# считались бы от чужого начала.
	_press(KEY_0)
	await _settled(cab)
	# «Всё в ноль» ставит ПОЛНЫЙ тормоз (машинист этим движением останавливается,
	# а не катится), поэтому исходное для замеров положение — отпущенный тормоз.
	# Первый прогон зонда об это и споткнулся: он ждал от «4» первой ступени, а
	# тормоз уже стоял на пятой.
	for i in range(cab.brake_notches):
		_press(KEY_3)
		await _settled(cab)
	_ok("тормоз отпущен клавишей «3»", cab.set_brake == 0, str(cab.set_brake))

	_press(KEY_R)
	await _settled(cab)
	_ok("реверсор клавишей R встал вперёд", cab.set_reverser == "forward", cab.set_reverser)

	# ТЯГА: три нажатия «2» — три ступени. Проверяется ВСТАВШЕЕ на сервере, а не
	# показанное клиентом: показать можно что угодно.
	for i in range(3):
		_press(KEY_2)
		await _settled(cab)
	_ok("три нажатия «2» дали три ступени тяги", cab.set_traction == 3, str(cab.set_traction))

	_press(KEY_1)
	await _settled(cab)
	_ok("нажатие «1» сбросило ступень", cab.set_traction == 2, str(cab.set_traction))

	# ТОРМОЗ: своя пара клавиш, и она не должна трогать тягу.
	_press(KEY_4)
	await _settled(cab)
	_ok("нажатие «4» дало ступень тормоза", cab.set_brake == 1, str(cab.set_brake))
	_ok("тормоз не тронул тягу", cab.set_traction == 2, str(cab.set_traction))

	# РЕВЕРСОР ПОД ТЯГОЙ НЕ ХОДИТ, и клавиша об этом знает: команда не уходит
	# вовсе, а не отказывается сервером.
	var was := cab.set_reverser
	_press(KEY_R)
	await _settled(cab)
	_ok("под тягой реверсор клавишей не переводится", cab.set_reverser == was, cab.set_reverser)

	# «ВСЁ В НОЛЬ»: тяга сброшена, тормоз ПОЛНЫЙ, реверсор в ноль.
	_press(KEY_0)
	await _settled(cab)
	_ok("«0» сбросило тягу", cab.set_traction == 0, str(cab.set_traction))
	_ok("«0» поставило полный тормоз", cab.set_brake == cab.brake_notches, str(cab.set_brake))
	_ok("«0» вернуло реверсор в ноль", cab.set_reverser == "neutral", cab.set_reverser)

	# И оставляем мир там, откуда взяли: отпущенный тормоз при нулевой тяге.
	_press(KEY_3)
	await _settled(cab)

	await _check_ride(w)
	await _check_camera(w)

	print("CAB PROBE %s" % ("OK" if _fails == 0 else "ОТКАЗОВ %d" % _fails))
	quit(_fails)


## МАШИНА ЕДЕТ — и это то, ради чего всё остальное.
##
## Меряется ПУТЬ ПО МИРУ, а не число в снапшоте: узел машины в осях мира до и
## после. Число из снапшота показало бы, что сервер посчитал; путь узла
## показывает, что клиент это применил.
func _check_ride(w: Node) -> void:
	var cab: Cab = w.cab
	var machine: Node3D = null
	for u_raw in w._stock_units:
		var u := u_raw as RollingStock.Unit
		if u.id == cab.unit_id:
			machine = u.node
	if machine == null:
		_ok("узел машины найден", false)
		return

	# ТОРМОЗ ОТПУСКАЕТСЯ ПЕРВЫМ, и это не формальность: прошлая проверка кончила
	# «всё в ноль», то есть ПОЛНЫМ тормозом, и машина под ним никуда не поедет.
	# Первый заход на этом и споткнулся — «0.0 м за 5 с», — и это была ошибка
	# зонда, а не мира.
	for i in range(cab.brake_notches):
		_press(KEY_3)
		await _settled(cab)
	_ok("тормоз отпущен перед поездкой", cab.set_brake == 0, str(cab.set_brake))

	var from := machine.global_position
	# ЕДЕМ В СТОРОНУ СТРЕЛКИ (Shift+R — реверсор назад), а не к упору.
	#
	# Мир зонд не пересоздаёт: сервер живёт своей жизнью, и машина вполне может
	# стоять там, куда её привёл прошлый прогон. Первый заход это и поймал —
	# «0.0 м за 5 с», потому что локомотив упирался в тупиковый конец главного
	# пути. В сторону стрелки путь есть всегда: за ней подход и край карты.
	_press_shift(KEY_R)
	await _settled(cab)
	_ok("реверсор встал назад", cab.set_reverser == "reverse", cab.set_reverser)
	for i in range(10):
		_press(KEY_2)
		await _settled(cab)
	_ok("набрана тяга", cab.set_traction == 10, str(cab.set_traction))

	# Пять секунд НАСТЕННОГО времени: темп мира 1:1, значит и модельного столько
	# же. Ждём кадрами, а не сном: сокет опрашивается в кадре.
	#
	# ПО ДОРОГЕ МЕРЯЕМ ДВА ЧИСЛА, без которых нельзя выбрать сглаживание показа
	# (решение ClearAhead-t5h §4: «сначала замерить, годится ли тот же буфер»):
	# как часто приходят снапшоты и как далеко машина прыгает между ними.
	var until := Time.get_ticks_msec() + 5000
	var gaps: Array[float] = []
	var frames: Array[float] = []
	var still := 0
	var last_seq := int(w.stats.get("channel_seq", 0))
	var last_at := Time.get_ticks_usec()
	var last_pos := machine.global_position
	while Time.get_ticks_msec() < until:
		await process_frame
		# ПЛАВНОСТЬ МЕРЯЕТСЯ ПОКАДРОВО, а не по снапшотам: дёрганье — это когда
		# машина стоит несколько кадров и прыгает на один. Замер по снапшотам
		# такого не видит вовсе, он видит только шаг сервера.
		var moved := last_pos.distance_to(machine.global_position)
		frames.append(moved)
		if moved < 0.001:
			still += 1
		last_pos = machine.global_position
		var seq := int(w.stats.get("channel_seq", 0))
		if seq != last_seq:
			var now := Time.get_ticks_usec()
			gaps.append(float(now - last_at) / 1000.0)
			last_seq = seq
			last_at = now
	if gaps.size() > 2 and frames.size() > 2:
		gaps.sort()
		var sorted := frames.duplicate()
		sorted.sort()
		print("CAB PROBE: снапшотов %d, промежуток мин %.1f / медиана %.1f / макс %.1f мс"
			% [gaps.size(), gaps[0], gaps[gaps.size() / 2], gaps[gaps.size() - 1]])
		print("CAB PROBE: кадров %d, сдвиг за кадр медиана %.3f м, макс %.3f м, "
			% [frames.size(), sorted[sorted.size() / 2], sorted[sorted.size() - 1]]
			+ "неподвижных кадров %.0f%%" % [100.0 * float(still) / float(frames.size())])
	var went := from.distance_to(machine.global_position)
	_ok("машина проехала по миру", went > 5.0, "%.1f м за 5 с" % went)
	_ok("скорость дошла до клиента", float(w.stats.get("stock_speed_ms", 0.0)) > 1.0,
		"%.2f м/с" % float(w.stats.get("stock_speed_ms", 0.0)))

	# И ОСТАНОВКА: полный тормоз, сброс тяги.
	_press(KEY_0)
	await _settled(cab)
	# Ждём РОВНОГО НУЛЯ, а не «почти»: сервер гасит остаток скорости на месте, и
	# «0.001 м/с» означало бы, что где-то остался ползущий хвост. Ждать не
	# дольше восьми секунд: биение приходит раз в секунду модельного времени,
	# значит нулевую скорость клиент узнает не позже этого.
	var stopping := Time.get_ticks_msec() + 8000
	while Time.get_ticks_msec() < stopping and float(w.stats.get("stock_speed_ms", 0.0)) != 0.0:
		await process_frame
	_ok("машина остановилась тормозом ровно", float(w.stats.get("stock_speed_ms", 0.0)) == 0.0,
		"%.4f м/с" % float(w.stats.get("stock_speed_ms", 0.0)))
	var stopped := machine.global_position
	await process_frame
	await process_frame
	_ok("и стоит", stopped.distance_to(machine.global_position) < 0.01)

	# Отпускаем тормоз, чтобы следующая проверка застала кабину в покое.
	for i in range(cab.brake_notches):
		_press(KEY_3)
		await _settled(cab)


## КАМЕРА СМОТРИТ НА МАШИНУ, А НЕ НА ЧЕЛОВЕКА.
##
## Требование владельца (разговор 2026-08-14), и проверяется оно числом, потому
## что глазами разница читается плохо: человек стоит в кабине, то есть у КОНЦА
## машины, и кадр, наведённый на него, отрезает половину тридцатичетырёхметрового
## кузова.
##
## Проверяется ПОСЛЕ перебора видов клавишей V: обзор наводится заново каждый раз,
## когда в него возвращаются, и первая редакция теряла машину именно здесь —
## фокус ставил driver и ставил его на себя.
func _check_camera(w: Node) -> void:
	var cab: Cab = w.cab
	var machine: Node3D = null
	for u_raw in w._stock_units:
		var u := u_raw as RollingStock.Unit
		if u.id == cab.unit_id:
			machine = u.node
	if machine == null:
		_ok("узел машины найден", false)
		return
	var orbit := w.camera as OrbitCamera
	if orbit == null:
		_ok("камера роли — обзорная", false)
		return
	# Три нажатия V — полный круг видов: обзор -> первое лицо -> третье -> обзор.
	for i in range(3):
		_press(KEY_V)
		await process_frame
		await process_frame
	var to_machine := orbit.focus.distance_to(machine.global_position)
	var to_person := orbit.focus.distance_to((w._driver as Node3D).global_position)
	_ok("обзор наведён на машину", to_machine < 0.5, "до машины %.2f м" % to_machine)
	_ok("а не на человека", to_machine <= to_person, "до человека %.2f м" % to_person)


## _settled — дождаться, пока нажатие превратится в команду, а команда вернётся
## ответом.
##
## ДВА ОЖИДАНИЯ, А НЕ ОДНО, и второе выяснилось первым же прогоном: все числа
## оказались сдвинуты на шаг назад. Причина записана в проекте раньше нас
## (walk_probe): Input.parse_input_event только КЛАДЁТ событие в буфер, а
## разбирается буфер не в этом кадре. Ожидание «пока pending > 0» возвращалось
## немедленно — команда ещё не ушла, — и следующая строка читала прошлое
## состояние.
##
## Поэтому сначала ждём, пока нажатие ДОЙДЁТ (появится ожидание ответа), и лишь
## потом — пока ответ ВЕРНЁТСЯ. Клавиша, которая нарочно ничего не делает
## (реверсор под тягой), команды не порождает — для неё первое ожидание истекает
## коротким сроком, и это законный исход, а не отказ.
const PRESS_WAIT := 0.5 ## с — сколько ждём, пока нажатие превратится в команду

func _settled(cab: Cab) -> void:
	var appeared := Time.get_ticks_msec() + int(PRESS_WAIT * 1000.0)
	while cab.pending == 0 and Time.get_ticks_msec() < appeared:
		await process_frame
	var deadline := Time.get_ticks_msec() + int(REPLY_WAIT * 1000.0)
	while cab.pending > 0 and Time.get_ticks_msec() < deadline:
		await process_frame
	# Кадр сверху: ответ мог прийти в этот же миг, а состояние обновляется в
	# обработчике сигнала.
	await process_frame


func _press(key: Key) -> void:
	_key(key, true, false)
	_key(key, false, false)


## _press_shift — та же клавиша с Shift: у реверсора вторая сторона живёт на нём.
func _press_shift(key: Key) -> void:
	_key(key, true, true)
	_key(key, false, true)


func _key(key: Key, pressed: bool, shift: bool) -> void:
	var ev := InputEventKey.new()
	ev.keycode = key
	ev.physical_keycode = key
	ev.pressed = pressed
	ev.shift_pressed = shift
	Input.parse_input_event(ev)


func _ok(name: String, cond: bool, detail: String = "") -> void:
	if not cond:
		_fails += 1
	print("CAB PROBE: %-46s %-28s %s" % [name, detail, "ok" if cond else "ОТКАЗ"])
