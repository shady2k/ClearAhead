## ПОКРОВ. Хвост /cover у того же адреса чанка: клетка одна, и два пути к ней
## развели бы её тождество надвое.
extends "res://tools/check_suite.gd"


func run() -> void:
	var rule := await ctx.rule()
	var addr := await ctx.first_addr()
	var cov := await ctx.api.cover(ctx.region, addr["level"], addr["cx"], addr["cz"])
	_ok("покров получен", cov.have(), cov.reason)
	if not cov.have():
		return
	var cb: PackedByteArray = cov.cells
	# Ячеек (samples−1)², а не samples²: покров ПЛОЩАДНОЙ, высота точечная.
	# Проверка ловит ровно ту ошибку, которая иначе прошла бы незамеченной —
	# сетку 65×65 у величины, у которой нет узлов.
	var want_cover := (rule.samples - 1) * (rule.samples - 1)
	_ok("покров = (samples−1)² байт", cb.size() == want_cover, "%d байт против %d" % [cb.size(), want_cover])
	# База высот у покрова смысла не имеет: класс поверхности не отсчитывается
	# ни от чего. Лишний заголовок читался бы как «здесь есть высоты» и врал.
	#
	# Спрашивается ПРОВОД: через слой заголовков не видно вовсе — он для того и
	# заведён, — а проверяется здесь именно договор с сервером.
	var cov_wire: Dictionary = await ctx.net.fetch("/regions/%s/chunks/%d/%d/%d/cover" % [
		ctx.region, addr["level"], addr["cx"], addr["cz"]])
	_ok("у покрова нет заголовка базы отсчётов",
		NetClient.header_value(cov_wire["headers"], WorldApi.HEADER_BASE_Z) == "")
	# Классы — из объявленного перечня. Неизвестный код означал бы, что сервер и
	# клиент разошлись версиями контракта, и это обязано быть видно числом, а не
	# странным цветом на экране.
	var bad_class := 0
	var lo := 99
	var hi := -1
	var seen := {}
	for b in cb:
		var cls := b >> 4
		var clo := b & 0x0f
		if cls >= TerrainMesh.SURFACE_MEADOW and cls <= TerrainMesh.SURFACE_BARE_SOIL:
			seen[cls] = int(seen.get(cls, 0)) + 1
		else:
			bad_class += 1
		lo = mini(lo, clo)
		hi = maxi(hi, clo)
	_ok("классы покрова из объявленного перечня", bad_class == 0, "чужих кодов %d" % bad_class)
	_ok("сомкнутость в [0, 15]", lo >= 0 and hi <= 15, "%d…%d" % [lo, hi])
	_ok("покров не одноцветный", seen.size() >= 2, str(seen))
