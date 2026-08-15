## ModelBuild — СБОРЩИК ТЕЛА ПО ПРИСЛАННОМУ ОПИСАНИЮ.
##
## Читает описание модели (content/model.go, media type
## application/vnd.clearahead.model+json) и складывает из него узлы Godot. Своих
## чисел у него нет НИ ОДНОГО: ни размера, ни цвета, ни угла. Всё приезжает
## файлом ассета — теми же байтами по тому же адресу-хешу, что и glb локомотива.
##
## # Что здесь клиентское, а что нет
##
## КЛИЕНТСКОЕ — способ построить меш: какой примитив движка взять под ящик, как
## нарисовать многоугольник знака, сколько граней у цилиндра... нет, граней тоже
## не клиентское, их называет описание. Клиентское ровно одно: ЧЕМ рисовать.
##
## НЕ КЛИЕНТСКОЕ — что рисовать. Слово владельца 2026-08-15: «наш сервер должен
## поддерживать любой рендер: godot, unity и что угодно ещё; соответственно данных
## сервера должно хватать для полноценной отрисовки на клиенте».
##
## # Оси
##
## Описание объявляет соглашение само (поле axes), и клиент ОТКАЗЫВАЕТ на
## незнакомом, а не подставляет своё. Единственное, что он умеет сегодня, —
## x_right_y_up_z_back: правая тройка, та же, что у glTF и у самого Godot,
## поэтому перевода координат здесь нет вовсе. Он появится в тот день, когда
## появится второе соглашение, и появится ОДНИМ местом.
##
## # Подвижность
##
## Часть с pivot поворачивается по СОСТОЯНИЮ устройства: «положение остряка
## straight — повернуть на 90°». Состояние приходит из мира (снапшот канала), углы
## — из описания. Клиент не знает ни того, что означает straight, ни на сколько
## поворачивать: он сопоставляет строку строке.
##
## Плавность перехода между состояниями — ЗОНА РЕНДЕРА и живёт здесь (сегодня её
## нет: положение встаёт сразу, как у настоящего указателя, который щёлкает).
class_name ModelBuild
extends RefCounted

## Соглашение об осях, которое клиент умеет читать. Совпадает с осями Godot,
## поэтому перевода нет.
const AXES_GLTF := "x_right_y_up_z_back"
const UNITS_M := "m"
const ANGLES_DEG := "deg"
const FORMAT_VERSION := 1

## Формы. Имена — договор с сервером (content.Shape*), второго написания клиент
## не заводит.
const SHAPE_BOX := "box"
const SHAPE_CYLINDER := "cylinder"
const SHAPE_FRUSTUM := "frustum"
const SHAPE_PLATE := "plate"

## Разрешение картинки, которой наносится знак или надпись на щиток.
##
## ЕДИНСТВЕННОЕ ЧИСЛО ЭТОГО ФАЙЛА, и оно не о мире, а о том, ЧЕМ рисовать:
## многоугольник и текст описания — векторные, а Godot кладёт их на меш
## текстурой. Тысяча точек на метр щитка — кромка не мылится вблизи.
const MARK_PX_PER_M := 1000.0
const MARK_PX_MAX := 512


## Built — собранное тело и то, чем им управлять.
class Built extends RefCounted:
	## root — корень тела. Пустой узел, если описание не собралось.
	var root: Node3D = null
	## pivots — подвижные части: имя состояния -> список {node, states}.
	## Держатся списком, потому что по одному состоянию поворачиваются несколько
	## частей (у указателя — щиток, у механизма — балансир).
	var pivots := {}
	## reason — почему не собралось. Пусто — собралось.
	var reason := ""

	func failed() -> bool:
		return reason != ""

	## apply_state — показать состояние устройства: повернуть все части, которые
	## по нему подвижны.
	##
	## Состояние, которого модель не знает, часть НЕ ПОВОРАЧИВАЕТ: описание
	## называет углы для тех значений, которые предусмотрел автор, и подставлять
	## ноль вместо неназванного значило бы показывать положение, которого нет.
	## Возвращает число повёрнутых частей — по нему мир видит, дошло ли состояние.
	func apply_state(key: String, value: String) -> int:
		if not pivots.has(key):
			return 0
		var turned := 0
		for p in (pivots[key] as Array):
			var states: Dictionary = p["states"]
			if not states.has(value):
				continue
			var node: Node3D = p["node"]
			var deg := float(states[value])
			match String(p["axis"]):
				"x":
					node.rotation.x = deg_to_rad(deg)
				"y":
					node.rotation.y = deg_to_rad(deg)
				_:
					node.rotation.z = deg_to_rad(deg)
			turned += 1
		return turned


## build — собрать тело по описанию.
##
## labels — тексты для надписей: имя состояния -> строка. Текст в модели не
## лежит (номер стрелки — факт о станции), и щиток без текста остаётся чистым
## щитком, а не получает выдуманный номер.
static func build(doc: Dictionary, labels: Dictionary) -> Built:
	var out := Built.new()
	if int(doc.get("format_version", 0)) != FORMAT_VERSION:
		out.reason = "версия формата модели %s, клиент читает %d" % [
			str(doc.get("format_version", null)), FORMAT_VERSION]
		return out
	if String(doc.get("axes", "")) != AXES_GLTF:
		out.reason = "соглашение об осях %s неизвестно клиенту (умеет %s)" % [
			String(doc.get("axes", "")), AXES_GLTF]
		return out
	if String(doc.get("units", "")) != UNITS_M or String(doc.get("angles", "")) != ANGLES_DEG:
		out.reason = "единицы %s и углы %s не те, что клиент умеет (%s, %s)" % [
			String(doc.get("units", "")), String(doc.get("angles", "")), UNITS_M, ANGLES_DEG]
		return out
	var mats := _materials(doc.get("materials", {}) as Dictionary)
	out.root = Node3D.new()
	out.root.name = String(doc.get("model", "model"))
	for raw in (doc.get("parts", []) as Array):
		var node := _part(raw as Dictionary, mats, labels, out)
		if node != null:
			out.root.add_child(node)
	return out


## _materials — палитра описания в материалы движка.
##
## Цвет описан в sRGB — в нём его читает человек, — а движок ждёт линейный:
## перевод делает Color.srgb_to_linear... его делает сам StandardMaterial3D для
## albedo_color, поэтому здесь цвет кладётся как есть.
static func _materials(raw: Dictionary) -> Dictionary:
	var out := {}
	for id in raw:
		var m := raw[id] as Dictionary
		var mat := StandardMaterial3D.new()
		mat.albedo_color = Color.from_string(String(m.get("colour", "#ff00ff")), Color.MAGENTA)
		mat.roughness = float(m.get("roughness", 0.8))
		mat.metallic = float(m.get("metallic", 0.0))
		out[String(id)] = mat
	return out


## _part — часть описания в узел сцены. Рекурсивно, вместе с детьми.
static func _part(p: Dictionary, mats: Dictionary, labels: Dictionary, out: Built) -> Node3D:
	var node := Node3D.new()
	node.name = String(p.get("name", "part"))
	var at := _vec3(p.get("at", []))
	node.position = at
	var rot := _vec3(p.get("rotate", []))
	# Постоянный поворот. Описание разрешает только одну ненулевую ось (сервер
	# это и проверяет), поэтому порядок Эйлера здесь ни на что не влияет.
	node.rotation = Vector3(deg_to_rad(rot.x), deg_to_rad(rot.y), deg_to_rad(rot.z))

	var shape := String(p.get("shape", ""))
	var mat: StandardMaterial3D = mats.get(String(p.get("material", "")), null)
	match shape:
		"":
			pass
		SHAPE_BOX:
			node.add_child(_box(_vec3(p.get("size", [])), mat))
		SHAPE_CYLINDER:
			node.add_child(_cylinder(float(p.get("radius", 0.0)), float(p.get("radius", 0.0)),
				float(p.get("height", 0.0)), int(p.get("sides", 12)), String(p.get("axis", "y")), mat))
		SHAPE_FRUSTUM:
			# Радиус у многогранника движка — половина ДИАГОНАЛИ, а описание даёт
			# СТОРОНУ: у четырёхгранной станины это разные числа.
			var k: float = 1.0 if int(p.get("sides", 12)) > 8 else sqrt(2.0)
			node.add_child(_cylinder(float(p.get("top", 0.0)) * k / 2.0,
				float(p.get("bottom", 0.0)) * k / 2.0, float(p.get("height", 0.0)),
				int(p.get("sides", 12)), String(p.get("axis", "y")), mat))
		SHAPE_PLATE:
			_plate(node, p, mats, labels, mat)
		_:
			# Неизвестная форма НЕ ПРОПУСКАЕТСЯ МОЛЧА: предмет, у которого не
			# нарисовалась половина, выглядит исправным.
			out.reason = "форма %s клиенту неизвестна" % shape

	var pivot: Variant = p.get("pivot", null)
	if pivot is Dictionary:
		var pv := pivot as Dictionary
		var key := String(pv.get("by", ""))
		if not out.pivots.has(key):
			out.pivots[key] = []
		(out.pivots[key] as Array).append({
			"node": node,
			"axis": String(pv.get("axis", "y")),
			"states": pv.get("states", {}) as Dictionary,
		})
	for child in (p.get("parts", []) as Array):
		var c := _part(child as Dictionary, mats, labels, out)
		if c != null:
			node.add_child(c)
	return node


static func _vec3(raw: Variant) -> Vector3:
	var a := raw as Array
	if a == null or a.size() < 3:
		return Vector3.ZERO
	return Vector3(float(a[0]), float(a[1]), float(a[2]))


static func _box(size: Vector3, mat: StandardMaterial3D) -> MeshInstance3D:
	var mi := MeshInstance3D.new()
	mi.name = "Mesh"
	var box := BoxMesh.new()
	box.size = size
	mi.mesh = box
	mi.material_override = mat
	return mi


static func _cylinder(top: float, bottom: float, height: float, sides: int,
		axis: String, mat: StandardMaterial3D) -> MeshInstance3D:
	var mi := MeshInstance3D.new()
	mi.name = "Mesh"
	var cyl := CylinderMesh.new()
	cyl.top_radius = top
	cyl.bottom_radius = bottom
	cyl.height = height
	cyl.radial_segments = maxi(sides, 3)
	cyl.rings = 1
	mi.mesh = cyl
	# Цилиндр движка стоит вдоль Y; вдоль X и Z его кладёт поворот.
	match axis:
		"x":
			mi.rotation.z = PI / 2
		"z":
			mi.rotation.x = PI / 2
	mi.material_override = mat
	return mi


## _plate — ЩИТОК: тонкая пластина, на которую нанесены знак и надпись.
##
## Лицо смотрит вдоль +Z описания. Знак и надпись кладутся картинкой на обе
## стороны, если описание этого просит: указатель читают с обоих концов стрелки,
## и пустая изнанка выглядела бы как другой указатель.
static func _plate(node: Node3D, p: Dictionary, mats: Dictionary,
		labels: Dictionary, mat: StandardMaterial3D) -> void:
	var size := p.get("size", []) as Array
	if size == null or size.size() < 2:
		return
	var w := float(size[0])
	var h := float(size[1])
	var t := float(p.get("thickness", 0.01))
	node.add_child(_box(Vector3(w, h, t), mat))
	var tex := _face_texture(node, p, mats, labels, w, h)
	if tex == null:
		return
	var both := true
	var mark: Variant = p.get("mark", null)
	var label: Variant = p.get("label", null)
	if mark is Dictionary:
		both = bool((mark as Dictionary).get("both_sides", true))
	elif label is Dictionary:
		both = bool((label as Dictionary).get("both_sides", true))
	var sides: Array = [1.0, -1.0] if both else [1.0]
	for s in sides:
		var face := MeshInstance3D.new()
		face.name = "Face%s" % ("A" if float(s) > 0 else "B")
		var quad := QuadMesh.new()
		quad.size = Vector2(w, h)
		face.mesh = quad
		var fm := StandardMaterial3D.new()
		fm.albedo_texture = tex
		fm.shading_mode = BaseMaterial3D.SHADING_MODE_PER_PIXEL
		fm.roughness = mat.roughness if mat != null else 0.85
		face.material_override = fm
		face.position = Vector3(0, 0, float(s) * (t * 0.5 + 0.002))
		if float(s) < 0:
			face.rotation.y = PI
		node.add_child(face)


## _face_texture — лицо щитка картинкой: поле, знак и надпись.
##
## SubViewport — единственный способ получить в Godot произвольный рисунок
## текстурой: и многоугольник, и текст требуют канвы, а канва — вьюпорта.
## Обновление ОДНОРАЗОВОЕ: ни знак, ни номер не меняются.
static func _face_texture(owner_node: Node3D, p: Dictionary, mats: Dictionary,
		labels: Dictionary, w: float, h: float) -> Texture2D:
	var mark: Variant = p.get("mark", null)
	var label: Variant = p.get("label", null)
	var text := ""
	if label is Dictionary:
		text = String(labels.get(String((label as Dictionary).get("by", "")), ""))
	if not (mark is Dictionary) and text == "":
		return null
	var px := Vector2i(
		clampi(int(w * MARK_PX_PER_M), 16, MARK_PX_MAX),
		clampi(int(h * MARK_PX_PER_M), 16, MARK_PX_MAX))
	var vp := SubViewport.new()
	vp.name = "Face"
	vp.size = px
	vp.transparent_bg = false
	vp.disable_3d = true
	vp.render_target_update_mode = SubViewport.UPDATE_ONCE
	var bg := ColorRect.new()
	var field: StandardMaterial3D = mats.get(String(p.get("material", "")), null)
	bg.color = field.albedo_color if field != null else Color.WHITE
	bg.set_anchors_preset(Control.PRESET_FULL_RECT)
	vp.add_child(bg)
	if mark is Dictionary:
		var mk := mark as Dictionary
		var poly := PackedVector2Array()
		for pt_raw in (mk.get("polygon", []) as Array):
			var pt := pt_raw as Array
			# Начало координат знака — ЛЕВЫЙ НИЖНИЙ угол лица; у канвы Godot оно в
			# левом верхнем, отсюда переворот по вертикали.
			poly.append(Vector2(float(pt[0]) * px.x, (1.0 - float(pt[1])) * px.y))
		var node := Polygon2D.new()
		node.polygon = poly
		var mm: StandardMaterial3D = mats.get(String(mk.get("material", "")), null)
		node.color = mm.albedo_color if mm != null else Color.BLACK
		vp.add_child(node)
	if text != "":
		var lb := label as Dictionary
		var lbl := Label.new()
		lbl.text = text
		lbl.set_anchors_preset(Control.PRESET_FULL_RECT)
		lbl.horizontal_alignment = HORIZONTAL_ALIGNMENT_CENTER
		lbl.vertical_alignment = VERTICAL_ALIGNMENT_CENTER
		var lm: StandardMaterial3D = mats.get(String(lb.get("material", "")), null)
		lbl.add_theme_color_override("font_color", lm.albedo_color if lm != null else Color.BLACK)
		lbl.add_theme_font_size_override("font_size", int(px.y * float(lb.get("height", 0.6))))
		lbl.autowrap_mode = TextServer.AUTOWRAP_OFF
		vp.add_child(lbl)
	# ВЬЮПОРТ ОБЯЗАН БЫТЬ В ДЕРЕВЕ, иначе он не рисует вовсе, и текстура выходит
	# пустой. Живёт он под самим щитком: у щитка та же жизнь, что у рисунка на нём.
	owner_node.add_child(vp)
	return vp.get_texture()
