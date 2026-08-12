## check_report.gd — счёт проверок и АДРЕС ОТКАЗА.
##
## Отдельный объект, а не пара переменных в бегуне, ровно ради одного: отказ
## обязан называть ФАЙЛ, в котором живёт. Пока проверки лежали в одном файле,
## имени проверки хватало; после разноса по каталогу «ОТКАЗ ширина полосы =
## width» не говорит, куда идти смотреть.
##
## Отказ ОДНОЙ проверки не прекращает прогон — это свойство было и до разноса,
## и оно здесь сохранено: ok() только записывает. Прекращает прогон бегун, и
## только когда дальше нечего проверять (нет манифеста — нет ни одного числа).
extends RefCounted

## Сколько проверок сделано и какие отказали. Отказ хранится уже с адресом.
var checks := 0
var failures: Array[String] = []

## ОТКАЗЫ УСТРОЙСТВА, а не проверки: не загрузился файл, суита не дала ни одной
## проверки, каталог пуст. Считаются отдельно нарочно — они не проверяют мир, но
## обязаны давать ненулевой код возврата. Иначе разнос по файлам покупает новую
## беду взамен старой: проверка, тихо исчезнувшая вместе с опечаткой в имени
## файла, выглядит как зелёный прогон.
var infra_failures: Array[String] = []

## Замер по суитам — то, ради чего затевался разнос: видно, что стоит дорого.
var suites: Array[Dictionary] = []

var _suite := ""
var _suite_checks := 0
var _suite_failed := 0
var _suite_t0 := 0


func begin(path: String) -> void:
	_suite = path
	_suite_checks = 0
	_suite_failed = 0
	_suite_t0 = Time.get_ticks_usec()
	print("--- %s" % path)


func end() -> void:
	var ms := float(Time.get_ticks_usec() - _suite_t0) / 1000.0
	suites.append({"path": _suite, "checks": _suite_checks, "failed": _suite_failed, "ms": ms})
	if _suite_checks == 0:
		# Суита без единой проверки — почти всегда опечатка в имени файла или
		# ранний return по несобранным данным. Молчать об этом нельзя: счёт
		# проверок просто уменьшится, а прогон останется зелёным.
		infra("%s: ни одной проверки не сделано" % _suite)
	_suite = ""


func ok(name: String, cond: bool, detail: String = "") -> void:
	checks += 1
	_suite_checks += 1
	if cond:
		print("  ok   %s %s" % [name, detail])
	else:
		_suite_failed += 1
		failures.append("%s: %s %s" % [_suite, name, detail])
		print("  ОТКАЗ %s %s" % [name, detail])


func infra(message: String) -> void:
	infra_failures.append(message)
	print("  СБОЙ УСТРОЙСТВА %s" % message)


## finish — итог и КОД ВОЗВРАТА. На нём стоит make, поэтому ненулевой он при
## любом отказе, включая отказ устройства.
func finish(title: String, ms: float) -> int:
	# Отказы и сбои устройства считаются РАЗДЕЛЬНО: «проверок 1, отказов 2»
	# читается как арифметическая ошибка. Оба дают ненулевой код возврата.
	var infra_tail := "" if infra_failures.is_empty() else ", сбоев устройства %d" % infra_failures.size()
	print("=== %s: проверок %d, отказов %d%s, %.0f мс ===" % [
		title, checks, failures.size(), infra_tail, ms])
	for s_raw in suites:
		var s: Dictionary = s_raw
		print("  %5d проверок  %7.0f мс  %s%s" % [s["checks"], s["ms"], s["path"],
			"" if int(s["failed"]) == 0 else "  ОТКАЗОВ %d" % int(s["failed"])])
	for f in failures:
		print("  ОТКАЗ: %s" % f)
	for f in infra_failures:
		print("  СБОЙ УСТРОЙСТВА: %s" % f)
	return 0 if failures.is_empty() and infra_failures.is_empty() else 1
