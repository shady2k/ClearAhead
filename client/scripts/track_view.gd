## TrackView — путь на экране.
##
## Сюда приходит уже РАЗМЕЩЁННОЕ (TrackBuild) и превращается в меши. Ни одного
## решения о мире здесь нет: только про то, из каких треугольников это сложить.
##
## Два вида осевой отрисовки, и выбор между ними ОПРЕДЕЛЯЕТСЯ ДАННЫМИ, а не
## вкусом:
##
##   • ЛЕНТА шириной 2 × ballast.half_width — на участке, покрытом строительным
##     прогоном (construction_runs), у типа которого эта ширина есть;
##   • НИТЬ в один экранный пиксель — там, где такой связи нет. Ширины не
##     прислали, и лента любой ширины была бы выдумкой. Тонкая линия — это
##     видимое «размер неизвестен», а не украшение.
##
## Так на затравке ST_A ветви стрелок (SW1:*, SW2:*) остаются нитью: ни один
## construction_run их не покрывает. Это не дефект клиента, а состояние данных.
##
## ПУТЬ СТАЛ ТЕЛОМ 2026-08-12. До того здесь всё было плоским и на отметке оси —
## не по лени, а потому что контракт не говорил, от чего эта отметка считается.
## Редакция 6 §2 назвала её ПОВЕРХНОСТЬЮ КАТАНИЯ, и вертикаль стала считаемой.
##
## Глубина: раньше ось пути лежала ровно на обработанной поверхности рельефа
## (сервер сажал землю на отметку оси), всё дралось за z-буфер, и лечилось это
## отключением ПРОВЕРКИ ГЛУБИНЫ. Теперь земля лежит на `formation_to_rail_top`
## ниже — на затравке это 0.68 м, — и телам драться не с чем: они занимают ровно
## тот объём, который раньше был щелью. Поэтому solid_material проверку глубины
## НЕ отключает.
##
## Отключённой она осталась у ЛИНИЙ — нитей и галочек крестовин: те по-прежнему
## лежат на отметке оси, потому что у них нет толщины ни в каком смысле, и
## поднимать их некуда.
##
## Порядок слоёв (render_priority) сохранён для плоских слоёв; телам он не нужен
## и не назначается — их разводит сама геометрия.
class_name TrackView
extends RefCounted

## Порядок слоёв. Числа экранные: что поверх чего, а не что выше чего.
const PRIO_BALLAST := 1
const PRIO_PLATFORM := 2
const PRIO_SLEEPER := 3
const PRIO_RAIL := 4
const PRIO_LINE := 5
const PRIO_FROG := 6


## prism_mesh — балластная призма ТЕЛОМ: трапеция, протянутая вдоль оси.
##
## Все пять чисел приезжают с сервера, ни одно не выведено:
##
##   верх призмы     z − rail_height − sleeper_height + crib_depth
##   низ призмы      z − formation_to_rail_top
##   полуширина верха   ballast.half_width
##   полуширина низа    half_width + side_slope · высота призмы
##
## Заложение откоса — «метров по горизонтали на метр по вертикали», ровно как у
## земляных работ: одна единица на два откоса в одной карте заведена нарочно
## (контракт §3), потому что две единицы у одноимённых величин — заготовленная
## ошибка.
##
## Строится четырьмя полосами (верх, два откоса, низ), а не замкнутым объёмом с
## торцами: торцы на стыке соседних участков смотрели бы внутрь друг друга, и
## их пришлось бы гасить по признаку смежности, которого у клиента нет. Цена
## названа: у одиночного участка призма открыта с концов, и в виде вплотную это
## видно.
static func prism_mesh(span: TrackBuild.Span) -> ArrayMesh:
	if not span.has_prism() or span.axis.size() < 2:
		return null
	var n := span.axis.size()
	var top_hw := span.ballast_half_width_m
	var prism_h := span.ballast_depth_m + span.ballast_crib_depth_m
	var bot_hw := top_hw + span.ballast_side_slope * prism_h

	var top_l := PackedVector3Array(); top_l.resize(n)
	var top_r := PackedVector3Array(); top_r.resize(n)
	var bot_l := PackedVector3Array(); bot_l.resize(n)
	var bot_r := PackedVector3Array(); bot_r.resize(n)
	for k in n:
		var p: TrackGeom.AxisPoint = span.axis[k]
		var nl := p.left()
		var z_top := p.z - span.rail_height_m - span.sleeper_height_m + span.ballast_crib_depth_m
		var z_bot := p.z - span.formation_to_rail_top_m
		top_l[k] = TerrainMesh.to_godot(p.x + nl.x * top_hw, p.y + nl.y * top_hw, z_top)
		top_r[k] = TerrainMesh.to_godot(p.x - nl.x * top_hw, p.y - nl.y * top_hw, z_top)
		bot_l[k] = TerrainMesh.to_godot(p.x + nl.x * bot_hw, p.y + nl.y * bot_hw, z_bot)
		bot_r[k] = TerrainMesh.to_godot(p.x - nl.x * bot_hw, p.y - nl.y * bot_hw, z_bot)

	var verts := PackedVector3Array()
	var norms := PackedVector3Array()
	var idx := PackedInt32Array()
	_quad_strip(verts, norms, idx, top_r, top_l)
	_quad_strip(verts, norms, idx, top_l, bot_l)
	_quad_strip(verts, norms, idx, bot_r, top_r)
	_quad_strip(verts, norms, idx, bot_l, bot_r)
	return _mesh(verts, norms, idx)


## ribbon_mesh — лента постоянной полуширины вдоль оси.
static func ribbon_mesh(axis: Array[TrackGeom.AxisPoint], half_width_m: float) -> ArrayMesh:
	if axis.size() < 2 or half_width_m <= 0.0:
		return null

	var verts := PackedVector3Array()
	var norms := PackedVector3Array()
	verts.resize(axis.size() * 2)
	norms.resize(axis.size() * 2)

	for k in axis.size():
		var p: TrackGeom.AxisPoint = axis[k]
		# Ширина откладывается по нормали в плане; высота у обоих краёв та же,
		# что у оси: поперечный уклон отсыпки серверу неизвестен, и придумать его
		# значило бы повторить историю с восемью промилле продольного профиля.
		var n := p.left()
		verts[k * 2] = TerrainMesh.to_godot(p.x + n.x * half_width_m, p.y + n.y * half_width_m, p.z)
		verts[k * 2 + 1] = TerrainMesh.to_godot(p.x - n.x * half_width_m, p.y - n.y * half_width_m, p.z)
		norms[k * 2] = Vector3.UP
		norms[k * 2 + 1] = Vector3.UP

	return _strip(verts, norms)


## strip_mesh — полоса между двумя присланными кромками.
##
## Отдельно от ribbon_mesh: у платформы кромки НЕ симметричны относительно оси
## (offset и offset + width с одной стороны), и подгонять её под ленту значило бы
## считать её шириной вдвое большей, чем прислано.
static func strip_mesh(near: PackedVector3Array, far: PackedVector3Array) -> ArrayMesh:
	var n := mini(near.size(), far.size())
	if n < 2:
		return null
	var verts := PackedVector3Array()
	var norms := PackedVector3Array()
	verts.resize(n * 2)
	norms.resize(n * 2)
	for k in n:
		verts[k * 2] = TerrainMesh.to_godot(near[k].x, near[k].y, near[k].z)
		verts[k * 2 + 1] = TerrainMesh.to_godot(far[k].x, far[k].y, far[k].z)
		norms[k * 2] = Vector3.UP
		norms[k * 2 + 1] = Vector3.UP
	return _strip(verts, norms)


## sleeper_mesh — вся решётка одним мешем.
##
## Одним, а не 768 узлами: шпала не имеет доменной идентичности (render-contract
## §4 — «порядковый номер n не является доменной идентичностью»), выделять ей
## узел сцены нечем и незачем. Прямоугольник плоский: длина поперёк пути,
## ширина вдоль, якорь в геометрическом центре — ровно как задано правилом
## ориентации в §4.
static func sleeper_mesh(list: Array[TrackBuild.Sleeper]) -> ArrayMesh:
	if list.is_empty():
		return null
	var verts := PackedVector3Array()
	var norms := PackedVector3Array()
	var idx := PackedInt32Array()
	for s_raw in list:
		var s: TrackBuild.Sleeper = s_raw
		var p := s.pose
		# Шпала — коробка, если высоту прислали, и плоский прямоугольник, если
		# нет. Второй случай остаётся не для красоты: карта без sleeper.height
		# сегодня отвергается валидатором, но ответ СТАРОГО сервера её не несёт,
		# и клиент обязан показать решётку без толщины, а не исчезнуть.
		_box(verts, norms, idx, p.x, p.y, p.forward(),
			s.width_m * 0.5, s.length_m * 0.5, s.bottom_z(), s.top_z())
	return _mesh(verts, norms, idx)


## rail_body_mesh — рельсы телом.
##
## # Две ветки, и обе объявлены
##
## ПРИСЛАНО СЕЧЕНИЕ (`rail.section`) — рельс строится экструзией присланного
## многоугольника: головка, шейка, подошва. Ни одного числа формы клиент при
## этом не знает и не заводит.
##
## СЕЧЕНИЯ НЕТ — прежнее ОБЪЯВЛЕННОЕ УПРОЩЕНИЕ: прямоугольник
## `head_width × rail.height`, внутренней гранью на ±gauge/2. Оно не выдаётся за
## профиль ни здесь, ни на сервере; карта без сечения законна (валидатор
## пропускает её нарочно), и клиент, подставивший бы сюда свой Р65, выдумал бы
## факт о мире.
##
## Ширина головки нужна ОБЕИМ веткам, и заведена она по условию: gauge задан
## между ВНУТРЕННИМИ РАБОЧИМИ ГРАНЯМИ, и без неё рельс некуда поставить, не
## выдумав. Цена умолчания измерена на снесённом спайке: нитки поставили по осям
## головок вместо рабочих граней, и колея вышла 1.335 вместо 1.435.
##
## У упрощения рисуются три грани — верх и два бока. Низ не рисуется: он лежит на
## шпале и не виден ниоткуда, а треугольники стоят денег.
static func rail_body_mesh(span: TrackBuild.Span) -> ArrayMesh:
	if span.has_rail_section():
		return rail_profile_mesh(span)
	if not span.has_rail_body() or span.axis.size() < 2:
		return null
	var n := span.axis.size()
	var inner := span.gauge_m * 0.5
	var outer := inner + span.rail_head_width_m
	# Вырез под накат ВМЕСТЕ С ФАСКАМИ: их рисуют соседние меши, и тело обязано
	# уступить им всю полосу разом.
	var bands := head_bands(span.rail_head_width_m)
	var head_i := inner + bands.x
	var head_o := inner + bands.w
	var verts := PackedVector3Array()
	var norms := PackedVector3Array()
	var idx := PackedInt32Array()

	var sides: Array[float] = [1.0, -1.0]
	for sgn in sides:
		# КУСКАМИ, А НЕ ЦЕЛИКОМ: у прохода стрелки нитка есть не везде — её место
		# занимают остряк с рамным рельсом и сердечник крестовины. Где именно,
		# сказал сервер (turnout_rail_gaps); у обычного пути кусок один — вся ось.
		for run in span.rail_runs(sgn):
			var top_i := _rail_chain(span, run, sgn, inner, true)
			var top_hi := _rail_chain(span, run, sgn, head_i, true)
			var top_ho := _rail_chain(span, run, sgn, head_o, true)
			var top_o := _rail_chain(span, run, sgn, outer, true)
			var bot_i := _rail_chain(span, run, sgn, inner, false)
			var bot_o := _rail_chain(span, run, sgn, outer, false)
			# Верх режется на три полосы: две ржавые кромки и середина, которую
			# забирает накат. Полосы стыкуются, а не накладываются, поэтому за
			# z-буфер здесь драться нечему.
			if sgn > 0.0:
				_quad_strip(verts, norms, idx, top_i, top_hi)
				_quad_strip(verts, norms, idx, top_ho, top_o)
				_quad_strip(verts, norms, idx, top_o, bot_o)
				_quad_strip(verts, norms, idx, bot_i, top_i)
			else:
				_quad_strip(verts, norms, idx, top_hi, top_i)
				_quad_strip(verts, norms, idx, top_o, top_ho)
				_quad_strip(verts, norms, idx, bot_o, top_o)
				_quad_strip(verts, norms, idx, top_i, bot_i)
	if idx.is_empty():
		return null
	return _mesh(verts, norms, idx)


## railhead_mesh — НАКАТ: полоса посреди головки, по которой идёт колесо.
##
## Отдельным мешем, а не вторым материалом на том же: у наката другая физика
## поверхности целиком (полированная сталь против ржавчины), и одним материалом
## это не выражается. У спайка это были отдельный SurfaceTool `_railhead` и
## отдельный коммит `_mat_railhead` — здесь ровно то же, только полосой в верхней
## грани, а не накладкой поверх неё. Почему не накладкой — ниже, у RAILHEAD_WIDTH.
static func railhead_mesh(span: TrackBuild.Span) -> ArrayMesh:
	if not span.has_rail_body() or span.axis.size() < 2:
		return null
	var inner := span.gauge_m * 0.5
	# Вырез под накат ВМЕСТЕ С ФАСКАМИ: их рисуют соседние меши, и тело обязано
	# уступить им всю полосу разом.
	var bands := head_bands(span.rail_head_width_m)
	var head_i := inner + bands.x
	var head_o := inner + bands.w
	var verts := PackedVector3Array()
	var norms := PackedVector3Array()
	var idx := PackedInt32Array()
	var sides: Array[float] = [1.0, -1.0]
	for sgn in sides:
		# Кусками — по тому же правилу, что и тело рельса: наката нет там, где нет
		# самой нитки, иначе блестящая полоса шла бы поверх сердечника.
		for run in span.rail_runs(sgn):
			var a := _rail_chain(span, run, sgn, head_i, true)
			var b := _rail_chain(span, run, sgn, head_o, true)
			if sgn > 0.0:
				_quad_strip(verts, norms, idx, a, b)
			else:
				_quad_strip(verts, norms, idx, b, a)
	if idx.is_empty():
		return null
	return _mesh(verts, norms, idx)


## rail_fillet_mesh — ФАСКИ ГОЛОВКИ: две полоски по краям наката на каждой нитке.
##
## Отдельным мешем по той же причине, по которой отделён накат: у поверхности
## своя физика, а материал у меша один. Полосы стыкуются с накатом и с телом, а
## не накладываются, — за z-буфер драться нечему.
static func rail_fillet_mesh(span: TrackBuild.Span) -> ArrayMesh:
	if not span.has_rail_body() or span.axis.size() < 2:
		return null
	var inner := span.gauge_m * 0.5
	var bands := head_bands(span.rail_head_width_m)
	var verts := PackedVector3Array()
	var norms := PackedVector3Array()
	var idx := PackedInt32Array()
	var sides: Array[float] = [1.0, -1.0]
	for sgn in sides:
		for run in span.rail_runs(sgn):
			for pair in [Vector2(bands.x, bands.y), Vector2(bands.z, bands.w)]:
				var a := _rail_chain(span, run, sgn, inner + pair.x, true)
				var b := _rail_chain(span, run, sgn, inner + pair.y, true)
				if sgn > 0.0:
					_quad_strip(verts, norms, idx, a, b)
				else:
					_quad_strip(verts, norms, idx, b, a)
	if idx.is_empty():
		return null
	return _mesh(verts, norms, idx)


## rail_profile_mesh — рельсы ЭКСТРУЗИЕЙ ПРИСЛАННОГО СЕЧЕНИЯ.
##
## # Как сечение садится на путь
##
## Сечение приходит в своих осях: x поперёк пути ОТ ВНУТРЕННЕЙ РАБОЧЕЙ ГРАНИ
## наружу, y от ПОВЕРХНОСТИ КАТАНИЯ вниз. Оба датума контрактные и уже здесь: у
## правой нитки рабочая грань лежит на +gauge/2 по левой нормали, у левой — на
## −gauge/2, а отметка оси `p.z` и ЕСТЬ поверхность катания (датум z, редакция 6
## §2). Перевод поэтому в одну строку на точку и без единой поправки.
##
## НАРУЖУ У КАЖДОЙ НИТКИ СВОЁ, и это то же зеркало, каким уже разведены нитки:
## сечение одно на обе, второго многоугольника нет, потому что два описания
## одного рельса разошлись бы колеёй.
##
## # Чего здесь нет
##
## Торцов. Рельс уходит за границу участка и стыкуется со следующим; закрыв его
## крышкой, мы получили бы стык, видимый изнутри на каждом шве, — а нижняя грань
## не рисовалась и у прямоугольника по той же причине, по которой её не видно.
##
## Боковая поверхность строится ПО ОБХОДУ сечения, включая замыкающее ребро
## «последняя точка → первая»: сечение замкнуто по построению контракта, и
## пропустить это ребро значило бы оставить рельс со щелью во всю длину пути.
static func rail_profile_mesh(span: TrackBuild.Span) -> ArrayMesh:
	if not span.has_rail_section() or span.axis.size() < 2:
		return null
	var sec := span.rail_section
	var m := sec.size()
	var verts := PackedVector3Array()
	var norms := PackedVector3Array()
	var idx := PackedInt32Array()
	var sides: Array[float] = [1.0, -1.0]
	for sgn in sides:
		# Кусками — разбор у rail_body_mesh: нитки нет там, где её место занято
		# остряком с рамным рельсом либо сердечником.
		for run in span.rail_runs(sgn):
			_profile_run(span, run, sgn, sec, verts, norms, idx)
	if idx.is_empty():
		return null
	return _mesh(verts, norms, idx)


## _profile_run — экструзия сечения по одному непрерывному куску нитки.
##
## Отдельной функцией, а не третьим вложенным циклом: внутри и так два — по
## вершинам сечения и по его рёбрам, — и третий сделал бы правило «где режется
## верх головки» неразличимым в лесенке отступов.
static func _profile_run(span: TrackBuild.Span, run: Array, sgn: float,
		sec: PackedVector2Array, verts: PackedVector3Array,
		norms: PackedVector3Array, idx: PackedInt32Array) -> void:
	var m := sec.size()
	# Цепочка на каждую вершину сечения считается ОДИН РАЗ и переиспользуется
	# соседним ребром: посчитай её дважды, и полосы разойдутся на округлении.
	var chains: Array[PackedVector3Array] = []
	for i in m:
		chains.append(_section_chain(span, run, sgn, sec[i]))
	var bands := head_bands(span.rail_head_width_m)
	for i in m:
		var j := (i + 1) % m
		var a := sec[i]
		var b := sec[j]
		# ВЕРХ ГОЛОВКИ РЕЖЕТСЯ, а накат кладётся в вырез. Не украшение: `z`
		# элемента объявлен ПОВЕРХНОСТЬЮ КАТАНИЯ (редакция 6 §2), поэтому
		# накат лежит В уровне головки, а не накладкой поверх неё (разбор — у
		# RAILHEAD_WIDTH). Затяни здесь верх сплошной полосой — и накат стал
		# бы дракой за z-буфер во всю длину пути.
		if absf(a.y) <= TOP_EPS and absf(b.y) <= TOP_EPS:
			for seg in _top_gaps(a.x, b.x, bands.x, bands.w):
				var c0 := _section_chain(span, run, sgn, Vector2(seg.x, 0.0))
				var c1 := _section_chain(span, run, sgn, Vector2(seg.y, 0.0))
				if sgn > 0.0:
					_quad_strip(verts, norms, idx, c1, c0)
				else:
					_quad_strip(verts, norms, idx, c0, c1)
			continue
		# ПРАВАЯ НИТКА ОБХОДИТСЯ В ОБРАТНУЮ СТОРОНУ, и это не подгонка знака.
		#
		# Сечение объявлено против часовой стрелки в СВОИХ осях (x наружу, y
		# вверх). У правой нитки перевод в оси движка ЗЕРКАЛЬНЫЙ: x сечения
		# растёт в сторону −Z, y — в сторону +Y, и обход, оставленный тем же,
		# становится в мире часовым. Ровно на этом рельс и вышел изнанкой в
		# первой редакции — все 104 треугольника нормалями внутрь металла.
		#
		# У левой нитки зеркала нет (x сечения растёт в сторону +Z), и обход
		# сохраняется. Тем же зеркалом уже разведены и нитки прямоугольника
		# выше — там оно записано порядком аргументов, здесь знаком.
		if sgn > 0.0:
			_quad_strip(verts, norms, idx, chains[j], chains[i])
		else:
			_quad_strip(verts, norms, idx, chains[i], chains[j])


## frog_rail_mesh — усовик или контррельс: то же сечение вдоль присланной линии.
##
## # Почему это НЕ ветка rail_profile_mesh
##
## У той две нитки, симметричные относительно оси, и вынос у неё постоянный.
## Здесь ОДНА нитка, вынос у неё СВОЙ В КАЖДОЙ ТОЧКЕ (отгиб к раструбу), а
## сторона, в которую растёт сечение, прислана сервером. Общего между ними —
## одна строка про сечение; свести их значило бы завести функцию с тремя флагами.
##
## # Накат: у усовика он ЕСТЬ, у контррельса нет (2026-08-16)
##
## Сперва наката не было ни у той, ни у другой нитки, и это было объявлено
## упрощением. Цена вскрылась в кадре: крестовина вышла КОРИЧНЕВЫМ ПЯТНОМ среди
## блестящих ниток — «почему она коричневая?». По усовику колесо катится, и след
## на нём такой же, как на любой ходовой нитке; по контррельсу не катится вовсе.
##
## Поэтому верхнее ребро сечения режется под накат ровно так же, как у рельса
## участка, — но только у тех ниток, которые ходовые. Кто ходовая, говорит вид,
## присланный сервером.
##
## Торцов нет по той же причине, что у рельса участка: нитка кончается стыком со
## следующей, а крышка была бы видна изнутри.
static func frog_rail_mesh(rail: TrackBuild.FrogRail) -> ArrayMesh:
	if not rail.ready() or rail.rail_section.size() < 3:
		return null
	var sec := rail.rail_section
	var m := sec.size()
	var verts := PackedVector3Array()
	var norms := PackedVector3Array()
	var idx := PackedInt32Array()
	var chains: Array[PackedVector3Array] = []
	for i in m:
		chains.append(_frog_chain(rail, sec[i]))
	var bands := head_bands(rail.rail_head_width_m)
	var railed := _rides_on(rail.kind)
	for i in m:
		var j := (i + 1) % m
		var a := sec[i]
		var b := sec[j]
		# ВЕРХ ГОЛОВКИ РЕЖЕТСЯ ПОД НАКАТ — у ходовой нитки, разбор в шапке.
		if railed and absf(a.y) <= TOP_EPS and absf(b.y) <= TOP_EPS:
			for seg in _top_gaps(a.x, b.x, bands.x, bands.w):
				var c0 := _frog_chain(rail, Vector2(seg.x, 0.0))
				var c1 := _frog_chain(rail, Vector2(seg.y, 0.0))
				if rail.grow > 0.0:
					_quad_strip(verts, norms, idx, c1, c0)
				else:
					_quad_strip(verts, norms, idx, c0, c1)
			continue
		# Зеркало то же, что у правой нитки участка: сечение объявлено против
		# часовой в СВОИХ осях, и когда оно растёт по левой нормали (grow > 0),
		# перевод в оси движка переворачивает обход.
		if rail.grow > 0.0:
			_quad_strip(verts, norms, idx, chains[j], chains[i])
		else:
			_quad_strip(verts, norms, idx, chains[i], chains[j])
	if idx.is_empty():
		return null
	return _mesh(verts, norms, idx)


## frog_railhead_mesh — НАКАТ короткой нитки: та же полоса, что у рельса участка.
##
## Пусто у нитки, по которой колесо не катится: у контррельса следа нет и быть не
## может, а нарисованный он соврал бы про то, где идёт колесо.
static func frog_railhead_mesh(rail: TrackBuild.FrogRail) -> ArrayMesh:
	if not rail.ready() or not _rides_on(rail.kind):
		return null
	return _ride_strip(rail, 0.0)


## frog_rail_fillet_mesh — фаски короткой нитки: то же, что у рельса участка.
static func frog_rail_fillet_mesh(rail: TrackBuild.FrogRail) -> ArrayMesh:
	if not rail.ready() or not _rides_on(rail.kind):
		return null
	return _ride_strip(rail, rail.rail_head_width_m * RAILHEAD_FILLET)


## _ride_strip — полоса вдоль головки: сам накат (fillet = 0) либо две фаски по
## его краям.
##
## ШИРИНА НАКАТА СВОЯ В КАЖДОЙ ТОЧКЕ, когда её прислали: у остряка и сердечника
## колесо принимает нагрузку постепенно, и полоса растёт с нуля. Где ширина ноль,
## полоса рвётся — там наката нет вовсе.
static func _ride_strip(rail: TrackBuild.FrogRail, fillet: float) -> ArrayMesh:
	var head := rail.rail_head_width_m
	var verts := PackedVector3Array()
	var norms := PackedVector3Array()
	var idx := PackedInt32Array()
	var a := PackedVector3Array()
	var b := PackedVector3Array()
	var c := PackedVector3Array()
	var d := PackedVector3Array()
	for k in rail.axis.size():
		var ride: float = rail.ride_at(k)
		if ride < 0.0:
			ride = head * RAILHEAD_WIDTH
		# Наката нет там, где нитка опущена под головку соседней: колесо там идёт
		# по ней, а не по этой. Слово владельца: «как у этих железок внутри может
		# быть накат?»
		if ride <= 1e-4:
			_one_strip(verts, norms, idx, a, b, rail.grow)
			_one_strip(verts, norms, idx, c, d, rail.grow)
			a = PackedVector3Array(); b = PackedVector3Array()
			c = PackedVector3Array(); d = PackedVector3Array()
			continue
		var inner := (head - ride) * 0.5
		var outer := inner + ride
		if fillet <= 0.0:
			a.append(_frog_point(rail, k, inner))
			b.append(_frog_point(rail, k, outer))
		else:
			a.append(_frog_point(rail, k, maxf(inner - fillet, 0.0)))
			b.append(_frog_point(rail, k, inner))
			c.append(_frog_point(rail, k, outer))
			d.append(_frog_point(rail, k, minf(outer + fillet, head)))
	_one_strip(verts, norms, idx, a, b, rail.grow)
	_one_strip(verts, norms, idx, c, d, rail.grow)
	if idx.is_empty():
		return null
	return _mesh(verts, norms, idx)


## _frog_point — точка на верху нитки крестовины, на выносе x от рабочей грани.
static func _frog_point(rail: TrackBuild.FrogRail, k: int, x: float) -> Vector3:
	var p: TrackGeom.AxisPoint = rail.axis[k]
	var nl := p.left()
	var off: float = rail.faces[k] + rail.grow * x * rail.width_at(k)
	return TerrainMesh.to_godot(p.x + nl.x * off, p.y + nl.y * off, p.z - rail.sink_at(k))


## _one_strip — полоса с обходом по стороне роста. Зеркало то же, что у нитки.
static func _one_strip(verts: PackedVector3Array, norms: PackedVector3Array,
		idx: PackedInt32Array, a: PackedVector3Array, b: PackedVector3Array,
		grow: float) -> void:
	if a.size() < 2:
		return
	if grow > 0.0:
		_quad_strip(verts, norms, idx, a, b)
	else:
		_quad_strip(verts, norms, idx, b, a)


## _rides_on — катится ли колесо по этой нитке.
##
## Усовик — ходовая: он и есть нитка прохода, отведённая перед сердечником.
## Остряк — тоже: по нему колесо идёт на свой путь. Контррельс — нет: он держит
## гребень, а колесо по нему не катится.
static func _rides_on(kind: String) -> bool:
	return kind == TrackBuild.FROG_WING or kind == TrackBuild.BLADE


## frog_casting_mesh — СЕРДЕЧНИК: сплошная отливка между двумя гранями.
##
## # Почему это перемычка, а не рельс
##
## Сердечник — то место, где обе нитки перестают быть нитками: они входят в
## отливку и выходят её крыльями. До 2026-08-16 их там не было вовсе — нитки
## просто ПЕРЕСЕКАЛИСЬ, месили друг друга гранями и давали в кадре зазубрину.
## Владелец сказал прямо: «крестовина неправильно нарисована».
##
## Тело строится МЕЖДУ двумя присланными гранями: одна идёт по нитке прямого
## прохода, другая по нитке бокового, и сходятся они в точке крестовины. То, что
## между ними, и есть отливка — от поверхности катания вниз на высоту рельса.
##
## Форма приближённая и это объявлено: у настоящего сердечника есть горло, хвост
## и переменная ширина желоба, а здесь — прямая перемычка постоянной высоты.
## Зато она СПЛОШНАЯ, и пересечения ниток в кадре больше нет.
static func frog_casting_mesh(a: TrackBuild.FrogRail, b: TrackBuild.FrogRail) -> ArrayMesh:
	var lv := _casting_levels(a, b)
	if lv.is_empty():
		return null
	var verts := PackedVector3Array()
	var norms := PackedVector3Array()
	var idx := PackedInt32Array()
	var left: Array = lv["left"]
	var right: Array = lv["right"]
	# ВЕРХ — только некатаная середина: две полосы наката и две фаски вдоль
	# рабочих граней рисуют соседние меши (frog_casting_head_mesh и …_fillet_mesh).
	_quad_strip(verts, norms, idx, lv["mid_a"], lv["mid_b"])
	# БОКА ПО УРОВНЯМ: площадка → плечо → шейка → подошва. Скосы между ними и есть
	# то, чем отливка отличается от вертикальной стенки.
	for i in range(left.size() - 1):
		_quad_strip(verts, norms, idx, left[i], left[i + 1])
		_quad_strip(verts, norms, idx, right[i + 1], right[i])
	# Дно рисуется: сердечник виден снизу — между брусьями просвет.
	_quad_strip(verts, norms, idx, left[left.size() - 1], right[right.size() - 1])
	if idx.is_empty():
		return null
	return _mesh(verts, norms, idx)


## _casting_levels — линии сердечника на четырёх уровнях по высоте.
##
## # Почему у отливки профиль, а не стенка
##
## Грани сердечника идут по ниткам и у острия сходятся в миллиметры. Тело,
## построенное между ними как есть, давало ВЕРТИКАЛЬНЫЙ НОЖ: стенку в сантиметр
## толщиной и восемнадцать высотой посреди пути.
##
## У настоящей отливки в миллиметры сходится только ВЕРХНЯЯ ПЛОЩАДКА, а под ней
## лежит рельсовое тело. Модель CA-1/9-R65-v1 (§Б) даёт его четырьмя уровнями,
## считая ширину площадки w = |грань − грань|:
##
##   верх (глубина 0)         w — как разошлись грани, без всякого ограничения
##   плечо (45 мм)            max(75, w)
##   шейка (135 мм)           max(30, w − 45)
##   подошва (180 мм)         max(150, w + 75)
##
## Скос 11 → 75 мм на глубине головной части и есть то, что убирает нож.
static func _casting_levels(a: TrackBuild.FrogRail, b: TrackBuild.FrogRail) -> Dictionary:
	if not a.ready() or not b.ready():
		return {}
	var top_a := _frog_chain(a, Vector2.ZERO)
	var top_b := _frog_chain(b, Vector2.ZERO)
	var n := mini(top_a.size(), top_b.size())
	if n < 2:
		return {}
	if (top_a[1] - top_a[0]).cross(top_b[0] - top_a[0]).y < 0.0:
		var swap := top_a
		top_a = top_b
		top_b = swap
	top_a.resize(n)
	top_b.resize(n)
	var head := a.rail_head_width_m
	var full := a.rail_height_m
	# Глубины уровней — доли высоты рельса: головная часть, шейка, подошва.
	var depth := [0.0, full * CASTING_SHOULDER, full * CASTING_NECK, full]
	var left: Array = []
	var right: Array = []
	for _i in depth.size():
		left.append(PackedVector3Array())
		right.append(PackedVector3Array())
	var mid_a := PackedVector3Array()
	var mid_b := PackedVector3Array()
	for k in n:
		var wide := top_a[k].distance_to(top_b[k])
		var dir := (top_b[k] - top_a[k]).normalized() if wide > 1e-6 else _across(top_a, k)
		var mid := (top_a[k] + top_b[k]) * 0.5
		# ПОНИЖЕНИЕ ОСТРИЯ: у настоящей отливки верх выходит на поверхность катания
		# не сразу — колесо передаёт нагрузку постепенно. Считает его тот, кто знает
		# расстояние от острия, — сама нитка (FrogRail.sink_at).
		var sink := Vector3(0.0, a.sink_at(k), 0.0)
		# ПЛОЩАДКА НЕ БЫВАЕТ УЖЕ ЧЕТЫРЁХ МИЛЛИМЕТРОВ: у самого острия грани сходятся
		# в ноль, и полосы вырождаются в линию — треугольники нулевой площади, у
		# которых нет ни нормали, ни стороны. Проверка оболочки показала это
		# вывернутыми рёбрами, как только шаг разбиения стал мелким.
		var top_w := maxf(wide, CASTING_TIP_M)
		var widths := [
			top_w,
			maxf(head, top_w),
			maxf(head * CASTING_NECK_SHARE, top_w - head * CASTING_SHOULDER_STEP),
			maxf(head * 2.0, top_w + head),
		]
		for i in depth.size():
			var half := float(widths[i]) * 0.5
			var down := Vector3(0.0, float(depth[i]), 0.0)
			# ЧЕРЕЗ ЛОКАЛЬНУЮ И ОБРАТНО: Packed*Array в GDScript — значение, и
			# append по месту в чужом массиве уходит в копию. Первая редакция этой
			# функции так и потеряла все боковины разом: проверка оболочки сказала
			# «граней вбок 0».
			var lrow: PackedVector3Array = left[i]
			var rrow: PackedVector3Array = right[i]
			lrow.append(mid - dir * half - down - sink)
			rrow.append(mid + dir * half - down - sink)
			left[i] = lrow
			right[i] = rrow
		# Некатаная середина верха: то, что осталось между полосами наката с их
		# фасками. Отступ берётся ИЗ ТОЙ ЖЕ ширины наката, по которой полосы и
		# строятся, — иначе тело и накат разойдутся, и вдоль отливки откроется щель.
		var ride: float = a.ride_at(k)
		if ride < 0.0:
			ride = head * RAILHEAD_WIDTH
		var edge := ride + head * RAILHEAD_FILLET
		if ride <= 1e-4 or wide < 2.0 * edge:
			mid_a.append(top_a[k] - sink)
			mid_b.append(top_b[k] - sink)
		else:
			mid_a.append(top_a[k] + dir * edge - sink)
			mid_b.append(top_b[k] - dir * edge - sink)
	return {"left": left, "right": right, "mid_a": mid_a, "mid_b": mid_b}


## frog_casting_head_mesh — ВЕРХНЯЯ ПЛОЩАДКА СЕРДЕЧНИКА: по ней идёт колесо.
##
## Отдельным мешем по той же причине, по которой накат рельса отделён от его
## тела: у полированной стали другая физика поверхности, и одним материалом это
## не выражается. Заведено 2026-08-16 по вопросу владельца «почему она коричневая,
## эта крестовина?» — вся зона крестовины была ржавой, включая то, что колесо
## полирует каждым проходом.
static func frog_casting_head_mesh(a: TrackBuild.FrogRail, b: TrackBuild.FrogRail) -> ArrayMesh:
	return _casting_bands(a, b, true)


## frog_casting_fillet_mesh — ФАСКИ СЕРДЕЧНИКА: полоски между накатом и серединой.
##
## Тот же переход, что на головке рельса: «не может же быть ржавчина и тут же уже
## накат». Считается той же функцией, что накат, — только другими долями.
static func frog_casting_fillet_mesh(a: TrackBuild.FrogRail, b: TrackBuild.FrogRail) -> ArrayMesh:
	return _casting_bands(a, b, false)


## _casting_bands — пара продольных полос на площадке сердечника, по одной у
## каждой рабочей грани, между долями [from, to] ширины головки.
##
## ДВЕ ПОЛОСЫ ПО КРАЯМ, А НЕ ВСЯ ПЛОЩАДКА: колесо идёт по сердечнику вдоль его
## рабочих граней — по одной с каждой стороны, — а середина хвоста не катана
## ничем, там колея уже разошлась. Сплошная блестящая площадка читалась «клином
## полированной стали», чего на переводе не бывает.
##
## И НЕ С САМОГО ОСТРИЯ: пока отливка уже двух полос, колесо ещё несёт усовик, и
## блестеть тут нечему. Слово владельца: «как у этих железок внутри может быть
## накат?»
static func _casting_bands(a: TrackBuild.FrogRail, b: TrackBuild.FrogRail,
		from_ride: bool) -> ArrayMesh:
	var lv := _casting_levels(a, b)
	if lv.is_empty():
		return null
	var top_a: PackedVector3Array = (lv["left"] as Array)[0]
	var top_b: PackedVector3Array = (lv["right"] as Array)[0]
	var fillet := a.rail_head_width_m * RAILHEAD_FILLET
	var verts := PackedVector3Array()
	var norms := PackedVector3Array()
	var idx := PackedInt32Array()
	var la := PackedVector3Array()
	var lb := PackedVector3Array()
	var ra := PackedVector3Array()
	var rb := PackedVector3Array()
	for k in mini(top_a.size(), top_b.size()):
		var ride: float = a.ride_at(k)
		if ride < 0.0:
			ride = a.rail_head_width_m * RAILHEAD_WIDTH
		var lo := 0.0 if from_ride else ride
		var hi := ride if from_ride else ride + fillet
		var wide := top_a[k].distance_to(top_b[k])
		# Полос нет, пока площадка у́же двух полос с фасками: там колесо ещё несёт
		# усовик, и накату взяться неоткуда.
		if ride <= 1e-4 or wide < 2.0 * (ride + fillet):
			_strip_pair(verts, norms, idx, la, lb, ra, rb)
			la = PackedVector3Array(); lb = PackedVector3Array()
			ra = PackedVector3Array(); rb = PackedVector3Array()
			continue
		var dir := (top_b[k] - top_a[k]).normalized()
		la.append(top_a[k] + dir * lo)
		lb.append(top_a[k] + dir * hi)
		ra.append(top_b[k] - dir * hi)
		rb.append(top_b[k] - dir * lo)
	_strip_pair(verts, norms, idx, la, lb, ra, rb)
	if idx.is_empty():
		return null
	return _mesh(verts, norms, idx)


## _strip_pair — две полосы одним куском. Правая зеркальна левой относительно оси
## сердечника, но обход у неё тот же: зеркало уже учтено порядком концов.
static func _strip_pair(verts: PackedVector3Array, norms: PackedVector3Array,
		idx: PackedInt32Array, la: PackedVector3Array, lb: PackedVector3Array,
		ra: PackedVector3Array, rb: PackedVector3Array) -> void:
	if la.size() < 2:
		return
	_quad_strip(verts, norms, idx, la, lb)
	_quad_strip(verts, norms, idx, ra, rb)


## _across — поперечное направление там, где грани сошлись в точку и разности
## между ними уже нет. Берётся из хода самой линии: ось пути тут прямая, и
## перпендикуляр к ней в горизонтали — единственное осмысленное «поперёк».
static func _across(line: PackedVector3Array, k: int) -> Vector3:
	var j: int = k + 1 if k + 1 < line.size() else k - 1
	var along := (line[j] - line[k])
	if k + 1 >= line.size():
		along = -along
	along.y = 0.0
	if along.length() < 1e-9:
		return Vector3.RIGHT
	return along.normalized().cross(Vector3.UP)



## _frog_chain — направляющая по одной вершине сечения вдоль нитки крестовины.
##
## Вынос рабочей грани у каждой точки СВОЙ и уже посчитан при разборе
## (TrackBuild.frog_rails): показ его не выводит, он его откладывает.
static func _frog_chain(rail: TrackBuild.FrogRail, pt: Vector2) -> PackedVector3Array:
	var n := rail.axis.size()
	var out := PackedVector3Array()
	out.resize(n)
	for k in n:
		var p: TrackGeom.AxisPoint = rail.axis[k]
		var nl := p.left()
		# ОСТРОЖКА: сечение сужается к острию и опускается под головку рамного
		# рельса. У ниток без острожки доля равна единице, понижение нулю, и
		# выражение сходится к прежнему.
		var off: float = rail.faces[k] + rail.grow * pt.x * rail.width_at(k)
		out[k] = TerrainMesh.to_godot(
			p.x + nl.x * off, p.y + nl.y * off, p.z + pt.y - rail.sink_at(k))
	return out


## TOP_EPS — с какого отступа точка сечения считается лежащей на поверхности
## катания. Десятая доля миллиметра: мельче любого размера рельса и крупнее шума
## разбора чисел из JSON.
const TOP_EPS := 1e-4

## _top_gaps — что осталось от верхнего ребра сечения после выреза под накат.
##
## Возвращает отрезки по x В ТОМ ЖЕ НАПРАВЛЕНИИ, в каком шло ребро: направление
## обхода задаёт сторону нормали, и вернуть отрезки «как удобно» значило бы
## вывернуть кусок головки наизнанку.
##
## Ребро, не задевшее полосу наката, возвращается целиком — сечение вправе
## описывать верх головки не одним ребром, а несколькими.
static func _top_gaps(xa: float, xb: float, lo: float, hi: float) -> Array[Vector2]:
	var out: Array[Vector2] = []
	var left := minf(xa, xb)
	var right := maxf(xa, xb)
	var cut_lo := maxf(lo, left)
	var cut_hi := minf(hi, right)
	if cut_hi <= cut_lo:
		out.append(Vector2(xa, xb))
		return out
	var parts: Array[Vector2] = []
	if cut_lo > left:
		parts.append(Vector2(left, cut_lo))
	if right > cut_hi:
		parts.append(Vector2(cut_hi, right))
	if xa <= xb:
		return parts
	for k in range(parts.size() - 1, -1, -1):
		out.append(Vector2(parts[k].y, parts[k].x))
	return out


## _section_chain — направляющая по одной вершине сечения вдоль всего участка.
##
## Вершина сечения задана в осях (x наружу от рабочей грани, y вниз от
## поверхности катания); здесь она превращается в мировую точку у каждой точки
## оси. Отдельной функцией по той же причине, что и _rail_chain: соседние полосы
## обязаны считать одну и ту же точку одним и тем же выражением.
static func _section_chain(span: TrackBuild.Span, axis: Array, sgn: float,
		pt: Vector2) -> PackedVector3Array:
	# НИТКА НЕПОДВИЖНА ВСЕГДА, в том числе на переводе: рамными рельсами стали
	# обе нитки прямого прохода, а остряк с 2026-08-16 — своя деталь со своим
	# телом (TrackBuild.Blade). До того показ отводил саму нитку, и это означало,
	# что рамного рельса не существует: его роль играл прижатый остряк, уезжавший
	# вместе с переводом.
	var n := axis.size()
	var offset := (span.gauge_m * 0.5 + pt.x) * sgn
	var out := PackedVector3Array()
	out.resize(n)
	for k in n:
		var p: TrackGeom.AxisPoint = axis[k]
		var nl := p.left()
		out[k] = TerrainMesh.to_godot(p.x + nl.x * offset, p.y + nl.y * offset, p.z + pt.y)
	return out


## _rail_chain — направляющая нитки на боковом выносе `offset` от оси, поверху
## (`at_top`) или по подошве. Вынесена затем, что тело и накат обязаны считать
## одни и те же точки: разойдись они на округлении, и между ними откроется щель
## во всю длину пути.
static func _rail_chain(span: TrackBuild.Span, axis: Array, sgn: float,
		offset: float, at_top: bool) -> PackedVector3Array:
	# Нитка неподвижна — разбор тот же, что у _section_chain.
	var n := axis.size()
	var off := offset * sgn
	var out := PackedVector3Array()
	out.resize(n)
	for k in n:
		var p: TrackGeom.AxisPoint = axis[k]
		var nl := p.left()
		var z := p.z if at_top else p.z - span.rail_height_m
		out[k] = TerrainMesh.to_godot(p.x + nl.x * off, p.y + nl.y * off, z)
	return out


## slab_mesh — плита платформы: верх, торец у пути и дальний торец.
##
## Толщина нужна не для объёма ради объёма: платформа видна СБОКУ, и торец плиты
## с просветом под ним — то, чем она отличается от прямоугольника, нарисованного
## на земле. Низ плиты не рисуется — он лежит на насыпи и не виден.
static func slab_mesh(strip: TrackBuild.PlatformStrip) -> ArrayMesh:
	if not strip.has_slab():
		return null
	var n := mini(strip.near.size(), strip.far.size())
	if n < 2:
		return null
	var t := strip.slab_thickness_m
	var top_n := PackedVector3Array(); top_n.resize(n)
	var top_f := PackedVector3Array(); top_f.resize(n)
	var bot_n := PackedVector3Array(); bot_n.resize(n)
	var bot_f := PackedVector3Array(); bot_f.resize(n)
	for k in n:
		var a := strip.near[k]
		var b := strip.far[k]
		top_n[k] = TerrainMesh.to_godot(a.x, a.y, a.z)
		top_f[k] = TerrainMesh.to_godot(b.x, b.y, b.z)
		bot_n[k] = TerrainMesh.to_godot(a.x, a.y, a.z - t)
		bot_f[k] = TerrainMesh.to_godot(b.x, b.y, b.z - t)
	# ОБХОД ЗАВИСИТ ОТ СТОРОНЫ ПЛАТФОРМЫ, И БЕЗ ЭТОЙ ПРОВЕРКИ ПРАВАЯ ИСЧЕЗАЛА.
	#
	# `_quad_strip` берёт нормаль как (p2−p0)×(p1−p0), то есть направление грани
	# задаёт ПОРЯДОК двух краёв. Для платформы слева от оси пара «ближний,
	# дальний» даёт нормаль верхней грани вверх, а для платформы справа — ВНИЗ:
	# грань становится задней, отсекается при cull_back, и плиты на экране нет
	# вовсе. У затравки ST_A платформа объявлена `side: "right"`, поэтому она не
	# рисовалась ни разу, хотя HUD честно писал «платформ 1 (плитой 1)» — меш
	# строился, просто был вывернут наизнанку.
	#
	# Поймано владельцем на кадре 2026-08-12. Это тот же класс ошибки, что нашли
	# в тот же день у `_box` (коробки домов и упоров стояли изнанкой наружу), и
	# лечится он тем же способом: не подгонкой материала, а ОРИЕНТАЦИЕЙ ОБХОДА.
	# `cull_disabled` здесь отвергнут — он прячет вывернутый меш, а не правит его,
	# и стоит вдвое по граням.
	#
	# Перестановка краёв геометрию не меняет: плита в сечении симметрична, и
	# «ближний» с «дальним» — только имена концов. Зато один разворот выправляет
	# ВСЕ ТРИ ленты разом: верх, торец у пути и дальний торец обходятся согласованно.
	var a_edge := top_n
	var b_edge := top_f
	var a_bot := bot_n
	var b_bot := bot_f
	var tangent := top_n[1] - top_n[0]
	var across := top_f[0] - top_n[0]
	if tangent.cross(across).y < 0.0:
		a_edge = top_f
		b_edge = top_n
		a_bot = bot_f
		b_bot = bot_n

	var verts := PackedVector3Array()
	var norms := PackedVector3Array()
	var idx := PackedInt32Array()
	_quad_strip(verts, norms, idx, a_edge, b_edge)
	_quad_strip(verts, norms, idx, a_bot, a_edge)
	_quad_strip(verts, norms, idx, b_edge, b_bot)
	return _mesh(verts, norms, idx)


## buffer_stop_mesh — упоры коробками.
##
## Габарит присланный: height — над поверхностью катания, width — поперёк пути.
## Длина вдоль пути НЕ прислана и здесь взята равной трети ширины — это решение
## художника того же рода, что длина крыла крестовины, и оно названо: упор,
## нарисованный плоским прямоугольником, не читался бы как препятствие. Второй
## клиент вправе взять другую пропорцию, и мир от этого не изменится.
static func buffer_stop_mesh(list: Array[TrackBuild.BufferStop], length_ratio: float) -> ArrayMesh:
	if list.is_empty():
		return null
	var verts := PackedVector3Array()
	var norms := PackedVector3Array()
	var idx := PackedInt32Array()
	for b_raw in list:
		var b: TrackBuild.BufferStop = b_raw
		var p := b.pose
		_box(verts, norms, idx, p.x, p.y, p.forward(),
			b.width_m * length_ratio * 0.5, b.width_m * 0.5, p.z, p.z + b.height_m)
	if idx.is_empty():
		return null
	return _mesh(verts, norms, idx)


## rail_mesh — нитки линиями.
##
## Линия, а не полоса в метрах: ширина нитки — величина ЭКРАННАЯ (render-contract
## §2, «толщина нитки 2 экранных px — по определению экранная»). Полоса в метрах
## заявляла бы ширину головки рельса, которой в контракте нет.
static func rail_mesh(threads: Array[PackedVector3Array]) -> ImmediateMesh:
	var any := false
	var mesh := ImmediateMesh.new()
	for line in threads:
		if line.size() < 2:
			continue
		any = true
		mesh.surface_begin(Mesh.PRIMITIVE_LINE_STRIP)
		for p in line:
			mesh.surface_add_vertex(TerrainMesh.to_godot(p.x, p.y, p.z))
		mesh.surface_end()
	return mesh if any else null


static func line_mesh(axis: Array[TrackGeom.AxisPoint]) -> ImmediateMesh:
	if axis.size() < 2:
		return null
	var mesh := ImmediateMesh.new()
	mesh.surface_begin(Mesh.PRIMITIVE_LINE_STRIP)
	for p_raw in axis:
		var p: TrackGeom.AxisPoint = p_raw
		mesh.surface_add_vertex(TerrainMesh.to_godot(p.x, p.y, p.z))
	mesh.surface_end()
	return mesh


## _quad_strip — полоса между двумя направляющими, дописанная в общие буферы.
##
## Нормаль считается из самой геометрии квада, а не задаётся Vector3.UP: у
## откоса призмы она наклонная, и UP сделал бы откос неотличимым по свету от
## верха. Порядок направляющих задаёт сторону: (a, b) даёт нормаль наружу при
## обходе против часовой в плане.
static func _quad_strip(verts: PackedVector3Array, norms: PackedVector3Array,
		idx: PackedInt32Array, a: PackedVector3Array, b: PackedVector3Array) -> void:
	var n := mini(a.size(), b.size())
	if n < 2:
		return
	for k in n - 1:
		var p0 := a[k]
		var p1 := b[k]
		var p2 := a[k + 1]
		var p3 := b[k + 1]
		var nrm := (p2 - p0).cross(p1 - p0).normalized()
		if nrm == Vector3.ZERO:
			nrm = Vector3.UP
		var base := verts.size()
		verts.append(p0); verts.append(p1); verts.append(p2); verts.append(p3)
		for c in 4:
			norms.append(nrm)
		idx.append_array([base, base + 1, base + 2, base + 1, base + 3, base + 2])


## _box — прямоугольный параллелепипед по оси пути: длина вдоль, ширина поперёк,
## от z_bot до z_top. Шесть граней; торцы нужны, потому что коробка одиночная.
## box_into — та же коробка, что _box, но с цветом вершины. Публичная: постройки
## строятся ею же, и второй экземпляр той же геометрии разошёлся бы с первым.
static func box_into(verts: PackedVector3Array, norms: PackedVector3Array, cols: PackedColorArray,
		idx: PackedInt32Array, cx: float, cy: float, fwd: Vector2, half_len: float, half_wid: float,
		z_bot: float, z_top: float, col: Color) -> void:
	var before := verts.size()
	_box(verts, norms, idx, cx, cy, fwd, half_len, half_wid, z_bot, z_top)
	# ЦВЕТ КЛАДЁТСЯ ЛИНЕЙНЫМ, а принимается sRGB — в нём записаны константы и в
	# нём же их читает человек. Перевод живёт ЗДЕСЬ, в единственной двери в меш,
	# а не у каждого вызывающего: правило уже записано (bd recall
	# godot-vertex-color-linear), и терялось оно ровно тем, что было в одном
	# месте (terrain_mesh.gd) и отсутствовало в двух других.
	#
	# Цена пропуска замерена спайком и видна на кадре: albedo_color движок
	# переводит сам, а ARRAY_COLOR берёт как есть, и 0.41 выходит на экран как
	# 0.67. Посёлок из шести разных цветов читался двенадцатью одинаковыми
	# светло-серыми плитами — не потому, что цвета одинаковы, а потому, что все
	# они выбелены к одному пределу.
	var lin := col.srgb_to_linear()
	for _k in range(before, verts.size()):
		cols.append(lin)


## ОБХОД ГРАНЕЙ КОРОБКИ БЫЛ ВЫВЕРНУТ, и «прозрачные шпалы» — это он.
##
## Замер, а не рассуждение (2026-08-12, роль driver, 1600×900, ближний план):
##
##  1. Шпала на экране — полоса 21 px. Расчёт по геометрии кадра даёт 19 px для
##     ОДНОЙ вертикальной стенки, торчащей из балласта на 0.10 м, и 32 px для
##     верхней грани. Верхней грани на экране нет вовсе.
##  2. Временный шейдер, красящий грань её мировой нормалью, показал в этой
##     полосе нормаль (1, 0, 0) — стенку, отвёрнутую ОТ камеры (машинист смотрит
##     вдоль +x). То есть рисуется изнанка дальней стенки, а ближняя отброшена.
##  3. Тот же шейдер с `cull_disabled` дал в той же точке нормаль (0, −1, 0):
##     верхняя грань появилась, и движок перевернул ей нормаль как ИЗНАНКЕ
##     (godot-cull-disabled-flips-normal). Значит верх был отброшен CULL_BACK.
##
## Отсюда картинка: сверху шпала не имеет верха, сквозь неё виден балласт на
## 0.10 м ниже, а тёмная полоса — изнанка дальней стенки. Снаружи это и читается
## «шпалы прозрачные».
##
## ПРИЧИНА НЕ В `to_godot`. Он отображает (x, y, z) в (x, z, −y), и это ПОВОРОТ:
## определитель +1, ориентация сохраняется, обход не разворачивается. Проверять
## это гипотезой про зеркало — тупик: зеркала здесь нет.
##
## Причина в том, что у Godot лицевой считается грань, обход которой на экране
## ПО ЧАСОВОЙ, то есть правая нормаль обхода смотрит ОТ зрителя. `_quad_strip` в
## этом же файле так и намотан — он берёт нормаль как `(p2−p0)×(p1−p0)`, обратную
## правой нормали своего обхода, и призма рисуется лицом наружу. `_box` считал
## нормаль как `(p1−p0)×(p2−p0)`, то есть СОГЛАСНО обходу, — и оттого выворачивал
## каждую свою коробку наизнанку.
##
## Цена ошибки была не только у шпал: `box_into` строит ею же дома и упоры, и они
## всё это время стояли изнанкой наружу. На коробке это незаметно — силуэт тот
## же, а свет правдоподобен, потому что нормали остались наружными.
##
## Лечение — порядок вершин, а не `cull_mode = CULL_DISABLED`: тот прячет дефект
## и стоит вдвое по граням.
static func _box(verts: PackedVector3Array, norms: PackedVector3Array, idx: PackedInt32Array,
		cx: float, cy: float, fwd: Vector2, half_len: float, half_wid: float,
		z_bot: float, z_top: float) -> void:
	var f := fwd.normalized()
	if f == Vector2.ZERO:
		return
	var l := Vector2(-f.y, f.x)
	# Массивы типизированы явно: у нетипизированного литерала элемент цикла
	# приезжает Variant, и sgn.x не выводится — GDScript отказывается разбирать
	# файл целиком. Поймано снимком, а не проверками: check.gd не трогает
	# track_view.gd вовсе и потому дал 78 «ok» при неразбираемом рисующем коде.
	var levels: Array[float] = [z_bot, z_top]
	var signs: Array[Vector2] = [Vector2(-1, -1), Vector2(1, -1), Vector2(1, 1), Vector2(-1, 1)]
	var corners := PackedVector3Array()
	for sz in levels:
		for sgn in signs:
			var px := cx + f.x * half_len * sgn.x + l.x * half_wid * sgn.y
			var py := cy + f.y * half_len * sgn.x + l.y * half_wid * sgn.y
			corners.append(TerrainMesh.to_godot(px, py, sz))
	# Грани перечислены явно: 0..3 — низ, 4..7 — верх, порядок обхода задаёт
	# наружную нормаль.
	var faces: Array[PackedInt32Array] = [
		PackedInt32Array([4, 5, 6, 7]), PackedInt32Array([3, 2, 1, 0]),
		PackedInt32Array([0, 1, 5, 4]), PackedInt32Array([1, 2, 6, 5]),
		PackedInt32Array([2, 3, 7, 6]), PackedInt32Array([3, 0, 4, 7]),
	]
	for face in faces:
		var p0: Vector3 = corners[face[0]]
		var p1: Vector3 = corners[face[1]]
		var p2: Vector3 = corners[face[2]]
		var p3: Vector3 = corners[face[3]]
		var nrm := (p1 - p0).cross(p2 - p0).normalized()
		if nrm == Vector3.ZERO:
			nrm = Vector3.UP
		var base := verts.size()
		verts.append(p0); verts.append(p1); verts.append(p2); verts.append(p3)
		for c in 4:
			norms.append(nrm)
		# Обход ПРОТИВ нормали: правая нормаль обхода p0→p2→p1 равна −nrm, и
		# именно так Godot узнаёт лицевую грань (разбор в шапке функции).
		idx.append_array([base, base + 2, base + 1, base, base + 3, base + 2])


static func _strip(verts: PackedVector3Array, norms: PackedVector3Array) -> ArrayMesh:
	var pairs := verts.size() / 2
	var idx := PackedInt32Array()
	for k in pairs - 1:
		var a := k * 2
		var b := k * 2 + 1
		var c := (k + 1) * 2
		var d := (k + 1) * 2 + 1
		idx.append_array([a, c, b, b, c, d])
	return _mesh(verts, norms, idx)


static func _mesh(verts: PackedVector3Array, norms: PackedVector3Array, idx: PackedInt32Array) -> ArrayMesh:
	var arrays := []
	arrays.resize(Mesh.ARRAY_MAX)
	arrays[Mesh.ARRAY_VERTEX] = verts
	arrays[Mesh.ARRAY_NORMAL] = norms
	arrays[Mesh.ARRAY_INDEX] = idx
	var mesh := ArrayMesh.new()
	mesh.add_surface_from_arrays(Mesh.PRIMITIVE_TRIANGLES, arrays)
	return mesh


## solid_material — материал ТЕЛА: обычный, с проверкой глубины.
##
## Отличается от flat_material ровно тем, ради чего писалась вся редакция 6:
## тело занимает объём, у него есть верх и бок под разным углом к свету, и
## отключать ему проверку глубины больше не за что. Раньше отключали потому, что
## путь и земля лежали на одной отметке и дрались за z-буфер; теперь земля на
## `formation_to_rail_top` ниже, и щель между ними заполнена призмой.
static func solid_material(colour: Color) -> StandardMaterial3D:
	var m := StandardMaterial3D.new()
	m.albedo_color = colour
	return m


## ФАКТУРА ПУТИ — ПОРТ ИЗ СНЕСЁННОГО СПАЙКА, а не новое сочинение.
##
## Материалы щебня и шпалы были написаны в `spike_world.gd` (коммит 809280f^) и
## доведены там снимками; вместе с кодом там записаны три ошибки, которые стоили
## заходов, и переписывать их заново значило бы наступить на них второй раз:
##
##  1. КЛЕТОЧНЫЙ ШУМ, А НЕ ПЕРЛИН. У перлина нет высоких частот: вблизи балласт
##     читается серой лентой с разводами. Щебень — это ЯЧЕЙКИ С ГРАНИЦАМИ, и
##     даёт их F2−F1: ноль в середине зерна, максимум на шве с соседним.
##  2. SEAMLESS ЗДЕСЬ ВРЕДЕН. NoiseTexture2D делает бесшовность кросс-фейдом
##     тайла с самим собой; клеточный шум от наложения двух сеток теряет ровно
##     то, ради чего взят, — границу зерна. Первый заход спайка с seamless дал
##     наждак вместо щебня. Шов тайла на узкой полосе не виден.
##  3. РАМПА ТОЛЬКО ГАСИТ, ПОДНЯТЬ ОНА НЕ МОЖЕТ: печётся в RGBA8, всё выше
##     единицы обрезается. У F2−F1 бо́льшая часть площади — середина зерна, то
##     есть малые значения; рампа 0.38…1.0 съедала две трети альбедо, и «сухой
##     светлый щебень» выходил почти чёрным. Светлоту задаёт ЦВЕТ, рампа
##     оставляет себе только тень в шве.
##
## Трипланар вместо UV — оттуда же: у мешей пути развёртки нет, и заводить её
## означало бы третью систему координат рядом с (x, y) и u.
const BALLAST_STONE := 0.050   # м — характерный размер щебёнки (натура 3–6 см)
const BALLAST_TILE := 512      # px — сторона тайла
const BALLAST_CELL_PX := 32.0  # px — сторона ячейки, она же 1/frequency
const C_BALLAST := Color(0.50, 0.48, 0.45)  # сухой щебень на солнце
const C_SLEEPER := Color(0.22, 0.17, 0.13)  # пропитанная шпала
## Направление волокна берётся анизотропным uv1_scale, то есть в МИРОВЫХ осях, и
## ограничение названо честно ещё спайком: на дуге и на путях под углом волокно
## поедет. Правильное лечение — свои UV на коробке шпалы; это отдельная работа.
const SLEEPER_GRAIN := 0.045   # м — период волокна ПОПЕРЁК бруса
const SLEEPER_RUN := 2.2       # м — период рисунка ВДОЛЬ бруса

## РЕЛЬС — ДВА МАТЕРИАЛА И ДВЕ ПОВЕРХНОСТИ. Порт из спайка вместе с доводом:
## тело рельса ржавое, а ржавчина — ДИЭЛЕКТРИК (metallic 0, шероховатость
## высокая). Металлом остаётся только накат, по которому идёт колесо. Прежние
## 0.55/0.35 спайк назвал «полуметаллом, какого не бывает», и до 2026-08-12
## здесь стоял ещё худший случай: ОДИН плоский solid_material(0.42, 0.40, 0.40) —
## albedo и больше ничего. Комментарий рядом при этом описывал два материала:
## он пережил код, который объяснял.
## Ржавчина ПРИГЛУШЕНА 2026-08-16: было (0.24, 0.19, 0.16), и владелец сказал
## «сильно коричневая». Бок рельса под открытым небом — это не рыжая окалина, а
## тёмный серо-бурый налёт: рыжина держится недели, дальше поверхность темнеет и
## сереет. Отношение красного к синему было 1.50, стало 1.16.
const C_RAIL := Color(0.215, 0.195, 0.185)   # бок и подошва: ржавчина
const C_RAILHEAD := Color(0.62, 0.63, 0.66)  # накат: полированная колесом сталь
## Фаска — между ними и ближе к накату: она стёрта колесом, но не отполирована.
const C_RAILFILLET := Color(0.40, 0.39, 0.38)

## ШИРИНА НАКАТА — ДОЛЯ ГОЛОВКИ, А НЕ СВОИ МЕТРЫ, и это решение надо назвать.
##
## У спайка накат был выдуман целиком: RAILHEAD_W = 0.062 м рядом с его же
## выдуманной головкой 0.071 м. У нас ширина головки ПРИСЛАНА (`rail.head_width`,
## на затравке 0.075 м), и повторять сюда спайковы 0.062 значило бы завести в
## клиенте второе число про размер рельса — ровно то, ради отсутствия чего
## head_width и появился в контракте. Поэтому от спайка берётся не число, а его
## ОТНОШЕНИЕ: 0.062 / 0.071 = 0.87 ширины головки. Пришлёт сервер другой рельс —
## накат поедет вместе с ним.
const RAILHEAD_WIDTH := 0.72   # доля ширины головки, которую занимает чистый накат

## ФАСКА — ПЕРЕХОД ОТ НАКАТА К РЖАВЧИНЕ, доля ширины головки на каждую сторону.
##
## Заведена 2026-08-16 словом владельца: «переход какой-то должен быть, не может
## же быть ржавчина и тут же уже накат». И он прав по существу: у настоящего
## рельса край головки скруглён, колесо задевает его вскользь, и полоса там не
## зеркальная и не ржавая — она серая, потёртая.
##
## Выражено ТРЕТЬИМ МАТЕРИАЛОМ, а не градиентом: у нас нет ни развёртки, ни
## вершинных цветов на путевых мешах, а металличность по вершинам не
## интерполируется вовсе — «полуметалла не бывает» действует и здесь. Три полосы
## стыкуются, как и прежде две.
##
## Сумма: 0.72 наката + 2 × 0.075 фаски = 0.87 головки — ровно столько, сколько
## занимал накат до этой правки.
const RAILHEAD_FILLET := 0.075

## УРОВНИ СЕРДЕЧНИКА — доли высоты рельса и ширины головки (CA-1/9-R65-v1 §Б).
##
## Глубина головной части 45 мм из 180 — четверть; шейка кончается на 135 мм —
## три четверти. Ширины: шейка не у́же 30 мм (0.4 головки), подошва не у́же 150 мм
## (две головки), а от ширины площадки шейка отступает на 45 мм (0.6 головки).
const CASTING_SHOULDER := 0.25
const CASTING_NECK := 0.75
const CASTING_NECK_SHARE := 0.40
const CASTING_SHOULDER_STEP := 0.60
## Наименьшая ширина верхней площадки, метры: у настоящего сердечника остриё
## срезано до 9–12 мм, острее не бывает по технологии литья.
const CASTING_TIP_M := 0.009

## ВЫСТУП В 4 ММ ОТВЕРГНУТ. У спайка накат стоял НА головке и торчал над ней на
## RAILHEAD_T = 0.004 м — у него верх рельса был его собственной выдумкой, и
## поднять над ним ещё миллиметры ничего не нарушало. У нас `z` элемента объявлен
## ПОВЕРХНОСТЬЮ КАТАНИЯ (контракт редакции 6 §2), и накат, поднятый над ней, был
## бы поверхностью катания на 4 мм выше той, которую назвал сервер. Поэтому накат
## лежит В уровне головки полосой, а не накладкой поверх неё. Цена названа: с
## выступом кромка наката ловила бы ещё и блик по ребру, здесь его нет.


static func ballast_material() -> StandardMaterial3D:
	var period := BALLAST_STONE * (float(BALLAST_TILE) / BALLAST_CELL_PX)
	var m := StandardMaterial3D.new()
	m.albedo_color = C_BALLAST
	m.roughness = 1.0
	m.albedo_texture = _stone_texture(false)
	m.normal_enabled = true
	m.normal_texture = _stone_texture(true)
	m.normal_scale = 1.5
	m.uv1_triplanar = true
	m.uv1_scale = Vector3.ONE / period
	return m


static func _stone_texture(as_normal: bool) -> NoiseTexture2D:
	var noise := FastNoiseLite.new()
	noise.seed = 0xB0115
	noise.noise_type = FastNoiseLite.TYPE_CELLULAR
	noise.cellular_return_type = FastNoiseLite.RETURN_DISTANCE2_SUB
	noise.cellular_distance_function = FastNoiseLite.DISTANCE_EUCLIDEAN
	noise.cellular_jitter = 1.0
	noise.frequency = 1.0 / BALLAST_CELL_PX
	noise.fractal_type = FastNoiseLite.FRACTAL_NONE
	var nt := NoiseTexture2D.new()
	nt.width = BALLAST_TILE
	nt.height = BALLAST_TILE
	nt.seamless = false
	nt.noise = noise
	if as_normal:
		nt.as_normal_map = true
		nt.bump_strength = 24.0
	else:
		var ramp := Gradient.new()
		ramp.offsets = PackedFloat32Array([0.0, 0.30, 0.75, 1.0])
		ramp.colors = PackedColorArray([
			Color(0.62, 0.62, 0.62), Color(0.82, 0.82, 0.82),
			Color(1.00, 1.00, 1.00), Color(1.00, 1.00, 1.00)])
		nt.color_ramp = ramp
	return nt


static func sleeper_material() -> StandardMaterial3D:
	var m := StandardMaterial3D.new()
	m.albedo_color = C_SLEEPER
	m.roughness = 0.82
	m.albedo_texture = _wood_texture(false)
	m.normal_enabled = true
	m.normal_texture = _wood_texture(true)
	# Дерево почти плоское: рельеф даёт трещина, а не волокно.
	m.normal_scale = 0.7
	m.uv1_triplanar = true
	m.uv1_scale = Vector3(1.0 / SLEEPER_GRAIN, 1.0 / SLEEPER_RUN, 1.0 / SLEEPER_RUN)
	return m


## rail_material — ТЕЛО рельса: ржавчина, а не металл.
static func rail_material() -> StandardMaterial3D:
	var m := StandardMaterial3D.new()
	m.albedo_color = C_RAIL
	m.roughness = 0.85
	m.metallic = 0.0
	return m


## head_bands — границы полос на верху головки, от внутренней грани наружу.
##
## Одна функция на всех, кто их считает: тело рельса режет по крайним, накат
## лежит между средними, фаски — в двух зазорах. Посчитай их порознь — полосы
## разойдутся, и между ними откроется щель во всю длину пути.
##
## Возвращает (наружная граница выреза слева, начало наката, конец наката,
## наружная граница выреза справа) в осях сечения — от рабочей грани наружу.
static func head_bands(head_width_m: float) -> Vector4:
	var band := head_width_m * RAILHEAD_WIDTH
	var fil := head_width_m * RAILHEAD_FILLET
	var band_i := (head_width_m - band) * 0.5
	var band_o := band_i + band
	return Vector4(band_i - fil, band_i, band_o, band_o + fil)


## railfillet_material — ФАСКА: кромка головки между накатом и ржавчиной.
##
## Полуметалл здесь ЗАКОННЫЙ, в отличие от тела рельса: это не «сталь, окрашенная
## ржавчиной», а стёртая поверхность с остатками окисла — у неё и правда
## промежуточное отражение. Довод, по которому полуметалла не бывает у ТЕЛА,
## записан у railhead_material и остаётся в силе.
static func railfillet_material() -> StandardMaterial3D:
	var m := StandardMaterial3D.new()
	m.albedo_color = C_RAILFILLET
	m.roughness = 0.62
	m.metallic = 0.45
	return m


## railhead_material — НАКАТ: сталь.
##
## metallic 1.0, а не 0.85: полуметалла не бывает. Это от спайка остаётся.
##
## # ШЕРОХОВАТОСТЬ ВЕРНУЛАСЬ К СПАЙКОВЫМ 0.22, И РАЗБОР НИЖЕ ОСТАВЛЕН КАК ОШИБКА
##
## Ниже лежит разбор, по которому шероховатость подняли до 0.85. Он ВЕРЕН в
## каждом замере и НЕВЕРЕН в выводе; оставлен целиком нарочно, потому что цена
## его — полдня, и повторять эту дорогу не надо.
##
## Что было упущено: синева наката — не болезнь, а ФОН, на котором виден блик.
## Владелец сличил два окна рядом — наш клиент и запущенный спайк — и увидел
## главное, чего не показывает пипетка по одной точке: у спайка вдоль наката идёт
## ЯРКАЯ БЕЛАЯ НИТЬ бликового отражения СОЛНЦА, и именно она читается сталью.
## Накат при этом синеватый и у него.
##
## Пипетка мерила усреднённый цвет полосы и потому видела только фон. Подъём
## шероховатости фон и выправил — b/r с 2.00 до 1.31, — а нить размазал: та самая
## «цена», названная в конце разбора строкой «блик по нити становится плоским»,
## и оказалась единственным, что имело значение. Правка лечила измеренное и
## убивала неизмеренное.
##
## Урок общий, и он дороже числа: замер по одной точке не заменяет сличения с
## эталоном. У нас эталон запускается — им и надо проверять.
##
## Жалоба владельца 2026-08-12: «конечно они не должны быть синими». Накат и
## правда был синим, и вот замером (пипетка по снимку роли driver, 1600×900,
## отношение синего канала к красному; у балласта рядом, серого и матового, оно
## ровно 1.00):
##
##     шероховатость 0.22 → rgb (70, 96, 140), b/r 2.00   ← было
##     шероховатость 0.55 → rgb (82, 99, 133), b/r 1.62
##     шероховатость 0.85 → rgb (112, 123, 147), b/r 1.31 ← стало
##     шероховатость 1.00 → rgb (123, 131, 153), b/r 1.24
##
## ПРИЧИНА СИНЕВЫ — НЕ ОСВЕЩЁННОСТЬ, А ТО, ЧТО ОТРАЖАТЬ НЕЧЕГО. У металла
## диффузной составляющей нет вовсе: весь его цвет — отражение карты излучения, а
## в ней у нас нет ничего, кроме процедурного неба. Настоящий накат серебрится
## потому, что зеркалит ВЕСЬ мир — балласт, шпалы, траву, состав; наш зеркалит
## один участок неба, и при зените (0.30, 0.50, 0.78) отражение обязано выйти
## синим. Проверено подстановкой: небо с красным зенитом даёт накат (160, 62, 70)
## — то есть цвет наката это цвет неба и ничего больше.
##
## Шероховатость здесь — не «сталь на самом деле матовая», а единственный
## доступный способ заставить отражение УСРЕДНИТЬ полусферу вместо одного луча:
## с ростом шероховатости в него входит и бледный горизонт, и нижняя половина
## неба (ground_horizon/ground_bottom, они серо-зелёные), и цвет сходится к
## нейтральному — к тому же, что даёт балласту его диффузное усреднение.
##
## ЧТО ОТВЕРГНУТО, И ПОЧЕМУ:
##   * ambient_light_energy (0.38 у спайка против 1.0 у нас) — НЕ РАБОТАЕТ ВОВСЕ.
##     Замер: 0.0 и 1.0 дают ПОБАЙТНО ОДИНАКОВЫЙ кадр. Так и задокументировано у
##     Godot: параметр действует, только если ambient_light_sky_contribution < 1,
##     а у нас (и у спайка) он равен единице. Плюс ambient — заливка ДИФФУЗНАЯ, а
##     у металла диффузной части нет: даже будь параметр жив, наката он бы не
##     коснулся.
##   * ssr_enabled — отражение экранного пространства подставило бы в накат
##     настоящее окружение. Замер: кадр не изменился ни на пиксель.
##   * sky_curve пошире (бледный горизонт выше по небу) — вылечило бы и накат, и
##     небо, но это правка НЕБА ради шестисантиметровой полосы: цвет всего кадра
##     ради одной детали.
##
## ЦЕНА НАЗВАНА: блик по нити становится плоским. При 0.22 накат менялся от
## (83, 108, 148) вдали до (60, 88, 135) вблизи — это и был «отблеск»; при 0.85
## разброс 115 → 111, то есть нить ровная по всей длине. Взамен исчезает бликовый
## алиасинг, ради которого спайк и поднимал шероховатость с «научных» 0.08–0.18:
## его довод («у матового наката нить идеально ровная, у зеркального рвётся»)
## работает в ту же сторону, что и эта правка, — просто спайк остановился на
## полпути, потому что смотрел на накат сверху, где он мельче пикселя и цвета не
## имеет. У машиниста та же полоса занимает 14 пикселей поперёк.
## # ТРЕТИЙ ЗАХОД НА СИНЕВУ, 2026-08-16, и на этот раз лечится причина
##
## Разбор выше верен целиком и остаётся: у МЕТАЛЛА нет диффузной составляющей,
## весь его цвет — отражение карты излучения, а в ней у нас нет ничего, кроме
## процедурного неба. Отсюда и вывод разбора: «цвет наката это цвет неба и
## ничего больше». Шероховатость лечила следствие — усредняла полусферу, — и
## платила бликом; поэтому её и откатили.
##
## Из четырёх отвергнутых вариантов не был назван пятый, самый прямой: НЕ ДЕЛАТЬ
## НАКАТ МЕТАЛЛОМ. Диэлектрик берёт цвет из АЛЬБЕДО и света, а не из отражения
## неба, и блик у него остаётся — солнечный, а не небесный. Синеть ему нечем.
##
## Физически это неправда: сталь — металл. Но и «металл, зеркалящий одно небо»
## неправда ровно так же, и вторая заметнее: владелец жаловался на синие рельсы
## ДВАЖДЫ (2026-08-12 и 2026-08-16). Из двух неправд выбрана та, которая даёт
## верный ЦВЕТ, и выбор назван здесь, а не спрятан в числах.
##
## ЗАМЕР (пипетка по снимку роли driver, кадр throat, самое синее в нижней
## половине; у балласта рядом отношение b/r ровно 1.00):
##
##     металл, шероховатость 0.22 → rgb (64, 85, 109), b/r 1.72   ← было
##     диэлектрик, шероховатость 0.25 → см. строку ниже            ← стало
##
## Настоящее лечение — окружение, которое есть что зеркалить: отражающая проба
## у камеры либо небо с честной нижней полусферой. Оба стоят кадра целиком, и
## оба ждут своего дня; этот выбор их не отменяет, а откладывает.
static func railhead_material() -> StandardMaterial3D:
	var m := StandardMaterial3D.new()
	m.albedo_color = C_RAILHEAD
	m.roughness = 0.25
	m.metallic = 0.0
	# Блик диэлектрика слабее металлического, и накат без него стал бы просто
	# светлой полосой. Полная зеркальность у диэлектрика — это не «как металл»,
	# а «как полированный камень»: цвет остаётся своим, ярче становится только
	# отблик.
	m.specular = 1.0
	return m


static func _wood_texture(as_normal: bool) -> NoiseTexture2D:
	var noise := FastNoiseLite.new()
	noise.seed = 0x0D06
	noise.noise_type = FastNoiseLite.TYPE_PERLIN
	noise.frequency = 0.09
	noise.fractal_octaves = 4
	var nt := NoiseTexture2D.new()
	nt.width = 256
	nt.height = 256
	nt.seamless = false
	nt.noise = noise
	if as_normal:
		nt.as_normal_map = true
		nt.bump_strength = 6.0
	else:
		# Резкий тёмный край — это и есть трещина: рампа с уступом, а не плавный
		# градиент, иначе выходит та же серая муть.
		var ramp := Gradient.new()
		ramp.offsets = PackedFloat32Array([0.0, 0.16, 0.30, 1.0])
		ramp.colors = PackedColorArray([
			Color(0.34, 0.34, 0.34), Color(0.62, 0.62, 0.62),
			Color(1.00, 1.00, 1.00), Color(1.00, 1.00, 1.00)])
		nt.color_ramp = ramp
	return nt


## flat_material — материал плоского слоя пути.
##
## Проверка глубины выключена по причине из шапки, а запись глубины — заодно:
## слои лежат в одной плоскости (все на отметке оси), и запись без проверки
## рвала бы их на куски по порядку отрисовки.
## ПРОВЕРКА ГЛУБИНЫ БОЛЬШЕ НЕ ОТКЛЮЧАЕТСЯ, и это отмена лечения, а не новая
## настройка. Отключали её потому, что путь и земля лежали на ОДНОЙ отметке и
## дрались за z-буфер: сервер сажал землю на отметку оси. С 2026-08-12 земля
## лежит на formation_to_rail_top ниже (на затравке 0.68 м), драться не с чем.
##
## Оставленное отключение стало вредным ровно тогда, когда появился вид с оси:
## галочки крестовин и нити рисовались ПОВЕРХ ВСЕГО и висели сквозь рельеф
## красными полосами на горизонте. Плоскому виду сверху это было незаметно.
##
## render_priority сохранён: он разводит слои, лежащие в одной плоскости, а
## таких ещё хватает — нити и галочки толщины не имеют ни в каком смысле.
static func flat_material(colour: Color, priority: int, unshaded: bool = false) -> StandardMaterial3D:
	var m := StandardMaterial3D.new()
	m.albedo_color = colour
	m.cull_mode = BaseMaterial3D.CULL_DISABLED
	m.render_priority = priority
	if unshaded:
		m.shading_mode = BaseMaterial3D.SHADING_MODE_UNSHADED
	return m
