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
## Чего здесь нет и не будет, пока контракт молчит: ВСЯКОЙ ВЕРТИКАЛИ. Толщина
## призмы, высота шпалы, профиль рельса, верх плиты платформы не выводятся ни из
## чего, потому что не сказано даже, от чего отсчитывается `z` элемента — от
## головки рельса, от верха призмы или от бровки (разбор 2026-08-12, §3.4).
## Пока это не названо, неизвестно даже, вверх откладывать или вниз. Поэтому всё
## плоское, на отметке оси, и об этом написано на экране.
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


## Шпала: поза центра и два размера из типа пути. Высоты нет — см. шапку.
class Sleeper:
	var pose: TrackGeom.AxisPoint
	## Поперёк пути (`sleeper.length`) и вдоль (`sleeper.width`) — именно так их
	## называет render-contract §3, и перепутать их значит развернуть решётку.
	var length_m: float
	var width_m: float
	var run_id: String
	var element_id: String


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
	var gauge_m: float = -1.0
	var ballast_half_width_m: float = -1.0
	var axis: Array[TrackGeom.AxisPoint] = []

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
	var element_id: String
	var side: String
	var offset_m: float
	var width_m: float
	## Кромки: near — та, что у пути (на `offset`), far — дальняя
	## (на `offset + width`). Обе на отметке ОСИ: высота верха плиты неизвестна.
	var near: PackedVector3Array = PackedVector3Array()
	var far: PackedVector3Array = PackedVector3Array()


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
	var hand: String
	var mark: String
	var branches: Array[String] = []
	## Покрыт ли хоть один проход строительным прогоном. false значит: ни колеи,
	## ни шпал, ни ширины — и рисовать их не от чего.
	var typed: bool = false


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
			if t.has("gauge"):
				span.gauge_m = float(t["gauge"])
			if ballast.has("half_width"):
				span.ballast_half_width_m = float(ballast["half_width"])
			out.append(span)
	return out


## offset_line — полилиния, отложенная по левой нормали на d (со знаком).
static func offset_line(axis: Array[TrackGeom.AxisPoint], d: float) -> PackedVector3Array:
	var out := PackedVector3Array()
	out.resize(axis.size())
	for k in axis.size():
		var p: TrackGeom.AxisPoint = axis[k]
		var n := p.left()
		out[k] = Vector3(p.x + n.x * d, p.y + n.y * d, p.z)
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
			strip.element_id = eid
			strip.side = side
			strip.offset_m = offset
			strip.width_m = width
			strip.near = offset_line(axis, sgn * offset)
			strip.far = offset_line(axis, sgn * (offset + width))
			out.append(strip)
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
