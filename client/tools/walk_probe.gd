extends SceneTree
## ЗОНД ХОДЬБЫ. Отвечает ЧИСЛАМИ на четыре вопроса, на которые ни снимок, ни лог
## не отвечают: стоит ли человек НА земле, идёт ли он, вертит ли головой и
## переходит ли через путь.
##
## Зачем отдельный зонд, а не «посмотрите на снимок». Снимок с высоты глаз
## УСТРОЕН ТАК, ЧТО СКРЫВАЕТ ровно эти дефекты:
##
##   * утоп по колено или парит на 30 см — из своих же глаз не видно НИЧЕГО,
##     кадр в обоих случаях правильный, и ошибка вылезет только когда кто-нибудь
##     посмотрит на человека со стороны;
##   * упёрся в первую шпалу — на снимке это просто другой кадр, а не отказ;
##   * взгляд мышью не работает — снимок снимается по заданному курсу и потому
##     всегда верен (тот же класс бага, что зум на трекпаде: жест есть в коде, но
##     на этом вводе его не изобразить, см. tools/camera_probe.gd).
##
##   godot --rendering-method forward_plus --path client \
##       --script res://tools/walk_probe.gd
##
## --headless НЕ ГОДИТСЯ: сцене нужен вьюпорт (см. шапку Makefile).

const FPV := preload("res://scripts/spike_fpv.gd")
const Relief := preload("res://scripts/spike_relief.gd")

const WARMUP := 0.25             # с — на оседание после постановки
const WALK_T := 2.0              # с — сколько идём прямо
const CROSS_T := 9.0             # с — сколько идём поперёк на путь
const LOOK_REL := 240.0          # пикселей движения мыши для пробы взгляда

## ПРОБА ПЕРЕХОДА СТОИТ НЕ ТАМ, ГДЕ ЧЕЛОВЕК ПОЯВЛЯЕТСЯ. На месте появления
## (u = 62) между ним и осью лежит платформа, а её борт — 1.05 м, то есть стена, а
## не ступень: первый заход зонда показал «прошёл 0.96 м» и был прав. На платформу
## с земли не всходят, и лестниц в контракте нет — поэтому переход меряется там,
## где платформы нет: она занимает u = 40..100 (PLAT_MAIN в эталоне).
const CROSS_U := 140.0           # м вдоль LOCO_ELEMENT: за дальним концом платформы
const CROSS_LAT := -6.0          # м от оси: столько же, сколько до платформы

var _node: Node3D
var _stage := 0
var _t := 0.0                    # с в текущей стадии
var _from := Vector3.ZERO
var _fails := 0
var _min_gap := INF              # м — самый глубокий провал подошвы под поле высот
var _base_y := 0.0
var _max_climb := -INF           # м — самый высокий подъём над началом пробы перехода
var _trace := false
var _traced := 0.0

func _initialize() -> void:
	# Открываемся в виде от первого лица: он ведёт человека, а обзорный — камеру.
	FPV.fpv_mode = 1
	_trace = OS.get_cmdline_user_args().has("--trace")
	_node = (load("res://scenes/spike_fpv.tscn") as PackedScene).instantiate()
	root.add_child(_node)

## ВРЕМЯ ЗОНДА — ФИЗИЧЕСКОЕ, А НЕ ЭКРАННОЕ. Сперва проба жила в _process, и одна
## и та же ходьба мерилась то 1.42, то 1.30 м/с: человека ведёт _physics_process с
## постоянным шагом, а кадры на этой сцене идут по 19-30 мс и «две секунды по
## часам» это то 118, то 104 шага физики. Разброс был в измерителе, а не в ходьбе.
func _physics_process(delta: float) -> bool:
	_t += delta
	if _node == null:
		return false
	var drv: CharacterBody3D = _node._driver
	if drv == null:
		printerr("WALK PROBE FAIL: машиниста нет в сцене")
		quit(1)
		return true
	# Провал считается КАЖДЫЙ кадр, а не в конце: сквозь землю проваливаются на
	# полсекунды и вылезают обратно, и по конечной точке этого не видно.
	if _stage > 0:
		var p := drv.global_position
		_min_gap = minf(_min_gap, p.y - _node._height_at(p.x, p.z))
		# Подъём считается ПО МАКСИМУМУ за пробу, а не по конечной точке: за девять
		# секунд человек успевает не только взойти на путь, но и сойти с него на ту
		# сторону, и по концу выходило бы «не поднялся» ровно там, где он прошёл
		# путь насквозь. Замер это и показал: 8.79 м пути при подъёме +0.48 против
		# 5.08 м при +1.32 — вторая пробежка кончилась на щебне, первая за ним.
		if _stage == 3:
			_max_climb = maxf(_max_climb, p.y - _base_y)
	match _stage:
		0:
			if _t < WARMUP:
				return false
			_stand(drv)
			_from = drv.global_position
			_press(KEY_W)
			_next()
		1:
			if _t < WALK_T:
				return false
			_release(KEY_W)
			_walked(drv)
			_next()
		2:
			_look()
			_cross_setup(drv)
			_press(KEY_W)
			_next()
		3:
			if _t < CROSS_T:
				# След пути — по требованию: когда переход не удаётся, надо видеть, на
				# каком метре человек встал, а не только что он не дошёл.
				if _trace and _t - _traced >= 0.5:
					_traced = _t
					var q := drv.global_position
					print("WALK PROBE:   t=%.1f  x=%.2f z=%.2f y=%.2f  |v|=%.2f  пол=%s"
						% [_t, q.x, q.z, q.y, Vector2(drv.velocity.x, drv.velocity.z).length(),
							"да" if drv.is_on_floor() else "нет"])
				return false
			_release(KEY_W)
			_crossed(drv)
			_report()
			return true
	return false

func _next() -> void:
	_stage += 1
	_t = 0.0

## 1. СТОИТ ЛИ НА ТВЕРДИ. Две разные проверки, и обе нужны: движок может считать
## человека стоящим на полу, пока он висит на юбке мира или на боку локомотива.
func _stand(drv: CharacterBody3D) -> void:
	var p := drv.global_position
	var gap: float = p.y - _node._height_at(p.x, p.z)
	_ok("стоит на полу", drv.is_on_floor(), "да" if drv.is_on_floor() else "ВИСИТ")
	# Допуск 0.12 м: под ногами может оказаться не голая земля, а полка коридора
	# земляных работ, и расхождение в пару сантиметров — это шаг сетки, а не дефект.
	_ok("подошва на земле", absf(gap) < 0.12, "зазор %+.3f м" % gap)

## 2. ИДЁТ ЛИ. Скорость — по пройденному за известное время, а не по velocity:
## velocity врёт при упоре в стену (тело толкается, но не едет).
func _walked(drv: CharacterBody3D) -> void:
	var d := drv.global_position - _from
	var v := Vector2(d.x, d.z).length() / WALK_T
	# Нижняя граница с запасом на разгон (ACCEL берёт скорость за 0.1 с) и на
	# уклон; верхняя — против «случайно побежал».
	_ok("идёт вперёд", v > FPV.WALK_SPEED * 0.75 and v < FPV.WALK_SPEED * 1.25,
		"%.2f м/с при заявленных %.2f" % [v, FPV.WALK_SPEED])

## 3. ВЕРТИТ ЛИ ГОЛОВОЙ. Мышь подаётся не через parse_input_event, а прямым
## вызовом того же метода, который зовёт _unhandled_input: относительное движение
## мыши движок в очереди не воспроизводит, и подделка проверяла бы подделку.
func _look() -> void:
	var yaw0: float = _node._yaw
	var pitch0: float = _node._pitch
	var fwd0: Vector3 = -_node._camera.global_transform.basis.z
	_node._look_by(Vector2(LOOK_REL, 0.0))
	_node._look_by(Vector2(0.0, LOOK_REL * 0.25))
	_node._place_camera()
	var fwd1: Vector3 = -_node._camera.global_transform.basis.z
	var turned := rad_to_deg(fwd0.angle_to(fwd1))
	var want := LOOK_REL * FPV.MOUSE_SENS
	_ok("взгляд мышью", turned > want * 0.5,
		"поворот %.1f° (рыскание %+.1f°, тангаж %+.1f°)"
			% [turned, _node._yaw - yaw0, _node._pitch - pitch0])

## 4. ПЕРЕХОДИТ ЛИ ЧЕРЕЗ ПУТЬ. Самая важная проверка спайка: балласт, шпала и
## головка рельса — это ступени 0.20-0.36 м, а у CharacterBody3D шага через
## ступеньку нет вовсе. Без неё человек упирается в первую же шпалу, и это
## выглядит не отказом, а «просто он там не пошёл».
##
## Человек ставится сбоку от оси и разворачивается НА путь; ждём подъём на высоту
## призмы и пройденный путь, которого хватает, чтобы оказаться между рельсами.
func _cross_setup(drv: CharacterBody3D) -> void:
	var el: Dictionary = _node._elements.get(Relief.LOCO_ELEMENT, {})
	var fr: Dictionary = _node._rail_frame(el, CROSS_U) if not el.is_empty() else {}
	if fr.is_empty():
		printerr("WALK PROBE: нет рамки пути на u = %.1f — проба перехода пропущена" % CROSS_U)
		_from = drv.global_position
		_base_y = drv.global_position.y
		return
	var lat: Vector3 = fr.lat
	var start: Vector3 = (fr.o as Vector3) + lat * CROSS_LAT
	drv.velocity = Vector3.ZERO
	drv.global_position = Vector3(start.x, _node._height_at(start.x, start.z) + 0.02, start.z)
	# Курс — В СТОРОНУ ОСИ: латераль отрицательная, значит идти надо по +lat.
	var to_axis := (lat * (-signf(CROSS_LAT))).normalized()
	_node._yaw = rad_to_deg(atan2(-to_axis.x, -to_axis.z))
	_node._pitch = 0.0
	_node._apply_look()
	_from = drv.global_position
	_base_y = drv.global_position.y

func _crossed(drv: CharacterBody3D) -> void:
	var d := drv.global_position - _from
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
## Тот же приём нужен и camera_probe.gd, где W отпускается и D нажимается подряд.
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
