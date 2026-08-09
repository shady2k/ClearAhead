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
##   - крестовина: V из двух коротких крыльев в точке расхождения ниток,
##     угол крыльев — по марке frog из контракта (1/9 — тангенс: 1 поперёк
##     на 9 вдоль), а не угадан.
##
## Положение остряков — СОСТОЯНИЕ, а не геометрия: симуляции ещё нет,
## стрелка рисуется в справочном положении (остряк прижат к рамному рельсу,
## маршрут по прямой). Признак перевода здесь НЕ выдумывается — когда сервер
## начнёт сообщать положение, это будет отдельная задача.
##
## Всё рисуется в СУЩЕСТВУЮЩИЕ слои (ballast/sleepers/rails), а не отдельным
## узлом: поэлементная отрисовка красила балласт стрелок поверх ниток путей
## (ClearAhead-aj2). Константы размеров живут в world.gd рядом с остальными;
## сюда приходит сам World, чтобы читать их и пользоваться его _add_line
## (толщина линий — в экранных px и пересчитывается при зуме).

const GM := preload("res://scripts/geometry_math.gd")
const SleeperLayer := preload("res://scripts/sleeper_layer.gd")

## Марка крестовины "1/9" -> { ok: true, value: угол в рад } (тангенс угла).
## Неразбираемая марка — { ok: false, error }, а не молчаливый дефолт.
static func parse_frog_mark(mark: String) -> Dictionary:
	var parts := mark.strip_edges().split("/")
	if parts.size() != 2:
		return {"ok": false, "error": "марка крестовины «%s» не вида N/M" % mark}
	var num_s := parts[0].strip_edges()
	var den_s := parts[1].strip_edges()
	if not num_s.is_valid_int() or not den_s.is_valid_int():
		return {"ok": false, "error": "марка крестовины «%s» не вида N/M" % mark}
	var num := int(num_s)
	var den := int(den_s)
	if num <= 0 or den <= 0:
		return {"ok": false, "error": "марка крестовины «%s»: числа должны быть > 0" % mark}
	return {"ok": true, "value": atan(float(num) / float(den))}

## Общий балласт пары ветвей: конверт из внешних кромок (прямая — со стороны,
## противоположной отклонению, отклоняемая — со стороны отклонения).
static func draw_ballast(world, layer: Node2D, turnout: Dictionary) -> void:
	var side := _side_sign(turnout.role)
	var w: float = world.BALLAST_HALF_W
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
static func draw_sleepers(world, layer: Node2D, turnout: Dictionary) -> void:
	var side := _side_sign(turnout.role)
	var h: float = world.SLEEPER_HALF
	var straight_pts := GM.sample_chain(turnout.straight.start, turnout.straight.primitives)
	var diverging_pts := GM.sample_chain(turnout.diverging.start, turnout.diverging.primitives)
	var str_res := GM.resample_uniform(straight_pts, world.SLEEPER_STEP)
	var div_res := GM.resample_uniform(diverging_pts, world.SLEEPER_STEP)
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
	layer_node.setup(segs, world.SLEEPER_COLOR, world.SLEEPER_WIDTH)
	layer.add_child(layer_node)

## Рельсы панели: рамные рельсы прямой ветви, наружная нитка отклоняемой,
## остряк (внутренняя нитка отклоняемой) и крестовина.
static func draw_rails(world, layer: Node2D, turnout: Dictionary) -> void:
	var side := _side_sign(turnout.role)
	var g: float = world.GAUGE_HALF
	var straight_pts := GM.sample_chain(turnout.straight.start, turnout.straight.primitives)
	var diverging_pts := GM.sample_chain(turnout.diverging.start, turnout.diverging.primitives)
	# рамные рельсы — две нитки прямой ветви (как у обычного пути)
	for sign in [1.0, -1.0]:
		world._add_line(layer, GM.offset_polyline(straight_pts, g * sign), world.RAIL_COLOR, world.RAIL_PX)
	# остряк (внутренняя нитка) и наружная нитка отклоняемой ветви
	world._add_line(layer, GM.offset_polyline(diverging_pts, -side * g), world.RAIL_COLOR, world.RAIL_PX)
	world._add_line(layer, GM.offset_polyline(diverging_pts, side * g), world.RAIL_COLOR, world.RAIL_PX)
	_draw_frog(world, layer, turnout, straight_pts, diverging_pts, side)

## Крестовина: V из двух крыльев в точке расхождения ниток. Положение точки —
## из геометрии (отклоняемая ветвь отошла от прямой на полную колею), угол
## между крыльями — по марке frog из контракта. Крылья нарисованы поверх
## ниток чуть толще, чтобы узел читался.
static func _draw_frog(world, layer: Node2D, turnout: Dictionary, straight_pts: PackedVector2Array, diverging_pts: PackedVector2Array, side: float) -> void:
	var frog := _frog_point(straight_pts, diverging_pts, side, world.GAUGE_HALF * 2.0, world.GAUGE_HALF)
	if not frog.ok:
		return  # _frog_point уже напечатал причину
	var mark := parse_frog_mark(turnout.role.frog)
	if not mark.ok:
		printerr("TURNOUT %s: %s — крестовина не нарисована" % [turnout.role.turnout, mark.error])
		return
	var h0 := float(turnout.straight.start.plan.heading)
	var alpha: float = mark.value
	var p: Vector2 = frog.point
	var w_len: float = world.FROG_WING
	# первое крыло — по рамному рельсу прямой ветви, второе — под углом марки
	# к стороне отклонения (там же идёт остряк после точки расхождения)
	var wing1 := PackedVector2Array([p, p + Vector2(cos(h0), sin(h0)) * w_len])
	var wing2 := PackedVector2Array([p, p + Vector2(cos(h0 + side * alpha), sin(h0 + side * alpha)) * w_len])
	world._add_line(layer, wing1, world.RAIL_COLOR, world.FROG_PX)
	world._add_line(layer, wing2, world.RAIL_COLOR, world.FROG_PX)

## Точка расхождения ниток: первая дуга пути отклоняемой ветви, где её центр
## отошёл от прямой ветви на полную колею. Там внутренняя нитка отклоняемой
## ветви сходится с рамным рельсом прямой — это и есть крестовина.
## { ok, point } в координатах сервера (Y вверх).
static func _frog_point(straight_pts: PackedVector2Array, diverging_pts: PackedVector2Array, side: float, gauge: float, gauge_half: float) -> Dictionary:
	var toe := straight_pts[0]
	var dir := (straight_pts[straight_pts.size() - 1] - toe).normalized()
	# знак: cross((p-toe), dir) > 0 — справа от движения (как offset_polyline,
	# где положительное смещение — влево); отклонение идёт в сторону hand
	var target := -side * gauge
	var prev := diverging_pts[0]
	var prev_dev := (prev - toe).cross(dir)
	for i in range(1, diverging_pts.size()):
		var cur := diverging_pts[i]
		var dev := (cur - toe).cross(dir)
		if (dev - target) * (prev_dev - target) <= 0.0:
			var frac := 0.0
			if absf(dev - prev_dev) > 1e-9:
				frac = (target - prev_dev) / (dev - prev_dev)
			frac = clampf(frac, 0.0, 1.0)
			var p := prev.lerp(cur, frac)
			# точка ВСТРЕЧИ ниток: внутренняя нитка отклоняемой ветви
			# (offset -side*gauge_half от её центра) — там же рамный рельс
			var t := (cur - prev).normalized()
			var n := Vector2(-t.y, t.x)
			return {"ok": true, "point": p + n * (-side * gauge_half)}
		prev = cur
		prev_dev = dev
	printerr("TURNOUT: ветви не расходятся на полную колею — крестовина не рисуется")
	return {"ok": false, "point": Vector2()}

## +1 — отклонение влево от движения (hand=left), -1 — вправо.
static func _side_sign(role: Dictionary) -> float:
	return 1.0 if role.hand == "left" else -1.0
