extends SceneTree
## ЗОНД ПРОИЗВОДИТЕЛЬНОСТИ СПАЙКА МИРА. Отвечает числами на вопрос «почему
## тормозит», вместо перебора догадок.
##
## Мерит две РАЗНЫЕ вещи, которые на глаз сливаются в одно «лагает»:
##
##   1. СТОИМОСТЬ КАДРА на разных дистанциях камеры — это про то, что рисуется:
##      сколько вызовов отрисовки и примитивов в кадре. Отдельно с локомотивом и
##      без него, потому что подозрение на готовую модель надо либо подтвердить,
##      либо снять, а не носить с собой.
##   2. СТОИМОСТЬ ПЕРЕСАДКИ ТРАВЫ (_add_grass) — это про то, что СЧИТАЕТСЯ, и
##      считается оно в главном потоке, то есть кадр на это время встаёт колом.
##      Зум её и вызывает: радиус посадки привязан к охвату кадра.
##
## Разделять обязательно. Ровный низкий фпс и рывок раз в несколько кадров
## лечатся разным, а выглядят похоже — и первое, что делают, перепутав их, это
## оптимизируют не то.
##
##   godot --rendering-method forward_plus --path client \
##       --script res://tools/perf_probe.gd

const DISTANCES := [30.0, 120.0, 420.0, 1500.0]
const FRAMES := 40               # кадров в замере
const SETTLE := 45               # кадров на устаканивание (трава ждёт 0.35 с)
const ZOOM_FROM := 30.0          # м — откуда прогоняется зум
const ZOOM_TO := 1500.0          # м — докуда
const ZOOM_STEP := 1.06          # множитель за кадр: так же зумит трекпад
const PAN_FRAMES := 260          # кадров непрерывной панорамы
const PAN_SPEED := 1.2           # м за кадр — примерно как WASD с Shift

var _node: Node3D
var _started := false
var _done := false

func _initialize() -> void:
	_node = (load("res://scenes/spike_world.tscn") as PackedScene).instantiate()
	root.add_child(_node)

func _process(_delta: float) -> bool:
	if not _started:
		_started = true
		_run()
	return _done

func _run() -> void:
	# Вертикальная синхронизация КАПАЕТ НА ИЗМЕРЕНИЕ: с ней всякий кадр легче
	# 16.7 мс округляется до 16.7, и разницу между 4 мс и 15 мс не увидеть.
	DisplayServer.window_set_vsync_mode(DisplayServer.VSYNC_DISABLED)

	# ЛОКОМОТИВ СРАВНИВАЕТСЯ НА ОДНОЙ И ТОЙ ЖЕ ДИСТАНЦИИ, ВКЛ/ВЫКЛ ПОДРЯД.
	# Первый заход мерил все дистанции с ним, потом все без — и между заходами
	# успевала пересесть трава. Счётчики застывали на числах последнего кадра, и
	# «без ВЛ80» выходило одинаковым на 30 и на 1500 м, чего не бывает.
	print("PERF: кадр (трава заморожена, чтобы её пересадка не попала в замер)")
	for d in DISTANCES:
		var on := await _measure_at(d, true)
		var off := await _measure_at(d, false)
		print("PERF: дистанция %6.0f м  с ВЛ80 %6.2f мс / %4d выз. / %8d прим.   без %6.2f мс / %4d выз. / %8d прим."
			% [d, on.ms, on.draw, on.prim, off.ms, off.draw, off.prim])
	if _node._loco_node != null:
		_node._loco_node.visible = true

	# ДОЛЯ ТРАВЫ В КАДРЕ. 16.5 млн примитивов — это ВЕСЬ кадр (земля, лес, кусты,
	# путь, плюс проходы теней), и приписывать их траве нельзя. Вопрос «упрусь ли
	# я в отрисовку раньше, чем в постройку» решается только этим замером: что
	# останется, если траву выключить.
	print("PERF: доля травы в кадре")
	for d in [30.0, 120.0, 420.0]:
		var on := await _measure_grass(d, true)
		var off := await _measure_grass(d, false)
		print("PERF: дистанция %6.0f м  с травой %6.2f мс / %4d выз. / %8d прим.   без %6.2f мс / %4d выз. / %8d прим."
			% [d, on.ms, on.draw, on.prim, off.ms, off.draw, off.prim])
	if _node._grass_root != null:
		_node._grass_root.visible = true

	print("PERF: пересадка травы (_add_grass) — главный поток стоит на это время")
	for d in DISTANCES:
		_node._cam_dist = d
		_node._cam_ortho = false
		_node._apply_camera()
		await process_frame
		var t0 := Time.get_ticks_usec()
		_node._add_grass()
		var t1 := Time.get_ticks_usec()
		print("PERF: охват %6.0f м  пересадка %7.1f мс" % [_node._view_extent(), float(t1 - t0) / 1000.0])

	# ГЛАВНАЯ ПРОВЕРКА: сколько раз ЗУМ запускает пересадку. Стоимость одной
	# пересадки — половина ответа; вторая половина в том, сколько их случается.
	# Зум прогоняется так же, как его делает рука: множителем на шаг, а не
	# прыжком, — иначе пороги пересекаются разом и счёт выходит другой.
	_node._cam_dist = ZOOM_FROM
	_node._cam_ortho = false
	_node._apply_camera()
	_node._add_grass()
	for i in SETTLE:
		await process_frame
	var builds0: int = _node._grass_builds
	while _node._cam_dist < ZOOM_TO:
		_node._cam_dist = minf(_node._cam_dist * ZOOM_STEP, ZOOM_TO)
		_node._apply_camera()
		await process_frame
	# Пересадка отложена на 0.35 с после остановки — дожидаемся её.
	for i in 90:
		await process_frame
	print("PERF: зум %.0f -> %.0f м: пересадок травы %d (было бы ~10 при радиусе по охвату)"
		% [ZOOM_FROM, ZOOM_TO, _node._grass_builds - builds0])

	# ЗУМ ПОСЛЕ ПАНОРАМЫ — отдельный сценарий, и жалоба владельца именно про него.
	# Сам по себе зум пересадку не запускает, но панорама оставляет её ОТЛОЖЕННОЙ,
	# и сработать она может уже во время зума. Считаем порознь: сколько на самой
	# панораме и сколько после неё на зуме.
	for shift in [60.0, 300.0]:
		_node._cam_dist = ZOOM_FROM
		_node._apply_camera()
		_node._add_grass()
		for i in SETTLE:
			await process_frame
		var b0: int = _node._grass_builds
		_node._cam_focus += Vector3(shift, 0.0, 0.0)
		_node._apply_camera()
		for i in SETTLE:
			await process_frame
		var b_pan: int = _node._grass_builds - b0
		# Второй заход — БЕЗ ПАУЗЫ между панорамой и зумом: так и делает рука, и
		# именно так отложенная на 0.35 с пересадка попадает в середину зума.
		_node._cam_focus += Vector3(shift, 0.0, 0.0)
		_node._apply_camera()
		var b_before_zoom: int = _node._grass_builds
		while _node._cam_dist < ZOOM_TO:
			_node._cam_dist = minf(_node._cam_dist * ZOOM_STEP, ZOOM_TO)
			_node._apply_camera()
			await process_frame
		var b_in_zoom: int = _node._grass_builds - b_before_zoom
		for i in 90:
			await process_frame
		print("PERF: сдвиг %4.0f м: с паузой построек %d · без паузы, ВО ВРЕМЯ ЗУМА %d"
			% [shift, b_pan, b_in_zoom])

	# САМЫЙ ХУДШИЙ КАДР ПРИ НЕПРЕРЫВНОЙ ПАНОРАМЕ — то, на что жаловался владелец.
	# Средний кадр тут бесполезен: замирание на две секунды в среднем по сотне
	# кадров растворяется, а глазом видно именно его.
	_node._cam_dist = 120.0
	_node._apply_camera()
	_node._add_grass()
	for i in SETTLE:
		await process_frame
	var worst := 0.0
	var t_prev := Time.get_ticks_usec()
	for i in PAN_FRAMES:
		_node._cam_focus += Vector3(PAN_SPEED, 0.0, 0.0)
		_node._apply_camera()
		await process_frame
		var t_now := Time.get_ticks_usec()
		worst = maxf(worst, float(t_now - t_prev) / 1000.0)
		t_prev = t_now
	# УСПЕВАЕТ ЛИ ПОСАДКА ЗА КАМЕРОЙ. Без этого «худший кадр 20 мс» ничего не
	# доказывает: не строить вовсе тоже дёшево, только под ногами будет голо.
	var bald := _count_bald()
	print("PERF: панорама %.0f м подряд: ХУДШИЙ кадр %.1f мс (было ~2170 мс одним куском), "
		% [PAN_FRAMES * PAN_SPEED, worst]
		+ "ГОЛЫХ чанков %d из %d, не в своей подробности %d"
		% [bald[0], _node._grass_want.size(), bald[1]])
	for k in [120, 240, 480]:
		for i in k:
			await process_frame
		bald = _count_bald()
		print("PERF: ещё %d кадров спустя: голых %d, не в своей подробности %d"
			% [k, bald[0], bald[1]])
	_done = true

## Траву держим на месте: центр круга совмещается с точкой взгляда, и условие
## пересадки (камера ушла от центра) не срабатывает. Иначе в окне замера
## оказалась бы двухсекундная пересадка, размазанная по кадрам, — на этом первый
## заход и врал.
func _measure_grass(dist: float, with_grass: bool) -> Dictionary:
	if _node._grass_root != null:
		_node._grass_root.visible = with_grass
	return await _measure_at(dist, true)

func _measure_at(dist: float, with_loco: bool) -> Dictionary:
	if _node._loco_node != null:
		_node._loco_node.visible = with_loco
	_node._cam_dist = dist
	_node._cam_ortho = false
	_node._apply_camera()
	_node._grass_center = _node._cam_focus
	for i in SETTLE:
		await process_frame
	var t0 := Time.get_ticks_usec()
	var draw := 0
	var prim := 0
	for i in FRAMES:
		await process_frame
		draw = maxi(draw, RenderingServer.get_rendering_info(RenderingServer.RENDERING_INFO_TOTAL_DRAW_CALLS_IN_FRAME))
		prim = maxi(prim, RenderingServer.get_rendering_info(RenderingServer.RENDERING_INFO_TOTAL_PRIMITIVES_IN_FRAME))
	var t1 := Time.get_ticks_usec()
	return {"ms": float(t1 - t0) / 1000.0 / float(FRAMES), "draw": draw, "prim": prim}

## Голый чанк — у которого НЕТ НИ ОДНОГО построенного уровня: под ногами земля.
## Это и видно глазу. Несовпадение подробности глазу почти не видно, поэтому
## считается отдельно и спросу с него меньше.
func _count_bald() -> Array:
	var bald := 0
	var coarse := 0
	for flat in _node._grass_want.keys():
		var want: int = _node._grass_want[flat]
		var any := false
		for k in 5:
			if _node._grass_chunks.has(Vector3i(flat.x, flat.y, k)):
				any = true
				break
		if not any:
			bald += 1
		elif not _node._grass_chunks.has(Vector3i(flat.x, flat.y, want)):
			coarse += 1
	return [bald, coarse]
