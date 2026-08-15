## ОТРЕЗОК ПУТИ НА СТОРОНЕ КЛИЕНТА — координата вдоль пути и склейка двух окон.
##
## Чистая по природе: ни сети, ни фикстуры здесь не нужно вовсе. Отрезок — это
## арифметика над присланной цепочкой, и проверять её надо на цепочках, которые
## можно написать руками и прочитать глазами.
##
## # Зачем это проверять отдельно от постановки
##
## Потому что ошибка здесь ТИХАЯ. Показ, ошибшийся в координате вдоль пути на
## длину элемента, не падает и не рисует пустой кадр — он ставит машину не туда,
## и заметить это можно только глазами и только на границе. Ровно так проект уже
## терял 12.6 м на переходе E_MAIN → SW1:straight, и нашёл это владелец, а не
## проверка.
##
## # Что здесь НЕ проверяется
##
## Согласие с сервером. Форму отрезка сверяет договор (95_channel_contract.gd),
## числа приходят с провода, а связность сети клиент не знает вовсе. Здесь —
## только то, что клиент делает с уже присланным.
extends "res://tools/check_suite.gd"

## Допуск сравнения расстояний, метры. Микрометр — разрешающая способность мира;
## правило проекта запрещает сверять float64 байт в байт.
const EPS := 1e-6


func run() -> void:
	_check_one_element()
	_check_two_elements()
	_check_reverse_visit()
	_check_merge()
	_check_merge_refuses()


## Один элемент: координата вдоль пути совпадает со смещением по u.
func _check_one_element() -> void:
	var span: Array = [{"element": "A", "from": 10.0, "to": 30.0,
		"direction": TrackSpan.FORWARD}]
	_ok("длина отрезка из одного визита", is_equal_approx(TrackSpan.length(span), 20.0),
		str(TrackSpan.length(span)))
	_ok("конец B — начало интервала у визита forward",
		is_equal_approx(TrackSpan.offset_of(span, "A", 10.0), 0.0),
		str(TrackSpan.offset_of(span, "A", 10.0)))
	_ok("конец A — конец интервала у визита forward",
		is_equal_approx(TrackSpan.offset_of(span, "A", 30.0), 20.0),
		str(TrackSpan.offset_of(span, "A", 30.0)))
	var mid := TrackSpan.point_at(span, 5.0)
	_ok("точка на 5 м от конца B", String(mid["element"]) == "A" and is_equal_approx(float(mid["u"]), 15.0),
		"%s при u = %s" % [mid.get("element"), str(mid.get("u"))])
	# ТОЧКИ ВНЕ ОТРЕЗКА НЕ СУЩЕСТВУЕТ, и это не то же самое, что «на конце B».
	_ok("точка чужого элемента отрезку не принадлежит",
		TrackSpan.offset_of(span, "Б", 15.0) < 0.0, str(TrackSpan.offset_of(span, "Б", 15.0)))
	_ok("точка за интервалом отрезку не принадлежит",
		TrackSpan.offset_of(span, "A", 40.0) < 0.0, str(TrackSpan.offset_of(span, "A", 40.0)))


## Два элемента: координата ПРОДОЛЖАЕТСЯ через границу, а не сбрасывается.
##
## Это и есть то, ради чего отрезок приехал: u на границе сбрасывается в ноль, и
## линейная интерполяция по нему дала бы прыжок на длину элемента.
func _check_two_elements() -> void:
	var span: Array = [
		{"element": "A", "from": 90.0, "to": 100.0, "direction": TrackSpan.FORWARD},
		{"element": "B", "from": 0.0, "to": 24.0, "direction": TrackSpan.FORWARD},
	]
	_ok("длина отрезка через границу", is_equal_approx(TrackSpan.length(span), 34.0),
		str(TrackSpan.length(span)))
	_ok("граница элементов — одна точка на пути",
		is_equal_approx(TrackSpan.offset_of(span, "A", 100.0), 10.0)
			and is_equal_approx(TrackSpan.offset_of(span, "B", 0.0), 10.0),
		"A: %s, B: %s" % [str(TrackSpan.offset_of(span, "A", 100.0)),
			str(TrackSpan.offset_of(span, "B", 0.0))])
	var p := TrackSpan.point_at(span, 12.0)
	_ok("точка за границей лежит на втором элементе",
		String(p["element"]) == "B" and is_equal_approx(float(p["u"]), 2.0),
		"%s при u = %s" % [p.get("element"), str(p.get("u"))])
	# ЗА КОНЦАМИ ОТРЕЗКА ТОЧКА ПРИЖИМАЕТСЯ, а не выдумывается: позы за концом
	# присланного пути не существует, и это то же решение, что у pose_at.
	var beyond := TrackSpan.point_at(span, 1000.0)
	_ok("за концом A точка прижимается к концу отрезка",
		String(beyond["element"]) == "B" and is_equal_approx(float(beyond["u"]), 24.0),
		"%s при u = %s" % [beyond.get("element"), str(beyond.get("u"))])


## ВСТРЕЧНЫЙ ЭЛЕМЕНТ: у визита reverse ход B → A идёт ПРОТИВ роста u.
##
## Оси соседних элементов бывают встречны — это свойство карты, а не редкость, —
## и путать здесь знак значит ставить машину задом наперёд ровно на половине
## станции.
func _check_reverse_visit() -> void:
	var span: Array = [{"element": "A", "from": 10.0, "to": 30.0,
		"direction": TrackSpan.REVERSE}]
	_ok("у визита reverse конец B — КОНЕЦ интервала",
		is_equal_approx(TrackSpan.offset_of(span, "A", 30.0), 0.0),
		str(TrackSpan.offset_of(span, "A", 30.0)))
	_ok("у визита reverse конец A — начало интервала",
		is_equal_approx(TrackSpan.offset_of(span, "A", 10.0), 20.0),
		str(TrackSpan.offset_of(span, "A", 10.0)))
	var p := TrackSpan.point_at(span, 5.0)
	_ok("точка на 5 м от конца B идёт против роста u",
		is_equal_approx(float(p["u"]), 25.0), str(p.get("u")))


## СКЛЕЙКА ДВУХ ОКОН одного пути.
##
## Два снимка застали машину в разных местах; общая цепочка обязана покрыть оба и
## дать им одну координату. Проверяется на всех трёх взаимных положениях: окно
## уехало вперёд, назад и осталось на месте.
func _check_merge() -> void:
	var back: Array = [
		{"element": "A", "from": 90.0, "to": 100.0, "direction": TrackSpan.FORWARD},
		{"element": "B", "from": 0.0, "to": 24.0, "direction": TrackSpan.FORWARD},
	]
	var ahead: Array = [
		{"element": "A", "from": 97.0, "to": 100.0, "direction": TrackSpan.FORWARD},
		{"element": "B", "from": 0.0, "to": 31.0, "direction": TrackSpan.FORWARD},
	]
	var chain := TrackSpan.merge(back, ahead)
	_ok("цепочка покрывает оба окна", chain.size() == 2 and is_equal_approx(TrackSpan.length(chain), 41.0),
		"визитов %d, длина %s" % [chain.size(), str(TrackSpan.length(chain))])
	# И ОБА ПОЛОЖЕНИЯ ЛЕЖАТ В НЕЙ, и разность их координат равна пройденному пути.
	var was := TrackSpan.offset_of(chain, "B", 7.0)
	var now := TrackSpan.offset_of(chain, "B", 14.0)
	_ok("обе точки лежат в цепочке", was >= 0.0 and now >= 0.0, "%s / %s" % [str(was), str(now)])
	_ok("разность координат равна пройденному пути", is_equal_approx(now - was, 7.0),
		str(now - was))

	# ОКНО УШЛО НА ЦЕЛЫЙ ЭЛЕМЕНТ: хвост потерял A, голова набрала C.
	var far: Array = [
		{"element": "B", "from": 20.0, "to": 40.0, "direction": TrackSpan.FORWARD},
		{"element": "C", "from": 0.0, "to": 14.0, "direction": TrackSpan.FORWARD},
	]
	var long_chain := TrackSpan.merge(back, far)
	_ok("цепочка сводит окна без общего края", long_chain.size() == 3,
		"визитов %d" % long_chain.size())
	if long_chain.size() == 3:
		_ok("средний элемент цепочки покрыт целиком",
			is_equal_approx(float(long_chain[1]["from"]), 0.0)
				and is_equal_approx(float(long_chain[1]["to"]), 40.0),
			"[%s, %s]" % [str(long_chain[1]["from"]), str(long_chain[1]["to"])])

	# СКЛЕЙКА СИММЕТРИЧНА: порядок снимков не меняет цепочки. Свойство, а не
	# пример: показ зовёт merge для пары (старый, новый), а разбор — как придётся.
	var flipped := TrackSpan.merge(ahead, back)
	_ok("склейка не зависит от порядка окон", _same(chain, flipped), str(flipped))

	# ОДНО ОКНО ПУСТО — цепочка есть второе окно: единица без состояния физики
	# отрезка не имеет, и это законно.
	_ok("пустое окно не мешает склейке", _same(TrackSpan.merge([], back), back))
	_ok("два пустых окна дают пустую цепочку", TrackSpan.merge([], []).is_empty())


## ЦЕПОЧКА НЕ СТРОИТСЯ — и это отказ, а не правдоподобная склейка.
##
## Правило проекта «валидатор отказывает, а не чинит» действует и здесь: два
## отрезка без общего элемента — это либо очень долгое молчание сервера, либо
## машина, ушедшая на другую ветвь. Склеенные наугад, они дали бы показ, едущий
## по пути, которого не было.
func _check_merge_refuses() -> void:
	var here: Array = [{"element": "A", "from": 0.0, "to": 10.0, "direction": TrackSpan.FORWARD}]
	var there: Array = [{"element": "Z", "from": 0.0, "to": 10.0, "direction": TrackSpan.FORWARD}]
	_ok("окна без общего элемента не склеиваются", TrackSpan.merge(here, there).is_empty())

	# РАЗОШЛИСЬ ПОСЛЕ ОБЩЕГО ЭЛЕМЕНТА: машина ушла на другую ветвь стрелки.
	var straight: Array = [
		{"element": "SW", "from": 0.0, "to": 33.0, "direction": TrackSpan.FORWARD},
		{"element": "MAIN", "from": 0.0, "to": 5.0, "direction": TrackSpan.FORWARD},
	]
	var diverging: Array = [
		{"element": "SW", "from": 0.0, "to": 33.0, "direction": TrackSpan.FORWARD},
		{"element": "SIDING", "from": 0.0, "to": 5.0, "direction": TrackSpan.FORWARD},
	]
	_ok("окна, разошедшиеся на стрелке, не склеиваются",
		TrackSpan.merge(straight, diverging).is_empty())

	# ИСПОРЧЕННЫЙ ПРОВОД — пустой отрезок, а не половина: визит без направления
	# не говорит, каким концом тело лежит, и читать его как forward значило бы
	# применить молчаливое умолчание.
	_ok("визит без направления отвергается целиком",
		TrackSpan.parse([{"element": "A", "from": 0.0, "to": 1.0}]).is_empty())
	_ok("вывернутый интервал отвергается целиком",
		TrackSpan.parse([{"element": "A", "from": 5.0, "to": 1.0,
			"direction": TrackSpan.FORWARD}]).is_empty())
	_ok("отрезка нет вовсе — пустой массив", TrackSpan.parse(null).is_empty())


func _same(a: Array, b: Array) -> bool:
	if a.size() != b.size():
		return false
	for i in a.size():
		var x := a[i] as Dictionary
		var y := b[i] as Dictionary
		if String(x["element"]) != String(y["element"]) \
				or String(x["direction"]) != String(y["direction"]) \
				or absf(float(x["from"]) - float(y["from"])) > EPS \
				or absf(float(x["to"]) - float(y["to"])) > EPS:
			return false
	return true
