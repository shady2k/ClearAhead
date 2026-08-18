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
	# УЗЕЛ КЛАДЁТСЯ В ДЕРЕВО, и это не оформление. Godot считает global_transform
	# только для узлов В ДЕРЕВЕ; у отсоединённой ветки он отдаёт МЕСТНЫЙ поворот.
	# Первый замер указателя (ниже) на этом и обманулся: щиток отвечал единичным
	# базисом при любом положении, и проверка «ребром к ходу» проходила от того,
	# что мерить было нечего.
	ctx.tree.root.add_child(stand)
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

	# УКАЗАТЕЛЬ ЧИТАЕТСЯ С ПУТИ, И ЧИТАЕТСЯ ПРАВИЛЬНО — а не просто «показывает
	# два разных угла».
	#
	# Проверки выше держали РАЗЛИЧИМОСТЬ: прямое положение выглядит не так, как
	# боковое, стрела правой стрелки — не так, как левой. Различимость проходит и
	# у ЗЕРКАЛЬНОГО указателя: поменяй два угла местами — и он покажет боковое
	# положение за прямое, а обе проверки останутся зелёными. Владелец увидел
	# именно это (2026-08-15): «подсказка для стрелки показывает в неверном
	# направлении».
	#
	# Ниже проверяется СМЫСЛ, по ИСИ, и в осях мира, а не в углах описания:
	#
	#   по прямому пути — щиток РЕБРОМ к подъезжающему (видна полоса);
	#   на боковой      — щиток ПЛАШМЯ, и стрела указывает в сторону схода.
	#
	# Ось пути берётся из позы привода, потому что именно вдоль неё и поставлен
	# узел механизма (SwitchStand.build).
	var vane := _find_node(stand, "vane")
	_ok("у стрелы есть щиток", vane != null)
	if vane != null:
		var f := d0.pose.forward()
		var along := Vector3(f.x, 0.0, -f.y)          # ход по возрастанию u в осях движка
		var l := d0.pose.left()
		var to_left := Vector3(l.x, 0.0, -l.y)        # левая рука хода там же
		stand.show_position(SwitchStand.POS_STRAIGHT, SwitchStand.HAND_RIGHT)
		# Лицо щитка — нормаль пластины, ось Z её узла.
		var face_straight: Vector3 = vane.global_transform.basis.z.normalized()
		_ok("по прямому пути щиток стоит ребром к подъезжающему",
			absf(face_straight.dot(along)) < 0.2,
			"лицо к ходу %.3f (ребро — около нуля)" % face_straight.dot(along))
		stand.show_position(SwitchStand.POS_DIVERGING, SwitchStand.HAND_RIGHT)
		var face_div: Vector3 = vane.global_transform.basis.z.normalized()
		_ok("на боковой путь щиток разворачивается плашмя",
			absf(face_div.dot(along)) > 0.8,
			"лицо к ходу %.3f (плашмя — около единицы)" % face_div.dot(along))
		# Стрела нарисована в сторону +X щитка (знак mark в описании), и на
		# правой стрелке она обязана смотреть ВПРАВО от хода.
		var tip: Vector3 = vane.global_transform.basis.x.normalized()
		_ok("на правой стрелке стрела указывает вправо от хода",
			tip.dot(to_left) < -0.7,
			"стрела к левой руке %.3f (вправо — около минус единицы)" % tip.dot(to_left))
		stand.show_position(SwitchStand.POS_DIVERGING, SwitchStand.HAND_LEFT)
		var tip_left: Vector3 = vane.global_transform.basis.x.normalized()
		_ok("на левой стрелке стрела указывает влево от хода",
			tip_left.dot(to_left) > 0.7,
			"стрела к левой руке %.3f (влево — около единицы)" % tip_left.dot(to_left))

		# ОБЕ СТОРОНЫ ЩИТКА ПОКАЗЫВАЮТ В ОДНУ СТОРОНУ МИРА.
		#
		# Настоящая стрела на железном щитке смотрит туда, куда уходит боковой
		# путь, с какого конца на неё ни гляди. Изнанка же делается поворотом на
		# 180°, и без зеркала развёртки она смотрит ровно наоборот — а подходят к
		# SW1 затравки как раз со стороны крестовины, то есть на изнанку.
		#
		# Направление знака считается из ДВУХ множителей: куда повёрнут сам щиток
		# и в какую сторону положена развёртка. Второй множитель здесь и
		# проверяется — без него зеркало можно снять, не уронив ни строки.
		var dirs: Array[Vector3] = []
		for c in vane.get_children():
			var mi := c as MeshInstance3D
			if mi == null:
				continue
			var m := mi.material_override as StandardMaterial3D
			# Лица отбираются по НАНЕСЁННОМУ РИСУНКУ, а не по имени узла: третий
			# ребёнок щитка — сама пластина, и знака на ней нет.
			if m == null or m.albedo_texture == null:
				continue
			var flip := -1.0 if m.uv1_scale.x < 0.0 else 1.0
			dirs.append(mi.global_transform.basis.x.normalized() * flip)
		_ok("у щитка обе стороны", dirs.size() == 2, str(dirs.size()))
		if dirs.size() == 2:
			_ok("знак с изнанки указывает в ту же сторону мира, что с лица",
				dirs[0].dot(dirs[1]) > 0.99,
				"лицо %s, изнанка %s" % [dirs[0], dirs[1]])

	# ПЕРЕВОДНАЯ ТЯГА. Длина у неё НЕ СВОЙСТВО ТЕЛА: это расстояние от станины до
	# остряка, а вынос станины считает сервер. До 2026-08-16 у ручного привода
	# тяги не было вовсе, а у электрического она была длиной 0.6 м при
	# расстоянии до нитки 1.115 м — то есть висела в воздухе (ClearAhead-bsjq,
	# слово владельца: «сам девайс стоит, но он не прикреплён к рельсу»).
	#
	# Проверяется, что длина ПРИШЛА и ПРИМЕНИЛАСЬ: часть, оставшаяся единичной,
	# и есть тяга, не дотянувшаяся до рельса.
	_ok("сервер прислал длину тяги", d0.reach_straight_m > 0.0 and d0.reach_diverging_m > 0.0,
		"прямо %.3f, на боковую %.3f" % [d0.reach_straight_m, d0.reach_diverging_m])
	# ТЯГА ДЛИННЕЕ ВЫНОСА СТАНИНЫ, то есть пересекает путь, а не торчит вбок.
	# Сравнивается с ПРИСЛАННЫМ выносом, а не с половиной колеи: колеи у привода
	# своей нет, и вписать её сюда числом значило бы выдумать факт о станции
	# ровно там, где проверяют, что клиент их не выдумывает.
	_ok("тяга длиннее выноса станины — она пересекает путь",
		d0.reach_straight_m > absf(d0.offset_m),
		"тяга %.3f м при выносе %.3f м" % [d0.reach_straight_m, absf(d0.offset_m)])
	var rod := _find_node(stand, "rod")
	_ok("у механизма есть тяга", rod != null)
	if rod != null:
		stand.show_position(SwitchStand.POS_STRAIGHT, SwitchStand.HAND_RIGHT, 0.0)
		var at_straight: float = rod.scale.z
		_ok("тяга растянута на присланную длину",
			absf(at_straight - d0.reach_straight_m) < 1e-6,
			"масштаб %.4f при длине %.4f м" % [at_straight, d0.reach_straight_m])
		stand.show_position(SwitchStand.POS_DIVERGING, SwitchStand.HAND_RIGHT, 1.0)
		_ok("на боковом пути тяга иной длины — она ходит вместе с остряком",
			absf(rod.scale.z - d0.reach_diverging_m) < 1e-6,
			"масштаб %.4f при длине %.4f м" % [rod.scale.z, d0.reach_diverging_m])
		# И ПОСЕРЕДИНЕ ПЕРЕВОДА — посередине: длина идёт вместе с остряком, а не
		# скачком в конце.
		stand.show_position(SwitchStand.POS_DIVERGING, SwitchStand.HAND_RIGHT, 0.5)
		var mid := (d0.reach_straight_m + d0.reach_diverging_m) * 0.5
		_ok("на середине перевода тяга посередине", absf(rod.scale.z - mid) < 1e-6,
			"масштаб %.4f при середине %.4f м" % [rod.scale.z, mid])

	# ИСИ ПРОВЕРЯЕТСЯ У КАЖДОГО ТЕЛА, А НЕ У ПЕРВОГО ПОПАВШЕГОСЯ.
	#
	# Всё, что выше, построено на drives[0] — то есть на ОДНОМ механизме из двух.
	# Углы указателя лежат в описании тела, у каждого рода своём, и проверка
	# одного тела не говорит о другом ровно ничего. Владелец увидел это раньше
	# проверки: «опять они смотрят не в ту сторону» — про кадр с электроприводом,
	# которого ни одна строка выше не касалась.
	for d_raw in drives:
		var dd: TrackBuild.TurnoutDrive = d_raw
		var mm := _model(String(dd.drive))
		if mm.is_empty():
			continue
		var st2 := SwitchStand.build(dd, dd.base_drop_m, mm)
		ctx.tree.root.add_child(st2)
		var vn := _find_node(st2, "vane")
		if vn == null:
			_ok("у механизма %s есть щиток указателя" % dd.drive, false)
			st2.queue_free()
			continue
		var f2 := dd.pose.forward()
		var along2 := Vector3(f2.x, 0.0, -f2.y)
		var l2 := dd.pose.left()
		var left2 := Vector3(l2.x, 0.0, -l2.y)
		st2.show_position(SwitchStand.POS_STRAIGHT, SwitchStand.HAND_RIGHT)
		var n_str: Vector3 = vn.global_transform.basis.z.normalized()
		_ok("%s: по прямому пути щиток ребром к подъезжающему" % dd.drive,
			absf(n_str.dot(along2)) < 0.2, "лицо к ходу %.3f" % n_str.dot(along2))
		st2.show_position(SwitchStand.POS_DIVERGING, SwitchStand.HAND_RIGHT)
		var n_div: Vector3 = vn.global_transform.basis.z.normalized()
		_ok("%s: на боковой путь щиток плашмя" % dd.drive,
			absf(n_div.dot(along2)) > 0.8, "лицо к ходу %.3f" % n_div.dot(along2))
		var tip2: Vector3 = vn.global_transform.basis.x.normalized()
		_ok("%s: на правой стрелке стрела указывает вправо от хода" % dd.drive,
			tip2.dot(left2) < -0.7, "стрела к левой руке %.3f" % tip2.dot(left2))

		# МАШИНИСТУ ЕСТЬ ЧТО ВИДЕТЬ В ОБОИХ ПОЛОЖЕНИЯХ. Пластин у указателя две,
		# крест-накрест: стрела и белая полоса. По прямому пути к машинисту
		# повёрнута ПОЛОСА, на боковой — СТРЕЛА.
		#
		# Одной пластины не хватало, и это видно было только с пути: щиток,
		# стоящий ребром, — это три сантиметра торца, то есть ничего. Со стороны
		# же (а игрок стоит именно сбоку, он и переводит стрелку рукой) видна
		# стрела — при прямом положении. Владелец прочёл это как «опять смотрят не
		# в ту сторону», и был прав: сигнал читался наоборот.
		#
		# Нашёл это СТЕНД ПРЕДМЕТА (make bench) за пять секунд — после того как
		# те же полчаса ушли на снимки мира с небом и тенями.
		var stripe := _find_node(st2, "ahead")
		_ok("%s: у указателя есть знак прямого пути" % dd.drive, stripe != null)
		if stripe != null:
			st2.show_position(SwitchStand.POS_STRAIGHT, SwitchStand.HAND_RIGHT)
			var s_str: Vector3 = stripe.global_transform.basis.z.normalized()
			_ok("%s: по прямому пути к машинисту повёрнут знак «прямо»" % dd.drive,
				absf(s_str.dot(along2)) > 0.8, "лицо к ходу %.3f" % s_str.dot(along2))
			st2.show_position(SwitchStand.POS_DIVERGING, SwitchStand.HAND_RIGHT)
			var s_div: Vector3 = stripe.global_transform.basis.z.normalized()
			_ok("%s: на боковой знак «прямо» уходит ребром, уступая стреле" % dd.drive,
				absf(s_div.dot(along2)) < 0.2, "лицо к ходу %.3f" % s_div.dot(along2))
		st2.queue_free()

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
	#
	# 2026-08-17 Label3D в клиенте появился снова — в слое портов (port_debug.gd),
	# и граница проведена ровно здесь: слой МИРОМ НЕ ЯВЛЯЕТСЯ. Он невидим, пока его
	# не позовут ключом --ports или клавишей F2, и то, что он невидим по умолчанию,
	# проверяется отдельно (checks/pure/49_ports.gd). Строка ниже осталась прежней и
	# смотрит на world.gd: подпись, заведённая ТАМ, — это подпись в мире.
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
