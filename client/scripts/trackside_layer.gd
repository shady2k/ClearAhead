class_name TracksideDrawer
extends RefCounted
## Отрисовка путевых объектов (trackside) из контракта RenderGeometry.
##
## Платформа — полоса вдоль элемента на интервале [from, to] в метрах
## координаты u и со стороны side ("left"/"right" от движения). Спаны —
## список: объект может пересекать границу элементов. Непрерывные спаны
## склеиваются в ОДИН полигон (одна платформа — один узел сцены), а
## разнесённые остаются отдельными полигонами в том же узле: ложной
## диагонали между разнесёнными кусками нет.
##
## Неизвестный вид объекта и неизвестная сторона — видимая ошибка (printerr),
## а не молчаливый пропуск: парсер уже валит разбор на неизвестном виде, но
## отрисовка не должна делать вид, что всё хорошо.
##
## Константы размеров (в метрах) живут в world.gd рядом с остальными; сюда
## приходит сам World, чтобы читать их.

const GM := preload("res://scripts/geometry_math.gd")

## Точки центральной линии цепочки МЕЖДУ дугами from и to (м, координата u),
## включая обе границы. Пустой результат — вырожденный интервал (to <= from,
## from >= длины цепочки).
##
## Живёт здесь, а не в geometry_math.gd: путевые объекты — единственный
## потребитель, а границы диффа воркера — world.gd и новые файлы.
static func sample_range(start: Dictionary, primitives: Array[Dictionary], from: float, to: float) -> PackedVector2Array:
	var out := PackedVector2Array()
	if to <= from or to <= 0.0:
		return out
	var x := float(start.plan.x)
	var y := float(start.plan.y)
	var h := float(start.plan.heading)
	var total := 0.0
	for p in primitives:
		var len := float(p.length)
		var seg_from := total
		total += len
		var seg_to := total
		var is_arc: bool = p.kind != "straight"
		if seg_to <= from:
			# примитив целиком до интервала — перемотать позу
			var end := _prim_end(x, y, h, p)
			x = end.x
			y = end.y
			h = end.h
			continue
		if seg_from >= to:
			break
		var a := maxf(from, seg_from)
		var b := minf(to, seg_to)
		var start_idx := out.size()
		if is_arc:
			var angle := float(p.angle)
			var kappa := angle / len
			var steps := maxi(int(ceil((b - a) / GM.ARC_SAMPLE_STEP)), 8)
			for i in range(0, steps + 1):
				out.append(_arc_point(x, y, h, kappa, a + (b - a) * float(i) / float(steps) - seg_from))
		else:
			var dir := Vector2(cos(h), sin(h))
			out.append(Vector2(x, y) + dir * (a - seg_from))
			out.append(Vector2(x, y) + dir * (b - seg_from))
		# поза в конце примитива; дубль точки на стыке примитивов — вон
		var end := _prim_end(x, y, h, p)
		x = end.x
		y = end.y
		h = end.h
		if start_idx > 0 and out.size() > start_idx and out[start_idx].distance_to(out[start_idx - 1]) < 1e-9:
			out.remove_at(start_idx)
	return out

## Рисует путевой объект в слой parent. elements — индекс id -> элемент.
static func draw(world, parent: Node2D, obj: Dictionary, elements: Dictionary) -> void:
	if obj.kind != "platform":
		printerr("TRACKSIDE %s: вид «%s» не рисуется (умею только platform)" % [obj.id, obj.kind])
		return
	var side: String = obj.get("side", "")
	var side_sign := 1.0
	if side == "left":
		side_sign = 1.0
	elif side == "right":
		side_sign = -1.0
	else:
		printerr("TRACKSIDE %s: неизвестная сторона «%s» — платформа не рисуется" % [obj.id, side])
		return
	var runs := _runs(world, obj, elements)
	if runs.is_empty():
		return  # _runs уже напечатал причины
	var container := Node2D.new()
	container.name = obj.id
	for run in runs:
		if run.size() < 2:
			continue
		# полоса от ближней кромки (offset) до дальней (offset + width):
		# левая кромка + правая в обратном порядке
		var poly := PackedVector2Array(GM.offset_polyline(run, side_sign * world.PLATFORM_OFFSET))
		var edge := GM.offset_polyline(run, side_sign * (world.PLATFORM_OFFSET + world.PLATFORM_WIDTH))
		for i in range(edge.size() - 1, -1, -1):
			poly.append(edge[i])
		var plat := Polygon2D.new()
		plat.polygon = poly
		plat.color = world.PLATFORM_COLOR
		container.add_child(plat)
	parent.add_child(container)

static func _runs(world, obj: Dictionary, elements: Dictionary) -> Array:
	var runs: Array = []
	var current := PackedVector2Array()
	for span in obj.spans:
		var el: Dictionary = elements.get(span.element, {})
		if el.is_empty():
			printerr("TRACKSIDE %s: спан ссылается на неизвестный элемент %s" % [obj.id, span.element])
			continue
		var chain := sample_range(el.start, el.primitives, float(span.from), float(span.to))
		if chain.is_empty():
			printerr("TRACKSIDE %s: вырожденный спан %s [%v..%v]" % [obj.id, span.element, span.from, span.to])
			continue
		if current.is_empty():
			current = chain
		elif current[current.size() - 1].distance_to(chain[0]) <= world.SPAN_JOIN_EPS:
			# непрерывное продолжение — общий полигон (первая точка спана
			# совпадает с последней предыдущего, дубль не нужен)
			for i in range(1, chain.size()):
				current.append(chain[i])
		else:
			runs.append(current)
			current = chain
	if not current.is_empty():
		runs.append(current)
	return runs

static func _arc_point(x0: float, y0: float, h0: float, kappa: float, s: float) -> Vector2:
	if absf(kappa) < 1e-12:
		return Vector2(x0 + cos(h0) * s, y0 + sin(h0) * s)
	# интеграл касательной: x = x0 + (sin(h+ks)-sin h)/k, y аналогично
	return Vector2(
		x0 + (sin(h0 + kappa * s) - sin(h0)) / kappa,
		y0 + (cos(h0) - cos(h0 + kappa * s)) / kappa)

static func _prim_end(x0: float, y0: float, h0: float, p: Dictionary) -> Dictionary:
	var len := float(p.length)
	if p.kind == "straight":
		return {"x": x0 + cos(h0) * len, "y": y0 + sin(h0) * len, "h": h0}
	var angle := float(p.angle)
	var kappa := angle / len
	if absf(kappa) < 1e-12:
		return {"x": x0 + cos(h0) * len, "y": y0 + sin(h0) * len, "h": h0 + angle}
	return {
		"x": x0 + (sin(h0 + angle) - sin(h0)) / kappa,
		"y": y0 + (cos(h0) - cos(h0 + angle)) / kappa,
		"h": h0 + angle,
	}
