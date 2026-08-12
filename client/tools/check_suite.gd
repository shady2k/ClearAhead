## check_suite.gd — общий предок файла проверок.
##
## Даёт две вещи и больше ничего: _ok() и ctx. Всё остальное живёт в самом
## файле проверки, и это нарочно — фреймворка здесь нет и не заводится
## (решение владельца 2026-08-12: разделить без зависимости). Чужой код в
## client/addons не появляется, самодельный _ok остаётся.
##
## Наследование через путь, а не через class_name: имена CheckSuite,
## CheckContext, CheckReport — оснастка проверок, и в глобальном списке классов
## игры им делать нечего. Побочно это снимает зависимость от кэша
## global_script_class_cache.cfg, из-за отсутствия которого цели client* уже
## падали однажды (см. шапку client-import в Makefile).
extends RefCounted

const CheckContext := preload("res://tools/check_context.gd")

var ctx: CheckContext


## run — тело проверки. Может быть корутиной (await), может не быть: бегун
## зовёт через await в обоих случаях.
func run() -> void:
	push_error("check_suite: файл проверки не определил run()")


func _ok(name: String, cond: bool, detail: String = "") -> void:
	ctx.report.ok(name, cond, detail)
