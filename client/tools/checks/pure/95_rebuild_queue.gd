## ОЧЕРЕДЬ РАБОТ ПОД БЮДЖЕТОМ (sqym.7) — бюджет, прерывание, приоритет, отчёт.
##
## Проверяются четыре поведения брифа W6-A, и каждое — на фикстурном времени:
## очередь принимает часы извне, а шаг задания ДВИГАЕТ их, как реальная работа
## движет реальные часы. Проверка видит превышение бюджета, не дожидаясь
## реальной миллисекунды:
##
##   • превышение бюджета ЛОВИТСЯ, а не проходит молча: шаг, съевший кадр
##     целиком, назван в отчёте по имени — закончился он или нет;
##   • работа раскладывается по кадрам: задание, не законченное за кадр,
##     продолжается со следующего, курсор живёт в состоянии задания;
##   • приоритет пересчитывается по ходу: задание для клетки за спиной уступает
##     видимой, а вытесненное задание возобновляется со своего курсора;
##   • брошенное задание не доделывается: его работа выбрасывается целиком.
##
## Шаги — счётчики ячеек: задание обрабатывает список «ячеек» по одной за вызов,
## кладя их в общий словарь. Это та же форма, какой мир строит меши и сажает
## траву, — без сцены и без сети.
extends "res://tools/check_suite.gd"

const RebuildQueueScript := preload("res://scripts/rebuild_queue.gd")


## Фикстурные часы: время движется только когда шаг задания толкает его.
class FakeClock:
	var now := 0

	func tick(us: int) -> void:
		now += us

	func read() -> int:
		return now


## Задание-счётчик. Курсор лежит В СОСТОЯНИИ задания (state["cursor"]) — ровно
## как у настоящих продолжаемых заданий: всё, что переживает границу кадра,
## обязано переживать её в state, и проверка это доказывает. Каждый вызов шага
## стоит cost_us фикстурного времени и движет часы.
class CellJob:
	var cells: Array = []
	var per_call := 1
	var cost_us := 1000
	var clock: FakeClock
	var out := {}
	var step_calls := 0

	func step(_budget_us: int, _t0: int, state: Dictionary) -> bool:
		step_calls += 1
		if clock != null:
			clock.tick(cost_us)
		var cursor: int = int(state.get("cursor", 0))
		var n := mini(per_call, cells.size() - cursor)
		for k in n:
			out[cells[cursor]] = true
			cursor += 1
		state["cursor"] = cursor
		return cursor >= cells.size()


func run() -> void:
	var clock := FakeClock.new()
	var q := RebuildQueueScript.new(Callable(clock, "read"))

	# --- БЮДЖЕТ: ЗА КАДР УХОДИТ НЕ БОЛЬШЕ ОБЪЯВЛЕННОГО ----------------------
	var j1 := CellJob.new()
	j1.cells = ["a1", "a2", "a3", "a4", "a5", "a6", "a7", "a8"]
	j1.per_call = 2
	j1.cost_us = 1000
	j1.clock = clock
	q.push("job1", Callable(j1, "step"), 0.0, {})
	# Кадр в 3 мс: по два шага на миллисекунду — три шага (шесть ячеек), на
	# четвёртом бюджет кончается.
	var budget_us := 3000
	var frame1: Dictionary = q.work(budget_us)
	_ok("за кадр ушло не больше бюджета",
		int(frame1["spent_us"]) <= budget_us and int(frame1["done"]) == 0,
		"потрачено %d из %d" % [frame1["spent_us"], budget_us])
	_ok("незаконченное задание осталось в очереди", q.pending() == 1)
	_ok("сделана часть ячеек, а не все",
		j1.out.size() == 6 and j1.out.has("a1") and not j1.out.has("a8"))
	# Второй кадр — задание продолжается С ТОГО ЖЕ МЕСТА: курсор в state.
	clock.tick(budget_us)
	var frame2: Dictionary = q.work(budget_us)
	_ok("задание закончилось на втором кадре",
		int(frame2["done"]) == 1 and q.pending() == 0,
		"сделано %d, осталось %d" % [frame2["done"], q.pending()])
	_ok("дозделаны ровно оставшиеся ячейки, без повторов",
		j1.out.size() == 8 and j1.out.has("a8"))
	_ok("повторных шагов не было: ячейки не считались дважды", j1.step_calls == 4)

	# --- ПРЕВЫШЕНИЕ ЛОВИТСЯ: ШАГ, СЪЕВШИЙ ВЕСЬ КАДР, НАЗВАН ------------------
	var q2 := RebuildQueueScript.new(Callable(clock, "read"))
	# Шаг сам жуёт больше бюджета (толкает часы на 20 мс при бюджете 8 мс) — как
	# реальный меш, который не удалось нарезать мельче. Очередь обязана назвать
	# его, а не промолчать. Задание при этом ЗАКОНЧИЛОСЬ — превышение всё равно
	# называется: грубая зернистость не прячется за успехом.
	var heavy_step := func(_b: int, _t0: int, _s: Dictionary) -> bool:
		clock.tick(20000)
		return true
	q2.push("heavy", heavy_step, 0.0, {})
	var heavy_res: Dictionary = q2.work(8000)
	_ok("грубый шаг назван в отчёте превышением",
		(heavy_res["over_budget"] as Array).has("heavy"),
		"превысили: %s" % str(heavy_res["over_budget"]))
	_ok("задание при этом закончено, очередь пуста", q2.pending() == 0)

	# --- ПРИОРИТЕТ: КЛЕТКА ЗА СПИНОЙ УСТУПАЕТ ВИДИМОЙ ------------------------
	var far := CellJob.new()
	far.cells = ["f1", "f2", "f3"]
	far.per_call = 1
	far.cost_us = 1000
	far.clock = clock
	var near := CellJob.new()
	near.cells = ["n1", "n2", "n3"]
	near.per_call = 1
	near.cost_us = 1000
	near.clock = clock
	var q3 := RebuildQueueScript.new(Callable(clock, "read"))
	q3.push("far", Callable(far, "step"), 500.0, {})
	q3.push("near", Callable(near, "step"), 10.0, {})
	# Камера двинулась: дальняя клетка оказалась ближе, ближняя — за спиной.
	# Пересчёт приоритетов по НОВЫМ расстояниям переставляет очередь.
	var prio := {"far": 10.0, "near": 500.0}
	q3.reprioritize_all(func(job): return float(prio.get(String(job["id"]), 1e9)))
	# Кадр на одну ячейку: сделать успеет только фронт очереди.
	q3.work(1000)
	_ok("после пересчёта видимая клетка строится раньше ушедшей за спину",
		far.out.has("f1") and not near.out.has("n1"),
		"far: %s, near: %s" % [str(far.out), str(near.out)])

	# --- ВЫТЕСНЕНИЕ ТЕКУЩЕГО: КУРСОР СОХРАНЯЕТСЯ -----------------------------
	var slow := CellJob.new()
	slow.cells = ["s1", "s2", "s3", "s4"]
	slow.per_call = 1
	slow.cost_us = 1000
	slow.clock = clock
	var urgent := CellJob.new()
	urgent.cells = ["u1"]
	urgent.per_call = 1
	urgent.cost_us = 1000
	urgent.clock = clock
	var q4 := RebuildQueueScript.new(Callable(clock, "read"))
	q4.push("slow", Callable(slow, "step"), 1.0, {})
	# Кадр 1: slow делает одну ячейку и остаётся текущим.
	q4.work(1000)
	_ok("медленное задание начало работу и осталось текущим",
		slow.out.has("s1") and q4.pending() == 1)
	# Камера прыгнула: приоритет slow упал, появилась срочная работа.
	q4.push("urgent", Callable(urgent, "step"), 0.0, {})
	q4.reprioritize_all(func(job): return 0.0 if String(job["id"]) == "urgent" else 100.0)
	q4.work(1000)
	_ok("срочная работа сделана первой после вытеснения", urgent.out.has("u1"))
	_ok("вытесненное задание продолжилось со своего курсора",
		slow.out.has("s1") and not slow.out.has("s2"))
	_ok("вытесненное задание доделалось без повтора",
		int(q4.work(1 << 30)["done"]) == 1 and slow.out.has("s4") and slow.out.size() == 4)

	# --- ОТМЕНА: БРОШЕННОЕ НЕ ДОДЕЛЫВАЕТСЯ -----------------------------------
	var doomed := CellJob.new()
	doomed.cells = ["d1", "d2", "d3"]
	doomed.per_call = 1
	doomed.cost_us = 1000
	doomed.clock = clock
	var q5 := RebuildQueueScript.new(Callable(clock, "read"))
	q5.push("doomed", Callable(doomed, "step"), 0.0, {})
	q5.work(1000)          # первая ячейка сделана, задание в руках
	q5.cancel("doomed")
	_ok("отменённое задание исчезло из очереди", q5.pending() == 0)
	q5.work(1 << 30)
	_ok("отменённое задание не доделалось",
		doomed.out.size() == 1 and not doomed.out.has("d2"))
