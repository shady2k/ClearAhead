extends "res://scripts/spike_world.gd"
## СПАЙК-ОДНОДНЕВКА №3 — «машинист»: ЧЕЛОВЕК В МИРЕ И ВИД С ВЫСОТЫ ЕГО ГЛАЗ.
##
## Отвечает на один вопрос владельца: как выглядит наш путь не с четырёхсот
## метров, а с полутора — оттуда, откуда на него смотрит человек. Это тот самый
## вид, ради которого в направлении проекта записано «камера — не точка зрения, а
## обязательство о том, что обязано существовать»
## (specs/2026-08-09-project-direction-design.md §2).
##
## Наследует ВЕСЬ мир от spike_world.gd (рельеф, путь, лес, река, посёлок,
## локомотив) и добавляет ровно три вещи, которых там нет:
##
##   1. ТВЕРДЬ. У спайка мира не было ни одной коллизии: он рисовал картинку, по
##      которой не ходят. Здесь каждый нарисованный меш (кроме воды) получает
##      сетчатое тело, и человек стоит на ТОЙ ЖЕ поверхности, которую видит, —
##      не на копии поля высот и не на подобранной константе.
##   2. ПЕРСОНАЖ. Процедурная фигура ростом 1.80 в сигнальном жилете: голова,
##      кузов, руки и ноги отдельными узлами, ноги и руки ходят при шаге. Она
##      нужна не для красоты, а как ЛИНЕЙКА: пока в кадре нет человека, ошибка
##      масштаба в пути, платформе и локомотиве ничем не проверяется.
##   3. ТРИ ВИДА, а не один: обзор (родительская орбита), от первого лица и от
##      третьего. Переключение — V.
##
## ЧТО ЭТОТ СПАЙК НЕ ДЕЛАЕТ. Он не заводит ни физику поезда, ни кабину, ни
## управление локомотивом: направление проекта держит ядро дискретным, и «машинист
## — мечта, а не цель» (§2 там же). Здесь человек ХОДИТ по станции, а не ведёт
## состав. Ничего из этого не трогает ни контракт, ни боевой клиент: как и оба
## предыдущих спайка, файл живёт сам по себе и ничего никому не навязывает.
##
## ИЗВЕСТНЫЕ ГРАНИЦЫ, чтобы их не открывали заново:
##   * лес, кусты и трава ПРОХОДИМЫ НАСКВОЗЬ — они MultiMesh, а у него коллизий
##     нет вовсе; ставить сорок тысяч капсул на деревья ради спайка незачем;
##   * на платформу (1.05 м над землёй) не взойти: шаг берёт ступень STEP_UP, а
##     лестниц в геометрии нет — их нет и в контракте;
##   * по реке ходят посуху: вода нарочно исключена из тверди (см. _add_water).
##
## Запуск: make fpv / make fpv-shot / make walk-probe (нужен Forward+).

## --- откуда начинает человек -------------------------------------------------
## Точка задаётся В КООРДИНАТАХ ПУТИ (элемент, u вдоль него, латераль от оси), а
## не в метрах мира: мир не имеет единого начала координат, и любая зашитая пара
## x,z развалится от первой же правки фикстуры. Латераль отрицательная — та же
## сторона, что у платформы (side = "right", spike_relief.gd:999).
static var fpv_u := 62.0         # м вдоль LOCO_ELEMENT: локомотив стоит на 78
static var fpv_lat := -6.0       # м от оси: за дальней кромкой платформы (4.75)
static var fpv_yaw := 0.0        # градусы от направления пути: 0 — вдоль, на машину
static var fpv_pitch := -3.0     # градусы: чуть вниз, чтобы в кадр вошли ноги пути
static var fpv_mode := 1         # каким видом открываться: 0 обзор, 1 глаза, 2 третье

## --- сложение человека -------------------------------------------------------
## Числа — с натуры: рост 1.80, глаза на 6-7 см ниже темени, плечи 0.44, шаг
## (полный цикл, две ноги) 1.5 м. Всё это ЛИНЕЙКА для остального кадра, поэтому
## подгонять их под картинку нельзя — подгонять надо картинку.
const BODY_H := 1.80             # м — рост с кепкой
const EYE_H := 1.66              # м — высота глаз над подошвой
const HIP_H := 0.90              # м — тазобедренный сустав
const SHOULDER_H := 1.42         # м — плечевой сустав
const NECK_H := 1.46             # м — верх кузова
const HEAD_R := 0.098            # м — полуширина головы
const BODY_HALF_W := 0.22        # м — полуширина плеч
const BODY_HALF_D := 0.13        # м — полутолщина кузова
const CAPSULE_R := 0.30          # м — радиус капсулы: по плечам, а не по животу
const STRIDE := 1.50             # м — путь за полный цикл маха

## --- ход ---------------------------------------------------------------------
## Скорости человека, а не игрового персонажа: 1.45 м/с — обычный шаг по путям
## (5.2 км/ч), 3.9 м/с — бег трусцой. Быстрее нельзя: на станции длиной 900 м
## завышенная скорость мгновенно врёт про размер — а размер здесь и проверяется.
const WALK_SPEED := 1.45         # м/с
const RUN_SPEED := 3.90          # м/с
const ACCEL := 14.0              # м/с² — разгон и торможение до целевой скорости
const JUMP_V := 3.10             # м/с — прыжок примерно на полметра
const AIR_CONTROL := 0.35        # доля разгона, доступная в воздухе

## СТУПЕНЬ, КОТОРУЮ БЕРЁТ ШАГ. Не косметика: без неё человек упирается в первую
## же шпалу. Отметки, ради которых число именно такое (все от подошвы балласта):
## шпала 0.20, головка рельса 0.36, борт откоса призмы — наклонный и берётся
## обычным ходом. Платформа 1.05 сюда не входит нарочно: человек не запрыгивает
## на неё с земли, он идёт к торцу — которого в геометрии пока нет.
const STEP_UP := 0.42            # м
const STEP_PROGRESS := 0.35      # доля намеченного пути, ниже которой считаем «упёрся»

## --- взгляд ------------------------------------------------------------------
const MOUSE_SENS := 0.11         # градусов на пиксель движения мыши
const PITCH_LIMIT := 87.0        # градусы — предел наклона, чтобы не переворачиваться
const FPV_FOV := 72.0            # градусы — поле зрения от первого лица
const TP_DIST := 3.6             # м — камера от третьего лица позади человека
const TP_HEIGHT := 1.70          # м — и на этой высоте
const BOB_AMP := 0.020           # м — качание головы при шаге; не больше, иначе укачивает

## --- палитра фигуры ----------------------------------------------------------
## Сигнальный жилет — единственное яркое пятно на человеке, и это правда: по
## путям иначе не ходят. Остальное тёмное и матовое, как форменная одежда.
const C_VEST := Color(0.82, 0.34, 0.04)      # сигнальный жилет
const C_VEST_TAPE := Color(0.74, 0.75, 0.77) # светоотражающая лента
const C_CLOTH := Color(0.15, 0.18, 0.25)     # куртка и брюки
const C_BOOT := Color(0.10, 0.10, 0.11)      # ботинки
const C_SKIN := Color(0.63, 0.46, 0.36)      # лицо и кисти
const C_CAP := Color(0.11, 0.13, 0.18)       # форменная кепка

const MODE_ORBIT := 0
const MODE_EYE := 1
const MODE_THIRD := 2
const MODE_NAMES := ["обзор", "от первого лица", "от третьего лица"]

var _driver: CharacterBody3D
var _rig: Node3D                 # всё тело: его вращает рыскание
var _head: Node3D                # голова: её наклоняет тангаж
var _eye: Node3D                 # точка глаза, куда садится камера
var _leg_l: Node3D
var _leg_r: Node3D
var _arm_l: Node3D
var _arm_r: Node3D
var _head_meshes := []           # что прячется от собственных глаз
var _mode := MODE_EYE
var _yaw := 0.0                  # градусы — рыскание тела
var _pitch := 0.0                # градусы — тангаж головы
var _gait := 0.0                 # м — накопленный путь, по нему считается мах
var _home := Vector3.ZERO        # куда возвращает F
var _no_solid := {}              # узлы, которые НЕ становятся твердью
var _driver_ready := false
var _settle_pending := true      # человек ещё не поставлен лучом на твердь

func _ready() -> void:
	_mode = clampi(fpv_mode, MODE_ORBIT, MODE_THIRD)
	super()                       # весь мир: путь, земля, лес, локомотив, свет
	_tune_shadows()
	_build_collision()
	_spawn_driver()
	# Трава сажается вокруг точки взгляда, а человек встаёт уже после сборки
	# мира — поэтому посадка отложена (см. _add_grass) и делается здесь, когда
	# известно, где он стоит.
	_driver_ready = true
	_cam_focus = Vector3(_driver.global_position.x, 0.0, _driver.global_position.z)
	_add_grass()
	_apply_mode(_mode, false)
	print("FPV: V — вид (обзор / первое лицо / третье) · мышь — взгляд · WASD — идти · "
		+ "Shift — бежать · пробел — прыжок · F — вернуться к локомотиву · Esc — отпустить мышь")

## ТЕНЬ НАСТРОЕНА НА ДРУГОЙ ПЛАН, И ЭТО ВИДНО ИМЕННО С ЗЕМЛИ. У мира она
## растянута на 900 м — под кадр, где станция помещается целиком. С высоты глаз
## те же четыре каскада приходится делить между шпалой под ногами и лесом на
## километре, и под ногами не остаётся разрешения вовсе: тень от собственной
## фигуры расползается в серое пятно. Сто восемьдесят метров — это всё, что
## человек и так различает как отдельные предметы.
func _tune_shadows() -> void:
	for c in get_children():
		var light := c as DirectionalLight3D
		if light == null or not light.shadow_enabled:
			continue
		light.directional_shadow_max_distance = 180.0
		light.directional_shadow_split_1 = 0.06
		light.directional_shadow_split_2 = 0.18
		light.directional_shadow_split_3 = 0.45

## --- твердь ------------------------------------------------------------------
## КОЛЛИЗИЯ БЕРЁТСЯ ИЗ ТЕХ ЖЕ МЕШЕЙ, КОТОРЫЕ ВИДНЫ, а не считается отдельно.
##
## Соблазн был обратный: поле высот — чистая функция координат (_height_at), и
## поставить человека на неё формулой стоило бы три строки. Так делать нельзя по
## той же причине, по которой в проекте заведён `rgk`: вторая формула той же
## поверхности расходится с первой на первой же правке. И расходится она тут
## особенно нагло — балласт, шпалы, платформа и локомотив в поле высот не
## существуют вовсе, а человек по ним ходит.
##
## Что остаётся вне тверди:
##   * вода — по ней ходят вброд, а не пешком (помечена в _add_water);
##   * лес, кусты, трава — MultiMesh, у него коллизий нет в принципе.
func _build_collision() -> void:
	var t0 := Time.get_ticks_usec()
	var tris := 0
	var bodies := 0
	for mi in _solid_meshes(self):
		mi.create_trimesh_collision()
		bodies += 1
		tris += _mesh_tris(mi.mesh)
	print("FPV: твердь — %d тел, %d треугольников, %.0f мс"
		% [bodies, tris, (Time.get_ticks_usec() - t0) / 1000.0])
	if _loco_node != null:
		_set_solid(_loco_node, _loco_node.visible)

## ТВЕРДЬ ЛОКОМОТИВА ГАСНЕТ ВМЕСТЕ С НИМ. Видимость на коллизии не влияет вовсе,
## поэтому спрятанная клавишей L машина осталась бы невидимой стеной поперёк
## станции — а это неотличимо от поломки физики и стоит часа поисков.
func _toggle_loco() -> void:
	super()
	if _loco_node != null:
		_set_solid(_loco_node, _loco_node.visible)

func _set_solid(node: Node, on: bool) -> void:
	if node is CollisionShape3D:
		(node as CollisionShape3D).disabled = not on
	for c in node.get_children():
		_set_solid(c, on)

## Обход поддерева: MeshInstance3D, не помеченные как «не твердь». Рекурсия, а не
## get_children: модель локомотива приходит из .glb своим деревом узлов.
func _solid_meshes(node: Node) -> Array:
	var out := []
	if _no_solid.has(node):
		return out
	if node is MeshInstance3D and (node as MeshInstance3D).mesh != null:
		out.append(node)
	for c in node.get_children():
		out.append_array(_solid_meshes(c))
	return out

## Треугольников в меше — по длине массивов, а не через get_faces(): тот
## разворачивает всю геометрию в новый массив, а у земли это четверть миллиона
## треугольников, которые тут же выбрасываются.
func _mesh_tris(mesh: Mesh) -> int:
	var n := 0
	for s in mesh.get_surface_count():
		var am := mesh as ArrayMesh
		if am == null:
			continue
		var idx := am.surface_get_array_index_len(s)
		n += (idx if idx > 0 else am.surface_get_array_len(s)) / 3
	return n

## Вода рисуется, но твердью не становится. Помечается ЧЕРЕЗ РАЗНИЦУ ДЕТЕЙ, а не
## по имени узла: _commit имён не даёт, и опознавать ленту по материалу значило бы
## завязаться на его настройки.
func _add_water() -> void:
	var before := get_child_count()
	super()
	for i in range(before, get_child_count()):
		_no_solid[get_child(i)] = true

## Посадка травы ждёт человека: круг сажается вокруг _cam_focus, а он до
## _spawn_driver показывает на точку съёмки родителя. Без этой отсрочки трава
## строится дважды — сперва не там, потом там.
func _add_grass() -> void:
	if not _driver_ready:
		return
	super()

## --- человек -----------------------------------------------------------------
func _spawn_driver() -> void:
	_driver = CharacterBody3D.new()
	_driver.name = "Driver"
	# Пол наклонный почти всюду: откос призмы 1:1.5 — это 34°, и с обычными 45°
	# человек по нему ходит, а не съезжает. Прилипание нужно на спуске: без него
	# каждая шпала подбрасывает и ход превращается в скачки.
	_driver.floor_max_angle = deg_to_rad(52.0)
	_driver.floor_snap_length = 0.6
	_driver.up_direction = Vector3.UP
	add_child(_driver)

	var shape := CollisionShape3D.new()
	var cap := CapsuleShape3D.new()
	cap.radius = CAPSULE_R
	cap.height = BODY_H - 0.06        # темя чуть выше капсулы: кепка не упирается в притолоку
	shape.shape = cap
	shape.position = Vector3(0.0, cap.height * 0.5, 0.0)
	_driver.add_child(shape)

	_build_rig()

	var start := _driver_start()
	_home = start
	_driver.global_position = start
	_yaw = _start_yaw()
	_pitch = fpv_pitch
	_apply_look()

## Начальная точка — В КООРДИНАТАХ ПУТИ; отметка берётся С ЗАПАСОМ ВВЕРХ, а точную
## находит луч на первом же шаге физики (_settle).
##
## Ставить по полю высот напрямую нельзя, и это не мелочь: поле высот не знает ни
## про балласт, ни про платформу, ни про локомотив. На оси пути «отметка земли»
## — это ПОЛКА ПОД ПРИЗМОЙ, то есть на полметра ниже щебня, и человек оказался бы
## по пояс в насыпи. Спрашивать надо ту же твердь, по которой он потом пойдёт.
func _driver_start() -> Vector3:
	var el: Dictionary = _elements.get(LOCO_ELEMENT, {})
	var p := Vector3.ZERO
	if not el.is_empty():
		var fr := _rail_frame(el, fpv_u)
		if not fr.is_empty():
			p = fr.o + (fr.lat as Vector3) * fpv_lat
	return Vector3(p.x, _height_at(p.x, p.z) + SETTLE_LIFT, p.z)

## ПОСТАНОВКА ЛУЧОМ, А НЕ ПАДЕНИЕМ. Падение выглядит тем же самым, но снимать
## снимок пришлось бы «через сколько-нибудь кадров, наверное, хватит» — а это
## гадание, которое ломается от первого же места, где падать выше. Луч даёт
## точную отметку за один шаг физики.
##
## В _ready луч пустить нельзя: тела тверди созданы в этом же кадре, и физический
## сервер о них ещё не знает — луч уходит в пустоту.
const SETTLE_LIFT := 2.4         # м — насколько человек ставится выше поля высот
const SETTLE_DROP := 12.0        # м — докуда ищется твердь под ним

func _settle() -> void:
	_settle_pending = false
	var from := _driver.global_position + Vector3.UP * 0.2
	var q := PhysicsRayQueryParameters3D.create(from, from + Vector3.DOWN * SETTLE_DROP)
	q.exclude = [_driver.get_rid()]
	var hit := _driver.get_world_3d().direct_space_state.intersect_ray(q)
	if hit.is_empty():
		printerr("FPV: под машинистом нет тверди на %.0f м вниз — оставлен где стоял" % SETTLE_DROP)
		return
	var p: Vector3 = hit.position
	_driver.global_position = p + Vector3.UP * 0.01
	_driver.velocity = Vector3.ZERO
	_home = _driver.global_position
	print("FPV: машинист на %.1f,%.1f, отметка %.2f м (земля %.2f)"
		% [p.x, p.z, p.y, _height_at(p.x, p.z)])

## Рыскание отсчитывается ОТ НАПРАВЛЕНИЯ ПУТИ, а не от оси X мира: путь на кривой,
## и «смотреть вдоль» на языке мировых углов — разное число в каждой точке.
func _start_yaw() -> float:
	var el: Dictionary = _elements.get(LOCO_ELEMENT, {})
	if el.is_empty():
		return fpv_yaw
	var fr := _rail_frame(el, fpv_u)
	if fr.is_empty():
		return fpv_yaw
	var d: Vector3 = fr.dir
	# Godot: -Z вперёд, поэтому азимут считается от -Z против часовой.
	return rad_to_deg(atan2(-d.x, -d.z)) + fpv_yaw

## Фигура: узлы суставов + меши на них. Отдельные узлы нужны не ради анимации как
## таковой — стоящий столбиком человек читается манекеном, и как линейка работает
## хуже: глаз не верит масштабу, которому не верит поза.
func _build_rig() -> void:
	_rig = Node3D.new()
	_rig.name = "Rig"
	_driver.add_child(_rig)

	var mat := StandardMaterial3D.new()
	mat.albedo_color = Color(1, 1, 1)
	mat.vertex_color_use_as_albedo = true
	mat.roughness = 0.82

	# Кузов с жилетом — неподвижная часть.
	var torso := SurfaceTool.new()
	torso.begin(Mesh.PRIMITIVE_TRIANGLES)
	_add_torso(torso)
	_rig.add_child(_mesh_node(torso, mat, "Torso"))

	# Голова на своём узле: тангаж наклоняет её вместе с камерой.
	_head = Node3D.new()
	_head.name = "Head"
	_head.position = Vector3(0.0, NECK_H, 0.0)
	_rig.add_child(_head)
	var head := SurfaceTool.new()
	head.begin(Mesh.PRIMITIVE_TRIANGLES)
	_add_head(head)
	var head_mi := _mesh_node(head, mat, "HeadMesh")
	_head.add_child(head_mi)
	_head_meshes.append(head_mi)

	# ГЛАЗ — НЕ РЕБЁНОК ГОЛОВЫ, А СВОЙ УЗЕЛ. Разница не косметическая: если камера
	# висит на голове, то наклон взгляда ПОВОРАЧИВАЕТ ЕЁ ВОКРУГ ШЕИ, и взгляд под
	# ноги уносит точку зрения на двадцать сантиметров вперёд и вниз. Наружу это
	# выходит так, что мир при взгляде вниз «подъезжает» — тошнотворно и ни на что
	# не похоже. Глаз вращается НА МЕСТЕ, а голова наклоняется отдельно, ради
	# правильной тени и вида со стороны.
	_eye = Node3D.new()
	_eye.name = "Eye"
	_eye.position = Vector3(0.0, EYE_H, -HEAD_R * 0.55)
	_rig.add_child(_eye)

	_leg_l = _limb_node("LegL", Vector3(-0.105, HIP_H, 0.0), _make_leg(mat))
	_leg_r = _limb_node("LegR", Vector3(0.105, HIP_H, 0.0), _make_leg(mat))
	_arm_l = _limb_node("ArmL", Vector3(-BODY_HALF_W - 0.03, SHOULDER_H, 0.0), _make_arm(mat))
	_arm_r = _limb_node("ArmR", Vector3(BODY_HALF_W + 0.03, SHOULDER_H, 0.0), _make_arm(mat))

func _limb_node(name_v: String, at: Vector3, mesh_node: MeshInstance3D) -> Node3D:
	var n := Node3D.new()
	n.name = name_v
	n.position = at
	n.add_child(mesh_node)
	_rig.add_child(n)
	return n

func _mesh_node(tool: SurfaceTool, mat: Material, name_v: String) -> MeshInstance3D:
	var mi := MeshInstance3D.new()
	mi.mesh = tool.commit()
	mi.material_override = mat
	mi.name = name_v
	return mi

## Кузов: куртка, поверх неё жилет с двумя лентами. Жилет чуть шире куртки —
## иначе две коробки в одной плоскости дерутся за пиксели (тот же дефект, что был
## у жёлтой кромки платформы).
func _add_torso(tool: SurfaceTool) -> void:
	_body_box(tool, Vector3(0.0, (HIP_H + NECK_H) * 0.5, 0.0),
		Vector3(BODY_HALF_W, (NECK_H - HIP_H) * 0.5, BODY_HALF_D), C_CLOTH)
	var vest_y0 := HIP_H + 0.06
	var vest_y1 := SHOULDER_H - 0.02
	var hw := BODY_HALF_W + 0.012
	var hd := BODY_HALF_D + 0.012
	_body_box(tool, Vector3(0.0, (vest_y0 + vest_y1) * 0.5, 0.0),
		Vector3(hw, (vest_y1 - vest_y0) * 0.5, hd), C_VEST)
	for y in [vest_y0 + 0.10, vest_y1 - 0.13]:
		_body_box(tool, Vector3(0.0, y, 0.0),
			Vector3(hw + 0.006, 0.032, hd + 0.006), C_VEST_TAPE)
	# Шея — иначе голова висит над плечами отдельным кубиком.
	_body_box(tool, Vector3(0.0, NECK_H + 0.03, 0.0), Vector3(0.055, 0.045, 0.055), C_SKIN)

## Голова строится ОТ УЗЛА ШЕИ (локальный ноль — на NECK_H), поэтому все отметки
## здесь относительные.
func _add_head(tool: SurfaceTool) -> void:
	var top := BODY_H - 0.08 - NECK_H          # темя без кепки
	_body_box(tool, Vector3(0.0, top * 0.5 + 0.04, 0.0),
		Vector3(HEAD_R, top * 0.5 - 0.02, HEAD_R * 1.06), C_SKIN)
	# Кепка: тулья и козырёк вперёд (-Z).
	_body_box(tool, Vector3(0.0, top + 0.03, 0.0),
		Vector3(HEAD_R + 0.008, 0.045, HEAD_R * 1.07), C_CAP)
	_body_box(tool, Vector3(0.0, top - 0.005, -HEAD_R * 1.06 - 0.035),
		Vector3(HEAD_R * 0.92, 0.012, 0.038), C_CAP)

## Нога от бедра вниз: локальный ноль в суставе, всё вниз по -Y.
func _make_leg(mat: Material) -> MeshInstance3D:
	var tool := SurfaceTool.new()
	tool.begin(Mesh.PRIMITIVE_TRIANGLES)
	_body_box(tool, Vector3(0.0, -HIP_H * 0.5 + 0.04, 0.0),
		Vector3(0.078, HIP_H * 0.5 - 0.04, 0.085), C_CLOTH)
	_body_box(tool, Vector3(0.0, -HIP_H + 0.055, -0.025),
		Vector3(0.075, 0.055, 0.125), C_BOOT)
	return _mesh_node(tool, mat, "Leg")

## Рука от плеча вниз: рукав и кисть.
func _make_arm(mat: Material) -> MeshInstance3D:
	var tool := SurfaceTool.new()
	tool.begin(Mesh.PRIMITIVE_TRIANGLES)
	var len_v := SHOULDER_H - HIP_H + 0.22
	_body_box(tool, Vector3(0.0, -len_v * 0.5 + 0.05, 0.0),
		Vector3(0.055, len_v * 0.5 - 0.05, 0.06), C_CLOTH)
	_body_box(tool, Vector3(0.0, -len_v + 0.05, 0.0),
		Vector3(0.05, 0.055, 0.055), C_SKIN)
	return _mesh_node(tool, mat, "Arm")

## Коробка с ЯВНЫМИ наружными нормалями и всеми шестью гранями. Родительский
## _box_c для фигуры не годится: он темнит боковины (это правило для домов,
## которые смотрят крышей вверх) и не кладёт дно — а на человека смотрят снизу
## вверх, когда он стоит на платформе.
##
## Обмотка подгоняется ПРОВЕРКОЙ, а не на глаз: в проекте лицевой считается
## грань, у которой (p1-p0)×(p2-p0) смотрит ВНУТРЬ тела (см. _box_c и NORMAL_SIGN
## в spike_world.gd). Ошибиться здесь значит получить человека-невидимку,
## у которого видно только изнанку спины.
func _body_box(tool: SurfaceTool, c: Vector3, h: Vector3, col: Color) -> void:
	var x := Vector3(h.x, 0.0, 0.0)
	var y := Vector3(0.0, h.y, 0.0)
	var z := Vector3(0.0, 0.0, h.z)
	_body_face(tool, c + y, x, z, Vector3.UP, col)
	_body_face(tool, c - y, x, z, Vector3.DOWN, col)
	_body_face(tool, c + z, x, y, Vector3.BACK, col)
	_body_face(tool, c - z, x, y, Vector3.FORWARD, col)
	_body_face(tool, c + x, y, z, Vector3.RIGHT, col)
	_body_face(tool, c - x, y, z, Vector3.LEFT, col)

func _body_face(tool: SurfaceTool, c: Vector3, a: Vector3, b: Vector3,
		outward: Vector3, col: Color) -> void:
	var p0 := c + a + b
	var p1 := c - a + b
	var p2 := c - a - b
	var p3 := c + a - b
	if (p1 - p0).cross(p2 - p0).dot(outward) > 0.0:
		var t := p1
		p1 = p3
		p3 = t
	_quad_cn(tool, p0, p1, p2, p3, outward, outward, outward, outward, col, col, col, col)

## --- ход ---------------------------------------------------------------------
func _physics_process(delta: float) -> void:
	if _driver == null:
		return
	if _settle_pending:
		_settle()
		return
	if _mode == MODE_ORBIT:
		return
	var want := Vector2.ZERO
	if Input.is_key_pressed(KEY_W) or Input.is_key_pressed(KEY_UP):
		want.y -= 1.0
	if Input.is_key_pressed(KEY_S) or Input.is_key_pressed(KEY_DOWN):
		want.y += 1.0
	if Input.is_key_pressed(KEY_D) or Input.is_key_pressed(KEY_RIGHT):
		want.x += 1.0
	if Input.is_key_pressed(KEY_A) or Input.is_key_pressed(KEY_LEFT):
		want.x -= 1.0
	var speed := RUN_SPEED if Input.is_key_pressed(KEY_SHIFT) else WALK_SPEED
	var dir := (_driver.global_transform.basis * Vector3(want.x, 0.0, want.y))
	dir.y = 0.0
	if dir.length() > 0.001:
		dir = dir.normalized()
	var target := dir * speed
	var rate := ACCEL * delta * (1.0 if _driver.is_on_floor() else AIR_CONTROL)
	_driver.velocity.x = move_toward(_driver.velocity.x, target.x, rate)
	_driver.velocity.z = move_toward(_driver.velocity.z, target.z, rate)
	if _driver.is_on_floor():
		if Input.is_key_pressed(KEY_SPACE):
			_driver.velocity.y = JUMP_V
		elif _driver.velocity.y < 0.0:
			_driver.velocity.y = 0.0
	else:
		_driver.velocity.y -= float(ProjectSettings.get_setting("physics/3d/default_gravity", 9.8)) * delta
	_move_with_step(delta)
	# Мах ног считается по ПРОЙДЕННОМУ ПУТИ, а не по времени: тогда шаг остаётся
	# шагом и при разгоне, и на бегу, и ноги не скользят по земле.
	var ground_v := Vector2(_driver.velocity.x, _driver.velocity.z).length()
	_gait += ground_v * delta
	_animate(ground_v)

## ХОД СО СТУПЕНЬКОЙ. У CharacterBody3D её нет: он либо въезжает на наклон, либо
## упирается в вертикальную стенку — а шпала, головка рельса и борт плиты это
## ровно вертикальные стенки высотой 20-36 см. Без этого человек не переходит
## через путь вовсе, и весь спайк теряет смысл.
##
## Приём стандартный: сходили обычным ходом; если упёрлись (прошли меньше трети
## намеченного), откатились, поднялись на STEP_UP, сходили ещё раз и прилипли к
## полу. Проверка «упёрлись» по ПРОЙДЕННОМУ, а не по is_on_wall(): скольжение
## вдоль стены — тоже касание стены, но человек при этом идёт, и поднимать его
## там незачем.
func _move_with_step(delta: float) -> void:
	var from := _driver.global_position
	var vel := _driver.velocity
	var want := Vector2(vel.x, vel.z).length() * delta
	_driver.move_and_slide()
	if want < 0.001 or not _driver.is_on_floor():
		return
	var gone := Vector2(_driver.global_position.x - from.x, _driver.global_position.z - from.z).length()
	if gone >= want * STEP_PROGRESS:
		return
	var lifted := _driver.global_transform
	lifted.origin = from + Vector3.UP * STEP_UP
	if _driver.test_move(lifted, Vector3.ZERO):
		return                      # на ступеньке занято — это стена, а не ступень
	_driver.global_position = lifted.origin
	_driver.velocity = vel
	_driver.move_and_slide()
	_driver.apply_floor_snap()

## Мах конечностей: синус от пройденного пути. Руки в противофазе ногам — иначе
## получается не ходьба, а строевой шаг.
func _animate(ground_v: float) -> void:
	var swing: float = clampf(ground_v / WALK_SPEED, 0.0, 2.2)
	var a := sin(_gait * TAU / STRIDE) * deg_to_rad(30.0) * swing
	_leg_l.rotation.x = a
	_leg_r.rotation.x = -a
	_arm_l.rotation.x = -a * 0.62
	_arm_r.rotation.x = a * 0.62

## --- камера и виды -----------------------------------------------------------
func _process(delta: float) -> void:
	if _mode == MODE_ORBIT:
		super(delta)              # орбита, панорама клавишами и подсадка травы
		return
	if _driver == null:
		return
	# Трава живёт вокруг _cam_focus, и в этих видах точка взгляда — сам человек.
	# Родительский _process звать нельзя: там те же WASD уводят камеру.
	_cam_focus = Vector3(_driver.global_position.x, 0.0, _driver.global_position.z)
	_grass_tick()
	_place_camera()

func _place_camera() -> void:
	if _camera == null:
		return
	_camera.projection = Camera3D.PROJECTION_PERSPECTIVE
	_camera.fov = FPV_FOV
	_camera.near = 0.05
	_camera.far = 4000.0
	_camera.current = true
	if _mode == MODE_EYE:
		var xf := _eye.global_transform
		# Качание при шаге. Амплитуда мала нарочно: это подсказка о движении, а не
		# аттракцион — на большой человека укачивает быстрее, чем он дойдёт до горловины.
		xf.origin += Vector3.UP * (sin(_gait * TAU / (STRIDE * 0.5)) * BOB_AMP)
		_camera.global_transform = xf
		return
	# Третье лицо: камера позади и выше, смотрит человеку в затылок.
	var back := _driver.global_transform.basis.z
	var eye_p := _driver.global_position + Vector3.UP * TP_HEIGHT + back * TP_DIST
	_camera.global_position = eye_p
	_camera.look_at(_driver.global_position + Vector3.UP * (EYE_H - 0.15), Vector3.UP)

func _apply_look() -> void:
	if _driver == null:
		return
	_driver.rotation.y = deg_to_rad(_yaw)
	_eye.rotation.x = deg_to_rad(_pitch)
	# Голова идёт за взглядом, но не складывается пополам: шея у человека берёт
	# около шестидесяти градусов, а глаз — все восемьдесят семь.
	_head.rotation.x = deg_to_rad(clampf(_pitch, -60.0, 60.0))

## Смена вида. Мышь захватывается только в видах от лица: захваченный курсор,
## из которого нельзя выйти, — ловушка, а Esc обязан работать всегда.
func _apply_mode(mode: int, say: bool = true) -> void:
	_mode = mode
	for mi in _head_meshes:
		# Своя голова из своих глаз не видна, но ТЕНЬ от неё видна и нужна: без
		# головы тень на балласте читается обезглавленной.
		(mi as MeshInstance3D).cast_shadow = GeometryInstance3D.SHADOW_CASTING_SETTING_SHADOWS_ONLY \
			if mode == MODE_EYE else GeometryInstance3D.SHADOW_CASTING_SETTING_ON
	if _rig != null:
		_rig.visible = true
	if mode == MODE_ORBIT:
		Input.set_mouse_mode(Input.MOUSE_MODE_VISIBLE)
		_apply_camera()
	else:
		Input.set_mouse_mode(Input.MOUSE_MODE_CAPTURED)
		_apply_look()
		_place_camera()
	if say:
		print("FPV: вид — %s" % MODE_NAMES[mode])

## Вернуть курсор после паузы оболочки. Публичный метод, потому что решение
## «захватывать или отпустить» принадлежит ВИДУ, а вид знает только мир: в
## обзоре курсор свободен, в двух других захвачен. Оболочка, угадывающая это за
## мир, разошлась бы с ним при первом же новом виде.
func resume_input() -> void:
	if _mode == MODE_ORBIT:
		Input.set_mouse_mode(Input.MOUSE_MODE_VISIBLE)
	else:
		Input.set_mouse_mode(Input.MOUSE_MODE_CAPTURED)

func _unhandled_input(event: InputEvent) -> void:
	if event is InputEventKey and event.pressed and not event.echo:
		match event.keycode:
			KEY_V:
				_apply_mode((_mode + 1) % 3)
				get_viewport().set_input_as_handled()
				return
			KEY_ESCAPE:
				if _mode != MODE_ORBIT:
					_apply_mode(MODE_ORBIT)
				else:
					Input.set_mouse_mode(Input.MOUSE_MODE_VISIBLE)
				get_viewport().set_input_as_handled()
				return
			KEY_F:
				if _mode != MODE_ORBIT:
					_go_home()
					get_viewport().set_input_as_handled()
					return
			KEY_L:
				# Единственная клавиша родителя, которая осмысленна и с высоты глаз:
				# убрать машину и посмотреть, что за ней. Остальные (P, зум) правят
				# состояние обзорной камеры, которое в этих видах всё равно
				# перезаписывается каждый кадр.
				if _mode != MODE_ORBIT:
					_toggle_loco()
					get_viewport().set_input_as_handled()
					return
	if _mode == MODE_ORBIT:
		super(event)
		return
	# ВЗГЛЯД МЫШЬЮ ЖИВЁТ ТОЛЬКО ПРИ ЗАХВАЧЕННОМ КУРСОРЕ. Проверка не формальность:
	# при отпущенном курсоре относительное движение приходит рывками на границе
	# окна, и вид дёргается сам по себе — тот же класс бага, что зум на трекпаде.
	if event is InputEventMouseMotion and Input.get_mouse_mode() == Input.MOUSE_MODE_CAPTURED:
		_look_by((event as InputEventMouseMotion).relative)
		get_viewport().set_input_as_handled()
		return
	if event is InputEventMouseButton and (event as InputEventMouseButton).pressed \
			and Input.get_mouse_mode() != Input.MOUSE_MODE_CAPTURED:
		Input.set_mouse_mode(Input.MOUSE_MODE_CAPTURED)
		get_viewport().set_input_as_handled()

## Поворот взгляда. Вынесен отдельным методом, чтобы его звал зонд: подать мыши
## «настоящее» относительное движение через parse_input_event нельзя так же
## надёжно, как клавишу, а проверять поворот всё равно надо.
func _look_by(rel: Vector2) -> void:
	_yaw -= rel.x * MOUSE_SENS
	_pitch = clampf(_pitch - rel.y * MOUSE_SENS, -PITCH_LIMIT, PITCH_LIMIT)
	_apply_look()

## Обратный билет: та же клавиша F, что возвращает камеру на локомотив в обзоре.
## На карте два на полтора километра пешком теряются за минуту.
func _go_home() -> void:
	_driver.velocity = Vector3.ZERO
	_driver.global_position = _home
	_yaw = _start_yaw()
	_pitch = fpv_pitch
	_apply_look()
	print("FPV: машинист вернулся к локомотиву")
