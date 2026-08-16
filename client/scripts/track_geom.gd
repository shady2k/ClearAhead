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
	## Читаемая метка элемента из провода: игроку показывают её, а не UUID.
	var name: String = ""
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
	## Уклон ДО первого звена профиля. При пустой цепочке он один описывает
	## элемент целиком — это прежнее поведение, и оно не сломано.
	var slope: float = 0.0
	## Цепочка вертикального профиля: [{length, s0, s1}], уклоны безразмерные.
	##
	## До 2026-08-12 её не было в проводе вовсе, и отметка считалась линейно —
	## start_z + slope·u. Контракт редакции 6 §5 назвал это тем, чем оно было:
	## не «нейтральностью в плане», а потерей на последнем шаге. Цепочка ЕСТЬ в
	## карте, компилятор её разбирает и терял при сериализации.
	var profile: Array[Dictionary] = []
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
		return AxisPoint.new(x, y, start_z + rise_at(uu), h, uu)

	## rise_at — подъём от start_z до точки u по цепочке профиля.
	##
	## Звено с постоянным уклоном даёт прямую, звено с меняющимся — ПАРАБОЛУ:
	## уклон меняется по u линейно, значит отметка — интеграл линейной функции.
	## Формула на звене длиной L с уклонами s0 и s1, при локальном t:
	##
	##     rise = s0·t + (s1 − s0)·t² / (2L)
	##
	## Пустая цепочка даёт прежний линейный расчёт. Это не подстановка вместо
	## данных: сервер шлёт цепочку всегда (плоский элемент приезжает одним звеном
	## нулевого уклона), и пустой она бывает только у ответа старого сервера.
	func rise_at(uu: float) -> float:
		if profile.is_empty():
			return slope * uu
		var rise := 0.0
		var rest := uu
		for seg in profile:
			if rest <= 0.0:
				break
			var seg_len := float(seg["length"])
			if seg_len <= 0.0:
				continue
			var t := minf(rest, seg_len)
			var s0 := float(seg["s0"])
			var s1 := float(seg["s1"])
			rise += s0 * t + (s1 - s0) * t * t / (2.0 * seg_len)
			rest -= t
		return rise

	## sample_range — точки оси на отрезке [u0, u1] с заданной подробностью.
	##
	## Подробность (максимальная хорда и максимальный поворот на шаг) — свойство
	## РИСУНКА. Границы отрезка входят в результат ровно теми позами, что даёт
	## pose_at: кусок рецепта не должен «начинаться приблизительно».
	## Насколько две точки оси считаются одной, метры.
	##
	## Миллиметр: мельче любой видимой подробности пути и на два порядка крупнее
	## шума накопления длин примитивов, из-за которого отбраковка и заведена.
	const SAME_POINT_EPS_M := 1e-3

	func sample_range(u0: float, u1: float, max_seg_m: float, max_ang_rad: float) -> Array[AxisPoint]:
		var out: Array[AxisPoint] = []
		if u1 <= u0:
			return out
		out.append(pose_at(u0))
		var last_u := u0
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
				var u := lo + seg * float(i) / float(steps)
				# ТОЧКИ БЛИЖЕ SAME_POINT_EPS_M СЧИТАЮТСЯ ОДНОЙ.
				#
				# Найдено замером 2026-08-16. Начало отрезка может лечь ВПЛОТНУЮ к
				# границе примитива: у эпюры Р65 1/9 корень остряка приходится ровно
				# на конец остряковой кривой — не по совпадению, а по устройству,
				# кривая на то и остряковая. Тогда pose_at(u0) и первая точка
				# следующего примитива расходились на длину накопления, сотую долю
				# миллиметра, и между ними вставал вырожденный четырёхугольник.
				#
				# В кадре это давало слипшиеся тела: проверка оболочки насчитала 53
				# ребра, входящих больше чем в две грани. С отбраковкой — ноль.
				if u - last_u < SAME_POINT_EPS_M:
					continue
				out.append(pose_at(u))
				last_u = u
		return out


## tessellate_element — рецепт в точки.
##
## max_seg_m — предельная длина хорды, max_ang_rad — предельный поворот на шаг.
## Оба числа задают ПОДРОБНОСТЬ КАРТИНКИ, а не мир: их вправе выбирать клиент.
static func tessellate_element(el: Dictionary, max_seg_m: float, max_ang_rad: float) -> Element:
	var out := Element.new()
	out.id = String(el.get("id", ""))
	out.name = String(el.get("name", ""))
	out.kind = String(el.get("kind", ""))

	var start: Dictionary = el.get("start", {}) as Dictionary
	var plan: Dictionary = start.get("plan", {}) as Dictionary
	out.start_x = float(plan.get("x", 0.0))
	out.start_y = float(plan.get("y", 0.0))
	out.start_heading = float(plan.get("heading", 0.0))
	out.start_z = float(start.get("z", 0.0))
	out.slope = float(start.get("slope", 0.0))
	# Цепочка профиля разворачивается в пару уклонов на звено — той же формой,
	# какой её держит компилятор сервера. Уклоны в проводе в ПРОМИЛЛЕ (как в
	# карте), здесь переводятся в безразмерные один раз и в одном месте.
	var running_slope := out.slope
	for v_raw in (el.get("profile", []) as Array):
		var v: Dictionary = v_raw as Dictionary
		var seg_len := float(v.get("length", 0.0))
		var kind := String(v.get("kind", ""))
		var s0 := running_slope
		var s1 := running_slope
		if kind == "grade":
			s0 = float(v.get("slope_permille", 0.0)) / 1000.0
			s1 = s0
		elif kind == "vertical_curve":
			s1 = float(v.get("end_slope_permille", 0.0)) / 1000.0
		else:
			# Неизвестное звено НЕ пропускается молча: пропуск сдвинул бы весь
			# оставшийся профиль по u, и отметка разошлась бы с серверной тем
			# сильнее, чем дальше от начала. Цепочка обрывается, и это видно.
			break
		out.profile.append({"length": seg_len, "s0": s0, "s1": s1})
		running_slope = s1
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
## ОСИ. Шаг остаётся отдельным аргументом, а не прячется внутрь: расхождение шага
## с серверным даёт у границы полосы лишние адреса, по которым приезжает «чанка
## нет», и это надо видеть, а не гадать. С 2026-08-12 угадывать его и не
## приходится — манифест называет axis_step_m, и зовущий берёт число оттуда.
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
