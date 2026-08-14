## ЧТО ВЫШЛО ИЗ РИСУЮЩЕГО КОДА ПУТИ, а не что в него вошло.
##
## Дыра, ради которой суита заведена (ClearAhead-9u9): до 2026-08-14 ни одна
## проверка не смотрела на РЕЗУЛЬТАТ TrackView. 40_construction проверяет вход —
## раскладку, размеры, адреса, — и на этом останавливается; всё, что дальше
## превращает рецепт в треугольники, держалось на снимке экрана. Снимок ловит
## пустой кадр, но не отличает 234 шпалы от 233 и не говорит, какая функция
## отдала null.
##
## # ЧИСЛА СЧИТАЮТСЯ НЕ ТЕМ ЖЕ СПОСОБОМ, ЧТО В КОДЕ
##
## Правило суиты 40 действует и здесь: треугольники считаются из СТРОЕНИЯ формы,
## объявленного в шапках TrackView, и числа точек оси — а не повторением его
## арифметики. Призма — четыре полосы (верх, два откоса, низ), значит 8·(n−1)
## треугольников на участок из n точек. Рельс телом — по четыре полосы на нитку,
## 16·(n−1). Накат — по одной, 4·(n−1). Коробка — шесть граней, 12
## треугольников и 24 вершины (вершины не общие: у граней разные нормали).
## Разойдётся строение с кодом — разойдётся и число.
##
## # ОБХОД ПРОВЕРЯЕТСЯ ЭТАЛОНОМ ДВИЖКА, А НЕ ПАМЯТЬЮ
##
## Дважды за один день (2026-08-12) проект терял геометрию на вывернутом обходе:
## коробки шпал и домов стояли изнанкой наружу («прозрачные шпалы»), правая
## платформа не рисовалась вовсе. Оба раза находил владелец глазами, оба раза
## код при этом выглядел правдоподобно.
##
## Правило «лицевая грань обходится по часовой» здесь НЕ ЗАПИСАНО КОНСТАНТОЙ —
## оно спрашивается у BoxMesh движка: у примитива нормали заведомо наружные, и
## связь между ними и правой нормалью обхода — это и есть искомое соглашение.
## Помнить его наизусть уже стоило дорого; проверка, помнящая его неверно, была
## бы хуже отсутствующей.
extends "res://tools/check_suite.gd"

## Сколько коробок берётся на проверку обхода. Обход — свойство ОДНОЙ коробки, и
## 234 шпалы проверяют его 234 раза одинаково; сотни лишних миллисекунд за это
## не платятся. Счёт треугольников при этом идёт по всей решётке.
const WINDING_SAMPLE := 6

## Допуск на «вершина лежит в плоскости верха плиты». PackedVector3Array всегда
## float32 (правило проекта: float64 не сверяется байт в байт), а отметка верха
## приходит в него из double.
const EPS_TOP_M := 1e-4

## Габарит пробной платформы. Числа свои, не из сети, нарочно: у затравки ST_A
## платформа объявлена одной стороной, а проверяется здесь ИМЕННО РАЗНИЦА СТОРОН.
const PROBE_OFFSET_M := 1.75
const PROBE_WIDTH_M := 4.0
const PROBE_HEIGHT_M := 1.1
const PROBE_SLAB_M := 0.2


func run() -> void:
	var network := await ctx.network_data()
	var elements := await ctx.elements()
	if elements.is_empty():
		return
	var by_id := TrackBuild.elements_by_id(elements)
	var spans := TrackBuild.covered_spans(network, by_id, CheckContext.MAX_SEG_M, CheckContext.MAX_ANG_RAD)

	var front := _front_sign()
	_ok("соглашение об обходе снято с BoxMesh движка", front != 0.0,
		"правая нормаль обхода лицевой грани %s нормали грани" % ["сонаправлена" if front > 0.0 else "противонаправлена"])

	_check_prisms(spans)
	_check_rails(spans)
	_check_sleepers(network, by_id, front)
	_check_buffer_stops(network, by_id, front)
	_check_platforms(network, by_id, front)
	_check_frogs(network, by_id)
	_check_lines(spans, elements)
	_check_refusals(spans)


## ПРИЗМА. Строится у каждого покрытого участка — и это уже проверено суитой 40
## по входу (`has_prism`); здесь проверяется, что из входа вышел меш.
func _check_prisms(spans: Array[TrackBuild.Span]) -> void:
	var empty: Array[String] = []
	var want := 0
	var got := 0
	for sp in spans:
		if not sp.has_prism():
			continue
		want += 8 * (sp.axis.size() - 1)
		var mesh := TrackView.prism_mesh(sp)
		if mesh == null:
			empty.append(sp.element_id)
			continue
		got += _tris(mesh)
	_ok("призма построена у каждого участка, у которого есть чем", empty.is_empty(), str(empty))
	_ok("треугольников призмы = 8·(точек оси − 1)", got == want, "%d против %d" % [got, want])


## РЕЛЬС И НАКАТ. Две поверхности, и они обязаны считать одни и те же точки:
## разойдись тело с накатом — между ними откроется щель во всю длину пути.
func _check_rails(spans: Array[TrackBuild.Span]) -> void:
	var empty_body: Array[String] = []
	var empty_head: Array[String] = []
	var want_body := 0
	var got_body := 0
	var want_head := 0
	var got_head := 0
	for sp in spans:
		if not sp.has_rail_body():
			continue
		var n := sp.axis.size()
		want_body += 16 * (n - 1)
		want_head += 4 * (n - 1)
		var body := TrackView.rail_body_mesh(sp)
		if body == null:
			empty_body.append(sp.element_id)
		else:
			got_body += _tris(body)
		var head := TrackView.railhead_mesh(sp)
		if head == null:
			empty_head.append(sp.element_id)
		else:
			got_head += _tris(head)
	_ok("рельс телом построен у каждого участка с колеёй", empty_body.is_empty(), str(empty_body))
	_ok("накат построен у каждого участка с колеёй", empty_head.is_empty(), str(empty_head))
	_ok("треугольников рельса = 16·(точек оси − 1)", got_body == want_body,
		"%d против %d" % [got_body, want_body])
	_ok("треугольников наката = 4·(точек оси − 1)", got_head == want_head,
		"%d против %d" % [got_head, want_head])


## РЕШЁТКА. Одним мешем на всю станцию — у шпалы нет доменной идентичности, и
## узел сцены ей выделять нечем.
func _check_sleepers(network: Dictionary, by_id: Dictionary, front: float) -> void:
	var sleepers: Array[TrackBuild.Sleeper] = TrackBuild.sleepers(network, by_id)["list"]
	if sleepers.is_empty():
		_ok("решётка построена", false, "ни одной шпалы во входе")
		return
	var mesh := TrackView.sleeper_mesh(sleepers)
	_ok("решётка построена одним мешем", mesh != null)
	if mesh == null:
		return
	_ok("треугольников решётки = 12 на шпалу", _tris(mesh) == 12 * sleepers.size(),
		"%d при %d шпалах" % [_tris(mesh), sleepers.size()])
	_ok("вершин решётки = 24 на шпалу", _verts(mesh) == 24 * sleepers.size(),
		"%d при %d шпалах" % [_verts(mesh), sleepers.size()])

	# ОБХОД. Шпала — замкнутая коробка с известным центром, и этого хватает:
	# «наружу» здесь не мнение, а направление от центра к грани.
	var flat := 0
	var checked := 0
	var seen := 0
	var bad_face := 0
	var bad_normal := 0
	for k in mini(WINDING_SAMPLE, sleepers.size()):
		var s: TrackBuild.Sleeper = sleepers[k]
		if s.height_m <= 0.0:
			# Плоская шпала — законный случай (высоту не прислали), но объёма у
			# неё нет, и «наружу» для неё не определено.
			flat += 1
			continue
		var one: Array[TrackBuild.Sleeper] = [s]
		var box := TrackView.sleeper_mesh(one)
		if box == null:
			continue
		var centre := TerrainMesh.to_godot(s.pose.x, s.pose.y, (s.top_z() + s.bottom_z()) * 0.5)
		var verdict := _outward(box, centre, front)
		checked += 1
		seen += int(verdict["faces"])
		bad_face += int(verdict["wrong_face"])
		bad_normal += int(verdict["wrong_normal"])
	# Граней СЧИТАНО ровно двенадцать на коробку: без этого «изнанкой ноль»
	# истинно и тогда, когда смотреть было не на что.
	_ok("шпалы взяты на проверку обхода", checked > 0 and seen == checked * 12,
		"%d коробок, граней %d, плоских %d" % [checked, seen, flat])
	_ok("все грани шпалы лицевые снаружи", bad_face == 0, "%d граней изнанкой" % bad_face)
	_ok("нормали шпалы смотрят наружу", bad_normal == 0, "%d нормалей внутрь" % bad_normal)


## УПОРЫ. Коробки той же функцией, что и шпалы, — значит и обход у них общий.
func _check_buffer_stops(network: Dictionary, by_id: Dictionary, front: float) -> void:
	var stops: Array[TrackBuild.BufferStop] = TrackBuild.buffer_stops(network, by_id)["list"]
	if stops.is_empty():
		_ok("упоры построены", false, "ни одного упора во входе")
		return
	var ratio := 1.0 / 3.0
	var mesh := TrackView.buffer_stop_mesh(stops, ratio)
	_ok("упоры построены одним мешем", mesh != null)
	if mesh == null:
		return
	_ok("треугольников упоров = 12 на упор", _tris(mesh) == 12 * stops.size(),
		"%d при %d упорах" % [_tris(mesh), stops.size()])
	var st: TrackBuild.BufferStop = stops[0]
	var one: Array[TrackBuild.BufferStop] = [st]
	var box := TrackView.buffer_stop_mesh(one, ratio)
	var centre := TerrainMesh.to_godot(st.pose.x, st.pose.y, st.pose.z + st.height_m * 0.5)
	var verdict := _outward(box, centre, front)
	# Граней СЧИТАНО больше нуля — иначе проверка истинна вакуумно, а это ровно
	# та беда, ради которой заведена вся суита.
	_ok("грани упора лицевые снаружи",
		int(verdict["faces"]) == 12 and int(verdict["wrong_face"]) == 0, str(verdict))


## ПЛАТФОРМА. Здесь живёт ошибка, стоившая целой платформы на экране: обход плиты
## зависит от СТОРОНЫ, и правая рисовалась изнанкой, то есть не рисовалась.
## Затравка ST_A объявляет одну сторону, поэтому вторая проверяется пробной
## полосой, построенной здесь же.
func _check_platforms(network: Dictionary, by_id: Dictionary, front: float) -> void:
	var strips: Array[TrackBuild.PlatformStrip] = TrackBuild.platforms(
		network, by_id, CheckContext.MAX_SEG_M, CheckContext.MAX_ANG_RAD)["list"]
	var with_slab := 0
	var empty: Array[String] = []
	var want := 0
	var got := 0
	var flat_want := 0
	var flat_got := 0
	for p in strips:
		var n := mini(p.near.size(), p.far.size())
		flat_want += 2 * (n - 1)
		var flat := TrackView.strip_mesh(p.near, p.far)
		if flat != null:
			flat_got += _tris(flat)
		if not p.has_slab():
			continue
		with_slab += 1
		want += 6 * (n - 1)
		var slab := TrackView.slab_mesh(p)
		if slab == null:
			empty.append(p.id)
			continue
		got += _tris(slab)
	_ok("полоса платформы построена: 2·(точек кромки − 1)", flat_got == flat_want,
		"%d против %d" % [flat_got, flat_want])
	_ok("плита построена у каждой платформы с высотой", empty.is_empty(),
		"%s, плит %d из %d полос" % [str(empty), with_slab, strips.size()])
	_ok("треугольников плиты = 6·(точек кромки − 1)", got == want, "%d против %d" % [got, want])

	for side in ["left", "right"]:
		var probe := _probe_strip(String(side))
		var mesh := TrackView.slab_mesh(probe)
		if mesh == null:
			_ok("пробная плита стороны %s построена" % side, false)
			continue
		# ВЕРХ ПЛИТЫ ВИДЕН СВЕРХУ — иначе платформы на экране нет. Верхние
		# треугольники отбираются по отметке, а не по порядку в массиве: порядок
		# принадлежит рисующему коду, отметка — платформе.
		var top := _top_faces(mesh, PROBE_HEIGHT_M)
		var wrong := 0
		for tri_raw in top:
			var tri: Array = tri_raw
			var rh: Vector3 = (tri[1] as Vector3 - tri[0] as Vector3).cross(tri[2] as Vector3 - tri[0] as Vector3)
			if signf(rh.dot(Vector3.UP)) != front:
				wrong += 1
		_ok("верх плиты стороны %s лицевой сверху" % side, top.size() > 0 and wrong == 0,
			"верхних треугольников %d, изнанкой %d" % [top.size(), wrong])


## КРЕСТОВИНЫ. Галочка — две касательные по два треугольника; нулевая касательная
## пропускается, и это единственная причина расхождения счёта.
func _check_frogs(network: Dictionary, by_id: Dictionary) -> void:
	var frogs: Array[TrackBuild.Frog] = TrackBuild.frogs(network, by_id)["list"]
	if frogs.is_empty():
		_ok("крестовины построены", false, "ни одной крестовины во входе")
		return
	var want := 0
	for f in frogs:
		for t in f.tangents:
			if t.length() > 0.0:
				want += 2
	var mesh := TrackView.frog_mesh(frogs, 3.0, 0.35)
	_ok("галочки крестовин построены", mesh != null)
	if mesh == null:
		return
	_ok("треугольников галочек = 2 на касательную", _tris(mesh) == want,
		"%d против %d при %d крестовинах" % [_tris(mesh), want, frogs.size()])


## НИТКИ И ОСИ. Линиями, а не полосами: ширина нитки — величина экранная.
func _check_lines(spans: Array[TrackBuild.Span], elements: Array[TrackGeom.Element]) -> void:
	var want_surfaces := 0
	var got_surfaces := 0
	var empty: Array[String] = []
	for sp in spans:
		var threads := sp.threads()
		var n := 0
		for line in threads:
			if line.size() >= 2:
				n += 1
		want_surfaces += n
		var mesh := TrackView.rail_mesh(threads)
		if mesh == null:
			if n > 0:
				empty.append(sp.element_id)
			continue
		got_surfaces += mesh.get_surface_count()
	_ok("нитки построены у всех участков с колеёй", empty.is_empty(), str(empty))
	_ok("поверхностей ниток = число ниток длиннее точки", got_surfaces == want_surfaces,
		"%d против %d" % [got_surfaces, want_surfaces])

	var no_line: Array[String] = []
	for el in elements:
		if el.points.size() < 2:
			continue
		var mesh := TrackView.line_mesh(el.points)
		if mesh == null or mesh.get_surface_count() != 1:
			no_line.append(el.id)
	_ok("ось нитью построена у каждого элемента", no_line.is_empty(), str(no_line))


## ОТКАЗ, А НЕ ПОЛОВИНА МЕША. Правило проекта («валидатор отказывает, а не
## чинит») в рисующем коде значит вот что: не хватило присланного — не рисуй.
## Полоса нулевой ширины и призма без габарита выглядели бы на экране правдой.
func _check_refusals(spans: Array[TrackBuild.Span]) -> void:
	var axis: Array[TrackGeom.AxisPoint] = [
		TrackGeom.AxisPoint.new(0.0, 0.0, 0.0, 0.0, 0.0),
		TrackGeom.AxisPoint.new(10.0, 0.0, 0.0, 0.0, 10.0),
		TrackGeom.AxisPoint.new(20.0, 0.0, 0.0, 0.0, 20.0),
	]
	var ribbon := TrackView.ribbon_mesh(axis, 1.5)
	_ok("лента из трёх точек — четыре треугольника", ribbon != null and _tris(ribbon) == 4,
		"%d" % _tris(ribbon))
	_ok("лента нулевой ширины отвергнута", TrackView.ribbon_mesh(axis, 0.0) == null)
	var one_point: Array[TrackGeom.AxisPoint] = [axis[0]]
	_ok("лента из одной точки отвергнута", TrackView.ribbon_mesh(one_point, 1.5) == null)
	_ok("ось из одной точки отвергнута", TrackView.line_mesh(one_point) == null)

	var short_edge := PackedVector3Array([Vector3.ZERO])
	_ok("полоса из одной пары кромок отвергнута",
		TrackView.strip_mesh(short_edge, short_edge) == null)

	var bare := TrackBuild.Span.new()
	bare.element_id = "PROBE"
	bare.axis = axis
	_ok("призма без габарита отвергнута", TrackView.prism_mesh(bare) == null)
	_ok("рельс без колеи отвергнут", TrackView.rail_body_mesh(bare) == null)
	_ok("накат без колеи отвергнут", TrackView.railhead_mesh(bare) == null)

	var no_sleepers: Array[TrackBuild.Sleeper] = []
	_ok("пустая решётка отвергнута", TrackView.sleeper_mesh(no_sleepers) == null)
	var no_stops: Array[TrackBuild.BufferStop] = []
	_ok("пустой список упоров отвергнут", TrackView.buffer_stop_mesh(no_stops, 1.0 / 3.0) == null)
	var no_frogs: Array[TrackBuild.Frog] = []
	_ok("пустой список крестовин отвергнут", TrackView.frog_mesh(no_frogs, 3.0, 0.35) == null)

	# ПРОВЕРКА НЕ ДОЛЖНА ЗАВИСЕТЬ ОТ ТОГО, ЧТО СЕГОДНЯ ПРИСЛАЛИ: если завтра
	# затравка потеряет габарит призмы, отказы выше останутся, а счёт выше
	# покажет ноль участков. Здесь это названо числом.
	_ok("участков во входе больше нуля", spans.size() > 0, "%d" % spans.size())


## _probe_strip — пробная платформа своей стороны, построенная здесь.
##
## Ось идёт вдоль +x, значит левая нормаль смотрит в +y: сторона задаётся знаком
## отступа, ровно как её задаёт TrackBuild от левой нормали позы.
func _probe_strip(side: String) -> TrackBuild.PlatformStrip:
	var p := TrackBuild.PlatformStrip.new()
	p.id = "PROBE_" + side
	p.element_id = "PROBE"
	p.side = side
	p.offset_m = PROBE_OFFSET_M
	p.width_m = PROBE_WIDTH_M
	p.height_m = PROBE_HEIGHT_M
	p.slab_thickness_m = PROBE_SLAB_M
	var sgn := 1.0 if side == "left" else -1.0
	var near := PackedVector3Array()
	var far := PackedVector3Array()
	for k in 3:
		var x := float(k) * 5.0
		near.append(Vector3(x, sgn * PROBE_OFFSET_M, PROBE_HEIGHT_M))
		far.append(Vector3(x, sgn * (PROBE_OFFSET_M + PROBE_WIDTH_M), PROBE_HEIGHT_M))
	p.near = near
	p.far = far
	return p


## _front_sign — СОГЛАШЕНИЕ ДВИЖКА ОБ ОБХОДЕ, спрошенное у него самого.
##
## У BoxMesh нормали заведомо наружные, поэтому знак скалярного произведения
## «правая нормаль обхода · нормаль грани» и есть правило: у Godot он −1
## (лицевая грань обходится так, что правая нормаль смотрит ОТ зрителя). Ноль
## значит, что грани примитива между собой не согласны, — тогда эталона нет и
## проверять обход нечем.
func _front_sign() -> float:
	var arrays := BoxMesh.new().get_mesh_arrays()
	var sign_seen := 0.0
	for tri_raw in _faces(arrays):
		var tri: Array = tri_raw
		var rh: Vector3 = (tri[1] as Vector3 - tri[0] as Vector3).cross(tri[2] as Vector3 - tri[0] as Vector3)
		var s := signf(rh.dot(tri[3] as Vector3))
		if s == 0.0 or (sign_seen != 0.0 and s != sign_seen):
			return 0.0
		sign_seen = s
	return sign_seen


## _outward — сколько граней замкнутого тела смотрят внутрь.
##
## «Наружу» берётся от ЦЕНТРА ТЕЛА, а не от нормалей самого меша: нормаль
## вывернутой коробки остаётся правдоподобной (её и рисуют светом), а вот обход
## — нет. Именно поэтому «прозрачные шпалы» и выглядели ошибкой материала.
func _outward(mesh: ArrayMesh, centre: Vector3, front: float) -> Dictionary:
	var wrong_face := 0
	var wrong_normal := 0
	var faces := 0
	if mesh == null:
		return {"faces": 0, "wrong_face": 0, "wrong_normal": 0}
	for tri_raw in _faces(mesh.surface_get_arrays(0)):
		var tri: Array = tri_raw
		var v0: Vector3 = tri[0]
		var v1: Vector3 = tri[1]
		var v2: Vector3 = tri[2]
		var out := (v0 + v1 + v2) / 3.0 - centre
		var rh := (v1 - v0).cross(v2 - v0)
		faces += 1
		if signf(rh.dot(out)) != front:
			wrong_face += 1
		if (tri[3] as Vector3).dot(out) <= 0.0:
			wrong_normal += 1
	return {"faces": faces, "wrong_face": wrong_face, "wrong_normal": wrong_normal}


## _top_faces — треугольники, целиком лежащие на отметке верха.
func _top_faces(mesh: ArrayMesh, top_z: float) -> Array:
	var out: Array = []
	for tri_raw in _faces(mesh.surface_get_arrays(0)):
		var tri: Array = tri_raw
		# to_godot кладёт отметку в y, и это единственное место суиты, где
		# соглашение об осях приходится знать.
		if (absf((tri[0] as Vector3).y - top_z) < EPS_TOP_M
				and absf((tri[1] as Vector3).y - top_z) < EPS_TOP_M
				and absf((tri[2] as Vector3).y - top_z) < EPS_TOP_M):
			out.append(tri)
	return out


## _faces — треугольники массивов поверхности как [v0, v1, v2, нормаль v0].
func _faces(arrays: Array) -> Array:
	var out: Array = []
	if arrays.is_empty():
		return out
	var vs: PackedVector3Array = arrays[Mesh.ARRAY_VERTEX]
	var ns: PackedVector3Array = arrays[Mesh.ARRAY_NORMAL] if arrays[Mesh.ARRAY_NORMAL] != null else PackedVector3Array()
	var idx: PackedInt32Array = arrays[Mesh.ARRAY_INDEX] if arrays[Mesh.ARRAY_INDEX] != null else PackedInt32Array()
	if idx.is_empty():
		for k in vs.size() / 3:
			out.append([vs[k * 3], vs[k * 3 + 1], vs[k * 3 + 2],
				ns[k * 3] if ns.size() > k * 3 else Vector3.UP])
		return out
	for k in idx.size() / 3:
		var a := idx[k * 3]
		var b := idx[k * 3 + 1]
		var c := idx[k * 3 + 2]
		out.append([vs[a], vs[b], vs[c], ns[a] if ns.size() > a else Vector3.UP])
	return out


## _tris / _verts — счёт ПО ДЛИНЕ МАССИВОВ, а не через get_faces(): тот
## разворачивает геометрию в новый массив. Довод тот же, что у world.gd::
## _mesh_tris, и копия здесь нарочная — проверка не занимает у проверяемого.
func _tris(mesh: Mesh) -> int:
	var am := mesh as ArrayMesh
	if am == null:
		return 0
	var n := 0
	for s in am.get_surface_count():
		var idx := am.surface_get_array_index_len(s)
		n += (idx if idx > 0 else am.surface_get_array_len(s)) / 3
	return n


func _verts(mesh: Mesh) -> int:
	var am := mesh as ArrayMesh
	if am == null:
		return 0
	var n := 0
	for s in am.get_surface_count():
		n += am.surface_get_array_len(s)
	return n
