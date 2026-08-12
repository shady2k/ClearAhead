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

	# Погашенный человек не слушает ничего: поверх мира стоит меню. Та же грабля,
	# что у камеры выше, и куплена она была именно на роли машиниста — событие,
	# дошедшее до мира из-под меню, снаружи неотличимо от сломанного управления.
	driver.set_active(false)
	view_before = driver.mode_name()
	await _feed(_key(KEY_V))
	_ok("машинист: в паузе вид не переключается", driver.mode_name() == view_before,
		"вид «%s»" % driver.mode_name())
	driver.free()

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
