## ВОДА. Проверяется не наличие поля, а то, ради чего оно заведено: у каждой
## точки оси есть ПОЛОЖИТЕЛЬНАЯ полуширина по обе стороны. Ноль значил бы, что
## урез замерить не удалось, и лента выродилась бы в линию — на кадре это
## выглядит отсутствием реки, а не отказом.
##
## Суита сетевая, потому что проверяет ДАННЫЕ СЕРВЕРА, а не их обработку: на
## снимке ответа она подтверждала бы только, что снимок был снят удачно.
extends "res://tools/check_suite.gd"


func run() -> void:
	var obj_res := await ctx.api.objects(ctx.region, await ctx.revision())
	_ok("объекты региона получены", not obj_res.failed(), obj_res.reason)
	if obj_res.failed():
		return
	var rivers: Array = obj_res.data.get("rivers", []) as Array
	_ok("реки в проводе", not rivers.is_empty(), "рек %d" % rivers.size())
	var degenerate := 0
	var pts := 0
	for rv_raw in rivers:
		for p_raw in ((rv_raw as Dictionary).get("axis", []) as Array):
			var p: Dictionary = p_raw as Dictionary
			pts += 1
			if float(p.get("half_left", 0.0)) <= 0.0 or float(p.get("half_right", 0.0)) <= 0.0:
				degenerate += 1
	_ok("урез замерен во всех точках оси", degenerate == 0,
		"точек %d, вырожденных %d" % [pts, degenerate])
