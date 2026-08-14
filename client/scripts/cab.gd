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

## Желаемое — то, что видно игроку немедленно.
var traction := 0
var brake := 0
var reverser := "neutral"

## Вставшее — то, что подтвердил сервер.
var set_traction := 0
var set_brake := 0
var set_reverser := "neutral"

## pending — сколько команд ждут ответа. Больше нуля — рукоятка показана
## оптимистично, и это видно в HUD.
var pending := 0

## refusal — последний отказ сервера, человеческими словами. Пусто, если всё
## встало.
var refusal := ""


## bind — сесть в кабину машины: пределы из паспорта, положение с сервера.
func bind(unit: String, limits: Dictionary, controls: Dictionary) -> void:
	unit_id = unit
	traction_notches = int(limits.get("traction_notches", 0))
	brake_notches = int(limits.get("brake_notches", 0))
	pending = 0
	refusal = ""
	confirm(controls)
	traction = set_traction
	brake = set_brake
	reverser = set_reverser


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
	if pending <= 0:
		traction = set_traction
		brake = set_brake
		reverser = set_reverser


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
	if traction == 0 and brake == brake_notches and reverser == "neutral":
		return false
	traction = 0
	brake = brake_notches
	reverser = "neutral"
	return true


## sent — команда ушла: ждём ответа.
func sent() -> void:
	pending += 1


## text — строка кабины для HUD.
func text() -> String:
	if not aboard():
		return ""
	var wait := "" if pending <= 0 else "  [color=#ffc060](ждём сервер)[/color]"
	var line := "[b]кабина[/b]: тяга %d/%d, тормоз %d/%d, реверсор %s%s" % [
		traction, traction_notches, brake, brake_notches,
		String(REVERSER_NAMES.get(reverser, reverser)), wait]
	if pending > 0 and (traction != set_traction or brake != set_brake or reverser != set_reverser):
		# ДВА ЧИСЛА, КОГДА ОНИ РАЗОШЛИСЬ. Показывать только желаемое значило бы
		# скрыть, что машина ещё не приняла команду.
		line += "\n  на машине: тяга %d, тормоз %d, реверсор %s" % [
			set_traction, set_brake, String(REVERSER_NAMES.get(set_reverser, set_reverser))]
	if refusal != "":
		line += "\n  [color=#ff8080]%s[/color]" % refusal
	return line
