## ShellUI — экраны оболочки: меню, выбор региона, выбор роли, пауза.
##
## Порт из снесённого спайка (`shell_ui.gd`, 809280f^). Строится КОДОМ, а не
## сценой, по той же причине, что и HUD: раскладка — один столбец кнопок, и
## держать её в .tscn значит править её в двух местах.
##
## ЗАГЛУШКИ ОБЪЯВЛЕНЫ ЗАГЛУШКАМИ. «Загрузить» и «Сохранить» — пункты без провода:
## сохранять пока нечего, состояния игры не существует. Кнопка нажимается и
## ЧЕСТНО ГОВОРИТ, чего ещё нет. Серая неактивная кнопка выглядит поломкой и не
## объясняет себя — это то же правило, по которому отказ сервера показывается
## человеку строкой, а не молчит в лог.
class_name ShellUI
extends CanvasLayer

signal new_game_requested
## Выбран регион. Несёт ИМЯ РЕГИОНА из каталога сервера: именно им спрашивают
## манифест, сеть и рельеф, и подставлять туда что-либо другое нечего.
signal region_chosen(region: String)
signal role_chosen(role: int)
## Нажата дальность взгляда: перебрать на следующую. Именно СИГНАЛ, а не выбор
## значения здесь: список дальностей и текущая принадлежат оболочке (app.gd) —
## она их разбирает из ключа запуска и отдаёт миру. Экран знает только надпись.
signal view_reach_next_requested
signal resume_requested
## «Назад» с экрана выбора. Отдельный сигнал, а не прямой вызов show_menu():
## экраном владеет этот узел, а СОСТОЯНИЕМ — оболочка, и кнопка, переключающая
## картинку молча, оставила бы их расходиться (Esc после «Назад» пришлось бы
## жать дважды).
signal back_requested
signal to_menu_requested
signal quit_requested

const TITLE_SIZE := 40
const ITEM_SIZE := 20
const NOTE_SIZE := 14
const BUTTON_MIN_WIDTH := 300

## Экраны взаимоисключающи: виден ровно один или ни одного (когда игрок в мире).
enum Screen { NONE, MENU, REGION, ROLE, PAUSE }

## Роли объявлены ЗДЕСЬ, а не в app.gd: сигнал role_chosen несёт именно их, и
## подписчику нужны те же числа. Порядок — как в замысле: машинист, ДСП,
## строитель.
enum Role { DRIVER, DSP, BUILDER }

const ROLE_NAMES := {
	Role.DRIVER: "Машинист",
	Role.DSP: "ДСП",
	Role.BUILDER: "Строитель",
}

## Одна строка про то, чего в роли пока НЕТ. Показывается на выборе, чтобы игрок
## не искал управление, которого ещё не написано.
##
## Строка обязана СОВПАДАТЬ С ТЕМ, ЧТО РАБОТАЕТ, и обе половины этого правила уже
## куплены. У машиниста тут стояло «ходьбы и поездов пока нет: человек снесён
## вместе со старым клиентом» — верно до 2026-08-12 (вечер), когда человек
## вернулся переносом из спайка. Строка, отрицающая то, что есть, — то же враньё,
## что и строка, обещающая то, чего нет; поправлена вместе с кодом, а не после.
##
## Границы названы поимённо, чтобы их не искали как дефекты: на платформу с земли
## не всходят (лестниц нет ни в геометрии, ни в контракте), сквозь лес и траву
## проходят насквозь (это MultiMesh, коллизий у него нет вовсе), по реке ходят
## посуху (вода нарочно вне тверди), поездов нет (подвижного состава нет в модели
## мира).
const ROLE_NOTES := {
	Role.DRIVER: "человек ростом 1.80 ходит по той же поверхности, которую видит · V — обзор, от первого лица, от третьего · подойти к локомотиву и сесть в кабину — E; ВЕСТИ его нельзя, органов управления нет · на платформу с земли не взойти и в кабину не влезть по лестнице (лестниц нет ни там, ни там), лес и трава проходимы насквозь, по реке ходят посуху",
	Role.DSP: "мир сверху, вся горловина разом · плоской схемы пока нет (снесена), щёлкать стрелки нечем (В2)",
	Role.BUILDER: "мир сверху под углом, ортография · инструментов правки пока нет",
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
	_note.custom_minimum_size = Vector2(560, 0)
	_note.autowrap_mode = TextServer.AUTOWRAP_WORD_SMART
	_panel.add_child(_note)
	# Экран НЕ строится здесь, и это правка 2026-08-12: в меню появился пункт с
	# настройкой, которой владеет оболочка (app.gd), а не этот узел. Построить
	# меню до того, как владелец сказал текущую дальность взгляда, значило бы
	# нарисовать её из головы. Владелец зовёт show_menu сразу после add_child.


## --- экраны ---

## show_menu — главное меню. reach_text — НАДПИСЬ пункта дальности взгляда,
## готовая: экран не знает ни списка дальностей, ни текущей, и не должен.
##
## focus_reach — оставить фокус на пункте дальности. Нужен, потому что перебор
## значения ПЕРЕСТРАИВАЕТ экран: без этого фокус после каждого нажатия
## возвращался бы на «Новую игру», и перебрать дальность с клавиатуры было бы
## нельзя — кнопка, до которой не дойти клавишей, для клавиатуры не существует.
func show_menu(reach_text: String, focus_reach: bool = false) -> void:
	_screen = Screen.MENU
	_build("ClearAhead", "", [
		{"text": "Новая игра", "on": func() -> void: new_game_requested.emit()},
		# ДАЛЬНОСТЬ ВЗГЛЯДА — ПУНКТ МЕНЮ, А НЕ ЗАГЛУШКА: он работает. Нажатие
		# перебирает значения по кругу — отдельного экрана настроек нет, потому
		# что настройка сегодня одна, и экран из одной строки был бы вежливостью
		# без содержания.
		{"text": reach_text, "focus": focus_reach,
			"hint": "как далеко видно землю. Свойство ВЗГЛЯДА, как и угол камеры роли: сколько мира держать в кадре, решает игрок, а сервер объявляет, докуда он засеян. Просьба сверх этого даст его край — и скажет об этом.",
			"on": func() -> void: view_reach_next_requested.emit()},
		{"text": "Загрузить", "stub": "Загружать пока нечего: сохранённой игры не существует — состояние появится вместе с движением (В2-В3). Выбор РЕГИОНА живёт в «Новой игре»."},
		{"text": "Выход", "on": func() -> void: quit_requested.emit()},
	])


## Выбор региона. Список приходит с сервера каталогом регионов, литералов
## регионов в клиенте нет — и не должно быть: миры добавляются на сервере, а не
## пересборкой клиента.
##
## НЕИГРАБЕЛЬНЫЙ РЕГИОН ПОКАЗЫВАЕТСЯ, А НЕ ПРЯЧЕТСЯ. Регион без сети в памяти
## отказывает на манифесте, то есть снаружи неотличим от опечатки. Спрятать его
## значило бы соврать про содержимое сервера; показать заглушкой — назвать
## причину до того, как игрок упрётся в отказ.
func show_regions(regions: Array, note: String) -> void:
	_screen = Screen.REGION
	var items: Array = []
	for r_raw in regions:
		var r: Dictionary = r_raw as Dictionary
		var name := String(r.get("region", ""))
		var epoch := int(r.get("epoch", 0))
		var rev := int(r.get("revision", 0))
		# Ревизия и эпоха в ПОДПИСИ, а не в имени кнопки: имя адресует регион,
		# а эти два числа про его содержимое и поменяются без переименования.
		var hint := "эпоха %d · ревизия %d" % [epoch, rev] if rev > 0 else "эпоха %d" % epoch
		if bool(r.get("playable", false)):
			items.append({"text": name, "hint": hint,
				"on": func() -> void: region_chosen.emit(name)})
		else:
			items.append({"text": name, "hint": hint,
				"stub": "%s: рельеф есть, а сети нет — сервер отвечает на манифест этого региона отказом. Войти нечем, и это сказано здесь, а не после нажатия." % name})
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
	# кнопки прошлого экрана видимыми до конца кадра, и они успели бы
	# нарисоваться поверх новых.
	for child in _panel.get_children():
		if child == _title or child == _note:
			continue
		_panel.remove_child(child)
		child.queue_free()

	var first: Button = null
	var wanted: Button = null
	for item_raw in items:
		var item: Dictionary = item_raw as Dictionary
		var b := _make_button(item)
		_panel.add_child(b)
		if first == null:
			first = b
		# Пункт вправе попросить фокус себе: экран, перестроенный ради собственной
		# надписи, обязан оставить клавиатуру там, где она была.
		if bool(item.get("focus", false)):
			wanted = b
		var hint := String(item.get("hint", ""))
		if hint != "":
			_panel.add_child(_make_hint(hint))
	# Фокус на первой кнопке: без него клавиатура в меню не работает вовсе.
	var focus_on := wanted if wanted != null else first
	if focus_on != null:
		focus_on.grab_focus()


func _make_button(item: Dictionary) -> Button:
	var b := Button.new()
	b.text = String(item["text"])
	b.custom_minimum_size = Vector2(BUTTON_MIN_WIDTH, 0)
	b.add_theme_font_size_override("font_size", ITEM_SIZE)
	var stub := String(item.get("stub", ""))
	if stub != "":
		b.pressed.connect(func() -> void: say(stub))
	else:
		b.pressed.connect(item["on"] as Callable)
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


## say — строка состояния поверх меню: загрузка, отказы сервера, объяснения
## заглушек. Одно место на все три, потому что читателю это одно и то же — ответ
## экрана на его действие.
func say(text: String) -> void:
	_note.text = text
	_note.visible = text != ""
