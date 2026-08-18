extends SceneTree
## ЗОНД КАМЕРЫ. Отвечает числом на вопрос, на который снимок не отвечает:
## ОКАЗЫВАЕТСЯ ЛИ ОБЗОРНАЯ КАМЕРА ВНУТРИ ЗЕМЛИ, и на каких углах.
##
##   godot --path client --script res://tools/camera_probe.gd -- \
##       --server=… --region=ST_A --role=driver --driver-view=orbit
##
## # Зачем зонд, если есть снимок
##
## Кадр, снятый из-под земли, ВЫГЛЯДИТ КАК КАДР: нижняя половина экрана — не
## чернота и не дыра, а нижняя полусфера неба цвета дымки (покров односторонний,
## `ground_look.gd`: изнутри земли его нет вовсе). Глазом это отличить от вида на
## пологий склон можно, автоматом — нет: цвет тот же, что у настоящего горизонта.
## Поэтому меряется не картинка, а ГЕОМЕТРИЯ: где стоит глаз и где под ним твердь.
##
## # Чем он меряет
##
## Поверхность спрашивается лучом по тверди — тем же, чем ходит человек. Своей
## копии поля высот у клиента нет и быть не может: земля приезжает чанками, а под
## ногами кроме земли бывают балласт, шпалы и плита платформы (тот же довод, что
## у walk_probe).
##
## ОБА СОСТОЯНИЯ В ОДНОМ ПРОГОНЕ. Каждый угол меряется дважды — с упором
## (OrbitCamera.set_solid) и без него, — и «до» с «после» получаются на одном
## мире, одной затравке и одних углах. Порознь их сравнивать было бы нечестно:
## рельеф зависит от засеянной базы, а она у каждого своя.
##
## --headless НЕ ГОДИТСЯ по той же причине, что у остальных зондов: мир строится
## сценой, а сцене нужен вьюпорт. Окно уводится за экран.

const SETTLE_WAIT := 90.0        ## с — сколько ждём мир и человека
const SAY_EVERY_S := 5.0

## Углы перебора. Возвышения — от нижнего предела камеры вверх: беда живёт у
## горизонта, и выше двадцати градусов её не нашёл ни один замер.
const ELEVATIONS := [2.0, 4.0, 8.0]
const AZIMUTH_STEP := 45.0
## Кадры перебора, метры. Зум сам по себе беду не создаёт (высота и вынос
## пропорциональны отступу, отношение не меняется), но МЕСТО, где стоит камера,
## он меняет: при кадре в 20 м она в двадцати метрах от точки взгляда, при
## двухстах — в двух сотнях, и рельеф там другой.
const FRAMES := [20.0, 60.0, 200.0]
## Шаг по пути, метры. МЕСТО РЕШАЕТ ВСЁ: на насыпи-плато под камерой запас в
## десятки метров при любом угле, а рядом с косогором его нет вовсе.
const SPOT_STEP_M := 60.0
const SPOT_MAX := 48
## Отходы от оси, метры. ЧЕЛОВЕК ХОДИТ НЕ ТОЛЬКО ПО ПУТИ, и смотреть он будет
## оттуда, где встал. Вдоль оси земля выровнена земляными работами сервера
## (насыпь, выемка), и замер по одной оси отвечал бы только за неё.
const LATERALS := [-60.0, -25.0, 0.0, 25.0, 60.0]

## Луч, которым спрашивается отметка тверди под камерой. Пускается ВЫШЕ ЛЮБОГО
## рельефа затравки: земля ST_A лежит на 130…145 м.
const PROBE_UP := 400.0
const PROBE_DOWN := 800.0

## Ниже этого запаса кадр уже испорчен: метр — высота самой высокой травы, то же
## число, что GROUND_CLEAR_M у камеры.
const CLEAR_WANT_M := 1.0

var _app: Node
var _t := 0.0
var _said := 0.0
var _fails := 0
var _running := false


func _initialize() -> void:
	# Мир строится ОБОЛОЧКОЙ, как у игрока: зонд не собирает свою сцену, иначе он
	# проверял бы не то, что запускается.
	_app = (load("res://scenes/main.tscn") as PackedScene).instantiate()
	root.add_child(_app)


func _physics_process(delta: float) -> bool:
	_t += delta
	var w := _world()
	if w == null or w._driver == null or not w._driver.is_settled():
		if _t > SETTLE_WAIT:
			printerr("CAMERA PROBE FAIL: за %.0f с не дождались человека — нужны ключи --role=driver --driver-view=orbit"
				% SETTLE_WAIT)
			quit(1)
			return true
		if _t - _said > SAY_EVERY_S:
			_said = _t
			print("зонд: ждём мир — %.0f с из %.0f" % [_t, SETTLE_WAIT])
		return false
	if _running:
		return false
	_running = true
	_measure(w)
	return false


func _measure(w: Node) -> void:
	var cam := root.get_camera_3d() as OrbitCamera
	if cam == null:
		printerr("CAMERA PROBE FAIL: обзорной камеры в сцене нет — вид не орбитальный")
		quit(1)
		return
	var spots := _spots(w)
	if spots.is_empty():
		printerr("CAMERA PROBE FAIL: точек пути не нашлось — мерить негде")
		quit(1)
		return
	print("CAMERA PROBE: точек %d, кадров %d, углов на кадр %d — всего замеров %d" % [
		spots.size(), FRAMES.size(), ELEVATIONS.size() * int(360.0 / AZIMUTH_STEP),
		spots.size() * FRAMES.size() * ELEVATIONS.size() * int(360.0 / AZIMUTH_STEP)])

	# ДВА ПРОГОНА ОДНОГО ПЕРЕБОРА. Первый — с выключенным упором: он и есть «как
	# было». Второй — с включённым.
	var was: Dictionary = await _sweep(w, cam, spots, false)
	var now: Dictionary = await _sweep(w, cam, spots, true)

	print("           худший зазор   углов под землёй   углов ближе %.1f м   из" % CLEAR_WANT_M)
	print("без упора  %+9.2f м   %10d       %10d   %6d" % [
		was["worst"], was["under"], was["tight"], was["total"]])
	print("с упором   %+9.2f м   %10d       %10d   %6d" % [
		now["worst"], now["under"], now["tight"], now["total"]])
	print("CAMERA PROBE: хуже всего без упора — %s" % was["where"])

	# 1. ГЛАВНОЕ: под землёй камера не оказывается ни на одном угле.
	_ok("камера нигде не под землёй", int(now["under"]) == 0,
		"углов под землёй %d из %d, худший зазор %+.2f м" % [
			now["under"], now["total"], now["worst"]])
	# 2. Запас не «ноль с копейками», а объявленный: иначе глаз сидит в траве.
	#    Половина от объявленного — допуск на то, что запас меряется ПО НОРМАЛИ к
	#    задетой грани, а зонд меряет по отвесу: на откосе это разные метры.
	_ok("запас до земли не меньше половины объявленного",
		float(now["worst"]) > CLEAR_WANT_M * 0.5,
		"худший зазор %+.2f м при объявленном %.1f м" % [now["worst"], CLEAR_WANT_M])
	# 3. ПРОВЕРКА НЕ ПУСТАЯ. Без этого пункта зелёный зонд означал бы только, что
	#    мир ровный: если и без упора никто не тонул, мерить было нечего.
	_ok("было чему ломаться", int(was["under"]) > 0,
		"без упора под землёй %d углов из %d" % [was["under"], was["total"]])
	# 4. Угол и кадр не отбираются там, где ничто не мешает: на чистом месте
	#    камера обязана стоять ровно там, куда её послали.
	_ok("на чистом месте упор молчит", int(now["quiet_bad"]) == 0,
		"углов, где упор сработал впустую: %d" % now["quiet_bad"])

	await _shoot(w, cam, was)
	print("CAMERA PROBE %s" % ("OK" if _fails == 0 else "ОТКАЗОВ %d" % _fails))
	quit(_fails)


## _sweep — перебор всех точек и углов. Отдаёт худший зазор, счёт провалов и то
## место, где было хуже всего.
##
## Между углами камера НАВОДИТСЯ ЗАНОВО (configure), а не доворачивается: упор
## отпускает штангу плавно, и остаток от прошлого угла завысил бы зазор на
## следующем — то есть зонд хвалил бы упор за чужую работу.
func _sweep(w: Node, cam: OrbitCamera, spots: Array, on: bool) -> Dictionary:
	cam.set_solid(on)
	var out := {"worst": INF, "where": "", "focus": Vector3.ZERO, "az": 0.0, "el": 0.0,
		"frame": FRAMES[0], "under": 0, "tight": 0, "total": 0, "quiet_bad": 0}
	for spot in spots:
		var focus: Vector3 = spot
		for frame in FRAMES:
			var asked := float(frame) / (2.0 * tan(deg_to_rad(OrbitCamera.CAM_FOV) * 0.5))
			for e in ELEVATIONS:
				var a := 0.0
				while a < 360.0:
					cam.configure(focus, float(frame), a, float(e), false)
					# Шаг физики — упору: он считается там, а не в apply (разбор — в
					# OrbitCamera._physics_process). Без ожидания зонд мерил бы камеру
					# до упора и получил бы «как было» в обоих прогонах.
					await physics_frame
					var p := cam.global_position
					var ground := _ground_y(w, p)
					var az := a
					a += AZIMUTH_STEP
					if not is_finite(ground):
						continue          # тверди под камерой нет вовсе — не наш случай
					out["total"] = int(out["total"]) + 1
					var gap := p.y - ground
					if gap < 0.0:
						out["under"] = int(out["under"]) + 1
					if gap < CLEAR_WANT_M:
						out["tight"] = int(out["tight"]) + 1
					if gap < float(out["worst"]):
						out["worst"] = gap
						out["focus"] = focus
						out["az"] = az
						out["el"] = float(e)
						out["frame"] = float(frame)
						out["where"] = "(%.0f, %.0f) кадр %.0f м, возв. %.0f°, азимут %.0f°, зазор %+.2f м" % [
							focus.x, focus.z, frame, e, az, gap]
					# УПОР ВПУСТУЮ: там, где между точкой взгляда и запрошенным
					# местом камеры НЕТ ТВЕРДИ, камера обязана остаться ровно там,
					# куда её послали.
					#
					# Свобода спрашивается лучом, а не по зазору под камерой, и это
					# правка самого зонда: первая редакция считала «зазор больше трёх
					# метров» признаком чистого места и насчитала 413 ложных тревог.
					# Камера за холмом стоит высоко над землёй и всё равно обязана
					# подтянуться — иначе игрок смотрит в склон.
					if on and cam.standoff_m() < asked - 0.5 and _clear(w, focus, az, float(e), asked):
						out["quiet_bad"] = int(out["quiet_bad"]) + 1
	cam.set_solid(false)
	return out


## _clear — свободен ли путь от точки взгляда до запрошенного места камеры.
## Тем же лучом и по тому же слою, каким его спрашивает сама камера.
func _clear(w: Node, focus: Vector3, az: float, el: float, dist: float) -> bool:
	var a := deg_to_rad(az)
	var e := deg_to_rad(el)
	var dir := Vector3(cos(a), 0.0, sin(a)) * cos(e) + Vector3.UP * sin(e)
	var q := PhysicsRayQueryParameters3D.create(focus, focus + dir * dist)
	q.collision_mask = OrbitCamera.WORLD_SOLID_MASK
	var hit: Dictionary = w.get_world_3d().direct_space_state.intersect_ray(q)
	return hit.is_empty()


## _spots — точки взгляда вдоль ВСЕХ путей, поднятые над головкой рельса.
##
## По всем элементам, а не по тому, на котором стоит человек: рельеф у станции
## один, а у горловины другой, и замер по одному элементу отвечал бы за одно
## место. Элементы берутся у мира — те самые, по которым он катает состав.
##
## Подъём — половина роста человека: обзорная камера вертится вокруг середины его
## фигуры, а не вокруг подошвы (Driver._orbit_focus), и мерить надо ту точку,
## которая будет в игре.
func _spots(w: Node) -> Array:
	var out: Array = []
	for id in w._stock_elements:
		var el: TrackGeom.Element = w._stock_elements[id]
		var u := 0.0
		while u <= el.length_m and out.size() < SPOT_MAX:
			var pose := el.pose_at(u)
			var n := pose.left()
			for lat in LATERALS:
				var at := TerrainMesh.to_godot(pose.x + n.x * float(lat), pose.y + n.y * float(lat), pose.z)
				# ОТМЕТКА — ЛУЧОМ, а не от головки рельса: в стороне от оси земля
				# лежит где хочет, и точка взгляда, взятая по рельсу, повисла бы в
				# воздухе на склоне или утонула бы в косогоре.
				var g := _ground_y(w, at)
				if not is_finite(g):
					continue
				out.append(Vector3(at.x, g + Driver.BODY_H * 0.5, at.z))
			u += SPOT_STEP_M
	return out


## _ground_y — отметка тверди под точкой. Луч сверху вниз, как у walk_probe.
func _ground_y(w: Node, p: Vector3) -> float:
	var from := p + Vector3.UP * PROBE_UP
	var q := PhysicsRayQueryParameters3D.create(from, from + Vector3.DOWN * PROBE_DOWN)
	var hit: Dictionary = w.get_world_3d().direct_space_state.intersect_ray(q)
	if hit.is_empty():
		return NAN
	return (hit["position"] as Vector3).y


## _shoot — два снимка ХУДШЕГО МЕСТА: без упора и с ним. Без ключа --shot= не
## делает ничего.
##
## Оба, а не один: кадр из-под земли выглядит как кадр, и доказательством
## починки служит только пара, снятая с одной точки и одного угла.
func _shoot(w: Node, cam: OrbitCamera, was: Dictionary) -> void:
	var path := ""
	for arg in OS.get_cmdline_user_args():
		if arg.begins_with("--shot="):
			path = arg.substr(7)
	if path == "" or not is_finite(float(was["worst"])):
		return
	w.ui_label.visible = false
	for pair in [["off", false], ["on", true]]:
		cam.set_solid(bool(pair[1]))
		cam.configure(was["focus"] as Vector3, float(was["frame"]),
			float(was["az"]), float(was["el"]), false)
		await physics_frame
		# Кадр НАДО ДОЖДАТЬСЯ, а не форсировать: force_draw сразу после правки
		# сцены отдаёт прошлый кадр (грабля мира, записанная в Makefile).
		await RenderingServer.frame_post_draw
		await RenderingServer.frame_post_draw
		var tex := root.get_texture()
		if tex == null:
			return
		var img := tex.get_image()
		if img == null:
			print("СНИМОК НЕ СДЕЛАН: get_image() вернул null")
			return
		var out := path.replace(".png", "_%s.png" % pair[0])
		var err := img.save_png(out)
		print("СНИМОК %s: %s" % ["СОХРАНЁН" if err == OK else "НЕ СОХРАНЁН", out])
	cam.set_solid(true)


func _ok(name: String, cond: bool, note: String = "") -> void:
	if not cond:
		_fails += 1
	print("  %s %s%s" % ["ok  " if cond else "ОТКАЗ", name, "" if note == "" else "   " + note])


## Мир — узел, который умеет наводиться на машину. По методу, а не по имени
## класса: у world.gd имени класса нет, а искать по пути сцены значит ломаться от
## её переименования.
func _world() -> Node:
	return _find(root)


func _find(node: Node) -> Node:
	if node.has_method("_look_at_machine"):
		return node
	for c in node.get_children():
		var f := _find(c)
		if f != null:
			return f
	return null
