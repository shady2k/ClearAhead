## SwitchStand — ПЕРЕВОДНОЙ МЕХАНИЗМ СТРЕЛКИ В КАДРЕ: станина, указатель
## положения и табличка с номером.
##
## # Что здесь клиентское, а что нет
##
## МЕСТО И ВИД приезжают с сервера (turnout_drives): у какого прохода, на каком u,
## на сколько в сторону и какого механизма. ПОЛОЖЕНИЕ остряка приезжает живым
## состоянием (снапшот канала). Клиент не выводит ни того, ни другого.
##
## ГАБАРИТЫ ТЕЛА — здесь, и это объявленная граница владения, а не дыра
## контракта: пока привод рисуется, а не показывается ассетом, его размеры — то
## же решение художника, что длина крыла крестовины (контракт отрисовки §1,
## таблица владения). Все они названы константами списком ниже, чтобы выдумка
## была видна, а не растворялась в коде. В день, когда приедет каталог ассетов,
## список уезжает туда целиком.
##
## # ТАБЛИЧКА, А НЕ НАДПИСЬ
##
## Решение владельца 2026-08-15: у стрелки должна быть ТАБЛИЧКА — предмет в мире,
## стоящий на своих ногах и живущий по законам света и перспективы, — а не
## парящая подпись Label3D, которая одинаково велика с двух метров и с двухсот и
## читается сквозь рельеф. Вместе с этим решением из мира убраны ВСЕ Label3D
## (world.gd::_label снесён).
##
## Номер на щитке — ТЕКСТУРА, нарисованная разово в SubViewport. Другого способа
## получить произвольную строку картинкой в Godot нет: рисование текста требует
## канвы, а канва требует вьюпорта. Цена — один разовый проход рендера на
## стрелку; стрелок на станции единицы.
##
## # Почему примитивы движка, а не SurfaceTool
##
## Коробка, цилиндр и диск у движка уже есть, с правильными нормалями и обходом.
## Своя сборка нужна там, где форма СЧИТАЕТСЯ по присланным числам (шпалы, призма,
## рельс); здесь же форма постоянная, и повторять её вершинами значило бы завести
## своё место для ошибки обхода, за которое проект уже платил днём разбора
## («прозрачные шпалы»).
class_name SwitchStand
extends Node3D

## --- габариты тела: решения художника, названные списком --------------------

## Фундамент под станиной: бетонная подушка, из-за которой привод не висит в
## воздухе на неровной земле.
const PAD_L := 1.10
const PAD_W := 0.80
const PAD_H := 0.14

## РУЧНОЙ ПЕРЕВОД. Станина низкая и вытянутая вдоль пути; над ней балансир с
## противовесом — единственная часть, которая ходит при переводе, и потому её
## видно даже без указателя.
const MANUAL_BODY_L := 0.62
const MANUAL_BODY_W := 0.42
const MANUAL_BODY_H := 0.34
const LEVER_LEN := 1.05
const LEVER_R := 0.030
const WEIGHT_R := 0.16          # противовес — шар на конце рычага
const LEVER_PIVOT_H := 0.40     # высота оси качания над подошвой
const LEVER_UP_DEG := 32.0      # наклон рычага в одном положении…
const LEVER_DOWN_DEG := -32.0   # …и в другом

## ЭЛЕКТРОПРИВОД. Габарит взят с СП-6М (около 1.3 × 0.6 × 0.6 м) — это
## ОРИЕНТИР, а не норма: паспорта привода в проекте нет, и число названо
## оценкой, как того требует правило «оценка называется оценкой».
const ELECTRIC_BODY_L := 1.30
const ELECTRIC_BODY_W := 0.60
const ELECTRIC_BODY_H := 0.58
const ELECTRIC_LID_OVER := 0.04  # свес крышки за корпус
const ELECTRIC_LID_H := 0.06
const ROD_R := 0.035             # рабочая тяга к остряку
const ROD_H := 0.22              # на этой высоте она идёт к рельсам

## УКАЗАТЕЛЬ ПОЛОЖЕНИЯ. Два щитка на одной оси, скрещённых под прямым углом:
## круглый — плюсовое положение, прямоугольный — отклонённое. Переводится вся
## голова целиком на 90°, поэтому к подходящему поезду всегда обращён лицом ровно
## тот щиток, который называет нынешнее положение. Это устройство настоящего
## стрелочного указателя, а не выдумка ради читаемости.
const MAST_R := 0.045
const MAST_H := 1.55
const DISC_R := 0.26
const DISC_T := 0.035
const VANE_L := 0.52
const VANE_H := 0.38
const VANE_T := 0.035

## ТАБЛИЧКА. Щиток с номером стрелки на своём столбике, лицом ПОПЕРЁК пути:
## её читают, идя вдоль пути или подъезжая, а не сверху.
const PLATE_W := 0.56
const PLATE_H := 0.30
const PLATE_T := 0.035
const PLATE_TOP := 1.15        # верх щитка над подошвой
const POST_R := 0.035
const PLATE_MARGIN := 0.02     # насколько текстура утоплена от кромки щитка

## Разрешение текстуры номера. 512×256 — щиток 0.56 м шириной, то есть около
## 900 точек на метр: номер читается вблизи и не мылится.
const PLATE_PX := Vector2i(512, 256)

## --- палитра ----------------------------------------------------------------
## Цвета — тоже решения художника. Записаны в sRGB (в нём их читает человек);
## перевод в линейное делает материал.
const C_PAD := Color(0.66, 0.65, 0.63)        # бетон
const C_MANUAL := Color(0.28, 0.30, 0.33)     # чугунная станина
const C_LEVER := Color(0.42, 0.44, 0.47)
const C_WEIGHT := Color(0.72, 0.16, 0.14)     # противовес красят, чтобы был виден
const C_ELECTRIC := Color(0.45, 0.47, 0.50)   # крашеный стальной корпус
const C_LID := Color(0.34, 0.36, 0.39)
const C_ROD := Color(0.55, 0.56, 0.58)
const C_MAST := Color(0.30, 0.31, 0.33)
const C_DISC := Color(0.94, 0.94, 0.92)       # белый круг: по прямому пути
const C_VANE := Color(0.12, 0.12, 0.13)       # чёрный щиток: на боковой
const C_VANE_BAND := Color(0.94, 0.94, 0.92)  # светлая полоса поперёк него
const C_PLATE := Color(0.93, 0.93, 0.90)      # щиток таблички
const C_PLATE_TEXT := Color(0.09, 0.09, 0.10)
const C_POST := Color(0.32, 0.33, 0.35)

## Положения остряка. Строки те же, что на проводе: третьего написания одного и
## того же клиент не заводит.
const POS_STRAIGHT := "straight"
const POS_DIVERGING := "diverging"

## owner — идентификатор стрелки, которой принадлежит привод. По нему приходит
## положение из снапшота и по нему же его переводит команда.
var owner_id := ""
## label — метка стрелки: то, что написано на табличке и что показывает пульт.
var label := ""
## drive — вид механизма: "manual" | "electric".
var drive := ""
## Положение, которое показано СЕЙЧАС. Пустая строка значит «ещё не показывали»:
## первый снапшот обязан повернуть указатель, даже если стрелка стоит прямо.
var shown := ""

var _head: Node3D = null    # голова указателя: поворачивается при переводе
var _lever: Node3D = null   # балансир ручного привода; у электрического null
## Сторона от оси пути: +1 слева, −1 справа. Ставится по знаку присланного
## выноса; тело строится в осях привода, и метры выноса в нём уже ни к чему.
var _side := 1.0


## build — собрать привод по присланному описанию.
##
## Поза уже смещена сервером в сторону (TrackBuild.drives), поэтому здесь только
## перевод в оси движка и поворот вдоль пути. Разворот лицом к пути делается по
## ЗНАКУ ВЫНОСА: привод, стоящий слева, смотрит вправо, и наоборот — иначе
## указатель и табличка оказались бы обращены в поле.
static func build(d: TrackBuild.TurnoutDrive, base_drop_m: float) -> SwitchStand:
	var s := SwitchStand.new()
	s.owner_id = d.owner
	s.label = d.label
	s.drive = d.drive
	s._side = 1.0 if d.offset_m >= 0.0 else -1.0
	s.name = "Drive_%s" % d.owner
	var p := d.pose
	# ПОДОШВА ПРИВОДА — НЕ НА ГОЛОВКЕ РЕЛЬСА. Датум z — верх головки (контракт
	# отрисовки §2), а станина стоит на переводном брусе, то есть на высоту
	# рельса ниже. Число ПРИСЛАНО типом устройства (разбор — у
	# TrackBuild.TurnoutDrive.base_drop_m); ноль означает «типа нет», и тогда
	# привод честно встаёт на отметку оси — заметно и объяснимо.
	s.position = TerrainMesh.to_godot(p.x, p.y, p.z - base_drop_m)
	# Ось −Z узла идёт вдоль пути по возрастанию u: то же правило, по которому
	# ставится человек (Driver.yaw_for), и второй копии его знака не заводится.
	var fwd := p.forward()
	s.rotation.y = atan2(-fwd.x, fwd.y)
	s._assemble()
	return s


## _assemble — тело привода. Локальные оси: −Z вдоль пути, +X вправо от хода,
## +Y вверх; ноль — на ВЕРХЕ ПЕРЕВОДНОГО БРУСА (см. build).
##
## Плита фундамента отсюда идёт ВНИЗ: она изображает опорную раму, которой
## станина стоит на брусьях, и заканчивается ровно на нуле. Земли под ней нет и
## не спрашивается: отметки грунта в этой точке у клиента не существует.
func _assemble() -> void:
	# Сторона, в которую смотрит лицо привода: он стоит сбоку от пути и обращён
	# к нему.
	var facing := -1.0 if position_side() > 0.0 else 1.0

	_add_box(Vector3(0, PAD_H * 0.5 - PAD_H, 0), Vector3(PAD_W, PAD_H, PAD_L), C_PAD, "Pad")
	if drive == "electric":
		_assemble_electric(facing)
	else:
		_assemble_manual()
	_assemble_indicator()
	_assemble_plate()
	# ПОЛОЖЕНИЕ ЗДЕСЬ НЕ СТАВИТСЯ. Указатель, повёрнутый «пока прямо», был бы
	# фактом о мире, которого сервер не присылал; shown остаётся пустым, и первый
	# же снапшот поворачивает голову, даже если стрелка и вправду стоит прямо.


## position_side — с какой стороны от оси пути стоит привод: +1 слева, −1
## справа. Знак выноса задаёт сервер, клиент его только читает.
func position_side() -> float:
	return _side


func _assemble_manual() -> void:
	_add_box(Vector3(0, MANUAL_BODY_H * 0.5, 0),
		Vector3(MANUAL_BODY_W, MANUAL_BODY_H, MANUAL_BODY_L), C_MANUAL, "Body")
	# БАЛАНСИР. Узел качания стоит на оси станины; рычаг уходит от него назад
	# вдоль пути, противовес — на конце. Ходит он целиком, и это единственная
	# часть ручного привода, по которой положение видно без указателя.
	_lever = Node3D.new()
	_lever.name = "Lever"
	_lever.position = Vector3(0, LEVER_PIVOT_H, 0)
	add_child(_lever)
	var arm := _cylinder(LEVER_R, LEVER_LEN, C_LEVER, "Arm")
	# Цилиндр движка стоит вдоль +Y; кладём его вдоль −Z и сдвигаем на полдлины,
	# чтобы качался конец, а не середина.
	arm.rotation.x = PI / 2
	arm.position = Vector3(0, 0, LEVER_LEN * 0.5)
	_lever.add_child(arm)
	var weight := _sphere(WEIGHT_R, C_WEIGHT, "Weight")
	weight.position = Vector3(0, 0, LEVER_LEN)
	_lever.add_child(weight)


func _assemble_electric(facing: float) -> void:
	_add_box(Vector3(0, ELECTRIC_BODY_H * 0.5, 0),
		Vector3(ELECTRIC_BODY_W, ELECTRIC_BODY_H, ELECTRIC_BODY_L), C_ELECTRIC, "Body")
	_add_box(Vector3(0, ELECTRIC_BODY_H + ELECTRIC_LID_H * 0.5, 0),
		Vector3(ELECTRIC_BODY_W + ELECTRIC_LID_OVER * 2, ELECTRIC_LID_H,
			ELECTRIC_BODY_L + ELECTRIC_LID_OVER * 2), C_LID, "Lid")
	# РАБОЧАЯ ТЯГА к остряку: уходит от корпуса В СТОРОНУ ПУТИ. Длина её здесь
	# СВОЯ, а не присланная, и это видно: до остряка от станины столько, сколько
	# составляет вынос, но вынос — расстояние до ОСИ, и класть тягу до оси значило
	# бы просунуть её под оба рельса.
	var rod := _cylinder(ROD_R, ELECTRIC_BODY_W, C_ROD, "Rod")
	rod.rotation.z = PI / 2
	rod.position = Vector3(facing * ELECTRIC_BODY_W, ROD_H, 0)
	add_child(rod)


## _assemble_indicator — голова указателя: мачта и два скрещённых щитка.
func _assemble_indicator() -> void:
	var mast := _cylinder(MAST_R, MAST_H, C_MAST, "Mast")
	mast.position = Vector3(0, MAST_H * 0.5, 0)
	add_child(mast)
	_head = Node3D.new()
	_head.name = "Head"
	_head.position = Vector3(0, MAST_H, 0)
	add_child(_head)
	# КРУГ — плюсовое положение. Плоскостью поперёк пути, то есть лицом к тому,
	# кто по этому пути едет.
	var disc := _cylinder(DISC_R, DISC_T, C_DISC, "Disc")
	disc.rotation.x = PI / 2
	_head.add_child(disc)
	# ЩИТОК — отклонённое. Скрещён с кругом под прямым углом, поэтому лицом к
	# поезду он оказывается ровно тогда, когда голова повёрнута на 90°.
	var vane := _box_mesh(Vector3(VANE_T, VANE_H, VANE_L), C_VANE, "Vane")
	_head.add_child(vane)
	var band := _box_mesh(Vector3(VANE_T * 1.2, VANE_H * 0.22, VANE_L), C_VANE_BAND, "VaneBand")
	_head.add_child(band)


## _assemble_plate — ТАБЛИЧКА: столбик, щиток и номер текстурой на обеих сторонах.
func _assemble_plate() -> void:
	# Табличка стоит рядом со станиной, а не на ней: у настоящей свой столбик, и
	# на корпусе привода ей мешала бы крышка.
	var at_z := (ELECTRIC_BODY_L if drive == "electric" else MANUAL_BODY_L) * 0.5 + 0.45
	var post := _cylinder(POST_R, PLATE_TOP - PLATE_H, C_POST, "PlatePost")
	post.position = Vector3(0, (PLATE_TOP - PLATE_H) * 0.5, at_z)
	add_child(post)
	var board := Node3D.new()
	board.name = "Plate"
	board.position = Vector3(0, PLATE_TOP - PLATE_H * 0.5, at_z)
	# Щиток разворачивается ПОПЕРЁК пути: лицом к тому, кто идёт вдоль него.
	board.rotation.y = PI / 2
	add_child(board)
	board.add_child(_box_mesh(Vector3(PLATE_W, PLATE_H, PLATE_T), C_PLATE, "Board"))
	var tex := _plate_texture(label)
	if tex == null:
		return
	# Номер — на ОБЕИХ сторонах: щиток читают с обеих, и пустая изнанка выглядела
	# бы как другая табличка.
	for s in [1.0, -1.0]:
		var face := MeshInstance3D.new()
		face.name = "Face%s" % ("A" if s > 0 else "B")
		var quad := QuadMesh.new()
		quad.size = Vector2(PLATE_W - PLATE_MARGIN * 2, PLATE_H - PLATE_MARGIN * 2)
		face.mesh = quad
		var mat := StandardMaterial3D.new()
		mat.albedo_texture = tex
		mat.shading_mode = BaseMaterial3D.SHADING_MODE_PER_PIXEL
		mat.roughness = 0.85
		# Щиток непрозрачный, номер на нём непрозрачен тоже: текстура рисуется
		# белым фоном, а не альфой, — так у неё нет краёв, которые движок стал бы
		# сортировать.
		face.material_override = mat
		face.position = Vector3(0, 0, s * (PLATE_T * 0.5 + 0.002))
		if s < 0:
			face.rotation.y = PI
		board.add_child(face)


## show_position — повернуть указатель и балансир в присланное положение.
##
## Единственная точка, где живое состояние превращается в поворот. Возвращает
## true, если что-то и вправду повернулось: мир по этому решает, стоит ли
## пересчитывать зависящее от показа.
func show_position(pos: String) -> bool:
	if pos != POS_STRAIGHT and pos != POS_DIVERGING:
		# Неизвестное положение НЕ ПОКАЗЫВАЕТСЯ вовсе: указатель, повёрнутый «на
		# всякий случай прямо», врал бы про стрелку, о которой мы ничего не знаем.
		return false
	if pos == shown:
		return false
	shown = pos
	if _head != null:
		_head.rotation.y = 0.0 if pos == POS_STRAIGHT else PI / 2
	if _lever != null:
		var deg := LEVER_UP_DEG if pos == POS_STRAIGHT else LEVER_DOWN_DEG
		_lever.rotation.x = deg_to_rad(deg)
	return true


## plan_point — где привод стоит в осях движка. Спрашивают подсказка подошедшему
## и пульт: расстояние до стрелки меряется до её привода, а не до крестовины.
func plan_point() -> Vector3:
	return global_position


## --- сборка примитивов ------------------------------------------------------

func _add_box(at: Vector3, size: Vector3, col: Color, name_v: String) -> void:
	var mi := _box_mesh(size, col, name_v)
	mi.position = at
	add_child(mi)


func _box_mesh(size: Vector3, col: Color, name_v: String) -> MeshInstance3D:
	var mi := MeshInstance3D.new()
	mi.name = name_v
	var box := BoxMesh.new()
	box.size = size
	mi.mesh = box
	mi.material_override = _material(col)
	return mi


func _cylinder(r: float, h: float, col: Color, name_v: String) -> MeshInstance3D:
	var mi := MeshInstance3D.new()
	mi.name = name_v
	var cyl := CylinderMesh.new()
	cyl.top_radius = r
	cyl.bottom_radius = r
	cyl.height = h
	cyl.radial_segments = 12
	cyl.rings = 1
	mi.mesh = cyl
	mi.material_override = _material(col)
	return mi


func _sphere(r: float, col: Color, name_v: String) -> MeshInstance3D:
	var mi := MeshInstance3D.new()
	mi.name = name_v
	var sph := SphereMesh.new()
	sph.radius = r
	sph.height = r * 2
	sph.radial_segments = 12
	sph.rings = 6
	mi.mesh = sph
	mi.material_override = _material(col)
	return mi


func _material(col: Color) -> StandardMaterial3D:
	var m := StandardMaterial3D.new()
	m.albedo_color = col
	m.roughness = 0.78
	m.metallic = 0.0
	return m


## _plate_texture — номер стрелки картинкой.
##
## SubViewport — единственный способ получить произвольную строку текстурой:
## рисование текста в Godot требует канвы, а канва — вьюпорта. Обновление ставим
## ОДНОРАЗОВОЕ: номер не меняется, а вьюпорт, обновляющийся каждый кадр, — это
## лишний проход рендера на каждую стрелку навсегда.
##
## Пустая метка таблички не даёт: щиток без номера — это щиток, а выдуманный
## номер был бы фактом о станции, которого сервер не присылал.
func _plate_texture(text: String) -> Texture2D:
	if text.strip_edges() == "":
		return null
	var vp := SubViewport.new()
	vp.name = "PlateText"
	vp.size = PLATE_PX
	vp.transparent_bg = false
	vp.disable_3d = true
	vp.render_target_update_mode = SubViewport.UPDATE_ONCE
	var bg := ColorRect.new()
	bg.color = C_PLATE
	bg.set_anchors_preset(Control.PRESET_FULL_RECT)
	vp.add_child(bg)
	var lbl := Label.new()
	lbl.text = text
	lbl.set_anchors_preset(Control.PRESET_FULL_RECT)
	lbl.horizontal_alignment = HORIZONTAL_ALIGNMENT_CENTER
	lbl.vertical_alignment = VERTICAL_ALIGNMENT_CENTER
	lbl.add_theme_color_override("font_color", C_PLATE_TEXT)
	# Кегль от высоты щитка, а не числом: длинная метка иначе вылезла бы за
	# кромку, а короткая потерялась бы посередине.
	lbl.add_theme_font_size_override("font_size", int(PLATE_PX.y * 0.62))
	lbl.autowrap_mode = TextServer.AUTOWRAP_OFF
	vp.add_child(lbl)
	add_child(vp)
	return vp.get_texture()
