extends CanvasLayer
## UI — не зумится: статус загрузки, ошибки сервера, кнопка «Повторить».

signal retry_requested

var _status := Label.new()
var _retry := Button.new()

func _ready() -> void:
	var panel := PanelContainer.new()
	panel.set_anchors_preset(Control.PRESET_TOP_LEFT)
	panel.position = Vector2(8, 8)
	var box := VBoxContainer.new()
	panel.add_child(box)
	_status.text = ""
	_status.add_theme_font_size_override("font_size", 14)
	box.add_child(_status)
	_retry.text = "Повторить"
	_retry.visible = false
	_retry.pressed.connect(_on_retry_pressed)
	box.add_child(_retry)
	add_child(panel)

func _on_retry_pressed() -> void:
	retry_requested.emit()

func set_status(text: String) -> void:
	_status.text = text
	_retry.visible = false

func set_error(text: String) -> void:
	_status.text = text
	_retry.visible = true
