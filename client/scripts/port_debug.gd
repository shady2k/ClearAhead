## PortDebug — ПОРТЫ ДЕТАЛЕЙ В МИРЕ: где деталь кончилась, где металла нет по
## объявлению и что это за деталь, если по ней щёлкнуть.
##
## # Зачем слой, если смычку доказывает сервер
##
## Сервер её и доказывает: геометрия устройства не уезжает клиенту, пока каждый
## внутренний порт не назвал, чем металл продолжен (track.Validate). Обойти эту
## проверку нельзя — кроме одного места, где её отключают НАРОЧНО: объявленного
## разрыва. Разрыв говорит «металла тут нет, и это задумано», и сборка молчит про
## порт, который он покрыл.
##
## Ровно там и жила ошибка, ради которой слой заведён. 2026-08-17 наружная нитка
## бокового маршрута SW1 начиналась в 0.3546 м от ближайшего металла, а сборка
## молчала: разрыв был объявлен от острия до конца рамных рельсов, и объявление
## оказалось неверным (замер колеи: 1520 мм в острие против 1165 мм у стыка —
## рамный рельс прямого пути вести боковую нитку не может). Числа нашлись в Go
## отдельным замером; в кадре видно было только «рельс начинается не там».
##
## # ПОЧЕМУ ПОДПИСЕЙ В КАДРЕ НЕТ (вторая редакция того же дня)
##
## Первая редакция подписывала каждый конец детали её именем, вторая — номером.
## Обе владелец прочёл одинаково: «это невозможно читать». Он прав, и дело не в
## длине строки. Деталей на стрелке четырнадцать, концов вдвое больше, и ЛЮБАЯ
## подпись у каждого закрывает то, что называет: сам путь.
##
## Теперь в мире стоят только МЕТКИ МЕСТА — булавка и поперечный штрих у конца
## детали, пунктир вдоль объявленного разрыва. Имя и числа показываются ПО
## ЗАПРОСУ: щелчок выбирает деталь, мир подсвечивает её тело, панель печатает
## атрибуты. Спрашивают про одну деталь за раз — и отвечать надо про одну.
##
## Выбор считается ЗДЕСЬ (pick), а не в мире: точки детали и так посчитаны для
## меток, и второй их проход в world.gd означал бы две геометрии одной детали.
##
## # Чего слой НЕ делает
##
## Он НЕ СУДИТ о смычке. Ни одного «этот порт свободен» здесь не считается: судит
## сервер, и второй судья, выводящий то же самое своими правилами, дал бы два
## ответа на один вопрос — то самое, от чего уводит вся модель деталей. Слой
## показывает ровно присланное: концы деталей, их атрибуты и объявленные разрывы.
## Свободный порт узнаётся глазом — рядом с ним не стоит второй.
##
## Точка порта считается ТЕМ ЖЕ правилом, что и всё поперечное у клиента: поза
## оси плюс вынос по левой нормали (контракт отрисовки §4). Второго языка «где это
## поперёк пути» проект не держит, и слой его не заводит.
##
## # Почему поверх всего
##
## Проверка глубины у меток выключена, и это ровно то, что однажды признали
## вредным у ниток крестовины: они висели сквозь рельеф красными полосами на
## горизонте. Разница в том, ЧЕМ вещь является. Нитка — часть мира и обязана жить
## по законам света и перспективы; метка порта миром не является вовсе, она
## инструмент поверх него и по умолчанию невидима (клавиша F2, ключ --ports).
class_name PortDebug

## Булавка порта: столбик вверх от рабочей грани. Высота выбрана так, чтобы он
## был виден в кадре сверху рядом со шпалой (0.28 м) и не закрывал соседний путь.
const PIN_H := 0.60
const PIN_W := 0.04

## Поперечная метка: штрих ПОПЕРЁК пути в точке порта. Нужна отдельно от булавки
## затем, что в виде сверху столбик читается точкой, а стык — это место ВДОЛЬ
## нитки, и глазу нужна линия, по которой видно, совпали два конца или разъехались.
const TICK_LEN := 0.50
const TICK_W := 0.03
const TICK_H := 0.02

## Шаг штриха вдоль объявленного разрыва: разрыв рисуется пунктиром, а не
## сплошной лентой, потому что он и есть ОТСУТСТВИЕ детали — сплошная линия на
## его месте читалась бы деталью.
const GAP_STEP_M := 0.5

## Потолок штрихов на один разрыв: 240 штук по полметра — 120 м пунктира, длиннее
## любого прохода устройства (у Р65 1/9 это 27.6 м). Потолок нужен затем, что
## разрыв вправе быть объявлен на перегоне, и километр пунктира дал бы две тысячи
## узлов. Обрезка НЕ МОЛЧАЛИВАЯ: обрезанное уезжает в skipped и попадает в отчёт.
const GAP_MARKS_MAX := 240

const C_START := Color(0.25, 0.95, 0.55)
const C_END := Color(1.00, 0.50, 0.20)
const C_GAP := Color(0.98, 0.20, 0.18)

## ШАГ ВЫБОРКИ ДЕТАЛИ ДЛЯ ЩЕЛЧКА, метры. Полметра: короче самой короткой детали
## устройства (сердечник 1.8 м) втрое, и попадание по ней не зависит от того,
## куда пришлась выборка.
const PICK_STEP_M := 0.5

## ДОПУСК ЩЕЛЧКА, метры — насколько далеко от оси детали ещё считается попаданием.
## Треть колеи: соседняя нитка отстоит на 1.52 м и в допуск не попадает, а
## промахнуться по рельсу на четверть метра сверху проще простого.
const PICK_TOL_M := 0.5


## build — весь слой одним узлом.
##
## Возвращает узел, ПЕРЕЧЕНЬ ДЕТАЛЕЙ (parts) для выбора щелчком, строки для
## отчёта и числа. Числа не для красоты — «портов 0» на карте со стрелкой
## означает, что слой не построился, и увидеть это надо в отчёте, а не по пустому
## кадру.
##
## Номер детали сквозной по региону и берётся из ПОРЯДКА ПРОВОДА: сервер отдаёт
## детали каноническим порядком (track.sortRails), значит два запуска на одной
## карте дадут одни и те же номера. Появление детали номера сдвинет — и это
## объявлено: номер есть указатель в разговоре, а не тождество. Тождество —
## ключ (rail:3, blade:1), по нему щелчок находит тело.
static func build(network: Dictionary, by_id: Dictionary) -> Dictionary:
	var node := Node3D.new()
	node.name = "Ports"
	var skipped: Array[String] = []
	var index: Array[String] = []
	var parts: Array[Dictionary] = []
	var ports := 0
	var num := 0

	# ВСЕ РЕЛЬСЫ РЕГИОНА одним списком — тем же, из которого показ строит тела
	# (TrackBuild.rails). Второго разбора провода слой не заводит: разойдись они,
	# метка стояла бы не у той детали, которую показывает.
	var wire := -1
	for r_raw in (network.get("rails", []) as Array):
		var r: Dictionary = r_raw as Dictionary
		wire += 1
		var eid := String(r.get("element", ""))
		if not by_id.has(eid):
			skipped.append("порт нитки %s: элемент %s не пришёл" % [r.get("kind", ""), eid])
			continue
		var el: TrackGeom.Element = by_id[eid]
		# ГРАНЬ НА КОНЦЕ — end_face_from и end_face_to, а не face: у нитки с
		# отгибом это разные величины, и порт лежит там, где деталь КОНЧИЛАСЬ, то
		# есть после отгиба. Концов ДВА, и грани у них свои: усовик приходит в
		# горло на половине горла, а за корнем сердечника садится на нитку.
		var face := float(r.get("face", 0.0))
		var face_from := float(r.get("end_face_from", face))
		var face_to := float(r.get("end_face_to", face))
		var from := float(r.get("from", 0.0))
		var to := float(r.get("to", 0.0))
		num += 1
		_port(node, el, from, face_from, C_START)
		_port(node, el, to, face_to, C_END)
		var pname := part_label(r, el)
		index.append("%3d  %-46s %8.3f…%8.3f  %s" % [num, pname, from, to, String(r.get("id", ""))])
		parts.append({
			"key": "rail:%d" % wire, "num": num, "name": pname, "kind": String(r.get("kind", "")),
			"element": eid, "element_name": _where(el), "from": from, "to": to,
			"attrs": r, "points": _sample(el, from, to, face),
			"ends": [_face_point(el, from, face_from), _face_point(el, to, face_to)],
		})
		ports += 2

	# ОСТРЯКИ — такие же детали со своими портами: острие и корень. Приезжают
	# отдельным списком (turnout_blades), потому что подвижны, но стык в корне у
	# них тот же, что у всякого рельса, и прятать его было бы неправдой.
	wire = -1
	for b_raw in (network.get("turnout_blades", []) as Array):
		var b: Dictionary = b_raw as Dictionary
		wire += 1
		var pid := String(b.get("passage", ""))
		if not by_id.has(pid):
			skipped.append("порт остряка: проход %s не пришёл" % pid)
			continue
		var el: TrackGeom.Element = by_id[pid]
		var face := float(b.get("offset", 0.0))
		var length := float(b.get("length", 0.0))
		num += 1
		_port(node, el, 0.0, face, C_START)
		_port(node, el, length, face, C_END)
		var bname := "%s остряк|%s" % [_where(el), b.get("branch", "")]
		index.append("%3d  %-46s %8.3f…%8.3f  остряк, вынос %+0.3f" % [num, bname, 0.0, length, face])
		parts.append({
			"key": "blade:%d" % wire, "num": num, "name": bname, "kind": TrackBuild.BLADE,
			"element": pid, "element_name": _where(el), "from": 0.0, "to": length,
			"attrs": b, "points": _sample(el, 0.0, length, face),
			"ends": [_face_point(el, 0.0, face), _face_point(el, length, face)],
		})
		ports += 2

	var gaps := 0
	wire = -1
	for g_raw in (network.get("rail_gaps", []) as Array):
		var g: Dictionary = g_raw as Dictionary
		wire += 1
		var eid := String(g.get("element", ""))
		if not by_id.has(eid):
			skipped.append("разрыв %s: элемент %s не пришёл" % [g.get("kind", ""), eid])
			continue
		num += 1
		var el: TrackGeom.Element = by_id[eid]
		if _gap(node, el, g):
			skipped.append("разрыв %s на %s длиннее %d штрихов по %.1f м — нарисован не весь" % [
				g.get("kind", ""), eid, GAP_MARKS_MAX, GAP_STEP_M])
		var from := float(g.get("from", 0.0))
		var to := float(g.get("to", 0.0))
		var face := float(g.get("face", 0.0))
		var gname := "%s разрыв|%s" % [_where(el), g.get("kind", "")]
		index.append("%3d  %-46s %8.3f…%8.3f  РАЗРЫВ: %s" % [
			num, gname, from, to, String(g.get("why", ""))])
		# РАЗРЫВ ВЫБИРАЕТСЯ ЩЕЛЧКОМ НАРАВНЕ С ДЕТАЛЬЮ, и это не курьёз: вопрос
		# «почему тут нет металла» задают ровно тому месту, где его нет. Тела у
		# него нет, подсвечивать нечего — панель отвечает объявлением.
		parts.append({
			"key": "gap:%d" % wire, "num": num, "name": gname, "kind": "разрыв",
			"element": eid, "element_name": _where(el), "from": from, "to": to,
			"attrs": g, "points": _sample(el, from, to, face),
			"ends": [_face_point(el, from, face), _face_point(el, to, face)],
		})
		gaps += 1

	return {"node": node, "ports": ports, "gaps": gaps, "index": index,
		"parts": parts, "skipped": skipped}


## pick — какая деталь под лучом взгляда. Возвращает −1, если ни одна.
##
## Луч, а не точка на земле: смотрят на стрелку и сверху, и от привода, а
## пересечение с плоскостью пути дало бы в наклонном виде промах тем больший, чем
## ниже камера.
##
## БЛИЖАЙШАЯ, А ПРИ РАВЕНСТВЕ — КОРОТКАЯ. Остряк лежит на рамном рельсе гранью к
## грани, и обе детали от щелчка равноудалены; спрашивают в этом месте про
## остряк — он и есть то особенное, ради чего сюда смотрят. Правило объявлено
## здесь, чтобы выбор не выглядел случайным.
static func pick(parts: Array[Dictionary], from: Vector3, dir: Vector3) -> int:
	var best := -1
	var best_d := PICK_TOL_M
	var best_len := INF
	for i in parts.size():
		var pts: PackedVector3Array = parts[i]["points"]
		var d := INF
		for p in pts:
			d = minf(d, _ray_distance(from, dir, p))
		if d > best_d + 0.001:
			continue
		var length: float = absf(float(parts[i]["to"]) - float(parts[i]["from"]))
		if absf(d - best_d) <= 0.001 and length >= best_len:
			continue
		best = i
		best_d = d
		best_len = length
	return best


## describe — атрибуты выбранной детали строками, как их прислал сервер.
##
## ПЕЧАТАЕТСЯ ПРИСЛАННОЕ, А НЕ ВЫВЕДЕННОЕ: панель отвечает на вопрос «что мне про
## эту деталь сказали», и вычисленное в ней значение отвечало бы на другой.
## Исключение одно и оно названо: мировые координаты концов — их считает сам слой
## по правилу §4, и без них не увидеть, где деталь лежит на самом деле.
static func describe(part: Dictionary) -> Array[String]:
	var out: Array[String] = []
	out.append("вид: %s" % String(part["kind"]))
	out.append("ось: %s  (%s)" % [String(part["element_name"]), String(part["element"])])
	out.append("отрезок: %.3f … %.3f м  (длина %.3f м)" % [
		float(part["from"]), float(part["to"]),
		absf(float(part["to"]) - float(part["from"]))])
	var ends: Array = part["ends"]
	out.append("начало: x=%.4f  y=%.4f  z=%.4f" % [ends[0].x, -ends[0].z, ends[0].y])
	out.append("конец:  x=%.4f  y=%.4f  z=%.4f" % [ends[1].x, -ends[1].z, ends[1].y])
	var attrs: Dictionary = part["attrs"]
	var keys := attrs.keys()
	keys.sort()
	for k in keys:
		var v = attrs[k]
		if v is Array and (v as Array).size() > 4:
			# Станции сечения — таблица в десятки строк. В панели от неё нужна
			# длина: сама таблица читается там, где её читают, — в проводе.
			out.append("%s: %d записей" % [k, (v as Array).size()])
			continue
		out.append("%s: %s" % [k, v])
	return out


## _port — булавка и поперечный штрих в точке грани.
static func _port(node: Node3D, el: TrackGeom.Element, u: float, face: float, colour: Color) -> void:
	var p := el.pose_at(u)
	var nl := p.left()
	var pos := TerrainMesh.to_godot(p.x + nl.x * face, p.y + nl.y * face, p.z)

	var pin := MeshInstance3D.new()
	var bm := BoxMesh.new()
	bm.size = Vector3(PIN_W, PIN_H, PIN_W)
	pin.mesh = bm
	pin.material_override = _mat(colour)
	pin.position = pos + Vector3(0.0, PIN_H * 0.5, 0.0)
	node.add_child(pin)
	node.add_child(_tick(pos, p.heading, colour))


## _gap — объявленный разрыв пунктиром вдоль своей нитки.
##
## Возвращает true, если штрихи упёрлись в потолок: обрезка обязана быть названа,
## иначе кадр показывал бы короткий разрыв там, где объявлен длинный.
static func _gap(node: Node3D, el: TrackGeom.Element, g: Dictionary) -> bool:
	var from := float(g.get("from", 0.0))
	var to := float(g.get("to", 0.0))
	var face := float(g.get("face", 0.0))
	if to <= from:
		return false
	var marks := int(floor((to - from) / GAP_STEP_M)) + 1
	var cut := marks > GAP_MARKS_MAX
	marks = mini(marks, GAP_MARKS_MAX)
	for i in marks:
		var p := el.pose_at(from + float(i) * GAP_STEP_M)
		var nl := p.left()
		node.add_child(_tick(TerrainMesh.to_godot(
			p.x + nl.x * face, p.y + nl.y * face, p.z), p.heading, C_GAP))
	return cut


## _tick — штрих ПОПЕРЁК пути: коробка, повёрнутая по курсу оси.
##
## Курс переводится в оси движка поворотом вокруг Y: у to_godot плановое
## направление (cos h, sin h) становится (cos h, 0, −sin h), а это и есть
## Vector3.RIGHT, повёрнутый на h. Значит локальный X смотрит вперёд, локальный
## Z — поперёк, и длина штриха кладётся по Z.
static func _tick(pos: Vector3, heading: float, colour: Color) -> MeshInstance3D:
	var mi := MeshInstance3D.new()
	var bm := BoxMesh.new()
	bm.size = Vector3(TICK_W, TICK_H, TICK_LEN)
	mi.mesh = bm
	mi.material_override = _mat(colour)
	mi.transform = Transform3D(Basis(Vector3.UP, heading), pos)
	return mi


## _mat — материал метки: без света и без проверки глубины (разбор — в шапке).
static func _mat(colour: Color) -> StandardMaterial3D:
	var m := StandardMaterial3D.new()
	m.albedo_color = colour
	m.shading_mode = BaseMaterial3D.SHADING_MODE_UNSHADED
	m.cull_mode = BaseMaterial3D.CULL_DISABLED
	m.no_depth_test = true
	m.render_priority = 18
	return m


## highlight_material — чем красится ВЫБРАННАЯ деталь.
##
## Своим цветом поверх её собственного материала, а не подсветкой рядом: спросили
## «что это за деталь», и ответ обязан показать ровно её тело — то, которое
## строит показ. Метка рядом ответила бы «где-то здесь».
static func highlight_material() -> StandardMaterial3D:
	var m := StandardMaterial3D.new()
	m.albedo_color = Color(1.00, 0.85, 0.20)
	m.shading_mode = BaseMaterial3D.SHADING_MODE_UNSHADED
	m.cull_mode = BaseMaterial3D.CULL_DISABLED
	m.render_priority = 17
	return m


## _sample — точки детали в осях движка: по ним ищется попадание щелчка.
##
## Вынос берётся ПОСТОЯННЫЙ (рабочий), а не отгибной: отгиб уводит грань на
## сантиметры, а допуск щелчка — полметра, и считать его здесь значило бы
## повторить закон отгиба ради точности, которой вопрос не требует.
static func _sample(el: TrackGeom.Element, from: float, to: float, face: float) -> PackedVector3Array:
	var out := PackedVector3Array()
	var span := absf(to - from)
	var steps := maxi(1, int(ceil(span / PICK_STEP_M)))
	for i in steps + 1:
		out.append(_face_point(el, from + (to - from) * float(i) / float(steps), face))
	return out


## _face_point — точка рабочей грани в осях движка: поза оси плюс вынос по левой
## нормали. То же правило, каким кладётся всё поперечное (контракт §4).
static func _face_point(el: TrackGeom.Element, u: float, face: float) -> Vector3:
	var p := el.pose_at(u)
	var nl := p.left()
	return TerrainMesh.to_godot(p.x + nl.x * face, p.y + nl.y * face, p.z)


## _ray_distance — расстояние от точки до ЛУЧА (не до прямой): за спиной камеры
## деталей не выбирают.
static func _ray_distance(from: Vector3, dir: Vector3, p: Vector3) -> float:
	var t := (p - from).dot(dir)
	if t <= 0.0:
		return INF
	return (p - (from + dir * t)).length()


## part_label — как деталь называется в перечне и в панели.
##
## Из идентификатора детали выбрасывается UUID владельца: он одинаков у всех
## деталей одной стрелки и занимает всю строку, ничего не различая. Остаётся
## хвост — «stock|straight_inner», — и он же стоит в отказе сборки, так что
## панель и текст отказа называют деталь ОДНИМ И ТЕМ ЖЕ словом.
static func part_label(r: Dictionary, el: TrackGeom.Element) -> String:
	var kind := String(r.get("kind", ""))
	var tail := _tail(String(r.get("id", "")))
	if tail == "":
		tail = kind
	elif not tail.begins_with(kind):
		# У перегонной нитки хвост — «0|left», номер спана и сторона: вида в нём
		# нет, и без него две нитки разных прогонов подписаны одинаково.
		tail = "%s|%s" % [kind, tail]
	return "%s %s" % [_where(el), tail]


## _tail — идентификатор без ведущего UUID.
static func _tail(id: String) -> String:
	if id == "":
		return ""
	var parts := id.split("|", false)
	if parts.size() > 1 and parts[0].length() == 36 and parts[0].contains("-"):
		parts.remove_at(0)
	return "|".join(parts)


## _where — на какой оси лежит деталь. У прохода стрелки это метка САМОГО
## устройства (сервер даёт обоим проходам имя стрелки), и без ветви два прохода
## SW1 назывались бы одинаково.
static func _where(el: TrackGeom.Element) -> String:
	var base := el.name if el.name != "" else el.id.right(6)
	var branch := String(el.role.get("branch", ""))
	if branch == "":
		return base
	return "%s:%s" % [base, "прямой" if branch == "straight" else "боковой"]
