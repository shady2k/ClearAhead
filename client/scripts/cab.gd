## Cab — КАБИНА: рукоятки под рукой машиниста.
##
## Держит два положения органов и знает разницу между ними:
##
##   ЖЕЛАЕМОЕ  — куда игрок только что дёрнул рукоятку. Показывается НЕМЕДЛЕННО.
##   ВСТАВШЕЕ  — что ответил сервер (или что приехало снапшотом). Это истина.
##
## # Почему их два, и почему это не «предсказание физики»
##
## Вертикальный срез §6 различает эти два вида предсказания прямо: «положение
## органов управления показывается локально и немедленно: дёрнул контроллер —
## рукоятка переместилась, физический отклик придёт со снапшотом. Это
## оптимистичное отображение СВОЕГО ОРГАНА УПРАВЛЕНИЯ, а не физики; смешивать
## эти два вида предсказания нельзя».
##
## Рукоятка — вещь в руке игрока: она обязана двигаться в тот же кадр, иначе
## управление ощущается вязким на всю дорогу до сервера и обратно. Скорость,
## сила и положение машины — НЕ в руке игрока, и их клиент не предсказывает
## вовсе (правило sjq: чего сервер не прислал, того на экране нет).
##
## # Пределы приходят с сервера
##
## Сколько ступеней у контроллера — свойство МАШИНЫ, и оно приезжает паспортом
## (/content, блок controls). Клиент их не выдумывает и не зашивает: своя копия
## разошлась бы с сервером при первой же второй машине, и разошлась бы молча —
## лишняя ступень просто получала бы отказ.
##
## # Откат по отказу
##
## Сервер вправе не принять положение (ступень за пределом, тяга при нулевом
## реверсоре). Тогда желаемое ОТКАТЫВАЕТСЯ к вставшему, а причина показывается
## человеку. Оставить рукоятку в желаемом положении значило бы врать глазами:
## на экране контроллер набран, на машине — нет.
class_name Cab
extends RefCounted

## Положения реверсора и их порядок при переборе. Порядок не случаен: ноль
## посередине, и «вперёд → назад» без остановки в нуле не проскочить.
const REVERSERS := ["reverse", "neutral", "forward"]


## Человеческие имена положений. Машинные строки — договор, эти — для глаз.
const REVERSER_NAMES := {
	"reverse": "назад",
	"neutral": "ноль",
	"forward": "вперёд",
}

## Чья это кабина. Пусто — игрок не в кабине, и рукояток нет.
var unit_id := ""

## Пределы из паспорта. Ноль значит «паспорт молчит», и тогда рукоятки не
## двигаются вовсе: лучше неподвижная рукоятка, чем ступень, выдуманная клиентом.
var traction_notches := 0
var brake_notches := 0
## Органы и действия, объявленные паспортом: записи с сервера (content.Organ).
## Пусто — машина без органов управления. Клиент их не выводит и не дополняет: он
## спрашивает, есть ли орган, как он называется и какой клавишей его ведут.
##
## Ключи записи: id, kind ("organ" | "action"), name, up, down (списки имён
## клавиш вида «W», «2», «shift+R»).
var organs: Array[Dictionary] = []

## Желаемое — то, что видно игроку немедленно.
var traction := 0
var brake := 0
var reverser := "neutral"
## Положение ручки крана машиниста. Пусто значит, что у машины НЕТ ТОРМОЗНОЙ
## МАГИСТРАЛИ, — тормозная система свойство машины, и «крана нет» отличается от
## «кран в первом положении». Пульт такой рукоятки не рисует, а команда её не
## несёт (LiveChannel.controls_params).
var handle := ""
## Задание крана вспомогательного тормоза, ТЫСЯЧНЫЕ кгс/см² (шкала провода).
var independent := 0
## Предел вспомогательного крана из паспорта. Ноль — крана нет.
var independent_max := 0

## Вставшее — то, что подтвердил сервер.
var set_traction := 0
var set_brake := 0
var set_reverser := "neutral"
var set_handle := ""
var set_independent := 0
## Давления, пришедшие снапшотом, ТЫСЯЧНЫЕ кгс/см². Это СЛЕДСТВИЕ, а не орган:
## выставить их нельзя, они приходят с сервера и показываются приборами.
## БУКСОВАНИЕ и ФАКТИЧЕСКАЯ позиция контроллера — с сервера. Оба следствия, а не
## органы: рукоятка несёт только своё положение, а позицию, силу и срыв выводит
## сервер (слово владельца 2026-08-15).
var slipping := false
var notch_milli := 0
var main_pressure := 0
var pipe_pressure := 0
var cylinder_pressure := 0
var has_air := false
## Шкалы приборов, ТЫСЯЧНЫЕ кгс/см², и конструкционная скорость — всё из
## паспорта. Ноль значит «у машины этого нет».
var charge_milli := 0
var cylinder_full_milli := 0
var main_max_milli := 0
var max_speed_kmh := 0.0


## _milli — кгс/см² паспорта в тысячные провода. Одно место перевода: два
## разошлись бы округлением.
static func _milli(v: Variant) -> int:
	return int(round(float(v) * 1000.0))

## pending — сколько команд ждут ответа. Больше нуля — рукоятка показана
## оптимистично, и это видно в HUD.
var pending := 0

## refusal — последний отказ сервера, человеческими словами. Пусто, если всё
## встало.
var refusal := ""


## bind — сесть в кабину машины: пределы из паспорта, положение с сервера.
func bind(unit: String, passport: Dictionary, controls: Dictionary) -> void:
	unit_id = unit
	var limits: Dictionary = (passport.get("controls", {})) as Dictionary
	traction_notches = int(limits.get("traction_notches", 0))
	brake_notches = int(limits.get("brake_notches", 0))
	# КАКИЕ ОРГАНЫ У МАШИНЫ ЕСТЬ — СПИСКОМ ИЗ ПАСПОРТА, а не выводом по пределам.
	# Слово владельца 2026-08-15: «мы передаём серверу локомотив, его паспорт,
	# возможности; клиент лишь использует их — на стороне клиента не должно быть
	# никакой информации». Правило вывода («кран есть тогда, когда есть
	# магистраль») жило и здесь, и на сервере, и разошлось: клиент держал клавиши
	# ступенчатого тормоза у машины, у которой его нет вовсе.
	organs.clear()
	for o in (passport.get("organs", []) as Array):
		organs.append(o as Dictionary)
	pending = 0
	refusal = ""
	max_speed_kmh = float(passport.get("max_speed", 0.0))
	# ШКАЛЫ ПРИБОРОВ — ИЗ ПАСПОРТА, а не из головы. Манометр, у которого предел
	# выдуман клиентом, показывает верное давление на неверной шкале, и стрелка
	# врёт ровно тем, чем врал бы сам прибор. Пустой блок air значит, что у машины
	# НЕТ МАГИСТРАЛИ: приборов и крана пульт для неё не рисует.
	var air_spec: Dictionary = ((passport.get("brake", {}) as Dictionary).get("air", {})) as Dictionary
	independent_max = _milli(air_spec.get("independent_max", 0.0))
	charge_milli = _milli(air_spec.get("charge", 0.0))
	cylinder_full_milli = _milli(air_spec.get("cylinder_full", 0.0))
	main_max_milli = _milli(air_spec.get("main_max", 0.0))
	confirm(controls)
	traction = set_traction
	brake = set_brake
	reverser = set_reverser
	handle = set_handle
	independent = set_independent


## leave — выйти из кабины.
func leave() -> void:
	unit_id = ""
	pending = 0
	refusal = ""


## aboard — в кабине ли мы.
func aboard() -> bool:
	return unit_id != ""


## confirm — принять положение, вставшее на сервере.
##
## Желаемое подтягивается к нему ТОЛЬКО когда ничего не ждём: иначе снапшот,
## пришедший между дёрганьем рукоятки и ответом на него, отбросил бы рукоятку
## назад на глазах у игрока — и он дёрнул бы её второй раз.
func confirm(controls: Dictionary) -> void:
	if controls.is_empty():
		return
	set_traction = int(controls.get("traction", 0))
	set_brake = int(controls.get("brake", 0))
	set_reverser = String(controls.get("reverser", "neutral"))
	# КРАН ПРИХОДИТ ТОЛЬКО ОТ МАШИНЫ, У КОТОРОЙ ОН ЕСТЬ. Умолчания здесь нет
	# нарочно: пустая строка означает «крана нет», и подставить «поездное» значило
	# бы нарисовать на пульте рукоятку, которой у машины не существует.
	set_handle = String(controls.get("handle", ""))
	set_independent = int(controls.get("independent", "0"))
	if pending <= 0:
		traction = set_traction
		brake = set_brake
		reverser = set_reverser
		handle = set_handle
		independent = set_independent


## air — давления снапшота. Приходят отдельно от органов, потому что это
## следствие, а не команда: приборы показывают их, но выставить их нельзя.
##
## СНИМКИ КОПЯТСЯ ДЛЯ ПОКАЗА, а не только замещают друг друга: давления приходят
## десять раз в секунду, а рисуется шестьдесят, и стрелка, показывающая последнее
## пришедшее, стоит пять кадров и прыгает на шестом. Владелец это и увидел: «на
## приборах стрелки рывками идут».
## machine — состояние машины, которое НЕ является органом управления:
## фактическая позиция контроллера и признак буксования.
func machine(unit: Dictionary) -> void:
	notch_milli = int(unit.get("notch", 0))
	slipping = bool(unit.get("slipping", false))


func air(pressures: Dictionary, at_us: int = 0) -> void:
	has_air = not pressures.is_empty()
	if not has_air:
		return
	main_pressure = int(pressures.get("main", "0"))
	pipe_pressure = int(pressures.get("pipe", "0"))
	cylinder_pressure = int(pressures.get("cylinder", "0"))
	if at_us <= 0:
		return
	if not _dial.is_empty() and int((_dial[_dial.size() - 1] as Dictionary)["t"]) == at_us:
		return
	# ПОЗИЦИЯ КОНТРОЛЛЕРА — В ТОТ ЖЕ РЯД. Она ползёт на сервере непрерывно (набор
	# по позиции в секунду), а приходит десятью снимками, и без интерполяции
	# ползунок тяги шагает десять раз в секунду при шестидесяти кадрах. Владелец
	# назвал это «стрелки на приборах рывками» — и был прав ТРЕТИЙ раз: давления я
	# сгладил, а позицию забыл, хотя она единственная, что непрерывно меняется на
	# разгоне.
	_dial.append({"t": at_us, "main": main_pressure, "pipe": pipe_pressure,
		"cyl": cylinder_pressure, "kmh": speed_kmh, "notch": notch_milli})
	while _dial.size() > 4:
		_dial.remove_at(0)


## _dial — снимки приборов: {t, main, pipe, cyl, kmh}. Четыре, как у кинематики
## положения: показу нужны два, ещё два — запас на задержавшийся снимок.
var _dial: Array = []


## shown — что показывают приборы В ЭТО МГНОВЕНИЕ модельного времени.
##
## ТО ЖЕ ПРАВИЛО, ЧТО У ПОЛОЖЕНИЯ МАШИНЫ (DisplayMotion): показываем состояние на
## «сейчас минус буфер», интерполируя между двумя снимками. Второе правило рядом
## с первым разошлось бы с ним, и стрелка отставала бы от машины — или обгоняла.
##
## Это ПОКАЗ, а не догадка о машине: между снимками мы не придумываем давление, а
## показываем то, что уже прислано, в тот момент, к которому оно относится. Тем и
## отличается от экстраполяции, которой у клиента нет (t5h §8).
func shown(now_us: int) -> Dictionary:
	var last := {"main": main_pressure, "pipe": pipe_pressure,
		"cyl": cylinder_pressure, "kmh": speed_kmh, "notch": notch_milli}
	if _dial.size() < 2 or now_us <= 0:
		return last
	var a: Dictionary = _dial[_dial.size() - 2]
	var b: Dictionary = _dial[_dial.size() - 1]
	for i in range(_dial.size() - 1):
		var x: Dictionary = _dial[i]
		var y: Dictionary = _dial[i + 1]
		if now_us >= int(x["t"]) and now_us <= int(y["t"]):
			a = x
			b = y
			break
	if now_us >= int(b["t"]):
		return {"main": b["main"], "pipe": b["pipe"], "cyl": b["cyl"], "kmh": b["kmh"],
			"notch": b["notch"]}
	if now_us <= int(a["t"]):
		return {"main": a["main"], "pipe": a["pipe"], "cyl": a["cyl"], "kmh": a["kmh"],
			"notch": a["notch"]}
	var span := float(int(b["t"]) - int(a["t"]))
	if span <= 0.0:
		return last
	var k := float(now_us - int(a["t"])) / span
	return {
		"main": lerpf(float(a["main"]), float(b["main"]), k),
		"pipe": lerpf(float(a["pipe"]), float(b["pipe"]), k),
		"cyl": lerpf(float(a["cyl"]), float(b["cyl"]), k),
		"kmh": lerpf(float(a["kmh"]), float(b["kmh"]), k),
		"notch": lerpf(float(a["notch"]), float(b["notch"]), k),
	}


## Положения крана в порядке ручки: от отпуска к экстренному. Порядок — договор
## сервера (brake.Handles), и второе написание здесь разошлось бы с ним молча.
const HANDLES := ["release", "run", "lap", "hold", "service", "emergency"]
const HANDLE_NAMES := {
	"release": "I отпуск и зарядка",
	"run": "II поездное",
	"lap": "III перекрыша без питания",
	"hold": "IV перекрыша с питанием",
	"service": "V служебное торможение",
	"emergency": "VI экстренное",
}


## shift_handle — ВЕСТИ РУЧКУ СОСЕДНИМ ПОЛОЖЕНИЕМ, а не прыгать по ней.
##
## Соседним, потому что у настоящей ручки нет способа попасть из отпуска в
## экстренное, минуя остальные, — она едет по сектору. Клавиша, ставящая
## положение сразу, дала бы машинисту то, чего у него нет.
func shift_handle(delta: int) -> bool:
	if handle == "":
		return false
	var i := HANDLES.find(handle)
	if i < 0:
		return false
	var want: int = clampi(i + delta, 0, HANDLES.size() - 1)
	if want == i:
		return false
	handle = String(HANDLES[want])
	return true


## set_handle_at — поставить ручку в названное положение. Для МЫШИ: по сектору
## крана тянут прямо, и вести её соседними положениями там незачем.
func set_handle_at(pos: String) -> bool:
	if handle == "" or not HANDLES.has(pos) or pos == handle:
		return false
	handle = pos
	return true


## ПРЯМАЯ ПОСТАНОВКА ОРГАНОВ — ДЛЯ МЫШИ. Клавиша ведёт орган соседним
## положением, мышь ставит его туда, куда указали: у ползунка и у сектора крана
## ход виден целиком, и заставлять игрока щёлкать двенадцать раз до нужной
## ступени значило бы делать мышь худшей клавиатурой.
##
## Возвращают true, только когда положение ИЗМЕНИЛОСЬ: иначе ведение мыши слало
## бы команду на каждый пиксель, а сервер отвечал бы на каждую.
func set_traction_at(want: int) -> bool:
	var v: int = clampi(want, 0, traction_notches)
	# ТЯГА ПРИ НУЛЕВОМ РЕВЕРСОРЕ — отказ сервера, и предупредить о нём здесь
	# честнее, чем послать заведомо отказную команду: рукоятка не дёрнется, а
	# причина уже написана на пульте у реверсора.
	if v > 0 and reverser == "neutral":
		return false
	if v == traction:
		return false
	traction = v
	return true


func set_reverser_at(pos: String) -> bool:
	if pos == reverser or not REVERSERS.has(pos):
		return false
	# ПОД ТЯГОЙ РЕВЕРСОР НЕ ПЕРЕВОДИТСЯ — то же правило, что у клавиши.
	if traction > 0:
		return false
	reverser = pos
	return true


func set_independent_at(want: int) -> bool:
	if independent_max <= 0:
		return false
	var v: int = clampi(want, 0, independent_max)
	if v == independent:
		return false
	independent = v
	return true


## speed_kmh — скорость машины для прибора. Приходит СНАРУЖИ (мир кладёт её из
## снапшота): у кабины нет доступа к общему мешку чисел мира, она знает органы.
var speed_kmh := 0.0


## notch_independent — кран вспомогательного тормоза ступенью в десятую долю
## предела. Своих ступеней у него нет: кран непрерывный, и десятая — шаг РУКИ, а
## не машины. Названо, чтобы не сошло за число паспорта.
func notch_independent(delta: int) -> bool:
	if independent_max <= 0:
		return false
	var stepv: int = maxi(1, independent_max / 10)
	var want: int = clampi(independent + delta * stepv, 0, independent_max)
	if want == independent:
		return false
	independent = want
	return true


## answered — сервер ответил на нашу команду.
func answered(controls: Dictionary) -> void:
	pending = maxi(0, pending - 1)
	refusal = ""
	confirm(controls)


## refused — сервер отказал. Рукоятки возвращаются туда, где они на самом деле.
func refused(text: String) -> void:
	pending = maxi(0, pending - 1)
	refusal = text
	traction = set_traction
	brake = set_brake
	reverser = set_reverser


## notch_traction — сдвинуть контроллер тяги на ступень.
##
## Возвращает true, если положение изменилось и его надо отправить. Упор в
## предел — не отказ и не сообщение: рукоятка просто не идёт дальше, как у
## настоящей машины.
func notch_traction(delta: int) -> bool:
	if not aboard():
		return false
	var want := clampi(traction + delta, 0, traction_notches)
	if want == traction:
		return false
	traction = want
	return true


## notch_brake — сдвинуть тормоз на ступень.
## has_organ — ЕСТЬ ЛИ У ЭТОЙ МАШИНЫ такой орган. Отвечает ПАСПОРТ, а не клиент.
##
## Спрашивают пульт (рисовать ли рукоятку) и разбор клавиш. Тормозная система —
## свойство машины (слово владельца: «у разных локомотивов своя тормозная
## система»), и набор органов у неё свой.
##
## Имя органа — то, что прислал сервер (content.Organ*); имя ДЕЙСТВИЯ клавиши
## отличается от него хвостом «+»/«−», и снять этот хвост — работа клавиатуры,
## которая клиенту и принадлежит.
func has_organ(action: String) -> bool:
	return not organ_of(action).is_empty()


## organ_of — запись органа по имени действия. Пустой словарь — органа нет.
func organ_of(action: String) -> Dictionary:
	var name := action
	if name.ends_with("+") or name.ends_with("-"):
		name = name.substr(0, name.length() - 1)
	for o in organs:
		if String(o.get("id", "")) == name:
			return o
	return {}


func notch_brake(delta: int) -> bool:
	if not aboard():
		return false
	var want := clampi(brake + delta, 0, brake_notches)
	if want == brake:
		return false
	brake = want
	return true


## shift_reverser — перевести реверсор на соседнее положение.
##
## ТОЛЬКО ПРИ НУЛЕВОЙ ТЯГЕ, и это не наше правило игры: реверсор настоящей
## машины переводится при выключенном контроллере, иначе рвётся дуга на
## контактах. Сервер такой команды и не примет — тяга при нулевом реверсоре ему
## запрещена, — но узнать об этом от рукоятки лучше, чем от отказа.
func shift_reverser(delta: int) -> bool:
	if not aboard() or traction != 0:
		return false
	var at := REVERSERS.find(reverser)
	if at < 0:
		at = REVERSERS.find("neutral")
	var want := clampi(at + delta, 0, REVERSERS.size() - 1)
	if want == at:
		return false
	reverser = String(REVERSERS[want])
	return true


## release_all — всё в ноль: тяга сброшена, тормоз полный, реверсор в ноль.
##
## Одно движение, потому что оно нужно в спешке. Тормоз ставится ПОЛНЫЙ, а не
## нулевой: «всё в ноль» у машиниста означает остановиться, а не покатиться.
func release_all() -> bool:
	if not aboard():
		return false
	# «ВСЁ В НОЛЬ» У МАШИНЫ С МАГИСТРАЛЬЮ — ЭТО ЭКСТРЕННОЕ, а не «полный тормоз
	# ступенью». Ступеней у неё нет, и оставить их здесь значило бы, что клавиша
	# аварийной остановки не останавливает: она ставила бы число, которое на этой
	# машине ни на что не влияет.
	var want_handle := "emergency" if has_organ("handle") else handle
	# ВСПОМОГАТЕЛЬНЫЙ ПРИ ЭТОМ ОТПУСКАЕТСЯ, А НЕ СТАВИТСЯ НА ПОЛНОЕ.
	#
	# Ставился он здесь на предельное, и это была ЛОВУШКА, а не запас нажатия.
	# Экстренное положение крана машиниста и без него даёт полный цилиндр через
	# воздухораспределитель (brake.stepCylinder берёт БОЛЬШЕЕ из двух, а не
	# сумму), так что торможению вспомогательный не прибавлял ничего. Зато он
	# ОСТАВАЛСЯ выставленным после остановки: цилиндр держал 4.0 независимо от
	# крана машиниста, и машина стояла заторможенной наглухо, сколько ни води
	# ручку по сектору. Отпустить можно было только клавишей Z, нажав её десяток
	# раз, — собственный зонд проекта (cab_probe._free_the_brakes) именно так это
	# и обходит. Владелец увидел это как «тормоза как-то странно работают».
	var want_indep := 0
	if traction == 0 and brake == brake_notches and reverser == "neutral" 			and handle == want_handle and independent == want_indep:
		return false
	traction = 0
	brake = brake_notches
	reverser = "neutral"
	handle = want_handle
	independent = want_indep
	return true


## sent — команда ушла: ждём ответа.
func sent() -> void:
	pending += 1


## text — строка кабины для HUD.
func text() -> String:
	if not aboard():
		return ""
	var wait := "" if pending <= 0 else "  [color=#ffc060](ждём сервер)[/color]"
	var line := "[b]кабина[/b]: тяга %d/%d, реверсор %s%s" % [
		traction, traction_notches, String(REVERSER_NAMES.get(reverser, reverser)), wait]
	# СТУПЕНЧАТЫЙ ТОРМОЗ ПОКАЗЫВАЕТСЯ, ТОЛЬКО ЕСЛИ ОН У МАШИНЫ ЕСТЬ. «тормоз 0/0»
	# у машины с магистралью — это прибор, показывающий орган, которого нет.
	if has_organ("brake"):
		line += ", тормоз %d/%d" % [brake, brake_notches]
	if pending > 0 and (traction != set_traction or brake != set_brake or reverser != set_reverser):
		# ДВА ЧИСЛА, КОГДА ОНИ РАЗОШЛИСЬ. Показывать только желаемое значило бы
		# скрыть, что машина ещё не приняла команду.
		line += "\n  на машине: тяга %d, тормоз %d, реверсор %s" % [
			set_traction, set_brake, String(REVERSER_NAMES.get(set_reverser, set_reverser))]
	if refusal != "":
		line += "\n  [color=#ff8080]%s[/color]" % refusal
	return line


## ЗДЕСЬ БЫЛА panel() — пульт СТРОКОЙ, живший несколько часов между жалобой
## владельца «сел в локомотив, где HUD?» и настоящим пультом (cab_panel.gd).
## Снесена вместе со своим _bar и узлом, который её показывал: рисованные приборы
## отвечают на тот же вопрос лучше, а два ответа на один вопрос — это два места,
## где они разойдутся.
