## ПУСТОТА И НЕВЕРНЫЙ АДРЕС — и то, во что их переводит слой.
##
## Самая сетевая суита файла: каждая её проверка говорит кодом, и заменить их
## фикстурой значило бы проверять собственную выдумку вместо договора с
## сервером. Адреса выдуманы нарочно — на затравке ни 204 внутри охвата, ни 404
## не приходят сами, и обе ветки впервые сработали бы у игрока.
##
## Пары «на проводе код — в игре слово» стоят рядом не для симметрии: игра про
## коды не знает и знать не должна, и доказательство, что слой ПЕРЕВОДИТ, а не
## выдумывает, есть только в такой паре.
extends "res://tools/check_suite.gd"


func run() -> void:
	var rule := await ctx.rule()

	# Пустота законна: 204, а не отказ. Адрес заведомо вне охвата.
	var far_cell := int(ceil(rule.radius_of(rule.max_level) * 4.0 / rule.side_of(0))) + 8
	var empty: Dictionary = await ctx.net.fetch("/regions/%s/chunks/0/%d/%d" % [
		ctx.region, far_cell, far_cell])
	_ok("далёкий чанк — 204", empty["ok"] and empty["code"] == 204, "код %s" % empty.get("code"))
	if empty["ok"] and empty["code"] == 204:
		_ok("у 204 пустое тело", (empty["body"] as PackedByteArray).size() == 0)
	# ИМЯ ИСХОДА, а не код. Игра на этом строит спуск к грубому уровню, и если
	# слой однажды назовёт пустоту отказом, на экране появятся красные строки
	# вместо земли — молча, потому что рисоваться при этом будет всё то же.
	var empty_layer := await ctx.api.chunk(ctx.region, 0, far_cell, far_cell)
	_ok("далёкий чанк — «чанка нет», а не отказ",
		empty_layer.no_chunk() and not empty_layer.failed(), empty_layer.reason)
	_ok("у «чанка нет» пустое тело и нулевая база",
		empty_layer.blob.size() == 0 and empty_layer.base_z_m == 0.0)

	# Неверный адрес — 404, и это отказ, в отличие от 204.
	var bad: Dictionary = await ctx.net.fetch("/regions/%s/chunks/%d/0/0" % [
		ctx.region, rule.max_level + 5])
	_ok("уровень вне правила — 404", bad["ok"] and bad["code"] == 404, "код %s" % bad.get("code"))
	var bad_region: Dictionary = await ctx.net.fetch("/regions/НЕТ_ТАКОГО/chunks/0/0/0")
	_ok("несуществующий регион — 404", bad_region["ok"] and bad_region["code"] == 404,
		"код %s" % bad_region.get("code"))
	# ОБРАТНАЯ СТОРОНА того же различения: неверный адрес обязан доехать до игры
	# ОТКАЗОМ, а не «чанка нет». Иначе опечатка в имени региона выглядела бы как
	# край мира — пустой экран без единой красной строки.
	var bad_layer := await ctx.api.chunk(ctx.region, rule.max_level + 5, 0, 0)
	_ok("уровень вне правила — отказ, а не «чанка нет»",
		bad_layer.failed() and not bad_layer.no_chunk(), bad_layer.reason)
	var bad_region_layer := await ctx.api.chunk("НЕТ_ТАКОГО", 0, 0, 0)
	_ok("несуществующий регион — отказ, а не «чанка нет»",
		bad_region_layer.failed() and not bad_region_layer.no_chunk(), bad_region_layer.reason)

	# ПЛИТКА — три ресурса одного места одним вопросом. Ими живёт рельеф в мире,
	# и условия («покров и лес только там, где приехали высоты»; «лес только у
	# уровня 0») перенесены в слой вместе с ними.
	var tiles: Array = await ctx.api.terrain(ctx.region,
		rule.cells_for_level(await ctx.axis(), await ctx.bbox(), 1).slice(0, 1))
	if not tiles.is_empty():
		var t: WorldApi.Tile = tiles[0]
		_ok("плитка уровня 1: высоты и покров приехали вместе",
			t.heights.have() and t.cover.have(),
			"%s / %s" % [t.heights.reason, t.cover.reason])
		_ok("плитка уровня 1: леса нет и его не спрашивали",
			t.forest.no_forest() and not t.forest.failed())
