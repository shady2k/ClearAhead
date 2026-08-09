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
	_expect_ok('{"map_id":"M","map_revision":1,"elements":[],"track_types":[{"id":"T1","gauge":1.435,"sleeper":{"pitch":0.6,"length":2.5,"width":0.28},"ballast":{"half_width":1.75}}],"construction_runs":[{"id":"R1","type":"T1","coordinate":"u","phase":0,"spans":[{"element":"a","from":0,"to":100,"direction":"forward"},{"element":"b","from":20,"to":80,"direction":"reverse"}]}],"features":[{"owner":"SW_1","kind":"frog","point":{"x":1.5,"y":-0.7},"addresses":[{"element":"SW_1:straight","u":29.3,"tangent":{"x":1,"y":0}},{"element":"SW_1:diverging","u":29.2,"tangent":{"x":0.995,"y":-0.11}}]}],"placement_algorithm":"placement-v1"}',
		"полный контракт с конструкцией и крестовиной")
	_expect_ok('{"map_id":"M","map_revision":1,"elements":[],"track_types":[],"construction_runs":[],"features":[],"placement_algorithm":""}',
		"пустые новые массивы допустимы")

	# --- track_types ---
	_expect_fail('{"map_id":"M","map_revision":1,"elements":[],"track_types":[{"id":"T1","gauge":0,"sleeper":{"pitch":0.6,"length":2.5,"width":0.28},"ballast":{"half_width":1.75}}]}',
		"gauge == 0")
	_expect_fail('{"map_id":"M","map_revision":1,"elements":[],"track_types":[{"id":"T1","gauge":1.435,"sleeper":{"pitch":0,"length":2.5,"width":0.28},"ballast":{"half_width":1.75}}]}',
		"sleeper.pitch == 0")
	_expect_fail('{"map_id":"M","map_revision":1,"elements":[],"track_types":[{"id":"T1","gauge":1.435,"sleeper":{"length":2.5,"width":0.28},"ballast":{"half_width":1.75}}]}',
		"нет sleeper.pitch")
	_expect_fail('{"map_id":"M","map_revision":1,"elements":[],"track_types":[{"id":"T1","gauge":1.435,"sleeper":{"pitch":0.6,"length":2.5,"width":0.28}}]}',
		"нет ballast")
	_expect_fail('{"map_id":"M","map_revision":1,"elements":[],"track_types":[{"id":"T1","gauge":1.435,"sleeper":{"pitch":0.6,"length":2.5,"width":0.28},"ballast":{"half_width":-1.75}}]}',
		"ballast.half_width < 0")
	_expect_fail('{"map_id":"M","map_revision":1,"elements":[],"track_types":[{"id":"T1","gauge":1.435,"sleeper":{"pitch":0.6,"length":2.5,"width":0.28},"ballast":{"half_width":1.75}},{"id":"T1","gauge":1.520,"sleeper":{"pitch":0.6,"length":2.5,"width":0.28},"ballast":{"half_width":1.75}}]}',
		"дубликат id типа")

	# --- construction_runs ---
	_expect_fail('{"map_id":"M","map_revision":1,"elements":[],"construction_runs":[{"id":"R1","coordinate":"u","phase":0,"spans":[{"element":"a","from":0,"to":10,"direction":"forward"}]}]}',
		"run без type (в проводе ссылка всегда явная)")
	_expect_fail('{"map_id":"M","map_revision":1,"elements":[],"construction_runs":[{"id":"R1","type":"T1","coordinate":"s","phase":0,"spans":[{"element":"a","from":0,"to":10,"direction":"forward"}]}]}',
		"coordinate не u")
	_expect_fail('{"map_id":"M","map_revision":1,"elements":[],"construction_runs":[{"id":"R1","type":"T1","coordinate":"u","phase":-0.1,"spans":[{"element":"a","from":0,"to":10,"direction":"forward"}]}]}',
		"phase < 0")
	_expect_fail('{"map_id":"M","map_revision":1,"elements":[],"construction_runs":[{"id":"R1","type":"T1","coordinate":"u","phase":0,"spans":[]}]}',
		"run с пустыми спанами")
	_expect_fail('{"map_id":"M","map_revision":1,"elements":[],"construction_runs":[{"id":"R1","type":"T1","coordinate":"u","phase":0,"spans":[{"element":"a","from":0,"to":10,"direction":"diagonal"}]}]}',
		"неизвестное направление спана")
	_expect_fail('{"map_id":"M","map_revision":1,"elements":[],"construction_runs":[{"id":"R1","type":"T1","coordinate":"u","phase":0,"spans":[{"element":"a","from":10,"to":5,"direction":"forward"}]}]}',
		"to < from")
	_expect_fail('{"map_id":"M","map_revision":1,"elements":[],"construction_runs":[{"id":"R1","type":"T1","coordinate":"u","phase":0,"spans":[{"element":"a","from":0,"to":10,"direction":"forward"}]},{"id":"R1","type":"T1","coordinate":"u","phase":0,"spans":[{"element":"a","from":0,"to":10,"direction":"forward"}]}]}',
		"дубликат id run")

	# --- features ---
	_expect_fail('{"map_id":"M","map_revision":1,"elements":[],"features":[{"owner":"SW_1","kind":"switch_toe","point":{"x":0,"y":0},"addresses":[{"element":"a","u":1,"tangent":{"x":1,"y":0}},{"element":"b","u":1,"tangent":{"x":1,"y":0}}]}]}',
		"неизвестный вид особенности")
	_expect_fail('{"map_id":"M","map_revision":1,"elements":[],"features":[{"owner":"SW_1","kind":"frog","point":{"x":0,"y":0},"addresses":[{"element":"a","u":1,"tangent":{"x":1,"y":0}}]}]}',
		"у крестовины один адрес")
	_expect_fail('{"map_id":"M","map_revision":1,"elements":[],"features":[{"owner":"SW_1","kind":"frog","point":{"x":"0","y":0},"addresses":[{"element":"a","u":1,"tangent":{"x":1,"y":0}},{"element":"b","u":1,"tangent":{"x":1,"y":0}}]}]}',
		"point.x — строка")
	_expect_fail('{"map_id":"M","map_revision":1,"elements":[],"features":[{"owner":"SW_1","kind":"frog","point":{"x":0,"y":0},"addresses":[{"element":"a","u":1,"tangent":{"x":1}},{"element":"b","u":1,"tangent":{"x":1,"y":0}}]}]}',
		"tangent.y отсутствует")

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
