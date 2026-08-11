## TerrainMesh — блоб чанка в меш.
##
## Сервер отдаёт ОТСЧЁТЫ ВЫСОТ, клиент делает меш — граница из ClearAhead-sjq.
## Здесь и только здесь живёт разбор двоичного тела чанка.
##
## Контракт тела (world-chunk-contract, проверено запросом):
##   • samples × samples значений int16, little-endian;
##   • строками по возрастанию j, внутри строки по возрастанию i;
##   • высота отсчёта = base_z + значение / 100, метры;
##   • base_z приезжает ЗАГОЛОВКОМ X-Chunk-Base-Z-Mm в целых миллиметрах;
##   • последний ряд и столбец ОБЩИЕ с соседом — без них между чанками щель.
##
## Про int16: у PackedByteArray нет to_int16_array (есть to_int32_array и
## to_float32_array, но они читают не то). Остаётся decode_s16 в цикле —
## samples² вызовов на чанк. Цена замерена и возвращается наружу полем
## decode_usec: от неё зависит открытый вопрос, не придётся ли серверу слать
## float32 вдвое большим объёмом.
class_name TerrainMesh
extends RefCounted


## build — меш одного чанка.
##
## Возвращает: mesh, vertices, z_min, z_max, decode_usec, build_usec.
## Ошибка размера тела — это ошибка, а не повод дорисовать: пустой результат.
static func build(blob: PackedByteArray, base_z_m: float, level: int, cx: int, cz: int, rule: ChunkRule) -> Dictionary:
	var n := rule.samples
	var want := n * n * 2
	if blob.size() != want:
		return {"ok": false, "error": "чанк %d/%d/%d: тело %d байт, ожидалось %d" % [level, cx, cz, blob.size(), want]}

	var count := n * n
	var h := PackedFloat32Array()
	h.resize(count)

	# ЗАМЕР: только цикл декодирования, без выделения буфера и без сборки меша.
	var t_dec := Time.get_ticks_usec()
	for k in count:
		h[k] = float(blob.decode_s16(k * 2))
	var decode_usec := Time.get_ticks_usec() - t_dec

	var t_build := Time.get_ticks_usec()

	var side := rule.side_of(level)
	var step := rule.step_of(level)
	var ox := float(cx) * side
	var oz := float(cz) * side

	var verts := PackedVector3Array()
	var norms := PackedVector3Array()
	verts.resize(count)
	norms.resize(count)

	var z_min := INF
	var z_max := -INF

	for j in n:
		for i in n:
			var k := j * n + i
			var z := base_z_m + h[k] * 0.01
			if z < z_min:
				z_min = z
			if z > z_max:
				z_max = z
			verts[k] = to_godot(ox + float(i) * step, oz + float(j) * step, z)

			# Нормаль — центральной разностью по сетке отсчётов. У края чанка
			# соседа нет, и разность берётся односторонней: это ошибка ОСВЕЩЕНИЯ
			# на шов, а не ошибка формы. Дорисовывать ряд соседа значило бы
			# выдумать отсчёты, которых не присылали.
			var il := maxi(i - 1, 0)
			var ir := mini(i + 1, n - 1)
			var jd := maxi(j - 1, 0)
			var ju := mini(j + 1, n - 1)
			var dzdx := (h[j * n + ir] - h[j * n + il]) * 0.01 / (float(ir - il) * step)
			var dzdy := (h[ju * n + i] - h[jd * n + i]) * 0.01 / (float(ju - jd) * step)
			norms[k] = Vector3(-dzdx, 1.0, dzdy).normalized()

	var idx := PackedInt32Array()
	idx.resize((n - 1) * (n - 1) * 6)
	var w := 0
	for j in n - 1:
		for i in n - 1:
			var a := j * n + i
			var b := j * n + i + 1
			var c := (j + 1) * n + i
			var d := (j + 1) * n + i + 1
			idx[w] = a
			idx[w + 1] = c
			idx[w + 2] = b
			idx[w + 3] = b
			idx[w + 4] = c
			idx[w + 5] = d
			w += 6

	var arrays := []
	arrays.resize(Mesh.ARRAY_MAX)
	arrays[Mesh.ARRAY_VERTEX] = verts
	arrays[Mesh.ARRAY_NORMAL] = norms
	arrays[Mesh.ARRAY_INDEX] = idx

	var mesh := ArrayMesh.new()
	mesh.add_surface_from_arrays(Mesh.PRIMITIVE_TRIANGLES, arrays)
	var build_usec := Time.get_ticks_usec() - t_build

	return {
		"ok": true,
		"mesh": mesh,
		"vertices": count,
		"triangles": (n - 1) * (n - 1) * 2,
		"z_min": z_min,
		"z_max": z_max,
		"decode_usec": decode_usec,
		"build_usec": build_usec,
	}


## to_godot — ЕДИНСТВЕННОЕ место перевода осей.
##
## Сервер говорит в плановых (x, y) и высоте z. У Godot вверх смотрит Y.
## Берём: X = x, Y = высота, Z = -y. Знак у y ставится затем, чтобы взгляд
## сверху давал привычную карту (y растёт вверх экрана), а не её зеркало.
## Это соглашение ОТОБРАЖЕНИЯ, а не данные: ни одна величина им не меняется.
static func to_godot(x: float, y: float, z: float) -> Vector3:
	return Vector3(x, z, -y)
