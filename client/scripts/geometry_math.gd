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

## Поза на осевой линии цепочки в координате u (м от начала элемента).
## Аналитическая, НЕ тесселированная: тесселяция — клиентская деталь и на
## позиции влиять не должна (спека §4 «Позиция считается от аналитической
## pose(u), не от тесселированной полилинии»). Возврат:
## { ok, x, y, heading } — координаты сервера (Y вверх), heading — угол
## касательной по ходу возрастания u.
static func pose_at(start: Dictionary, primitives: Array[Dictionary], u: float) -> Dictionary:
	var x := float(start.plan.x)
	var y := float(start.plan.y)
	var h := float(start.plan.heading)
	if not is_finite(u) or u < 0.0:
		return {"ok": false, "x": x, "y": y, "heading": h,
			"error": "pose_at: u вне домена: %v" % u}
	var remaining := u
	for p in primitives:
		var len := float(p.length)
		if remaining <= len:
			if p.kind == "straight":
				return {"ok": true, "x": x + cos(h) * remaining,
					"y": y + sin(h) * remaining, "heading": h}
			var kappa := float(p.angle) / len
			if absf(kappa) < 1e-12:
				return {"ok": true, "x": x + cos(h) * remaining,
					"y": y + sin(h) * remaining, "heading": h}
			# интеграл касательной: x = x0 + (sin(h+ks)-sin h)/k, y аналогично
			return {
				"ok": true,
				"x": x + (sin(h + kappa * remaining) - sin(h)) / kappa,
				"y": y + (cos(h) - cos(h + kappa * remaining)) / kappa,
				"heading": h + kappa * remaining,
			}
		# примитив целиком до u — перемотать позу к его концу
		if p.kind == "straight":
			x += cos(h) * len
			y += sin(h) * len
		else:
			var angle := float(p.angle)
			var kappa := angle / len
			if absf(kappa) < 1e-12:
				x += cos(h) * len
				y += sin(h) * len
			else:
				x += (sin(h + angle) - sin(h)) / kappa
				y += (cos(h) - cos(h + angle)) / kappa
			h += angle
		remaining -= len
	# u за концом цепочки: последняя поза (домен гарантируют спаны run'а)
	return {"ok": true, "x": x, "y": y, "heading": h}

## Длина run: сумма длин спанов (спека §4).
static func run_length(run: Dictionary) -> float:
	var total := 0.0
	for span in run.spans:
		total += float(span.to) - float(span.from)
	return total

## Отображение накопленной координаты run r в локальный u спана (спека §4):
## forward — u = from + (r − r₀), reverse — u = to − (r − r₀). Граница между
## спанами принадлежит следующему спану; r == run_length — за пределами
## (полуоткрытое правило [0, run_length)).
## { ok: true, element, u } | { ok: false, error }.
static func run_to_local(run: Dictionary, r: float) -> Dictionary:
	if not is_finite(r) or r < 0.0:
		return {"ok": false, "error": "координата run вне домена: %v" % r}
	var r0 := 0.0
	for span in run.spans:
		var span_len := float(span.to) - float(span.from)
		if r >= r0 + span_len:
			r0 += span_len
			continue
		if span.direction == "reverse":
			return {"ok": true, "element": span.element,
				"u": float(span.to) - (r - r0)}
		return {"ok": true, "element": span.element,
			"u": float(span.from) + (r - r0)}
	return {"ok": false, "error": "координата run %v за пределами [0, %v)" % [r, r0]}

## Моменты размещения шпал run по рецепту (спека §4): полуоткрытое правило
## phase + n×pitch ∈ [0, run_length). Шпала в конечной точке НЕ ставится —
## на стыке спанов/элементов сдвоенной шпалы не возникает. Неположительный
## шаг или нечисловые параметры — пусто, а не зацикливание.
static func run_sleeper_offsets(phase: float, pitch: float, length: float) -> PackedFloat64Array:
	if not is_finite(phase) or not is_finite(pitch) or not is_finite(length):
		return PackedFloat64Array()
	if pitch <= 0.0 or length <= 0.0:
		return PackedFloat64Array()
	var out := PackedFloat64Array()
	var n := 0
	while true:
		var r := phase + float(n) * pitch
		if r >= length:
			break
		out.append(r)
		n += 1
	return out

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
	if not is_finite(step) or step <= 0.0:
		printerr("resample_uniform: шаг %v неположителен — выборка не строится" % step)
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
