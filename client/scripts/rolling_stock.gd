class_name RollingStock
extends RefCounted

## ПОДВИЖНОЙ СОСТАВ — первое, что в этом мире имеет ПОЛОЖЕНИЕ, а не место.
##
## Разница не в словах. У дерева, платформы и упора место записано в карте и не
## меняется никогда; у машины положение принадлежит ПАРТИИ и завтра поедет. Отсюда
## три разных источника, и клиент не вправе смешивать их в один:
##
##   /content                какие машины бывают: габарит и ссылка на вид;
##   /regions/{r}/live       что где стоит: тип и точка на элементе;
##   /assets/sha256-…        байты вида, отдельным запросом, кэш навсегда.
##
## Клиент не придумывает ничего из этого. Прежний клиент снесли в том числе за
## строку `LOCO_ELEMENT = "E_MAIN"` — за локомотив, которого на карте сервера не
## было вовсе.
##
## # Два шага показа, и второй не обязателен
##
## Сначала машина рисуется ГАБАРИТНОЙ КОРОБКОЙ по присланным числам, и это
## законная картинка, а не заглушка: длина, ширина и высота приехали с сервера,
## положение тоже. Когда доедут двадцать мегабайт вида — коробка сменяется мешем.
##
## Так сделано потому, что ассет большой, а положение маленькое: ждать меш,
## чтобы показать, ГДЕ стоит машина, значило бы держать пустой кадр ради
## украшения. И если вид не доехал вовсе — на экране остаётся честная коробка с
## отметкой в HUD, а не пустое место.
##
## # Чего здесь нет
##
## Ни скорости, ни ускорения, ни интерполяции. Сервер их не присылает, потому
## что ничего не движется; когда пришлёт — сюда придёт счисление пути, и оно
## будет ОБЪЯВЛЕННОЙ кинематикой показа, а не второй физикой.

## Цвет габаритной коробки. Намеренно НЕ похож на локомотив: коробка обязана
## читаться как «вид ещё не доехал», а не как машина странного цвета.
const BOX_COLOUR := Color(0.85, 0.42, 0.16)
const BOX_ALPHA := 0.55


## Unit — одна поставленная единица: что показываем и чем можем заменить.
class Unit extends RefCounted:
	var id := ""
	var type_id := ""
	var appearance := ""
	var node: Node3D = null
	var box: Node3D = null
	var mesh_shown := false
	## pose — поза точки отсчёта в осях мира (x, y — план, z — поверхность
	## катания), плюс курс. Хранится, чтобы поставить меш, когда он доедет, не
	## пересчитывая элемент заново.
	var pose: TrackGeom.AxisPoint = null
	var length_m := 0.0
	var width_m := 0.0
	var height_m := 0.0
	var bogie_base_m := 0.0
	var reversed := false
	## Элемент и смещение, на которых машина стоит. Держатся не для отрисовки —
	## она уже случилась, — а для ЗОНДА: спросить «а где здесь ось» можно только
	## у того же элемента, по которому машину и ставили.
	var element: TrackGeom.Element = null
	var u_m := 0.0
	## ПОСТЫ МАШИНИСТА в осях узла единицы — уже после масштаба и сдвига каталога.
	## Пусто значит «в эту машину сесть некуда», и это законно: у вагона кабины
	## нет. Считаются при постановке, а не при посадке, потому что живут ровно
	## столько же, сколько сама единица.
	var cabs: Array[Vector3] = []


## place — ставит единицы живого состояния на путь.
##
## Возвращает {units, skipped, notes}. Пропуск — не отказ
## загрузки: единица, которую невозможно поставить, называется словами в HUD, а
## остальной мир рисуется. Причина в том, что состояние приходит отдельно от
## сети и может обогнать её на одну правку.
##
## КАТАЛОГ ВИДОВ НУЖЕН ЗДЕСЬ, хотя байты вида доедут много позже: посты
## машиниста объявлены записью каталога, а не паспортом, и приезжают вместе с
## ней — то есть килобайтами, а не двадцатью мегабайтами. Сесть в машину можно
## раньше, чем доедет её вид; на экране это будет коробка изнутри, и это честная
## картинка ровно того, что известно.
static func place(parent: Node3D, live: Dictionary, types: Dictionary,
		assets: Dictionary, elements: Array[TrackGeom.Element]) -> Dictionary:
	var by_id := {}
	for el in elements:
		by_id[el.id] = el

	var out: Array[Unit] = []
	var skipped: Array[String] = []
	# ЗАМЕЧАНИЕ — НЕ ПРОПУСК, и это разные списки нарочно. Пропущенная единица не
	# нарисована вовсе; единица с испорченным постом стоит на своём месте и верна
	# во всём, кроме того, что в неё нельзя сесть. Слить их в один список значило
	# бы сказать «локомотива нет» про локомотив, который видно.
	var notes: Array[String] = []
	for raw in (live.get("units", []) as Array):
		var d := raw as Dictionary
		var uid := String(d.get("id", ""))
		var type_id := String(d.get("type", ""))
		var at := d.get("at", {}) as Dictionary
		var el_id := String(at.get("element", ""))

		# Тип обязан быть в наборе. Подставить «средний локомотив» нельзя: это
		# ровно то выдумывание размеров, за которое снесли прежний клиент.
		if not types.has(type_id):
			skipped.append("%s: типа %s нет в наборе контента" % [uid, type_id])
			continue
		if not by_id.has(el_id):
			skipped.append("%s: элемента %s нет в сети" % [uid, el_id])
			continue

		var t := types[type_id] as Dictionary
		var el := by_id[el_id] as TrackGeom.Element
		var u := Unit.new()
		u.id = uid
		u.type_id = type_id
		u.appearance = String(t.get("appearance", ""))
		u.length_m = float(t.get("length", 0.0))
		u.width_m = float(t.get("width", 0.0))
		u.height_m = float(t.get("height", 0.0))
		u.reversed = String(at.get("direction", "")) == "reverse"
		u.bogie_base_m = float(t.get("bogie_base", 0.0))
		var u_ref := float(at.get("u", 0.0))
		u.element = el
		u.u_m = u_ref
		u.pose = el.pose_at(u_ref)

		var posts := cabs_of(assets.get(u.appearance, {}) as Dictionary)
		u.cabs = posts["cabs"]
		if String(posts["reason"]) != "":
			notes.append("%s: вид %s: %s" % [uid, u.appearance, posts["reason"]])

		u.node = Node3D.new()
		u.node.name = "unit_" + uid
		u.node.transform = _stand(el, u_ref, u.bogie_base_m, u.reversed)
		parent.add_child(u.node)

		u.box = _box(u)
		u.node.add_child(u.box)
		out.append(u)
	return {"units": out, "skipped": skipped, "notes": notes}


## move — переставить УЖЕ НАРИСОВАННУЮ машину туда, где она теперь.
##
## # Почему двигаем, а не пересобираем
##
## Когда локомотив поехал (ClearAhead-fcy), снапшоты пошли каждый тик — десять
## раз в секунду. Сносить и собирать заново узел, коробку и двадцатимегабайтный
## меш десять раз в секунду нельзя: это не медленно, это неверно — меш пришлось
## бы каждый раз доставать заново.
##
## Двигается ровно то, что двигается: трансформ узла. Всё остальное — вид,
## коробка, посты — остаётся на месте, потому что от места машины не зависит.
##
## # Что здесь НЕ делается
##
## Не сглаживается и не досчитывается. Между снапшотами клиент показывает
## последнее присланное положение: объявленной кинематики показа
## (ClearAhead-t5h §6) в этой редакции нет, и досчитывать нечем. Десять снимков
## в секунду при инерции поезда — это ход, а не рывки; когда понадобится
## больше, придёт объявленный экстраполятор, а не самодеятельность рендера.
##
## Возвращает причину словами, если переставить нельзя (элемента нет в сети), и
## пустую строку, если всё встало.
static func move(u: Unit, at: Dictionary, elements: Dictionary) -> String:
	var el_id := String(at.get("element", ""))
	if not elements.has(el_id):
		return "%s: элемента %s нет в сети" % [u.id, el_id]
	var el := elements[el_id] as TrackGeom.Element
	var u_ref := float(at.get("u", 0.0))
	var reversed := String(at.get("direction", "")) == "reverse"
	if u.element == el and is_equal_approx(u.u_m, u_ref) and u.reversed == reversed:
		return ""
	u.element = el
	u.u_m = u_ref
	u.reversed = reversed
	u.pose = el.pose_at(u_ref)
	u.node.transform = _stand(el, u_ref, u.bogie_base_m, reversed)
	return ""


## cabs_of — посты машиниста записи каталога, переведённые в оси УЗЛА ЕДИНИЦЫ.
##
## Сервер объявляет их в осях ассета и ДО постановки — теми же осями, в которых
## лежат вершины меша. Значит и перевод обязан быть тем же самым: сначала
## масштаб, потом сдвиг, сдвиг НЕ домножается. Это не повтор ради удобства, а
## единственный способ, при котором смена масштаба увозит пост вместе с окнами
## кабины; своя формула разошлась бы с мешем молча, и разошлась бы ровно на ту
## величину, которую однажды уже поймал зонд (0.31 м вдоль хода).
##
## Записи нет вовсе — постов нет: вид, не объявленный в наборе, уедет в отказы
## отдельно (world.gd::_load_stock_meshes), и второй раз кричать об этом здесь
## незачем. А вот пост, записанный НЕ ТРЕМЯ ЧИСЛАМИ, — это испорченный набор, и
## он получает причину словами. Молча выбросить такой пост значило бы показать
## «в эту машину сесть некуда» там, где на самом деле сломан ответ сервера.
##
## Возвращает {cabs: Array[Vector3], reason: String}.
static func cabs_of(asset: Dictionary) -> Dictionary:
	var out: Array[Vector3] = []
	var scale := float(asset.get("scale", 1.0))
	var tr: Array = asset.get("translation", [0.0, 0.0, 0.0]) as Array
	var shift := Vector3(float(tr[0]), float(tr[1]), float(tr[2]))
	for i in (asset.get("cabs", []) as Array).size():
		var p := (asset.get("cabs", []) as Array)[i] as Array
		if p == null or p.size() != 3:
			return {"cabs": [] as Array[Vector3],
				"reason": "пост %d записан не тремя числами" % i}
		out.append(Vector3(float(p[0]), float(p[1]), float(p[2])) * scale + shift)
	return {"cabs": out, "reason": ""}


## show_mesh — ставит доехавший вид вместо коробки.
##
## Возвращает пустую строку при успехе и причину при отказе. Отказ здесь не
## фатален: коробка остаётся, и это по-прежнему верное положение верной машины —
## просто без её вида.
static func show_mesh(u: Unit, bytes: PackedByteArray, asset: Dictionary) -> String:
	var doc := GLTFDocument.new()
	var state := GLTFState.new()
	var err := doc.append_from_buffer(bytes, "", state)
	if err != OK:
		return "разбор glTF вернул %d" % err
	var scene := doc.generate_scene(state)
	if scene == null:
		return "glTF разобран, но сцена не собралась"

	var holder := Node3D.new()
	holder.name = "look"
	# ПОСТАНОВКА — из каталога, и порядок объявлен там же: сначала масштаб,
	# потом сдвиг. Обратный порядок дал бы другой результат, поэтому он назван,
	# а не подразумевается.
	var scale := float(asset.get("scale", 1.0))
	var tr: Array = asset.get("translation", [0.0, 0.0, 0.0]) as Array
	var shift := Vector3(float(tr[0]), float(tr[1]), float(tr[2]))
	# ОСИ АССЕТА — glTF: Y вверх, длинная ось Z. Оси мира — z вверх, и перевод
	# между ними работа КЛИЕНТА (спека каталога §10.1): это техника показа, а не
	# факт о станции. Godot держит Y вверх, поэтому перевод сводится к тому, что
	# узел единицы уже повёрнут по курсу — вид просто ложится в него.
	# СДВИГ БЕРЁТСЯ КАК ЕСТЬ, а не умножается на масштаб, и это куплено ошибкой:
	# первая редакция писала `shift * scale` и уводила машину на 0.31 м вдоль
	# хода (замер зондом tools/stock_probe.gd). Каталог объявляет сдвиг В МЕТРАХ
	# МИРА, то есть ПОСЛЕ масштаба, — так записано у content.Asset.Translation, и
	# второе домножение было чтением контракта наоборот.
	holder.transform = Transform3D(Basis.IDENTITY.scaled(Vector3(scale, scale, scale)), shift)
	holder.add_child(scene)
	u.node.add_child(holder)

	if u.box != null:
		u.box.queue_free()
		u.box = null
	u.mesh_shown = true
	return ""


## _stand — ПОСТАНОВКА ПО ДВУМ ШКВОРНЯМ, а не по касательной в одной точке.
##
## Разница видна только на кривой, и она куплена кадром владельца: боевая машина
## стоит серединой ровно на стыке дуги R=500 и прямой, и жёсткая постановка по
## касательной уводила хвост от оси на L²/2R = 0.29 м — колёса стояли РЯДОМ с
## рельсами. Настоящая машина опирается на путь двумя тележками и ложится
## ХОРДОЙ между ними; так теперь и ставится.
##
## Отход СЕРЕДИНЫ кузова при этом остаётся и равен провесу той же хорды (для
## базы 24.7 м на R=500 — 0.15 м). Это не недоделка, а предел одного жёсткого
## тела: у ВЛ80 две секции, и на кривой они поворачиваются друг относительно
## друга. Лечится это двумя единицами в сцепе, то есть В4, а не здесь.
##
## База НУЛЕВАЯ или не приехавшая — постановка вырождается в прежнюю, по
## касательной. Это законный случай: у короткой единицы база может быть меньше
## шага оси, и хорда тогда ничего не уточняет.
static func _stand(el: TrackGeom.Element, u_ref: float, base_m: float, reversed: bool) -> Transform3D:
	var half := base_m * 0.5
	var a := el.pose_at(u_ref + half)
	var b := el.pose_at(u_ref - half)
	var heading := el.pose_at(u_ref).heading
	var origin := TerrainMesh.to_godot(a.x, a.y, a.z).lerp(TerrainMesh.to_godot(b.x, b.y, b.z), 0.5)
	if half > 0.0 and (a.x != b.x or a.y != b.y):
		# Курс — по ХОРДЕ между шкворнями. Отметка тоже усредняется: на переломе
		# профиля тело опирается на два конца, а не висит по касательной.
		heading = atan2(a.y - b.y, a.x - b.x)
	else:
		origin = TerrainMesh.to_godot(el.pose_at(u_ref).x, el.pose_at(u_ref).y, el.pose_at(u_ref).z)
	return Transform3D(_basis_at(heading, reversed), origin)


## _basis_at — поворот единицы по курсу оси.
##
## Длинная ось ассета — +Z (соглашение glTF), курс задан в плане мира. Поворот
## вокруг вертикали на heading + 90° переводит одно в другое; направление
## reverse доворачивает на 180°, потому что машина повёрнута другим концом, а не
## едет назад.
static func _basis_at(heading: float, reversed: bool) -> Basis:
	var a := heading + PI * 0.5
	if reversed:
		a += PI
	return Basis(Vector3.UP, a)


## _box — габаритная коробка по присланным числам.
##
## Ставится ОТ ПОВЕРХНОСТИ КАТАНИЯ вверх: z точки отсчёта — головка рельса
## (контракт отрисовки редакции 6 §2), и высота отсчитывается оттуда же.
## Полупрозрачная, чтобы не выглядеть телом: сквозь неё виден путь, на котором
## машина стоит.
static func _box(u: Unit) -> Node3D:
	var mesh := BoxMesh.new()
	mesh.size = Vector3(u.width_m, u.height_m, u.length_m)
	var mat := StandardMaterial3D.new()
	mat.albedo_color = Color(BOX_COLOUR.r, BOX_COLOUR.g, BOX_COLOUR.b, BOX_ALPHA)
	mat.transparency = BaseMaterial3D.TRANSPARENCY_ALPHA
	mat.cull_mode = BaseMaterial3D.CULL_DISABLED
	mesh.material = mat
	var mi := MeshInstance3D.new()
	mi.name = "gabarit"
	mi.mesh = mesh
	mi.position = Vector3(0.0, u.height_m * 0.5, 0.0)
	return mi
