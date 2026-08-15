## OrbitCamera — камера, вращающаяся вокруг точки на земле. Порт из снесённого
## спайка (`spike_relief.gd`, 809280f^), а не сочинение заново.
##
## # ОНА ТЕПЕРЬ ОДНА НА ВСЕ ТРИ РОЛИ
##
## Рядом жила вторая — FreeCamera, наблюдатель, ЛЕТАЮЩИЙ по миру: у неё было
## положение и взгляд, и оба менялись независимо. Ею смотрел машинист. Снесена
## 2026-08-12 (вечер) вместе с файлом, и довод владельца записан дословно:
## «машинист сейчас — это не персонаж, а просто летающая камера». Летающая камера
## на высоте глаза даёт похожий кадр и не проверяет ничего — у неё нет ни роста,
## ни тени, ни походки. Теперь у машиниста ЧЕЛОВЕК (driver.gd), а эта камера
## достаётся ему ОБЗОРНЫМ ВИДОМ — орбитой вокруг него самого.
##
## Что осталось от разницы двух камер: в видах от первого и третьего лица эту
## камеру ведёт голова человека, и на время этих видов у неё отбирают жесты
## (Driver.set_active). Смешанного органа управления по-прежнему нет — есть два,
## и переключаются они целиком, а не посреди жеста.
##
## # Что здесь ЗАКОННО константами
##
## Азимут, возвышение, ширина кадра, скорость орбиты и зума — свойства ВЗГЛЯДА.
## Граница ClearAhead-sjq разрешает клиенту камеру целиком: «вид не есть мир».
##
## И чего здесь НЕТ, хотя у спайка было: ТОЧКИ ФОКУСА КОНСТАНТОЙ. Снесённый
## клиент держал `shot_focus = Vector2(240.0, 0.0)` и `size = 300.0` — числа,
## взятые ниоткуда, и это одна из причин, по которым тот клиент снесли. Фокус
## приходит СНАРУЖИ: из габаритов того, что приехало с сервера, либо от человека,
## если роль пешая.
class_name OrbitCamera
extends Camera3D

## Поле зрения перспективного режима. Оно же переводит ширину орто-кадра в
## дистанцию, чтобы переключение проекции не прыгало по масштабу.
const CAM_FOV := 50.0

const ORBIT_RATE := 0.30      # градусов на пиксель
const ZOOM_STEP := 0.88       # во сколько раз меняется масштаб за щелчок колеса
## Основание для НЕПРЕРЫВНОГО зума жестом. У колеса есть щелчок и постоянный
## шаг, у двупальцевого скролла — только величина смещения, и шаг для него
## обязан считаться степенью. Число перенесено из спайка вместе с остальным
## лечением мака: 1.06 в степени типичного delta.y даёт на трекпаде примерно ту
## же скорость, что щелчок колеса на мыши.
const ZOOM_PAN_BASE := 1.06
const PAN_RATE := 0.55        # доля видимого поперечника в секунду
const PAN_FAST := 4.0         # множитель с Shift

const ELEV_MIN := 2.0
const ELEV_MAX := 89.0
const SIZE_MIN := 8.0
const DIST_MIN := 8.0
## ВЕРХНИЕ ПРЕДЕЛЫ БЕЗ МИРА. Здесь стояли SIZE_MAX = 8000 и DIST_MAX = 20000 как
## настоящий потолок зума, и это было ошибкой того же рода, что зашитая точка
## фокуса: камера, не знающая ни дальности взгляда, ни охвата региона, объявляла
## своим числом, сколько мира можно показать.
##
## Кадр владельца, которым это поймано: отъезд на восемь километров при мире,
## загруженном на два. Дымка выводится из дальности взгляда, и на восьми
## километрах она съедает 99.9 % — то есть камера смотрела дальше, чем сама же
## объявила видимость, и показывала белое пятно.
##
## Действующий потолок приходит СНАРУЖИ (WorldBounds), как приходят углы и точка
## фокуса. Эти два числа остались тем, чем и обязаны быть, — защитой от
## бесконечности там, где мира ещё нет: до загрузки и в проверках.
const SIZE_CEILING := 8000.0
const DIST_CEILING := 20000.0

## Упор изменился: доехали до края мира или отошли от него. Строкой, а не
## булевым: панель обязана назвать ПРИЧИНУ, иначе глухая стена неотличима от
## поломки. Пустая строка значит «упора нет».
signal limit_changed(note: String)

var focus := Vector3.ZERO      # точка интереса в осях Godot
var azimuth_deg := 205.0
var elev_deg := 45.0
var frame_size_m := 300.0      # видимая высота кадра в ортографии
var ortho := true

## Край мира. Пока его нет (камера построена, мир не загружен), камера ходит по
## потолкам выше — иначе её пришлось бы держать неподвижной до конца загрузки.
var bounds: WorldBounds = null

var _dist := 300.0
var _limit_note := ""
## Пока камера не в игре, ввод ей не принадлежит: в паузе и в меню жесты мыши
## обязаны доставаться кнопкам, а не станции под ними.
var _active := true
## Клавиатурная панорама. Мышь от этого флага не зависит — разбор у set_keys.
var _keys := true


## configure — навестись на то, что приехало.
##
## centre и size_m считает вызывающий из ГАБАРИТОВ ДАННЫХ. Углы и проекция —
## свойства взгляда и приходят от роли.
func configure(centre: Vector3, size_m: float, az_deg: float, el_deg: float, orthographic: bool) -> void:
	focus = centre
	frame_size_m = clampf(size_m, SIZE_MIN, SIZE_CEILING)
	azimuth_deg = az_deg
	elev_deg = clampf(el_deg, ELEV_MIN, ELEV_MAX)
	ortho = orthographic
	_dist = _dist_for(frame_size_m)
	apply()


func set_active(on: bool) -> void:
	_active = on


## set_keys — отдать или забрать у камеры КЛАВИАТУРНУЮ панораму, не трогая мышь.
##
## Отдельно от set_active, потому что это разные вопросы. set_active отвечает
## «смотрит ли игрок этой камерой»; здесь — «чьи сейчас буквы». В кабине камера
## по-прежнему смотрит и по-прежнему водится мышью, а WASD принадлежит машине:
## ноги у сидящего выключены, и клавиши свободны.
##
## Заведено 2026-08-15 по слову владельца: «когда мы в кабине, мы управляем
## камерой мышью, как это уже реализовано во многих играх». До того W в кабине
## панорамировал обзор, и снаружи это выглядело так, будто машинист не управляет
## ничем. Прежнее устройство держалось на том, что буквы заняты ходьбой, — а в
## кабине они не заняты ничем: ноги там выключены целиком. Раскладку рукояток с
## тех пор присылает сервер (content.Organ), и разбирает её world._cab_action.
func set_keys(on: bool) -> void:
	_keys = on


## set_bounds — объявить камере упоры и СРАЗУ их применить.
##
## Применить сразу — не мелочь, а пункт правила: предел по зуму выведен из
## дальности взгляда, и стоило игроку УМЕНЬШИТЬ дальность, как разрешённый кадр
## сузился под уже отъехавшей камерой. Оставить её снаружи значило бы показать
## после смены настройки ровно тот кадр, против которого предел и заведён.
func set_bounds(b: WorldBounds) -> void:
	bounds = b
	apply()


## limit_note — почему дальше нельзя. Пусто, пока никуда не упирались.
func limit_note() -> String:
	return _limit_note


func _dist_for(size_m: float) -> float:
	return size_m / (2.0 * tan(deg_to_rad(CAM_FOV) * 0.5))


## apply — собрать положение камеры из точки, углов и масштаба.
## standoff_m — на сколько метров камера отставлена от точки взгляда.
##
## Наружу это нужно ДЫМКЕ. В ортографии отступ на масштаб не влияет, но дымка
## считается от положения камеры, а не от того, куда смотрят: при кадре в 4 км
## камера стоит в 4.4 км, то есть вдвое дальше объявленной видимости, и весь кадр
## выбеливается. Спайк это знал и записал тем же числом: «отступ в 6 км даёт
## верную геометрию и полностью выбеленный дымкой кадр». Мир отодвигает начало
## глубинного тумана на этот отступ, и дымка начинает мерить удаление ОТ ПЛОСКОСТИ
## ФОКУСА, а не от камеры.
func standoff_m() -> float:
	return _dist


func apply() -> void:
	_clamp_to_world()
	if ortho:
		# В ортографии дистанция на масштаб не влияет вовсе, но держим её
		# согласованной с размером кадра: иначе переключение проекции прыгало бы.
		_dist = _dist_for(frame_size_m)
	var az := deg_to_rad(azimuth_deg)
	var el := deg_to_rad(elev_deg)
	var horiz := Vector3(cos(az), 0.0, sin(az))
	position = focus + (horiz * cos(el) + Vector3.UP * sin(el)) * _dist
	look_at(focus, Vector3.UP)
	projection = PROJECTION_ORTHOGONAL if ortho else PROJECTION_PERSPECTIVE
	size = frame_size_m
	fov = CAM_FOV
	far = maxf(_dist * 8.0, 4000.0)
	# БЛИЖНЯЯ ПЛОСКОСТЬ В ОРТОГРАФИИ УХОДИТ ЗА КАМЕРУ, и это не хитрость, а
	# перенесённое лечение обрезанного края (разбор — в спайке, дословно).
	#
	# В ортографии дистанция на масштаб не влияет ВОВСЕ — влияет только
	# отсечение, и ближняя плоскость там параллельна экрану. На ширине кадра
	# 40 м камера стоит в сорока метрах, то есть ВНУТРИ сцены, и весь передний
	# план ближе неё срезается: на экране это прямая линия поперёк кадра.
	#
	# Отодвинуть камеру нельзя, хотя картинку это бы не изменило: от ПОЛОЖЕНИЯ
	# камеры считаются дымка (fog_density) и дальность зерна земли (detail_far в
	# шейдере покрова). Проверено снимком спайка: отступ в 6 км даёт верную
	# геометрию и полностью выбеленный дымкой кадр.
	#
	# У ортографической проекции ближняя плоскость — обычная граница линейного
	# отображения глубины, и отрицательной ей быть не запрещено: она просто
	# оказывается ПОЗАДИ камеры. Так сцена целиком попадает в объём отсечения, а
	# камера остаётся там, где её ждут дымка и шейдер. В перспективе так нельзя:
	# там ближняя плоскость стоит в знаменателе.
	near = -_dist if ortho else 0.05


## _clamp_to_world — ВЕСЬ КАДР ОСТАЁТСЯ ВНУТРИ ЗАГРУЖЕННОГО МИРА.
##
## Двух проверок хватает ровно потому, что они об одном: положение отвечает за
## «где стою», ширина кадра — за «докуда смотрю», и мир кончается там, где
## кончается засеянное (разбор целиком — в шапке WorldBounds).
##
## Одно место на все жесты: и колесо, и щипок, и клавиши, и панорама сходятся в
## apply(), поэтому упор нельзя обойти жестом, о котором забыли.
func _clamp_to_world() -> void:
	var note := ""
	if bounds != null and bounds.max_frame_m > 0.0:
		# В ортографии масштаб держит ширина кадра, в перспективе — дистанция
		# (см. _zoom_by). Поэтому и предел применяется к разным величинам, а не к
		# одной «на всякий случай».
		if frame_size_m > bounds.max_frame_m:
			frame_size_m = bounds.max_frame_m
			if ortho:
				note = "кадр упёрся в %.0f м: дальше загруженного мира нет — дальность взгляда %.0f м" % [
					bounds.max_frame_m, bounds.reach_m]
		var dist_max := _dist_for(bounds.max_frame_m)
		if _dist > dist_max:
			_dist = dist_max
			if not ortho:
				note = "отъезд упёрся в %.0f м кадра: дальше загруженного мира нет — дальность взгляда %.0f м" % [
					bounds.max_frame_m, bounds.reach_m]
	if bounds != null and bounds.outside(focus) > 0.0:
		focus = bounds.pull_in(focus)
		note = "камера упёрлась в край региона: дальше %.0f м от оси мира нет ни у клиента, ни у сервера" % [
			bounds.max_offset_m]
	if note != _limit_note:
		_limit_note = note
		limit_changed.emit(note)
##
## Shift+ЛКМ как панорама — не украшение, а перенесённая грабля: у спайка
## панорама висела на средней и правой кнопке, а трекпад мака ни ту, ни другую
## перетаскиванием не изображает. Двупальцевый клик даёт ПКМ, но не даёт
## ПКМ+движение, и наружу это выходило так, что камера «привязана к одной
## точке»: орбита работает, а увести взгляд нечем.
##
## ЗУМ НА МАКЕ — вторая половина той же граблей, и она была ПОТЕРЯНА при переносе
## из спайка (лечение жило в spike_world.gd, 809280f^, а сюда переехал только
## родительский обработчик колеса). macOS отличает мышь от трекпада по фазе
## события прокрутки: у колеса фазы нет и приходит InputEventMouseButton с
## WHEEL_UP/WHEEL_DOWN, а у двупальцевого скролла фаза есть, и вместо колеса
## приходит InputEventPanGesture; щипок приходит InputEventMagnifyGesture.
## Колеса на трекпаде не бывает ВОВСЕ — поэтому камера, слушающая только его,
## снаружи выглядит так, будто зум сломан. Клавиши +/− — третье средство,
## работающее на любом вводе, включая тачпад без жестов и удалённый рабочий стол.
##
## Двупальцевый скролл отдан именно ЗУМУ, а не панораме, хотя на маке скролл
## обычно значит «прокрутить». Отвергнуто потому, что скролл здесь — тот же орган,
## что колесо мыши, и разное значение у одного органа на двух устройствах
## означало бы, что подсказка управления врёт одному из них.
func _unhandled_input(event: InputEvent) -> void:
	if not _active:
		return
	if event is InputEventMouseMotion:
		var mm := event as InputEventMouseMotion
		var mask := mm.button_mask
		if (mask & (MOUSE_BUTTON_MASK_LEFT | MOUSE_BUTTON_MASK_MIDDLE | MOUSE_BUTTON_MASK_RIGHT)) == 0:
			return
		var only_left := (mask & MOUSE_BUTTON_MASK_LEFT) != 0 \
			and (mask & (MOUSE_BUTTON_MASK_MIDDLE | MOUSE_BUTTON_MASK_RIGHT)) == 0
		# Shift берётся У САМОГО СОБЫТИЯ, а не опросом Input. Событие несёт
		# состояние модификаторов на момент, когда оно произошло, а
		# Input.is_key_pressed — на момент обработки, и это разные моменты:
		# движения мыши копятся за кадр (use_accumulated_input) и разбираются
		# позже. Отпущенный посреди протяжки Shift превращал бы хвост панорамы
		# в орбиту, и выглядело бы это как «камера иногда прыгает».
		if only_left and not mm.shift_pressed:
			_orbit(mm.relative)
		else:
			_pan(mm.relative)
		apply()
		get_viewport().set_input_as_handled()
	elif event is InputEventMouseButton and (event as InputEventMouseButton).pressed:
		var mb := event as InputEventMouseButton
		if mb.button_index == MOUSE_BUTTON_WHEEL_UP:
			_zoom(-1.0)
		elif mb.button_index == MOUSE_BUTTON_WHEEL_DOWN:
			_zoom(1.0)
		else:
			return
		apply()
		get_viewport().set_input_as_handled()
	elif event is InputEventPanGesture:
		_zoom_by(pow(ZOOM_PAN_BASE, (event as InputEventPanGesture).delta.y))
		apply()
		get_viewport().set_input_as_handled()
	elif event is InputEventMagnifyGesture:
		# factor у щипка — во сколько раз развели пальцы, то есть насколько
		# ПРИБЛИЗИТЬ. Наш множитель считает обратное (больше единицы — отдалить),
		# отсюда деление. Нижняя граница — защита от деления на ноль: движок
		# отдаёт factor как 1+magnification, и предела снизу у него нет.
		_zoom_by(1.0 / maxf((event as InputEventMagnifyGesture).factor, 0.01))
		apply()
		get_viewport().set_input_as_handled()
	elif event is InputEventKey and (event as InputEventKey).pressed and not (event as InputEventKey).echo:
		match (event as InputEventKey).keycode:
			KEY_P:
				toggle_projection()   # apply() зовёт сама: она меняет и проекцию
			KEY_EQUAL, KEY_KP_ADD:
				_zoom(-1.0)
				apply()
			KEY_MINUS, KEY_KP_SUBTRACT:
				_zoom(1.0)
				apply()
			_:
				return
		get_viewport().set_input_as_handled()


## Панорама клавишами. Шаг пропорционален тому, что видно в кадре, а не задан в
## метрах: с постоянным шагом вблизи камера улетает за кадр, а на общем плане
## ползёт.
func _process(delta: float) -> void:
	if not _active or not _keys:
		return
	var keys := Vector2.ZERO
	if Input.is_key_pressed(KEY_W) or Input.is_key_pressed(KEY_UP):
		keys.y += 1.0
	if Input.is_key_pressed(KEY_S) or Input.is_key_pressed(KEY_DOWN):
		keys.y -= 1.0
	if Input.is_key_pressed(KEY_D) or Input.is_key_pressed(KEY_RIGHT):
		keys.x += 1.0
	if Input.is_key_pressed(KEY_A) or Input.is_key_pressed(KEY_LEFT):
		keys.x -= 1.0
	if keys == Vector2.ZERO:
		return
	var span := frame_size_m if ortho else _dist
	var step := PAN_RATE * span * delta
	if Input.is_key_pressed(KEY_SHIFT):
		step *= PAN_FAST
	var m := keys.normalized() * step
	focus += _right() * m.x + _forward() * m.y
	apply()


## Плоские орты камеры: панорама идёт ПО ЗЕМЛЕ, а не по экрану. Иначе при
## наклоне 45° клавиша «вперёд» уводила бы фокус под землю.
func _right() -> Vector3:
	var b := global_transform.basis
	return Vector3(b.x.x, 0.0, b.x.z).normalized()


func _forward() -> Vector3:
	var b := global_transform.basis
	return Vector3(-b.z.x, 0.0, -b.z.z).normalized()


func _orbit(rel: Vector2) -> void:
	azimuth_deg -= rel.x * ORBIT_RATE
	elev_deg = clampf(elev_deg + rel.y * ORBIT_RATE, ELEV_MIN, ELEV_MAX)


func _pan(rel: Vector2) -> void:
	var vp := get_viewport().get_visible_rect().size
	if vp.y <= 0.0:
		return
	var wpp: float
	if ortho:
		wpp = frame_size_m / vp.y
	else:
		wpp = 2.0 * _dist * tan(deg_to_rad(CAM_FOV) * 0.5) / vp.y
	focus -= _right() * rel.x * wpp
	focus += _forward() * rel.y * wpp


## Зум щелчком: dir < 0 — приблизить, dir > 0 — отдалить. Шаг постоянный, потому
## что у колеса и у клавиши +/− нет величины — только факт.
func _zoom(dir: float) -> void:
	_zoom_by(ZOOM_STEP if dir < 0.0 else 1.0 / ZOOM_STEP)


## _zoom_by — множитель масштаба; больше единицы значит отдалиться.
##
## Отдельно от _zoom(dir) потому, что жест трекпада НЕПРЕРЫВЕН: у скролла и щипка
## есть величина, и загонять её в два фиксированных шага значило бы выбросить
## ровно то, чем жест отличается от колеса.
##
## Что именно меняется, зависит от проекции, и это не мелочь: в ортографии
## дистанция на масштаб не влияет вовсе (см. apply), поэтому там двигать надо
## ширину кадра, а в перспективе — дистанцию.
func _zoom_by(f: float) -> void:
	if ortho:
		frame_size_m = clampf(frame_size_m * f, SIZE_MIN, SIZE_CEILING)
	else:
		_dist = clampf(_dist * f, DIST_MIN, DIST_CEILING)


## toggle_projection — P. Масштаб сохраняется: в кадре остаётся то же, что было.
func toggle_projection() -> void:
	ortho = not ortho
	if ortho:
		frame_size_m = clampf(2.0 * _dist * tan(deg_to_rad(CAM_FOV) * 0.5), SIZE_MIN, SIZE_CEILING)
	else:
		_dist = clampf(_dist_for(frame_size_m), DIST_MIN, DIST_CEILING)
	apply()


func projection_name() -> String:
	return "ортографическая" if ortho else "перспективная"
