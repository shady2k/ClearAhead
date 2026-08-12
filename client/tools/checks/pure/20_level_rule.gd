## ПРАВИЛО УРОВНЯ — арифметика на границах кругов, а не в серединке.
##
## Сети здесь делать нечего: числа правила приходят фикстурой кодом
## (CheckContext.RULE_FIXTURE), и проверяется ChunkRule, а не то, что сервер
## сегодня прислал 512. До разноса 2026-08-12 эти пять проверок требовали
## поднятого сервера ровно потому, что r0 и max_level брались из живого
## манифеста.
##
## ГРАНИЦА ВКЛЮЧЕНА В СВОЙ УРОВЕНЬ (d <= r), как у сервера (chunk.LevelFor).
## Проверка стоит здесь именно ради этого равенства: до 2026-08-12 у клиента
## было строгое `d < r`, и на самой окружности он спрашивал уровень грубее
## серверного.
extends "res://tools/check_suite.gd"


func run() -> void:
	var rule := await ctx.rule()
	_ok("level_for(0)==0", rule.level_for(0.0) == 0)
	_ok("level_for(r0)==0 — граница принадлежит своему уровню",
		rule.level_for(rule.level0_radius_m) == 0)
	_ok("level_for(r0+ε)==1", rule.level_for(rule.level0_radius_m + 0.001) == 1)
	_ok("level_for(последний радиус)==max_level",
		rule.level_for(rule.radius_of(rule.max_level)) == rule.max_level)
	_ok("level_for(за последним уровнем)==-1",
		rule.level_for(rule.radius_of(rule.max_level) + 1.0) == -1)
