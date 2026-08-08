class_name GeometryParser
extends RefCounted
## Разбор RenderGeometry — контракт объявлен файлом
## contract/render_geometry.golden.json (единственная форма, не додумывать):
##
##   { "map_id": str, "map_revision": int,
##     "elements": [ { "id": str,
##       "start": { "plan": {"x": м, "y": м, "heading": рад}, "z": м, "slope": рад },
##       "primitives": [ {"kind":"straight","length": м}
##                      | {"kind":"arc","length": м,"radius": м,"angle": рад} ] } ] }
##
## Правила:
##   - неизвестный kind — ОШИБКА, а не пропуск;
##   - нехватка обязательного поля или неверный тип — ОШИБКА;
##   - length > 0, radius > 0; angle — конечное число (знак: + = поворот влево);
##   - лишние поля игнорируются (сервер может добавить своё позже);
##   - z и slope в B1 не рисуются (станция плоская), но в контракте есть.
##
## Чистый парсер без узлов сцены: работает и в игре, и в headless-проверке
## (godot --headless --path client --script tests/...).

const KIND_STRAIGHT := "straight"
const KIND_ARC := "arc"

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

	return {
		"ok": true,
		"geometry": {
			"map_id": map_id,
			"map_revision": int(rev_v),
			"elements": elements,
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

	return { "ok": true, "value": { "id": id, "start": start_ok.value, "primitives": prims } }

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
