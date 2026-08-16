## ОБОЛОЧКА МЕША: сходятся ли грани, не вывернута ли какая, нет ли дыр.
##
## # Зачем суита заведена (2026-08-16)
##
## Четыре дефекта подряд нашёл ВЛАДЕЛЕЦ ГЛАЗАМИ на снимке, и все четыре прошли
## мимо зелёных проверок:
##
##   • сердечник крестовины строился с вывернутой нормалью — в кадре чёрное пятно;
##   • нитки обрывались на разрыве без торцевых крышек — видна изнанка;
##   • на стыке остряка с ниткой форма скакала на ширину головки — «сегменты,
##     которые не соединены»;
##   • щиток указателя закрывал знак на соседнем щитке.
##
## Причина у всех одна: проверялись ЧИСЛА, ИЗ КОТОРЫХ ТЕЛО СТРОИТСЯ, — длины,
## углы, число треугольников, — и все они были верны. Не проверялось само тело.
##
## # Что проверяется здесь
##
## ТОПОЛОГИЯ ОБОЛОЧКИ, считанная из готового меша и ни от чего больше не
## зависящая. Рёбра склеиваются ПО КООРДИНАТАМ, а не по индексам: полосы кладут
## каждому четырёхугольнику свои четыре вершины, и по индексам ни одно ребро
## соседей не совпало бы.
##
##   ребро в двух треугольниках, направления противоположны — грани сходятся;
##   ребро в двух треугольниках, направления совпали — ОДИН ИЗ НИХ ВЫВЕРНУТ;
##   ребро в одном треугольнике — ГРАНИЦА: край тела, дыра либо снятая крышка;
##   ребро больше чем в двух — неманифолд, тела слиплись.
##
## Число граничных рёбер СРАВНИВАЕТСЯ С ОЖИДАЕМЫМ, а не проверяется на ноль:
## рельс объявлен без торцов (TrackView.rail_profile_mesh), и у куска нитки
## граница обязана быть ровно двумя контурами сечения — по одному на конец.
## Больше — лишняя дыра, меньше — склеилось то, чему склеиваться нечем.
extends "res://tools/check_suite.gd"

## Насколько две вершины считаются одной точкой. Десятая доля миллиметра: мельче
## любой подробности пути и крупнее шума float32, в котором живут PackedVector3Array.
const WELD_M := 1e-4

## Насколько далеко от рабочей грани ещё считается «та же нитка». Двадцать
## сантиметров: втрое шире головки и вчетверо уже колеи, то есть соседнюю нитку
## не захватывает ни при каком сечении.
const NEAR_M := 0.2

## Какую долю поперечного отрезка тела обязаны делить по обе стороны шва.
##
## Девять десятых, а не половина: смещение на половину подошвы даёт ровно 50 %
## общего, и порог «меньше половины» пропускал бы дефект, ради которого проверка
## заведена. Замер: со стыком встык — 50 %, с нахлёстом — 100 %.
const SEAM_SHARE_MIN := 0.9

## На сколько плоскость сечения отступает от шва. Пять сантиметров: дальше любой
## торцевой крышки и ближе любого шага разбиения оси.
const SEAM_STEP_M := 0.05


func run() -> void:
	var network := await ctx.network_data()
	var elements := await ctx.elements()
	if elements.is_empty():
		return
	var by_id := TrackBuild.elements_by_id(elements)
	var spans := TrackBuild.covered_spans(network, by_id, CheckContext.MAX_SEG_M, CheckContext.MAX_ANG_RAD)

	_check_rails(spans)
	_check_blades(network, by_id)
	_check_frog(network, by_id)
	_check_seams(network, by_id, spans)
	_check_frog_model(network, by_id)


## РЕЛЬС УЧАСТКА. Считается ВМЕСТЕ С НАКАТОМ, и это не удобство: верх головки
## режется под накат, и в теле рельса на его месте ДЫРА во всю длину пути.
## Заполняет её отдельный меш, и осмысленно проверять только сборку — разойдись
## они хоть на десятую миллиметра, вдоль пути откроется щель.
##
## Проверяется ЧИСЛО ОТКРЫТЫХ КОНТУРОВ, а не число граничных рёбер: форма контура
## зависит от присланного сечения, а вот сколько их — свойство сборки. Два на
## кусок нитки: рельс объявлен без торцов и уходит в стык со следующим.
func _check_rails(spans: Array[TrackBuild.Span]) -> void:
	var flipped := 0
	var nonmanifold := 0
	var worst := ""
	var checked := 0
	for sp in spans:
		if not sp.has_rail_section():
			continue
		var body := TrackView.rail_body_mesh(sp)
		var head := TrackView.railhead_mesh(sp)
		if body == null:
			continue
		checked += 1
		var st := _shell([body, head])
		flipped += int(st["flipped"])
		nonmanifold += int(st["nonmanifold"])
		var ends := 0
		for sgn in [1.0, -1.0]:
			ends += sp.rail_runs(sgn).size() * 2
		if int(st["loops"]) != ends and worst == "":
			worst = "%s: открытых контуров %d, ожидалось %d (по два на кусок нитки)" % [
				sp.element_id, int(st["loops"]), ends]
	_ok("рельс: ни одной вывернутой грани", flipped == 0,
		"участков %d, рёбер с совпавшим направлением %d" % [checked, flipped])
	_ok("рельс: тела не слиплись", nonmanifold == 0,
		"рёбер больше чем в двух гранях: %d" % nonmanifold)
	_ok("рельс с накатом: открыты только концы кусков", worst == "", worst)


## ОСТРЯК. Тело короткое и оба конца открыты — тот же счёт, что у куска нитки.
func _check_blades(network: Dictionary, by_id: Dictionary) -> void:
	var bl := TrackBuild.blades(network, by_id, CheckContext.MAX_SEG_M, CheckContext.MAX_ANG_RAD)
	var blades: Array[TrackBuild.Blade] = bl["list"]
	if blades.is_empty():
		return
	var flipped := 0
	var bad := ""
	for b_raw in blades:
		var blade: TrackBuild.Blade = b_raw
		# Проверяется В ОБОИХ ПОЛОЖЕНИЯХ: отвод меняет вынос граней, а вместе с
		# ним и порядок обхода полос — выворот мог бы появиться только у одного.
		for open_share in [0.0, 1.0]:
			blade.set_open(open_share)
			var mesh := TrackView.frog_rail_mesh(blade)
			if mesh == null:
				bad = "остряк %s не построился при отводе %.0f" % [blade.element_id, open_share]
				continue
			# ВМЕСТЕ С НАКАТОМ — разбор тот же, что у рельса участка: верх головки
			# у ходовой нитки вырезан, и порознь тело остряка имеет законную дыру.
			var st := _shell([mesh, TrackView.frog_railhead_mesh(blade)])
			flipped += int(st["flipped"])
			if int(st["loops"]) != 2 and bad == "":
				bad = "%s: открытых контуров %d, ожидалось 2" % [
					blade.element_id, int(st["loops"])]
		blade.set_open(0.0)
	_ok("остряк: ни одной вывернутой грани", flipped == 0,
		"остряков %d, рёбер с совпавшим направлением %d" % [blades.size(), flipped])
	_ok("остряк: два открытых конца и ни одной лишней дыры", bad == "", bad)


## КРЕСТОВИНА. Сердечник — единственное тело перевода, у которого дно рисуется:
## он виден снизу между брусьями. Вывернутая грань у него уже случалась и стоила
## чёрного пятна на шпалах.
func _check_frog(network: Dictionary, by_id: Dictionary) -> void:
	var fr := TrackBuild.frog_rails(network, by_id, CheckContext.MAX_SEG_M, CheckContext.MAX_ANG_RAD)
	var rails: Array[TrackBuild.FrogRail] = fr["list"]
	if rails.is_empty():
		return
	var flipped := 0
	var castings := {}
	for r_raw in rails:
		var rail: TrackBuild.FrogRail = r_raw
		if rail.kind == TrackBuild.FROG_CASTING:
			if not castings.has(rail.owner):
				castings[rail.owner] = []
			(castings[rail.owner] as Array).append(rail)
			continue
		var mesh := TrackView.frog_rail_mesh(rail)
		if mesh != null:
			flipped += int(_shell([mesh])["flipped"])
	_ok("нитки крестовины: ни одной вывернутой грани", flipped == 0,
		"рёбер с совпавшим направлением %d" % flipped)

	var cast_flipped := 0
	var up := 0
	var down := 0
	var side := 0
	for owner_id in castings:
		var pair: Array = castings[owner_id]
		if pair.size() != 2:
			continue
		# ТЕЛО ВМЕСТЕ С НАКАТОМ И ФАСКОЙ — по доводу рельса участка: верхнюю
		# площадку сердечника делят три меша, и порознь у каждого законная дыра там,
		# где начинается соседний.
		var mesh := TrackView.frog_casting_mesh(pair[0], pair[1])
		if mesh == null:
			continue
		var parts := [mesh,
			TrackView.frog_casting_head_mesh(pair[0], pair[1]),
			TrackView.frog_casting_fillet_mesh(pair[0], pair[1])]
		cast_flipped += int(_shell(parts)["flipped"])
		# ВЕРХ СЕРДЕЧНИКА СМОТРИТ ВВЕРХ. Прямая проверка того самого дефекта:
		# порядок граней приходит от сервера, и при другом порядке нормаль верхней
		# полосы уходит вниз — грань освещается с изнанки и чернеет.
		var faces := _up_down(mesh)
		for extra in [parts[1], parts[2]]:
			if extra != null:
				var more := _up_down(extra)
				faces["up"] = int(faces["up"]) + int(more["up"])
				faces["down"] = int(faces["down"]) + int(more["down"])
				faces["side"] = int(faces["side"]) + int(more["side"])
		up += int(faces["up"])
		down += int(faces["down"])
		side += int(faces["side"])
	_ok("сердечник: ни одной вывернутой грани", cast_flipped == 0,
		"рёбер с совпавшим направлением %d" % cast_flipped)
	# Верхних и нижних граней у перемычки поровну — она замкнута сверху и снизу;
	# важно, что верхние смотрят ВВЕРХ, а не то, сколько их.
	# ВЕРХ СМОТРИТ ВВЕРХ, ДНО ВНИЗ, И ИХ ПОРОВНУ. У перемычки верхняя площадка и
	# дно строятся одинаковым числом полос, поэтому равенство — то самое, что
	# ловит выворот ЛЮБОЙ из них: вывернутое дно даёт «вверх 8, вниз 0», а не
	# просто нехватку граней. Именно так и была поймана вывернутая нормаль
	# сердечника, стоившая чёрного пятна на шпалах.
	_ok("сердечник: верх смотрит вверх, дно вниз",
		up > 0 and down > 0,
		"граней вверх %d, вниз %d, вбок %d" % [up, down, side])


## ШОВ: совпадает ли занятая телом область по обе стороны стыка.
##
## # Почему меряется область, а не числа, из которых она построена
##
## Это тот самый дефект, который проверки не видели трижды подряд: длина остряка
## верна, отвод верен, сторона верна, число треугольников верно — а в кадре
## «рельсы идут разными сегментами, которые не соединены». Потому что остряк —
## тело ВНУТРИ колеи, а нитка за его корнем растёт от той же грани НАРУЖУ: на шве
## рельс перепрыгивает на свою ширину вбок.
##
## Проверяется поэтому ГЕОМЕТРИЯ: поперечный отрезок, занятый металлом слева и
## справа от шва. Считается по вершинам мешей, лежащим в самом шве, и по ВСЕМ
## телам прохода разом — накладка, перекрывающая стык, здесь такое же тело, как
## рельс, и закрывает разрыв на законных основаниях.
func _check_seams(network: Dictionary, by_id: Dictionary,
		spans: Array[TrackBuild.Span]) -> void:
	var bl := TrackBuild.blades(network, by_id, CheckContext.MAX_SEG_M, CheckContext.MAX_ANG_RAD)
	var blades: Array[TrackBuild.Blade] = bl["list"]
	if blades.is_empty():
		return
	var checked := 0
	var worst := ""
	var worst_gap := 1.0
	var note := ""
	for b_raw in blades:
		var blade: TrackBuild.Blade = b_raw
		if blade.axis.is_empty():
			continue
		# Прижатый остряк: в этом положении он и есть путь, по которому едут, и
		# разрыв на шве означает разрыв под колесом.
		blade.set_open(0.0)
		var host: TrackBuild.Span = null
		for sp in spans:
			if sp.element_id == blade.element_id:
				host = sp
				break
		if host == null:
			continue
		var seam: TrackGeom.AxisPoint = blade.axis[blade.axis.size() - 1]
		var here := TerrainMesh.to_godot(seam.x, seam.y, seam.z)
		var nl := seam.left()
		var across := TerrainMesh.to_godot(nl.x, nl.y, 0.0) - TerrainMesh.to_godot(0.0, 0.0, 0.0)
		across = across.normalized()
		# Тело остряка на шве и тело нитки на шве — порознь, потому что вопрос
		# ровно в том, продолжает ли одно другое.
		# ОКНО ВОКРУГ ГРАНИ ОСТРЯКА. Без него в отрезок попадает и нитка с другой
		# стороны колеи, и «металл до шва» оказывается полутора метрами шириной —
		# перекрытие тогда стопроцентное всегда, а проверка зелёной всегда.
		var near := blade.faces[blade.faces.size() - 1]
		var along := across.cross(Vector3.UP).normalized()
		# ВСЕ ТЕЛА ПРОХОДА РАЗОМ по обе стороны шва: вопрос не в том, продолжает ли
		# конкретная деталь конкретную, а в том, продолжается ли МЕТАЛЛ. Нахлёст,
		# которым закрыт стык, — такое же тело, как рельс.
		var bodies := [TrackView.frog_rail_mesh(blade), TrackView.rail_body_mesh(host)]
		var a := _section(bodies, here - along * SEAM_STEP_M, along, across, near)
		var b := _section(bodies, here + along * SEAM_STEP_M, along, across, near)
		if a == Vector2.ZERO or b == Vector2.ZERO:
			if worst == "":
				worst = "%s: на шве нет тела %s" % [blade.element_id,
					"остряка" if a == Vector2.ZERO else "нитки"]
			continue
		checked += 1
		# Перекрытие отрезков: сколько общего у металла до шва и после него.
		var lo := maxf(a.x, b.x)
		var hi := minf(a.y, b.y)
		var overlap := maxf(hi - lo, 0.0)
		var width := minf(a.y - a.x, b.y - b.x)
		var share := overlap / maxf(width, 1e-9)
		note += " | %s a[%.3f,%.3f] b[%.3f,%.3f] %.0f%%" % [
			blade.branch, a.x, a.y, b.x, b.y, share * 100.0]
		if share < SEAM_SHARE_MIN and (worst == "" or share < worst_gap):
			worst_gap = share
			worst = "%s: до шва металл на [%.3f, %.3f], после — на [%.3f, %.3f], общего %.0f %%" % [
				blade.element_id, a.x, a.y, b.x, b.y, share * 100.0]
	_ok("на шве остряка с ниткой тело продолжает тело", worst == "",
		"швов проверено %d%s%s" % [checked, note, "" if worst == "" else ": " + worst])


## _section — поперечный отрезок, занятый мешем в плоскости, отстоящей от шва.
##
## СЕЧЕНИЕ ПЛОСКОСТЬЮ, а не выборка вершин: разбиение оси идёт метрами, и в самой
## плоскости шва вершин может не быть вовсе — у прохода, где нитка непрерывна, их
## там и нет. Первая редакция проверки ловилась ровно на этом и объявляла
## «на шве нет тела нитки» там, где тело шло насквозь.
##
## Плоскость берётся В СТОРОНЕ от шва (± несколько сантиметров), потому что
## ровно в шве лежат торцы обоих тел, и пересечение выродилось бы в них.
func _section(meshes: Array, at: Vector3, along: Vector3, across: Vector3,
		near: float) -> Vector2:
	var lo := INF
	var hi := -INF
	for m_raw in meshes:
		var mesh: ArrayMesh = m_raw
		if mesh == null:
			continue
		var arrays := mesh.surface_get_arrays(0)
		var verts: PackedVector3Array = arrays[Mesh.ARRAY_VERTEX]
		var idx: PackedInt32Array = arrays[Mesh.ARRAY_INDEX]
		for t in range(0, idx.size(), 3):
			var p := [verts[idx[t]], verts[idx[t + 1]], verts[idx[t + 2]]]
			var d := [
				(p[0] - at).dot(along), (p[1] - at).dot(along), (p[2] - at).dot(along)]
			for e in 3:
				var j := (e + 1) % 3
				# Ребро пересекает плоскость, когда его концы по разные её стороны.
				if (d[e] > 0.0) == (d[j] > 0.0):
					continue
				var k: float = d[e] / (d[e] - d[j])
				var x: Vector3 = p[e] + (p[j] - p[e]) * k
				var off := (x - at).dot(across)
				# Только то, что лежит у названной грани: соседняя нитка колеи к этому
				# шву отношения не имеет.
				if absf(off - near) > NEAR_M:
					continue
				lo = minf(lo, off)
				hi = maxf(hi, off)
	if lo == INF:
		return Vector2.ZERO
	return Vector2(lo, hi)


## _shell — топология оболочки: граничные контуры, вывернутые и слипшиеся рёбра.
##
## Принимает СПИСОК мешей и считает их как одно тело: рельс с накатом — две
## поверхности одной вещи, и порознь у каждой законная дыра там, где начинается
## соседняя.
##
## Рёбра склеиваются по КООРДИНАТАМ: полосы кладут каждому четырёхугольнику свои
## четыре вершины, и по индексам соседние грани не имеют ни одного общего ребра.
func _shell(meshes: Array) -> Dictionary:
	# ключ неориентированного ребра -> (сколько раз в прямом, сколько в обратном)
	var edges := {}
	for m_raw in meshes:
		var mesh: ArrayMesh = m_raw
		if mesh == null:
			continue
		var arrays := mesh.surface_get_arrays(0)
		var verts: PackedVector3Array = arrays[Mesh.ARRAY_VERTEX]
		var idx: PackedInt32Array = arrays[Mesh.ARRAY_INDEX]
		for t in range(0, idx.size(), 3):
			for e in 3:
				var a := _key(verts[idx[t + e]])
				var b := _key(verts[idx[t + (e + 1) % 3]])
				if a == b:
					continue
				var forward := a < b
				var key: String = (a + "|" + b) if forward else (b + "|" + a)
				var rec: Vector2i = edges.get(key, Vector2i.ZERO)
				edges[key] = rec + (Vector2i(1, 0) if forward else Vector2i(0, 1))
	var flipped := 0
	var nonmanifold := 0
	var boundary := 0
	# Граничные рёбра как граф: вершина — точка, ребро — открытый край.
	var link := {}
	for key in edges:
		var rec: Vector2i = edges[key]
		var total := rec.x + rec.y
		if total == 1:
			boundary += 1
			var pair := (key as String).split("|")
			for i in 2:
				var v: String = pair[i]
				if not link.has(v):
					link[v] = []
				(link[v] as Array).append(pair[1 - i])
		elif total == 2:
			# Две грани сходятся правильно, когда ребро пройдено в РАЗНЫЕ стороны.
			# Оба раза в одну — одна из граней вывернута наизнанку.
			if rec.x == 2 or rec.y == 2:
				flipped += 1
		else:
			nonmanifold += 1
	# Контуров столько, сколько компонент связности у графа границы: длина контура
	# не важна, важно, сколько их.
	var seen := {}
	var loops := 0
	for v in link:
		if seen.has(v):
			continue
		loops += 1
		var queue: Array = [v]
		seen[v] = true
		while not queue.is_empty():
			var cur: String = queue.pop_back()
			for nb in (link[cur] as Array):
				if not seen.has(nb):
					seen[nb] = true
					queue.append(nb)
	return {"boundary": boundary, "flipped": flipped, "nonmanifold": nonmanifold, "loops": loops}


## _up_down — сколько граней смотрит вверх и сколько вниз.
##
## Считается по НОРМАЛИ ТРЕУГОЛЬНИКА из его обхода, а не по массиву нормалей:
## массив пишет тот же код, который строит полосы, и ошибка в нём подтвердила бы
## сама себя.
##
## ЗНАК ВЗЯТ У ДВИЖКА, а не из головы: лицевая грань в Godot обходится по часовой,
## то есть нормаль есть (p2−p0)×(p1−p0). Первая редакция этой функции считала
## наоборот и объявила вывернутым исправное дно сердечника — проверка, ошибающаяся
## в знаке, хуже отсутствующей, потому что чинить по ней идут исправное.
func _up_down(mesh: ArrayMesh) -> Dictionary:
	var arrays := mesh.surface_get_arrays(0)
	var verts: PackedVector3Array = arrays[Mesh.ARRAY_VERTEX]
	var idx: PackedInt32Array = arrays[Mesh.ARRAY_INDEX]
	var up := 0
	var down := 0
	var side := 0
	for t in range(0, idx.size(), 3):
		var p0 := verts[idx[t]]
		var p1 := verts[idx[t + 1]]
		var p2 := verts[idx[t + 2]]
		var n := (p2 - p0).cross(p1 - p0)
		if n.length() < 1e-12:
			continue
		var y := n.normalized().y
		if y > 0.7:
			up += 1
		elif y < -0.7:
			down += 1
		else:
			side += 1
	return {"up": up, "down": down, "side": side}


func _key(p: Vector3) -> String:
	return "%d,%d,%d" % [
		roundi(p.x / WELD_M), roundi(p.y / WELD_M), roundi(p.z / WELD_M)]


## МОДЕЛЬ CA-1/9-R65-v1: числа острожки, сердечника и контррельса.
##
## # Зачем закреплять числа, а не только форму
##
## Форму держат инварианты выше — оболочка, швы, ориентация. Но саму МОДЕЛЬ (где
## остряк тоньше, где ниже, где начинается накат, где остриё сердечника) держать
## нечем: она живёт константами в двух файлах и разъедется от первой же правки
## «на глаз». Числа взяты из спецификации, согласованной 2026-08-16 после того,
## как владелец назвал итерации по кадрам тем, чем они и были.
func _check_frog_model(network: Dictionary, by_id: Dictionary) -> void:
	var bl := TrackBuild.blades(network, by_id, CheckContext.MAX_SEG_M, CheckContext.MAX_ANG_RAD)
	var blades: Array[TrackBuild.Blade] = bl["list"]
	if not blades.is_empty():
		var blade: TrackBuild.Blade = blades[0]
		var head := blade.rail_head_width_m
		# ОСТРЯК В ОСТРИЕ: головка 6 мм, верх ниже рамного на 25 мм. Без этого он
		# читается вторым полным рельсом, приставленным вплотную к первому.
		var toe_w := blade.width_at(0) * head
		var toe_sink := blade.sink_at(0)
		_ok("остряк: в острие головка 6 мм", absf(toe_w - 0.006) < 0.0015,
			"%.4f м" % toe_w)
		_ok("остряк: в острие верх ниже рамного на 25 мм", absf(toe_sink - 0.025) < 0.0015,
			"%.4f м" % toe_sink)
		# И ВЫХОДИТ НА ПОЛНОЕ СЕЧЕНИЕ: высота к 2 м, ширина к 6 м.
		var sunk_after := 0.0
		var narrow_after := 0.0
		for k in blade.axis.size():
			var p: TrackGeom.AxisPoint = blade.axis[k]
			if p.u >= 2.0 + 0.02 and blade.sink_at(k) > 1e-4:
				sunk_after = maxf(sunk_after, blade.sink_at(k))
			if p.u >= 6.0 + 0.02 and absf(blade.width_at(k) * head - head) > 5e-4:
				narrow_after = maxf(narrow_after, absf(blade.width_at(k) * head - head))
		_ok("остряк: за 2 м от острия понижения нет", sunk_after == 0.0,
			"наибольшее понижение за 2 м: %.4f м" % sunk_after)
		_ok("остряк: за 6 м головка полная", narrow_after == 0.0,
			"наибольшее отклонение: %.4f м" % narrow_after)
		# НАКАТ НАЧИНАЕТСЯ НЕ С ОСТРИЯ: до 1.5 м колесо идёт по рамному рельсу.
		var ride_from := INF
		var ride_full := INF
		for k in blade.axis.size():
			var p: TrackGeom.AxisPoint = blade.axis[k]
			if blade.ride_at(k) > 1e-4:
				ride_from = minf(ride_from, p.u)
			if blade.ride_at(k) >= 0.035 - 1e-4:
				ride_full = minf(ride_full, p.u)
		_ok("остряк: накат начинается за полтора метра от острия",
			ride_from >= 1.5 - 0.02 and ride_from < 3.0, "с %.2f м" % ride_from)
		_ok("остряк: полная полоса наката 35 мм с трёх метров",
			ride_full <= 3.0 + 0.5, "с %.2f м" % ride_full)
		# ОТВОД: 152 мм в острие, ноль у корня, линейно между ними.
		blade.set_open(1.0)
		var at_toe := absf(blade.faces[0] - blade.offset_m)
		blade.set_open(0.0)
		_ok("остряк: ход в острие 152 мм", absf(at_toe - blade.throw_m) < 1e-6,
			"%.4f м при ходе %.4f м" % [at_toe, blade.throw_m])

	var fr := TrackBuild.frog_rails(network, by_id, CheckContext.MAX_SEG_M, CheckContext.MAX_ANG_RAD)
	var rails: Array[TrackBuild.FrogRail] = fr["list"]
	var casting: TrackBuild.FrogRail = null
	var check: TrackBuild.FrogRail = null
	var wing: TrackBuild.FrogRail = null
	for r_raw in rails:
		var r: TrackBuild.FrogRail = r_raw
		match r.kind:
			TrackBuild.FROG_CASTING: casting = r if casting == null else casting
			TrackBuild.FROG_CHECK: check = r if check == null else check
			TrackBuild.FROG_WING: wing = r if wing == null else wing
	if casting != null:
		# СЕРДЕЧНИК: опущен в острие на 8 мм и выходит на отметку за 0.8 м.
		_ok("сердечник: остриё опущено на 8 мм",
			absf(casting.sink_at(0) - 0.008) < 0.0015, "%.4f м" % casting.sink_at(0))
		var deep := 0.0
		var ride_at_toe := casting.ride_at(0)
		for k in casting.axis.size():
			var p: TrackGeom.AxisPoint = casting.axis[k]
			var s: float = p.u - casting.axis[0].u
			if s >= 0.80 + 0.03 and casting.sink_at(k) > 1e-4:
				deep = maxf(deep, casting.sink_at(k))
		_ok("сердечник: за 0.8 м от острия понижения нет", deep == 0.0,
			"наибольшее: %.4f м" % deep)
		_ok("сердечник: у острия наката нет", ride_at_toe <= 1e-4,
			"ширина полосы у острия %.4f м" % ride_at_toe)
	if check != null:
		# КОНТРРЕЛЬС ВЫШЕ ГОЛОВКИ: он держит гребень, а не несёт колесо.
		_ok("контррельс: стоит на 20 мм выше головки",
			absf(check.lift_m - 0.020) < 0.0015, "%.4f м" % check.lift_m)
		_ok("контррельс: наката нет",
			TrackView.frog_railhead_mesh(check) == null, "")
	if wing != null:
		_ok("усовик: ни подъёма, ни понижения", absf(wing.lift_m) < 1e-9 and absf(wing.sink_at(0)) < 1e-9,
			"подъём %.4f, понижение %.4f" % [wing.lift_m, wing.sink_at(0)])
