## TrackBuild — рецепты сервера в РАЗМЕЩЁННЫЕ вещи. Ни одного меша.
##
## Зачем отдельным слоем, а не внутри рисующего кода: раскладку шпал по рецепту
## проверяет `tools/check.gd` — без окна, числами. Если бы она жила в функции,
## строящей ArrayMesh, единственным способом её проверить остался бы снимок
## экрана, а снимком не проверить, что шпал ровно 234, а не 233.
##
## ЗАКОН ТОТ ЖЕ: ни одного числа о мире здесь нет. Всё, что возвращается, —
## следствие присланных `construction_runs`, `track_types`, `structures` и
## `features`. Нет цепочки до типа — нет и вещи: пустой список, а не подстановка
## от соседа.
##
## ВЕРТИКАЛЬ ПРИЕХАЛА 2026-08-12, и вместе с ней — то, чего не хватало, чтобы её
## применить: `z` объявлен ПОВЕРХНОСТЬЮ КАТАНИЯ (контракт отрисовки, редакция 6,
## §2). Отсюда единственное правило, по которому здесь считается всё вертикальное:
##
##     от `z` откладывают ВНИЗ, и первым идёт рельс.
##
##     верх головки        z
##     подошва рельса      z − rail.height
##     верх шпалы          то же
##     низ шпалы           z − rail.height − sleeper.height
##     верх призмы         низ шпалы + ballast.crib_depth
##     основная площадка   z − formation_to_rail_top
##
## `formation_to_rail_top` берётся ПРИСЛАННЫМ, а не складывается здесь из трёх
## слагаемых: сервер считает ту же сумму для земляных работ, и два независимых
## сложения одного числа разойдутся округлением — рельеф встанет не там, где низ
## призмы.
class_name TrackBuild
extends RefCounted

## Допуск полуоткрытого правила `phase + n·pitch ∈ [0, run_length)`.
##
## Он нужен именно потому, что правило полуоткрытое: у RUN_SIDING длина 60 м при
## шаге 0.6, и последняя станция попадает ровно на конец. В double `0.6 · 100`
## даёт 60.000000000000014, то есть «случайно снаружи»; при другом порядке
## сложения было бы «случайно внутри» — и число шпал зависело бы от машины.
## Проект это уже проходил (расхождение ~1e-13 в эталоне контракта), поэтому
## граница сравнивается с допуском, а не байт в байт.
const STATION_EPS := 1e-9


## Шпала: поза центра и три размера из типа пути.
class Sleeper:
	var pose: TrackGeom.AxisPoint
	## Поперёк пути (`sleeper.length`) и вдоль (`sleeper.width`) — именно так их
	## называет контракт §3, и перепутать их значит развернуть решётку.
	var length_m: float
	var width_m: float
	## Высота шпалы. −1 значит «не прислана» — тогда шпала рисуется плоской, а не
	## коробкой выдуманной толщины.
	var height_m: float = -1.0
	## Высота рельса: шпала лежит НЕ на отметке оси, а на высоту рельса ниже.
	var rail_height_m: float = -1.0
	var run_id: String
	var element_id: String

	## top_z / bottom_z — верх и низ шпалы. Оба отсчитаны вниз от поверхности
	## катания, потому что именно ею объявлен `z`.
	func top_z() -> float:
		return pose.z - maxf(rail_height_m, 0.0)

	func bottom_z() -> float:
		return top_z() - maxf(height_m, 0.0)


## Покрытый участок: кусок элемента, за который отвечает один строительный
## прогон, вместе с уже посчитанной осью.
##
## Участок, а не элемент целиком: спан прогона вправе покрывать часть элемента, и
## лента во всю длину элемента была бы отсыпкой там, где её не объявляли. На
## затравке ST_A спаны совпадают с элементами, и разницы не видно — тем важнее
## не закладывать в код совпадение, которое завтра исчезнет.
class Span:
	var element_id: String
	var run_id: String
	var type_id: String
	## −1 значит «не прислано». Ноль значил бы «прислан ноль», а это разное.
	##
	## Различие не педантизм: ballast.crib_depth = 0 — законное значение (призма
	## вровень с постелью шпалы), и спутать его с «не прислали» значило бы
	## нарисовать призму там, где её высоту не объявляли.
	var gauge_m: float = -1.0
	var ballast_half_width_m: float = -1.0
	var rail_height_m: float = -1.0
	var rail_head_width_m: float = -1.0
	var sleeper_height_m: float = -1.0
	var ballast_depth_m: float = -1.0
	var ballast_crib_depth_m: float = -1.0
	var ballast_side_slope: float = -1.0
	var formation_to_rail_top_m: float = -1.0
	## Сечение рельса: точки (x, y) в осях сечения — x поперёк пути от
	## ВНУТРЕННЕЙ РАБОЧЕЙ ГРАНИ наружу, y от поверхности катания вниз. Пусто
	## значит «сечения не прислали», и тогда рельс рисуется прежним ОБЪЯВЛЕННЫМ
	## упрощением — прямоугольником head_width × height. Своего профиля клиент не
	## подставляет: выдумать сечение — ровно то, что контракт запрещает.
	var rail_section: PackedVector2Array = PackedVector2Array()
	## Покрыт ли участок строительным прогоном. У прохода стрелки false: тип у
	## него есть (role.type), а run'а нет и быть не может — решётка устройства
	## нерегулярна (контракт §6). Отсюда: призма и нитки на ветви рисуются, шпалы
	## нет, и это состояние данных, а не недоделка клиента.
	var from_run: bool = true
	var axis: Array[TrackGeom.AxisPoint] = []

	## has_prism — хватает ли присланного, чтобы построить призму телом.
	##
	## Все пять чисел разом, а не «сколько есть»: призма из четырёх чисел с
	## подставленным пятым — выдумка, отличающаяся от честной только тем, что её
	## не видно.
	func has_prism() -> bool:
		return (ballast_half_width_m > 0.0 and ballast_depth_m >= 0.0
			and ballast_crib_depth_m >= 0.0 and ballast_side_slope > 0.0
			and formation_to_rail_top_m > 0.0)

	## has_rail_body — хватает ли на рельс телом.
	func has_rail_body() -> bool:
		return gauge_m > 0.0 and rail_height_m > 0.0 and rail_head_width_m > 0.0

	## has_rail_section — прислан ли профиль. Три точки — нижняя граница здравого
	## смысла показа; настоящую границу (восемь) держит валидатор карты, и
	## повторять её здесь значило бы завести второе правило о том, что такое
	## сечение.
	func has_rail_section() -> bool:
		return has_rail_body() and rail_section.size() >= 3

	## threads — ровно две нитки на ±gauge/2 в плановых (x, y, z отметки оси).
	##
	## Нитки СИМВОЛИЧЕСКИЕ — так предписано render-contract §3: они не
	## претендуют на совпадение с геометрическими осями физических головок,
	## потому что ширины головки в контракте нет («потребителя нет»). Пустой
	## список значит, что колею не прислали, и рисовать нечего.
	func threads() -> Array[PackedVector3Array]:
		var out: Array[PackedVector3Array] = []
		if gauge_m <= 0.0 or axis.size() < 2:
			return out
		out.append(TrackBuild.offset_line(axis, gauge_m * 0.5))
		out.append(TrackBuild.offset_line(axis, -gauge_m * 0.5))
		return out


## Полоса платформы вдоль одного спана.
class PlatformStrip:
	var id: String
	## Читаемая метка сооружения из провода: игроку показывают её, а не UUID.
	var label: String
	var element_id: String
	var side: String
	var offset_m: float
	var width_m: float
	## Верх плиты НАД ПОВЕРХНОСТЬЮ КАТАНИЯ и толщина плиты. −1 значит «не
	## прислано» — тогда платформа остаётся полосой на отметке оси.
	var height_m: float = -1.0
	var slab_thickness_m: float = -1.0
	## Кромки: near — та, что у пути (на `offset`), far — дальняя
	## (на `offset + width`). z у обеих — отметка ВЕРХА ПЛИТЫ, если высоту
	## прислали, иначе отметка оси.
	var near: PackedVector3Array = PackedVector3Array()
	var far: PackedVector3Array = PackedVector3Array()

	func has_slab() -> bool:
		return height_m > 0.0 and slab_thickness_m > 0.0


## Тупиковый упор: точка на оси и габарит.
##
## Появился в проводе 2026-08-12. До того упоров не отдавали вовсе, и снесённый
## спайк выводил их ИЗ ТОПОЛОГИИ сам — то есть рисовал то, чего ему не присылали.
## Этому клиенту такое запрещено, поэтому до появления `structures` вида
## `buffer_stop` тупики просто обрывались ничем.
class BufferStop:
	var id: String
	var element_id: String
	var pose: TrackGeom.AxisPoint
	var height_m: float
	var width_m: float


## Крестовина: точка и обе касательные, всё присланное.
class Frog:
	var owner: String
	var mark: String
	var point: Vector3
	## По касательной на адрес, в порядке сервера: прямой проход, затем боковой.
	var tangents: Array[Vector2] = []


## Стрелка как ОДНО устройство, а не два независимых элемента.
##
## Пара ветвей с общим `role.turnout` — общая решётка и общий балласт (разбор
## §4.5). Сегодня ни решётки, ни балласта нет: ветви не покрыты ни одним run'ом,
## а типа устройства провод не несёт вовсе. Устройство всё равно собирается —
## чтобы нехватка была названа устройством, а не четырьмя безымянными нитями.
class Device:
	var id: String
	## Читаемая метка устройства из провода: игроку показывают её, а не UUID.
	var label: String
	var hand: String
	var mark: String
	var branches: Array[String] = []
	## Покрыт ли хоть один проход строительным прогоном. false значит: ни колеи,
	## ни шпал, ни ширины — и рисовать их не от чего.
	var typed: bool = false


## Переводной механизм стрелки: где он стоит, какой он и что написано на его
## табличке.
##
## КЛИЕНТ ЗДЕСЬ НЕ ВЫВОДИТ НИЧЕГО, кроме позы из присланного адреса — ровно как у
## бруса. Сторону (знак выноса), место вдоль устройства и вид механизма посчитал
## и назвал сервер (track/drive.go): два клиента, каждый выводящий сторону из
## рукости, дали бы два разных ответа на первой же карте, где рукость и геометрия
## разошлись.
##
## РАЗМЕРОВ ТЕЛА В ПРОВОДЕ НЕТ, и это не дыра контракта: пока привод — предмет,
## который клиент РИСУЕТ, а не ассет, который он показывает, его габариты
## принадлежат клиенту (контракт отрисовки §1, та же строка, что у длины крыла
## крестовины). Числа названы списком в switch_stand.gd.
class TurnoutDrive:
	## Длина тяги в двух положениях стрелки, метры. Ноль — тяги нет.
	var reach_straight_m: float = 0.0
	var reach_diverging_m: float = 0.0
	var owner: String
	## Метка стрелки: ровно то, что написано на табличке.
	var label: String
	## "manual" — ручной перевод с балансиром, "electric" — электропривод.
	var drive: String
	var element_id: String
	## Поза станины: уже СМЕЩЁННАЯ на присланный вынос по левой нормали оси.
	var pose: TrackGeom.AxisPoint
	## Вынос от оси, метры со знаком. Держится отдельно от позы: по знаку
	## поворачивают привод лицом к пути, а из позы его уже не достать.
	var offset_m: float
	## На сколько подошва привода ниже головки рельса, метры.
	##
	## ПРИВОД СТОИТ НА ПЕРЕВОДНЫХ БРУСЬЯХ, А НЕ НА ЗЕМЛЕ, и это устройство
	## настоящего перевода: станина крепится к брусьям, продолженным за габарит
	## пути, — потому она и оказывается рядом с путём, а не в поле. Значит подошва
	## лежит на верхе бруса, то есть на высоту рельса ниже головки: та же отметка,
	## по которой клиент кладёт шпалу (TrackBuild.Sleeper.top_z).
	##
	## Землю здесь спрашивать НЕЛЬЗЯ ни в каком виде: у клиента нет ответа «какая
	## отметка под этой точкой» — рельеф приходит полем высот, — а посадка по
	## formation_to_rail_top (первая редакция этого поля) закапывала привод, когда
	## земля рядом с путём оказывалась выше основной площадки. Это было видно на
	## снимке: над травой торчали указатель и табличка, а станины не было.
	##
	## Ноль значит «типа нет» — тогда привод честно встаёт на отметку оси.
	var base_drop_m: float


static func types_by_id(network: Dictionary) -> Dictionary:
	var out := {}
	for t_raw in (network.get("track_types", []) as Array):
		var t: Dictionary = t_raw as Dictionary
		out[String(t.get("id", ""))] = t
	return out


static func elements_by_id(elements: Array[TrackGeom.Element]) -> Dictionary:
	var out := {}
	for el in elements:
		out[el.id] = el
	return out


## spans_of_run — спаны прогона с накопленной координатой r₀ начала каждого.
##
## Накопленная координата run'а `r`, а не локальное `u`, — требование
## render-contract §4: локальное `u` начинается заново на каждом элементе, и
## фаза, перезапущенная на стыке, переставила бы шпалы при смене внутренней
## нарезки карты, хотя путь не менялся.
static func spans_of_run(run: Dictionary) -> Array:
	var out: Array = []
	var r0 := 0.0
	for s_raw in (run.get("spans", []) as Array):
		var s: Dictionary = s_raw as Dictionary
		var from_u := float(s.get("from", 0.0))
		var to_u := float(s.get("to", 0.0))
		var span_len := absf(to_u - from_u)
		out.append({
			"element": String(s.get("element", "")),
			"from": from_u,
			"to": to_u,
			"direction": String(s.get("direction", "forward")),
			"r0": r0,
			"length": span_len,
		})
		r0 += span_len
	return out


## station_count — сколько станций даёт полуоткрытое правило.
##
## Отдельной функцией, потому что это ровно то число, которое проверяет
## check.gd: шпал обязано быть столько же, сколько тут насчитано, иначе раскладка
## понята не так, как писан рецепт.
static func station_count(run_length: float, phase: float, pitch: float) -> int:
	if pitch <= 0.0 or run_length <= 0.0:
		return 0
	return maxi(0, int(ceil((run_length - phase) / pitch - STATION_EPS)))


## sleepers — решётка по всем прогонам.
##
## Возвращает {"list": Array[Sleeper], "runs": Array[Dictionary], "skipped": …}.
## «Пропущено» — не мелочь отчётности: прогон, у которого не нашлось типа или у
## типа нет шага, обязан быть НАЗВАН, а не молча дать пустую решётку, неотличимую
## от исправной.
static func sleepers(network: Dictionary, by_id: Dictionary) -> Dictionary:
	var types := types_by_id(network)
	var list: Array[Sleeper] = []
	var runs: Array = []
	var skipped: Array[String] = []

	for r_raw in (network.get("construction_runs", []) as Array):
		var run: Dictionary = r_raw as Dictionary
		var run_id := String(run.get("id", ""))
		var type_id := String(run.get("type", ""))
		if not types.has(type_id):
			skipped.append("%s: тип «%s» в track_types не пришёл" % [run_id, type_id])
			continue
		# Координата размещения объявлена явно и обязана быть `u`: по `s` клиент
		# разложить не сможет — цепочки вертикального профиля в контракте нет
		# (render-contract §4).
		var coordinate := String(run.get("coordinate", ""))
		if coordinate != "u":
			skipped.append("%s: координата размещения «%s», а не «u»" % [run_id, coordinate])
			continue
		var sleeper: Dictionary = (types[type_id] as Dictionary).get("sleeper", {}) as Dictionary
		if not (sleeper.has("pitch") and sleeper.has("length") and sleeper.has("width")):
			skipped.append("%s: у типа %s нет pitch/length/width — шаг решётки неизвестен" % [run_id, type_id])
			continue
		var pitch := float(sleeper["pitch"])
		var s_len := float(sleeper["length"])
		var s_wid := float(sleeper["width"])
		# Высота шпалы и высота рельса — отдельно от трёх обязательных: без них
		# шпала рисуется плоской, но раскладка от этого не страдает, и терять
		# из-за них всю решётку было бы хуже, чем нарисовать её без толщины.
		var s_hgt := float(sleeper.get("height", -1.0))
		var rail: Dictionary = (types[type_id] as Dictionary).get("rail", {}) as Dictionary
		var r_hgt := float(rail.get("height", -1.0))
		var phase := float(run.get("phase", 0.0))

		var spans := spans_of_run(run)
		var run_length := 0.0
		for sp in spans:
			run_length += float(sp["length"])

		var count := station_count(run_length, phase, pitch)
		var placed := 0
		var lost := 0
		for n in count:
			var r := phase + float(n) * pitch
			var addr := address_at(spans, r)
			if addr.is_empty():
				lost += 1
				continue
			var eid := String(addr["element"])
			if not by_id.has(eid):
				lost += 1
				continue
			var el: TrackGeom.Element = by_id[eid]
			var s := Sleeper.new()
			s.pose = el.pose_at(float(addr["u"]))
			s.length_m = s_len
			s.width_m = s_wid
			s.height_m = s_hgt
			s.rail_height_m = r_hgt
			s.run_id = run_id
			s.element_id = eid
			list.append(s)
			placed += 1
		if lost > 0:
			skipped.append("%s: %d станций не легли ни на один присланный элемент" % [run_id, lost])
		runs.append({
			"id": run_id, "type": type_id, "run_length": run_length,
			"phase": phase, "pitch": pitch, "count": count, "placed": placed,
		})

	return {"list": list, "runs": runs, "skipped": skipped}


## timbers — РЕШЁТКА УСТРОЙСТВ: переводные брусья стрелок.
##
## Возвращает то же, что sleepers: {"list": Array[Sleeper], "grids": Array,
## "skipped": …}. Один тип на выходе не для экономии — брус И ЕСТЬ шпала для
## всего, что ниже: он так же лежит поперёк, так же на высоту рельса ниже оси и
## так же рисуется коробкой. Различаются они ровно двумя вещами, и обе приезжают
## числом: длина СВОЯ у каждого бруса и центр смещён с оси.
##
## КЛИЕНТ НЕ ВЫВОДИТ ЗДЕСЬ НИЧЕГО. Ни длины, ни числа брусьев, ни того, где
## кончается комплект, — всё это посчитал сервер (track/timbers.go), потому что
## два клиента, независимо выводящие решётку из топологии, дадут два разных
## ответа. Снесённый спайк выводил её сам, и это записано дырой Д8 его разбора.
##
## ОПОРНАЯ ЛИНИЯ ОДНА НА УСТРОЙСТВО — осевая ПРЯМОГО прохода, её называет поле
## element. Отсюда и берётся то, ради чего решётка отдана устройству: брусья
## лежат поперёк прямого пути, а не поперёк своей ветви, и под боковым путём
## тоже. Ставь их прогоном по ветви — и они развернулись бы вслед за ней.
static func timbers(network: Dictionary, by_id: Dictionary) -> Dictionary:
	var types := types_by_id(network)
	var list: Array[Sleeper] = []
	var grids: Array = []
	var skipped: Array[String] = []

	for g_raw in (network.get("turnout_grids", []) as Array):
		var grid: Dictionary = g_raw as Dictionary
		var owner_id := String(grid.get("owner", ""))
		var element_id := String(grid.get("element", ""))
		if not by_id.has(element_id):
			skipped.append("решётка %s: элемент %s не пришёл" % [owner_id, element_id])
			continue
		var type_id := String(grid.get("type", ""))
		if not types.has(type_id):
			skipped.append("решётка %s: тип «%s» в track_types не пришёл" % [owner_id, type_id])
			continue
		# Высота рельса — из ТИПА УСТРОЙСТВА, а не из типа примыкающего пути:
		# брус лежит в вертикальном стеке своего перевода, и стрелка законно
		# соединяет пути разных типов (render-contract §4).
		var rail: Dictionary = (types[type_id] as Dictionary).get("rail", {}) as Dictionary
		var r_hgt := float(rail.get("height", -1.0))
		var t_wid := float(grid.get("width", -1.0))
		var t_hgt := float(grid.get("height", -1.0))
		if t_wid <= 0.0:
			skipped.append("решётка %s: не прислана ширина бруса" % owner_id)
			continue

		var el: TrackGeom.Element = by_id[element_id]
		var placed := 0
		var lost := 0
		var l_min := INF
		var l_max := -INF
		for t_raw in (grid.get("timbers", []) as Array):
			var timber: Dictionary = t_raw as Dictionary
			var t_len := float(timber.get("length", -1.0))
			if t_len <= 0.0:
				lost += 1
				continue
			var p := el.pose_at(float(timber.get("u", 0.0)))
			# СМЕЩЕНИЕ ЦЕНТРА — по ЛЕВОЙ нормали, той же, которой сервер его считал
			# и которой ориентирована шпала (§4). Сдвигается поза, а не меш: коробка
			# строится вокруг точки позы, и другого места для сдвига нет.
			var off := float(timber.get("offset", 0.0))
			var n := p.left()
			var s := Sleeper.new()
			s.pose = TrackGeom.AxisPoint.new(
				p.x + n.x * off, p.y + n.y * off, p.z, p.heading, p.u)
			s.length_m = t_len
			s.width_m = t_wid
			s.height_m = t_hgt
			s.rail_height_m = r_hgt
			s.run_id = owner_id
			s.element_id = element_id
			list.append(s)
			placed += 1
			l_min = minf(l_min, t_len)
			l_max = maxf(l_max, t_len)
		if lost > 0:
			skipped.append("решётка %s: %d брусьев без длины" % [owner_id, lost])
		grids.append({
			"owner": owner_id, "element": element_id, "type": type_id,
			"placed": placed, "length_min": l_min, "length_max": l_max,
		})

	return {"list": list, "grids": grids, "skipped": skipped}


## address_at — накопленная координата прогона в (элемент, u).
##
## Правило отображения записано в render-contract §4 дословно:
##   forward:  u = from + (r − r₀)
##   reverse:  u = to   − (r − r₀)
static func address_at(spans: Array, r: float) -> Dictionary:
	for sp_raw in spans:
		var sp: Dictionary = sp_raw
		var r0 := float(sp["r0"])
		var span_len := float(sp["length"])
		if r < r0 - STATION_EPS or r >= r0 + span_len - STATION_EPS:
			continue
		var t := r - r0
		var u := float(sp["from"]) + t
		if String(sp["direction"]) == "reverse":
			u = float(sp["to"]) - t
		return {"element": String(sp["element"]), "u": u}
	return {}


## covered_spans — все участки, покрытые прогонами, с осью и размерами типа.
##
## Колея и ширина отсыпки берутся ТОЛЬКО по цепочке run → type. Участка, до
## которого цепочка не дошла, здесь нет вовсе: ветви стрелок на затравке ST_A не
## покрыты ни одним прогоном, и оттого не получают ни колеи, ни решётки, ни
## ленты. Это состояние данных, и оно должно быть видно, а не залатано размерами
## соседа.
static func covered_spans(network: Dictionary, by_id: Dictionary, max_seg_m: float, max_ang_rad: float) -> Array[Span]:
	var types := types_by_id(network)
	var out: Array[Span] = []
	for r_raw in (network.get("construction_runs", []) as Array):
		var run: Dictionary = r_raw as Dictionary
		var type_id := String(run.get("type", ""))
		if not types.has(type_id):
			continue
		var t: Dictionary = types[type_id]
		var ballast: Dictionary = t.get("ballast", {}) as Dictionary
		for sp_raw in spans_of_run(run):
			var sp: Dictionary = sp_raw
			var eid := String(sp["element"])
			if not by_id.has(eid):
				continue
			var el: TrackGeom.Element = by_id[eid]
			var u0 := minf(float(sp["from"]), float(sp["to"]))
			var u1 := maxf(float(sp["from"]), float(sp["to"]))
			var axis := el.sample_range(u0, u1, max_seg_m, max_ang_rad)
			if axis.size() < 2:
				continue
			var span := Span.new()
			span.element_id = eid
			span.run_id = String(run.get("id", ""))
			span.type_id = type_id
			span.axis = axis
			_fill_type(span, t)
			out.append(span)

	# Проходы стрелок. Run'ами они не покрываются по правилу — решётка
	# устройства нерегулярна, — но ТИП у них есть с 2026-08-12: он приезжает в
	# role.type (контракт §6). До того у ветвей не было ни одного размера, и они
	# рисовались ниткой; теперь у них есть призма и колея, а шпал по-прежнему
	# нет, потому что рецепта решётки устройства в контракте нет.
	for eid in by_id:
		var el: TrackGeom.Element = by_id[eid]
		if el.role.is_empty():
			continue
		var dev_type := String(el.role.get("type", ""))
		if dev_type == "" or not types.has(dev_type):
			continue
		var axis_all := el.sample_range(0.0, el.length_m, max_seg_m, max_ang_rad)
		if axis_all.size() < 2:
			continue
		var dspan := Span.new()
		dspan.element_id = eid
		dspan.run_id = ""
		dspan.type_id = dev_type
		dspan.from_run = false
		dspan.axis = axis_all
		_fill_type(dspan, types[dev_type] as Dictionary)
		# ПРОХОД — МАРШРУТ, НЕ ПАРА ФИЗИЧЕСКИХ РЕЛЬСОВ, и с 2026-08-17 это верно
		# для всякого участка: рельсы не порождает ни один из них, все приезжают
		# деталями в rails. Участок остался ради призмы, шпал и адресации.
		out.append(dspan)
	return out


## _fill_type — перенос чисел типа в участок. Одной функцией на оба источника
## (run и устройство): два места, читающие один тип, разошлись бы в том, какое
## поле считать обязательным.
static func _fill_type(span: Span, t: Dictionary) -> void:
	var ballast: Dictionary = t.get("ballast", {}) as Dictionary
	var rail: Dictionary = t.get("rail", {}) as Dictionary
	var sleeper: Dictionary = t.get("sleeper", {}) as Dictionary
	if t.has("gauge"):
		span.gauge_m = float(t["gauge"])
	if ballast.has("half_width"):
		span.ballast_half_width_m = float(ballast["half_width"])
	if ballast.has("depth"):
		span.ballast_depth_m = float(ballast["depth"])
	if ballast.has("crib_depth"):
		span.ballast_crib_depth_m = float(ballast["crib_depth"])
	if ballast.has("side_slope"):
		span.ballast_side_slope = float(ballast["side_slope"])
	if rail.has("height"):
		span.rail_height_m = float(rail["height"])
	if rail.has("head_width"):
		span.rail_head_width_m = float(rail["head_width"])
	# СЕЧЕНИЕ БЕРЁТСЯ КАК ПРИСЛАНО, без единой правки: ни замыкания контура, ни
	# разворота обхода. Контур замкнут по построению (первая точка следует за
	# последней), направление обхода объявлено контрактом и проверено
	# валидатором карты — «починить» их здесь значило бы завести на клиенте
	# второе мнение о том, где у рельса лицо.
	span.rail_section = PackedVector2Array()
	for pt_raw in (rail.get("section", []) as Array):
		var pt := pt_raw as Array
		if pt == null or pt.size() < 2:
			continue
		span.rail_section.append(Vector2(float(pt[0]), float(pt[1])))
	if sleeper.has("height"):
		span.sleeper_height_m = float(sleeper["height"])
	# Сумма берётся ПРИСЛАННОЙ, а не складывается из трёх слагаемых: ту же сумму
	# считает сервер для земляных работ, и два сложения разойдутся округлением.
	if t.has("formation_to_rail_top"):
		span.formation_to_rail_top_m = float(t["formation_to_rail_top"])


## offset_line — полилиния, отложенная по левой нормали на d (со знаком) и
## поднятая на dz над отметкой оси.
##
## Подъём отдельным параметром, а не правкой z у вызывающего: смещение поперёк и
## смещение по высоте — разные вещи, и складывать их в одном числе значило бы
## завести координату, у которой два смысла.
static func offset_line(axis: Array[TrackGeom.AxisPoint], d: float, dz: float = 0.0) -> PackedVector3Array:
	var out := PackedVector3Array()
	out.resize(axis.size())
	for k in axis.size():
		var p: TrackGeom.AxisPoint = axis[k]
		var n := p.left()
		out[k] = Vector3(p.x + n.x * d, p.y + n.y * d, p.z + dz)
	return out


## platforms — полосы из `structures`.
##
## Поле называется `structures`, и это не мелочь: до 3637504 оно звалось
## `trackside`, клиент читал старое имя и писал в HUD «получено и НЕ рисуется:
## trackside 0» — враньё дважды, потому что получено было не ноль и не рисовалось
## по другой причине. Имя сверено с ответом ручки, а не с памятью.
static func platforms(network: Dictionary, by_id: Dictionary, max_seg_m: float, max_ang_rad: float) -> Dictionary:
	var out: Array[PlatformStrip] = []
	var skipped: Array[String] = []
	for s_raw in (network.get("structures", []) as Array):
		var st: Dictionary = s_raw as Dictionary
		var sid := String(st.get("id", ""))
		var kind := String(st.get("kind", ""))
		if kind == "buffer_stop":
			# Упоры собираются отдельной функцией: у них точечный спан и габарит,
			# а не две кромки вдоль. Здесь они пропускаются молча — молча именно
			# потому, что их НЕ пропустили, а обработали в другом месте.
			continue
		if kind != "platform":
			# Неизвестный вид сооружения НЕ рисуется приближением: его форма не
			# описана, и полоса «на всякий случай» была бы выдумкой.
			skipped.append("%s: вид «%s» этот клиент рисовать не умеет" % [sid, kind])
			continue
		if not (st.has("side") and st.has("offset") and st.has("width")):
			skipped.append("%s: нет side/offset/width — где кромки, неизвестно" % sid)
			continue
		var side := String(st["side"])
		if side != "left" and side != "right":
			skipped.append("%s: сторона «%s» не left и не right" % [sid, side])
			continue
		var offset := float(st["offset"])
		var width := float(st["width"])
		# Сторона берётся из данных, а не из знака чего-нибудь: левая нормаль —
		# «left», ей противоположная — «right».
		var sgn := 1.0 if side == "left" else -1.0
		for sp_raw in (st.get("spans", []) as Array):
			var sp: Dictionary = sp_raw as Dictionary
			var eid := String(sp.get("element", ""))
			if not by_id.has(eid):
				skipped.append("%s: спан ссылается на элемент %s, которого в сети нет" % [sid, eid])
				continue
			var el: TrackGeom.Element = by_id[eid]
			var u0 := minf(float(sp.get("from", 0.0)), float(sp.get("to", 0.0)))
			var u1 := maxf(float(sp.get("from", 0.0)), float(sp.get("to", 0.0)))
			var axis := el.sample_range(u0, u1, max_seg_m, max_ang_rad)
			if axis.size() < 2:
				continue
			var strip := PlatformStrip.new()
			strip.id = sid
			strip.label = String(st.get("name", sid))
			strip.element_id = eid
			strip.side = side
			strip.offset_m = offset
			strip.width_m = width
			# Высота верха плиты — НАД ПОВЕРХНОСТЬЮ КАТАНИЯ, поэтому прибавляется
			# к отметке оси, а не вычитается. Единственное число вертикали,
			# которое откладывают вверх: платформа выше рельса, всё остальное
			# ниже.
			strip.height_m = float(st.get("height", -1.0))
			strip.slab_thickness_m = float(st.get("slab_thickness", -1.0))
			var lift := strip.height_m if strip.height_m > 0.0 else 0.0
			strip.near = offset_line(axis, sgn * offset, lift)
			strip.far = offset_line(axis, sgn * (offset + width), lift)
			out.append(strip)
	return {"list": out, "skipped": skipped}


## buffer_stops — упоры из `structures`.
##
## Спан упора ТОЧЕЧНЫЙ (from == to) — это проверяет валидатор сервера, и клиент
## на проверку опирается, а не повторяет её: поза берётся в точке `from`.
static func buffer_stops(network: Dictionary, by_id: Dictionary) -> Dictionary:
	var out: Array[BufferStop] = []
	var skipped: Array[String] = []
	for s_raw in (network.get("structures", []) as Array):
		var st: Dictionary = s_raw as Dictionary
		if String(st.get("kind", "")) != "buffer_stop":
			continue
		var sid := String(st.get("id", ""))
		if not (st.has("height") and st.has("width")):
			skipped.append("%s: нет height/width — габарита упора не прислали" % sid)
			continue
		var spans: Array = st.get("spans", []) as Array
		if spans.is_empty():
			skipped.append("%s: спан пуст — где упор, неизвестно" % sid)
			continue
		var sp: Dictionary = spans[0] as Dictionary
		var eid := String(sp.get("element", ""))
		if not by_id.has(eid):
			skipped.append("%s: спан ссылается на элемент %s, которого в сети нет" % [sid, eid])
			continue
		var el: TrackGeom.Element = by_id[eid]
		var bs := BufferStop.new()
		bs.id = sid
		bs.element_id = eid
		bs.pose = el.pose_at(float(sp.get("from", 0.0)))
		bs.height_m = float(st["height"])
		bs.width_m = float(st["width"])
		out.append(bs)
	return {"list": out, "skipped": skipped}


## frogs — крестовины из `features`.
##
## Сервер даёт точку пересечения ниток и ОБА адреса с касательными; своего
## `heading` у особенности нет и быть не может — направлений два
## (render-contract §5). Отметка z берётся у элемента адреса: у самой точки её
## не присылают, а придумывать её незачем — адрес указывает, где именно её взять.
static func frogs(network: Dictionary, by_id: Dictionary) -> Dictionary:
	var out: Array[Frog] = []
	var skipped: Array[String] = []
	for f_raw in (network.get("features", []) as Array):
		var f: Dictionary = f_raw as Dictionary
		var owner := String(f.get("owner", ""))
		var kind := String(f.get("kind", ""))
		if kind != "frog":
			skipped.append("%s: особенность «%s» этот клиент рисовать не умеет" % [owner, kind])
			continue
		var pt: Dictionary = f.get("point", {}) as Dictionary
		if not (pt.has("x") and pt.has("y")):
			skipped.append("%s: у особенности нет точки" % owner)
			continue
		var addrs: Array = f.get("addresses", []) as Array
		if addrs.size() < 2:
			skipped.append("%s: адресов %d, а крыла два" % [owner, addrs.size()])
			continue
		var frog := Frog.new()
		frog.owner = owner
		var z := 0.0
		var z_known := false
		for a_raw in addrs:
			var a: Dictionary = a_raw as Dictionary
			var tg: Dictionary = a.get("tangent", {}) as Dictionary
			frog.tangents.append(Vector2(float(tg.get("x", 0.0)), float(tg.get("y", 0.0))))
			var eid := String(a.get("element", ""))
			if not z_known and by_id.has(eid):
				var el: TrackGeom.Element = by_id[eid]
				z = el.pose_at(float(a.get("u", 0.0))).z
				z_known = true
				if el.role.has("frog"):
					frog.mark = String(el.role["frog"])
		if not z_known:
			skipped.append("%s: ни один адрес не указывает на присланный элемент — отметки нет" % owner)
			continue
		frog.point = Vector3(float(pt["x"]), float(pt["y"]), z)
		out.append(frog)
	return {"list": out, "skipped": skipped}


## drives — переводные механизмы стрелок из блока turnout_drives.
##
## Адресация та же, что у бруса: опорная линия — прямой проход, u вдоль неё,
## offset по её левой нормали. Отметка z берётся У ЭЛЕМЕНТА АДРЕСА и не
## присылается отдельно — так же, как у крестовины: адрес указывает, где именно
## её взять, и второй источник высоты одной точки заводить незачем.
static func drives(network: Dictionary, by_id: Dictionary) -> Dictionary:
	var out: Array[TurnoutDrive] = []
	var skipped: Array[String] = []
	var types := types_by_id(network)
	for d_raw in (network.get("turnout_drives", []) as Array):
		var d: Dictionary = d_raw as Dictionary
		var owner_id := String(d.get("owner", ""))
		var element_id := String(d.get("element", ""))
		if not by_id.has(element_id):
			skipped.append("привод %s: элемент %s не пришёл" % [owner_id, element_id])
			continue
		var kind := String(d.get("drive", ""))
		if kind == "":
			# Вид механизма НЕ ПОДСТАВЛЯЕТСЯ: «наверное, ручной» — это выдумка о
			# том, чем оборудована станция, и она была бы видна в кадре телом,
			# которого там нет.
			skipped.append("привод %s: вид механизма не прислан" % owner_id)
			continue
		var el: TrackGeom.Element = by_id[element_id]
		var p := el.pose_at(float(d.get("u", 0.0)))
		var off := float(d.get("offset", 0.0))
		var n := p.left()
		var drive := TurnoutDrive.new()
		drive.owner = owner_id
		drive.label = String(d.get("name", ""))
		drive.drive = kind
		drive.element_id = element_id
		drive.offset_m = off
		# ДЛИНА ПЕРЕВОДНОЙ ТЯГИ в каждом из двух положений. Два числа, а не одно
		# с поправкой: «в каком положении вычитать ход остряка» — знание о том,
		# с какой стороны стоит привод, то есть факт о станции (разбор — у
		# track.reach). Ноль означает «тяги нет».
		drive.reach_straight_m = float(d.get("reach_straight", 0.0))
		drive.reach_diverging_m = float(d.get("reach_diverging", 0.0))
		drive.pose = TrackGeom.AxisPoint.new(
			p.x + n.x * off, p.y + n.y * off, p.z, p.heading, p.u)
		# ТИП УСТРОЙСТВА БЕРЁТСЯ У РОЛИ ПРОХОДА, а не у прогона: проходы стрелок
		# прогонами не покрываются по правилу, и свой тип они несут в role.type
		# (контракт редакции 6 §6). Это же место, откуда его берёт балласт ветвей,
		# и второго правила «где у устройства тип» клиент не заводит.
		var dev_type := String(el.role.get("type", ""))
		if types.has(dev_type):
			var rail: Dictionary = (types[dev_type] as Dictionary).get("rail", {}) as Dictionary
			drive.base_drop_m = maxf(float(rail.get("height", 0.0)), 0.0)
		out.append(drive)
	return {"list": out, "skipped": skipped}


## devices — стрелки, собранные из role{turnout, branch, hand, frog}.
##
## `hand` позволяет НЕ выводить сторону из геометрии — а вывод из геометрии
## именно то, чем клиент занимался бы, не будь этого поля.
static func devices(elements: Array[TrackGeom.Element]) -> Array[Device]:
	var by_turnout := {}
	var order: Array[String] = []
	for el in elements:
		if el.role.is_empty():
			continue
		var tid := String(el.role.get("turnout", ""))
		if tid == "":
			continue
		if not by_turnout.has(tid):
			var d := Device.new()
			d.id = tid
			d.label = el.name if el.name != "" else tid
			d.hand = String(el.role.get("hand", ""))
			d.mark = String(el.role.get("frog", ""))
			by_turnout[tid] = d
			order.append(tid)
		var dev: Device = by_turnout[tid]
		dev.branches.append(el.id)
		if el.type_id != "":
			dev.typed = true
	var out: Array[Device] = []
	for tid in order:
		out.append(by_turnout[tid])
	return out


## Нитка крестовины: усовик или контррельс. Короткий отрезок вдоль прохода с
## постоянным выносом рабочей грани и отгибами на концах.
##
## Своим классом, а не Span'ом: у Span две нитки, шпалы, призма и колея, а здесь
## ОДНА нитка и ничего больше. Ужать её в Span значило бы завести у него поля,
## осмысленные у одного участка из десяти.
class FrogRail:
	var owner: String
	var element_id: String
	## КЛЮЧ ДЕТАЛИ В ПРОВОДЕ: «rail:3» — третья запись блока rails, «blade:1» —
	## вторая запись turnout_blades. Нужен отладочному выбору (port_debug.gd):
	## по нему щелчок находит тело детали, чтобы подсветить именно его.
	##
	## Позиция в списке, а не присланный id, потому что id есть НЕ У ВСЕХ: усовик,
	## контррельс и грань сердечника приезжают безымянными. Ключ обязан быть у
	## каждой детали, иначе выбрать можно было бы не всякую.
	var key: String = ""
	## Присланный идентификатор детали. Пуст у безымянных — показывается как есть.
	var id: String = ""
	## Части одного физического рельса могут лежать на разных элементах. Этот
	## ключ велит показу сшить их в одно протянутое сечение без внутреннего шва.
	var continuous_id: String = ""
	var continuous_order: int = 0
	## Ключи исходных записей: отладочный выбор любой половины подсвечивает весь
	## физический рельс после сшивки.
	var part_keys: Array[String] = []
	## wing | check. Показу безразличен, отчёту — нет: по нему видно, что
	## приехало обе пары, а не две одинаковые.
	var kind: String
	## Точки оси прохода в пределах [from, to] и вынос РАБОЧЕЙ ГРАНИ у каждой:
	## на отгибах он уезжает к раструбу. Считается один раз при разборе — показ
	## только откладывает.
	var axis: Array[TrackGeom.AxisPoint] = []
	var faces: PackedFloat64Array = PackedFloat64Array()
	## В какую сторону от рабочей грани растёт сечение: +1 или −1 по левой
	## нормали. Прислано сервером, клиент не выводит.
	var grow: float = 1.0
	## Числа рельса того же типа, что у прохода: нитка крестовины — тот же рельс.
	var rail_section: PackedVector2Array = PackedVector2Array()
	var rail_height_m: float = -1.0
	var rail_head_width_m: float = -1.0
	## ОСТРОЖКА: доля ширины сечения и понижение верха в каждой точке.
	##
	## Пусто значит «сечение полное» — так у усовика и контррельса, они рельсы как
	## рельсы. У остряка иначе: он острогается, и в острие от него остаётся тонкий
	## клин НИЖЕ головки рамного рельса, иначе он читается вторым полным рельсом,
	## приставленным вплотную к первому (слово владельца на кадре 2026-08-16).
	var widths: PackedFloat64Array = PackedFloat64Array()
	var sinks: PackedFloat64Array = PackedFloat64Array()
	## ШИРИНА НАКАТА в каждой точке, метры. Пусто — полоса во всю объявленную
	## долю головки; ноль в точке — наката там нет вовсе.
	##
	## Переменной она стала потому, что нагрузка передаётся ПОСТЕПЕННО: колесо
	## переходит на остряк и на сердечник не скачком, и блестящая полоса растёт с
	## нуля (модель CA-1/9-R65-v1: остряк 1.50…3.00 м, сердечник 0.40…0.80 м от
	## острия).
	var rides: PackedFloat64Array = PackedFloat64Array()

	## width_at / sink_at — острожка в точке. Полное сечение, если её не задавали.
	func width_at(k: int) -> float:
		return 1.0 if k >= widths.size() else widths[k]

	## Отрицательное понижение — ПОДЪЁМ над поверхностью катания: так приезжает
	## контррельс, который держит гребень, а не несёт колесо. Отдельного поля под
	## подъём нет нарочно: вопрос один — где верх детали относительно датума, — и
	## два поля на него отвечали бы порознь.
	func sink_at(k: int) -> float:
		return 0.0 if k >= sinks.size() else sinks[k]

	## ride_at — ширина наката в точке. Отрицательное значит «не задавали», и тогда
	## показ берёт свою объявленную долю головки.
	func ride_at(k: int) -> float:
		return -1.0 if k >= rides.size() else rides[k]

	func ready() -> bool:
		return axis.size() >= 2 and faces.size() == axis.size() and rail_height_m > 0.0


## FrogCore — ТЕЛО СЕРДЕЧНИКА, присланное станциями сечения.
##
## Форму отливки клиент больше не выводит ни в одной точке. До 2026-08-17 он брал
## ширину между граневыми линиями и по пяти своим константам строил четыре уровня
## по высоте — то есть решал, какой толщины отливка под головой и насколько шире
## её подошва. Теперь на проводе сечение целиком, а здесь остаётся выбрать его на
## нужном расстоянии от острия и протянуть.
class FrogCore:
	var owner: String
	var length: float = 0.0
	## Станции: расстояние от острия и сечение на нём. Порознь, а не парами,
	## потому что читаются в цикле по точкам оси, а не по станциям.
	var us := PackedFloat64Array()
	var sections: Array[PackedVector2Array] = []

	func ready() -> bool:
		return us.size() >= 2 and us.size() == sections.size()

	## section_at — сечение на расстоянии u от острия.
	##
	## Между станциями ЛИНЕЙНО, за крайними — постоянно: то же правило, что у
	## строжки остряка и у ходовой поверхности, и объявлено оно контрактом, а не
	## выбрано здесь. Число точек одинаково на всех станциях — это обещание
	## сервера, и без него шов нечем сшить.
	func section_at(u: float) -> PackedVector2Array:
		if not ready():
			return PackedVector2Array()
		if u <= us[0]:
			return sections[0]
		var last := us.size() - 1
		if u >= us[last]:
			return sections[last]
		var i := 0
		while i + 1 < last and u > us[i + 1]:
			i += 1
		var span := us[i + 1] - us[i]
		var t: float = 0.0 if span <= 0.0 else (u - us[i]) / span
		var lo: PackedVector2Array = sections[i]
		var hi: PackedVector2Array = sections[i + 1]
		var n := mini(lo.size(), hi.size())
		var out := PackedVector2Array()
		out.resize(n)
		for k in n:
			out[k] = lo[k].lerp(hi[k], t)
		return out


## frog_cores — тела сердечников из блока frog_cores: владелец -> FrogCore.
##
## Словарём по владельцу, а не списком: спрашивают его ровно там, где собрана
## пара граней одной стрелки.
static func frog_cores(network: Dictionary) -> Dictionary:
	var out := {}
	for c_raw in (network.get("frog_cores", []) as Array):
		var c: Dictionary = c_raw as Dictionary
		var core := FrogCore.new()
		core.owner = String(c.get("owner", ""))
		core.length = float(c.get("length", 0.0))
		for st_raw in (c.get("stations", []) as Array):
			var st: Dictionary = st_raw as Dictionary
			var pts := PackedVector2Array()
			for p_raw in (st.get("section", []) as Array):
				var p: Array = p_raw as Array
				if p.size() >= 2:
					pts.append(Vector2(float(p[0]), float(p[1])))
			if pts.size() < 3:
				continue
			core.us.append(float(st.get("u", 0.0)))
			core.sections.append(pts)
		if core.ready():
			out[core.owner] = core
	return out


## rails — ВСЕ РЕЛЬСЫ РЕГИОНА из блока rails: перегонные нитки и детали
## устройств.
##
## С 2026-08-17 путь построения ОДИН. До того рельсы перегона клиент выводил сам
## — брал ось участка и откладывал две линии на ±gauge/2, — а детали устройства
## приезжали готовыми. Два пути построения одного и того же расходились молча, и
## владелец закрыл вопрос: «все рельсы должны генериться процедурно на сервере».
##
## ВЫНОС СЧИТАЕТСЯ ЗДЕСЬ, А НЕ ПРИ ПОКАЗЕ. Новый контракт присылает станции
## плана со значением и производной; старый — прямой отгиб на длине flare. В
## обоих случаях это арифметика над присланным, а не решение клиента о форме.
static func rails(network: Dictionary, by_id: Dictionary,
		max_seg_m: float, max_ang_rad: float) -> Dictionary:
	var out: Array[FrogRail] = []
	var skipped: Array[String] = []
	var types := types_by_id(network)
	var wire := -1
	for r_raw in (network.get("rails", []) as Array):
		var r: Dictionary = r_raw as Dictionary
		wire += 1
		var eid := String(r.get("element", ""))
		if not by_id.has(eid):
			skipped.append("нитка %s: элемент %s не пришёл" % [r.get("kind", ""), eid])
			continue
		var el: TrackGeom.Element = by_id[eid]
		var from := float(r.get("from", 0.0))
		var to := float(r.get("to", 0.0))
		if to <= from:
			skipped.append("нитка %s на %s вырождена" % [r.get("kind", ""), eid])
			continue
		var flare_from := float(r.get("flare_from", 0.0))
		var flare_to := float(r.get("flare_to", 0.0))
		var plan: Array = r.get("plan", []) as Array
		# ТОЧКИ ИЗЛОМА ОБЯЗАНЫ БЫТЬ В ОСИ. Разбиение оси считает только длину и
		# кривизну — про отгиб оно не знает, и на прямом коротком отрезке отдаёт
		# ровно две точки: начало и конец. Вынос при этом линейно уезжает от
		# раструба к раструбу, и рабочей части не остаётся вовсе.
		#
		# Найдено проверкой, а не глазом: «с отгибом 0, без 8».
		# ШАГ СВОЙ, МЕЛКИЙ — по доводу остряка: понижение острия сердечника сходит
		# на нет за 0.8 м, и на пятиметровом шаге от него не осталось бы ничего.
		var breaks: Array = []
		if plan.size() < 2:
			breaks.assign([from + flare_from, to - flare_to])
		for st_raw in plan:
			var st: Dictionary = st_raw as Dictionary
			breaks.append(from + float(st.get("u", 0.0)))
		var axis := _with_breaks(el,
			el.sample_range(from, to, minf(max_seg_m, FROG_STEP_M), max_ang_rad),
			breaks)
		if axis.size() < 2:
			continue
		var rail := FrogRail.new()
		rail.owner = String(r.get("owner", ""))
		rail.element_id = eid
		rail.key = "rail:%d" % wire
		rail.id = String(r.get("id", ""))
		rail.continuous_id = String(r.get("continuous_id", ""))
		rail.continuous_order = int(r.get("continuous_order", 0))
		rail.part_keys = [rail.key]
		rail.kind = String(r.get("kind", ""))
		rail.axis = axis
		rail.grow = signf(float(r.get("grow", 1.0)))
		var face := float(r.get("face", 0.0))
		var end_face_from := float(r.get("end_face_from", face))
		var end_face_to := float(r.get("end_face_to", face))
		rail.faces = PackedFloat64Array()
		for p in axis:
			if plan.size() >= 2:
				rail.faces.append(plan_face(plan, p.u - from))
			else:
				rail.faces.append(_flared(face, end_face_from, end_face_to,
					flare_from, flare_to, p.u - from, to - from))
		# ТИП ПРИСЛАН ДЕТАЛЬЮ. Раньше он брался у роли прохода, и это работало,
		# пока деталями были только рельсы устройства: у перегонной нитки роли
		# нет вовсе, её тип знает прогон. Роль осталась запасным ответом для
		# карт, снятых до 2026-08-17.
		var dev_type := String(r.get("type", ""))
		if dev_type == "":
			dev_type = String(el.role.get("type", ""))
		if types.has(dev_type):
			_fill_rail_metal(rail, types[dev_type] as Dictionary)
		_fill_frog_profile(rail, from, r.get("section", []) as Array)
		if not rail.ready():
			skipped.append("нитка %s на %s: рельс не описан типом %s" % [rail.kind, eid, dev_type])
			continue
		out.append(rail)
	return {"list": out, "skipped": skipped}


## stitch_continuous_rails собирает физический рельс из адресных частей.
##
## Сервер вынужден описать половины усовика на двух разных элементах, но это не
## два рельса. Если протянуть каждую половину отдельно, в изгибе оказываются два
## поперечных кольца с разными курсами: головки накладываются белым клином — тем
## самым «лишним изгибом» на кадре. Здесь рабочие точки переводятся в мир, в
## шов кладётся одно сечение по биссектрисе курсов, и меш получает только два
## наружных конца.
static func stitch_continuous_rails(list: Array[FrogRail]) -> Array[FrogRail]:
	var out: Array[FrogRail] = []
	var groups := {}
	for r in list:
		if r.continuous_id == "":
			out.append(r)
			continue
		if not groups.has(r.continuous_id):
			groups[r.continuous_id] = []
		(groups[r.continuous_id] as Array).append(r)
	var ids: Array = groups.keys()
	ids.sort()
	for id_raw in ids:
		var id := String(id_raw)
		var parts: Array = groups[id]
		if parts.size() < 2:
			for r in parts:
				out.append(r)
			continue
		parts.sort_custom(func(a: FrogRail, b: FrogRail) -> bool:
			return a.continuous_order < b.continuous_order)
		var joined: FrogRail = parts[0]
		var failed := false
		for k in range(1, parts.size()):
			joined = _stitch_rail_pair(joined, parts[k])
			if joined == null:
				failed = true
				break
		if failed:
			for r in parts:
				out.append(r)
		else:
			out.append(joined)
	return out


static func _stitch_rail_pair(first: FrogRail, second: FrogRail) -> FrogRail:
	if not first.ready() or not second.ready() \
			or first.owner != second.owner or absf(first.grow - second.grow) > 1e-9 \
			or first.rail_section != second.rail_section:
		return null
	var rail := FrogRail.new()
	rail.owner = first.owner
	rail.element_id = first.continuous_id
	rail.key = first.key
	rail.id = first.continuous_id
	rail.continuous_id = first.continuous_id
	rail.continuous_order = first.continuous_order
	rail.part_keys.assign(first.part_keys)
	rail.part_keys.append_array(second.part_keys)
	# Соединительный участок становится усовиком в переднем отводе; разные
	# серверные kind называют адресные роли частей, а continuous_id — одно тело.
	rail.kind = FROG_WING if first.kind == FROG_WING or second.kind == FROG_WING else first.kind
	rail.grow = first.grow
	rail.rail_section = first.rail_section
	rail.rail_height_m = first.rail_height_m
	rail.rail_head_width_m = first.rail_head_width_m

	# Все точки новой оси лежат прямо на рабочей грани, поэтому faces=0. Так
	# адресация двух чужих элементов заканчивается здесь и не протекает в меш.
	var running_u := 0.0
	var last_xy := Vector2.ZERO
	var have_last := false
	for k in range(first.axis.size() - 1):
		var p: TrackGeom.AxisPoint = first.axis[k]
		var xy := Vector2(p.x, p.y) + p.left() * first.faces[k]
		if have_last:
			running_u += xy.distance_to(last_xy)
		rail.axis.append(TrackGeom.AxisPoint.new(xy.x, xy.y, p.z, p.heading, running_u))
		rail.faces.append(0.0)
		last_xy = xy
		have_last = true

	var a: TrackGeom.AxisPoint = first.axis[first.axis.size() - 1]
	var b: TrackGeom.AxisPoint = second.axis[0]
	var axy := Vector2(a.x, a.y) + a.left() * first.faces[first.faces.size() - 1]
	var bxy := Vector2(b.x, b.y) + b.left() * second.faces[0]
	var seam := (axy + bxy) * 0.5
	if have_last:
		running_u += seam.distance_to(last_xy)
	var seam_forward := (a.forward() + b.forward()).normalized()
	if seam_forward == Vector2.ZERO:
		return null
	rail.axis.append(TrackGeom.AxisPoint.new(seam.x, seam.y, (a.z + b.z) * 0.5,
		atan2(seam_forward.y, seam_forward.x), running_u))
	rail.faces.append(0.0)
	last_xy = seam

	for k in range(1, second.axis.size()):
		var p: TrackGeom.AxisPoint = second.axis[k]
		var xy := Vector2(p.x, p.y) + p.left() * second.faces[k]
		running_u += xy.distance_to(last_xy)
		rail.axis.append(TrackGeom.AxisPoint.new(xy.x, xy.y, p.z, p.heading, running_u))
		rail.faces.append(0.0)
		last_xy = xy
	_recompute_axis_headings(rail.axis)
	return rail


## _recompute_axis_headings поворачивает каждое сечение поперёк ФАКТИЧЕСКОЙ
## линии сшитого рельса.
##
## До сшивки курс точки принадлежит оси прохода, а рабочая грань лежит от неё
## на переменном выносе. После переноса рабочих точек в мир этот старый курс
## больше не является касательной рельса: в отводе и особенно в горле сечения
## вставали косо и растягивали головку белыми клиньями. Концы смотрят вдоль
## соседнего звена, внутренние точки — по биссектрисе двух звеньев. Это только
## ориентация присланного сечения; положение рабочей грани не меняется.
static func _recompute_axis_headings(axis: Array[TrackGeom.AxisPoint]) -> void:
	if axis.size() < 2:
		return
	for k in axis.size():
		var here := Vector2(axis[k].x, axis[k].y)
		var forward := Vector2.ZERO
		if k > 0:
			forward += (here - Vector2(axis[k - 1].x, axis[k - 1].y)).normalized()
		if k + 1 < axis.size():
			forward += (Vector2(axis[k + 1].x, axis[k + 1].y) - here).normalized()
		if forward.length_squared() <= 1e-18:
			continue
		axis[k].heading = atan2(forward.y, forward.x)


## _fill_frog_profile — разложить присланные станции ходовой поверхности.
##
## ЧИСЕЛ ЗДЕСЬ БОЛЬШЕ НЕТ (2026-08-17). До этого дня понижение сердечника, длину
## выхода на отметку, начало и ширину наката и подъём контррельса задавали шесть
## констант класса — то есть форму ходовой поверхности крестовины ВЫБИРАЛ КЛИЕНТ.
## Теперь их шлёт сервер станциями (track.RenderSectionStation, разбор — в
## frog_profile.go), а здесь остаётся раскладка: между станциями линейно, за
## последней постоянно. Оба правила — часть контракта, а не выбор показа.
##
## Пустой список станций значит «нитка на отметке, накат обычный»: так у усовика,
## и это не пропуск, а его свойство.
static func _fill_frog_profile(rail: FrogRail, from_u: float, stations: Array) -> void:
	if stations.is_empty():
		return
	var su := PackedFloat64Array()
	var ss := PackedFloat64Array()
	var sr := PackedFloat64Array()
	for st_raw in stations:
		var st: Dictionary = st_raw as Dictionary
		su.append(float(st.get("u", 0.0)))
		ss.append(float(st.get("sink", 0.0)))
		sr.append(float(st.get("ride_width", 0.0)))
	rail.sinks = PackedFloat64Array()
	rail.rides = PackedFloat64Array()
	rail.sinks.resize(rail.axis.size())
	rail.rides.resize(rail.axis.size())
	for k in rail.axis.size():
		var p: TrackGeom.AxisPoint = rail.axis[k]
		var u := p.u - from_u
		var i := su.size() - 2
		for j in range(su.size() - 1):
			if u <= su[j + 1]:
				i = j
				break
		if i < 0:
			# Станция одна: сечение постоянно на всей детали.
			rail.sinks[k] = ss[0]
			rail.rides[k] = sr[0]
			continue
		var span := su[i + 1] - su[i]
		var t := 0.0 if span <= 0.0 else clampf((u - su[i]) / span, 0.0, 1.0)
		rail.sinks[k] = lerpf(ss[i], ss[i + 1], t)
		rail.rides[k] = lerpf(sr[i], sr[i + 1], t)


## _with_breaks — вставить в разбиение оси точки излома, которых оно не знает.
##
## Разбиение считает длину и кривизну; излом выноса — свойство НИТКИ, а не оси, и
## сообщить о нём может только тот, кто нитку строит. Точки за пределами
## отрезка и совпавшие с уже имеющимися отбрасываются: лишняя вершина в том же
## месте даёт вырожденный треугольник.
static func _with_breaks(el: TrackGeom.Element, axis: Array[TrackGeom.AxisPoint],
		breaks: Array) -> Array[TrackGeom.AxisPoint]:
	if axis.size() < 2:
		return axis
	var lo: float = axis[0].u
	var hi: float = axis[axis.size() - 1].u
	var out: Array[TrackGeom.AxisPoint] = []
	var extra: Array[float] = []
	for b_raw in breaks:
		var b := float(b_raw)
		if b > lo + BREAK_EPS_M and b < hi - BREAK_EPS_M:
			extra.append(b)
	if extra.is_empty():
		return axis
	extra.sort()
	var unique: Array[float] = []
	for b in extra:
		if unique.is_empty() or absf(b - unique[unique.size() - 1]) > BREAK_EPS_M:
			unique.append(b)
	extra = unique
	var k := 0
	for p in axis:
		while k < extra.size() and extra[k] < p.u - BREAK_EPS_M:
			out.append(el.pose_at(extra[k]))
			k += 1
		# Излом, совпавший с точкой разбиения, уже есть: пропускаем его, а не
		# кладём вторую вершину в ту же точку.
		while k < extra.size() and absf(extra[k] - p.u) <= BREAK_EPS_M:
			k += 1
		out.append(p)
	while k < extra.size():
		out.append(el.pose_at(extra[k]))
		k += 1
	return out


## ШАГ РАЗБИЕНИЯ КОРОТКИХ ДЕТАЛЕЙ, метры. Мельче шага пути на порядок: у остряка
## на восьми метрах меняются ширина, высота и накат, у сердечника всё это
## укладывается в первый метр.
const BLADE_STEP_M := 0.4
const FROG_STEP_M := 0.2


## Насколько две точки оси считаются одной. Миллиметр: мельче любой видимой
## подробности пути и крупнее шума разбора чисел из JSON.
const BREAK_EPS_M := 1e-3


## _flared — вынос рабочей грани на расстоянии `at` от начала нитки длиной `len`.
##
## ОТГИБЫ У КОНЦОВ РАЗНЫЕ, и потому их два числа. У контррельса они одинаковы —
## колесо входит в желоб с любой стороны; у УСОВИКА разные оба: одним концом он
## выходит из своей нитки, другим приходит в горло крестовины и там переходит на
## нитку соседнего прохода (разбор — в track/frograils.go на сервере).
##
## ГРАНЕЙ НА КОНЦАХ ТОЖЕ ДВЕ, и это правка 2026-08-17: половина усовика за горлом
## НАЧИНАЕТСЯ на половине горла от нитки, а КОНЧАЕТСЯ на самой нитке, прижавшись к
## ней за корнем сердечника. Одним числом два разных выноса не выражаются.
##
## Ноль с какой-то стороны означает «отгиба тут нет», и грань там сразу рабочая.
static func _flared(face: float, end_face_from: float, end_face_to: float,
		flare_from: float, flare_to: float, at: float, length: float) -> float:
	if flare_from > 0.0 and at < flare_from:
		return face + (end_face_from - face) * (1.0 - at / flare_from)
	if flare_to > 0.0 and length - at < flare_to:
		return face + (end_face_to - face) * (1.0 - (length - at) / flare_to)
	return face


## plan_face — вынос по присланному закону плана.
##
## Станции задают значение и производную dFace/du; между соседними действует
## кубический Эрмит. Это общий способ прочитать форму, а не решение клиента о
## том, как гнуть усовик: касательные и точки выбрал сервер.
static func plan_face(stations: Array, at: float) -> float:
	if stations.is_empty():
		return 0.0
	if stations.size() == 1:
		return float((stations[0] as Dictionary).get("face", 0.0))
	var first: Dictionary = stations[0] as Dictionary
	if at <= float(first.get("u", 0.0)):
		return float(first.get("face", 0.0))
	for i in range(stations.size() - 1):
		var a: Dictionary = stations[i] as Dictionary
		var b: Dictionary = stations[i + 1] as Dictionary
		var ua := float(a.get("u", 0.0))
		var ub := float(b.get("u", ua))
		if at > ub and i + 2 < stations.size():
			continue
		var span := ub - ua
		if span <= 0.0:
			return float(b.get("face", 0.0))
		var t := clampf((at - ua) / span, 0.0, 1.0)
		var t2 := t * t
		var t3 := t2 * t
		var fa := float(a.get("face", 0.0))
		var fb := float(b.get("face", fa))
		var ma := float(a.get("slope", 0.0)) * span
		var mb := float(b.get("slope", 0.0)) * span
		return (2.0 * t3 - 3.0 * t2 + 1.0) * fa \
			+ (t3 - 2.0 * t2 + t) * ma \
			+ (-2.0 * t3 + 3.0 * t2) * fb \
			+ (t3 - t2) * mb
	var last: Dictionary = stations[stations.size() - 1] as Dictionary
	return float(last.get("face", 0.0))


## _fill_rail_metal — перенести рельс типа в короткую нитку.
##
## Одной функцией на остряк и нитки крестовины: и то и другое — тот же рельс того
## же типа, и два места, читающие его порознь, разошлись бы в том, какое поле
## считать обязательным. Тот же довод, что у _fill_type для участка.
static func _fill_rail_metal(rail: FrogRail, t: Dictionary) -> void:
	var rl: Dictionary = t.get("rail", {}) as Dictionary
	rail.rail_height_m = float(rl.get("height", -1.0))
	rail.rail_head_width_m = float(rl.get("head_width", -1.0))
	rail.rail_section = PackedVector2Array()
	for pt_raw in (rl.get("section", []) as Array):
		var pt := pt_raw as Array
		if pt != null and pt.size() >= 2:
			rail.rail_section.append(Vector2(float(pt[0]), float(pt[1])))


## Виды коротких ниток устройства. Те же строки, что у сервера (track.FrogRail* и
## остряк): второго написания одного и того же клиент не заводит.
const FROG_WING := "wing"
const FROG_CHECK := "check"
const FROG_CASTING := "casting"
## РАМНЫЙ РЕЛЬС — вид нитки стрелки, а не крестовины. Нужен наводке кадра
## (frame=switch): стрелка кончается там, где кончается он, а не корнем остряка.
const RAIL_STOCK := "stock"
## Остряк на проводе отдельным списком (turnout_blades), а не видом нитки: у него
## есть то, чего нет ни у усовика, ни у контррельса, — ветвь и ход. Строка нужна
## показу, чтобы назвать узел, и отчёту, чтобы отличить его в списке.
const BLADE := "blade"


## ОСТРЯК — своя деталь, а не подвижная нитка участка.
##
## До 2026-08-16 остряк был отрезком нитки прохода, который показ отводил в
## сторону. Читалось стройно, а означало вот что: прижатый остряк занимал ровно
## объём рамного рельса, то есть рамного рельса не существовало вовсе — его роль
## играл сам остряк. При переводе он уезжал, и на освободившемся месте оставалась
## наружная нитка соседнего прохода, оказавшаяся там случайно.
##
## Теперь у остряка СВОЁ тело внутри колеи: рабочая грань там же, где у нитки
## (±gauge/2 своего прохода), а растёт оно к оси. Прижатый лежит вплотную к
## неподвижному рамному рельсу, отведённый отходит на ход.
##
## Наследует нитку крестовины, потому что описывается тем же самым: отрезок вдоль
## прохода, свой вынос рабочей грани в каждой точке и объявленная сторона роста.
## Разница ровно одна — вынос ЗАВИСИТ ОТ ПОЛОЖЕНИЯ СТРЕЛКИ и пересчитывается,
## пока остряк идёт.
class Blade extends FrogRail:
	## ОСТРОЖКА ПРИСЛАНА СЕРВЕРОМ (turnout_blades[].section), а не выбрана здесь.
	##
	## До 2026-08-16 на этом месте стояла таблица модели CA-1/9-R65-v1 §А —
	## расстояние от острия, ширина головки, понижение верха, — и три константы
	## наката. Читалось безобидно, а означало вот что: КЛИЕНТ ВЫБИРАЛ ФОРМУ
	## ХОДОВОЙ ПОВЕРХНОСТИ. Он решал, с какого места колесо переходит с рамного
	## рельса на остряк, — а сервер про это не знал и в blade.go утверждал
	## обратное, что сужения нет вовсе. Два места говорили про одну деталь
	## противоположное и разошлись молча.
	##
	## Сверх того таблица кончалась на 0.075 — ширине головки затравочного типа, —
	## и потому несла необъявленное допущение про рельс. Теперь доли живут в
	## модели сервера, а сюда приезжают метры уже своего типа.
	##
	## Четыре массива, а не массив четвёрок: они читаются в цикле по точкам оси, и
	## PackedFloat64Array не заводит объекта на станцию.
	var sec_u := PackedFloat64Array()
	var sec_head := PackedFloat64Array()
	var sec_sink := PackedFloat64Array()
	var sec_ride := PackedFloat64Array()

	var branch: String
	## Вынос прижатого остряка (±gauge/2) и ход в острие — как прислано сервером.
	var offset_m: float = 0.0
	var throw_m: float = 0.0
	var length_m: float = 0.0
	## Тело лежит внутри колеи, по другую сторону линии прилегания, чем профиль
	## логического порта. Направление приезжает отдельно: смешивать его с grow
	## нельзя — тогда либо исчезает остряк, либо размыкается корневой порт.
	var body_grow: float = 1.0
	## Куда растёт тело, прислано сервером (track.RenderTurnoutBlade.Grow).
	## НАСКОЛЬКО остряк отведён сейчас: 0 — прижат, 1 — отведён целиком.
	##
	## Доля, а не признак. Признаком было до 2026-08-16, и остряк прыгал на весь
	## ход за кадр — слово владельца: «стрелка, когда переключается, это не
	## должна делать резко». Перевод стал процессом НА СЕРВЕРЕ, и доля приезжает
	## снапшотом вместе с положением: клиент её не выдумывает и не сглаживает.
	##
	## До первого снапшота остряк ПРИЖАТ: показывать отвод, которого сервер не
	## присылал, значило бы рисовать состояние наугад.
	var open: float = 0.0

	## set_open — отвести остряк на присланную долю. Возвращает true, если вынос
	## и вправду изменился: по этому мир решает, пересобирать ли меш.
	func set_open(share: float) -> bool:
		var want := clampf(share, 0.0, 1.0)
		if is_equal_approx(want, open) and faces.size() == axis.size():
			return false
		open = want
		_lay_faces()
		return true

	## _lay_taper — ОСТРОЖКА: чем остряк тоньше и ниже у острия.
	##
	## У настоящего остряка головка острогана на клин: в острие от неё остаются
	## миллиметры, и лежит она НИЖЕ головки рамного рельса — колесо там идёт по
	## рамному и переходит на остряк, когда тот вышел на полную высоту. Без этого
	## остряк читается вторым полным рельсом, приставленным вплотную к первому
	## (слово владельца на кадре 2026-08-16).
	##
	## ЧИСЕЛ ЗДЕСЬ БОЛЬШЕ НЕТ. Раскладываются присланные станции: между ними
	## линейно, за последней постоянно — оба правила объявлены контрактом
	## (track.RenderSectionStation), а не выбраны тут. Продолжение последним
	## значением важно само по себе: остряк бывает короче таблицы, и обрыв на её
	## конце означал бы, что длина молча меняет форму корня.
	func _lay_taper() -> void:
		widths = PackedFloat64Array()
		sinks = PackedFloat64Array()
		rides = PackedFloat64Array()
		widths.resize(axis.size())
		sinks.resize(axis.size())
		rides.resize(axis.size())
		var head := maxf(rail_head_width_m, 1e-6)
		for k in axis.size():
			var p: TrackGeom.AxisPoint = axis[k]
			var i := _station_at(p.u)
			var t := _station_share(p.u, i)
			# Доля сечения — отношение острожённой головки к полной: сужается ВСЁ
			# сечение, а не одна головка. Иначе у рамного рельса появлялся бы второй
			# полный профиль с полной подошвой в 150 мм.
			widths[k] = lerpf(sec_head[i], sec_head[i + 1], t) / head
			sinks[k] = lerpf(sec_sink[i], sec_sink[i + 1], t)
			rides[k] = lerpf(sec_ride[i], sec_ride[i + 1], t)

	## _station_at — номер станции, с которой начинается отрезок, накрывающий u.
	## Последняя станция накрывает всё, что за ней: сечение там постоянно.
	func _station_at(u: float) -> int:
		var last := sec_u.size() - 2
		for i in range(sec_u.size() - 1):
			if u <= sec_u[i + 1]:
				return i
		return last

	## _station_share — доля пути от станции i до i+1. Вырожденный отрезок (две
	## станции на одном u) даёт ноль, а не деление на ноль.
	func _station_share(u: float, i: int) -> float:
		var span := sec_u[i + 1] - sec_u[i]
		if span <= 0.0:
			return 0.0
		return clampf((u - sec_u[i]) / span, 0.0, 1.0)

	## _lay_faces — вынос рабочей грани в каждой точке.
	##
	## Отвод НАИБОЛЬШИЙ В ОСТРИЕ и сходит на нет к корню — линейно по длине.
	## Линейность объявлена контрактом (mapfmt.TrackSwitch) и здесь не решается: у
	## настоящего остряка форму задаёт строжка.
	##
	## Знак: остряк отходит К ОСИ СВОЕГО ПРОХОДА, то есть в сторону,
	## противоположную его собственному выносу. Прижатый лежит на нитке ровно.
	func _lay_faces() -> void:
		faces = PackedFloat64Array()
		faces.resize(axis.size())
		for k in axis.size():
			var p: TrackGeom.AxisPoint = axis[k]
			var t := 1.0 - clampf(p.u / maxf(length_m, 1e-6), 0.0, 1.0)
			faces[k] = offset_m - signf(offset_m) * throw_m * t * open


## blades — остряки из блока turnout_blades.
##
## Тем же разбором, что нитки крестовины, и это не экономия: остряк и усовик
## описываются одинаково, а нарисованы одним и тем же кодом только потому, что
## они одно и то же — короткий рельс со своим выносом вдоль прохода.
static func blades(network: Dictionary, by_id: Dictionary,
		max_seg_m: float, max_ang_rad: float) -> Dictionary:
	var out: Array[Blade] = []
	var skipped: Array[String] = []
	# ТИПЫ ПУТИ ЗДЕСЬ БОЛЬШЕ НЕ НУЖНЫ: у остряка свой рельс, и он приезжает в
	# самой записи остряка. До 2026-08-16 профиль брали у типа пути прохода —
	# то есть строили остряк из путевого Р65.
	var wire := -1
	for b_raw in (network.get("turnout_blades", []) as Array):
		var b: Dictionary = b_raw as Dictionary
		wire += 1
		var eid := String(b.get("passage", ""))
		if not by_id.has(eid):
			skipped.append("остряк: проход %s не пришёл" % eid)
			continue
		var el: TrackGeom.Element = by_id[eid]
		var length := float(b.get("length", 0.0))
		var throw_m := float(b.get("throw", 0.0))
		if length <= 0.0 or throw_m <= 0.0:
			skipped.append("остряк на %s вырожден: длина %.3f, ход %.3f" % [eid, length, throw_m])
			continue
		# СТРОЖКИ НЕ ПРИСЛАЛИ — ОСТРЯКА НЕТ. Не «нарисуем полным сечением»:
		# полное сечение в острие означает второй рельс вплотную к рамному, то
		# есть картинку, которой сервер не называл. Тот же закон, что у участка
		# без типа: нет цепочки — нет и вещи.
		var section: Array = b.get("section", []) as Array
		if section.size() < 2:
			skipped.append("остряк на %s: строжка не прислана (станций %d)" % [eid, section.size()])
			continue
		# ШАГ СВОЙ, МЕЛКИЙ. Разбиение пути идёт метрами — у остряка это дало бы три
		# точки на 8.3 м, и вся острожка (ширина, понижение, начало наката)
		# посчиталась бы в них. Проверка сказала это числом: «накат начинается с
		# 4.15 м» вместо 1.5.
		var axis := el.sample_range(0.0, minf(length, el.length_m),
			minf(max_seg_m, BLADE_STEP_M), max_ang_rad)
		if axis.size() < 2:
			continue
		var blade := Blade.new()
		blade.owner = String(b.get("owner", ""))
		blade.element_id = eid
		blade.key = "blade:%d" % wire
		blade.kind = BLADE
		blade.branch = String(b.get("branch", ""))
		blade.axis = axis
		blade.offset_m = float(b.get("offset", 0.0))
		blade.throw_m = throw_m
		blade.length_m = length
		# Сторона роста ПРИСЛАНА, а не выведена из знака выноса: тот же довод, что
		# у усовика — сторона есть факт о станции, и два клиента, выводящие её
		# порознь, дали бы два разных ответа.
		blade.grow = signf(float(b.get("body_grow", -signf(blade.offset_m))))
		blade.body_grow = blade.grow
		for st_raw in section:
			var st: Dictionary = st_raw as Dictionary
			blade.sec_u.append(float(st.get("u", 0.0)))
			blade.sec_head.append(float(st.get("head_width", 0.0)))
			blade.sec_sink.append(float(st.get("sink", 0.0)))
			blade.sec_ride.append(float(st.get("ride_width", 0.0)))
		blade._lay_faces()
		# ПРОФИЛЬ ОСТРЯКА ПРИСЛАН ОТДЕЛЬНО, и это не тот рельс, что у пути.
		#
		# Остряки катают из острякового ОР65: ниже Р65 на 40 мм, подошва усечена,
		# головка вынесена в сторону рамного рельса. До 2026-08-16 клиент брал
		# рельс ТИПА ПУТИ и сужал его целиком — получался маленький симметричный
		# профиль, которому в перевод не встать.
		_fill_rail_metal(blade, b)
		# ОСТРОЖКА ПОСЛЕ МЕТАЛЛА: доля сечения считается от присланной ширины
		# головки, и до её прихода делить было бы не на что.
		blade._lay_taper()
		if not blade.ready():
			skipped.append("остряк на %s: остряковый рельс не описан" % eid)
			continue
		out.append(blade)
	return {"list": out, "skipped": skipped}
