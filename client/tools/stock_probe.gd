extends SceneTree
## ЗОНД ПОСТАНОВКИ ПОДВИЖНОГО СОСТАВА. Отвечает числами на вопрос, на который
## снимок отвечает спорами: стоит ли машина НА рельсах.
##
## Довод тот же, что у зонда ходьбы, и он куплен этим же заходом: на кадре из
## кабины видно, что «локомотив чуть сдвинут», а насколько и в какую сторону —
## не видно, и спорить об этом можно долго. Тут разница считается в сантиметрах
## против ПРИСЛАННЫХ сервером колеи и ширины головки.
##
##   godot --path client --script res://tools/stock_probe.gd -- --server=… --region=ST_A
##
## --headless НЕ ГОДИТСЯ по той же причине, что у walk_probe: мир строится
## сценой, а сцене нужен вьюпорт. Окно уводится за экран.
##
## ЧТО МЕРЯЕТСЯ. Габарит доехавшего меша приводится в СИСТЕМУ КООРДИНАТ ЕДИНИЦЫ
## (x — поперёк оси, y — вверх от поверхности катания, z — вдоль хода). В этой
## системе ответ и читается:
##
##   поперёк   центр меша обязан лежать на оси пути;
##   вверх     низ колеса обязан быть у нуля, а не над ним и не под ним глубоко;
##   вдоль     центр меша обязан совпасть с точкой отсчёта единицы;
##   колея     наружные грани колёс обязаны накрывать головки рельсов.
##
## Первые три — свойства ПОСТАНОВКИ (перевод осей, якорь, сдвиг каталога).
## Четвёртое — свойство МАСШТАБА, и оно отдельно: меш может стоять по оси
## идеально и всё равно не попадать колёсами на нитки.

const SETTLE_WAIT := 60.0   # с — сколько ждём мир и двадцать мегабайт вида

var _app: Node
var _t := 0.0
var _fails := 0
var _running := false


func _initialize() -> void:
	_app = (load("res://scenes/main.tscn") as PackedScene).instantiate()
	root.add_child(_app)


func _physics_process(delta: float) -> bool:
	_t += delta
	var w := _world()
	if w == null or not _placed(w):
		if _t > SETTLE_WAIT:
			print("зонд: подвижной состав не появился за %.0f с" % SETTLE_WAIT)
			quit(1)
			return true
		return false
	# ЗАМЕР УХОДИТ В КОРУТИНУ, а шаг физики возвращает false, и это не стиль:
	# `true` из _physics_process гасит цикл движка НЕМЕДЛЕННО, вместе с await
	# внутри замера. Первый заход так и оборвался — на экран успели выйти четыре
	# строки из пяти, а пятая ждала ответа сервера.
	if not _running:
		_running = true
		_run(w)
	return false


## _world — узел мира где угодно в дереве оболочки.
##
## Обходом, а не по известному пути: оболочка вправе переставить мир под другой
## узел, и зонд, знающий путь наизусть, сломался бы молча — «состав не появился»
## вместо «мир лежит не там».
## _run — замер и выход. Отдельно от шага физики, потому что внутри есть await:
## сеть спрашивается у сервера, и вернуть управление движку на это время надо.
func _run(w: Node) -> void:
	await _measure(w)
	quit(1 if _fails > 0 else 0)


func _world() -> Node:
	return _find(root)


func _find(n: Node) -> Node:
	if n.get("_stock_units") != null:
		return n
	for c in n.get_children():
		var got := _find(c)
		if got != null:
			return got
	return null


func _placed(w: Node) -> bool:
	var units: Array = w.get("_stock_units")
	if units == null or units.is_empty():
		return false
	# Ждём именно МЕШ: коробка встаёт сразу, и по ней меряется постановка
	# коробки, а не вида.
	for u in units:
		if not u.mesh_shown:
			return false
	return true


func _measure(w: Node) -> void:
	var units: Array = w.get("_stock_units")
	var types: Dictionary = w.get("_stock_types")
	print("=== зонд постановки: единиц %d ===" % units.size())
	for u_raw in units:
		var u = u_raw
		var t := types.get(u.type_id, {}) as Dictionary
		var node: Node3D = u.node
		var aabb := _aabb_local(node, node)
		if aabb.size == Vector3.ZERO:
			print("  %s: у вида нет ни одного меша" % u.id)
			_fails += 1
			continue
		var c := aabb.position + aabb.size * 0.5
		print("  %s (%s), длина паспорта %.2f м" % [u.id, u.type_id, float(t.get("length", 0.0))])
		print("    габарит меша в осях единицы: x %.3f…%.3f  y %.3f…%.3f  z %.3f…%.3f" % [
			aabb.position.x, aabb.end.x, aabb.position.y, aabb.end.y, aabb.position.z, aabb.end.z])
		_check("поперёк оси центр меша", c.x, 0.0, 0.05, "м")
		_check("низ меша от головки рельса", aabb.position.y, 0.0, 0.10, "м")
		_check("вдоль хода центр меша", c.z, 0.0, 0.10, "м")
		_check("длина меша против паспорта", aabb.size.z, float(t.get("length", 0.0)), 0.30, "м")
		_axis_offset(u)
		await _cab(node, u)
		await _wheels(w, node)


## _cab — СТОИТ ЛИ МАШИНИСТ В КАБИНЕ. Второй вопрос того же рода, что колёса на
## рельсах, и заданный по той же причине.
##
## Кадр из кабины на него не отвечает. Из глаз человека, оказавшегося в полу по
## колено или под потолком макушкой, видно РОВНО ТО ЖЕ: рамку лобового стекла и
## лес впереди. А если пост промахнулся на метр вдоль машины, кадр будет
## отличаться от правильного оконным переплётом, о котором можно спорить.
##
## ЛУЧОМ ПО НАРИСОВАННОМУ, а не по объявленному, и это тот же приём, каким
## человек спрашивает землю под ногами. Проверяется НЕ то, что число из каталога
## равно числу из каталога, — это тавтология, — а то, что под объявленной точкой
## есть НАРИСОВАННЫЙ пол, а над ней нарисованный потолок, и между ними влезает
## человек ростом 1.80.
##
## Меши состава твердью не становятся (world.gd::_build_solid проходит по миру
## ДО того, как доедет вид), поэтому твердь для луча строится здесь и живёт
## ровно один замер.
func _cab(unit: Node3D, u) -> void:
	if u.cabs.is_empty():
		print("    (постов машиниста у вида нет — в машину не сесть)")
		_fails += 1
		return
	var bodies: Array[Node] = []
	for mi in _meshes(unit):
		mi.create_trimesh_collision()
		var body := mi.get_child(mi.get_child_count() - 1)
		bodies.append(body)
	# Тела созданы в этом же кадре, и физический сервер о них ещё не знает: луч,
	# пущенный сразу, уходит в пустоту. Та же грабля, что у постановки человека
	# на твердь (Driver._settle), и лечится так же — шагом физики.
	await unit.get_tree().physics_frame

	var space := unit.get_world_3d().direct_space_state
	for i in u.cabs.size():
		var at: Vector3 = unit.to_global(u.cabs[i])
		print("    пост %d: %.3f, %.3f, %.3f (в осях единицы %.3f, %.3f, %.3f)" % [
			i, at.x, at.y, at.z, u.cabs[i].x, u.cabs[i].y, u.cabs[i].z])
		# ПОЛ. Луч сверху вниз с высоты пояса: снизу вверх он упёрся бы в пол же,
		# но с изнанки, и назвал бы его потолком.
		var floor_y := _hit(space, at + Vector3.UP * 0.9, Vector3.DOWN * 2.0)
		if is_finite(floor_y):
			_check("пол кабины под объявленным постом", floor_y, at.y, 0.05, "м")
		else:
			print("    МИМО под постом %d нет нарисованного пола на 2 м вниз" % i)
			_fails += 1
		# ПОТОЛОК. Человек ростом 1.80 обязан влезть стоя — иначе он в кабине не
		# стоит, а торчит из крыши.
		var roof_y := _hit(space, at + Vector3.UP * 0.1, Vector3.UP * 4.0)
		if is_finite(roof_y):
			var head := roof_y - at.y
			print("    потолок кабины на %.3f м над полом (человеку нужно %.2f м)" % [
				head, Driver.BODY_H])
			if head < Driver.BODY_H:
				print("    МИМО в кабине не встать: %.3f м при росте %.2f м" % [head, Driver.BODY_H])
				_fails += 1
		else:
			print("    МИМО над постом %d нет нарисованного потолка на 4 м вверх" % i)
			_fails += 1
	for b in bodies:
		b.queue_free()


## _hit — отметка первого пересечения луча, либо NAN.
func _hit(space: PhysicsDirectSpaceState3D, from: Vector3, delta: Vector3) -> float:
	var q := PhysicsRayQueryParameters3D.create(from, from + delta)
	var got := space.intersect_ray(q)
	return NAN if got.is_empty() else (got["position"] as Vector3).y


## _meshes — все меши поддерева, плоским списком.
func _meshes(node: Node) -> Array[MeshInstance3D]:
	var out: Array[MeshInstance3D] = []
	var mi := node as MeshInstance3D
	if mi != null and mi.mesh != null:
		out.append(mi)
	for c in node.get_children():
		out.append_array(_meshes(c))
	return out


## _axis_offset — ОТХОД ТЕЛА ОТ ОСИ ПО ВСЕЙ ДЛИНЕ.
##
## Проверка, которой не было и которая стоила кадра владельца. Прежние четыре
## числа мерились В ОСЯХ ЕДИНИЦЫ — а в них жёсткое тело идеально по построению,
## сколько бы ни уходило от кривой. Отход виден только в осях МИРА, и мерить его
## надо не в точке отсчёта (там он ноль всегда), а вдоль всей машины.
##
## Ноль здесь недостижим и не нужен: жёсткое тело на кривой ложится хордой, и
## провес хорды — это физика, а не ошибка. Допуск назван от неё: для базы 24.7 м
## на R=500 провес 0.15 м, и предел 0.20 м ловит возврат к постановке по
## касательной (та давала 0.29 м), не придираясь к самой хорде.
func _axis_offset(u) -> void:
	var el: TrackGeom.Element = u.element
	if el == null:
		print("    (элемент не сохранён — отход от оси не померить)")
		_fails += 1
		return
	var half: float = u.length_m * 0.5
	var worst := 0.0
	var worst_at := 0.0
	var steps := 24
	for i in range(steps + 1):
		var t := -half + (2.0 * half) * float(i) / float(steps)
		# Точка тела: вдоль собственной оси машины, то есть там, где нарисован меш.
		var body: Vector3 = u.node.global_transform * Vector3(0.0, 0.0, t)
		# Точка оси: на том же удалении вдоль пути от точки отсчёта.
		var a := el.pose_at(u.u_m + t)
		var axis := TerrainMesh.to_godot(a.x, a.y, a.z)
		var d := Vector2(body.x - axis.x, body.z - axis.z).length()
		if d > worst:
			worst = d
			worst_at = t
	print("    отход тела от оси: наибольший %.3f м на %+.1f м от точки отсчёта" % [worst, worst_at])
	_check("наибольший отход тела от оси", worst, 0.0, 0.20, "м")


## _wheels — САМОЕ ГЛАВНОЕ ЧИСЛО зонда: попадают ли колёса на головки рельсов.
##
## Колёса ищутся не по именам узлов чужого файла, а ПО ВЫСОТЕ: колесо — это то,
## что достаёт до уровня головки рельса и ниже. Имена узлов сегодня «bogey_3», у
## следующего ассета будут другие, а низ у колеса будет всегда.
##
## Сравнивается с ПРИСЛАННЫМИ числами: колея и ширина головки берутся из ответа
## сервера, а не из константы зонда, — иначе зонд проверял бы согласие клиента с
## самим собой.
func _wheels(w: Node, unit: Node3D) -> void:
	var api = w.get("api")
	var rev := int(w.get("stats").get("revision", 0))
	var net = await api.network(String(w.get("region")), rev)
	if not net.have():
		print("    (сеть не приехала: %s)" % net.reason)
		_fails += 1
		return
	var types := net.data.get("track_types", []) as Array
	if types.is_empty():
		print("    (сервер не прислал ни одного типа пути — сравнивать не с чем)")
		_fails += 1
		return
	var tt := types[0] as Dictionary
	var gauge := float(tt.get("gauge", 0.0))
	var head := float(tt.get("rail", {}).get("head_width", 0.0))
	# Головка рельса лежит от рабочей грани наружу — ровно так её кладёт клиент
	# (track_view.rail_body_mesh: inner = gauge/2, outer = inner + head_width).
	var rail_in := gauge * 0.5
	var rail_out := rail_in + head

	var hi := -INF
	for mi in _low_meshes(unit, unit):
		hi = maxf(hi, mi.y)
	if not is_finite(hi):
		print("    (ни один меш не достаёт до головки рельса — колёс не нашлось)")
		_fails += 1
		return
	# ЧТО ИМЕННО МЕРЯЕТСЯ, названо точно: габарит меша даёт НАРУЖНУЮ грань
	# колеса и не даёт внутреннюю — гребень и обод лежат в одном меше с осью, и
	# просвет между колёсами по габариту не виден. Поэтому проверка одна, зато
	# честная: наружная грань колеса против наружной грани головки.
	print("    колея: головка рельса %.3f…%.3f м от оси, наружная грань колеса %.3f м" % [
		rail_in, rail_out, hi])
	# Допуск 30 мм в плюс: колесо ШИРЕ головки по устройству (реборда и запас на
	# извилистое движение), и вылет наружу — норма. Уход ВНУТРЬ от рабочей грани
	# означал бы, что колесо провалилось между нитками.
	_check("наружная грань колеса за наружной гранью головки", hi, rail_out + 0.015, 0.03, "м")


## _low_meshes — поперечные границы мешей, достающих до уровня головки рельса.
## Возвращает пары (левая, правая) в осях единицы.
func _low_meshes(node: Node, origin: Node3D) -> Array:
	var out: Array = []
	for c in node.get_children():
		out.append_array(_low_meshes(c, origin))
	var mi := node as MeshInstance3D
	if mi != null and mi.mesh != null:
		var rel := origin.global_transform.affine_inverse() * mi.global_transform
		var box: AABB = rel * mi.get_aabb()
		# Колесо — это то, что уходит НИЖЕ головки рельса: у гребня иначе не
		# бывает. Порог был 5 см над нулём и ловил лишнее: у кузова низ на
		# +0.045, он проходил и давал «наружную грань колеса» 1.684 м — то есть
		# половину ширины машины. Ноль отсекает всё, кроме гребней.
		if box.position.y < 0.0:
			out.append(Vector2(absf(box.position.x), absf(box.end.x)))
	return out


## _check — сравнение с допуском. Допуск НАЗВАН у каждой строки, а не общий:
## 5 см поперёк — это уже заметно глазом на кадре, 10 см вдоль — нет.
func _check(what: String, got: float, want: float, tol: float, unit: String) -> void:
	var d := absf(got - want)
	var mark := "ok  " if d <= tol else "МИМО"
	if d > tol:
		_fails += 1
	print("    %s %s: %.3f %s, ожидалось %.3f ± %.2f (расхождение %.3f)" % [
		mark, what, got, unit, want, tol, got - want])


## _aabb_local — габарит всех мешей поддерева в системе координат узла единицы.
##
## Считается по ВИДИМЫМ мешам, а не по объявленным числам: вопрос зонда именно
## в том, совпало ли нарисованное с объявленным.
func _aabb_local(node: Node, origin: Node3D) -> AABB:
	var out := AABB()
	var first := true
	for child in node.get_children():
		var sub := _aabb_local(child, origin)
		if sub.size != Vector3.ZERO:
			out = sub if first else out.merge(sub)
			first = false
	var mi := node as MeshInstance3D
	if mi != null and mi.mesh != null:
		var box := mi.get_aabb()
		var rel := origin.global_transform.affine_inverse() * mi.global_transform
		var own := rel * box
		out = own if first else out.merge(own)
	return out
