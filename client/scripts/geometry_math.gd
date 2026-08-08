class_name GeometryMath
extends RefCounted
## Чистая геометрия в координатах СЕРВЕРА: Y вверх, heading против часовой
## стрелки от +X, положительный angle дуги — поворот ВЛЕВО.
##
## ЗДЕСЬ НЕТ ПЕРЕВОРОТА ОСИ. Станцию переворачивает единственный минус в
## world.gd (World.scale.y = -1). Второй минус у y в любом месте этого файла
## сделает станцию зеркальной — а этого не видно на глаз. Не добавлять.
##
## server_to_godot / server_rect_to_godot — тот же переворот, выраженный для
## узлов ВНЕ перевёрнутого поддерева World (камера, границы для фита).
## Сама станция рисуется в серверных координатах и переворачивается один раз,
## своим transform — эти функции минус не дублируют.

const ARC_SAMPLE_STEP := 0.5   # м — шаг тесселяции дуги (часть LOD)

## Точки центральной линии цепочки примитивов от абсолютной позы элемента.
## Прямая — две точки (нормали постоянны), дуга тесселируется шагом
## ARC_SAMPLE_STEP. Координаты сервера (Y вверх).
static func sample_chain(start: Dictionary, primitives: Array[Dictionary]) -> PackedVector2Array:
	var pts := PackedVector2Array()
	var x := float(start.plan.x)
	var y := float(start.plan.y)
	var h := float(start.plan.heading)
	pts.append(Vector2(x, y))
	for p in primitives:
		var len := float(p.length)
		if p.kind == "straight":
			x += cos(h) * len
			y += sin(h) * len
			pts.append(Vector2(x, y))
		else:
			# arc — парсер гарантирует только straight и arc; кривизна берётся
			# из angle/length (radius в контракте есть, но в форме пути не участвует)
			var angle := float(p.angle)
			var kappa := angle / len  # знаковая кривизна
			var steps := maxi(int(ceil(len / ARC_SAMPLE_STEP)), 8)
			for i in range(1, steps + 1):
				var s := len * float(i) / float(steps)
				if absf(kappa) < 1e-12:
					pts.append(Vector2(x + cos(h) * s, y + sin(h) * s))
				else:
					# интеграл касательной: x = x0 + (sin(h+ks)-sin h)/k, y аналогично
					pts.append(Vector2(
						x + (sin(h + kappa * s) - sin(h)) / kappa,
						y + (cos(h) - cos(h + kappa * s)) / kappa))
			if absf(kappa) < 1e-12:
				x += cos(h) * len
				y += sin(h) * len
			else:
				x += (sin(h + angle) - sin(h)) / kappa
				y += (cos(h) - cos(h + angle)) / kappa
			h += angle
	return pts

## Единичные нормали к полилинии, направленные ВЛЕВО от движения.
static func normals(points: PackedVector2Array) -> PackedVector2Array:
	var ns := PackedVector2Array()
	if points.size() <= 1:
		if points.size() == 1:
			ns.append(Vector2(0, 1))
		return ns
	for i in points.size():
		var a: Vector2
		var b: Vector2
		if i == 0:
			a = points[0]
			b = points[1]
		elif i == points.size() - 1:
			a = points[-2]
			b = points[-1]
		else:
			a = points[i - 1]
			b = points[i + 1]
		var t := (b - a).normalized()
		ns.append(Vector2(-t.y, t.x))
	return ns

## Полилиния, смещённая на offset влево от движения (отрицательный — вправо).
static func offset_polyline(points: PackedVector2Array, offset: float) -> PackedVector2Array:
	var ns := normals(points)
	var out := PackedVector2Array()
	out.resize(points.size())
	for i in points.size():
		out[i] = points[i] + ns[i] * offset
	return out

## Замкнутый полигон полосы: левая кромка + правая (в обратном порядке).
static func offset_polygon(points: PackedVector2Array, half_width: float) -> PackedVector2Array:
	var left := offset_polyline(points, half_width)
	var right := offset_polyline(points, -half_width)
	var poly := PackedVector2Array(left)
	for i in range(right.size() - 1, -1, -1):
		poly.append(right[i])
	return poly

## Пересэмплирование полилинии по дуге пути с шагом step (м).
static func resample_uniform(points: PackedVector2Array, step: float) -> PackedVector2Array:
	var out := PackedVector2Array()
	if points.is_empty():
		return out
	out.append(points[0])
	var total := 0.0
	var target := step
	for i in range(1, points.size()):
		var seg := points[i] - points[i - 1]
		var seg_len := seg.length()
		if seg_len <= 0.0:
			continue
		while total + seg_len >= target:
			var frac := (target - total) / seg_len
			out.append(points[i - 1] + seg * frac)
			target += step
		total += seg_len
	if (out[out.size() - 1] - points[points.size() - 1]).length() > 1e-9:
		out.append(points[points.size() - 1])
	return out

## Координаты сервера (Y вверх) -> координаты Godot (Y вниз).
## Для узлов вне перевёрнутого поддерева World (камера, фит). Не дублирует
## минус отрисовки: станцию переворачивает только World.scale.y = -1.
static func server_to_godot(p: Vector2) -> Vector2:
	return Vector2(p.x, -p.y)

## Серверный прямоугольник -> godot-прямоугольник с положительным размером.
static func server_rect_to_godot(r: Rect2) -> Rect2:
	var top_left := Vector2(r.position.x, -(r.position.y + r.size.y))
	return Rect2(top_left, Vector2(r.size.x, r.size.y))
