## ТЕССЕЛЯЦИЯ — сходимость длины и независимость позы от подробности.
##
## Чистая по природе: серверу здесь делать нечего, нужны ЧИСЛА элемента. Они
## приходят снимком ответа (fixtures/network_ST_A.json); слово «сервером» в имени
## первой проверки от этого не устарело — снимок и есть то, что сервер объявил.
extends "res://tools/check_suite.gd"


func run() -> void:
	var network := await ctx.network_data()
	var elements := await ctx.elements()
	# Пустая фикстура сорвала бы обращение по индексу; счёт элементов проверяется
	# у живого сервера («элементов больше нуля»), а здесь довольно выйти молча —
	# бегун объявит суиту без проверок сбоем устройства.
	if elements.is_empty():
		return

	var total := 0.0
	var declared := 0.0
	for el in elements:
		total += el.length_m
		declared += el.length_declared_m
	_ok("длина по примитивам = объявленной сервером", absf(total - declared) < 1e-6,
		"%.6f против %.6f" % [total, declared])

	# Тесселяция обязана начинаться ровно в присланной позе: если клиент
	# «уточнит» начало, вся цепочка уедет, и на стыках появятся ступеньки.
	var first: Dictionary = (network["elements"] as Array)[0] as Dictionary
	var fp: Dictionary = (first["start"] as Dictionary)["plan"] as Dictionary
	var p0: TrackGeom.AxisPoint = elements[0].points[0]
	_ok("первая точка = присланной позе",
		absf(p0.x - float(fp["x"])) < 1e-9 and absf(p0.y - float(fp["y"])) < 1e-9)

	# Поза в произвольной точке считается АНАЛИТИЧЕСКИ, а не по ломаной: этого
	# требует render-contract §4 дословно, иначе шпалы двух клиентов разойдутся
	# при одинаковых phase и pitch. Проверяется тем, что подробность тесселяции
	# на позу не влияет: грубее вдесятеро — та же точка.
	var coarse := TrackGeom.tessellate_element(first, 50.0, 0.5)
	var fine := TrackGeom.tessellate_element(first, 0.5, 0.005)
	var u_mid := fine.length_m * 0.5
	var pc := coarse.pose_at(u_mid)
	var pf := fine.pose_at(u_mid)
	_ok("pose(u) не зависит от подробности тесселяции",
		absf(pc.x - pf.x) < 1e-9 and absf(pc.y - pf.y) < 1e-9,
		"расхождение %.12f м" % Vector2(pc.x - pf.x, pc.y - pf.y).length())
