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
## Наружу отдаются позы в этих же координатах; перевод в оси Godot делает тот,
## кто строит меш, одной функцией и в одном месте.
class_name TrackGeom
extends RefCounted


## Точка оси с курсом. Курс нужен ленте: она откладывается по нормали к оси.
##
## Поле u — расстояние по оси от начала элемента. Оно не украшение: рецепты
## сервера (construction_runs, spans платформы, адреса особенностей) адресуют
## место ИМЕННО через u, и точка без u не сопоставима с ними.
class AxisPoint:
	var x: float
	var y: float
	var z: float
	var heading: float
	var u: float

	func _init(px: float, py: float, pz: float, ph: float, pu: float = 0.0) -> void:
		x = px
		y = py
		z = pz
		heading = ph
		u = pu

	## Левая нормаль в плане — поворот курса на +90°.
	##
	## «Левая» здесь не вкус: спека размещения (render-contract §4) задаёт
	## ориентацию шпалы через ЛЕВУЮ нормаль аналитической позы, и сторона
	## платформы (`side`) считается от неё же. Знак задан в одном месте, чтобы
	## расхождение с сервером искали здесь, а не в трёх рисующих функциях.
	func left() -> Vector2:
		return Vector2(-sin(heading), cos(heading))

	## Единичный вектор вдоль оси по возрастанию u.
	func forward() -> Vector2:
		return Vector2(cos(heading), sin(heading))


## Примитив рецепта, приведённый к одной форме: длина и кривизна.
##
## Прямая — это дуга нулевой кривизны, и разводить их двумя ветками в каждой
## функции значило бы трижды повторить один и тот же match. Кривизна со знаком:
## положительная — влево, как и угол дуги у сервера.
class Prim:
	var length: float
	var curvature: float

	func _init(plen: float, pcurv: float) -> void:
		length = plen
		curvature = pcurv


## Результат разбора одного элемента.
class Element:
	var id: String = ""
	var kind: String = ""
	var points: Array[AxisPoint] = []
	## Примитивы рецепта. Хранятся, а не выбрасываются после тесселяции: поза в
	## произвольной точке u обязана считаться АНАЛИТИЧЕСКИ, а не по ломаной.
	## Спека размещения (render-contract §4) требует этого дословно: иначе
	## клиент с шагом 0.5 м и клиент, считающий формулой, разойдутся при
	## одинаковых phase и pitch, то есть поставят шпалы в разные места.
	var prims: Array[Prim] = []
	var start_x: float = 0.0
	var start_y: float = 0.0
	var start_heading: float = 0.0
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
	## role{turnout, branch, hand, frog} — если сервер его прислал. Пустой
	## словарь значит «элемент не часть устройства», а не «устройство обычное».
	var role: Dictionary = {}

	## pose_at — поза на оси в точке u. Аналитически, без обращения к points.
	##
	## За концом элемента поза не выдумывается: u прижимается к [0, length_m].
	## Экстраполяция прямой за концом дуги была бы новой геометрией, которой
	## сервер не присылал.
	func pose_at(pu: float) -> AxisPoint:
		var uu := clampf(pu, 0.0, length_m)
		var x := start_x
		var y := start_y
		var h := start_heading
		var rest := uu
		for p in prims:
			if rest <= 0.0:
				break
			var s := minf(rest, p.length)
			if absf(p.curvature) < 1e-12:
				x += s * cos(h)
				y += s * sin(h)
			else:
				var nh := h + p.curvature * s
				x += (sin(nh) - sin(h)) / p.curvature
				y -= (cos(nh) - cos(h)) / p.curvature
				h = nh
			rest -= s
		return AxisPoint.new(x, y, start_z + slope * uu, h, uu)

	## sample_range — точки оси на отрезке [u0, u1] с заданной подробностью.
	##
	## Подробность (максимальная хорда и максимальный поворот на шаг) — свойство
	## РИСУНКА. Границы отрезка входят в результат ровно теми позами, что даёт
	## pose_at: кусок рецепта не должен «начинаться приблизительно».
	func sample_range(u0: float, u1: float, max_seg_m: float, max_ang_rad: float) -> Array[AxisPoint]:
		var out: Array[AxisPoint] = []
		if u1 <= u0:
			return out
		out.append(pose_at(u0))
		var acc := 0.0
		for p in prims:
			var a := acc
			var b := acc + p.length
			acc = b
			var lo := maxf(a, u0)
			var hi := minf(b, u1)
			if hi <= lo:
				continue
			var seg := hi - lo
			var steps := int(ceil(seg / max_seg_m))
			if absf(p.curvature) > 0.0:
				steps = maxi(steps, int(ceil(absf(p.curvature * seg) / max_ang_rad)))
			steps = maxi(steps, 1)
			for i in range(1, steps + 1):
				out.append(pose_at(lo + seg * float(i) / float(steps)))
		return out


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
	out.start_x = float(plan.get("x", 0.0))
	out.start_y = float(plan.get("y", 0.0))
	out.start_heading = float(plan.get("heading", 0.0))
	out.start_z = float(start.get("z", 0.0))
	out.slope = float(start.get("slope", 0.0))
	out.role = (el.get("role", {}) as Dictionary) if el.has("role") else {}

	for p_raw in (el.get("primitives", []) as Array):
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

		out.prims.append(Prim.new(seg_len, curvature))
		out.length_m += seg_len

	out.points = out.sample_range(0.0, out.length_m, max_seg_m, max_ang_rad)
	if out.points.is_empty():
		# Элемент нулевой длины: одна поза лучше пустоты — она хотя бы называет
		# место, где сервер объявил элемент.
		out.points.append(out.pose_at(0.0))
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
