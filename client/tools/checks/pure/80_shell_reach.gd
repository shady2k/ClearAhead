## ПУНКТ МЕНЮ «ДАЛЬНОСТЬ ВЗГЛЯДА» — ЧТО ОН ЕСТЬ И ЧТО ОН РАБОТАЕТ.
##
## Сети здесь делать нечего: проверяется список настройки и проводка кнопки.
##
## # Зачем это проверять, если видно глазом
##
## Не видно. Снимок экрана делает МИР (world.gd), и меню на него не попадает
## вовсе — оболочка до входа в роль не рисуется ни в одном снимке прогона.
## Значит пункт, потерявший обработчик, выглядел бы ровно как исправный: кнопка
## на месте, надпись на месте, нажатие не делает ничего. Это тот же класс, что
## заглушки в ShellUI, только наоборот: там кнопка честно говорит, что за ней
## ничего нет, а здесь за кнопкой обязано что-то быть.
##
## Порядок и содержимое списка тоже проверяются, и не для красоты: перебор идёт
## по кругу по нему же, а «сколько есть» обязано быть в нём ровно одно —
## бесконечность, попавшая в середину, оборвала бы перебор на себе.
extends "res://tools/check_suite.gd"

const AppScript := preload("res://scripts/app.gd")
const ShellUIScript := preload("res://scripts/shell_ui.gd")


func run() -> void:
	var list: Array = AppScript.VIEW_REACH_M
	_ok("список дальностей не пуст", list.size() >= 2, "%d пунктов" % list.size())

	var ascending := true
	var infinities := 0
	for i in list.size():
		var v := float(list[i])
		if not is_finite(v):
			infinities += 1
		if i > 0 and not (v > float(list[i - 1])):
			ascending = false
	_ok("дальности идут по возрастанию", ascending, str(list))
	_ok("«сколько есть» ровно одно и последнее", infinities == 1 and not is_finite(float(list[-1])),
		"бесконечностей %d" % infinities)
	_ok("все прочие дальности положительны", float(list[0]) > 0.0, "%.0f м" % float(list[0]))

	var idx: int = AppScript.VIEW_REACH_DEFAULT_INDEX
	_ok("умолчание указывает в список", idx >= 0 and idx < list.size(), "индекс %d" % idx)
	_ok("умолчание — конечная даль, а не «сколько есть»", is_finite(float(list[idx])),
		"%.0f м" % float(list[idx]))

	# ПУНКТ СТРОИТСЯ И НАЖИМАЕТСЯ. Надпись приходит снаружи: экран не знает ни
	# списка, ни текущей дальности, и проверка убеждается ровно в этом — она
	# передаёт свою строку и ищет её на кнопке.
	var shell := ShellUIScript.new()
	ctx.tree.root.add_child(shell)
	var mark := "ДАЛЬНОСТЬ-ПРОВЕРКИ 1234 м"
	shell.show_menu(mark)
	var button := _find_button(shell, mark)
	_ok("надпись дальности доехала до кнопки меню", button != null)
	if button != null:
		var pressed := [false]
		shell.view_reach_next_requested.connect(func() -> void: pressed[0] = true)
		button.emit_signal("pressed")
		_ok("нажатие просит следующую дальность", bool(pressed[0]))
	ctx.tree.root.remove_child(shell)
	shell.queue_free()


## _find_button — кнопка с этой надписью где-нибудь под узлом.
##
## Обходом, а не путём в сцене: раскладка меню — дело ShellUI, и проверка,
## знающая её путь, ломалась бы от перестановки кнопок.
func _find_button(node: Node, text: String) -> Button:
	for child in node.get_children():
		if child is Button and (child as Button).text == text:
			return child as Button
		var found := _find_button(child, text)
		if found != null:
			return found
	return null
