## СБОРКА МЕША ЧАНКА — счёт вершин и треугольников и отказ на теле не той длины.
##
## Тело чанка здесь СТРОИТСЯ КОДОМ, а не снимается с сервера, и это ровно тот
## довод, которым живёт seedmap на серверной стороне: намерение видно в вызове.
## Отметки на проверяемые числа не влияют вовсе — вершин samples² при любом
## содержимом, — поэтому тащить ради них 8450 байт снимка значило бы привязать
## арифметику к затравке.
##
## ЧЕГО ЭТА СУИТА НЕ ПРОВЕРЯЕТ: что сборка справляется с НАСТОЯЩИМИ отметками.
## Это осталось у сервера — checks/live собирает меш каждого приехавшего чанка
## (все 124 на затравке) и краснеет, если хоть один не собрался.
extends "res://tools/check_suite.gd"


func run() -> void:
	var rule := await ctx.rule()
	var n := rule.samples

	# Рампа, а не нули: постоянное поле дало бы вырожденные нормали, и проверка
	# перестала бы задевать половину сборщика, ничего об этом не сказав.
	var blob := PackedByteArray()
	blob.resize(n * n * 2)
	for k in n * n:
		blob.encode_s16(k * 2, (k % 997) - 400)

	var dec := TerrainMesh.decode(blob, 0, 0, 0, rule)
	_ok("тело разобрано", dec.get("ok", false), String(dec.get("error", "")))
	var built := TerrainMesh.build(dec["heights"], 140.0, 0, 0, 0, rule)
	_ok("меш собран", built.get("ok", false), String(built.get("error", "")))
	if built.get("ok", false):
		_ok("вершин = samples²", int(built["vertices"]) == n * n, "%d" % built["vertices"])
		_ok("треугольников = (samples-1)²·2", int(built["triangles"]) == (n - 1) * (n - 1) * 2,
			"%d" % built["triangles"])
	# Тело неверной длины обязано быть отказом, а не половиной меша.
	var short := blob.slice(0, blob.size() - 2)
	_ok("короткое тело отвергнуто",
		not TerrainMesh.decode(short, 0, 0, 0, rule).get("ok", true))
