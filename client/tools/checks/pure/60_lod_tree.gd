## ДЕРЕВО УРОВНЕЙ — арифметика того, из чего собирается показ.
##
## Сюда переехала суита «спуск к грубому» (60_descent.gd), снесённая вместе с
## маской квадов 2026-08-12. Проверяла она то же самое по существу: что место, у
## которого нет подробного соседа, не остаётся дырой. Изменился ответчик — прежде
## это решала маска, теперь связь узлов, — и потому проверяются три вещи, на
## которых новое устройство держится:
##
##   1. АДРЕС РОДИТЕЛЯ. Сдвиг вправо, а не деление: у отрицательных индексов они
##      расходятся, а клетки к западу от начала координат отрицательны всегда.
##   2. ОТБОР КЛЕТОК ПОКРЫВАЕТ КРУГ. Точка ближе radius_of(L) к оси обязана
##      попасть в какую-то отобранную клетку уровня L — иначе подробного соседа
##      нет там, где он объявлен, и грубому уровню придётся отвечать за место,
##      которое он не покрывает.
##   3. ЧЕТВЕРТИ СКЛАДЫВАЮТСЯ В ЦЕЛУЮ КЛЕТКУ. Узлом показа стала четверть, и она
##      обязана давать ровно те же квады, что целая клетка, — ни одного лишнего
##      и ни одного пропущенного.
##
## Сети здесь нет: правило приходит фикстурой кодом, тело чанка строится рампой.
extends "res://tools/check_suite.gd"


func run() -> void:
	var rule := await ctx.rule()
	var axis := await ctx.axis()
	if axis.is_empty():
		return

	# ОКРУГЛЕНИЕ ВНИЗ, А НЕ К НУЛЮ. (−3) / 2 = −1 у целочисленного деления и −2 у
	# сдвига; верен второй — клетка −3 лежит в теле родителя, накрывающего −4 и −3.
	var p := ChunkRule.parent_of(0, -3, -1)
	_ok("родитель клетки с отрицательным индексом — округление ВНИЗ",
		p[0] == 1 and p[1] == -2 and p[2] == -1, str(p))
	var inside := 0
	var outside := 0
	for cx in range(-9, 10):
		for cz in range(-9, 10):
			var pp: Array = ChunkRule.parent_of(0, cx, cz)
			# Тело клетки обязано лежать внутри тела родителя, и проверяется это
			# метрами, а не индексами: индексы и есть то, что может быть неверно.
			var x0 := float(cx) * rule.side_of(0)
			var px0 := float(pp[1]) * rule.side_of(1)
			var z0 := float(cz) * rule.side_of(0)
			var pz0 := float(pp[2]) * rule.side_of(1)
			if x0 >= px0 and x0 + rule.side_of(0) <= px0 + rule.side_of(1) \
					and z0 >= pz0 and z0 + rule.side_of(0) <= pz0 + rule.side_of(1):
				inside += 1
			else:
				outside += 1
	_ok("клетка лежит в теле родителя на всех знаках индексов", outside == 0,
		"проверено %d, вышли за родителя %d" % [inside + outside, outside])

	var bbox := await ctx.bbox()
	for level in [0, 1]:
		var cells: Array = rule.cells_for_level(axis, bbox, level)
		var have := {}
		for c_raw in cells:
			var c: Dictionary = c_raw
			have[Vector2i(int(c["cx"]), int(c["cz"]))] = true
		# Пробы по кругу уровня: шаг мелкий у нулевого (клетка 256 м), крупнее у
		# первого. Проверяется ПОКРЫТИЕ, поэтому проба берётся только там, где
		# уровень объявлен, — ближе radius_of(level) к оси.
		var side := rule.side_of(level)
		var r := rule.radius_of(level)
		var step := side * 0.25
		var probes := 0
		var missed := 0
		var x := bbox.position.x - r
		while x <= bbox.end.x + r:
			var z := bbox.position.y - r
			while z <= bbox.end.y + r:
				if ChunkRule.nearest_axis_dist(axis, Vector2(x, z)) <= r:
					probes += 1
					if not have.has(Vector2i(int(floor(x / side)), int(floor(z / side)))):
						missed += 1
				z += step
			x += step
		_ok("уровень %d: внутри его круга каждая проба попала в отобранную клетку" % level,
			missed == 0, "проб %d, мимо %d, клеток %d" % [probes, missed, cells.size()])
		# Обратная сторона: оплаченный трафик обязан быть нужен. Клетка, чьё тело
		# не достаёт до круга, — это разобранный впустую блоб и, с появлением
		# ленивого порождения, ещё и работа сервера.
		var idle := 0
		for c_raw in cells:
			var c: Dictionary = c_raw
			var x0 := float(int(c["cx"])) * side
			var z0 := float(int(c["cz"])) * side
			if rule.rect_axis_dist(axis, x0, z0, x0 + side, z0 + side) > r:
				idle += 1
		_ok("уровень %d: ни одна отобранная клетка не лежит целиком за кругом" % level,
			idle == 0, "лишних %d из %d" % [idle, cells.size()])

	_check_quadrants(rule)


## _check_quadrants — четыре четверти дают ровно ту же землю, что целая клетка.
##
## Рампа, а не нули: постоянное поле дало бы вырожденные нормали, и проверка
## перестала бы задевать половину сборщика, ничего об этом не сказав.
func _check_quadrants(rule: ChunkRule) -> void:
	var n := rule.samples
	var h := PackedFloat32Array()
	h.resize(n * n)
	for k in n * n:
		h[k] = float((k % 997) - 400)

	var whole := TerrainMesh.build(h, 140.0, 0, 0, 0, rule)
	_ok("целая клетка собралась", whole.get("ok", false), String(whole.get("error", "")))
	var tris := 0
	var verts := 0
	for q in 4:
		var part := TerrainMesh.build(h, 140.0, 0, 0, 0, rule, PackedByteArray(), q)
		if not part.get("ok", false):
			_ok("четверть %d собралась" % q, false, String(part.get("error", "")))
			return
		tris += int(part["triangles"])
		verts += int(part["vertices"])
	_ok("четыре четверти дают те же треугольники, что целая клетка",
		tris == int(whole["triangles"]), "%d против %d" % [tris, int(whole["triangles"])])
	# ЦЕНА ЧЕТВЕРТЕЙ НАЗВАНА ЧИСЛОМ: общий ряд отсчётов на шве строится дважды.
	# 4×33² = 4356 против 65² = 4225, то есть +3.1 %.
	_ok("вершин у четвертей больше ровно на общие ряды швов",
		verts == 4 * ((n - 1) / 2 + 1) * ((n - 1) / 2 + 1),
		"%d против %d у целой (+%.1f %%)" % [verts, int(whole["vertices"]),
			100.0 * (float(verts) / float(int(whole["vertices"])) - 1.0)])

	# ЮБКА — ЛИШНИЙ РЯД ВЕРШИН ПО КРАЮ, И ЕЁ ЦЕНА ТОЖЕ ЧИСЛОМ.
	var skirted := TerrainMesh.build(h, 140.0, 0, 0, 0, rule, PackedByteArray(), -1, 0.5)
	_ok("юбка добавила треугольники по краю",
		int(skirted["skirt_triangles"]) == 4 * (n - 1) * 2,
		"%d при крае в %d отсчётов" % [int(skirted["skirt_triangles"]), 4 * (n - 1)])
	_ok("без юбки треугольников юбки ноль", int(whole["skirt_triangles"]) == 0)
