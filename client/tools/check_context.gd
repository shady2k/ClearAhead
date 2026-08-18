## check_context.gd — ОТКУДА ПРОВЕРКА БЕРЁТ ДАННЫЕ.
##
## Один и тот же объект отвечает на вопросы «какое правило подробности», «какая
## сеть», «какие элементы» — и отвечает по-разному в двух режимах:
##
##   offline = true   — из фикстур, сети не касается вовсе (каталог checks/pure);
##   offline = false  — из живого сервера через WorldApi (каталог checks/live).
##
## Ради этого он и заведён. До разноса 2026-08-12 числа для арифметики
## (границы уровней, отбор клеток, раскладка шпал, тесселяция) брались из
## живого ответа, и потому 114 проверок из 132 требовали поднятого сервера на
## фиксированном адресе, хотя серверу в них делать нечего.
##
## # Чем заменены живые ответы — и почему по-разному
##
## У проекта на серверной стороне этот спор решён (шапка seedmap.go): фикстура
## СТРОИТСЯ КОДОМ, потому что тест тогда не ломается от правки боевой карты,
## намерение видно в вызове, а сломанное состояние нельзя получить случайно.
## Доводы применимы здесь НЕ ЦЕЛИКОМ, и разделение проведено по ним:
##
##  * ПРАВИЛО ПОДРОБНОСТИ — кодом (RULE_FIXTURE ниже). Это шесть чисел, и в
##    вызове видно, какие: r0 = 512 м, max_level = 4. Ровно тот случай, ради
##    которого доводы seedmap и написаны.
##  * ТЕЛО ЧАНКА — кодом, у той проверки, которой оно нужно (checks/pure —
##    сборка меша строит рампу сама). Проверяются числа вершин и треугольников,
##    и отметки на них не влияют вовсе.
##  * СЕТЬ — СНЯТА С СЕРВЕРА ОДИН РАЗ (fixtures/network_ST_A.json, цель
##    make client-fixtures). Здесь доводы seedmap не работают: у сервера
##    фабрика — это фабрика с опциями на том же языке, что и код под
##    проверкой; у клиента её нет и быть не может. «Построить сеть кодом»
##    означало бы написать в GDScript второй, ручной образец контракта — то
##    самое выдумывание того, чего не прислали, за которое снесли прежнего
##    клиента. Снимок ответа честнее: он не сочиняет, а цитирует.
##
## Цена снимка названа прямо: он СТАРЕЕТ. Устаревание ловится не здесь, а
## сетевыми проверками — «сеть несёт поле elements», «манифест несёт поле
## chunks» спрашивают живой сервер и краснеют, когда имена уезжают. Обновление
## снимка — одна команда, а не правка руками.
extends RefCounted

## Снимок ответа /regions/ST_A/revisions/2/network. Разложен по строкам нарочно:
## diff снимка обязан читаться, иначе обновление фикстуры проходит не глядя.
const NETWORK_FIXTURE := "res://tools/fixtures/network_ST_A.json"

## Правило подробности фикстурой КОДОМ. Числа совпадают с затравкой ST_A, но
## связаны с ней не файлом, а этой строкой: проверка границ уровней проверяет
## арифметику ChunkRule, а не то, что сервер сегодня прислал 512.
const RULE_FIXTURE := {
	"side_m": 256,
	"step_m": 4,
	"samples": 65,
	"level0_radius_m": 512,
	"max_level": 4,
	"axis_step_m": 5,
}

## Правило земли фикстурой кодом, по тому же доводу, что и правило подробности:
## проверка меряет АРИФМЕТИКУ порога, а не то, какие числа сервер объявил сегодня.
##
## Ставится в TerrainMesh.ground один раз при сборке набора проверок: без него
## TerrainMesh.build ОТКАЗЫВАЕТ — умолчания у правила земли нет ни у клиента, ни
## у проверки.
const GROUND_FIXTURE := {
	"scarp_slope_lo": 0.62,
	"scarp_slope_hi": 1.03,
	"grass_min_closure": 4,
	"no_understory": [3, 4],
}

## Подробность тесселяции. Те же числа, что у мира (world.gd зовёт с 5.0/0.05):
## своя копия означала бы, что проверка меряет не ту геометрию, которую видно.
const MAX_SEG_M := 5.0
const MAX_ANG_RAD := 0.05

var tree: SceneTree
var report
var offline := false
var region := "ST_A"

## Живые концы. В оффлайне остаются null НАРОЧНО: чистая проверка, потянувшаяся
## к сети, обязана падать громко, а не тихо ходить в неё.
var api: WorldApi = null
var net: NetClient = null

## Прогон прерывается только когда дальше проверять нечем: нет манифеста — нет
## правила, нет правила — нет ни одного адреса чанка.
var broken := false
var broken_reason := ""

var _rule: ChunkRule = null
var _man: Dictionary = {}
var _man_answer: WorldApi.Manifest = null
var _net_data: Dictionary = {}
var _net_answer: WorldApi.Network = null
var _elements: Array[TrackGeom.Element] = []
var _axis := PackedVector2Array()


## manifest_answer — исход запроса манифеста как таковой. Нужен ровно одной
## проверке («манифест получен»), остальным хватает manifest().
func manifest_answer() -> WorldApi.Manifest:
	if _man_answer == null:
		_man_answer = await api.manifest(region)
		if _man_answer.failed():
			_break("манифест не получен: " + _man_answer.reason)
		else:
			_man = _man_answer.data
	return _man_answer


func manifest() -> Dictionary:
	if _man.is_empty() and not offline:
		await manifest_answer()
	return _man


func rule() -> ChunkRule:
	if _rule != null:
		return _rule
	if offline:
		_rule = ChunkRule.from_manifest(RULE_FIXTURE)
		return _rule
	var man := await manifest()
	_rule = ChunkRule.from_manifest(man.get("chunks", {}) as Dictionary)
	if not _rule.valid():
		_break("правило подробности не собрано: " + str(_rule.missing()))
	return _rule


func revision() -> int:
	var man := await manifest()
	return int(man.get("revision", -1))


func network_answer() -> WorldApi.Network:
	if _net_answer == null:
		_net_answer = await api.network(region, await revision())
		if _net_answer.failed():
			_break("сеть не получена: " + _net_answer.reason)
		else:
			_net_data = _net_answer.data
	return _net_answer


func network_data() -> Dictionary:
	if not _net_data.is_empty():
		return _net_data
	if offline:
		var text := FileAccess.get_file_as_string(NETWORK_FIXTURE)
		var parsed = JSON.parse_string(text)
		if not (parsed is Dictionary):
			_break("фикстура сети не разобрана: " + NETWORK_FIXTURE)
			return _net_data
		_net_data = parsed as Dictionary
		return _net_data
	await network_answer()
	return _net_data


## elements — сеть, разобранная в примитивы. Тесселяция считается ОДИН РАЗ на
## прогон: она не бесплатна, а спрашивают её пять проверок из разных файлов.
func elements() -> Array[TrackGeom.Element]:
	if not _elements.is_empty():
		return _elements
	for e_raw in ((await network_data()).get("elements", []) as Array):
		_elements.append(TrackGeom.tessellate_element(e_raw as Dictionary, MAX_SEG_M, MAX_ANG_RAD))
	return _elements


func axis() -> PackedVector2Array:
	if _axis.is_empty():
		_axis = TrackGeom.sample_axis(await elements(), (await rule()).axis_step_m)
	return _axis


func bbox() -> Rect2:
	var a := await axis()
	var mn := a[0]
	var mx := a[0]
	for p in a:
		mn = Vector2(minf(mn.x, p.x), minf(mn.y, p.y))
		mx = Vector2(maxf(mx.x, p.x), maxf(mx.y, p.y))
	return Rect2(mn, mx - mn)


## first_addr — первый адрес уровня 0 по тому же отбору, каким ходит мир.
## Число 5.0 сюда не зашито: шаг выборки оси берётся из правила (ClearAhead-cg3).
func first_addr() -> Dictionary:
	var r := await rule()
	return r.cells_for_level(await axis(), await bbox(), 0)[0]


func _break(why: String) -> void:
	broken = true
	broken_reason = why
