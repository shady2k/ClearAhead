extends SceneTree
## ЗОНД ХОДЬБЫ. Отвечает ЧИСЛАМИ на четыре вопроса, на которые ни снимок, ни лог
## не отвечают: стоит ли человек НА тверди, идёт ли он, вертит ли головой и
## переходит ли через путь.
##
## Перенос из спайка машиниста (`client/tools/walk_probe.gd`, 809280f^) вместе с
## его доводом — снимок с высоты глаз УСТРОЕН ТАК, ЧТО СКРЫВАЕТ ровно эти дефекты:
##
##   * утоп по колено или парит на 30 см — из своих же глаз не видно НИЧЕГО,
##     кадр в обоих случаях правильный, и ошибка вылезет только когда кто-нибудь
##     посмотрит на человека со стороны;
##   * упёрся в первую шпалу — на снимке это просто другой кадр, а не отказ;
##   * взгляд мышью не работает — снимок снимается по заданному курсу и потому
##     всегда верен (тот же класс бага, что зум на трекпаде: жест есть в коде, но
##     на этом вводе его не изобразить).
##
##   godot --rendering-method forward_plus --path client \
##       --script res://tools/walk_probe.gd -- --server=… --region=ST_A
##
## --headless НЕ ГОДИТСЯ: мир строится сценой, и ему нужен вьюпорт. По той же
## причине зонд не живёт в tools/checks/ — те гоняются без окна, и живому серверу
## там делать нечего лишь у половины из них.
##
## ЧЕМ ЭТОТ ЗОНД ОТЛИЧАЕТСЯ ОТ СПАЙКОВОГО. Спайк мерил провал против СВОЕГО поля
## высот (_height_at) — чистой функции координат, которая у него была. У нас её
## нет и быть не может: земля приезжает чанками, а под ногами кроме земли бывают
## балласт, шпалы, плита платформы и дом. Поэтому поверхность спрашивается ТЕМ
## ЖЕ, чем ходят, — лучом по тверди, сверху вниз. Это не ослабление проверки, а
## усиление: сравнивается не с копией земли, а с тем, что нарисовано.

const WARMUP := 0.25             # с — на оседание после постановки
const WALK_T := 2.0              # с — сколько идём прямо
const CROSS_T := 9.0             # с — сколько идём поперёк на путь
const LOOK_REL := 240.0          # пикселей движения мыши для пробы взгляда
const SETTLE_WAIT := 30.0        # с — сколько ждём постройки мира и постановки

## ПРОБА ПЕРЕХОДА СТОИТ НЕ ТАМ, ГДЕ ЧЕЛОВЕК ПОЯВЛЯЕТСЯ. На месте появления между
## ним и осью лежит платформа, а её борт — стена, а не ступень: на платформу с
## земли не всходят, и лестниц в контракте нет. Поэтому переход меряется там, где
## платформы нет: на затравке ST_A PLAT_MAIN занимает u = 40…100.
const CROSS_U := 140.0           # м вдоль элемента: за дальним концом платформы
const CROSS_LAT := -6.0          # м от оси: столько же, сколько до платформы

## Луч, которым спрашивается поверхность под подошвой. Сверху вниз и мимо самого
## человека: снизу вверх он упёрся бы в его же капсулу.
const PROBE_UP := 2.0            # м — откуда пускается луч
const PROBE_DOWN := 20.0         # м — и докуда

var _app: Node
var _drv: Driver
var _stage := 0
var _t := 0.0                    # с в текущей стадии
var _from := Vector3.ZERO
var _fails := 0
var _min_gap := INF              # м — самый глубокий провал подошвы под твердь
var _base_y := 0.0
var _cross_started := false
var _max_climb := -INF           # м — самый высокий подъём над началом пробы перехода
var _trace := false
var _traced := 0.0


func _initialize() -> void:
	_trace = OS.get_cmdline_user_args().has("--trace")
	# Мир строится ОБОЛОЧКОЙ, как у игрока: зонд не собирает свою сцену, иначе он
	# проверял бы не то, что запускается. Роль задаётся ключом, а не здесь.
	_app = (load("res://scenes/main.tscn") as PackedScene).instantiate()
	root.add_child(_app)


## ВРЕМЯ ЗОНДА — ФИЗИЧЕСКОЕ, А НЕ ЭКРАННОЕ. Довод спайка: одна и та же ходьба
## мерилась то 1.42, то 1.30 м/с, потому что человека ведёт _physics_process с
## постоянным шагом, а кадры идут по 19-30 мс — «две секунды по часам» это то 118,
## то 104 шага физики. Разброс был в измерителе, а не в ходьбе.
func _physics_process(delta: float) -> bool:
	_t += delta
	if _drv == null:
		return _wait_for_driver()
	# Провал считается КАЖДЫЙ кадр, а не в конце: сквозь твердь проваливаются на
	# полсекунды и вылезают обратно, и по конечной точке этого не видно.
	if _stage > 0:
		var g := _gap()
		if is_finite(g):
			_min_gap = minf(_min_gap, g)
		# Подъём считается ПО МАКСИМУМУ за пробу, а не по конечной точке: за девять
		# секунд человек успевает не только взойти на путь, но и сойти с него на ту
		# сторону, и по концу выходило бы «не поднялся» ровно там, где он прошёл
		# путь насквозь.
		if _stage == 3 and _cross_started:
			_max_climb = maxf(_max_climb, _drv.global_position.y - _base_y)
	match _stage:
		0:
			if _t < WARMUP:
				return false
			_stand()
			_from = _drv.global_position
			_press(KEY_W)
			_next()
		1:
			if _t < WALK_T:
				return false
			_release(KEY_W)
			_walked()
			_next()
		2:
			_look()
			if not _cross_setup():
				_report()
				return true
			_next()
		3:
			# Постановка лучом занимает шаг физики: пока человек не встал, идти
			# нечем, и отсчёт пробы ещё не начался.
			if not _drv.is_settled():
				_t = 0.0
				return false
			if not _cross_started:
				_cross_started = true
				_from = _drv.global_position
				_base_y = _drv.global_position.y
				_max_climb = -INF
				_t = 0.0
				_press(KEY_W)
				return false
			if _t < CROSS_T:
				# След пути — по требованию: когда переход не удаётся, надо видеть,
				# на каком метре человек встал, а не только что он не дошёл.
				if _trace and _t - _traced >= 0.5:
					_traced = _t
					var q := _drv.global_position
					print("WALK PROBE:   t=%.1f  x=%.2f z=%.2f y=%.2f  |v|=%.2f  пол=%s"
						% [_t, q.x, q.z, q.y, Vector2(_drv.velocity.x, _drv.velocity.z).length(),
							"да" if _drv.is_on_floor() else "нет"])
				return false
			_release(KEY_W)
			_crossed()
			_report()
			return true
	return false


## _wait_for_driver — мир строится из сети, и сколько это займёт, зонду знать
## неоткуда. Ждём человека, а не «сколько-нибудь кадров, наверное, хватит».
func _wait_for_driver() -> bool:
	var found := _find_driver(root)
	if found != null and found.is_settled():
		_drv = found
		_t = 0.0
		print("WALK PROBE: человек найден, подошва на %.2f м" % _drv.global_position.y)
		return false
	if _t > SETTLE_WAIT:
		printerr("WALK PROBE FAIL: человек не встал за %.0f с — мир не построился или роль не пешая"
			% SETTLE_WAIT)
		quit(1)
		return true
	return false


func _find_driver(node: Node) -> Driver:
	var d := node as Driver
	if d != null:
		return d
	for c in node.get_children():
		var f := _find_driver(c)
		if f != null:
			return f
	return null


func _next() -> void:
	_stage += 1
	_t = 0.0


## _gap — зазор между подошвой и твердью под ней, метры.
##
## Луч идёт СВЕРХУ ВНИЗ и мимо самого человека. Знак читается так: ноль — стоит
## на поверхности, плюс — парит над ней, минус — утонул (луч встретил твердь выше
## подошвы). INF значит «под ногами тверди нет вовсе» — это отдельная беда, и она
## видна отказом постановки, а не этим числом.
func _gap() -> float:
	var p := _drv.global_position
	var from := p + Vector3.UP * PROBE_UP
	var q := PhysicsRayQueryParameters3D.create(from, from + Vector3.DOWN * PROBE_DOWN)
	q.exclude = [_drv.get_rid()]
	var hit := _drv.get_world_3d().direct_space_state.intersect_ray(q)
	if hit.is_empty():
		return INF
	return p.y - (hit["position"] as Vector3).y


## 1. СТОИТ ЛИ НА ТВЕРДИ. Две разные проверки, и обе нужны: движок может считать
## человека стоящим на полу, пока он висит на боку соседнего тела.
func _stand() -> void:
	var floored := _drv.is_on_floor()
	_ok("стоит на полу", floored, "да" if floored else "ВИСИТ")
	var gap := _gap()
	# Допуск 0.12 м: под ногами может оказаться не ровная площадка, а откос или
	# стык двух чанков разной подробности, и пара сантиметров там — шаг сетки, а
	# не дефект.
	_ok("подошва на тверди", is_finite(gap) and absf(gap) < 0.12,
		"зазор %+.3f м" % gap if is_finite(gap) else "тверди под ногами НЕТ")


## 2. ИДЁТ ЛИ. Скорость — по пройденному за известное время, а не по velocity:
## velocity врёт при упоре в стену (тело толкается, но не едет).
func _walked() -> void:
	var d := _drv.global_position - _from
	var v := Vector2(d.x, d.z).length() / WALK_T
	# Нижняя граница с запасом на разгон (ACCEL берёт скорость за 0.1 с) и на
	# уклон; верхняя — против «случайно побежал».
	_ok("идёт вперёд", v > Driver.WALK_SPEED * 0.75 and v < Driver.WALK_SPEED * 1.25,
		"%.2f м/с при заявленных %.2f" % [v, Driver.WALK_SPEED])


## 3. ВЕРТИТ ЛИ ГОЛОВОЙ. Мышь подаётся не через parse_input_event, а прямым
## вызовом того же метода, который зовёт _unhandled_input: относительное движение
## мыши движок в очереди не воспроизводит, и подделка проверяла бы подделку.
func _look() -> void:
	var cam := _drv.get_viewport().get_camera_3d()
	if cam == null:
		_ok("взгляд мышью", false, "камеры в сцене нет")
		return
	var fwd0 := -cam.global_transform.basis.z
	_drv.look_by(Vector2(LOOK_REL, 0.0))
	_drv.look_by(Vector2(0.0, LOOK_REL * 0.25))
	_drv.place_camera()
	var fwd1 := -cam.global_transform.basis.z
	var turned := rad_to_deg(fwd0.angle_to(fwd1))
	var want := LOOK_REL * Driver.MOUSE_SENS
	_ok("взгляд мышью", turned > want * 0.5, "поворот %.1f° на %.0f пикселей мыши"
		% [turned, LOOK_REL])


## 4. ПЕРЕХОДИТ ЛИ ЧЕРЕЗ ПУТЬ. Самая важная проба: балласт, шпала и головка
## рельса — это ступени 0.20-0.36 м, а у CharacterBody3D шага через ступеньку нет
## вовсе. Без него человек упирается в первую же шпалу, и выглядит это не отказом,
## а «просто он там не пошёл».
##
## Человек ставится сбоку от оси и разворачивается НА путь — в координатах ПУТИ, а
## не по зашитым метрам мира.
func _cross_setup() -> bool:
	var tp := _drv.track_point(CROSS_U, CROSS_LAT)
	if tp.is_empty():
		printerr("WALK PROBE FAIL: человек не знает своего элемента — проба перехода невозможна")
		_fails += 1
		return false
	# Курс — В СТОРОНУ ОСИ: латераль отрицательная, значит идти надо по +lat.
	var to_axis: Vector3 = (tp["lat_dir"] as Vector3) * (-signf(CROSS_LAT))
	_drv.put((tp["pos"] as Vector3) + Vector3.UP * Driver.SETTLE_LIFT,
		_drv.yaw_for(to_axis), 0.0)
	return true


func _crossed() -> void:
	var d := _drv.global_position - _from
	var gone := Vector2(d.x, d.z).length()
	# Подъём: подошва балласта заглублена в полку, верх шпалы примерно на полметра
	# выше земли рядом. Треть метра — это «встал на путь», а не «дошёл до откоса».
	_ok("переходит через путь", gone > absf(CROSS_LAT) * 0.7 and _max_climb > 0.30,
		"прошёл %.2f м, поднялся до %+.2f м" % [gone, _max_climb])
	_ok("не проваливается", _min_gap > -0.30, "самый глубокий провал %+.2f м" % _min_gap)


func _ok(what: String, ok: bool, note: String) -> void:
	if not ok:
		_fails += 1
	print("WALK PROBE: %-22s %-42s %s" % [what, note, "ok" if ok else "ПЛОХО"])


func _report() -> void:
	if _fails > 0:
		printerr("WALK PROBE FAIL: проб не прошло — %d" % _fails)
		quit(1)
		return
	print("WALK PROBE OK")
	quit(0)


## ОТПУСКАНИЕ И НАЖАТИЕ РАЗДЕЛЯЮТСЯ ПРОТАЛКИВАНИЕМ ОЧЕРЕДИ. Без него зонд врал
## через раз: parse_input_event только КЛАДЁТ событие в буфер, а разбирается буфер
## в начале кадра. Отпускание W в конце одной пробы и нажатие в начале следующей
## попадали в один буфер, и порядок их разбора решал исход — из двух прогонов
## подряд один показывал ходьбу 7.6 м, другой полную неподвижность при |v| = 0.
func _press(key: Key) -> void:
	_key(key, true)


func _release(key: Key) -> void:
	_key(key, false)


func _key(key: Key, pressed: bool) -> void:
	var ev := InputEventKey.new()
	ev.keycode = key
	ev.physical_keycode = key
	ev.pressed = pressed
	Input.parse_input_event(ev)
	Input.flush_buffered_events()
