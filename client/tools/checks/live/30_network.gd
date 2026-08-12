## СЕТЬ — ресурс есть, разбирается, и имена его полей те, которых ждёт клиент.
##
## Арифметика по сети (тесселяция, решётка, платформы, крестовины) живёт в
## checks/pure и считает по СНИМКУ ответа. Здесь остаётся ровно то, чего снимок
## доказать не может: что живой сервер сегодня отвечает и зовёт поля так же.
## Это же и защита снимка от устаревания — уехавшее имя краснеет здесь.
extends "res://tools/check_suite.gd"


func run() -> void:
	var net_res := await ctx.network_answer()
	_ok("сеть получена", not net_res.failed(), net_res.reason)
	if net_res.failed():
		return
	var network := await ctx.network_data()
	for f in ["elements", "structures", "track_types", "construction_runs", "features", "placement_algorithm"]:
		_ok("сеть несёт поле %s" % f, network.has(f))
	var elements := await ctx.elements()
	_ok("элементов больше нуля", elements.size() > 0, "%d" % elements.size())
