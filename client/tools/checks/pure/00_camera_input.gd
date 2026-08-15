## УПРАВЛЕНИЕ КАМЕРОЙ — чистая проверка: жесты к серверу не относятся.
##
## Проверка заведена 2026-08-12, когда владелец сказал «управление на mac не
## работает, а в спайке работало». Работало: лечение жило в spike_world.gd
## (809280f^) и при переносе в OrbitCamera потерялось — осталось только колесо
## мыши. macOS отличает мышь от трекпада по фазе события прокрутки: у колеса фазы
## нет, у двупальцевого скролла есть, и вместо колеса приходит
## InputEventPanGesture, а щипок приходит InputEventMagnifyGesture. Колеса на
## трекпаде не бывает ВОВСЕ.
##
## Почему не снимком: снимок доказывает, что нарисовано, но не знает, ЧЕМ его
## навели, — камера, поставленная configure и не слушающая ни одного жеста, даёт
## тот же кадр. Почему не прямым вызовом _unhandled_input: он доказал бы только,
## что метод существует. События синтезируются Input.parse_input_event и идут той
## же дорогой, что от движка, — через окно, GUI и лишь потом _unhandled_input.
##
## ЧЕГО ЭТА ПРОВЕРКА НЕ ДОКАЗЫВАЕТ: что macOS присылает именно такие события.
## Это свойство движка, а не наше; здесь проверено, что НА ТАКИЕ СОБЫТИЯ КАМЕРА
## ОТВЕЧАЕТ. Обратное — что на маке жест доезжает до окна — проверяется руками.
##
## Она стояла первой и до разноса по файлам — тогда потому, что разбор манифеста
## уходил из прогона при первом же отказе и утаскивал её с собой. Теперь она в
## checks/pure, и погашенный сервер её не задевает вовсе.
extends "res://tools/check_suite.gd"


func run() -> void:
	# Накопление ввода выключено на время проверки: движения мыши иначе копятся
	# до конца кадра, и «нажал-подвигал» разбиралось бы не там, где ожидает
	# await. Возвращаем как было — свойство глобальное.
	var accum := Input.is_using_accumulated_input()
	Input.set_use_accumulated_input(false)

	var cam := OrbitCamera.new()
	ctx.tree.root.add_child(cam)
	cam.configure(Vector3.ZERO, 300.0, 205.0, 45.0, true)

	# Колесо — то, что работало и обязано работать дальше.
	var base := cam.frame_size_m
	await _feed(_wheel(MOUSE_BUTTON_WHEEL_UP))
	_ok("орбита: колесо вверх приближает", cam.frame_size_m < base,
		"кадр %.1f → %.1f м" % [base, cam.frame_size_m])

	# Двупальцевый скролл. Положительный delta.y — от себя, то есть отдалить.
	base = cam.frame_size_m
	await _feed(_pan(Vector2(0.0, 4.0)))
	_ok("орбита: двупальцевый скролл отдаляет", cam.frame_size_m > base,
		"кадр %.1f → %.1f м" % [base, cam.frame_size_m])

	# Щипок. factor > 1 — пальцы развели, то есть приблизить.
	base = cam.frame_size_m
	await _feed(_magnify(1.5))
	_ok("орбита: щипок приближает", cam.frame_size_m < base,
		"кадр %.1f → %.1f м" % [base, cam.frame_size_m])

	# Клавиши — средство на любом вводе, включая тачпад без жестов.
	base = cam.frame_size_m
	await _feed(_key(KEY_MINUS))
	_ok("орбита: минус отдаляет", cam.frame_size_m > base,
		"кадр %.1f → %.1f м" % [base, cam.frame_size_m])
	base = cam.frame_size_m
	await _feed(_key(KEY_EQUAL))
	_ok("орбита: равно приближает", cam.frame_size_m < base,
		"кадр %.1f → %.1f м" % [base, cam.frame_size_m])

	# Shift+ЛКМ — панорама, ЛКМ без Shift — орбита. Модификатор читается У
	# СОБЫТИЯ, и проверяется здесь именно это: событию Shift объявлен, а клавиша
	# при этом физически не нажата — опрос Input.is_key_pressed дал бы орбиту.
	var az := cam.azimuth_deg
	var focus_before := cam.focus
	await _feed(_drag(Vector2(24.0, 0.0), true))
	_ok("орбита: Shift у события даёт панораму, а не орбиту",
		is_equal_approx(cam.azimuth_deg, az) and cam.focus != focus_before,
		"азимут %.2f→%.2f, фокус сдвинут на %.2f м" % [az, cam.azimuth_deg,
			(cam.focus - focus_before).length()])
	az = cam.azimuth_deg
	await _feed(_drag(Vector2(24.0, 0.0), false))
	_ok("орбита: ЛКМ без Shift крутит", not is_equal_approx(cam.azimuth_deg, az),
		"азимут %.2f→%.2f" % [az, cam.azimuth_deg])

	# Погашенная камера не слушает ничего: поверх мира стоит меню.
	cam.set_active(false)
	base = cam.frame_size_m
	await _feed(_pan(Vector2(0.0, 4.0)))
	_ok("орбита: в паузе жест не доходит", is_equal_approx(cam.frame_size_m, base),
		"кадр %.1f" % cam.frame_size_m)
	cam.free()

	# ЧЕЛОВЕК МАШИНИСТА. Здесь до 2026-08-12 (вечера) проверялась свободная камера:
	# колеса на трекпаде нет и у неё, а от колеса зависела скорость полёта — то
	# есть роль оставалась без управления вовсе. Камера снесена вместе с
	# free_camera.gd: у машиниста теперь ЧЕЛОВЕК, и вопрос к нему другой.
	#
	# Из трёх его видов один переключается клавишей, и это единственный орган,
	# который можно проверить без мира: взгляд мышью и ходьба требуют тверди под
	# ногами, то есть присланного рельефа, и меряются зондом ходьбы (tools/
	# walk_probe.gd) — ровно как в спайке, откуда человек перенесён.
	var driver := Driver.new()
	ctx.tree.root.add_child(driver)
	var view_before := driver.mode_name()
	await _feed(_key(KEY_V))
	_ok("машинист: V переключает вид", driver.mode_name() != view_before,
		"вид «%s» → «%s»" % [view_before, driver.mode_name()])

	# ПОСАДКА. Мира здесь нет, и он не нужен: пост — это узел и точка в его осях,
	# а «дотянусь ли» считается по расстоянию в плане. Что глаз при этом попадает
	# внутрь НАРИСОВАННОЙ кабины, доказывает зонд по доехавшему мешу — здесь
	# доказывается ввод и досягаемость.
	var rig := Node3D.new()
	ctx.tree.root.add_child(rig)
	var post := Driver.Post.new()
	post.unit_id = "LOCO_T"
	post.node = rig
	post.local = Vector3(0.0, 1.72, 15.5)
	driver.set_posts([post] as Array[Driver.Post])

	# ВДАЛИ — НИ ПОДСКАЗКИ, НИ ПОСАДКИ. Предложение показывают тогда, когда оно
	# настоящее, а E, сработавшая с сорока метров, — телепорт, а не посадка.
	driver.global_position = Vector3(0.0, 0.0, -25.0)
	await _feed(_key(KEY_E))
	_ok("машинист: вдали от машины E не сажает", not driver.is_boarded(),
		"до поста %.1f м" % [40.5])
	_ok("машинист: вдали подсказки нет", driver.prompt() == "",
		"подсказка «%s»" % driver.prompt())

	# ВБЛИЗИ — ПОДСКАЗКА, и она появляется САМА, от того, что человек подошёл.
	# Ждать её надо шага физики: расстояние считается там, а не в разборе ввода.
	driver.global_position = post.at() - Vector3(0.0, 1.72, 2.0)
	await ctx.tree.physics_frame
	await ctx.tree.process_frame
	_ok("машинист: у машины появляется подсказка", driver.prompt() != "",
		"подсказка «%s»" % driver.prompt())

	# ПРИВОД СТРЕЛКИ — второе, до чего человек дотягивается руками, и правило у
	# него своё: досягаемость короче (STAND_REACH_M против BOARD_REACH_M), потому
	# что стрелку берут за сам механизм, а не тянутся к посту на высоте кабины.
	#
	# Стрелка здесь собирается ФИКСТУРОЙ, а не берётся из сети: проверяется
	# досягаемость и подсказка, а не разбор провода (его проверяет 48_turnout_drive).
	var probe_drive := TrackBuild.TurnoutDrive.new()
	probe_drive.owner = "SW_T"
	probe_drive.label = "12"
	probe_drive.drive = "manual"
	probe_drive.pose = TrackGeom.AxisPoint.new(0.0, 0.0, 0.0, 0.0, 0.0)
	var probe_stand := SwitchStand.build(probe_drive, 0.0)
	ctx.tree.root.add_child(probe_stand)
	probe_stand.global_position = Vector3(0.0, 0.0, -60.0)
	driver.set_stands([probe_stand])
	await ctx.tree.physics_frame
	_ok("машинист: вдали от стрелки её не предлагают",
		not driver.prompt().contains("перевести"), "подсказка «%s»" % driver.prompt())
	_ok("машинист: вдали привод не выбран целью", driver.nearest_stand().is_empty())
	probe_stand.global_position = driver.global_position + Vector3(0.0, 0.0, 2.0)
	await ctx.tree.physics_frame
	await ctx.tree.process_frame
	_ok("машинист: у стрелки предлагают её перевести",
		driver.prompt().contains("перевести"), "подсказка «%s»" % driver.prompt())
	_ok("машинист: в подсказке названа именно эта стрелка",
		driver.prompt().contains("12"), "подсказка «%s»" % driver.prompt())
	_ok("машинист: цель клавиши — тот же привод",
		not driver.nearest_stand().is_empty()
		and (driver.nearest_stand()["stand"] as SwitchStand).owner_id == "SW_T")
	# Убираем привод, иначе его подсказка мешается посадке ниже: две строки в
	# одной подсказке законны, но проверка посадки ищет свою.
	driver.set_stands([])
	probe_stand.queue_free()
	await ctx.tree.physics_frame

	await _feed(_key(KEY_E))
	_ok("машинист: у машины E сажает в кабину", driver.is_boarded())
	# Пол кабины — не земля: сев, человек обязан оказаться ТАМ, где объявлен пост.
	_ok("машинист: сев, стоит на посту",
		driver.global_position.distance_to(post.at()) < 0.01,
		"разошлось на %.3f м" % driver.global_position.distance_to(post.at()))
	await ctx.tree.physics_frame
	_ok("машинист: в кабине подсказка предлагает выйти",
		driver.prompt().contains("выйти"), "подсказка «%s»" % driver.prompt())

	await _feed(_key(KEY_E))
	_ok("машинист: E выпускает из кабины", not driver.is_boarded())

	# F РАБОТАЕТ И ИЗ КАБИНЫ — это средство от застревания, и отказывать оно не
	# вправе (решение владельца, довод — Derail Valley). Проверяется именно
	# ВЫСАДКА: оставь пост занятым, и тело в тот же шаг физики уехало бы обратно.
	await _feed(_key(KEY_E))
	_ok("машинист: сел обратно", driver.is_boarded())
	await _feed(_key(KEY_F))
	_ok("машинист: F выносит из кабины", not driver.is_boarded())

	# Погашенный человек не слушает ничего: поверх мира стоит меню. Та же грабля,
	# что у камеры выше, и куплена она была именно на роли машиниста — событие,
	# дошедшее до мира из-под меню, снаружи неотличимо от сломанного управления.
	driver.set_active(false)
	view_before = driver.mode_name()
	await _feed(_key(KEY_V))
	_ok("машинист: в паузе вид не переключается", driver.mode_name() == view_before,
		"вид «%s»" % driver.mode_name())
	driver.global_position = post.at()
	await _feed(_key(KEY_E))
	_ok("машинист: в паузе E не сажает", not driver.is_boarded())
	_ok("машинист: в паузе подсказка погашена", driver.prompt() == "",
		"подсказка «%s»" % driver.prompt())
	driver.free()
	rig.free()

	Input.set_use_accumulated_input(accum)


## _feed — отдать событие движку и дождаться, пока оно разойдётся по дереву.
## Кадр ждётся, а не форсируется: тот же урок, что записан про снимок в шапке
## Makefile — ждать надо события, а не удобного момента.
##
## ЖЕСТ ЧЕРЕЗ Input.parse_input_event ПРИХОДИТ ДВАЖДЫ, и это замер, а не догадка:
## счётчиком вызовов _unhandled_input на Godot 4.7.1 получено 2 для
## InputEventPanGesture и InputEventMagnifyGesture против 1 для колеса и клавиши;
## тот же жест через root.push_input приходит один раз. Значит удвоение живёт в
## Input, а не в окне. Поэтому проверки выше смотрят НАПРАВЛЕНИЕ, а не величину:
## «кадр стал больше», а не «кадр вырос в 1.06^4 раза».
##
## Из этого же следует, что ZOOM_PAN_BASE трогать по этим числам НЕЛЬЗЯ. macOS
## отдаёт жест в тот же Input.parse_input_event, то есть удвоение получал и
## спайк, на котором владелец эту скорость и признал верной.
func _feed(event: InputEvent) -> void:
	Input.parse_input_event(event)
	Input.flush_buffered_events()
	await ctx.tree.process_frame


func _wheel(button: int) -> InputEventMouseButton:
	var e := InputEventMouseButton.new()
	e.button_index = button
	e.pressed = true
	return e


func _pan(delta: Vector2) -> InputEventPanGesture:
	var e := InputEventPanGesture.new()
	e.delta = delta
	return e


func _magnify(factor: float) -> InputEventMagnifyGesture:
	var e := InputEventMagnifyGesture.new()
	e.factor = factor
	return e


func _key(code: Key) -> InputEventKey:
	var e := InputEventKey.new()
	e.keycode = code
	e.pressed = true
	return e


func _drag(rel: Vector2, shift: bool) -> InputEventMouseMotion:
	var e := InputEventMouseMotion.new()
	e.relative = rel
	e.button_mask = MOUSE_BUTTON_MASK_LEFT
	e.shift_pressed = shift
	return e
