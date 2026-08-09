class_name TurnoutDrawer
extends RefCounted
## Отрисовка пары ветвей стрелки (элементы с role) как ОДНОГО объекта.
##
## Модель (упрощённая, сознательно): стрелка — пара ветвей из одной точки
## (носок). На панели [носок, пятка] рисуются:
##   - общий балласт: конверт из внешних кромок обеих ветвей, один полигон;
##   - одна шпальная решётка: длинные стрелочные брусья вдоль прямой ветви,
##     перекрывающие веер до внешней кромки отклоняемой ветви;
##   - рамные рельсы: две нитки прямой ветви на всю панель;
##   - остряк: внутренняя нитка отклоняемой ветви — у носка лежит на рамном
##     рельсе, у крестовины сходится с ним же (видимый ромб-линза);
##   - наружная нитка отклоняемой ветви — от носка до пятки;
##   - крестовина: V из двух крыльев в ТОЧКЕ из особенности контракта
##     (спека §5), угол крыльев — по касательным адресов; марка стрелки —
##     подписью рядом, если есть.
##
## Размеры (gauge, балласт, шпалы) — из типа пути, который world разрешает
## для ветви стрелки (world.type_for_element); констант тут нет.
##
## Положение остряков — СОСТОЯНИЕ, а не геометрия: симуляции ещё нет,
## стрелка рисуется в справочном положении (остряк прижат к рамному рельсу,
## маршрут по прямой). Признак перевода здесь НЕ выдумывается — когда сервер
## начнёт сообщать положение, это будет отдельная задача.
##
## Всё рисуется в СУЩЕСТВУЮЩИЕ слои (ballast/sleepers/rails), а не отдельным
## узлом: поэлементная отрисовка красила балласт стрелок поверх ниток путей
## (ClearAhead-aj2). К world приходит сам World, чтобы читать его _add_line
## (толщина линий — в экранных px и пересчитывается при зуме) и разрешать
## тип элемента.
const GM := preload("res://scripts/geometry_math.gd")
const SleeperLayer := preload("res://scripts/sleeper_layer.gd")

## Общий балласт пары ветвей: конверт из внешних кромок (прямая — со стороны,
## противоположной отклонению, отклоняемая — со стороны отклонения).
static func draw_ballast(world, layer: Node2D, turnout: Dictionary) -> void:
	var typ: Dictionary = world.type_for_element(turnout.straight)
	if typ.is_empty():
		printerr("TURNOUT %s: тип не найден — балласт не рисуется" % turnout.role.turnout)
		return
	var side := _side_sign(turnout.role)
	var w: float = typ.ballast_half_w
	var straight_pts := GM.sample_chain(turnout.straight.start, turnout.straight.primitives)
	var diverging_pts := GM.sample_chain(turnout.diverging.start, turnout.diverging.primitives)
	var poly := PackedVector2Array(GM.offset_polyline(straight_pts, -side * w))
	var div_edge := GM.offset_polyline(diverging_pts, side * w)
	for i in range(div_edge.size() - 1, -1, -1):
		poly.append(div_edge[i])
	var ballast := Polygon2D.new()
	ballast.polygon = poly
	ballast.color = world.BALLAST_COLOR
	layer.add_child(ballast)

## Одна шпальная решётка пары ветвей: брус на каждый шаг прямой ветви, от
## внешней кромки прямой до внешней кромки отклоняемой на той же дуге пути.
## Это эвристика устройства (уровень 3), не рецепт run'а — проходы стрелок
## run'ами не покрыты (спека §4).
static func draw_sleepers(world, layer: Node2D, turnout: Dictionary) -> void:
	var typ: Dictionary = world.type_for_element(turnout.straight)
	if typ.is_empty():
		printerr("TURNOUT %s: тип не найден — шпалы не рисуются" % turnout.role.turnout)
		return
	var side := _side_sign(turnout.role)
	var h: float = typ.sleeper_half
	var straight_pts := GM.sample_chain(turnout.straight.start, turnout.straight.primitives)
	var diverging_pts := GM.sample_chain(turnout.diverging.start, turnout.diverging.primitives)
	var str_res := GM.resample_uniform(straight_pts, typ.sleeper_pitch)
	var div_res := GM.resample_uniform(diverging_pts, typ.sleeper_pitch)
	var ns_str := GM.normals(str_res)
	var ns_div := GM.normals(div_res)
	var segs := PackedVector2Array()
	var n := mini(str_res.size(), div_res.size())
	for i in n:
		# у носка брус обычной длины, у пятки перекрывает обе ветви
		segs.append(str_res[i] + ns_str[i] * (-side * h))
		segs.append(div_res[i] + ns_div[i] * (side * h))
	# хвост прямой ветви (длиннее отклоняемой) — обычные шпалы
	for i in range(n, str_res.size()):
		segs.append(str_res[i] + ns_str[i] * h)
		segs.append(str_res[i] - ns_str[i] * h)
	var layer_node := SleeperLayer.new()
	layer_node.setup(segs, world.SLEEPER_COLOR, typ.sleeper_width)
	layer.add_child(layer_node)

## Рельсы панели: рамные рельсы прямой ветви, наружная нитка отклоняемой,
## остряк (внутренняя нитка отклоняемой) и крестовина. Смещение ниток ±gauge/2
## — из типа устройства (спека §3).
static func draw_rails(world, layer: Node2D, turnout: Dictionary, feature: Dictionary) -> void:
	var typ: Dictionary = world.type_for_element(turnout.straight)
	if typ.is_empty():
		printerr("TURNOUT %s: тип не найден — нитки не рисуются" % turnout.role.turnout)
		return
	var side := _side_sign(turnout.role)
	var g: float = typ.gauge * 0.5
	var straight_pts := GM.sample_chain(turnout.straight.start, turnout.straight.primitives)
	var diverging_pts := GM.sample_chain(turnout.diverging.start, turnout.diverging.primitives)
	# рамные рельсы — две нитки прямой ветви (как у обычного пути)
	for sign in [1.0, -1.0]:
		world._add_line(layer, GM.offset_polyline(straight_pts, g * sign), world.RAIL_COLOR, world.RAIL_PX)
	# остряк (внутренняя нитка) и наружная нитка отклоняемой ветви
	world._add_line(layer, GM.offset_polyline(diverging_pts, -side * g), world.RAIL_COLOR, world.RAIL_PX)
	world._add_line(layer, GM.offset_polyline(diverging_pts, side * g), world.RAIL_COLOR, world.RAIL_PX)
	_draw_frog(world, layer, turnout, feature)

## Крылья крестовины из особенности: по касательным адресов (спека §5),
## длина w_len — стиль клиента (FROG_WING). Возвращает массив из двух
## отрезков [p, p + tangent × w_len]: прямой проход, затем боковой.
static func frog_wings(feature: Dictionary, w_len: float) -> Array:
	var p := Vector2(feature.point.x, feature.point.y)
	var a0: Dictionary = feature.addresses[0]
	var a1: Dictionary = feature.addresses[1]
	return [
		PackedVector2Array([p, p + Vector2(a0.tangent.x, a0.tangent.y) * w_len]),
		PackedVector2Array([p, p + Vector2(a1.tangent.x, a1.tangent.y) * w_len]),
	]

## Крестовина из ОСОБЕННОСТИ контракта (спека §5): точка — feature.point
## (пересечение офсетных ниток, считает сервер), крылья — по касательным
## адресов (прямой проход, затем боковой; касательная по ходу возрастания u,
## то есть от носка к пятке). Карта без особенности — V не рисуется, это
## нормально. Марка стрелки — подпись рядом, если есть: стиль, не геометрия.
static func _draw_frog(world, layer: Node2D, turnout: Dictionary, feature: Dictionary) -> void:
	if feature.is_empty():
		return
	var wings := frog_wings(feature, world.FROG_WING)
	world._add_line(layer, wings[0], world.RAIL_COLOR, world.FROG_PX)
	world._add_line(layer, wings[1], world.RAIL_COLOR, world.FROG_PX)
	var frog: Variant = turnout.role.get("frog", "")
	if frog is String and not (frog as String).is_empty():
		var label := Label.new()
		label.text = frog
		label.add_theme_font_size_override("font_size", int(world.FROG_LABEL_FONT))
		label.modulate = Color(0.95, 0.95, 0.95, 0.9)
		label.position = Vector2(feature.point.x, feature.point.y) + Vector2(world.FROG_WING * 0.35, world.FROG_WING * 0.35)
		label.scale = Vector2(1.0, -1.0)  # под переворотом World текст зеркалится (как в debug.gd)
		layer.add_child(label)

## +1 — отклонение влево от движения (hand=left), -1 — вправо.
static func _side_sign(role: Dictionary) -> float:
	return 1.0 if role.hand == "left" else -1.0
