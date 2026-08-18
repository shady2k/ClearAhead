## СЛОЙ ПОРТОВ НАЗЫВАЕТ КАЖДУЮ ДЕТАЛЬ И НЕ ТЕРЯЕТ НИ ОДНОЙ.
##
## Чистая по природе: серверу здесь делать нечего, нужны присланные ДЕТАЛИ, и они
## приходят снимком ответа (fixtures/network_ST_A.json).
##
## # Что здесь закрепляется
##
## Слой заведён затем, чтобы на деталь можно было ПОКАЗАТЬ ПАЛЬЦЕМ: владелец
## смотрит кадр и называет ту, с которой беда. Два свойства делают это возможным,
## и оба проверяются здесь.
##
##   ПОЛНОТА. У каждой присланной нитки и каждого остряка ровно два конца, и оба
##     положены в мир. Потеря детали в слое хуже отсутствия слоя: кадр без метки
##     читается как «здесь ничего не кончается», то есть врёт ровно про то, ради
##     чего его смотрят. Пропуск законен только с объяснением — оно уезжает в
##     skipped и печатается отчётом, поэтому skipped обязан быть пуст на снимке,
##     где все элементы на месте.
##
##   РАЗЛИЧИМОСТЬ. В кадре у детали стоит НОМЕР, а имя — в перечне отчёта, и
##     перечень обязан покрывать все номера подряд: пропуск сделал бы номер в
##     кадре нерасшифровываемым, то есть вернул бы кадр в состояние «вижу метку,
##     не знаю чью». Проверяются обе половины: длина перечня и то, что имена в нём
##     не повторяются — одинаковым именем нельзя ответить на вопрос «что это».
##
## ФОРМУ И ТОЧКИ ЗДЕСЬ НЕ ПРОВЕРЯЮТ, и это не пропуск: точка порта считается
## по позе элемента и левой нормали (контракт §4), то есть тем же выражением, что
## стоит в слое. Проверка, повторяющая его, доказывала бы согласие кода с самим
## собой. Где детали смыкаются на самом деле — вопрос сборки, и он решается в Go
## отказом компиляции, а не картинкой.
extends "res://tools/check_suite.gd"

const PortDebugScript := preload("res://scripts/port_debug.gd")


func run() -> void:
	var network := await ctx.network_data()
	var elements := await ctx.elements()
	if elements.is_empty():
		return
	var by_id := TrackBuild.elements_by_id(elements)

	var rails := (network.get("rails", []) as Array).size()
	var blades := (network.get("turnout_blades", []) as Array).size()
	var declared_gaps := (network.get("rail_gaps", []) as Array).size()
	# ПРОВЕРКА НЕ ПУСТАЯ: снимок без единой детали дал бы «портов 0 из 0» и
	# зелёную строку. То же правило, что у границы транспортного слоя.
	_ok("в снимке сети есть детали, у которых бывают порты", rails > 0 and blades > 0,
		"ниток %d, остряков %d" % [rails, blades])

	var built: Dictionary = PortDebugScript.build(network, by_id)
	_ok("у каждой детали снимка два конца в мире",
		int(built["ports"]) == 2 * (rails + blades),
		"портов %d при ожидаемых %d = 2 · (%d + %d)" % [
			int(built["ports"]), 2 * (rails + blades), rails, blades])
	_ok("объявленные разрывы положены все",
		int(built["gaps"]) == declared_gaps,
		"разрывов %d при объявленных %d" % [int(built["gaps"]), declared_gaps])
	_ok("ни одна деталь снимка не пропущена молча",
		(built["skipped"] as Array).is_empty(), str(built["skipped"]))

	# СЛОЙ НЕ ЯВЛЯЕТСЯ МИРОМ, и держится это здесь. Подписи в мире снесены решением
	# владельца (проверка в 48_turnout_drive.gd смотрит на world.gd), а слой их
	# заводит — законно ровно потому, что по умолчанию его не видно. Признак
	# объявлен ложью, и узел строится с visible по нему: пропади любая из этих двух
	# строк — и парящие подписи вернулись бы в мир, не уронив ни одной проверки.
	var world_src := FileAccess.get_file_as_string("res://scripts/world.gd")
	_ok("слой портов по умолчанию выключен",
		world_src.contains("var ports_debug := false"), "world.gd::ports_debug")
	_ok("видимость слоя взята у признака, а не задана",
		world_src.contains("ports_node.visible = ports_debug"), "world.gd::_draw_track")

	# ПЕРЕЧЕНЬ ПОКРЫВАЕТ ВСЁ, ЧТО ПОДПИСАНО НОМЕРОМ: нитки, остряки и разрывы.
	var index: Array = built["index"]
	_ok("перечень называет каждую пронумерованную деталь",
		index.size() == rails + blades + declared_gaps,
		"строк %d при %d нитках, %d остряках и %d разрывах" % [
			index.size(), rails, blades, declared_gaps])
	var numbered := true
	for i in index.size():
		if int(String(index[i]).strip_edges().split(" ")[0]) != i + 1:
			numbered = false
	_ok("номера идут подряд с единицы", numbered,
		"первая строка: %s" % (String(index[0]) if not index.is_empty() else ""))

	var seen := {}
	var doubles: Array[String] = []
	for r_raw in (network.get("rails", []) as Array):
		var r: Dictionary = r_raw as Dictionary
		var eid := String(r.get("element", ""))
		if not by_id.has(eid):
			continue
		var label: String = PortDebugScript.part_label(r, by_id[eid])
		if seen.has(label):
			doubles.append(label)
		seen[label] = true
	_ok("двух деталей с одним именем в перечне нет", doubles.is_empty(),
		"повторов %d: %s" % [doubles.size(), ", ".join(doubles)])

	_check_pick(built["parts"])


## ЩЕЛЧОК ВЫБИРАЕТ ТУ ДЕТАЛЬ, КОТОРУЮ ВИДНО, И ПРАВИЛО СПОРНОГО МЕСТА ОБЪЯВЛЕНО.
##
## Спорное место настоящее: у острия КРИВОЛИНЕЙНЫЙ РАМНЫЙ РЕЛЬС идёт в
## сантиметрах от прямого остряка — на трёх метрах между ними 90 мм, — и щелчок
## обязан различать их, а не брать что ближе по списку. За корнем остряка на той
## же нитке начинается соединительный рельс, и там выбирается уже он.
##
## Луч пускается СВЕРХУ ВНИЗ: это вид, в котором стрелку и разбирают, и он
## единственный, где обе детали видны разом.
func _check_pick(parts: Array) -> void:
	var blade := -1
	var stock := -1
	for i in parts.size():
		var name := String((parts[i] as Dictionary)["name"])
		if name.ends_with("остряк|straight"):
			blade = i
		elif name.ends_with("closure|straight_inner|before_frog"):
			stock = i
	if blade < 0 or stock < 0:
		_ok("в снимке есть прямой остряк и его закорневая нитка", false,
			"остряк %d, соединительный %d" % [blade, stock])
		return
	var over := func(part: Dictionary, at: float) -> int:
		var pts: PackedVector3Array = part["points"]
		var k := clampi(int(round(at * float(pts.size() - 1))), 0, pts.size() - 1)
		var p := pts[k]
		return PortDebugScript.pick(parts, p + Vector3(0.0, 50.0, 0.0), Vector3.DOWN)
	# Над серединой остряка (u ≈ 3.25 м) в 90 мм от него идёт кривой рамный рельс.
	var got_blade: int = over.call(parts[blade] as Dictionary, 0.5)
	_ok("над остряком щелчок выбирает остряк, а не соседнюю нитку", got_blade == blade,
		"выбрано %s" % (String((parts[got_blade] as Dictionary)["name"]) if got_blade >= 0 else "ничего"))
	# За корнем остряка ту же нитку ведёт соединительный рельс.
	var got_stock: int = over.call(parts[stock] as Dictionary, 0.5)
	_ok("за корнем остряка щелчок выбирает соединительный рельс", got_stock == stock,
		"выбрано %s" % (String((parts[got_stock] as Dictionary)["name"]) if got_stock >= 0 else "ничего"))
