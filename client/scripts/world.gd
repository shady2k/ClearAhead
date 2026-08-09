extends Node2D
## World — всё, что в метрах и зумится: балласт, рельсы, шпалы.
## Дети рисуются в координатах СЕРВЕРА (Y вверх); станцию переворачивает
## ОДИН минус — transform этого узла:
##
##     scale.y = -1
##
## Больше нигде в отрисовке знака y нет: ни в тесселяции дуг (geometry_math.gd),
## ни в смещениях, ни в углах. Второй минус в любом месте сделает станцию
## зеркальной, и это незаметно. Debug — дочерний узел World и наследует тот же
## переворот; отдельного scale.y = -1 нигде больше нет.

const GM := preload("res://scripts/geometry_math.gd")
const SleeperLayer := preload("res://scripts/sleeper_layer.gd")
const TracksideDrawer := preload("res://scripts/trackside_layer.gd")
const TurnoutDrawer := preload("res://scripts/turnout_layer.gd")

enum Lod { SIMPLE = 0, MID = 1, FULL = 2 }

const LOD_MID_ZOOM := 1.5    # px/м — ниже: одна линия на путь
const LOD_FULL_ZOOM := 5.0   # px/м — выше: добавляются шпалы

## Инженерные размеры приходят из типа путевой конструкции контракта
## (geometry.track_types, спека §3): gauge, sleeper{pitch,length,width},
## ballast{half_width}. Своих метрических констант пути здесь больше нет.
## Платформа — отдельный объект со своими размерами (спека §3); их довозит
## следующая задача, пока остаются локальными.
const PLATFORM_OFFSET := 1.75 # м — ближняя кромка платформы от оси пути
const PLATFORM_WIDTH := 3.0   # м — ширина платформы
const PLATFORM_COLOR := Color(0.70, 0.70, 0.72)
const FROG_WING := 1.5        # м — длина крыла крестовины (V); СТИЛЬ, а не факт (спека §2)
const FROG_PX := 3.0          # экранные px — толщина крыла (чуть толще нитки)
const FROG_LABEL_FONT := 1.0  # мир-пространственные единицы — высота глифа подписи марки (зумируется)
const SPAN_JOIN_EPS := 0.05   # м — допуск смежности спанов платформы

const RAIL_PX := 2.0          # экранные px — толщина нитки (не метры!)
const SIMPLE_PX := 2.5        # экранные px — упрощённая линия

const BALLAST_COLOR := Color(0.58, 0.58, 0.58)
const RAIL_COLOR := Color(0.95, 0.95, 0.95)
const SIMPLE_COLOR := Color(0.80, 0.80, 0.80)
const SLEEPER_COLOR := Color(0.36, 0.36, 0.36)

## Порядок слоёв СНИЗУ ВВЕРХ: дети рисуются в порядке добавления, поэтому
## порядок имён здесь и есть порядок отрисовки. Новые слои (платформы,
## стрелки) добавляются в список и получают свой проход в rebuild().
const LAYER_ORDER: Array[String] = ["ballast", "sleepers", "platforms", "rails"]

var geometry: Dictionary = { "elements": [] }  # пустая станция до загрузки: зум до прихода геометрии не падает
var _zoom := 1.0
var _lod := -1
var _track_lines: Array = []  # [{ "line": Line2D, "px": float }] — ширины при зуме

## Индексы контракта (строит set_geometry): типы по id, покрытие элементов
## run'ами, индекс элементов, особенности по владельцу. Размеры пути берутся
## ОТСЮДА, не из констант (спека §3–5).
var _types := {}       # id типа -> { gauge, sleeper_pitch, sleeper_half, sleeper_width, ballast_half_w }
var _run_types := {}   # id элемента -> id типа run'а, покрывающего элемент
var _elements := {}    # id элемента -> элемент (позы и спаны)
var _features := {}    # owner -> особенность (крестовина)

@onready var _debug: Node = get_node_or_null("Debug")

func _ready() -> void:
	scale = Vector2(1.0, -1.0)  # ЕДИНСТВЕННЫЙ переворот станции (Y вверх -> Y вниз)

func set_geometry(geo: Dictionary) -> void:
	geometry = geo
	_index_contract()
	_lod = -1
	rebuild()

## Строит индексы контракта (спека §3–5): типы, покрытие run'ами, элементы,
## особенности. Вызывается до rebuild — отрисовка берёт размеры только отсюда.
func _index_contract() -> void:
	_types.clear()
	for t in geometry.get("track_types", []):
		_types[t.id] = {
			"gauge": float(t.gauge),
			"sleeper_pitch": float(t.sleeper.pitch),
			"sleeper_half": float(t.sleeper.length) * 0.5,
			"sleeper_width": float(t.sleeper.width),
			"ballast_half_w": float(t.ballast.half_width),
		}
	_run_types.clear()
	for run in geometry.get("construction_runs", []):
		for span in run.spans:
			_run_types[span.element] = run.type
	_elements.clear()
	for el in geometry.elements:
		_elements[el.id] = el
	_features.clear()
	for f in geometry.get("features", []):
		_features[f.owner] = f

## Тип путевой конструкции элемента: run, покрывающий элемент, — его type
## (спека §4). Пустой словарь — тип не найден, рисование пропускается с
## ошибкой; своих инженерных констант у клиента больше нет.
func type_for_element(el: Dictionary) -> Dictionary:
	var tid: String = _run_types.get(el.id, "")
	if tid != "":
		return _types.get(tid, {})
	# Ветвь стрелки run'ом не покрыта (проходы устройств — не регулярная
	# решётка, спека §4), а тип самого устройства провод не несёт (спека §5).
	# ВРЕМЕННО: для стрелочной эвристики берётся единственный тип карты —
	# многотипные устройства этой wire-формой неразрешимы (снимет тип
	# стрелочной конструкции, спека §5 вариант 2).
	return _single_type()

func _single_type() -> Dictionary:
	if _types.size() == 1:
		return _types.values()[0]
	if _types.is_empty():
		return {}
	printerr("ТИПОВ %d, а тип стрелочной ветви в проводе не передаётся — стрелка не рисуется" % _types.size())
	return {}

## Особенность устройства по владельцу (крестовина, спека §5). Пусто — карта
## без особенности, рисование особенности пропускается (это нормально).
func feature_for(owner: String) -> Dictionary:
	return _features.get(owner, {})

## zoom камеры (px/м). Толщина линий — в экранных px: делим на zoom.
## При смене уровня LOD сцена пересобирается.
func set_zoom(z: float) -> void:
	_zoom = z
	var lod := _lod_for(z)
	if lod != _lod:
		rebuild()
		return
	for entry in _track_lines:
		entry.line.width = entry.px / z

func _lod_for(z: float) -> int:
	if z >= LOD_FULL_ZOOM:
		return Lod.FULL
	if z >= LOD_MID_ZOOM:
		return Lod.MID
	return Lod.SIMPLE

func rebuild() -> void:
	for child in get_children():
		if child != _debug:
			child.queue_free()
	_track_lines.clear()
	_lod = _lod_for(_zoom)
	if _lod == Lod.SIMPLE:
		# Упрощённый уровень — одна линия на путь, слои не нужны. Стрелки и
		# платформы здесь не детализируются: у стрелки это две линии от носка
		# (ветвь за ветвью), что и есть «одна линия на путь».
		for el in geometry.elements:
			_add_line(self, GM.sample_chain(el.start, el.primitives), SIMPLE_COLOR, SIMPLE_PX)
		return
	# Рисуем СЛОЯМИ, а не поэлементно: сперва балласт всех элементов, затем
	# шпалы всех, затем нитки всех. Поэлементная отрисовка красила балласт
	# стрелок (их id ST_A_SW_* идут после путей ST_A_E_*) поверх ниток и шпал
	# уже нарисованных путей.
	#
	# Стрелка (пара ветвей с role) — ОДИН объект: общий балласт, одна шпальная
	# решётка, рамные рельсы, остряки и крестовина (TurnoutDrawer). Её ветви
	# исключены из поэлементных проходов, иначе поверх общего слоя остались бы
	# старые два балласта/решётки/нитки.
	var layers := _make_layers()
	var turnouts := _collect_turnouts()
	for el in geometry.elements:
		if _is_turnout_branch(el, turnouts):
			continue
		_draw_ballast(layers.ballast, el)
	for t in turnouts.values():
		TurnoutDrawer.draw_ballast(self, layers.ballast, t)
	for obj in geometry.get("trackside", []):
		TracksideDrawer.draw(self, layers.platforms, obj, _elements)
	if _lod == Lod.FULL:
		# Решётка — по рецепту run'ов (спека §4), а не поэлементно: локальный u
		# начинается заново на каждом элементе, и на стыке вышла бы сдвоенная
		# шпала. Ветви стрелок run'ами не покрыты — их рисует TurnoutDrawer.
		for run in geometry.get("construction_runs", []):
			_draw_run_sleepers(layers.sleepers, run)
		for t in turnouts.values():
			TurnoutDrawer.draw_sleepers(self, layers.sleepers, t)
	for el in geometry.elements:
		if _is_turnout_branch(el, turnouts):
			continue
		_draw_rails(layers.rails, el)
	for t in turnouts.values():
		TurnoutDrawer.draw_rails(self, layers.rails, t, feature_for(t.role.turnout))

## Создаёт пустые контейнеры слоёв под World в порядке LAYER_ORDER.
func _make_layers() -> Dictionary:
	var layers := {}
	for name in LAYER_ORDER:
		var layer := Node2D.new()
		layer.name = name
		add_child(layer)
		layers[name] = layer
	return layers

## Балласт элемента — один Polygon2D в слой балласта. Полуширина — из типа
## (ballast.half_width), своих констант нет.
func _draw_ballast(parent: Node2D, el: Dictionary) -> void:
	var typ := type_for_element(el)
	if typ.is_empty():
		printerr("BALLAST %s: тип не найден — не рисуется" % el.id)
		return
	var pts: PackedVector2Array = GM.sample_chain(el.start, el.primitives)
	var ballast := Polygon2D.new()
	ballast.polygon = GM.offset_polygon(pts, typ.ballast_half_w)
	ballast.color = BALLAST_COLOR
	parent.add_child(ballast)

## Две нитки элемента — Line2D в слой ниток (ширины живут в _track_lines).
## Смещение ±gauge/2 — из типа (спека §3: символические нитки на ±gauge/2).
func _draw_rails(parent: Node2D, el: Dictionary) -> void:
	var typ := type_for_element(el)
	if typ.is_empty():
		printerr("RAILS %s: тип не найден — не рисуется" % el.id)
		return
	var pts: PackedVector2Array = GM.sample_chain(el.start, el.primitives)
	for sign in [1.0, -1.0]:
		_add_line(parent, GM.offset_polyline(pts, typ.gauge * 0.5 * sign), RAIL_COLOR, RAIL_PX)

## Шпалы одного run — один SleeperLayer. Позиция — от АНАЛИТИЧЕСКОЙ pose(u)
## (спека §4), не от тесселированной полилинии; момент r — полуоткрытым
## правилом phase + n×pitch ∈ [0, run_length): конечная точка шпалу НЕ
## получает, на стыке спанов сдвоенной шпалы не возникает. Поперечная ось —
## по левой нормали позы.
func _draw_run_sleepers(parent: Node2D, run: Dictionary) -> void:
	var typ: Dictionary = _types.get(run.type, {})
	if typ.is_empty():
		printerr("RUN %s: тип %s не найден — шпалы не рисуются" % [run.id, run.type])
		return
	var pitch: float = typ.sleeper_pitch
	if not is_finite(pitch) or pitch <= 0.0:
		printerr("RUN %s: шаг шпал %v — шпалы не рисуются" % [run.id, pitch])
		return
	var phase := float(run.get("phase", 0.0))
	var length := GM.run_length(run)
	var offsets := GM.run_sleeper_offsets(phase, pitch, length)
	var segs := PackedVector2Array()
	var half: float = typ.sleeper_half
	for r in offsets:
		var local := GM.run_to_local(run, r)
		if not local.ok:
			printerr("RUN %s: %s" % [run.id, local.error])
			break
		var el: Dictionary = _elements.get(local.element, {})
		if el.is_empty():
			printerr("RUN %s: спан ссылается на неизвестный элемент %s" % [run.id, local.element])
			break
		var pose := GM.pose_at(el.start, el.primitives, local.u)
		if not pose.ok:
			printerr("RUN %s: %s" % [run.id, pose.error])
			break
		var nrm := Vector2(-sin(pose.heading), cos(pose.heading))
		var p := Vector2(pose.x, pose.y)
		segs.append(p - nrm * half)
		segs.append(p + nrm * half)
	var layer_node := SleeperLayer.new()
	layer_node.setup(segs, SLEEPER_COLOR, typ.sleeper_width)
	parent.add_child(layer_node)

func _add_line(parent: Node2D, points: PackedVector2Array, color: Color, px: float) -> void:
	var line := Line2D.new()
	line.points = points
	line.default_color = color
	line.width = px / _zoom
	line.joint_mode = Line2D.LINE_JOINT_ROUND
	line.begin_cap_mode = Line2D.LINE_CAP_ROUND
	line.end_cap_mode = Line2D.LINE_CAP_ROUND
	parent.add_child(line)
	_track_lines.append({ "line": line, "px": px })

## Группирует ветви стрелок по role.turnout. Полная пара (straight+diverging)
## рисуется как один объект; неполная — остаётся обычными путями, ошибка видна.
func _collect_turnouts() -> Dictionary:
	var turnouts := {}
	for el in geometry.elements:
		if not el.has("role"):
			continue
		var role: Dictionary = el.role
		var tid: String = role.turnout
		if not turnouts.has(tid):
			turnouts[tid] = {"straight": null, "diverging": null, "role": role}
		var bucket: Dictionary = turnouts[tid]
		if bucket[role.branch] != null:
			printerr("TURNOUT %s: дубликат ветви %s (%s)" % [tid, role.branch, el.id])
		bucket[role.branch] = el
	var complete := {}
	for tid in turnouts:
		var bucket: Dictionary = turnouts[tid]
		if bucket.straight != null and bucket.diverging != null:
			complete[tid] = bucket
		else:
			printerr("TURNOUT %s: нет пары ветвей — рисуется обычными путями" % tid)
	return complete

## Ветвь стрелки с ПОЛНОЙ парой не рисуется поэлементно (её рисует TurnoutDrawer).
func _is_turnout_branch(el: Dictionary, turnouts: Dictionary) -> bool:
	if not el.has("role"):
		return false
	return turnouts.has(el.role.turnout)

## Охват станции в координатах СЕРВЕРА. Камера живёт вне перевёрнутого
## поддерева, поэтому main переводит границы GM.server_rect_to_godot().
func get_server_bounds() -> Rect2:
	var minv := Vector2(INF, INF)
	var maxv := Vector2(-INF, -INF)
	for el in geometry.elements:
		for p in GM.sample_chain(el.start, el.primitives):
			minv = minv.min(p)
			maxv = maxv.max(p)
	if minv.x == INF:
		return Rect2()
	return Rect2(minv, maxv - minv)
