## ПОСТАНОВКА ЕДИНИЦЫ НА ПУТЬ — непрерывна и не выходит за элемент.
##
## Чистая по природе: серверу здесь делать нечего, нужны ЧИСЛА элементов, и они
## приходят снимком ответа (fixtures/network_ST_A.json).
##
## # Что здесь закрепляется, и чем это куплено
##
## RollingStock._stand кладёт кузов хордой между шкворнями, а Element.pose_at
## прижимает u к [0, length]. Пока хорда не сжималась, у конца элемента один
## шкворень упирался в границу, второй — нет, и СЕРЕДИНА кузова уезжала к концу.
##
## ЗАМЕР, которым это поймано (make cab-probe, отладочный сторож рывка): при
## переходе E_MAIN → SW1:straight середина прыгала на 12.576 / 12.742 / 13.193 /
## 13.271 м ЗА ОДИН КАДР при полубазе 12.35 м. На стрелке — всегда: проход ~33 м,
## машина 32.8 м.
##
## Две проверки ниже — это ровно два свойства, которых тогда не было:
##
##   НЕПРЕРЫВНОСТЬ ВНУТРИ ЭЛЕМЕНТА: шаг по u даёт шаг по миру того же порядка.
##   ВЫРОЖДЕНИЕ НА КОНЦАХ: при u = 0 и u = length постановка совпадает с точкой
##   оси. Это и есть то, что склеивает соседние элементы: с обеих сторон стыка
##   машина ставится в одну и ту же точку, и прыжка на границе нет по построению.
extends "res://tools/check_suite.gd"

## База шкворней ВЛ80 из паспорта набора. Взята числом, а не из /content:
## проверка чистая, сервера у неё нет. Разойдётся с паспортом — разойдётся в
## сторону БОЛЬШЕЙ базы, то есть более строгой проверки.
const BASE_M := 24.7

## Шаг обхода. 0.25 м — мельче полубазы на два порядка: сжатие хорды у конца
## обязано быть видно многими точками, а не двумя.
const STEP_M := 0.25


func run() -> void:
	var elements := await ctx.elements()
	if elements.is_empty():
		return

	var worst_jump := 0.0
	var worst_where := ""
	var worst_end := 0.0
	var worst_end_where := ""
	for el in elements:
		if el.length_m <= STEP_M:
			continue
		# НЕПРЕРЫВНОСТЬ. Порог — тройной шаг: на кривой хорда доворачивает кузов,
		# и шаг по миру чуть больше шага по u. Двенадцать метров он ловит с
		# запасом в полсотни раз, а законную неровность дуги не красит.
		var prev := RollingStock._stand(el, 0.0, BASE_M, false).origin
		var u := STEP_M
		while u <= el.length_m:
			var now := RollingStock._stand(el, u, BASE_M, false).origin
			var jump := prev.distance_to(now)
			if jump > worst_jump:
				worst_jump = jump
				worst_where = "%s при u = %.2f м" % [el.name if el.name != "" else el.id, u]
			prev = now
			u += STEP_M

		# ВЫРОЖДЕНИЕ НА КОНЦАХ. Хорда сжимается до нуля, и середина кузова
		# садится ровно на точку оси. Допуск 1e-6 м — микрометр, объявленная
		# разрешающая способность мира; байт в байт float не сверяется правилом
		# проекта.
		for u_end in [0.0, el.length_m]:
			var p := el.pose_at(u_end)
			var axis := TerrainMesh.to_godot(p.x, p.y, p.z)
			var stood := RollingStock._stand(el, u_end, BASE_M, false).origin
			var off := axis.distance_to(stood)
			if off > worst_end:
				worst_end = off
				worst_end_where = "%s при u = %.2f м" % [el.name if el.name != "" else el.id, u_end]

	_ok("постановка непрерывна вдоль элемента", worst_jump < STEP_M * 3.0,
		"худший шаг %.4f м при шаге по оси %.2f м (%s)" % [worst_jump, STEP_M, worst_where])
	_ok("на концах элемента постановка садится на ось", worst_end < 1e-6,
		"худшее отклонение %.9f м (%s)" % [worst_end, worst_end_where])
