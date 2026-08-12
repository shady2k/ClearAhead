## check.gd — проверка того, что НЕ пиксели. Гоняется без окна:
##
##   godot --headless --path client --script res://tools/check.gd -- --server=…
##
## Зачем отдельно от рисунка: снимок экрана доказывает, что нарисовано верно, но
## не доказывает, что верно обработано НЕНАРИСОВАННОЕ. Счастливый путь на
## затравке ST_A не задевает ни 204, ни 404 — все 55 чанков приезжают. Значит
## обе ветки надо задеть нарочно, иначе они впервые сработают у игрока.
##
## Записанная грабля (bd recall godot-client-check) отсюда никуда не делась:
## этим файлом НЕЛЬЗЯ заменить снимок. Он проверяет коды и числа, а не картинку.
extends SceneTree

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

var _failures: Array[String] = []
var _checks := 0
var _started := false


## Работа начинается с ПЕРВОГО КАДРА, а не из _initialize: в _initialize дерево
## ещё не вместило узлы, и HTTPRequest отвечает «!is_inside_tree()». Это тот же
## урок, что записан про такты в bd recall godot-client-check: ждать надо
## события, а не удобного момента.
func _process(_delta: float) -> bool:
	if not _started:
		_started = true
		_run()
	return false


func _ok(name: String, cond: bool, detail: String = "") -> void:
	_checks += 1
	if cond:
		print("  ok   %s %s" % [name, detail])
	else:
		_failures.append("%s %s" % [name, detail])
		print("  ОТКАЗ %s %s" % [name, detail])


func _run() -> void:
	var server := "http://127.0.0.1:8080"
	var region := "ST_A"
	for a in OS.get_cmdline_user_args():
		if a.begins_with("--server="):
			server = a.substr(9).rstrip("/")
		elif a.begins_with("--region="):
			region = a.substr(9)

	var net := NetClient.new()
	net.base_url = server
	root.add_child(net)

	print("=== проверка клиента без окна: %s, регион %s ===" % [server, region])

	# 1. Манифест: без правила подробности клиент не знает, что спрашивать.
	var man_res: Dictionary = await net.fetch_json("/regions/%s" % region)
	_ok("манифест 200", man_res["ok"], String(man_res.get("error", "")))
	if not man_res["ok"]:
		_finish()
		return
	var man: Dictionary = man_res["data"] as Dictionary
	var rule := ChunkRule.from_manifest(man.get("chunks", {}) as Dictionary)
	_ok("правило подробности заполнено", rule.valid(), rule.rule_text())

	# 1a. ИМЕНА ПОЛЕЙ. Проверка заведена после того, как клиент читал
	#     `network.trackside` (поле переименовано в `structures` коммитом
	#     3637504) и `track_hash` (умер вместе с ресурсом geometry). Ни то, ни
	#     другое не падало: get(имя, умолчание) на пропавшем поле молча даёт
	#     умолчание, и HUD показывал ноль вместо единицы. Отставшее имя обязано
	#     быть отказом здесь, а не наблюдением на снимке.
	for f in ["region", "epoch", "revision", "network_model_hash", "network_hash", "chunks"]:
		_ok("манифест несёт поле %s" % f, man.has(f))

	# 2. Правило уровня — на границах полос, а не в серединке.
	_ok("level_for(0)==0", rule.level_for(0.0) == 0)
	_ok("level_for(r0-ε)==0", rule.level_for(rule.level0_radius_m - 0.001) == 0)
	_ok("level_for(r0)==1", rule.level_for(rule.level0_radius_m) == 1)
	_ok("level_for(за последним уровнем)==-1",
		rule.level_for(rule.radius_of(rule.max_level) + 1.0) == -1)

	# 3. Сеть: рецепт разбирается, и длина по примитивам сходится с объявленной.
	var revision := int(man.get("revision", -1))
	var net_res: Dictionary = await net.fetch_json("/regions/%s/revisions/%d/network" % [region, revision])
	_ok("сеть 200", net_res["ok"], String(net_res.get("error", "")))
	if not net_res["ok"]:
		_finish()
		return
	var network: Dictionary = net_res["data"] as Dictionary
	for f in ["elements", "structures", "track_types", "construction_runs", "features", "placement_algorithm"]:
		_ok("сеть несёт поле %s" % f, network.has(f))
	var elements: Array[TrackGeom.Element] = []
	var total := 0.0
	var declared := 0.0
	for e_raw in (network.get("elements", []) as Array):
		var el := TrackGeom.tessellate_element(e_raw as Dictionary, 5.0, 0.05)
		elements.append(el)
		total += el.length_m
		declared += el.length_declared_m
	_ok("элементов больше нуля", elements.size() > 0, "%d" % elements.size())
	_ok("длина по примитивам = объявленной сервером", absf(total - declared) < 1e-6,
		"%.6f против %.6f" % [total, declared])

	# Тесселяция обязана начинаться ровно в присланной позе: если клиент
	# «уточнит» начало, вся цепочка уедет, и на стыках появятся ступеньки.
	var first: Dictionary = (network["elements"] as Array)[0] as Dictionary
	var fp: Dictionary = (first["start"] as Dictionary)["plan"] as Dictionary
	var p0: TrackGeom.AxisPoint = elements[0].points[0]
	_ok("первая точка = присланной позе",
		absf(p0.x - float(fp["x"])) < 1e-9 and absf(p0.y - float(fp["y"])) < 1e-9)

	# Поза в произвольной точке считается АНАЛИТИЧЕСКИ, а не по ломаной: этого
	# требует render-contract §4 дословно, иначе шпалы двух клиентов разойдутся
	# при одинаковых phase и pitch. Проверяется тем, что подробность тесселяции
	# на позу не влияет: грубее вдесятеро — та же точка.
	var coarse := TrackGeom.tessellate_element(first, 50.0, 0.5)
	var fine := TrackGeom.tessellate_element(first, 0.5, 0.005)
	var u_mid := fine.length_m * 0.5
	var pc := coarse.pose_at(u_mid)
	var pf := fine.pose_at(u_mid)
	_ok("pose(u) не зависит от подробности тесселяции",
		absf(pc.x - pf.x) < 1e-9 and absf(pc.y - pf.y) < 1e-9,
		"расхождение %.12f м" % Vector2(pc.x - pf.x, pc.y - pf.y).length())

	_check_construction(network, elements)

	# 4. Чанк, который ЕСТЬ: размер тела, заголовок базы, разбор.
	var axis := TrackGeom.sample_axis(elements, 5.0)
	var addr: Dictionary = rule.candidates(axis, _bbox(axis))[0]
	var c: Dictionary = await net.fetch("/regions/%s/chunks/%d/%d/%d" % [region, addr["level"], addr["cx"], addr["cz"]])
	_ok("чанк 200", c["ok"] and c["code"] == 200, "код %s" % c.get("code"))
	if c["ok"] and c["code"] == 200:
		var blob: PackedByteArray = c["body"]
		_ok("тело = samples²·2 байт", blob.size() == rule.samples * rule.samples * 2,
			"%d байт" % blob.size())
		var base_hdr := NetClient.header_value(c["headers"], "X-Chunk-Base-Z-Mm")
		_ok("заголовок X-Chunk-Base-Z-Mm есть", base_hdr != "", base_hdr)
		var built := TerrainMesh.build(blob, float(base_hdr.to_int()) / 1000.0, addr["level"], addr["cx"], addr["cz"], rule)
		_ok("меш собран", built.get("ok", false), String(built.get("error", "")))
		if built.get("ok", false):
			_ok("вершин = samples²", int(built["vertices"]) == rule.samples * rule.samples,
				"%d" % built["vertices"])
			_ok("треугольников = (samples-1)²·2",
				int(built["triangles"]) == (rule.samples - 1) * (rule.samples - 1) * 2,
				"%d" % built["triangles"])
		# Тело неверной длины обязано быть отказом, а не половиной меша.
		var short := blob.slice(0, blob.size() - 2)
		_ok("короткое тело отвергнуто",
			not TerrainMesh.build(short, 0.0, 0, 0, 0, rule).get("ok", true))

	# 5. Пустота законна: 204, а не отказ. Адрес заведомо вне охвата.
	var far_cell := int(ceil(rule.radius_of(rule.max_level) * 4.0 / rule.side_of(0))) + 8
	var empty: Dictionary = await net.fetch("/regions/%s/chunks/0/%d/%d" % [region, far_cell, far_cell])
	_ok("далёкий чанк — 204", empty["ok"] and empty["code"] == 204, "код %s" % empty.get("code"))
	if empty["ok"] and empty["code"] == 204:
		_ok("у 204 пустое тело", (empty["body"] as PackedByteArray).size() == 0)

	# 6. Неверный адрес — 404, и это отказ, в отличие от 204.
	var bad: Dictionary = await net.fetch("/regions/%s/chunks/%d/0/0" % [region, rule.max_level + 5])
	_ok("уровень вне правила — 404", bad["ok"] and bad["code"] == 404, "код %s" % bad.get("code"))
	var bad_region: Dictionary = await net.fetch("/regions/НЕТ_ТАКОГО/chunks/0/0/0")
	_ok("несуществующий регион — 404", bad_region["ok"] and bad_region["code"] == 404,
		"код %s" % bad_region.get("code"))

	# 7. Цена decode_s16 против «сервер шлёт float32 вдвое большим объёмом».
	#    Открытый вопрос проекта: замер здесь, вывод — в отчёте.
	if c["ok"] and c["code"] == 200:
		_bench(c["body"], rule)

	_finish()


## _check_construction — то, что рисуется по РЕЦЕПТУ, проверяется числом.
##
## Снимок доказывает, что решётка нарисована; он не доказывает, что шпал 234, а
## не 233, и что нитки стоят на ±gauge/2, а не на глаз. Каждое число здесь
## считается способом, НЕ ПОВТОРЯЮЩИМ рисующий код: иначе проверялось бы
## согласие кода с самим собой.
func _check_construction(network: Dictionary, elements: Array[TrackGeom.Element]) -> void:
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
	var spans := TrackBuild.covered_spans(network, by_id, 5.0, 0.05)
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

	var pl := TrackBuild.platforms(network, by_id, 5.0, 0.05)
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
		var axis := el.sample_range(_span_lo(network, p), _span_hi(network, p), 5.0, 0.05)
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


func _bench(blob: PackedByteArray, rule: ChunkRule) -> void:
	var n := rule.samples * rule.samples
	var rounds := 200

	var t0 := Time.get_ticks_usec()
	for _r in rounds:
		var out := PackedFloat32Array()
		out.resize(n)
		for k in n:
			out[k] = float(blob.decode_s16(k * 2))
	var t_s16 := float(Time.get_ticks_usec() - t0) / float(rounds)

	# Для сравнения — то, что было бы, шли сервер float32: вдвое больше байт,
	# зато один системный вызов вместо n.
	var wide := PackedByteArray()
	wide.resize(n * 4)
	t0 = Time.get_ticks_usec()
	for _r in rounds:
		var f := wide.to_float32_array()
		if f.size() != n:
			push_error("bench: to_float32_array дал %d" % f.size())
	var t_f32 := float(Time.get_ticks_usec() - t0) / float(rounds)

	print("=== цена разбора одного чанка (%d отсчётов, %d прогонов) ===" % [n, rounds])
	print("  decode_s16 в цикле:      %8.1f мкс  (%.1f нс на отсчёт, %d байт на проводе)" % [
		t_s16, t_s16 * 1000.0 / float(n), n * 2])
	print("  to_float32_array:        %8.1f мкс  (%.1f нс на отсчёт, %d байт на проводе)" % [
		t_f32, t_f32 * 1000.0 / float(n), n * 4])
	print("  разница:                 %8.1f мкс на чанк" % (t_s16 - t_f32))


func _bbox(axis: PackedVector2Array) -> Rect2:
	var mn := axis[0]
	var mx := axis[0]
	for p in axis:
		mn = Vector2(minf(mn.x, p.x), minf(mn.y, p.y))
		mx = Vector2(maxf(mx.x, p.x), maxf(mx.y, p.y))
	return Rect2(mn, mx - mn)


func _finish() -> void:
	print("=== проверок %d, отказов %d ===" % [_checks, _failures.size()])
	for f in _failures:
		print("  ОТКАЗ: %s" % f)
	quit(0 if _failures.is_empty() else 1)
