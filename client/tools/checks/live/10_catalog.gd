## КАТАЛОГ РЕГИОНОВ. На нём стоит весь вход в игру: оболочка спрашивает его
## первым и без него не доходит до выбора роли. Проверяется не «код 200», а то,
## ради чего он заведён, — что регион, в который клиент СОБИРАЕТСЯ войти, в
## каталоге есть и объявлен играбельным. Каталог, отдающий пустой список при
## живом регионе, оставил бы игрока в меню с отказом, и выглядело бы это
## поломкой клиента.
extends "res://tools/check_suite.gd"


func run() -> void:
	var cat_res := await ctx.api.regions()
	_ok("каталог регионов получен", not cat_res.failed(), cat_res.reason)
	if cat_res.failed():
		return
	var listed := {}
	for c_raw in cat_res.regions:
		var c: Dictionary = c_raw as Dictionary
		listed[String(c.get("region", ""))] = bool(c.get("playable", false))
	_ok("регион %s есть в каталоге" % ctx.region, listed.has(ctx.region), str(listed.keys()))
	_ok("регион %s объявлен играбельным" % ctx.region, bool(listed.get(ctx.region, false)),
		"иначе оболочка покажет заглушку вместо входа")
