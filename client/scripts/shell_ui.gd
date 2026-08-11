extends CanvasLayer
## Экраны оболочки: главное меню, выбор роли, пауза поверх мира.
##
## Строится кодом, а не сценой, по той же причине, что и ui.gd: раскладка здесь
## — один столбец кнопок, и держать её в .tscn значит править её в двух местах.
##
## ЗАГЛУШКИ ОБЪЯВЛЕНЫ ЗАГЛУШКАМИ. «Загрузить» и «Сохранить» — пункты меню без
## провода: сервер операции уже умеет (GET /maps, POST /maps/load, /maps/save),
## а сохранять пока нечего — состояния игры не существует, оно появится вместе с
## движением (В2-В3). Кнопка нажимается и ЧЕСТНО ГОВОРИТ, чего ещё нет: серая
## неактивная кнопка выглядит поломкой и не объясняет себя.

signal new_game_requested
## Выбрана карта. Несёт ИМЯ ФАЙЛА из каталога сервера, а не map_id: адресует
## карту в POST /maps/load/{name} именно оно.
signal map_chosen(name: String)
signal role_chosen(role: int)
signal resume_requested
## «Назад» с экрана ролей. Отдельный сигнал, а не прямой вызов show_menu():
## экраном владеет этот узел, а СОСТОЯНИЕМ — оболочка, и кнопка, переключающая
## картинку молча, оставила бы их расходиться (Esc после «Назад» пришлось бы
## жать дважды).
signal back_requested
signal to_menu_requested
signal quit_requested

const TITLE_SIZE := 40
const ITEM_SIZE := 20
const NOTE_SIZE := 14
const BUTTON_MIN_WIDTH := 260

## Экраны взаимоисключающи: виден ровно один или ни одного (когда игрок в мире).
enum Screen { NONE, MENU, MAP, ROLE, PAUSE }

## Роли объявлены здесь, а не в app.gd: сигнал role_chosen несёт именно их, и
## подписчику нужны те же числа. Порядок — как в замысле: машинист, ДСП, строитель.
enum Role { DRIVER, DSP, BUILDER }

const ROLE_NAMES := {
	Role.DRIVER: "Машинист",
	Role.DSP: "ДСП",
	Role.BUILDER: "Строитель",
}

## Одна строка про то, чего в роли пока нет. Показывается на выборе, чтобы
## игрок не искал управление, которого ещё не написано.
const ROLE_NOTES := {
	Role.DRIVER: "человек в мире, вид с высоты его глаз · ходьба есть, поезда пока не водятся",
	Role.DSP: "мир сверху и переключатель в схему · щёлкать стрелки пока нечем (В2)",
	Role.BUILDER: "мир сверху под углом · инструментов правки пока нет",
}

var _root: Control
var _panel: VBoxContainer
var _title: Label
var _note: Label
var _screen := Screen.NONE

func _ready() -> void:
	layer = 10
	_root = Control.new()
	_root.set_anchors_preset(Control.PRESET_FULL_RECT)
	_root.mouse_filter = Control.MOUSE_FILTER_STOP  # меню перехватывает мышь у мира
	add_child(_root)

	# Затемнение под меню: без него белые буквы теряются на светлом небе, а в
	# паузе непонятно, идёт игра или стоит.
	var dim := ColorRect.new()
	dim.set_anchors_preset(Control.PRESET_FULL_RECT)
	dim.color = Color(0.05, 0.07, 0.09, 0.82)
	_root.add_child(dim)

	var center := CenterContainer.new()
	center.set_anchors_preset(Control.PRESET_FULL_RECT)
	_root.add_child(center)

	_panel = VBoxContainer.new()
	_panel.add_theme_constant_override("separation", 10)
	center.add_child(_panel)

	_title = Label.new()
	_title.horizontal_alignment = HORIZONTAL_ALIGNMENT_CENTER
	_title.add_theme_font_size_override("font_size", TITLE_SIZE)
	_panel.add_child(_title)

	_note = Label.new()
	_note.horizontal_alignment = HORIZONTAL_ALIGNMENT_CENTER
	_note.add_theme_font_size_override("font_size", NOTE_SIZE)
	_note.add_theme_color_override("font_color", Color(0.75, 0.78, 0.82))
	_note.custom_minimum_size = Vector2(520, 0)
	_note.autowrap_mode = TextServer.AUTOWRAP_WORD_SMART
	_panel.add_child(_note)

	show_menu()

## --- экраны ---

func show_menu() -> void:
	_screen = Screen.MENU
	_build("ClearAhead", "", [
		{"text": "Новая игра", "on": func() -> void: new_game_requested.emit()},
		{"text": "Загрузить", "stub": "Загружать пока нечего: сохранённой игры не существует — состояние появится вместе с движением (В2-В3). Выбор КАРТЫ живёт в «Новой игре»."},
		{"text": "Выход", "on": func() -> void: quit_requested.emit()},
	])

## Выбор карты. Список приходит с сервера (GET /maps), литералов карт в клиенте
## нет — их и не должно быть: карты добавляются на сервере, а не пересборкой
## клиента. Пустой список сюда не доходит, о нём говорит отказ.
##
## Подпись приходит СНАРУЖИ, а не зашита здесь: в офлайне карта берётся из
## файла эталона, и строчка «карты живут на сервере» была бы прямой ложью на
## экране — ровно тем, против чего заведены объяснения у заглушек.
func show_maps(maps: Array, note: String) -> void:
	_screen = Screen.MAP
	var items: Array = []
	for m: Dictionary in maps:
		var rev: int = m.get("map_revision", 0)
		# Ревизия в подписи, а не в имени кнопки: имя адресует файл, ревизия —
		# это про содержимое, и она поменяется без переименования.
		var hint := "%s · ревизия %d" % [m.get("map_id", "?"), rev] if rev > 0 else str(m.get("map_id", ""))
		var name: String = m["name"]
		items.append({
			"text": name,
			"hint": hint,
			"on": func() -> void: map_chosen.emit(name),
		})
	items.append({"text": "Назад", "on": func() -> void: back_requested.emit()})
	_build("Где играем", note, items)

func show_roles() -> void:
	_screen = Screen.ROLE
	var items: Array = []
	for role: int in [Role.DRIVER, Role.DSP, Role.BUILDER]:
		items.append({
			"text": ROLE_NAMES[role],
			"hint": ROLE_NOTES[role],
			"on": func() -> void: role_chosen.emit(role),
		})
	items.append({"text": "Назад", "on": func() -> void: back_requested.emit()})
	_build("Кем играем", "Мир один и тот же. Роль решает, откуда вы на него смотрите и что можете делать.", items)

func show_pause() -> void:
	_screen = Screen.PAUSE
	_build("Пауза", "", [
		{"text": "Продолжить", "on": func() -> void: resume_requested.emit()},
		{"text": "Сохранить", "stub": "Сохранять пока нечего: состояния игры — времени, положения составов, заданий — ещё не существует, оно появится вместе с движением (В2-В3)."},
		{"text": "Выйти в меню", "on": func() -> void: to_menu_requested.emit()},
		{"text": "Выход", "on": func() -> void: quit_requested.emit()},
	])

func hide_all() -> void:
	_screen = Screen.NONE
	_root.visible = false

func current_screen() -> int:
	return _screen

## --- сборка столбца кнопок ---

func _build(title: String, note: String, items: Array) -> void:
	_root.visible = true
	_title.text = title
	_note.text = note
	_note.visible = note != ""
	# Снять со сцены СРАЗУ, а освободить отложенно: один queue_free() оставил бы
	# кнопки прошлого экрана видимыми до конца кадра, и они успели бы нарисоваться
	# поверх новых.
	for child in _panel.get_children():
		if child == _title or child == _note:
			continue
		_panel.remove_child(child)
		child.queue_free()

	var first: Button = null
	for item: Dictionary in items:
		var b := _make_button(item)
		_panel.add_child(b)
		if first == null:
			first = b
		var hint: String = item.get("hint", "")
		if hint != "":
			_panel.add_child(_make_hint(hint))
	# Фокус на первой кнопке: без него клавиатура в меню не работает вовсе, а
	# мышь в роли машиниста могла остаться захваченной миром.
	if first != null:
		first.grab_focus()

func _make_button(item: Dictionary) -> Button:
	var b := Button.new()
	b.text = item["text"]
	b.custom_minimum_size = Vector2(BUTTON_MIN_WIDTH, 0)
	b.add_theme_font_size_override("font_size", ITEM_SIZE)
	var stub: String = item.get("stub", "")
	if stub != "":
		b.pressed.connect(func() -> void: _say_stub(stub))
	else:
		b.pressed.connect(item["on"])
	return b

func _make_hint(text: String) -> Label:
	var l := Label.new()
	l.text = text
	l.horizontal_alignment = HORIZONTAL_ALIGNMENT_CENTER
	l.add_theme_font_size_override("font_size", NOTE_SIZE)
	l.add_theme_color_override("font_color", Color(0.62, 0.66, 0.71))
	l.custom_minimum_size = Vector2(BUTTON_MIN_WIDTH, 0)
	l.autowrap_mode = TextServer.AUTOWRAP_WORD_SMART
	return l

## Заглушка объясняет себя на экране, а не молчит в лог: это то же правило, по
## которому отказ сервера в клиенте показывается человеком читаемой строкой.
func _say_stub(text: String) -> void:
	_note.text = text
	_note.visible = true

## Строка состояния поверх меню — загрузка геометрии и отказы сервера.
func say(text: String) -> void:
	_note.text = text
	_note.visible = text != ""
