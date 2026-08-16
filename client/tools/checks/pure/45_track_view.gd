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
## треугольников на участок из n точек. Накат — по одной полосе на нитку,
## 4·(n−1). Коробка — шесть граней, 12 треугольников и 24 вершины (вершины не
## общие: у граней разные нормали). Разойдётся строение с кодом — разойдётся и
## число.
##
## РЕЛЬС СЧИТАЕТСЯ ПО ТОМУ, ЧТО ПРИСЛАЛ СЕРВЕР, и это не усложнение ради
## общности. С 2026-08-15 форма рельса — данные (ClearAhead-72p.8): сечение
## приезжает многоугольником, и «по четыре полосы на нитку» перестало быть
## правдой в тот же день. Число полос выводится из ЧИСЛА РЁБЕР СЕЧЕНИЯ плюс одно
## лишнее на каждое верхнее ребро — верх головки режется надвое, потому что в
## вырез ложится накат (разбор — у TrackView.rail_profile_mesh). Сечения нет —
## остаётся прежний прямоугольник в четыре полосы.
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
	_check_rail_profile(spans)
	_check_blades(network, by_id, spans)
	_check_rail_gaps(network, by_id, spans)
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
		# ПО КУСКАМ, А НЕ ПО ВСЕЙ ОСИ. У прохода стрелки нитка есть не везде — её
		# место занимают остряк с рамным рельсом и сердечник, — и каждый кусок
		# даёт свои (точек − 1) полос. Считай по всей оси, и проверка требовала бы
		# рисовать рельс там, где сервер сказал, что рельса нет.
		for sgn in [1.0, -1.0]:
			var steps := 0
			for run in sp.rail_runs(sgn):
				steps += maxi((run as Array).size() - 1, 0)
			want_body += _strips_of(sp) * 2 * steps
			want_head += 2 * steps
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
	_ok("треугольников рельса = 4·полос·(точек оси − 1) по присланному сечению",
		got_body == want_body, "%d против %d" % [got_body, want_body])
	_ok("треугольников наката = 4·(точек оси − 1)", got_head == want_head,
		"%d против %d" % [got_head, want_head])


## ПРОФИЛЬ РЕЛЬСА НА ПРЯМОМ УЧАСТКЕ: куда он сел и куда смотрит.
##
## # Почему участок здесь СВОЙ, а не взятый со станции
##
## Проверяются две вещи, у которых на кривой нет простого эталона: ПОСАДКА
## сечения на колею и НАПРАВЛЕНИЕ нормалей. На прямой ось рельса — прямая линия,
## и «наружу» у каждого треугольника считается точно, а не с оговоркой про
## доворот. Сечение и размеры при этом берутся ПРИСЛАННЫЕ, со станции: проверка
## меряет боевые данные, а не свою фикстуру.
##
## # Что именно закрепляется
##
## ПОСАДКА. Рабочая грань обязана лечь ровно на gauge/2 — это единственное, чем
## сечение связано с колеёй, и промах здесь не даёт ни отказа, ни пустого кадра:
## он даёт поехавшую колею (замер спайка: 1.335 вместо 1.435). Подошва обязана
## лечь ровно на rail.height ниже поверхности катания — иначе рельс висит над
## шпалой либо тонет в ней.
##
## ОБХОД. Вывернутый рельс проект уже терял дважды за один день (2026-08-12,
## коробки и платформа) и оба раза находил глазами владельца. У экструзии
## выворот стоит одного знака в порядке концов полосы, поэтому проверяется не
## код, а РЕЗУЛЬТАТ: нормаль каждого треугольника обязана смотреть ПРОЧЬ от оси
## самого рельса.
func _check_rail_profile(spans: Array[TrackBuild.Span]) -> void:
	var src: TrackBuild.Span = null
	for sp in spans:
		if sp.has_rail_section():
			src = sp
			break
	_ok("сервер прислал сечение рельса", src != null)
	if src == null:
		return

	# Прямая ось на десять метров, курс 0: плановый +x, отметка 100.
	var straight := TrackBuild.Span.new()
	straight.element_id = "PROBE"
	straight.gauge_m = src.gauge_m
	straight.rail_height_m = src.rail_height_m
	straight.rail_head_width_m = src.rail_head_width_m
	straight.rail_section = src.rail_section
	var axis: Array[TrackGeom.AxisPoint] = []
	for k in 3:
		axis.append(TrackGeom.AxisPoint.new(float(k) * 5.0, 0.0, 100.0, 0.0, float(k) * 5.0))
	straight.axis = axis

	var mesh := TrackView.rail_profile_mesh(straight)
	_ok("профиль построен", mesh != null)
	if mesh == null:
		return
	var arr := mesh.surface_get_arrays(0)
	var verts: PackedVector3Array = arr[Mesh.ARRAY_VERTEX]
	var norms: PackedVector3Array = arr[Mesh.ARRAY_NORMAL]
	var idx: PackedInt32Array = arr[Mesh.ARRAY_INDEX]

	# ПОСАДКА. Левая нормаль курса 0 — плановый +y, в осях движка это −Z, поэтому
	# правая нитка живёт на отрицательных z.
	#
	# РАБОЧАЯ ГРАНЬ ИЩЕТСЯ СРЕДИ ТОЧЕК НА ПОВЕРХНОСТИ КАТАНИЯ, а не среди всех:
	# подошва рельса ШИРЕ головки и заходит внутрь колеи на 37.5 мм. Первая
	# редакция этой проверки мерила ближайшую к оси точку вообще и получила
	# подошву — то есть проверяла не то, что называла.
	var near_z := -INF
	var low_y := INF
	var high_y := -INF
	for v in verts:
		low_y = minf(low_y, v.y)
		high_y = maxf(high_y, v.y)
	for v in verts:
		if v.z < 0.0 and absf(v.y - 100.0) < 1e-4 and v.z > near_z:
			near_z = v.z
	_ok("рабочая грань легла на половину колеи",
		absf(-near_z - straight.gauge_m * 0.5) < 1e-4,
		"грань на %.5f м от оси, половина колеи %.5f м" % [-near_z, straight.gauge_m * 0.5])
	_ok("поверхность катания легла на отметку оси", absf(high_y - 100.0) < 1e-4,
		"верх на %.5f при отметке 100" % high_y)
	_ok("подошва легла на объявленную высоту рельса",
		absf((100.0 - low_y) - straight.rail_height_m) < 1e-4,
		"высота меша %.5f м при rail.height %.5f м" % [100.0 - low_y, straight.rail_height_m])

	# ОБХОД. Проверяется ШАГОМ ПО НОРМАЛИ, а не углом к оси рельса.
	#
	# «Нормаль смотрит прочь от середины» — правило для ВЫПУКЛОГО тела, и на
	# двутавре оно врёт: изнанка головки смотрит ВНИЗ, а середина рельса лежит
	# ниже неё, и верная нормаль засчиталась бы вывернутой. Первая редакция этой
	# проверки на том и споткнулась: 72 треугольника «внутрь» при исправном меше.
	#
	# Настоящее правило локально: отступи от грани на миллиметр ПО нормали — и
	# окажешься СНАРУЖИ металла; отступи ПРОТИВ — окажешься внутри. Считается это
	# принадлежностью точки многоугольнику, то есть способом, к экструзии
	# отношения не имеющим, — это и делает проверку независимой.
	#
	# Миллиметр меньше самой тонкой части сечения (шейка 18 мм, подошва у кромки
	# 12 мм), поэтому шаг не проскакивает металл насквозь.
	const STEP := 1e-3
	var wrong_out := 0
	var wrong_in := 0
	var half := straight.gauge_m * 0.5
	for t in range(0, idx.size(), 3):
		var mid := (verts[idx[t]] + verts[idx[t + 1]] + verts[idx[t + 2]]) / 3.0
		var n := (norms[idx[t]] + norms[idx[t + 1]] + norms[idx[t + 2]]) / 3.0
		# Обратный перевод в оси СЕЧЕНИЯ. У правой нитки (z < 0) сечение растёт
		# в сторону −z, у левой — в сторону +z.
		var right := mid.z < 0.0
		var sx := (-mid.z - half) if right else (mid.z - half)
		var sy := mid.y - 100.0
		var nx := (-n.z) if right else n.z
		var here := Vector2(sx, sy)
		var step := Vector2(nx, n.y).normalized() * STEP
		if _inside_section(straight.rail_section, here + step):
			wrong_out += 1
		if not _inside_section(straight.rail_section, here - step):
			wrong_in += 1
	_ok("шаг по нормали рельса выводит наружу металла", wrong_out == 0,
		"граней с нормалью внутрь: %d" % wrong_out)
	_ok("шаг против нормали рельса ведёт внутрь металла", wrong_in == 0,
		"граней, у которых металла за спиной нет: %d" % wrong_in)


## _inside_section — лежит ли точка внутри сечения. Обычный счёт пересечений
## луча: способ, ничего не знающий об экструзии, и потому годный ей в судьи.
func _inside_section(poly: PackedVector2Array, p: Vector2) -> bool:
	var inside := false
	var m := poly.size()
	for i in m:
		var a := poly[i]
		var b := poly[(i + 1) % m]
		if (a.y > p.y) == (b.y > p.y):
			continue
		var x := a.x + (p.y - a.y) / (b.y - a.y) * (b.x - a.x)
		if x > p.x:
			inside = not inside
	return inside


## ОСТРЯК: своя ли это деталь, куда она отходит и не лезет ли в рамный рельс.
##
## # Зачем проверка, если положение уже видно на указателе
##
## Затем, что до 2026-08-15 перевод стрелки не менял на путях НИЧЕГО: игрок жал
## клавишу, панель меняла слово, указатель поворачивался, а рельсы стояли
## (ClearAhead-86mb, слово владельца). Указатель — рассказ о состоянии, остряк —
## само состояние, и проверять надо второе.
##
## # Что закрепляется
##
## ОСТРЯК ЛЕЖИТ ВНУТРИ КОЛЕИ. Рабочая грань его — там же, где у нитки, а тело
## растёт к оси. До 2026-08-16 остряк был подвижным участком самой нитки, то
## есть прижатый занимал объём рамного рельса, а рамного рельса не было вовсе.
## Это утверждение — то, чем новая модель отличается от старой, и оно первое.
## ОТВОД РАВЕН ПРИСЛАННОМУ ХОДУ. Не «остряк сдвинулся», а сдвинулся ровно на
## switch.throw: половина хода — это стрелка, по которой колесо пойдёт куда
## попало.
## ОТВОД СХОДИТ НА НЕТ К КОРНЮ: у корня остряк лежит на своей нитке, иначе с
## перевода уехала бы колея.
## НИТКИ ПРОХОДА НЕПОДВИЖНЫ. Рамный рельс не ездит вслед за остряком — теперь
## это свойство показа, а не совпадение: подвижных ниток у участка не осталось.
func _check_blades(network: Dictionary, by_id: Dictionary,
		spans: Array[TrackBuild.Span]) -> void:
	var bl := TrackBuild.blades(network, by_id, CheckContext.MAX_SEG_M, CheckContext.MAX_ANG_RAD)
	var blades: Array[TrackBuild.Blade] = bl["list"]
	_ok("сервер прислал остряки", not blades.is_empty(),
		"остряков %d, отвергнуто %d" % [blades.size(), (bl["skipped"] as Array).size()])
	if blades.is_empty():
		return
	var blade: TrackBuild.Blade = blades[0]

	# Тело строится в обоих положениях, и строение у него одно: меняется только
	# вынос рабочей грани, а число вершин — нет.
	blade.set_open(0.0)
	var shut := _vertices(TrackView.frog_rail_mesh(blade))
	blade.set_open(1.0)
	var open := _vertices(TrackView.frog_rail_mesh(blade))
	_ok("остряк строится в обоих положениях",
		not shut.is_empty() and shut.size() == open.size(),
		"прижат %d вершин, отведён %d" % [shut.size(), open.size()])
	if shut.is_empty() or shut.size() != open.size():
		return

	# НИЖЕ РАМНОГО РЕЛЬСА. Тело остряка растёт НАРУЖУ, как у всякого рельса, и
	# потому в плане накрывает место рамного. Не мешают они друг другу по ВЫСОТЕ:
	# остряк катают из острякового ОР65, который ниже путевого Р65, и кладут на
	# стрелочную подушку ровно в эту разность.
	#
	# Здесь стояла обратная проверка — «остряк лежит внутри колеи, а не в рамном
	# рельсе» — и требовала роста к оси. Требование было верно для той модели, в
	# которой остряк строился масштабированием путевого Р65, и неверно по
	# существу: замер показал проникновение в рамный рельс при ОБЕИХ сторонах
	# роста (52 мм к оси, 100 мм наружу). Неверна была не сторона, а профиль.
	#
	# Проверяется поэтому то, что и держит развязку: остряк обязан быть НИЖЕ.
	var deepest := 0.0
	for pt in blade.rail_section:
		deepest = minf(deepest, pt.y)
	# Высота ПУТЕВОГО рельса берётся у любого участка: тип пути на станции один,
	# а вопрос стоит про разность профилей, а не про конкретный прогон.
	var track_h := 0.0
	for sp in spans:
		track_h = maxf(track_h, sp.rail_height_m)
	_ok("остряк ниже путевого рельса — есть куда лечь подушке",
		blade.rail_height_m > 0.0 and track_h > 0.0
			and blade.rail_height_m < track_h - 1e-9,
		"высота остряка %.3f м, путевого %.3f м, низ сечения %.3f м"
			% [blade.rail_height_m, track_h, deepest])
	_ok("тело остряка растёт наружу, как у всякого рельса",
		signf(blade.grow) == signf(blade.offset_m),
		"вынос %+.3f, сторона роста %+.0f" % [blade.offset_m, blade.grow])

	# ОТВОД. Считается по граням, а не по вершинам: грань и есть то, чем остряк
	# прижимается, и она же уезжает на ход.
	blade.set_open(1.0)
	var at_toe := absf(blade.faces[0] - blade.offset_m)
	var at_root := absf(blade.faces[blade.faces.size() - 1] - blade.offset_m)
	var max_shift := 0.0
	for f in blade.faces:
		max_shift = maxf(max_shift, absf(f - blade.offset_m))
	_ok("в острие остряк отведён ровно на присланный ход",
		absf(at_toe - blade.throw_m) < 1e-6,
		"отвод %.5f м при switch.throw %.5f м" % [at_toe, blade.throw_m])
	_ok("больше хода остряк никуда не уходит", max_shift <= blade.throw_m + 1e-6,
		"наибольший отвод %.6f м при ходе %.6f м" % [max_shift, blade.throw_m])
	_ok("у корня остряк лежит на своей нитке", at_root < 1e-6,
		"отвод у корня %.6f м" % at_root)
	# ОТВЕДЁННЫЙ УХОДИТ К ОСИ СВОЕГО ПРОХОДА, а не от неё: уйди он наружу, колесо
	# на переводе пошло бы в зазор между остряком и рамным рельсом с той стороны,
	# с которой его никто не ждёт.
	_ok("отведённый остряк уходит к оси прохода",
		absf(blade.faces[0]) < absf(blade.offset_m),
		"грань в острие %+.4f при нитке %+.4f" % [blade.faces[0], blade.offset_m])
	blade.set_open(0.0)

	# НИТКИ ПРОХОДА НЕПОДВИЖНЫ. Проверяется на том же проходе, на котором лежит
	# остряк: его участок — единственное место, где нитка когда-либо ездила.
	var host: TrackBuild.Span = null
	for sp in spans:
		if sp.element_id == blade.element_id and sp.has_rail_body():
			host = sp
			break
	if host == null:
		return
	var before := _vertices(TrackView.rail_body_mesh(host))
	blade.set_open(1.0)
	var after := _vertices(TrackView.rail_body_mesh(host))
	blade.set_open(0.0)
	_ok("нитки прохода не ездят вслед за остряком", before == after,
		"вершин %d, перевод их не тронул" % before.size())


## РАЗРЫВЫ НИТОК: есть ли рельс там, где сервер сказал, что его нет.
##
## # Зачем это отдельной проверкой
##
## Разрыв — единственное, чем выражено, что у стрелки не четыре нитки, а три.
## Пропусти его показ, и в кадре вернётся ровно то, ради чего всё переделано:
## наружная нитка бокового прохода, лежащая внутри прижатого остряка. Замер, с
## которого началась работа: оси проходов расходятся как u²/2R, и при R = 300 м
## и головке 0.075 м первые 6.7 метра два рельса занимали один объём.
##
## Проверяется НЕ ЧИСЛО РАЗРЫВОВ, а результат: попала ли хоть одна вершина
## рельса внутрь отменённого участка. Число сказало бы лишь то, что разбор
## случился.
func _check_rail_gaps(network: Dictionary, by_id: Dictionary,
		spans: Array[TrackBuild.Span]) -> void:
	var gaps := TrackBuild.rail_gaps_by_element(network)
	_ok("сервер прислал разрывы ниток", not gaps.is_empty(),
		"элементов с разрывами: %d" % gaps.size())
	if gaps.is_empty():
		return
	var checked := 0
	var inside := 0
	var worst := ""
	for sp in spans:
		if not gaps.has(sp.element_id):
			continue
		var by_side: Dictionary = gaps[sp.element_id]
		for sgn_raw in by_side:
			var sgn := float(sgn_raw)
			checked += 1
			for c_raw in (by_side[sgn_raw] as Array):
				var cut: Vector2 = c_raw
				for run in sp.rail_runs(sgn):
					for p_raw in run:
						var p: TrackGeom.AxisPoint = p_raw
						# Края разрыва — законные точки: кусок нитки кончается ровно
						# на них. Внутрь же не должна попасть ни одна.
						if p.u > cut.x + 1e-6 and p.u < cut.y - 1e-6:
							inside += 1
							worst = "%s: точка u=%.3f внутри разрыва [%.3f, %.3f]" % [
								sp.element_id, p.u, cut.x, cut.y]
	_ok("нитки нет там, где сервер её отменил", inside == 0,
		"проверено ниток %d, точек внутри разрыва %d%s" % [
			checked, inside, "" if worst == "" else " — " + worst])
	# ЦЕЛОЕ НЕ ПОТЕРЯНО. Разрыв обязан отнять ровно свою длину, а не весь участок:
	# ошибка в нарезке (например, разрывы, не слитые перед вычитанием) съела бы
	# нитку целиком, и проверка выше этого не заметила бы — точек внутри разрыва
	# у пустой нитки нет.
	var lost := ""
	for sp in spans:
		if not gaps.has(sp.element_id) or sp.axis.is_empty():
			continue
		var by_side: Dictionary = gaps[sp.element_id]
		for sgn_raw in by_side:
			var sgn := float(sgn_raw)
			var cut_len := 0.0
			for c_raw in (by_side[sgn_raw] as Array):
				var cut: Vector2 = c_raw
				cut_len += cut.y - cut.x
			var kept := 0.0
			for run in sp.rail_runs(sgn):
				var pts: Array = run
				if pts.size() >= 2:
					kept += float(pts[pts.size() - 1].u) - float(pts[0].u)
			var whole: float = float(sp.axis[sp.axis.size() - 1].u) - float(sp.axis[0].u)
			if absf(kept - (whole - cut_len)) > 1e-3:
				lost = "%s, сторона %+.0f: осталось %.3f м из %.3f при разрывах %.3f м" % [
					sp.element_id, sgn, kept, whole, cut_len]
	_ok("разрыв отнимает ровно свою длину", lost == "", lost)


## _vertices — вершины меша в порядке построения. Порядок и есть то, чем
## сравниваются два положения остряка: строение у них одно, меняется только
## поперечная координата подвижной нитки.
func _vertices(mesh: Mesh) -> PackedVector3Array:
	if mesh == null:
		return PackedVector3Array()
	return mesh.surface_get_arrays(0)[Mesh.ARRAY_VERTEX]


## _strips_of — сколько ПОЛОС даёт одна нитка участка.
##
## Считается из присланного сечения, а не из кода TrackView: обойди сечение — и
## каждое ребро даст полосу, кроме верхних, где вырезан накат и полос выходит
## две. Сечения нет — прежний прямоугольник: верх режется на три, из них накат
## забирает середину, плюс два бока и низ.
func _strips_of(sp: TrackBuild.Span) -> int:
	if not sp.has_rail_section():
		return 4
	var sec := sp.rail_section
	var m := sec.size()
	var strips := 0
	for i in m:
		var a := sec[i]
		var b := sec[(i + 1) % m]
		# Верхнее ребро — то, оба конца которого лежат на поверхности катания.
		# Порог тот же, что у показа, и он назван там же.
		if absf(a.y) <= TrackView.TOP_EPS and absf(b.y) <= TrackView.TOP_EPS:
			strips += 2
		else:
			strips += 1
	return strips


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


## КРЕСТОВИНА. Раньше здесь считались треугольники ГАЛОЧКИ — плоской метки
## теоретической точки. Метка снята (ClearAhead-byoq): в виде от первого лица она
## читалась красным пятном на шпалах, а не крестовиной. Считаются нитки.
##
## # Что закрепляется
##
## ЧИСЛО НИТОК: четыре на стрелку — два усовика и два контррельса. Три означало
## бы, что одна пара приехала, а другая потерялась по дороге, и в кадре это
## выглядело бы исправной крестовиной с одной стороны.
##
## ОТГИБ РАСКРЫВАЕТ ЖЕЛОБ: у конца нитки вынос рабочей грани дальше от соседней
## нитки, чем в середине. Считается по ПРИСЛАННОМУ выносу, а не по коду разбора.
##
## ТРЕУГОЛЬНИКИ: полоса на каждое ребро сечения, две на полосу. Верх головки
## здесь НЕ режется — наката на нитке крестовины нет, — и в этом отличие от
## рельса участка.
func _check_frogs(network: Dictionary, by_id: Dictionary) -> void:
	var res := TrackBuild.frog_rails(network, by_id, CheckContext.MAX_SEG_M, CheckContext.MAX_ANG_RAD)
	var rails: Array[TrackBuild.FrogRail] = res["list"]
	_ok("нитки крестовин разобраны", not rails.is_empty(),
		"%d, пропущено: %s" % [rails.size(), str(res["skipped"])])
	if rails.is_empty():
		return
	var by_owner := {}
	var kinds := {}
	for r in rails:
		by_owner[r.owner] = int(by_owner.get(r.owner, 0)) + 1
		kinds[r.kind] = int(kinds.get(r.kind, 0)) + 1
	# ШЕСТЬ ЗАПИСЕЙ НА СТРЕЛКУ: два усовика, два контррельса и две ГРАНИ
	# СЕРДЕЧНИКА. Грани — не нитки: по ним строится не рельс, а тело отливки
	# между ними.
	var six := true
	for o in by_owner:
		if int(by_owner[o]) != 6:
			six = false
	_ok("у каждой стрелки шесть записей крестовины", six, str(by_owner))
	_ok("приехали все три пары: усовики, контррельсы, грани сердечника",
		int(kinds.get(TrackBuild.FROG_WING, 0)) == int(kinds.get(TrackBuild.FROG_CHECK, 0))
		and int(kinds.get(TrackBuild.FROG_WING, 0)) == int(kinds.get(TrackBuild.FROG_CASTING, 0))
		and int(kinds.get(TrackBuild.FROG_WING, 0)) > 0, str(kinds))

	# ОТГИБ ЕСТЬ У НИТОК И НЕТ У ОТЛИВКИ. У усовика и контррельса конец
	# раскрывается раструбом, у сердечника раструба нет вовсе: он сплошной.
	var flared := 0
	var straightened := 0
	for r in rails:
		var mid: float = r.faces[r.faces.size() / 2]
		var end: float = r.faces[0]
		var bent := absf(end - mid) > 1e-6
		if r.kind == TrackBuild.FROG_CASTING:
			if bent:
				straightened += 1
			continue
		if bent:
			flared += 1
		else:
			straightened += 1
	_ok("отгиб у каждой нитки и ни одного у отливки", straightened == 0,
		"с отгибом %d, лишних %d" % [flared, straightened])

	# ТЕЛО СЕРДЕЧНИКА строится между двумя гранями одной стрелки.
	var by_owner_cast := {}
	for r in rails:
		if r.kind != TrackBuild.FROG_CASTING:
			continue
		if not by_owner_cast.has(r.owner):
			by_owner_cast[r.owner] = []
		(by_owner_cast[r.owner] as Array).append(r)
	var cast_built := 0
	for o in by_owner_cast:
		var pair: Array = by_owner_cast[o]
		if pair.size() != 2:
			continue
		if TrackView.frog_casting_mesh(pair[0], pair[1]) != null:
			cast_built += 1
	_ok("сердечник построен у каждой стрелки", cast_built == by_owner_cast.size(),
		"%d из %d" % [cast_built, by_owner_cast.size()])

	# Число полос выводится из СТРОЕНИЯ, как и у рельса участка: по полосе на
	# ребро сечения плюс лишняя на каждое верхнее ребро У ХОДОВОЙ нитки — там верх
	# режется под накат. У контррельса наката нет, и лишней полосы тоже.
	var want := 0
	var got := 0
	var empty: Array[String] = []
	for r in rails:
		var strips := 0
		var rides := r.kind == TrackBuild.FROG_WING
		for i in r.rail_section.size():
			var a: Vector2 = r.rail_section[i]
			var b: Vector2 = r.rail_section[(i + 1) % r.rail_section.size()]
			if rides and absf(a.y) <= TrackView.TOP_EPS and absf(b.y) <= TrackView.TOP_EPS:
				strips += 2
			else:
				strips += 1
		want += 2 * strips * (r.axis.size() - 1)
		var mesh := TrackView.frog_rail_mesh(r)
		if mesh == null:
			empty.append("%s@%s" % [r.element_id, r.kind])
			continue
		got += _tris(mesh)
	_ok("нитка построена у каждой присланной", empty.is_empty(), str(empty))
	_ok("треугольников нитки = 2·полос·(точек оси − 1)", got == want,
		"%d против %d" % [got, want])


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
	var bare_frog := TrackBuild.FrogRail.new()
	_ok("нитка крестовины без рельса отвергнута", TrackView.frog_rail_mesh(bare_frog) == null)

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
