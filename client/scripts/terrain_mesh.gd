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

## ЦВЕТ ЗЕМЛИ ПО КРУТИЗНЕ — ПРАВИЛО ПОВЕРХ ПРИСЛАННОГО, А НЕ ДАННЫЕ.
##
## Покрова сервер не отдаёт вовсе, и зелень означала бы траву, которой в
## контракте нет. Но УКЛОН — не новое сведение о мире: он считается из тех же
## присланных высот, что и нормаль, той же центральной разностью. «Круче порога
## — другой цвет» — функция присланного ровно в том же смысле, в каком функцией
## присланного является освещение. Это единственный цвет, который земля вправе
## получить сегодня (разбор 2026-08-12, §4.6 и §5, строка «правило „уклон выше
## порога — другой цвет“»).
##
## Оба порога и оба цвета — РЕШЕНИЕ ХУДОЖНИКА. Второй клиент вправе взять другие,
## и мир от этого не изменится. Пороги выбраны по замеру уклонов чанка 0/0/0
## (медиана 0.087, 90-й процентиль 0.144, а откосы земляных работ сервера — 1:1.5,
## то есть ровно 0.667): полка ниже 0.20 остаётся ровной, а срез под путём
## наливается грунтом полностью.
const SCARP_SLOPE_LO := 0.20
const SCARP_SLOPE_HI := 0.50
const C_GROUND := Color(0.62, 0.62, 0.60)
const C_SCARP := Color(0.44, 0.37, 0.26)

## Классы покрова. Коды — часть контракта чанков: старший полубайт байта
## покрова. Клиент их не назначает, а ЧИТАЕТ, и перечень обязан совпадать с
## server/internal/chunk/chunk.go — там же записано, почему классов пять и
## почему песок объявлен, но сервером пока не порождается.
const SURFACE_MEADOW := 0
const SURFACE_FOREST_CONIFER := 1
const SURFACE_FOREST_BROAD := 2
const SURFACE_SAND := 3
const SURFACE_BARE_SOIL := 4

## ПАЛИТРА — ЗАКОННО КЛИЕНТСКАЯ, и это выправлено 2026-08-12 после разбора.
##
## Час назад здесь стояло обратное: «по границе владения цвет принадлежит
## серверу и приедет каталогом ассетов». Неверно, и неверно по тому же правилу,
## по которому клиент строит ель сам:
##
##     сервер говорит ЧТО и ГДЕ; как это выглядит — дело клиента.
##
## Класс поверхности — ФАКТ (сервер прислал «здесь хвойный лес»). Цвет хвойного
## леса фактом не является: второй рендерер вправе показать его иначе, и мир от
## этого не изменится. Ровно тот же критерий, что у меша ели, у толщины нитки в
## экранных пикселях и у длины крыла крестовины.
##
## Числа взяты у снесённого спайка (разбор §1.2), чтобы не подбирать глазом
## второй раз то, что уже подбиралось.
const COVER_COLOURS := {
	SURFACE_MEADOW: Color(0.41, 0.54, 0.22),
	SURFACE_FOREST_CONIFER: Color(0.20, 0.31, 0.14),
	SURFACE_FOREST_BROAD: Color(0.29, 0.43, 0.18),
	SURFACE_SAND: Color(0.70, 0.65, 0.48),
	SURFACE_BARE_SOIL: Color(0.50, 0.43, 0.31),
}
## Цвет ячейки нулевой сомкнутости: покров есть, а низового яруса нет.
const C_NO_CLOSURE := Color(0.50, 0.43, 0.31)


## cover_colour — цвет по классу и сомкнутости.
##
## Сомкнутость смешивает цвет класса с голым грунтом: внутри одного луга трава
## редеет плавно, и ступенька на границе классов есть, а внутри класса её быть
## не должно. Неизвестный класс НЕ подменяется лугом — он рисуется грунтом, и
## это видно: подстановка правдоподобного класса вместо неизвестного скрыла бы
## расхождение версий контракта.
static func cover_colour(cover_class: int, closure: int) -> Color:
	if not COVER_COLOURS.has(cover_class):
		return C_NO_CLOSURE
	var base: Color = COVER_COLOURS[cover_class]
	return C_NO_CLOSURE.lerp(base, clampf(float(closure) / 15.0, 0.0, 1.0))


## slope_colour — цвет вершины по крутизне. Вынесена, чтобы правило было в одном
## месте и его можно было проверить числом, а не глазом.
##
## Возвращается sRGB — в нём записаны константы и в нём же их читает человек.
## В меш кладётся ЛИНЕЙНЫЙ: albedo_color движок переводит сам, а ARRAY_COLOR
## берёт как есть, и 0.62, положенные напрямую, дают на экране 0.81 — земля
## выбеливается, и снимок врёт про яркость правила.
##
## base — цвет, от которого пляшет правило: серый, пока покрова нет, и цвет
## покрова, когда он приехал. Крутизна поверх покрова остаётся тем же правилом
## над присланным, каким была: обнажённый грунт на срезе не отменяется тем, что
## по паспорту здесь луг.
static func slope_colour(slope: float, base: Color = C_GROUND) -> Color:
	var t := clampf((slope - SCARP_SLOPE_LO) / (SCARP_SLOPE_HI - SCARP_SLOPE_LO), 0.0, 1.0)
	return base.lerp(C_SCARP, t)


## build — меш одного чанка.
##
## Возвращает: mesh, vertices, z_min, z_max, decode_usec, build_usec.
## Ошибка размера тела — это ошибка, а не повод дорисовать: пустой результат.
static func build(blob: PackedByteArray, base_z_m: float, level: int, cx: int, cz: int, rule: ChunkRule,
		cover: PackedByteArray = PackedByteArray()) -> Dictionary:
	var n := rule.samples
	var want := n * n * 2
	if blob.size() != want:
		return {"ok": false, "error": "чанк %d/%d/%d: тело %d байт, ожидалось %d" % [level, cx, cz, blob.size(), want]}

	# Покров — ЯЧЕЙКИ, а не отсчёты: их (n−1)², а не n². Пустой массив значит
	# «покрова нет» и рисуется серым — тем же, чем рисовалось до его появления.
	# Тело неверной длины НЕ додумывается: покров молча игнорируется и это
	# видно числом в отчёте, а не подставляется частично.
	var cover_cells := n - 1
	var has_cover := cover.size() == cover_cells * cover_cells

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
	var cols := PackedColorArray()
	verts.resize(count)
	norms.resize(count)
	cols.resize(count)

	var z_min := INF
	var z_max := -INF
	var steep := 0

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

			# Тот же уклон, что дал нормаль, красит и вершину: второй проход по
			# полю не нужен, а правило названо в шапке.
			var slope := sqrt(dzdx * dzdx + dzdy * dzdy)
			var base := C_GROUND
			if has_cover:
				# Вершина лежит в УГЛУ ячейки, а класс принадлежит площади:
				# берётся ячейка, для которой эта вершина — нижний левый угол, а
				# у крайнего ряда — последняя. Иначе крайняя вершина осталась бы
				# без класса, а достраивать ей соседа значило бы выдумать ячейку.
				var ci := mini(i, cover_cells - 1)
				var cj := mini(j, cover_cells - 1)
				var packed := cover[cj * cover_cells + ci]
				base = cover_colour(packed >> 4, packed & 0x0f)
			cols[k] = slope_colour(slope, base).srgb_to_linear()
			if slope >= SCARP_SLOPE_LO:
				steep += 1

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
	arrays[Mesh.ARRAY_COLOR] = cols
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
		# Отсчёты отдаются наружу, а не выбрасываются после сборки меша: по ним
		# рассевается растительность, и второй разбор того же тела ради тех же
		# чисел был бы двойной работой на каждый чанк.
		"heights": h,
		"steep_vertices": steep,
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
