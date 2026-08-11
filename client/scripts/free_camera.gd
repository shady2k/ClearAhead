## FreeCamera — свободная камера, чтобы можно было посмотреть.
##
## WASD — движение, Q/E — вниз/вверх, правая кнопка мыши — обзор, колесо —
## скорость. Ничего из этого не относится к миру: это орган управления
## наблюдателем.
##
## Начальное положение НЕ ЗАДАНО КОНСТАНТОЙ: его выставляет frame_bounds по
## габаритам того, что реально приехало с сервера. Прежний клиент был снесён в
## том числе за ROLE_CAMERA с фокусом (240, 0) — числом, взятым ниоткуда.
extends Camera3D

var speed: float = 60.0
var look_sensitivity: float = 0.0035

var _yaw: float = 0.0
var _pitch: float = 0.0
var _looking: bool = false


func _ready() -> void:
	_sync_angles()


func _sync_angles() -> void:
	_yaw = rotation.y
	_pitch = rotation.x


func _unhandled_input(event: InputEvent) -> void:
	if event is InputEventMouseButton:
		var mb := event as InputEventMouseButton
		if mb.button_index == MOUSE_BUTTON_RIGHT:
			_looking = mb.pressed
			Input.set_mouse_mode(Input.MOUSE_MODE_CAPTURED if _looking else Input.MOUSE_MODE_VISIBLE)
		elif mb.pressed and mb.button_index == MOUSE_BUTTON_WHEEL_UP:
			speed = minf(speed * 1.25, 4000.0)
		elif mb.pressed and mb.button_index == MOUSE_BUTTON_WHEEL_DOWN:
			speed = maxf(speed * 0.8, 1.0)
	elif event is InputEventMouseMotion and _looking:
		var mm := event as InputEventMouseMotion
		_yaw -= mm.relative.x * look_sensitivity
		_pitch = clampf(_pitch - mm.relative.y * look_sensitivity, -1.55, 1.55)
		rotation = Vector3(_pitch, _yaw, 0.0)


func _process(delta: float) -> void:
	var dir := Vector3.ZERO
	if Input.is_key_pressed(KEY_W):
		dir -= transform.basis.z
	if Input.is_key_pressed(KEY_S):
		dir += transform.basis.z
	if Input.is_key_pressed(KEY_A):
		dir -= transform.basis.x
	if Input.is_key_pressed(KEY_D):
		dir += transform.basis.x
	if Input.is_key_pressed(KEY_E):
		dir += Vector3.UP
	if Input.is_key_pressed(KEY_Q):
		dir -= Vector3.UP
	if dir != Vector3.ZERO:
		var mult := 4.0 if Input.is_key_pressed(KEY_SHIFT) else 1.0
		position += dir.normalized() * speed * mult * delta


## frame_bounds — смотреть на то, что приехало.
##
## centre и radius считаются вызывающим из ГАБАРИТОВ ДАННЫХ. Наклон и отношение
## дальности к радиусу — свойства взгляда, а не мира.
func frame_bounds(centre: Vector3, radius: float, pitch_deg: float, distance_factor: float) -> void:
	var pitch := deg_to_rad(pitch_deg)
	var dist := maxf(radius, 1.0) * distance_factor
	# Поле зрения по умолчанию (75° по вертикали, около 105° по горизонтали)
	# уводит станцию в точку. 50° — обычная «длинная» перспектива; это свойство
	# взгляда, а не мира.
	fov = 50.0
	# Отступ назад и ВВЕРХ: положительный наклон означает взгляд сверху вниз.
	var back := Vector3(0.0, sin(pitch), cos(pitch)) * dist
	position = centre + back
	look_at(centre, Vector3.UP)
	_sync_angles()
	far = maxf(far, dist * 8.0)
	near = maxf(0.1, dist * 0.001)
	speed = maxf(10.0, radius * 0.4)
