## OrbitCamera — камера, вращающаяся вокруг точки на земле. Порт из снесённого
## спайка (`spike_relief.gd`, 809280f^), а не сочинение заново.
##
## # Чем она отличается от FreeCamera и зачем их две
##
## FreeCamera — наблюдатель, ЛЕТАЮЩИЙ по миру: у неё есть положение и взгляд, и
## оба меняются независимо. Эта — наблюдатель, СМОТРЯЩИЙ НА МЕСТО: у неё есть
## точка интереса, азимут, возвышение и масштаб, а положение из них выводится.
## Разница не в удобстве, а в том, что означает жест: у первой мышь поворачивает
## голову, у второй — обходит станцию кругом.
##
## Два органа управления вместо одного с флагом — потому что смешанный вышел бы
## тем, что переключается посреди жеста.
##
## # Что здесь ЗАКОННО константами
##
## Азимут, возвышение, ширина кадра, скорость орбиты и зума — свойства ВЗГЛЯДА.
## Граница ClearAhead-sjq разрешает клиенту камеру целиком: «вид не есть мир».
##
## И чего здесь НЕТ, хотя у спайка было: ТОЧКИ ФОКУСА КОНСТАНТОЙ. Снесённый
## клиент держал `shot_focus = Vector2(240.0, 0.0)`, и это одна из причин сноса —
## число, взятое ниоткуда (см. шапку free_camera.gd). Фокус приходит СНАРУЖИ, из
## габаритов того, что приехало с сервера.
class_name OrbitCamera
extends Camera3D

## Поле зрения перспективного режима. Оно же переводит ширину орто-кадра в
## дистанцию, чтобы переключение проекции не прыгало по масштабу.
const CAM_FOV := 50.0

const ORBIT_RATE := 0.30      # градусов на пиксель
const ZOOM_STEP := 0.88       # во сколько раз меняется масштаб за щелчок колеса
const PAN_RATE := 0.55        # доля видимого поперечника в секунду
const PAN_FAST := 4.0         # множитель с Shift

const ELEV_MIN := 2.0
const ELEV_MAX := 89.0
const SIZE_MIN := 8.0
const SIZE_MAX := 8000.0
const DIST_MIN := 8.0
const DIST_MAX := 20000.0

var focus := Vector3.ZERO      # точка интереса в осях Godot
var azimuth_deg := 205.0
var elev_deg := 45.0
var frame_size_m := 300.0      # видимая высота кадра в ортографии
var ortho := true

var _dist := 300.0
## Пока камера не в игре, ввод ей не принадлежит: в паузе и в меню жесты мыши
## обязаны доставаться кнопкам, а не станции под ними.
var _active := true


## configure — навестись на то, что приехало.
##
## centre и size_m считает вызывающий из ГАБАРИТОВ ДАННЫХ. Углы и проекция —
## свойства взгляда и приходят от роли.
func configure(centre: Vector3, size_m: float, az_deg: float, el_deg: float, orthographic: bool) -> void:
	focus = centre
	frame_size_m = clampf(size_m, SIZE_MIN, SIZE_MAX)
	azimuth_deg = az_deg
	elev_deg = clampf(el_deg, ELEV_MIN, ELEV_MAX)
	ortho = orthographic
	_dist = _dist_for(frame_size_m)
	apply()


func set_active(on: bool) -> void:
	_active = on


func _dist_for(size_m: float) -> float:
	return size_m / (2.0 * tan(deg_to_rad(CAM_FOV) * 0.5))


## apply — собрать положение камеры из точки, углов и масштаба.
func apply() -> void:
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


## Ввод. ЛКМ — орбита, Shift+ЛКМ и СКМ/ПКМ — панорама, колесо — зум, P —
## проекция.
##
## Shift+ЛКМ как панорама — не украшение, а перенесённая грабля: у спайка
## панорама висела на средней и правой кнопке, а трекпад мака ни ту, ни другую
## перетаскиванием не изображает. Двупальцевый клик даёт ПКМ, но не даёт
## ПКМ+движение, и наружу это выходило так, что камера «привязана к одной
## точке»: орбита работает, а увести взгляд нечем.
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
		if only_left and not Input.is_key_pressed(KEY_SHIFT):
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
	elif event is InputEventKey and (event as InputEventKey).pressed and not (event as InputEventKey).echo:
		if (event as InputEventKey).keycode == KEY_P:
			toggle_projection()
			get_viewport().set_input_as_handled()


## Панорама клавишами. Шаг пропорционален тому, что видно в кадре, а не задан в
## метрах: с постоянным шагом вблизи камера улетает за кадр, а на общем плане
## ползёт.
func _process(delta: float) -> void:
	if not _active:
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


func _zoom(dir: float) -> void:
	var f := ZOOM_STEP if dir < 0.0 else 1.0 / ZOOM_STEP
	if ortho:
		frame_size_m = clampf(frame_size_m * f, SIZE_MIN, SIZE_MAX)
	else:
		_dist = clampf(_dist * f, DIST_MIN, DIST_MAX)


## toggle_projection — P. Масштаб сохраняется: в кадре остаётся то же, что было.
func toggle_projection() -> void:
	ortho = not ortho
	if ortho:
		frame_size_m = clampf(2.0 * _dist * tan(deg_to_rad(CAM_FOV) * 0.5), SIZE_MIN, SIZE_MAX)
	else:
		_dist = clampf(_dist_for(frame_size_m), DIST_MIN, DIST_MAX)
	apply()


func projection_name() -> String:
	return "ортографическая" if ortho else "перспективная"
