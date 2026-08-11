extends CanvasLayer
## HUD роли: кто вы, какая карта под вами и чем управлять. Не зумится.
##
## Управление ПЕЧАТАЕТСЯ НА ЭКРАНЕ, а не лежит в комментарии. Это правило уже
## оплачено дважды: панораму на клавишах невозможно найти наугад, и вид тогда
## выглядит намертво привязанным к одной точке (см. шапку spike_relief.gd).

const TITLE_SIZE := 16
const HINT_SIZE := 13

var _title := Label.new()
var _hint := Label.new()
var _note := Label.new()
var _panel: PanelContainer

func _ready() -> void:
	layer = 5
	_panel = PanelContainer.new()
	_panel.set_anchors_preset(Control.PRESET_TOP_LEFT)
	_panel.position = Vector2(8, 8)
	_panel.mouse_filter = Control.MOUSE_FILTER_IGNORE  # HUD не отбирает мышь у мира
	var box := VBoxContainer.new()
	box.add_theme_constant_override("separation", 2)
	_panel.add_child(box)

	_title.add_theme_font_size_override("font_size", TITLE_SIZE)
	box.add_child(_title)

	_hint.add_theme_font_size_override("font_size", HINT_SIZE)
	_hint.add_theme_color_override("font_color", Color(0.74, 0.78, 0.82))
	box.add_child(_hint)

	# Отладочная строка отдельным цветом: она про оснастку, а не про игру, и
	# путать её с управлением нельзя.
	_note.add_theme_font_size_override("font_size", HINT_SIZE)
	_note.add_theme_color_override("font_color", Color(0.45, 0.85, 0.60))
	_note.visible = false
	box.add_child(_note)

	add_child(_panel)
	visible = false

func show_role(title: String, hint: String) -> void:
	_title.text = title
	_hint.text = hint
	visible = true

func hide_role() -> void:
	visible = false

## Строка отладочного слоя. Пустая — строки нет вовсе, а не пустое место.
func show_note(text: String) -> void:
	_note.text = text
	_note.visible = text != ""
