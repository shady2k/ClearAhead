## ПОДВИЖНОЙ СОСТАВ — договор о трёх ресурсах разной природы.
##
## Проверяется не «рисуется ли локомотив» (это доказывает снимок), а то, что
## клиент получает ВСЁ, чем его ставит, и не выдумывает ничего сам. Прежний
## клиент снесли в том числе за строку LOCO_ELEMENT = "E_MAIN" — за локомотив на
## элементе, которого на карте сервера не было.
##
## Три ресурса и три разных срока жизни, и суита стережёт именно это различие:
## набор перепроверяется, состояние не кэшируется, байты кэшируются навсегда.
extends "res://tools/check_suite.gd"


func run() -> void:
	var set_res: WorldApi.Content = await ctx.api.content()
	_ok("набор контента приезжает", set_res.have(), set_res.reason)
	if not set_res.have():
		return
	var stock := set_res.data.get("stock", []) as Array
	var assets := set_res.data.get("assets", []) as Array
	_ok("в наборе есть паспорта", stock.size() > 0)
	_ok("в наборе есть ассеты", assets.size() > 0)

	# ГАБАРИТ ПРИЕЗЖАЕТ ЧИСЛАМИ. Без них клиент рисовал бы коробку своего
	# размера — то есть выдумывал бы размер машины.
	var t := (stock[0] as Dictionary)
	# База шкворней — не украшение паспорта: по ней машина ложится на путь
	# ХОРДОЙ. Без неё клиент ставит меш по касательной, и на кривой хвост уходит
	# с рельсов (замер: 0.29 м на R=500 при длине 34 м).
	for field in ["id", "length", "width", "height", "bogie_base", "appearance"]:
		_ok("паспорт несёт %s" % field, t.has(field), "поля нет")
	_ok("длина положительна", float(t.get("length", 0.0)) > 0.0)
	_ok("высота положительна", float(t.get("height", 0.0)) > 0.0)
	_ok("база шкворней внутри машины",
		float(t.get("bogie_base", 0.0)) > 0.0 and float(t.get("bogie_base", 0.0)) < float(t.get("length", 0.0)),
		"база %.2f при длине %.2f" % [float(t.get("bogie_base", 0.0)), float(t.get("length", 0.0))])

	# ССЫЛКА РАЗРЕШИМА. Паспорт, ссылающийся в пустоту, означал бы машину без
	# вида, и обнаружилось бы это только на экране.
	var by_name := {}
	for a_raw in assets:
		var a := a_raw as Dictionary
		by_name[String(a.get("name", ""))] = a
	var appearance := String(t.get("appearance", ""))
	_ok("вид паспорта объявлен в наборе", by_name.has(appearance), appearance)
	if not by_name.has(appearance):
		return
	var asset := by_name[appearance] as Dictionary

	# ПОСТАНОВКА — с сервера, а не из головы клиента: якорь, масштаб и сдвиг.
	# Без якоря меш ставится наугад; без масштаба он не той колеи.
	for field in ["hash", "size", "anchor", "scale", "translation", "attribution", "media_type"]:
		_ok("запись ассета несёт %s" % field, asset.has(field), "поля нет")
	_ok("масштаб положителен", float(asset.get("scale", 0.0)) > 0.0)
	_ok("якорь — поверхность катания",
		String(asset.get("anchor", "")) == "rail_top_gauge_center", String(asset.get("anchor", "")))

	# АТРИБУЦИЯ ЕДЕТ ВМЕСТЕ С БАЙТАМИ. Раздача ассета есть его распространение,
	# и обязательство CC-BY просыпается у сервера, а не у того, кто однажды
	# скачал файл. Отсутствие полей здесь — не косметика, а нарушение лицензии.
	var at := asset.get("attribution", {}) as Dictionary
	for field in ["title", "author", "source", "license", "modified"]:
		_ok("атрибуция несёт %s" % field, at.has(field), "поля нет")

	# ПОДРОБНОСТЕЙ УКЛАДКИ на проводе быть не должно: клиенту нужен адрес байтов
	# и постановка, а как сервер их добыл — не его дело.
	for hidden in ["file", "source_hash", "drop_nodes"]:
		_ok("на проводе нет подробности укладки %s" % hidden, not asset.has(hidden))

	# ЖИВОЕ СОСТОЯНИЕ — отдельным ресурсом и без ревизии в адресе: положение
	# принадлежит партии, а не ревизии карты.
	var live_res: WorldApi.Live = await ctx.api.live(ctx.region)
	_ok("живое состояние приезжает", live_res.have(), live_res.reason)
	if not live_res.have():
		return
	_ok("состояние называет партию", String(live_res.data.get("match", "")) != "")
	var units := live_res.data.get("units", []) as Array
	_ok("в партии есть подвижная единица", units.size() > 0)
	if units.is_empty():
		return
	var u := units[0] as Dictionary
	var at_pos := u.get("at", {}) as Dictionary
	_ok("единица называет тип", by_type(stock, String(u.get("type", ""))), String(u.get("type", "")))
	for field in ["element", "u", "direction"]:
		_ok("положение несёт %s" % field, at_pos.has(field), "поля нет")
	# НАПРАВЛЕНИЕ — не «куда едет», а каким концом повёрнута. Пустое значение
	# у машины означало бы «направления не имеет», что верно для платформы и
	# неверно для локомотива.
	_ok("направление задано явно",
		String(at_pos.get("direction", "")) in ["forward", "reverse"],
		String(at_pos.get("direction", "")))

	# СОСТОЯНИЕ НЕ КЭШИРУЕТСЯ. Сегодня ответ повторяется, потому что ничего не
	# движется; опереться на это кэшем значило бы сломаться в тот день, когда
	# локомотив поедет.
	var raw: Dictionary = await ctx.net.fetch("/regions/%s/live" % ctx.region)
	_ok("у живого состояния no-store", header_of(raw, "cache-control").contains("no-store"),
		header_of(raw, "cache-control"))

	# БАЙТЫ: адрес есть хеш содержимого, и клиент обязан их сверить.
	var address := String(asset.get("hash", ""))
	_ok("адрес байтов — sha256", address.begins_with("sha256-"), address)
	var head: Dictionary = await ctx.net.fetch("/assets/" + address)
	_ok("байты отдаются", head["ok"] and int(head["code"]) == 200, "код %s" % head.get("code"))
	if head["ok"] and int(head["code"]) == 200:
		_ok("длина совпала с объявленной",
			(head["body"] as PackedByteArray).size() == int(asset.get("size", -1)))
		_ok("у байтов immutable", header_of(head, "cache-control").contains("immutable"),
			header_of(head, "cache-control"))
		_ok("возобновляемая загрузка объявлена", header_of(head, "accept-ranges") == "bytes",
			header_of(head, "accept-ranges"))
		# СВЕРКА — та самая, ради которой адресация по содержимому и выбрана.
		var ctxh := HashingContext.new()
		ctxh.start(HashingContext.HASH_SHA256)
		ctxh.update(head["body"])
		_ok("байты сходятся со своим адресом", "sha256-" + ctxh.finish().hex_encode() == address)

	# Выдуманный адрес — 404, а не 204: пустоты в пространстве хешей не бывает.
	var nothing: Dictionary = await ctx.net.fetch("/assets/sha256-" + "0".repeat(64))
	_ok("выдуманный адрес — 404", nothing["ok"] and int(nothing["code"]) == 404,
		"код %s" % nothing.get("code"))


func by_type(stock: Array, id: String) -> bool:
	for t_raw in stock:
		if String((t_raw as Dictionary).get("id", "")) == id:
			return true
	return false


func header_of(res: Dictionary, name: String) -> String:
	for h in (res.get("headers", PackedStringArray()) as PackedStringArray):
		var parts := h.split(":", true, 1)
		if parts.size() == 2 and parts[0].strip_edges().to_lower() == name:
			return parts[1].strip_edges().to_lower()
	return ""
