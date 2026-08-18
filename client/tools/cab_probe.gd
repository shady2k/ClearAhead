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
	# «0» — ЭКСТРЕННАЯ ОСТАНОВКА, а не «всё в ноль»: она ставит кран в экстренное
	# и вспомогательный на полное (машинист этим движением останавливается, а не
	# катится). Поэтому исходное для замеров положение — отпущенный тормоз, и
	# ведём к нему ОБА крана. Первый прогон зонда об это и споткнулся: ждал от
	# ручки первой ступени, а она стояла в экстренном.
	for i in range(Cab.HANDLES.size()):
		_press(KEY_A)
		await _settled(cab)
	_press(KEY_D)
	await _settled(cab)
	for i in range(12):
		_press(KEY_Z)
		await _settled(cab)
	_ok("кран отпущен клавишей A, вспомогательный — Z",
		cab.set_handle == "run" and cab.set_independent == 0,
		"кран %s, вспомогательный %.2f" % [cab.set_handle, cab.set_independent / 1000.0])

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

	# W/S — ВТОРОЕ ИМЯ ТОЙ ЖЕ РУКОЯТКИ, заведено 2026-08-15 по слову владельца
	# («камерой в кабине управляют мышью»). Проверяется здесь, а не принимается на
	# веру: клавиша, которую никто не нажимал, ничем не лучше ненаписанной, а
	# отобрать её у камеры и забыть отдать машине — ровно один недосмотр.
	_press(KEY_W)
	await _settled(cab)
	_ok("W добавило ступень тяги", cab.set_traction == 3, str(cab.set_traction))
	_press(KEY_S)
	await _settled(cab)
	_ok("S сбросило ступень тяги", cab.set_traction == 2, str(cab.set_traction))

	# ТОРМОЗ — КРАНОМ МАШИНИСТА, а не ступенью. Ступени у машины с магистралью
	# нет вовсе (ClearAhead-4mwn), и проверять её здесь значило бы проверять
	# число, которое ни на что не влияет.
	_ok("кабина знает про магистраль", cab.has_air and cab.handle != "",
		"кран %s, давления есть %s" % [cab.handle, cab.has_air])
	var was_handle: String = cab.handle
	_press(KEY_D)
	await _settled(cab)
	_ok("D увёл кран по сектору", cab.set_handle != was_handle,
		"%s -> %s" % [was_handle, cab.set_handle])
	_ok("кран не тронул тягу", cab.set_traction == 2, str(cab.set_traction))
	# ДО СЛУЖЕБНОГО, А НЕ НА ОДНО ПОЛОЖЕНИЕ. Первый заход останавливался на
	# перекрыше, а она лишь ТРАВИТ (0.02 кгс/см² в секунду), и «магистраль
	# разряжается» проходило при 5.40 из 5.40 — то есть доказывало округление, а
	# не работу крана.
	await _handle_to(cab, "service")
	# ПОРОГ — ПЕРВАЯ СТУПЕНЬ ТОРМОЖЕНИЯ (0.5 кгс/см² разрядки): величина
	# инструкции, а не «хоть сколько-нибудь».
	var step_milli := 500
	var fell: bool = await _wait_until(
		func() -> bool: return cab.charge_milli - cab.pipe_pressure >= step_milli, 15.0)
	_ok("магистраль разряжается служебным на первую ступень", fell,
		"разрядка %.2f при зарядном %.2f" % [
			(cab.charge_milli - cab.pipe_pressure) / 1000.0, cab.charge_milli / 1000.0])
	var filled: bool = await _wait_until(
		func() -> bool: return cab.cylinder_pressure >= step_milli, 15.0)
	_ok("цилиндр наполнился от разрядки", filled, "%.2f" % (cab.cylinder_pressure / 1000.0))
	while cab.handle != was_handle:
		_press(KEY_A)
		await _settled(cab)
	_ok("A вернул кран назад по сектору", cab.set_handle == was_handle, cab.set_handle)

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
	# «ВСЁ В НОЛЬ» У МАШИНЫ С МАГИСТРАЛЬЮ — ЭКСТРЕННОЕ. Проверяется положение
	# крана, а не ступень: ступень у такой машины ни на что не влияет, и утверждать
	# про неё значило бы объявлять аварийную клавишу работающей, ничего не проверив.
	_ok("«0» поставило кран в экстренное", cab.set_handle == "emergency", cab.set_handle)
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

	var freed: bool = await _free_the_brakes(cab)
	_ok("тормоз отпущен краном перед поездкой", freed and cab.handle == "run",
		"кран %s, цилиндр %.2f, магистраль %.2f" % [cab.handle,
			cab.cylinder_pressure / 1000.0, cab.pipe_pressure / 1000.0])

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
	# И ЖДЁМ, ПОКА КОНТРОЛЛЕР ДОЙДЁТ. Рукоятка — задание, позиция идёт к нему
	# своим темпом (позиция в секунду), и мерить разгон сразу после команды значит
	# мерить НАБОР ПОЗИЦИЙ, а не тягу. Первый заход после этой правки так и
	# споткнулся: 4.4 м за 5 с вместо 12.7.
	var reached: bool = await _wait_until(
		func() -> bool: return cab.notch_milli >= 10 * 1000, 30.0)
	_ok("контроллер дошёл до заданной позиции", reached,
		"позиция %.1f из заданных 10" % [float(cab.notch_milli) / 1000.0])

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
	# ОТРЫВ КАМЕРЫ ОТ МАШИНЫ — замер, которым куплена жалоба владельца «камера не
	# едет за локомотивом». Прежняя проверка наводки стояла ПОСЛЕ поездки и на
	# СТОЯЩЕЙ машине, поэтому дефект прошёл мимо неё: фокус выставлялся однажды, в
	# миг посадки, и, пока машина не двигалась, был верен. Мерить надо ВО ВРЕМЯ
	# хода и по худшему кадру, а не по среднему: отстающая камера догоняет на
	# остановке, и среднее её выгородит.
	var orbit_ride: OrbitCamera = w.camera as OrbitCamera
	var orbit_gap := 0.0
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
		if orbit_ride != null:
			orbit_gap = maxf(orbit_gap, _focus_gap(orbit_ride, machine))
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
	# Допуск 0.5 м — тот же, что у наводки на стоящей: он про «на машине ли
	# фокус», а не про плавность. Отрыв в метры означал бы, что камера привязана
	# к месту, а не к машине.
	_ok("камера ехала за машиной", orbit_ride != null and orbit_gap < 0.5,
		"самый большой отрыв фокуса %.2f м за поездку %.1f м" % [orbit_gap, went])
	_ok("скорость дошла до клиента", float(w.stats.get("stock_speed_ms", 0.0)) > 1.0,
		"%.2f м/с" % float(w.stats.get("stock_speed_ms", 0.0)))

	# РЫВКИ ПРИ ТОРМОЖЕНИИ. Владелец: «нажимаешь экстренный, а поезд рывками то
	# останавливается, то едет быстрее. Ну и прыжки локомотива тоже видно».
	#
	# ЧИТАЕТСЯ СТОРОЖ МИРА, А НЕ МЕРЯЕТСЯ САМИМ ЗОНДОМ, и это исправление
	# собственной ошибки. Первая редакция считала сдвиг между своими await
	# process_frame — и находила «скачки» по полметра там, где сторож внутри
	# _process не видел ничего. Причина в измерителе: продолжение корутины и кадр
	# движка не одно и то же событие, и в одну итерацию зонда укладывалось два
	# кадра мира. Мерить надо там, где рисуют.
	var jerks_before := int(w.stats.get("display_jerks", 0))
	var slips_before := int(w.stats.get("display_clock_slips", 0))
	_press(KEY_0)
	await _settled(cab)
	# СКОЛЬКО РАЗНЫХ ЗНАЧЕНИЙ ПОКАЗАЛА СТРЕЛКА. Если их около десяти на сотню
	# кадров — прибор идёт ступенькой темпа снапшотов, и интерполяция не работает,
	# что бы она ни считала. Владелец говорит «стрелки на приборах рывками» третий
	# раз, и на этот раз ответом будет число.
	var seen_vals := {}
	var notch_vals := {}
	var frames_seen := 0
	var probe_until := Time.get_ticks_msec() + 3000
	while Time.get_ticks_msec() < probe_until:
		await process_frame
		var v: Dictionary = cab.shown(w._display.show_us())
		seen_vals[snappedf(float(v.get("cyl", 0.0)), 0.5)] = true
		notch_vals[snappedf(float(v.get("notch", 0.0)), 0.5)] = true
		frames_seen += 1
	print("CAB PROBE: стрелка ТЦ — %d разных значений на %d кадров, позиция контроллера — %d"
		% [seen_vals.size(), frames_seen, notch_vals.size()])
	# И ТЕМП СНИМКОВ У СТОЯЩЕЙ МАШИНЫ. Это и есть жалоба владельца целиком:
	# «поначалу всё плавно, а потом раз в секунду». Раз в секунду — это БИЕНИЕ,
	# то есть состояние, которое сервер считает неизменным. Пока давления не
	# входили в хеш, работающая пневматика стоящей машины была «неизменной».
	var at_rest: bool = await _wait_until(
		func() -> bool: return float(w.stats.get("stock_speed_ms", 0.0)) == 0.0, 20.0)
	# ПНЕВМАТИКА ДОЛЖНА РАБОТАТЬ ВО ВРЕМЯ ЗАМЕРА, иначе меряется покой. Первый
	# заход этого не сделал и получил ровно биение — 1.0 снимка в секунду, — что
	# было верным ответом на неверный вопрос.
	# КРАН В ОТПУСК, А НЕ В СЛУЖЕБНОЕ. Служебное разряжает магистраль, а после
	# экстренного она УЖЕ пуста: разряжать нечего, давления стоят, и замер ловил
	# биение — то есть верный ответ на неверный вопрос (ОТКАЗ «1.0 снимка в
	# секунду» на живом прогоне). Отпуск заряжает пустую магистраль всегда, и
	# работающая пневматика у стоящей машины — это ровно он.
	var at_release: bool = await _handle_to(cab, "release")
	_ok("кран доведён до отпуска из экстренного", at_release, cab.handle)
	var seq0 := int(w.stats.get("channel_seq", 0))
	var t0 := Time.get_ticks_msec()
	await _wait_until(func() -> bool: return Time.get_ticks_msec() - t0 > 3000, 6.0)
	var per_sec := float(int(w.stats.get("channel_seq", 0)) - seq0) * 1000.0 		/ float(Time.get_ticks_msec() - t0)
	_ok("у стоящей машины с работающей пневматикой снимки идут чаще биения",
		at_rest and per_sec > 3.0, "%.1f снимка в секунду" % per_sec)

	var until_stop := Time.get_ticks_msec() + 6000
	while Time.get_ticks_msec() < until_stop:
		await process_frame
	var jerks := int(w.stats.get("display_jerks", 0)) - jerks_before
	var slips := int(w.stats.get("display_clock_slips", 0)) - slips_before
	_ok("показ не дёргался при торможении", jerks == 0,
		"рывков %d, худший: %s" % [jerks, str(w.stats.get("display_jerk_worst", "нет"))])
	_ok("часы показа шли ровно", slips == 0,
		"расхождений %d, худшее: %s" % [slips, str(w.stats.get("display_clock_worst", "нет"))])

	# БУКСОВАНИЕ ДОХОДИТ ДО КАБИНЫ. Сквозная проверка: рукоятка на последнюю
	# позицию, контроллер идёт к ней своим темпом, и на позиции, которую сцепление
	# не держит, машина срывается — а лампа на пульте это показывает.
	#
	# Проверяется ПРИЗНАК С СЕРВЕРА, а не догадка клиента: буксование выводит
	# сервер, клиент его только рисует.
	# СНАЧАЛА ПРИВЕСТИ МАШИНУ В РАБОЧЕЕ: прошлая проба кончила «0», то есть
	# экстренным и реверсором в нуле. Под таким реверсором тяга ОТВЕРГАЕТСЯ
	# сервером, и первый заход это и поймал — позиция осталась нулевой.
	for i in range(Cab.HANDLES.size()):
		_press(KEY_A)
		await _settled(cab)
	_press(KEY_D)
	await _settled(cab)
	for i in range(12):
		_press(KEY_Z)
		await _settled(cab)
	await _wait_until(func() -> bool: return cab.cylinder_pressure == 0, 20.0)
	_press(KEY_R)
	await _settled(cab)
	for i in range(cab.traction_notches):
		_press(KEY_W)
		await _settled(cab)
	var slipped: bool = await _wait_until(func() -> bool: return cab.slipping, 45.0)
	_ok("буксование дошло до кабины", slipped,
		"позиция %.1f из %d, скорость %.2f м/с" % [float(cab.notch_milli) / 1000.0,
			cab.traction_notches, float(w.stats.get("stock_speed_ms", 0.0))])
	# И ПОЗИЦИЯ ОТСТАЁТ ОТ РУКОЯТКИ — иначе набор мгновенный, и срыв наступил бы
	# в тот же миг, когда рукоятку двинули.
	_ok("позиция контроллера отстаёт от рукоятки",
		cab.notch_milli < cab.traction * 1000,
		"рукоятка %d, позиция %.1f" % [cab.traction, float(cab.notch_milli) / 1000.0])

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
	# И СТОИТ — НО НЕ В ТОТ ЖЕ КАДР, и это исправление самой проверки.
	#
	# Показ живёт в прошлом на величину буфера (DisplayMotion): сервер отдал ноль,
	# а показ в этот миг ещё проезжает последние известные метры. Требовать
	# неподвижности В КАДРЕ ПРИХОДА НУЛЯ значит требовать, чтобы буфера не было
	# вовсе, — то есть проверять не то. Проверка ловила это отказом («0.27 м за
	# кадр при скорости 0.00») и была права насчёт числа и неправа насчёт вывода.
	#
	# Ждём, пока показ ДОГОНИТ, и только потом требуем покоя. Потолок ожидания —
	# потолок буфера с запасом: дольше догонять нечего, и если догоняет — это уже
	# отказ показа, а не его устройство.
	var settle := Time.get_ticks_msec() + int(DisplayMotion.BUFFER_CEIL_US / 1000) * 2
	var at := machine.global_position
	while Time.get_ticks_msec() < settle:
		await process_frame
		if at.distance_to(machine.global_position) < 0.001:
			break
		at = machine.global_position
	var stopped := machine.global_position
	await process_frame
	await process_frame
	_ok("и стоит", stopped.distance_to(machine.global_position) < 0.01,
		"сдвиг %.4f м, буфер показа %.0f мс" % [stopped.distance_to(machine.global_position),
			float(w._display.buffer_us()) / 1000.0])

	await _ride_across_the_boundary(w, cab, machine)

	# Отпускаем тормоз, чтобы следующая проверка застала кабину в покое.
	for i in range(cab.brake_notches):
		_press(KEY_3)
		await _settled(cab)


## ГРАНИЦА ЭЛЕМЕНТА — ОТДЕЛЬНЫЙ ПЕРЕГОН, и он заведён 2026-08-15 вместе с
## отрезком пути на проводе (ClearAhead-7n0v).
##
## # Почему прежние замеры этого не покрывали
##
## Потому что до границы не доезжали. Поездка зонда — пять секунд от u = 150 м
## главного пути, то есть около семидесяти метров; до стрелки оттуда сто с
## лишним. Все числа плавности мерились ВНУТРИ одного элемента, а дорого стоило
## ровно пересечение: правило «через границу не интерполируем» отдавало на нём
## целый промежуток снимков разом (ЗАМЕР владельцем: 0.72…0.73 м за кадр при
## 6.9 м/с), и сторож рывка эти кадры НЕ СЧИТАЛ — им была выписана поблажка.
##
## Поблажка снята вместе с правилом. Значит проверка обязана доехать до границы:
## «рывков ноль» без единого пересечения доказывает лишь то, что машина осталась
## на своём элементе.
##
## # Два утверждения, и второе не менее важно
##
## ПЕРЕСЕЧЕНИЯ БЫЛИ — иначе первое ничего не значит.
## РЫВКОВ НЕТ и ДЕРЖАНИЙ НЕТ — показ идёт по пути, а не по элементу.
func _ride_across_the_boundary(w: Node, cab: Cab, machine: Node3D) -> void:
	var border_before := int(w.stats.get("display_border_steps", 0))
	var jerks_before := int(w.stats.get("display_jerks", 0))
	var holds_before := int(w.stats.get("display_frame_holds", 0))
	var element_before := String(w.stats.get("stock_element", ""))

	# Тормоз отпустить, реверсор назад, тяга — и ехать, пока элемент не сменится.
	if not await _free_the_brakes(cab):
		_ok("перед перегоном через границу тормоз отпущен", false,
			"кран %s, цилиндр %.2f" % [cab.handle, cab.cylinder_pressure / 1000.0])
		return
	_press_shift(KEY_R)
	await _settled(cab)
	for i in range(10):
		_press(KEY_2)
		await _settled(cab)

	# ПОТОЛОК ОЖИДАНИЯ — ТРИДЦАТЬ СЕКУНД: до стрелки от места остановки около
	# сотни метров, машина набирает десяток метров в секунду. Больше означало бы
	# ждать того, чего не будет; меньше — красить медленный разгон отказом.
	# ЕДЕМ ДО ГРАНИЦЫ, А ПОТОМ ЕЩЁ ДВЕ СЕКУНДЫ. Остановиться на первом же
	# пересечении значило бы проверить ОДИН кадр: досчёт по пути работает не в
	# точке стыка, а на всём куске около него, и ошибка в нём вылезет на кадрах
	# ПОСЛЕ перехода — когда хвост ещё на прошлом элементе, а голова уже на новом.
	# Две секунды при десятке метров в секунду — это ещё один проход стрелки
	# (33.5 м) целиком.
	var crossings := 0
	var deadline := Time.get_ticks_msec() + 30000
	var after := 0
	while Time.get_ticks_msec() < deadline:
		await process_frame
		crossings = int(w.stats.get("display_border_steps", 0)) - border_before
		# ЖДЁМ ВТОРОГО ПЕРЕХОДА, а потом едем ещё секунду. Остановиться на первом
		# значило бы проверить один кадр: досчёт по пути работает не в точке
		# стыка, а на всём куске около неё, и ошибка вылезет на кадрах ПОСЛЕ
		# перехода — когда хвост ещё на прошлом элементе, а голова уже на новом.
		# Второй переход при этом не роскошь: он ловит выход с КОРОТКОГО элемента
		# (проход стрелки 33.5 м против машины 34.18 м), где тело не помещается
		# целиком ни в один элемент вовсе.
		if crossings >= 2:
			if after == 0:
				after = Time.get_ticks_msec() + 1000
			elif Time.get_ticks_msec() > after:
				break
	_press(KEY_0)
	await _settled(cab)

	var jerks := int(w.stats.get("display_jerks", 0)) - jerks_before
	var holds := int(w.stats.get("display_frame_holds", 0)) - holds_before
	_ok("машина пересекла границу элемента", crossings >= 2,
		"переходов %d, элемент %s -> %s" % [crossings, element_before.substr(0, 13),
			String(w.stats.get("stock_element", "")).substr(0, 13)])
	_ok("на границе элемента показ не дёрнулся", crossings >= 2 and jerks == 0,
		"рывков %d, худший: %s" % [jerks, str(w.stats.get("display_jerk_worst", "нет"))])
	# ДЕРЖАНИЕ СНИМКА — то самое прежнее поведение, и ноль здесь означает, что
	# показ ни разу не откатился к нему. Ненулевое — разговор про канал: общей
	# цепочки у двух снимков не нашлось.
	_ok("показ ни разу не держал снимок вместо досчёта по пути", holds == 0,
		"держаний %d" % holds)

	# Останавливаемся, чтобы следующая проверка застала машину в покое.
	var stopping := Time.get_ticks_msec() + 12000
	while Time.get_ticks_msec() < stopping and float(w.stats.get("stock_speed_ms", 0.0)) != 0.0:
		await process_frame

	# И ВОЗВРАЩАЕМСЯ, ОТКУДА ПРИЕХАЛИ.
	#
	# Не ради порядка: партия живёт в памяти сервера и переживает прогон зонда.
	# Перегон уводит машину на два элемента вперёд, и следующий запуск зонда
	# начинал бы с другого места — а он и начал: третий подряд прогон отказал
	# «проехала 0.0 м за 5 с», потому что машина стояла у края карты. Зонд,
	# который нельзя запустить дважды, чинит себя сам ровно один раз.
	if not await _free_the_brakes(cab):
		_ok("после перегона тормоз отпущен", false,
			"кран %s, цилиндр %.2f" % [cab.handle, cab.cylinder_pressure / 1000.0])
		return
	_press(KEY_R)
	await _settled(cab)
	for i in range(10):
		_press(KEY_2)
		await _settled(cab)
	var home := Time.get_ticks_msec() + 30000
	var back := 0
	while Time.get_ticks_msec() < home:
		await process_frame
		if String(w.stats.get("stock_element", "")) == element_before:
			if back == 0:
				back = Time.get_ticks_msec() + 3000
			elif Time.get_ticks_msec() > back:
				break
	_press(KEY_0)
	await _settled(cab)
	var rest := Time.get_ticks_msec() + 12000
	while Time.get_ticks_msec() < rest and float(w.stats.get("stock_speed_ms", 0.0)) != 0.0:
		await process_frame
	_ok("машина вернулась на элемент, с которого ушла",
		String(w.stats.get("stock_element", "")) == element_before,
		"элемент %s, ожидался %s" % [String(w.stats.get("stock_element", "")).substr(0, 13),
			element_before.substr(0, 13)])


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
	var to_machine := _focus_gap(orbit, machine)
	var to_person := _plan_gap(orbit.focus, (w._driver as Node3D).global_position)
	_ok("обзор наведён на машину", to_machine < 0.5, "до машины %.2f м в плане" % to_machine)
	_ok("а не на человека", to_machine <= to_person, "до человека %.2f м в плане" % to_person)


## _focus_gap — НА МАШИНЕ ЛИ ФОКУС, метры В ПЛАНЕ.
##
## В плане, а не в пространстве, и это не ослабление проверки. Точка взгляда
## обзорного вида поднята в СЕРЕДИНУ КУЗОВА (Driver._orbit_focus, разбор там же:
## кадру — прицел, упору камеры в землю — луч, выходящий из воздуха), а узел
## машины стоит на головке рельса. Разница по высоте между ними ЗАКОННА и равна
## полувысоте паспорта; уход в сторону — нет, и мерить надо его. До 2026-08-18
## здесь стояло расстояние в пространстве, и подъём фокуса оно прочитало как
## «камера потеряла машину на 2.55 м».
func _focus_gap(orbit: OrbitCamera, machine: Node3D) -> float:
	return _plan_gap(orbit.focus, machine.global_position)


func _plan_gap(a: Vector3, b: Vector3) -> float:
	return Vector2(a.x - b.x, a.z - b.z).length()


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

## _wait_until — ждать условия кадрами, не дольше срока. Нужен пневматике:
## команда мгновенна, а зарядка магистрали и опорожнение цилиндра занимают
## СЕКУНДЫ, и проверять их сразу после нажатия значило бы проверять мир, в
## котором тормоз отпускается щелчком.
func _wait_until(cond: Callable, seconds: float) -> bool:
	var deadline := Time.get_ticks_msec() + int(seconds * 1000.0)
	while Time.get_ticks_msec() < deadline:
		if bool(cond.call()):
			return true
		await process_frame
	return bool(cond.call())


## _handle_to — ВЕСТИ КРАН ПО СЕКТОРУ ДО НАЗВАННОГО ПОЛОЖЕНИЯ, в ту сторону, в
## которую идти надо.
##
## Заведена вместо двух одинаковых циклов «жми D, пока не служебное», и второй из
## них ВИСЕЛ НАСМЕРТЬ. Перед ним зонд жмёт «0», а она ставит кран в экстренное —
## последнее положение сектора. D дальше не идёт (Cab.shift_handle упирается в
## предел и возвращает false), команда не уходит, cab.handle не меняется, и цикл
## крутится вечно. Владелец увидел это как «тест в конце висит просто так».
##
## Число нажатий ОГРАНИЧЕНО длиной сектора: зонд, не доехавший до положения за
## шесть шагов, обязан сказать об этом отказом, а не молчанием на сутки.
func _handle_to(cab: Cab, want: String) -> bool:
	for _i in Cab.HANDLES.size():
		if cab.handle == want:
			return true
		var at := Cab.HANDLES.find(cab.handle)
		var to := Cab.HANDLES.find(want)
		if at < 0 or to < 0:
			return false
		_press(KEY_D if to > at else KEY_A)
		await _settled(cab)
	return cab.handle == want


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


## _spread — размах ряда: сколько, медиана, худшее. Медиана, а не среднее:
## одиночный скачок среднее сдвигает, а медиану — нет, и разница между ними и
## есть то, что мы ищем.
func _spread(v: Array[float]) -> String:
	if v.is_empty():
		return "нет кадров"
	var s := v.duplicate()
	s.sort()
	return "%d кадров, медиана %.3f, макс %.3f м" % [s.size(), s[s.size() / 2], s[s.size() - 1]]


## _free_the_brakes — ОТПУСТИТЬ ТОРМОЗ ЦЕЛИКОМ И ДОЖДАТЬСЯ, пока колодки отойдут.
##
## Рецепт целиком куплен отказами зонда, и обе его половины — отдельными:
##
##   КРАН МАШИНИСТА. Прошлая проба кончает «всё в ноль», то есть ПОЛНЫМ
##     тормозом, и машина под ним никуда не поедет («0.0 м за 5 с»). Ручка
##     ведётся к отпуску до упора, потом одним шагом в поездное.
##   ВСПОМОГАТЕЛЬНЫЙ. Он держит цилиндр НЕЗАВИСИМО от магистрали — в том и смысл
##     крана № 254, — поэтому заряженная магистраль колодки не отпускает, пока не
##     отпущен он (отказ: кран в поездном, магистраль 5.40, цилиндр 4.00, машина
##     проехала 0.1 м за 5 с).
##
## И ЖДЁМ. Команда мгновенна, пневматика — нет; «поставил и поехал» проверяло бы
## мир, в котором тормоз отпускается щелчком.
##
## ОДНИМ МЕСТОМ, а не по копии на каждый перегон: вторая копия разошлась бы с
## первой на первой же правке пневматики, и разошлась бы молча — перегон просто
## не поехал бы, а виноватым выглядел бы мир.
func _free_the_brakes(cab: Cab) -> bool:
	for i in range(Cab.HANDLES.size()):
		_press(KEY_A)
		await _settled(cab)
	_press(KEY_D)
	await _settled(cab)
	for i in range(12):
		_press(KEY_Z)
		await _settled(cab)
	return await _wait_until(func() -> bool: return cab.cylinder_pressure == 0, 20.0)


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
