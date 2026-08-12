## ЛЕС — битовая маска у чанка уровня 0 и её согласие с покровом.
##
## Эталон функции адреса (ForestJitter) переехал в checks/pure: он целочисленный
## и сети не требовал никогда. Здесь осталось сетевое: длина маски, инвариант
## между двумя ответами сервера и различение «нет ресурса» от «отказ».
##
## Покров спрашивается заново, а не берётся у соседней суиты: файлы проверок
## независимы нарочно — иначе порядок в каталоге стал бы скрытым договором, и
## перенос файла ломал бы чужую проверку. Цена независимости — один лишний
## запрос за прогон.
extends "res://tools/check_suite.gd"


func run() -> void:
	var rule := await ctx.rule()
	var addr := await ctx.first_addr()
	var fr := await ctx.api.forest(ctx.region, addr["level"], addr["cx"], addr["cz"])
	_ok("лес получен на уровне 0", fr.have(), fr.reason)
	if fr.have():
		var fb: PackedByteArray = fr.bits
		var want_forest := (rule.samples - 1) * (rule.samples - 1) / 8
		_ok("лес = (samples−1)²/8 байт", fb.size() == want_forest, "%d байт против %d" % [fb.size(), want_forest])
		var cov := await ctx.api.cover(ctx.region, addr["level"], addr["cx"], addr["cz"])
		if cov.have():
			# ИНВАРИАНТ: бит стоит только в лесном классе. Сервер держит его по
			# построению (лес считается из покрова), и проверка ловит расхождение
			# слоёв, а не подставляет правдоподобную породу.
			var tr := Forest.trees(fb, cov.cells, PackedFloat32Array(), 0.0, rule.samples,
				addr["cx"], addr["cz"], rule.side_of(0))
			_ok("бит леса только в лесном классе", int(tr["mismatched"]) == 0,
				"вне леса %d" % int(tr["mismatched"]))
	# Уровень выше нулевого — 404, а не 204: за коридором деревья рассыпает
	# клиент по покрову, и лес там не появится НИКОГДА.
	var f1: Dictionary = await ctx.net.fetch("/regions/%s/chunks/1/0/0/forest" % ctx.region)
	_ok("лес на уровне 1 — 404", f1["ok"] and f1["code"] == 404, "код %s" % f1.get("code"))
	# И то же САМОЕ, увиденное игрой: она про 404 не знает и знать не должна —
	# ей приезжает слово. Проверка стоит рядом с предыдущей нарочно: пара «на
	# проводе 404, в игре „леса нет“» и есть доказательство, что слой переводит,
	# а не выдумывает.
	var f1_layer := await ctx.api.forest(ctx.region, 1, 0, 0)
	_ok("слой называет это «леса нет», а не отказом",
		f1_layer.no_forest() and not f1_layer.failed(), f1_layer.reason)
