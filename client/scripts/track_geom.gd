## TrackGeom — тесселяция пути.
##
## Сервер отдаёт РЕЦЕПТ, а не полилинию: начальную позу элемента и список
## примитивов ({kind:"straight", length} и {kind:"arc", radius, angle, length}).
## Превратить рецепт в точки — работа клиента, и это прямо разрешено границей из
## ClearAhead-sjq: «путь — примитивы и рецепты, клиент — тесселяция по зуму».
##
## Считать геометрию МОЖНО. Выдумывать данные — НЕЛЬЗЯ. Поэтому здесь нет ни
## одного числа о мире: ни уклона, ни ширины, ни высоты. Только арифметика над
## тем, что прислано.
##
## Система координат: плановые (x, y) и высота z — так, как их называет сервер.
## Возвращаются Vector3(x_plan, y_plan, z_высота); перевод в оси Godot делает
## тот, кто строит меш, одной функцией и в одном месте.
class_name TrackGeom
extends RefCounted


## Точка оси с курсом. Курс нужен ленте: она откладывается по нормали к оси.
class AxisPoint:
	var x: float
	var y: float
	var z: float
	var heading: float

	func _init(px: float, py: float, pz: float, ph: float) -> void:
		x = px
		y = py
		z = pz
		heading = ph

	func pos() -> Vector3:
		return Vector3(x, y, z)


## Результат разбора одного элемента.
class Element:
	var id: String = ""
	var kind: String = ""
	var points: Array[AxisPoint] = []
	## Длина, посчитанная по примитивам. Совпадение с полем length сервера
	## проверяется отдельно и попадает в отчёт: расхождение значило бы, что
	## клиент понимает рецепт иначе, чем его писали.
	var length_m: float = 0.0
	var length_declared_m: float = 0.0
	var start_z: float = 0.0
	var slope: float = 0.0
	## Ширина отсыпки. НЕТ ЗНАЧЕНИЯ ПО УМОЛЧАНИЮ: если сервер не связал элемент
	## ни с одним типом пути, ширина остаётся отрицательной, и элемент рисуется
	## нитью в один пиксель — видимым признаком того, что размера не прислали.
	var ballast_half_width_m: float = -1.0
	var type_id: String = ""


## tessellate_element — рецепт в точки.
##
## max_seg_m — предельная длина хорды, max_ang_rad — предельный поворот на шаг.
## Оба числа задают ПОДРОБНОСТЬ КАРТИНКИ, а не мир: их вправе выбирать клиент.
static func tessellate_element(el: Dictionary, max_seg_m: float, max_ang_rad: float) -> Element:
	var out := Element.new()
	out.id = String(el.get("id", ""))
	out.kind = String(el.get("kind", ""))

	var start: Dictionary = el.get("start", {}) as Dictionary
	var plan: Dictionary = start.get("plan", {}) as Dictionary
	var x := float(plan.get("x", 0.0))
	var y := float(plan.get("y", 0.0))
	var heading := float(plan.get("heading", 0.0))
	out.start_z = float(start.get("z", 0.0))
	out.slope = float(start.get("slope", 0.0))

	var u := 0.0
	out.points.append(AxisPoint.new(x, y, out.start_z, heading))

	var prims: Array = el.get("primitives", []) as Array
	for p_raw in prims:
		var p: Dictionary = p_raw as Dictionary
		var kind := String(p.get("kind", ""))
		var seg_len := 0.0
		var curvature := 0.0

		if kind == "straight":
			seg_len = float(p.get("length", 0.0))
		elif kind == "arc":
			var radius := float(p.get("radius", 0.0))
			var angle := float(p.get("angle", 0.0))
			# Длина дуги выводится из радиуса и угла — это и есть рецепт.
			seg_len = absf(radius * angle)
			if seg_len > 0.0:
				curvature = angle / seg_len
		else:
			# Неизвестный примитив НЕ додумывается прямой: пропущенный кусок
			# честнее выдуманного. Он виден разрывом на экране и числом в отчёте.
			push_warning("TrackGeom: примитив неизвестного вида «%s» в элементе %s — пропущен" % [kind, out.id])
			continue

		if "length" in p:
			out.length_declared_m += float(p["length"])

		if seg_len <= 0.0:
			continue

		var steps := int(ceil(seg_len / max_seg_m))
		if absf(curvature) > 0.0:
			steps = maxi(steps, int(ceil(absf(curvature * seg_len) / max_ang_rad)))
		steps = maxi(steps, 1)

		var x0 := x
		var y0 := y
		var h0 := heading
		for i in range(1, steps + 1):
			var s := seg_len * float(i) / float(steps)
			var nh := h0 + curvature * s
			var nx: float
			var ny: float
			if absf(curvature) < 1e-12:
				nx = x0 + s * cos(h0)
				ny = y0 + s * sin(h0)
				nh = h0
			else:
				nx = x0 + (sin(nh) - sin(h0)) / curvature
				ny = y0 - (cos(nh) - cos(h0)) / curvature
			x = nx
			y = ny
			heading = nh
			out.points.append(AxisPoint.new(x, y, out.start_z + out.slope * (u + s), heading))

		u += seg_len

	out.length_m = u
	return out


## sample_axis — точки оси с равным шагом по длине.
##
## Нужны для выбора уровня чанка: правило сервера меряет расстояние от клетки до
## ОСИ. Шаг выборки сервер в манифесте не называет, поэтому он тут отдельным
## аргументом, а не спрятан: расхождение шага даёт лишь лишние 204 у границы
## полосы, и это надо видеть, а не гадать.
static func sample_axis(elements: Array[Element], step_m: float) -> PackedVector2Array:
	var out := PackedVector2Array()
	for el in elements:
		if el.points.is_empty():
			continue
		var acc := 0.0
		var prev: AxisPoint = el.points[0]
		out.append(Vector2(prev.x, prev.y))
		for k in range(1, el.points.size()):
			var cur: AxisPoint = el.points[k]
			acc += Vector2(cur.x - prev.x, cur.y - prev.y).length()
			if acc >= step_m:
				out.append(Vector2(cur.x, cur.y))
				acc = 0.0
			prev = cur
		var last: AxisPoint = el.points[el.points.size() - 1]
		out.append(Vector2(last.x, last.y))
	return out
