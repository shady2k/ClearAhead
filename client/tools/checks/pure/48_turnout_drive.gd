## ПЕРЕВОДНОЙ МЕХАНИЗМ В КАДРЕ: где он встал, чем он оказался и что показывает.
##
## Чистая по природе: сервер прислал место и вид (turnout_drives), а всё
## остальное — арифметика клиента над присланным, и живой сервер ей не нужен.
##
## # Что здесь закрепляется
##
## СТОРОНА. Привод обязан стоять С ТОЙ ЖЕ стороны, куда его отнёс сервер, и НЕ У
## ОСИ пути: знак выноса — единственное, что отделяет станину, стоящую на земле,
## от станины, стоящей между рельсами. Ошибка знака не даёт ни отказа, ни
## пустого кадра — она даёт предмет посреди колеи, и заметить её можно только
## числом либо глазами на снимке.
##
## УКАЗАТЕЛЬ ПОКАЗЫВАЕТ ПРИСЛАННОЕ И НИЧЕГО КРОМЕ. До первого снапшота
## положения нет вовсе — и голова не повёрнута ни во что; неизвестное положение
## не показывается тоже. Это то же правило, по которому клиент не рисует
## платформу без высоты.
##
## ТАБЛИЧКА ЕСТЬ И НА НЕЙ ПРИСЛАННАЯ МЕТКА. Проверяется существование щитка и
## текстуры на нём, а не то, как выглядит шрифт: рисунок — дело движка, а вот
## табличка без номера означала бы, что метка потерялась по дороге.
extends "res://tools/check_suite.gd"


func run() -> void:
	var network := await ctx.network_data()
	var elements := await ctx.elements()
	if elements.is_empty():
		return
	var by_id := TrackBuild.elements_by_id(elements)
	var res := TrackBuild.drives(network, by_id)
	var drives: Array[TrackBuild.TurnoutDrive] = res["list"]
	var raw := (network.get("turnout_drives", []) as Array)

	_ok("приводы разобраны все", drives.size() == raw.size(),
		"%d из %d, пропущено: %s" % [drives.size(), raw.size(), str(res["skipped"])])
	if drives.is_empty():
		return

	# ПАРА ВИДОВ НА СТАНЦИИ: ручной и электрический. Проверка перечня, а не
	# одного значения: код, зашивший один вид, прошёл бы проверку на одном.
	var kinds := {}
	for d in drives:
		kinds[d.drive] = true
	_ok("механизмы приехали разными", kinds.size() >= 2, str(kinds.keys()))

	var worst_off := INF
	var wrong_side := 0
	for d in drives:
		var el: TrackGeom.Element = by_id[d.element_id]
		var axis := el.pose_at(d.pose.u)
		# ВЫНОС МЕРЯЕТСЯ ОТ ОСИ ПО ЛЕВОЙ НОРМАЛИ — тем же способом, каким сервер
		# его назначил. Считается ЗАНОВО из позы, а не берётся из поля: поле и
		# проверяется.
		var n := axis.left()
		var got := (d.pose.x - axis.x) * n.x + (d.pose.y - axis.y) * n.y
		if absf(got - d.offset_m) > 1e-6:
			wrong_side += 1
		worst_off = minf(worst_off, absf(got))
	_ok("привод стоит там, куда его отнёс сервер", wrong_side == 0, "разошлись %d" % wrong_side)
	# ЗА ГАБАРИТОМ ПУТИ. Полшпалы у затравки — 1.375 м; привод, оказавшийся
	# ближе, стоял бы в колее.
	_ok("привод вынесен за габарит пути", worst_off > 1.0, "ближайший %.3f м от оси" % worst_off)

	# ТЕЛО ПРИХОДИТ С СЕРВЕРА. Описание берётся из фикстуры набора — того же
	# файла, что отгружает сервер: смысл проверки в том, что ОТГРУЖАЕМОЕ
	# описание собирается сегодняшним сборщиком.
	var d0: TrackBuild.TurnoutDrive = drives[0]
	var drop := d0.base_drop_m
	var model := _model(String(d0.drive))
	_ok("описание тела механизма %s прочитано" % d0.drive, not model.is_empty())
	if model.is_empty():
		return
	var stand := SwitchStand.build(d0, drop, model)
	_ok("тело собралось без отказа", stand.reason == "", stand.reason)
	_ok("привод сел на брус, а не на головку рельса", drop > 0.0 and stand.position.y < d0.pose.z,
		"подошва %.3f при отметке головки %.3f, рельс %.3f" % [stand.position.y, d0.pose.z, drop])
	_ok("тело встало под приводом", stand.get_child_count() > 0)

	# ПРИВОД БЕЗ ОПИСАНИЯ — ОТКАЗ, А НЕ ПУСТОЕ МЕСТО. Молча пропущенное тело
	# выглядит исправным кадром, в котором стрелку нечем перевести.
	var bodyless := SwitchStand.build(d0, drop, {})
	_ok("без описания привод объясняет, почему он пуст", bodyless.reason != "", bodyless.reason)
	bodyless.queue_free()

	# УКАЗАТЕЛЬ. До снапшота — ничего; присланное — показывает; неизвестное —
	# не показывает. НА СКОЛЬКО поворачивать, клиент не знает: углы в описании.
	_ok("до снапшота положение не показано", stand.shown == "", stand.shown)
	_ok("до снапшота сторона схода не показана", stand.shown_hand == "", stand.shown_hand)
	_ok("присланное положение показано",
		stand.show_position(SwitchStand.POS_DIVERGING, SwitchStand.HAND_RIGHT)
		and stand.shown == SwitchStand.POS_DIVERGING, stand.shown)
	_ok("повтор того же положения ничего не крутит",
		not stand.show_position(SwitchStand.POS_DIVERGING, SwitchStand.HAND_RIGHT), stand.shown)
	_ok("неизвестное положение не показывается",
		not stand.show_position("боком", SwitchStand.HAND_RIGHT)
		and stand.shown == SwitchStand.POS_DIVERGING, stand.shown)

	# ПОКАЗАНИЯ РАЗЛИЧАЮТСЯ, и это единственное, что клиент вправе утверждать об
	# указателе: сами углы — дело описания, а вот «оба положения выглядят
	# одинаково» было бы указателем, который ничего не показывает.
	var head := _find_node(stand, "indicator")
	_ok("у тела есть указатель", head != null)
	if head != null:
		var at_diverging: float = head.rotation.y
		stand.show_position(SwitchStand.POS_STRAIGHT, SwitchStand.HAND_RIGHT)
		_ok("по прямому пути указатель повёрнут иначе, чем на боковой",
			absf(head.rotation.y - at_diverging) > 0.01,
			"%.4f против %.4f рад" % [head.rotation.y, at_diverging])
	var arrow := _find_node(stand, "arrow")
	_ok("у указателя есть стрела", arrow != null)
	if arrow != null:
		var at_right: float = arrow.rotation.y
		stand.show_position(SwitchStand.POS_STRAIGHT, SwitchStand.HAND_LEFT)
		_ok("на левой стрелке стрела смотрит в другую сторону",
			absf(arrow.rotation.y - at_right) > 0.01,
			"%.4f против %.4f рад" % [arrow.rotation.y, at_right])

	# ТАБЛИЧКА: щиток на месте, номер на нём — присланный.
	var plate := _find_node(stand, "board")
	_ok("у стрелки есть табличка", plate != null)
	if plate != null:
		var faces := 0
		for c in plate.get_children():
			var mi := c as MeshInstance3D
			if mi == null:
				continue
			var mat := mi.material_override as StandardMaterial3D
			if mat != null and mat.albedo_texture != null:
				faces += 1
		# Обе стороны: щиток читают с обеих, и пустая изнанка выглядела бы как
		# другая табличка.
		_ok("номер нанесён на обе стороны таблички", faces == 2, str(faces))
	_ok("на табличке присланная метка", stand.label == String((raw[0] as Dictionary).get("name", "")),
		"%s против %s" % [stand.label, String((raw[0] as Dictionary).get("name", ""))])

	# НАДПИСЕЙ В МИРЕ БОЛЬШЕ НЕТ. Проверка держит решение владельца: подписи
	# снесены, и вернуться они могут только правкой, которая уронит эту строку.
	var world_src := FileAccess.get_file_as_string("res://scripts/world.gd")
	# Ищется ЗАВЕДЕНИЕ узла, а не упоминание слова: запись о том, что подписи
	# снесены и почему, обязана остаться в комментарии — она и есть решение.
	_ok("в мире не осталось парящих подписей", not world_src.contains("Label3D.new("),
		"world.gd снова заводит Label3D")

	stand.queue_free()


## _model — описание тела механизма из ОТГРУЖАЕМОГО файла набора. Читается с
## диска репозитория: сервера у чистой проверки нет, а проверять надо то, что
## сервер и отдаёт.
func _model(kind: String) -> Dictionary:
	for name in ["switch_stand_manual", "switch_stand_electric"]:
		var path := "res://../server/assets/%s.model.json" % name
		var text := FileAccess.get_file_as_string(path)
		if text == "":
			continue
		var parsed: Variant = JSON.parse_string(text)
		if not (parsed is Dictionary):
			continue
		var doc := parsed as Dictionary
		if String(doc.get("device", "")) == kind:
			return doc
	return {}


## _find_node — узел по имени где угодно в поддереве. Имя части — часть
## ОПИСАНИЯ, а не клиента: проверка ищет то, что назвал сервер.
func _find_node(root: Node, name_v: String) -> Node3D:
	if root.name == name_v and root is Node3D:
		return root as Node3D
	for c in root.get_children():
		var f := _find_node(c, name_v)
		if f != null:
			return f
	return null
