## ВЕРСИОННЫЕ АДРЕСА МИРА (sqym.5) — договор, на котором стоит sqym.6.
##
## Клиент с sqym.6 спрашивает сеть и патчи ПОД ЗАФИКСИРОВАННОЙ версией из
## projection_head (а не по ревизии и не по последней известной): это и есть
## «новые чанки камеры спрашиваются у зафиксированной версии» — адрес запроса
## называет манифест, и фикстура тут согласилась бы сама с собой. Здесь
## проверяется ПРОВОД: версионная сеть той же формы, патч несёт тело и базу,
## 204 — чистая база, а не ошибка, 404 — неверный адрес, и адреса immutable.
extends "res://tools/check_suite.gd"


func _has_header(headers: PackedStringArray, name: String, needle: String) -> bool:
	var v := NetClient.header_value(headers, name)
	return v != "" and (needle == "" or v.contains(needle))


func run() -> void:
	var man := await ctx.manifest()
	var head: Dictionary = man.get("projection_head", {}) as Dictionary
	var v := int(head.get("world_version", 0))
	_ok("манифест назвал версию мира", v > 0, "world_version = %d" % v)
	if v <= 0:
		return
	var live_res := await ctx.api.live(ctx.region)
	if live_res.failed():
		_ok("живое состояние получено", false, live_res.reason)
		return
	var match_id := String(live_res.data.get("match", ""))
	_ok("живое состояние назвало матч", match_id != "", match_id)
	if match_id == "":
		return

	# Сеть под версией: та же форма, что и по ревизии, но адрес версионный.
	var net_res: WorldApi.Network = await ctx.api.world_network(match_id, v)
	_ok("сеть версии %d получена" % v, net_res.have(), net_res.reason)
	if net_res.have():
		for f in ["elements", "structures", "track_types", "construction_runs", "features", "placement_algorithm"]:
			_ok("сеть версии несёт поле %s" % f, net_res.data.has(f))

	# Патч первого адреса: тело как у чанка + база отсчётов (тот же блоб, что
	# клиент отдаёт TerrainMesh, — никакой арифметики на клиенте).
	var rule := await ctx.rule()
	var addr := await ctx.first_addr()
	var p: WorldApi.Patch = await ctx.api.world_patch(match_id, v, addr["level"], addr["cx"], addr["cz"])
	_ok("патч первого адреса получен", p.have(), p.reason)
	if p.have():
		_ok("тело патча = samples²·2 байт", p.blob.size() == rule.samples * rule.samples * 2,
			"%d байт" % p.blob.size())
		_ok("патч помнит свой адрес",
			p.level == addr["level"] and p.cx == addr["cx"] and p.cz == addr["cz"])
		_ok("база патча в метрах от миллиметрового заголовка",
			p.base_z_m > -10000.0 and p.base_z_m < 10000.0, "%.3f м" % p.base_z_m)

	# 204 = «чистая база»: далёкая клетка. ПОЛНЫЙ ответ, а не ошибка — клиент
	# не переспрашивает и не красит экран отказом.
	var far_cell := int(ceil(rule.radius_of(rule.max_level) * 4.0 / rule.side_of(0))) + 8
	var far: WorldApi.Patch = await ctx.api.world_patch(match_id, v, 0, far_cell, far_cell)
	_ok("патч далёкой клетки — «чистая база», а не отказ",
		far.no_work() and not far.failed(), far.reason)
	_ok("у «чистой базы» пустое тело", far.blob.size() == 0)

	# 404 = неверный адрес, и это ОТКАЗ, в отличие от 204: версия за головой,
	# чужой матч. Путать их нельзя — 204 не переспрашивается, 404 виден.
	var bad_ver: WorldApi.Patch = await ctx.api.world_patch(match_id, v + 99, 0, far_cell, far_cell)
	_ok("версия за головой — отказ, а не «чистая база»",
		bad_ver.failed() and not bad_ver.no_work(), bad_ver.reason)
	var bad_match: WorldApi.Network = await ctx.api.world_network("НЕТ_ТАКОГО", v)
	_ok("чужой матч — отказ", bad_match.failed(), bad_match.reason)

	# Провод: версионные адреса immutable и несут ETag (то, на чём клиент
	# строит уверенность, что повтор патча ничего не меняет).
	var wire: Dictionary = await ctx.net.fetch("/matches/%s/worlds/%d/network" % [match_id, v])
	if wire["ok"] and wire["code"] == 200:
		_ok("версионная сеть — immutable",
			_has_header(wire["headers"], "Cache-Control", "immutable"))
		_ok("версионная сеть несёт ETag", _has_header(wire["headers"], "ETag", ""))
	var wire2: Dictionary = await ctx.net.fetch("/matches/%s/worlds/%d/network" % [match_id, v])
	_ok("повтор версионной сети даёт то же тело (immutable, не ложь)",
		wire["ok"] and wire2["ok"] and (wire["body"] as PackedByteArray) == (wire2["body"] as PackedByteArray))
