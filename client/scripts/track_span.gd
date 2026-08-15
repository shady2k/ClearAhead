## TrackSpan — ОТРЕЗОК ПУТИ, ЗАНЯТЫЙ МАШИНОЙ, глазами клиента.
##
## Приезжает с провода полем `span` единицы (contract/channel.v1.json, вид
## interval_u): упорядоченная от конца B к концу A цепочка интервалов
## {element, from, to, direction}. Направление говорит, совпадает ли ход B → A с
## ростом u ЭТОГО элемента; оси соседних элементов бывают встречны, поэтому
## направление живёт у каждого визита.
##
## # Клиент здесь НИЧЕГО НЕ ВЫДУМЫВАЕТ
##
## Ни связности сети, ни положения остряков у него нет и не будет: какой элемент
## соседний, решает сервер. Всё, что делает этот файл, — читает присланную
## цепочку как ОДНУ координату вдоль пути. Ни один элемент, которого нет в
## присланном отрезке, здесь не появляется.
##
## # Зачем координата вдоль пути
##
## Затем, что две вещи на экране её требуют, и обе до 2026-08-15 платили за её
## отсутствие:
##
##   ХОРДА МЕЖДУ ШКВОРНЯМИ. Машина ставится не по касательной в одной точке, а
##     хордой между двумя шкворнями (RollingStock._stand). Шкворень вправе
##     оказаться на СОСЕДНЕМ элементе, а Element.pose_at за конец элемента не
##     выходит — и правильно делает. Лечением было СЖАТИЕ хорды у конца
##     (ClearAhead-cc0x) ценой в поправку, которая у самого конца пропадала.
##   ПОКАЗ МЕЖДУ СНИМКАМИ. Правило 1 кинематики показа запрещало интерполяцию
##     через границу элемента: u сбрасывается, и продолжение неоднозначно. Цена
##     — целый промежуток снимков разом на каждой смене элемента (ЗАМЕР:
##     0.72…0.73 м за кадр при 6.9 м/с). Отрезок даёт двум снимкам на разных
##     элементах ОБЩУЮ систему отсчёта, и запрет становится не нужен.
##
## # Допуск, а не равенство
##
## Координаты приезжают float'ами в метрах, и точка отсчёта, посчитанная
## сервером в целых микрометрах, после перевода в u и обратно вправе оказаться на
## микрометр вне своего визита. Правило проекта («float64 не сверяется байт в
## байт») здесь применяется буквально: принадлежность интервалу спрашивается с
## допуском, и допуск назван.
class_name TrackSpan
extends RefCounted

## EPS — допуск принадлежности точки визиту, метры.
##
## Микрометр — единица, в которой сервер считает длины, и меньше неё расхождения
## не бывает по построению. Взят на порядок больше (10 мкм), чтобы покрыть и сам
## перевод s → u, идущий через float.
const EPS := 1e-5

## FORWARD, REVERSE — направление визита. Строки те же, что на проводе и у
## netloc.Direction: третьего написания одного и того же не заводится.
const FORWARD := "forward"
const REVERSE := "reverse"


## parse — отрезок с провода в нормализованный вид.
##
## Пустой массив означает «отрезка не прислали». Это законное состояние ровно для
## единицы без состояния физики — той же, у которой `at` есть запись расстановки,
## а не положение. Молчаливой подстановки отрезка по точке ЗДЕСЬ НЕТ: длину
## машины знает паспорт, а не адрес, и собранный из них отрезок был бы клиентской
## выдумкой ровно того рода, за которую снесли прежний клиент.
static func parse(raw: Variant) -> Array:
	var out: Array = []
	if typeof(raw) != TYPE_ARRAY:
		return out
	for item in (raw as Array):
		if typeof(item) != TYPE_DICTIONARY:
			return []
		var d := item as Dictionary
		var element := String(d.get("element", ""))
		var dir := String(d.get("direction", ""))
		if element == "" or (dir != FORWARD and dir != REVERSE):
			return []
		var from := float(d.get("from", 0.0))
		var to := float(d.get("to", 0.0))
		if to < from:
			return []
		out.append({"element": element, "from": from, "to": to, "direction": dir})
	return out


## length — длина отрезка вдоль пути, метры.
static func length(span: Array) -> float:
	var total := 0.0
	for v in span:
		total += float(v["to"]) - float(v["from"])
	return total


## side_b — координата конца B у визита: та, с которой ход B → A начинается.
static func side_b(v: Dictionary) -> float:
	return float(v["from"]) if String(v["direction"]) == FORWARD else float(v["to"])


## side_a — координата конца A у визита.
static func side_a(v: Dictionary) -> float:
	return float(v["to"]) if String(v["direction"]) == FORWARD else float(v["from"])


## offset_of — расстояние от конца B до точки (element, u) вдоль отрезка.
##
## Возвращает -1.0, если точки на отрезке нет. Отрицательным числом, а не нулём:
## ноль — законное расстояние (сам конец B), и путать «в начале» с «нигде» здесь
## значило бы ставить машину концом вперёд при первом же расхождении.
static func offset_of(span: Array, element: String, u: float) -> float:
	var acc := 0.0
	for v in span:
		var seg := float(v["to"]) - float(v["from"])
		if String(v["element"]) == element and u >= float(v["from"]) - EPS and u <= float(v["to"]) + EPS:
			return acc + absf(u - side_b(v))
		acc += seg
	return -1.0


## point_at — точка на расстоянии d от конца B.
##
## Отдаёт {element, u, direction}. Пустой словарь, если отрезок пуст; за концами
## отрезка точка ПРИЖИМАЕТСЯ к ним, а не выдумывается: позы за концом присланного
## пути не существует, и это то же решение, что у Element.pose_at.
static func point_at(span: Array, d: float) -> Dictionary:
	if span.is_empty():
		return {}
	var rest := maxf(d, 0.0)
	for v in span:
		var seg := float(v["to"]) - float(v["from"])
		if rest > seg:
			rest -= seg
			continue
		var forward := String(v["direction"]) == FORWARD
		var u := (float(v["from"]) + rest) if forward else (float(v["to"]) - rest)
		return {"element": String(v["element"]), "u": u, "direction": String(v["direction"])}
	var last: Dictionary = span[span.size() - 1]
	return {"element": String(last["element"]), "u": side_a(last),
		"direction": String(last["direction"])}


## merge — ОБЩАЯ ЦЕПОЧКА двух отрезков одной машины.
##
## # Зачем она и почему это не «склеить два списка»
##
## Показ идёт между двумя снимками, а снимки застали машину в разных местах: у
## одного хвост ещё на стрелке, у другого голова уже на главном пути. Чтобы
## сложить их положения одной формулой, нужна ОДНА координата, покрывающая оба, —
## и она есть, потому что оба отрезка суть окна одного пути: за промежуток
## снимков (замер: медиана 100 мс) машина проходит метры при собственной длине в
## десятки, и общий кусок у них заведомо есть.
##
## Цепочка строится так: находится первый ОБЩИЙ элемент, дальше списки обязаны
## идти шаг в шаг (это один путь), а концы берутся у того, кто дотянулся дальше.
## Интервал общего элемента — ОБЪЕДИНЕНИЕ: у одного снимка он покрыт с одной
## стороны, у другого с другой, и вместе они покрывают его целиком.
##
## Пустой массив означает «общего куска нет»: машина за промежуток проехала
## больше собственной длины (то есть снимков не было очень долго) либо её
## переставили. Тогда показ обязан не выдумывать середину, а держаться одного
## снимка — это делает вызывающий.
static func merge(a: Array, b: Array) -> Array:
	if a.is_empty():
		return b.duplicate(true)
	if b.is_empty():
		return a.duplicate(true)
	var ia := -1
	var ib := -1
	for i in a.size():
		var j := _index_of(b, String(a[i]["element"]))
		if j >= 0:
			ia = i
			ib = j
			break
	if ia < 0:
		return []
	# Один из отступов ОБЯЗАН быть нулевым: оба отрезка — окна одного пути, и
	# первый общий элемент не может иметь предшественников сразу у обоих.
	# Ненулевые оба означают, что цепочки не про один путь, и склеивать их нельзя.
	if ia > 0 and ib > 0:
		return []
	var out: Array = []
	if ia > 0:
		for i in ia:
			out.append((a[i] as Dictionary).duplicate())
	else:
		for i in ib:
			out.append((b[i] as Dictionary).duplicate())
	var k := 0
	while ia + k < a.size() or ib + k < b.size():
		var va: Variant = a[ia + k] if ia + k < a.size() else null
		var vb: Variant = b[ib + k] if ib + k < b.size() else null
		if va != null and vb != null:
			if String(va["element"]) != String(vb["element"]) \
					or String(va["direction"]) != String(vb["direction"]):
				# Пути разошлись: машина ушла на другую ветвь между снимками.
				# Общей координаты у таких отрезков нет, и выдумывать её нельзя.
				return []
			out.append({
				"element": String(va["element"]),
				"from": minf(float(va["from"]), float(vb["from"])),
				"to": maxf(float(va["to"]), float(vb["to"])),
				"direction": String(va["direction"]),
			})
		elif va != null:
			out.append((va as Dictionary).duplicate())
		else:
			out.append((vb as Dictionary).duplicate())
		k += 1
	return out


static func _index_of(span: Array, element: String) -> int:
	for i in span.size():
		if String(span[i]["element"]) == element:
			return i
	return -1
