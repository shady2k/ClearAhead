extends SceneTree
## Тесты парсера контракта: валидные тела принимаются, каждая нехватка поля,
## неверный тип и неизвестный kind — отклоняются. Запуск:
##   godot --headless --path client --script tests/test_geometry_parser.gd

const Parser := preload("res://scripts/geometry_parser.gd")

var _total := 0
var _failures := 0

func _initialize() -> void:
	_run()
	if _failures == 0:
		print("PARSER TESTS OK: %d проверок" % _total)
		quit(0)
	else:
		printerr("PARSER TESTS FAIL: %d из %d не прошло" % [_failures, _total])
		quit(1)

func _run() -> void:
	# --- валидные тела ---
	_expect_ok('{"map_id":"M","map_revision":2,"elements":[{"id":"a","start":{"plan":{"x":0,"y":0,"heading":0},"z":1,"slope":0},"primitives":[{"kind":"straight","length":10},{"kind":"arc","length":5,"radius":300,"angle":0.1}]}]}',
		"валидная геометрия")
	_expect_ok('{"map_id":"M","map_revision":1,"elements":[]}', "пустая станция допустима")
	_expect_ok('{"map_id":"M","map_revision":2.0,"elements":[]}', "revision как float-целое")
	_expect_ok('{"map_id":"M","map_revision":1,"elements":[],"extra_field":42}', "лишние поля игнорируются")
	_expect_ok('{"map_id":"M","map_revision":1,"elements":[{"id":"a","start":{"plan":{"x":0,"y":0,"heading":-3.14159},"z":142,"slope":-0},"primitives":[{"kind":"arc","length":33.21,"radius":300,"angle":-0.1107}]}]}',
		"отрицательные heading/angle допустимы")

	# --- корень ---
	_expect_fail("", "пустое тело")
	_expect_fail("{", "битый JSON")
	_expect_fail("42", "корень не объект")
	_expect_fail('{"map_revision":1,"elements":[]}', "нет map_id")
	_expect_fail('{"map_id":"","map_revision":1,"elements":[]}', "map_id пустая")
	_expect_fail('{"map_id":7,"map_revision":1,"elements":[]}', "map_id не строка")
	_expect_fail('{"map_id":"M","elements":[]}', "нет map_revision")
	_expect_fail('{"map_id":"M","map_revision":1.5,"elements":[]}', "revision не целое")
	_expect_fail('{"map_id":"M","map_revision":"1","elements":[]}', "revision строка")
	_expect_fail('{"map_id":"M","map_revision":1}', "нет elements")
	_expect_fail('{"map_id":"M","map_revision":1,"elements":[{"id":"a","start":{"plan":{"x":0,"y":0,"heading":0},"z":1,"slope":0},"primitives":[{"kind":"straight","length":1}]},{"id":"a","start":{"plan":{"x":0,"y":0,"heading":0},"z":1,"slope":0},"primitives":[{"kind":"straight","length":1}]}]}',
	"дубликат id")
	_expect_fail('{"map_id":"M","map_revision":1,"elements":["строка"]}', "элемент не объект")

	# --- start ---
	_expect_fail('{"map_id":"M","map_revision":1,"elements":[{"id":"a","primitives":[{"kind":"straight","length":1}]}]}',
		"нет start")
	_expect_fail('{"map_id":"M","map_revision":1,"elements":[{"id":"a","start":{"plan":{"x":0,"y":0,"heading":0},"z":1},"primitives":[{"kind":"straight","length":1}]}]}',
		"нет slope")
	_expect_fail('{"map_id":"M","map_revision":1,"elements":[{"id":"a","start":{"plan":{"x":0,"y":0},"z":1,"slope":0},"primitives":[{"kind":"straight","length":1}]}]}',
		"нет heading")
	_expect_fail('{"map_id":"M","map_revision":1,"elements":[{"id":"a","start":{"plan":{"x":0,"y":0,"heading":0},"slope":0},"primitives":[{"kind":"straight","length":1}]}]}',
		"нет z")
	_expect_fail('{"map_id":"M","map_revision":1,"elements":[{"id":"a","start":{"plan":{"x":"0","y":0,"heading":0},"z":1,"slope":0},"primitives":[{"kind":"straight","length":1}]}]}',
		"plan.x — строка")
	_expect_fail('{"map_id":"M","map_revision":1,"elements":[{"id":"a","start":{"plan":{"x":0,"y":0,"heading":0},"z":"1","slope":0},"primitives":[{"kind":"straight","length":1}]}]}',
		"z — строка")

	# --- примитивы ---
	_expect_fail('{"map_id":"M","map_revision":1,"elements":[{"id":"a","start":{"plan":{"x":0,"y":0,"heading":0},"z":1,"slope":0},"primitives":[{"kind":"straight"}]}]}',
		"straight без length")
	_expect_fail('{"map_id":"M","map_revision":1,"elements":[{"id":"a","start":{"plan":{"x":0,"y":0,"heading":0},"z":1,"slope":0},"primitives":[{"kind":"straight","length":0}]}]}',
		"length == 0")
	_expect_fail('{"map_id":"M","map_revision":1,"elements":[{"id":"a","start":{"plan":{"x":0,"y":0,"heading":0},"z":1,"slope":0},"primitives":[{"kind":"straight","length":-5}]}]}',
		"length < 0")
	_expect_fail('{"map_id":"M","map_revision":1,"elements":[{"id":"a","start":{"plan":{"x":0,"y":0,"heading":0},"z":1,"slope":0},"primitives":[{"kind":"straight","length":"10"}]}]}',
		"length — строка")
	_expect_fail('{"map_id":"M","map_revision":1,"elements":[{"id":"a","start":{"plan":{"x":0,"y":0,"heading":0},"z":1,"slope":0},"primitives":[{"kind":"arc","length":5}]}]}',
		"arc без radius и angle")
	_expect_fail('{"map_id":"M","map_revision":1,"elements":[{"id":"a","start":{"plan":{"x":0,"y":0,"heading":0},"z":1,"slope":0},"primitives":[{"kind":"arc","length":5,"radius":300}]}]}',
		"arc без angle")
	_expect_fail('{"map_id":"M","map_revision":1,"elements":[{"id":"a","start":{"plan":{"x":0,"y":0,"heading":0},"z":1,"slope":0},"primitives":[{"kind":"arc","length":5,"radius":0,"angle":0.1}]}]}',
		"arc radius == 0")
	_expect_fail('{"map_id":"M","map_revision":1,"elements":[{"id":"a","start":{"plan":{"x":0,"y":0,"heading":0},"z":1,"slope":0},"primitives":[{"kind":"arc","length":5,"radius":-300,"angle":0.1}]}]}',
		"arc radius < 0")
	_expect_fail('{"map_id":"M","map_revision":1,"elements":[{"id":"a","start":{"plan":{"x":0,"y":0,"heading":0},"z":1,"slope":0},"primitives":[{"kind":"clothoid","length":5}]}]}',
		"неизвестный kind")
	_expect_fail('{"map_id":"M","map_revision":1,"elements":[{"id":"a","start":{"plan":{"x":0,"y":0,"heading":0},"z":1,"slope":0},"primitives":[{"kind":"straight","length":1},{"kind":"spiral","length":1}]}]}',
		"неизвестный kind в середине цепочки")
	_expect_fail('{"map_id":"M","map_revision":1,"elements":[{"id":"a","start":{"plan":{"x":0,"y":0,"heading":0},"z":1,"slope":0},"primitives":[]}]}',
		"primitives пуст")
	_expect_fail('{"map_id":"M","map_revision":1,"elements":[{"id":"a","start":{"plan":{"x":0,"y":0,"heading":0},"z":1,"slope":0},"primitives":[1,2]}]}',
		"примитивы не объекты")

func _expect_ok(text: String, name: String) -> void:
	_total += 1
	var res := Parser.parse(text)
	if res.ok:
		return
	_failures += 1
	printerr("FAIL [%s]: ожидался успех, ошибка: %s" % [name, res.error])

func _expect_fail(text: String, name: String) -> void:
	_total += 1
	var res := Parser.parse(text)
	if not res.ok:
		return
	_failures += 1
	printerr("FAIL [%s]: ожидалась ошибка, парсер принял" % name)
