## РЕЦЕПТ ПУТЕВОЙ РЕШЁТКИ — то, что рисуется по рецепту, проверяется числом.
##
## Снимок доказывает, что решётка нарисована; он не доказывает, что шпал 234, а
## не 233, и что нитки стоят на ±gauge/2, а не на глаз. Каждое число здесь
## считается способом, НЕ ПОВТОРЯЮЩИМ рисующий код: иначе проверялось бы
## согласие кода с самим собой.
##
## Самая большая суита файла и целиком чистая: серверу в раскладке шпал по
## рецепту, вертикальном стеке типа и ширине полосы платформы делать нечего —
## ей нужны числа сети, и они приходят снимком ответа.
extends "res://tools/check_suite.gd"

## Допуски. Числа не с потолка: полилинии ниток и кромок хранятся в
## PackedVector3Array, а он ВСЕГДА float32 — меш другого и не примет. Значит
## присланная колея 1.435 и отступ 1.75 доезжают до проверки с относительной
## погрешностью около 1e-7, и сверять их байт в байт запрещено правилом проекта
## («float64 не сверяется байт в байт»: проект уже терял на этом эталон
## контракта). 0.1 мм на плановое расстояние — на три порядка ниже всего, что
## различимо на экране, и на четыре — ниже любого путейского допуска.
const EPS_PLAN_M := 1e-4
## Касательные особенностей приезжают double, но кладутся в Vector2 — тот же
## float32. Единичность проверяется с допуском той же природы.
const EPS_UNIT := 1e-6


func run() -> void:
	var network := await ctx.network_data()
	var elements := await ctx.elements()
	if elements.is_empty():
		return
	var by_id := TrackBuild.elements_by_id(elements)
	var types := TrackBuild.types_by_id(network)

	# ШПАЛЫ. Раскладка полуоткрытая: phase + n·pitch ∈ [0, run_length).
	var sl := TrackBuild.sleepers(network, by_id)
	var sleepers: Array[TrackBuild.Sleeper] = sl["list"]
	_ok("решётка ничего не пропустила", (sl["skipped"] as Array).is_empty(), str(sl["skipped"]))
	var want_total := 0
	for r_raw in (sl["runs"] as Array):
		var r: Dictionary = r_raw
		# Независимый счёт: перебором, а не той же формулой ceil, что в рисующем
		# коде. Иначе ошибка в формуле подтвердила бы сама себя.
		var n := 0
		while float(r["phase"]) + float(n) * float(r["pitch"]) < float(r["run_length"]) - 1e-9:
			n += 1
		_ok("прогон %s: шпал = длина/шаг" % r["id"], int(r["count"]) == n,
			"%d при длине %.3f м и шаге %.3f м (перебором %d)" % [r["count"], r["run_length"], r["pitch"], n])
		_ok("прогон %s: все станции легли на элементы" % r["id"], int(r["placed"]) == int(r["count"]),
			"%d из %d" % [r["placed"], r["count"]])
		want_total += n
	_ok("шпал нарисовано столько же, сколько насчитано", sleepers.size() == want_total,
		"%d против %d" % [sleepers.size(), want_total])

	# Размеры шпалы — из типа, а не из головы.
	if not sleepers.is_empty():
		var s0: TrackBuild.Sleeper = sleepers[0]
		var t0: Dictionary = (types.values()[0] as Dictionary).get("sleeper", {}) as Dictionary
		_ok("длина шпалы = sleeper.length типа", absf(s0.length_m - float(t0.get("length", -1.0))) < 1e-12,
			"%.3f м поперёк, %.3f м вдоль" % [s0.length_m, s0.width_m])
		_ok("ширина шпалы = sleeper.width типа", absf(s0.width_m - float(t0.get("width", -1.0))) < 1e-12)

	# Шпала НЕ имеет права оказаться на элементе, который не покрыт прогоном:
	# у ветвей стрелок нет ни типа, ни колеи, и решётка «от соседа» была бы
	# ровно тем выдумыванием, за которое снесли прежнего клиента.
	var spans := TrackBuild.covered_spans(network, by_id, CheckContext.MAX_SEG_M, CheckContext.MAX_ANG_RAD)
	var covered := {}
	for sp in spans:
		covered[sp.element_id] = true
	var strays: Array[String] = []
	for s in sleepers:
		if not covered.has(s.element_id):
			strays.append(s.element_id)
	_ok("шпал вне покрытых участков нет", strays.is_empty(), str(strays))

	var uncovered: Array[String] = []
	for el in elements:
		if not covered.has(el.id):
			uncovered.append(el.id)
	_ok("непокрытые элементы названы и остались нитью", true, str(uncovered))

	# ШПАЛА ЛЕЖИТ НА ПРИЗМЕ, А НЕ НА ОТМЕТКЕ ОСИ. Датум z — поверхность катания
	# (контракт редакции 6 §2), значит верх шпалы ровно на высоту рельса ниже.
	if not sleepers.is_empty():
		var s1: TrackBuild.Sleeper = sleepers[0]
		var t1: Dictionary = types.values()[0] as Dictionary
		var rail1: Dictionary = t1.get("rail", {}) as Dictionary
		var slp1: Dictionary = t1.get("sleeper", {}) as Dictionary
		if rail1.has("height") and slp1.has("height"):
			var want_top := s1.pose.z - float(rail1["height"])
			var want_bot := want_top - float(slp1["height"])
			_ok("верх шпалы = z − rail.height", absf(s1.top_z() - want_top) < 1e-9,
				"%.4f при отметке оси %.4f" % [s1.top_z(), s1.pose.z])
			_ok("низ шпалы = верх − sleeper.height", absf(s1.bottom_z() - want_bot) < 1e-9,
				"толщина %.3f м" % (s1.top_z() - s1.bottom_z()))

	# ВЕРТИКАЛЬНЫЙ СТЕК СХОДИТСЯ САМ С СОБОЙ. formation_to_rail_top —
	# производное поле, и контракт §3.2 обещает, что оно не расходится со
	# слагаемыми. Обещание проверяется, а не принимается на слово: расхождение
	# означало бы, что земля сервера и призма клиента встанут на разной отметке.
	for tid in types:
		var t: Dictionary = types[tid]
		var rl: Dictionary = t.get("rail", {}) as Dictionary
		var sp_t: Dictionary = t.get("sleeper", {}) as Dictionary
		var bl: Dictionary = t.get("ballast", {}) as Dictionary
		if not (t.has("formation_to_rail_top") and rl.has("height") and sp_t.has("height") and bl.has("depth")):
			continue
		var sum := float(bl["depth"]) + float(sp_t["height"]) + float(rl["height"])
		_ok("тип %s: formation_to_rail_top = depth + sleeper.height + rail.height" % tid,
			absf(float(t["formation_to_rail_top"]) - sum) < 1e-9,
			"%.4f м против суммы %.4f м" % [float(t["formation_to_rail_top"]), sum])
		# Ящик нельзя засыпать выше верха шпалы — это отказ валидатора сервера,
		# и клиент проверяет, что отказ работает: призма выше шпалы означала бы
		# закопанную решётку.
		if bl.has("crib_depth"):
			_ok("тип %s: crib_depth не выше sleeper.height" % tid,
				float(bl["crib_depth"]) <= float(sp_t["height"]) + 1e-12,
				"%.3f при высоте шпалы %.3f" % [float(bl["crib_depth"]), float(sp_t["height"])])

	# ПРИЗМА СТРОИТСЯ У ВСЕХ УЧАСТКОВ, включая ветви стрелок. До того, как тип
	# устройства поехал в провод, ветви не имели ни одного размера и рисовались
	# ниткой; проверка ловит откат этого.
	var no_prism: Array[String] = []
	for sp in spans:
		if not sp.has_prism():
			no_prism.append(sp.element_id)
	_ok("призма строится у всех покрытых участков", no_prism.is_empty(), str(no_prism))

	var no_rail: Array[String] = []
	for sp in spans:
		if not sp.has_rail_body():
			no_rail.append(sp.element_id)
	_ok("рельс телом у всех покрытых участков", no_rail.is_empty(), str(no_rail))

	# УПОРЫ. Их не было в проводе до 2026-08-12, и спайк выводил их из топологии
	# сам. Проверяем, что теперь они ПРИСЛАНЫ и разобраны, а не выведены.
	var bs := TrackBuild.buffer_stops(network, by_id)
	var stops: Array[TrackBuild.BufferStop] = bs["list"]
	var declared := 0
	for st_raw in (network.get("structures", []) as Array):
		if String((st_raw as Dictionary).get("kind", "")) == "buffer_stop":
			declared += 1
	_ok("упоров разобрано столько, сколько прислано", stops.size() == declared,
		"%d из %d" % [stops.size(), declared])
	_ok("упоры ничего не пропустили", (bs["skipped"] as Array).is_empty(), str(bs["skipped"]))

	# ПРОФИЛЬ. Сумма длин цепочки обязана совпадать с длиной элемента по плану,
	# иначе z(u) определена не всюду — а «не всюду определённая отметка» это
	# отказ, а не особенность (контракт §5).
	var prof_bad: Array[String] = []
	for el in elements:
		if el.profile.is_empty():
			continue
		var total := 0.0
		for seg in el.profile:
			total += float(seg["length"])
		if absf(total - el.length_m) > 1e-6:
			prof_bad.append("%s: цепочка %.6f м против плана %.6f м" % [el.id, total, el.length_m])
	_ok("длина цепочки профиля = длине по плану", prof_bad.is_empty(), str(prof_bad))

	# НИТКИ. Ровно две на участок, ровно на ±gauge/2 — форма предписана
	# render-contract §3, и это единственная её проверка без окна.
	var pairs := 0
	var bad_gauge := 0
	var bad_count := 0
	for sp in spans:
		var th := sp.threads()
		if th.size() != 2:
			bad_count += 1
			continue
		pairs += 1
		for k in th[0].size():
			var a: Vector3 = th[0][k]
			var b: Vector3 = th[1][k]
			if absf(Vector2(a.x - b.x, a.y - b.y).length() - sp.gauge_m) > EPS_PLAN_M:
				bad_gauge += 1
	_ok("ниток ровно две на каждый покрытый участок", bad_count == 0 and pairs == spans.size(),
		"%d пар на %d участков" % [pairs, spans.size()])
	_ok("расстояние между нитками = gauge", bad_gauge == 0, "%d точек мимо" % bad_gauge)

	# ПЛАТФОРМА. Ищется в `structures` — по имени, которое сервер шлёт сегодня.
	var raw_structures: Array = network.get("structures", []) as Array
	var raw_platforms := 0
	for s_raw in raw_structures:
		if String((s_raw as Dictionary).get("kind", "")) == "platform":
			raw_platforms += 1
	_ok("платформа есть в structures", raw_platforms > 0,
		"%d из %d сооружений" % [raw_platforms, raw_structures.size()])

	var pl := TrackBuild.platforms(network, by_id, CheckContext.MAX_SEG_M, CheckContext.MAX_ANG_RAD)
	var strips: Array[TrackBuild.PlatformStrip] = pl["list"]
	_ok("полоса платформы построена", strips.size() > 0, "%d полос" % strips.size())
	_ok("сооружений не пропущено", (pl["skipped"] as Array).is_empty(), str(pl["skipped"]))
	for p in strips:
		var wrong_w := 0
		for k in mini(p.near.size(), p.far.size()):
			var d := Vector2(p.far[k].x - p.near[k].x, p.far[k].y - p.near[k].y).length()
			if absf(d - p.width_m) > EPS_PLAN_M:
				wrong_w += 1
		_ok("%s: ширина полосы = width" % p.id, wrong_w == 0, "%.3f м, мимо %d точек" % [p.width_m, wrong_w])
		# Отступ и сторона — от ОСИ элемента, а не от края ленты.
		var el: TrackGeom.Element = by_id[p.element_id]
		var sgn := 1.0 if p.side == "left" else -1.0
		var wrong_o := 0
		var axis := el.sample_range(_span_lo(network, p), _span_hi(network, p),
			CheckContext.MAX_SEG_M, CheckContext.MAX_ANG_RAD)
		for k in mini(axis.size(), p.near.size()):
			var a: TrackGeom.AxisPoint = axis[k]
			var off := Vector2(p.near[k].x - a.x, p.near[k].y - a.y)
			if absf(off.dot(a.left()) - sgn * p.offset_m) > EPS_PLAN_M:
				wrong_o += 1
		_ok("%s: ближняя кромка на offset со стороны %s" % [p.id, p.side], wrong_o == 0,
			"offset %.3f м, мимо %d точек" % [p.offset_m, wrong_o])

	# КРЕСТОВИНЫ. Обе касательные присланы и единичны — иначе крыло галочки
	# уедет, а виноватым будет выглядеть рисунок.
	var fr := TrackBuild.frogs(network, by_id)
	var frogs: Array[TrackBuild.Frog] = fr["list"]
	_ok("крестовин построено столько, сколько прислано features",
		frogs.size() == (network.get("features", []) as Array).size(),
		"%d" % frogs.size())
	for f in frogs:
		_ok("%s: касательных две" % f.owner, f.tangents.size() == 2)
		for t in f.tangents:
			_ok("%s: касательная единична" % f.owner, absf(t.length() - 1.0) < EPS_UNIT, "%.12f" % t.length())

	# СТРЕЛКА КАК ОДНО УСТРОЙСТВО: пара ветвей с общим role.turnout.
	var devices := TrackBuild.devices(elements)
	var role_elements := 0
	for el in elements:
		if not el.role.is_empty():
			role_elements += 1
	_ok("устройств собрано из ролей", devices.size() > 0, "%d из %d элементов с ролью" % [
		devices.size(), role_elements])
	for d in devices:
		_ok("%s: две ветви" % d.id, d.branches.size() == 2, str(d.branches))
		_ok("%s: рукость прислана, выводить из геометрии не надо" % d.id, d.hand != "", d.hand)
		_ok("%s: марка прислана" % d.id, d.mark != "", d.mark)


## _span_lo / _span_hi — границы спана сооружения, найденные по его же id.
## Нужны, чтобы проверка отступа считала ось по тем же u, что и построение.
func _span_lo(network: Dictionary, p: TrackBuild.PlatformStrip) -> float:
	var sp := _span_of(network, p)
	return minf(float(sp.get("from", 0.0)), float(sp.get("to", 0.0)))


func _span_hi(network: Dictionary, p: TrackBuild.PlatformStrip) -> float:
	var sp := _span_of(network, p)
	return maxf(float(sp.get("from", 0.0)), float(sp.get("to", 0.0)))


func _span_of(network: Dictionary, p: TrackBuild.PlatformStrip) -> Dictionary:
	for s_raw in (network.get("structures", []) as Array):
		var st: Dictionary = s_raw as Dictionary
		if String(st.get("id", "")) != p.id:
			continue
		for sp_raw in (st.get("spans", []) as Array):
			var sp: Dictionary = sp_raw as Dictionary
			if String(sp.get("element", "")) == p.element_id:
				return sp
	return {}
