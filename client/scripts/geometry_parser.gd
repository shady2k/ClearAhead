class_name GeometryParser
extends RefCounted
## Разбор RenderGeometry — контракт объявлен файлом
## contract/render_geometry.golden.json (единственная форма, не додумывать):
##
##   { "map_id": str, "map_revision": int,
##     "elements": [ { "id": str,
##       "start": { "plan": {"x": м, "y": м, "heading": рад}, "z": м, "slope": рад },
##       "primitives": [ {"kind":"straight","length": м}
##                      | {"kind":"arc","length": м,"radius": м,"angle": рад} ],
##       "role": { "turnout": str, "branch": "straight"|"diverging",
##                 "hand": "right"|"left", "frog": str } — только у ветви стрелки } ],
##     "trackside": [ { "id": str, "kind": "platform"|"buffer_stop",
##       "side": str, "spans": [ { "element": str, "from": м, "to": м } ] } ],
##     "track_types": [ { "id": str, "gauge": м,
##       "sleeper": { "pitch": м, "length": м, "width": м },
##       "ballast": { "half_width": м } } ],
##     "construction_runs": [ { "id": str, "type": str, "coordinate": "u",
##       "phase": м, "spans": [ { "element": str, "from": м, "to": м,
##       "direction": "forward"|"reverse" } ] } ],
##     "features": [ { "owner": str, "kind": "frog",
##       "point": { "x": м, "y": м },
##       "addresses": [ { "element": str, "u": м,
##       "tangent": { "x": м, "y": м } } ] } ],
##     "placement_algorithm": str }
## Правила:
##   - неизвестный kind (примитива ИЛИ путевого объекта ИЛИ особенности) —
##     ОШИБКА, не пропуск;
##   - нехватка обязательного поля или неверный тип — ОШИБКА;
##   - length > 0, radius > 0; angle — конечное число (знак: + = поворот влево);
##   - спаны путевых объектов в координате u, from >= 0, to >= from;
##   - role.frog обязателен: ветвь всегда несёт марку крестовины;
##   - trackside необязателен (карта без путевых объектов его не шлёт);
##   - role необязателен (есть только у ветвей стрелок);
##   - track_types/construction_runs/features/placement_algorithm необязательны
##     (карта старой ревизии их не шлёт; форма — «пустой массив»/"").
##     run: type всегда явный (умолчание разрешил компилятор, спека §4),
##     coordinate только «u», direction только forward/reverse; у крестовины
##     адресов не меньше двух: прямой проход, затем боковой (спека §5);
##   - лишние поля игнорируются (сервер может добавить своё позже);
##   - z и slope в B1 не рисуются (станция плоская), но в контракте есть.
##
## Чистый парсер без узлов сцены: работает и в игре, и в headless-проверке
## (godot --headless --path client --script tests/...).

const KIND_STRAIGHT := "straight"
const KIND_ARC := "arc"
const KIND_PLATFORM := "platform"
const KIND_BUFFER_STOP := "buffer_stop"

const KIND_FEATURE_FROG := "frog"
const DIR_FORWARD := "forward"
const DIR_REVERSE := "reverse"

const BRANCH_STRAIGHT := "straight"
const BRANCH_DIVERGING := "diverging"

## Текстовое тело -> { "ok": true, "geometry": {...} } | { "ok": false, "error": str }.
static func parse(text: String) -> Dictionary:
	var data: Variant = JSON.parse_string(text)
	if data == null:
		return _fail("тело не является валидным JSON")
	return parse_data(data)

static func parse_data(data: Variant) -> Dictionary:
	if typeof(data) != TYPE_DICTIONARY:
		return _fail("корень — не объект: %s" % type_string(typeof(data)))
	var root: Dictionary = data

	if not root.has("map_id") or typeof(root["map_id"]) != TYPE_STRING \
			or (root["map_id"] as String).is_empty():
		return _fail("map_id должна быть непустой строкой")
	var map_id: String = root["map_id"]

	if not root.has("map_revision"):
		return _fail("нет поля map_revision")
	var rev_v: Variant = _as_int(root["map_revision"])
	if rev_v == null:
		return _fail("map_revision должна быть целым числом")

	if not root.has("elements"):
		return _fail("нет поля elements")
	if typeof(root["elements"]) != TYPE_ARRAY:
		return _fail("elements — не массив")
	var elements_raw: Array = root["elements"]
	var elements: Array[Dictionary] = []
	var seen_ids := {}
	for i in elements_raw.size():
		var elv: Variant = elements_raw[i]
		if typeof(elv) != TYPE_DICTIONARY:
			return _fail("elements[%d] — не объект" % i)
		var parsed := _parse_element(elv as Dictionary, i)
		if not parsed.ok:
			return parsed
		var elem: Dictionary = parsed.value
		if seen_ids.has(elem.id):
			return _fail("дубликат id элемента: %s" % elem.id)
		seen_ids[elem.id] = true
		elements.append(elem)

	# Путевые объекты. Поле необязательно: карта без них его не шлёт.
	var trackside: Array[Dictionary] = []
	if root.has("trackside"):
		if typeof(root["trackside"]) != TYPE_ARRAY:
			return _fail("trackside — не массив")
		var ts_raw: Array = root["trackside"]
		for i in ts_raw.size():
			var tv: Variant = ts_raw[i]
			if typeof(tv) != TYPE_DICTIONARY:
				return _fail("trackside[%d] — не объект" % i)
			var parsed := _parse_trackside(tv as Dictionary, i)
			if not parsed.ok:
				return parsed
			trackside.append(parsed.value)

	# Типы путевых конструкций (спека §3). Поле необязательно: карта старой
	# ревизии его не шлёт; форма — «пустой массив».
	var track_types: Array[Dictionary] = []
	if root.has("track_types"):
		if typeof(root["track_types"]) != TYPE_ARRAY:
			return _fail("track_types — не массив")
		var tt_raw: Array = root["track_types"]
		var seen_types := {}
		for i in tt_raw.size():
			var tv: Variant = tt_raw[i]
			if typeof(tv) != TYPE_DICTIONARY:
				return _fail("track_types[%d] — не объект" % i)
			var parsed := _parse_track_type(tv as Dictionary, i)
			if not parsed.ok:
				return parsed
			var typ: Dictionary = parsed.value
			if seen_types.has(typ.id):
				return _fail("дубликат id типа: %s" % typ.id)
			seen_types[typ.id] = true
			track_types.append(typ)

	# Run'ы размещения решётки (спека §4). Поле необязательно; в проводе
	# type всегда явный — умолчание разрешил компилятор.
	var construction_runs: Array[Dictionary] = []
	if root.has("construction_runs"):
		if typeof(root["construction_runs"]) != TYPE_ARRAY:
			return _fail("construction_runs — не массив")
		var cr_raw: Array = root["construction_runs"]
		var seen_runs := {}
		for i in cr_raw.size():
			var rv: Variant = cr_raw[i]
			if typeof(rv) != TYPE_DICTIONARY:
				return _fail("construction_runs[%d] — не объект" % i)
			var parsed := _parse_run(rv as Dictionary, i)
			if not parsed.ok:
				return parsed
			var run: Dictionary = parsed.value
			if seen_runs.has(run.id):
				return _fail("дубликат id run: %s" % run.id)
			seen_runs[run.id] = true
			construction_runs.append(run)

	# Особенности уровня 2 (спека §5). Поле необязательно; неизвестный вид —
	# ошибка, а не пропуск.
	var features: Array[Dictionary] = []
	if root.has("features"):
		if typeof(root["features"]) != TYPE_ARRAY:
			return _fail("features — не массив")
		var fe_raw: Array = root["features"]
		for i in fe_raw.size():
			var fv: Variant = fe_raw[i]
			if typeof(fv) != TYPE_DICTIONARY:
				return _fail("features[%d] — не объект" % i)
			var parsed := _parse_feature(fv as Dictionary, i)
			if not parsed.ok:
				return parsed
			features.append(parsed.value)

	# Версия алгоритма размещения — строка, часть артефакта (спека §4).
	var placement_algorithm := ""
	if root.has("placement_algorithm"):
		if typeof(root["placement_algorithm"]) != TYPE_STRING:
			return _fail("placement_algorithm должна быть строкой")
		placement_algorithm = root["placement_algorithm"]

	return {
		"ok": true,
		"geometry": {
			"map_id": map_id,
			"map_revision": int(rev_v),
			"elements": elements,
			"trackside": trackside,
			"track_types": track_types,
			"construction_runs": construction_runs,
			"features": features,
			"placement_algorithm": placement_algorithm,
		},
	}

static func _parse_track_type(t: Dictionary, idx: int) -> Dictionary:
	if not t.has("id") or typeof(t["id"]) != TYPE_STRING \
			or (t["id"] as String).is_empty():
		return _fail("track_types[%d]: id должна быть непустой строкой" % idx)
	var id: String = t["id"]
	if not t.has("gauge") or not _is_number(t["gauge"]):
		return _fail("%s: gauge должна быть числом" % id)
	if float(t["gauge"]) <= 0.0:
		return _fail("%s: gauge должна быть > 0" % id)
	if not t.has("sleeper") or typeof(t["sleeper"]) != TYPE_DICTIONARY:
		return _fail("%s: нет sleeper" % id)
	var sleeper: Dictionary = t["sleeper"]
	for key in ["pitch", "length", "width"]:
		if not sleeper.has(key) or not _is_number(sleeper[key]):
			return _fail("%s: sleeper.%s должна быть числом" % [id, key])
		if float(sleeper[key]) <= 0.0:
			return _fail("%s: sleeper.%s должна быть > 0" % [id, key])
	if not t.has("ballast") or typeof(t["ballast"]) != TYPE_DICTIONARY:
		return _fail("%s: нет ballast" % id)
	var ballast: Dictionary = t["ballast"]
	if not ballast.has("half_width") or not _is_number(ballast["half_width"]):
		return _fail("%s: ballast.half_width должна быть числом" % id)
	if float(ballast["half_width"]) <= 0.0:
		return _fail("%s: ballast.half_width должна быть > 0" % id)
	return {
		"ok": true,
		"value": {
			"id": id,
			"gauge": float(t["gauge"]),
			"sleeper": {
				"pitch": float(sleeper["pitch"]),
				"length": float(sleeper["length"]),
				"width": float(sleeper["width"]),
			},
			"ballast": { "half_width": float(ballast["half_width"]) },
		},
	}

static func _parse_run(r: Dictionary, idx: int) -> Dictionary:
	if not r.has("id") or typeof(r["id"]) != TYPE_STRING \
			or (r["id"] as String).is_empty():
		return _fail("construction_runs[%d]: id должна быть непустой строкой" % idx)
	var id: String = r["id"]
	# В проводе ссылка всегда явная: умолчание разрешил компилятор (спека §4).
	if not r.has("type") or typeof(r["type"]) != TYPE_STRING \
			or (r["type"] as String).is_empty():
		return _fail("run %s: type должна быть непустой строкой" % id)
	if not r.has("coordinate") or typeof(r["coordinate"]) != TYPE_STRING \
			or r["coordinate"] != "u":
		return _fail("run %s: coordinate должна быть «u»" % id)
	if not r.has("phase") or not _is_number(r["phase"]):
		return _fail("run %s: phase должна быть числом" % id)
	var phase := float(r["phase"])
	if phase < 0.0:
		return _fail("run %s: phase не может быть отрицательной" % id)
	if not r.has("spans") or typeof(r["spans"]) != TYPE_ARRAY:
		return _fail("run %s: нет spans" % id)
	var spans_raw: Array = r["spans"]
	if spans_raw.is_empty():
		return _fail("run %s: spans пуст — run ничего не покрывает" % id)
	var spans: Array[Dictionary] = []
	for j in spans_raw.size():
		var sv: Variant = spans_raw[j]
		if typeof(sv) != TYPE_DICTIONARY:
			return _fail("run %s: spans[%d] — не объект" % [id, j])
		var so := _parse_run_span(sv as Dictionary, id, j)
		if not so.ok:
			return so
		spans.append(so.value)
	return {
		"ok": true,
		"value": {
			"id": id,
			"type": r["type"],
			"coordinate": "u",
			"phase": phase,
			"spans": spans,
		},
	}

static func _parse_run_span(s: Dictionary, id: String, j: int) -> Dictionary:
	if not s.has("element") or typeof(s["element"]) != TYPE_STRING \
			or (s["element"] as String).is_empty():
		return _fail("run %s: spans[%d]: element должна быть непустой строкой" % [id, j])
	for key in ["from", "to"]:
		if not s.has(key):
			return _fail("run %s: spans[%d]: нет %s" % [id, j, key])
		if not _is_number(s[key]):
			return _fail("run %s: spans[%d]: %s должна быть числом" % [id, j, key])
	if not s.has("direction") or typeof(s["direction"]) != TYPE_STRING:
		return _fail("run %s: spans[%d]: нет direction" % [id, j])
	var direction: String = s["direction"]
	if direction != DIR_FORWARD and direction != DIR_REVERSE:
		return _fail("run %s: spans[%d]: direction «%s» — неизвестна" % [id, j, direction])
	var from := float(s["from"])
	var to := float(s["to"])
	if from < 0.0:
		return _fail("run %s: spans[%d]: from не может быть отрицательным" % [id, j])
	if to < from:
		return _fail("run %s: spans[%d]: to (%v) меньше from (%v)" % [id, j, to, from])
	return { "ok": true, "value": { "element": s["element"], "from": from, "to": to, "direction": direction } }

static func _parse_feature(f: Dictionary, idx: int) -> Dictionary:
	if not f.has("owner") or typeof(f["owner"]) != TYPE_STRING \
			or (f["owner"] as String).is_empty():
		return _fail("features[%d]: owner должна быть непустой строкой" % idx)
	var owner: String = f["owner"]
	if not f.has("kind") or typeof(f["kind"]) != TYPE_STRING \
			or (f["kind"] as String).is_empty():
		return _fail("%s: нет kind" % owner)
	var kind: String = f["kind"]
	if kind != KIND_FEATURE_FROG:
		return _fail("%s: неизвестный вид особенности «%s»" % [owner, kind])
	if not f.has("point") or typeof(f["point"]) != TYPE_DICTIONARY:
		return _fail("%s: нет point" % owner)
	var point: Dictionary = f["point"]
	for key in ["x", "y"]:
		if not point.has(key) or not _is_number(point[key]):
			return _fail("%s: point.%s должна быть числом" % [owner, key])
	if not f.has("addresses") or typeof(f["addresses"]) != TYPE_ARRAY:
		return _fail("%s: нет addresses" % owner)
	var addrs_raw: Array = f["addresses"]
	if addrs_raw.size() < 2:
		return _fail("%s: addresses неполон — у крестовины не меньше двух адресов" % owner)
	var addresses: Array[Dictionary] = []
	for j in addrs_raw.size():
		var av: Variant = addrs_raw[j]
		if typeof(av) != TYPE_DICTIONARY:
			return _fail("%s: addresses[%d] — не объект" % [owner, j])
		var ao := _parse_address(av as Dictionary, owner, j)
		if not ao.ok:
			return ao
		addresses.append(ao.value)
	return {
		"ok": true,
		"value": {
			"owner": owner,
			"kind": kind,
			"point": { "x": float(point["x"]), "y": float(point["y"]) },
			"addresses": addresses,
		},
	}

static func _parse_address(a: Dictionary, owner: String, j: int) -> Dictionary:
	if not a.has("element") or typeof(a["element"]) != TYPE_STRING \
			or (a["element"] as String).is_empty():
		return _fail("%s: addresses[%d]: element должна быть непустой строкой" % [owner, j])
	if not a.has("u") or not _is_number(a["u"]):
		return _fail("%s: addresses[%d]: u должна быть числом" % [owner, j])
	if not a.has("tangent") or typeof(a["tangent"]) != TYPE_DICTIONARY:
		return _fail("%s: addresses[%d]: нет tangent" % [owner, j])
	var tangent: Dictionary = a["tangent"]
	for key in ["x", "y"]:
		if not tangent.has(key) or not _is_number(tangent[key]):
			return _fail("%s: addresses[%d]: tangent.%s должна быть числом" % [owner, j, key])
	return {
		"ok": true,
		"value": {
			"element": a["element"],
			"u": float(a["u"]),
			"tangent": { "x": float(tangent["x"]), "y": float(tangent["y"]) },
		},
	}


static func _parse_element(el: Dictionary, idx: int) -> Dictionary:
	if not el.has("id") or typeof(el["id"]) != TYPE_STRING \
			or (el["id"] as String).is_empty():
		return _fail("elements[%d]: id должна быть непустой строкой" % idx)
	var id: String = el["id"]

	if not el.has("start"):
		return _fail("%s: нет start" % id)
	if typeof(el["start"]) != TYPE_DICTIONARY:
		return _fail("%s: start — не объект" % id)
	var start_ok := _parse_start(el["start"] as Dictionary, id)
	if not start_ok.ok:
		return start_ok

	if not el.has("primitives"):
		return _fail("%s: нет primitives" % id)
	if typeof(el["primitives"]) != TYPE_ARRAY:
		return _fail("%s: primitives — не массив" % id)
	var prims_raw: Array = el["primitives"]
	if prims_raw.is_empty():
		return _fail("%s: primitives пуст — элемент ничего не рисует" % id)
	var prims: Array[Dictionary] = []
	for j in prims_raw.size():
		var pv: Variant = prims_raw[j]
		if typeof(pv) != TYPE_DICTIONARY:
			return _fail("%s: primitives[%d] — не объект" % [id, j])
		var po := _parse_primitive(pv as Dictionary, id, j)
		if not po.ok:
			return po
		prims.append(po.value)

	var value := { "id": id, "start": start_ok.value, "primitives": prims }
	if el.has("role"):
		var ro := _parse_role(el["role"], id)
		if not ro.ok:
			return ro
		value["role"] = ro.value
	return { "ok": true, "value": value }

static func _parse_start(start: Dictionary, id: String) -> Dictionary:
	if not start.has("plan"):
		return _fail("%s: start.plan отсутствует" % id)
	if typeof(start["plan"]) != TYPE_DICTIONARY:
		return _fail("%s: start.plan — не объект" % id)
	var plan: Dictionary = start["plan"]
	for key in ["x", "y", "heading"]:
		if not plan.has(key):
			return _fail("%s: start.plan.%s отсутствует" % [id, key])
		if not _is_number(plan[key]):
			return _fail("%s: start.plan.%s должна быть числом" % [id, key])
	for key in ["z", "slope"]:
		if not start.has(key):
			return _fail("%s: start.%s отсутствует" % [id, key])
		if not _is_number(start[key]):
			return _fail("%s: start.%s должна быть числом" % [id, key])
	return {
		"ok": true,
		"value": {
			"plan": {
				"x": float(plan["x"]),
				"y": float(plan["y"]),
				"heading": float(plan["heading"]),
			},
			"z": float(start["z"]),
			"slope": float(start["slope"]),
		},
	}

static func _parse_primitive(p: Dictionary, id: String, j: int) -> Dictionary:
	if not p.has("kind"):
		return _fail("%s: primitives[%d]: нет kind" % [id, j])
	if typeof(p["kind"]) != TYPE_STRING:
		return _fail("%s: primitives[%d]: kind — не строка" % [id, j])
	var kind: String = p["kind"]
	if kind == KIND_STRAIGHT:
		var ln := _positive_length(p, id, j)
		if not ln.ok:
			return ln
		return { "ok": true, "value": { "kind": KIND_STRAIGHT, "length": ln.value } }
	if kind == KIND_ARC:
		var ln := _positive_length(p, id, j)
		if not ln.ok:
			return ln
		for key in ["radius", "angle"]:
			if not p.has(key):
				return _fail("%s: primitives[%d] (arc): нет %s" % [id, j, key])
			if not _is_number(p[key]):
				return _fail("%s: primitives[%d] (arc): %s должна быть числом" % [id, j, key])
		if float(p["radius"]) <= 0.0:
			return _fail("%s: primitives[%d] (arc): radius должна быть > 0" % [id, j])
		return {
			"ok": true,
			"value": {
				"kind": KIND_ARC,
				"length": ln.value,
				"radius": float(p["radius"]),
				"angle": float(p["angle"]),
			},
		}
	return _fail("%s: primitives[%d]: неизвестный kind «%s»" % [id, j, kind])

static func _positive_length(p: Dictionary, id: String, j: int) -> Dictionary:
	if not p.has("length"):
		return _fail("%s: primitives[%d]: нет length" % [id, j])
	if not _is_number(p["length"]):
		return _fail("%s: primitives[%d]: length должна быть числом" % [id, j])
	var length := float(p["length"])
	if length <= 0.0:
		return _fail("%s: primitives[%d]: length должна быть > 0" % [id, j])
	return { "ok": true, "value": length }

static func _is_number(v: Variant) -> bool:
	return (typeof(v) == TYPE_INT or typeof(v) == TYPE_FLOAT) and is_finite(float(v))

## Целое из числа: int или float с нулевой дробью; иначе null.
static func _as_int(v: Variant) -> Variant:
	if typeof(v) == TYPE_INT:
		return v
	if typeof(v) == TYPE_FLOAT and is_finite(v) and is_equal_approx(v, round(v)):
		return int(round(v))
	return null

static func _fail(msg: String) -> Dictionary:
	return { "ok": false, "error": msg }

static func _parse_role(r: Variant, id: String) -> Dictionary:
	if typeof(r) != TYPE_DICTIONARY:
		return _fail("%s: role — не объект" % id)
	var role: Dictionary = r
	for key in ["turnout", "branch", "hand"]:
		if not role.has(key):
			return _fail("%s: role.%s отсутствует" % [id, key])
		if typeof(role[key]) != TYPE_STRING or (role[key] as String).is_empty():
			return _fail("%s: role.%s должна быть непустой строкой" % [id, key])
	var branch: String = role["branch"]
	if branch != BRANCH_STRAIGHT and branch != BRANCH_DIVERGING:
		return _fail("%s: role.branch «%s» — неизвестная ветвь" % [id, branch])
	var hand: String = role["hand"]
	if hand != "right" and hand != "left":
		return _fail("%s: role.hand «%s» — неизвестная рукость" % [id, hand])
	var value := {
		"turnout": role["turnout"],
		"branch": branch,
		"hand": hand,
	}
	if not role.has("frog") or typeof(role["frog"]) != TYPE_STRING \
			or (role["frog"] as String).is_empty():
		return _fail("%s: role.frog отсутствует" % id)
	value["frog"] = role["frog"]
	return { "ok": true, "value": value }

static func _parse_trackside(ts: Dictionary, idx: int) -> Dictionary:
	if not ts.has("id") or typeof(ts["id"]) != TYPE_STRING \
			or (ts["id"] as String).is_empty():
		return _fail("trackside[%d]: id должна быть непустой строкой" % idx)
	var id: String = ts["id"]

	if not ts.has("kind") or typeof(ts["kind"]) != TYPE_STRING \
			or (ts["kind"] as String).is_empty():
		return _fail("%s: kind отсутствует" % id)
	var kind: String = ts["kind"]
	if kind != KIND_PLATFORM and kind != KIND_BUFFER_STOP:
		return _fail("%s: неизвестный kind «%s»" % [id, kind])

	if not ts.has("spans"):
		return _fail("%s: нет spans" % id)
	if typeof(ts["spans"]) != TYPE_ARRAY:
		return _fail("%s: spans — не массив" % id)
	var spans_raw: Array = ts["spans"]
	if spans_raw.is_empty():
		return _fail("%s: spans пуст — объект ничего не покрывает" % id)
	var spans: Array[Dictionary] = []
	for j in spans_raw.size():
		var sv: Variant = spans_raw[j]
		if typeof(sv) != TYPE_DICTIONARY:
			return _fail("%s: spans[%d] — не объект" % [id, j])
		var so := _parse_span(sv as Dictionary, id, j)
		if not so.ok:
			return so
		spans.append(so.value)

	var value := { "id": id, "kind": kind, "spans": spans }
	if ts.has("side"):
		if typeof(ts["side"]) != TYPE_STRING or (ts["side"] as String).is_empty():
			return _fail("%s: side должна быть непустой строкой" % id)
		value["side"] = ts["side"]
	return { "ok": true, "value": value }

static func _parse_span(s: Dictionary, id: String, j: int) -> Dictionary:
	if not s.has("element") or typeof(s["element"]) != TYPE_STRING \
			or (s["element"] as String).is_empty():
		return _fail("%s: spans[%d]: element должна быть непустой строкой" % [id, j])
	for key in ["from", "to"]:
		if not s.has(key):
			return _fail("%s: spans[%d]: нет %s" % [id, j, key])
		if not _is_number(s[key]):
			return _fail("%s: spans[%d]: %s должна быть числом" % [id, j, key])
	var from := float(s["from"])
	var to := float(s["to"])
	if from < 0.0:
		return _fail("%s: spans[%d]: from не может быть отрицательным" % [id, j])
	if to < from:
		return _fail("%s: spans[%d]: to (%v) меньше from (%v)" % [id, j, to, from])
	return { "ok": true, "value": { "element": s["element"], "from": from, "to": to } }
