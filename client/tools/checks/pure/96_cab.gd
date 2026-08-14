## КАБИНА — чистая проверка: арифметика рукояток и договор команды.
##
## Сети здесь нет вовсе, и это правильное место: всё, что проверяется ниже, —
## решения КЛИЕНТА о том, что показать игроку до ответа сервера. Живой разговор
## (команда ушла, положение встало, отказ вернулся) проверяет
## checks/live/15_channel.gd.
##
## # Зачем вообще проверять оптимистичный показ
##
## Потому что он единственное место клиента, где на экране может оказаться то,
## чего на сервере нет. Правило sjq («чего сервер не прислал, того на экране
## нет») здесь не нарушено и не ослаблено: рукоятка — вещь в руке игрока, а не
## факт о мире, и вертикальный срез §6 различает их прямо. Но раз уж расхождение
## разрешено, оно обязано быть КОРОТКИМ и ВИДИМЫМ: отказ откатывает рукоятку,
## снапшот подтверждает, а пока ждём — на экране видно оба числа.
extends "res://tools/check_suite.gd"

const ContractDoc := preload("res://tools/contract_doc.gd")

## Пределы фикстурой: те же числа, что у боевого ВЛ80 (33 ступени ЭКГ-8Ж и пять
## наших ступеней торможения). Связаны они с паспортом не файлом, а этой
## строкой: проверяется арифметика рукояток, а не то, что сервер сегодня прислал.
const LIMITS := {"traction_notches": 33, "brake_notches": 5}
const STOPPED := {"traction": 0, "brake": 0, "reverser": "neutral"}


func run() -> void:
	_check_notches()
	_check_reverser()
	_check_optimism()
	_check_command_matches_contract()


func _bound() -> Cab:
	var cab := Cab.new()
	cab.bind("U1", LIMITS, STOPPED)
	return cab


func _check_notches() -> void:
	var cab := _bound()
	_ok("до посадки рукояток нет", not Cab.new().aboard())
	_ok("в кабине рукоятки есть", cab.aboard())

	_ok("ступень тяги набирается", cab.notch_traction(1) and cab.traction == 1)
	_ok("ступень тяги сбрасывается", cab.notch_traction(-1) and cab.traction == 0)
	# УПОР В НОЛЬ — НЕ СОБЫТИЕ: рукоятка просто не идёт дальше, как у настоящей
	# машины, и слать серверу нечего.
	_ok("ниже нуля тяга не идёт", not cab.notch_traction(-1) and cab.traction == 0)

	for i in range(LIMITS["traction_notches"] + 5):
		cab.notch_traction(1)
	_ok("выше паспортного предела тяга не идёт",
		cab.traction == LIMITS["traction_notches"], str(cab.traction))

	cab.release_all()
	_ok("«всё в ноль» сбрасывает тягу", cab.traction == 0)
	# Тормоз ставится ПОЛНЫЙ: «всё в ноль» у машиниста означает остановиться, а
	# не покатиться.
	_ok("«всё в ноль» ставит полный тормоз", cab.brake == LIMITS["brake_notches"], str(cab.brake))
	_ok("«всё в ноль» ставит реверсор в ноль", cab.reverser == "neutral", cab.reverser)
	_ok("повторное «всё в ноль» ничего не меняет", not cab.release_all())

	var brakes := _bound()
	for i in range(LIMITS["brake_notches"] + 3):
		brakes.notch_brake(1)
	_ok("выше паспортного предела тормоз не идёт",
		brakes.brake == LIMITS["brake_notches"], str(brakes.brake))


func _check_reverser() -> void:
	var cab := _bound()
	_ok("реверсор идёт вперёд", cab.shift_reverser(1) and cab.reverser == "forward")
	_ok("реверсор возвращается в ноль", cab.shift_reverser(-1) and cab.reverser == "neutral")
	_ok("реверсор идёт назад", cab.shift_reverser(-1) and cab.reverser == "reverse")
	# ЧЕРЕЗ НОЛЬ НЕ ПРОСКОЧИТЬ: порядок положений задан списком, и «вперёд →
	# назад» проходит через остановку.
	_ok("из «назад» дальше некуда", not cab.shift_reverser(-1))

	# РЕВЕРСОР НЕ ПЕРЕВОДИТСЯ ПОД ТЯГОЙ, и это не наше правило игры: у настоящей
	# машины реверсор переводят при выключенном контроллере, иначе рвётся дуга.
	# Сервер такую команду и не примет (traction_without_reverser), но узнать об
	# этом от рукоятки лучше, чем от отказа.
	var under_load := _bound()
	under_load.shift_reverser(1)
	under_load.notch_traction(3)
	_ok("под тягой реверсор не переводится", not under_load.shift_reverser(-1))
	_ok("положение реверсора при этом не изменилось", under_load.reverser == "forward")


## ОПТИМИСТИЧНЫЙ ПОКАЗ и три его обязанности: показать немедленно, не спорить со
## снапшотом, откатиться на отказе.
func _check_optimism() -> void:
	var cab := _bound()
	cab.shift_reverser(1)
	cab.notch_traction(5)
	cab.sent()
	_ok("рукоятка показана немедленно", cab.traction == 5, str(cab.traction))
	_ok("на машине пока прежнее", cab.set_traction == 0, str(cab.set_traction))
	_ok("ожидание ответа видно в строке", cab.text().contains("ждём сервер"), cab.text())
	# Пока ждём ответа, снапшот НЕ отбрасывает рукоятку назад: иначе игрок
	# увидел бы, как его движение отменяют, и дёрнул бы рукоятку второй раз.
	cab.confirm(STOPPED)
	_ok("снапшот не спорит с ждущей рукояткой", cab.traction == 5, str(cab.traction))
	_ok("но истину о машине показывает", cab.set_traction == 0, cab.text())

	cab.answered({"traction": 5, "brake": 0, "reverser": "forward"})
	_ok("после ответа положение сошлось", cab.traction == 5 and cab.set_traction == 5)
	_ok("ожидание кончилось", not cab.text().contains("ждём сервер"))

	# ОТКАЗ ОТКАТЫВАЕТ. Оставить рукоятку в желаемом положении значило бы врать
	# глазами: на экране контроллер набран, на машине — нет.
	cab.notch_traction(4)
	cab.sent()
	cab.refused("ступень тяги 9, у типа VL80 их 33")
	_ok("отказ вернул рукоятку туда, где машина", cab.traction == 5, str(cab.traction))
	_ok("причина отказа видна человеку", cab.text().contains("ступень тяги 9"), cab.text())

	# ПОСЛЕ ОТКАЗА СНАПШОТ СНОВА ГЛАВНЫЙ: ожидание кончилось, и рукоятка встаёт
	# туда, куда её поставил сервер, даже если игрок ждал другого.
	cab.confirm({"traction": 2, "brake": 1, "reverser": "forward"})
	_ok("снапшот двигает рукоятку, когда ничего не ждём", cab.traction == 2, str(cab.traction))


## КОМАНДА СВЕРЯЕТСЯ С ДОГОВОРОМ — вторая сторона на стороне клиента.
##
## Проверяется ровно то, что клиент ОТПРАВЛЯЕТ (LiveChannel.controls_params), а
## не собранный здесь пересказ: пересказ согласился бы с клиентом в любой общей
## ошибке.
func _check_command_matches_contract() -> void:
	var doc = ContractDoc.load_channel()
	_ok("договор канала прочитан", not doc.failed(), doc.reason)
	if doc.failed():
		return
	_ok("команда органов объявлена договором",
		doc.methods.has(LiveChannel.CONTROLS_METHOD), str(doc.methods.keys()))
	if not doc.methods.has(LiveChannel.CONTROLS_METHOD):
		return
	var decl := doc.methods[LiveChannel.CONTROLS_METHOD] as Dictionary
	var params := LiveChannel.controls_params("c1", "U1", 7, 0, "forward")
	var bad: String = doc.validate(String(decl.get("params", "")), params)
	_ok("params команды сходятся с договором", bad == "", bad)

	# И ответ разбирается тем же договором: клиент читает controls из него, и
	# поле, переименованное на сервере, обязано падать здесь, а не молча
	# оставлять рукоятку на нуле.
	var result: Variant = doc.sample(String(decl.get("result", "")), {
		"unit": "U1", "traction": 7, "brake": 0, "reverser": "forward",
	})
	var bad_result: String = doc.validate(String(decl.get("result", "")), result)
	_ok("образец ответа сходится с договором", bad_result == "", bad_result)
	var cab := _bound()
	cab.sent()
	cab.answered((result as Dictionary).get("controls", {}) as Dictionary)
	_ok("клиент разобрал ответ команды", cab.set_traction == 7 and cab.set_reverser == "forward",
		cab.text())
