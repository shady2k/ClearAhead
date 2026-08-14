## VersionSet — накопление ОДНОЙ версии мира и её атомарная фиксация.
##
## Мир переключается между версиями ЦЕЛИКОМ или никак. Сеть и патчи приезжают
## разными запросами, и между ними мир успевает поменяться: подмешивать их в
## видимый набор по одному — ровно то окно спеки §6.3, ради закрытия которого
## общая версия и заведена (бида sqym.6). Этот класс держит набор новой версии
## в стороне, пока он не готов целиком, и только тогда отдаёт его целиком.
##
## # ПРАВИЛО ПРИЁМА: ответ, назвавший не ту версию, — МУСОР
##
## Патчи разных версий не коммутативны (спека §5.1): применить патч старой
## версии к набору новой значило бы смешать два мира в одной клетке. Поэтому
## каждый ответ принимается только с версией набора; опоздавший ответ старой
## версии отбрасывается, а не применяется.
##
## # ПОВТОР ТОГО ЖЕ ОТВЕТА НИЧЕГО НЕ МЕНЯЕТ
##
## Набор хранит СОСТОЯНИЕ клетки, а не историю доставки. Патч несёт абсолютные
## отметки и идемпотентен; приём переносит идемпотентность на себя: второй раз
## тот же ответ — no-op, набор не меняется.
##
## # 204 — ПОЛНЫЙ ОТВЕТ КЛЕТКИ, А НЕ ОТКАЗ
##
## «Чистая база» (нет земляных работ на этой версии) делает набор готовым так
## же, как 200: отсутствие патча здесь — сведение о мире, а не дыра в наборе.
## Отказ (транспорт, 404) набор НЕ делает готовым: применять половину набора
## нельзя, набор помечается упавшим, и вызывающий держит старую версию.
##
## Форма ответа — утиный протокол, повторяющий WorldApi.Patch: have() — есть
## отсчёты, no_work() — чистая база, failed() — отказ с reason. Тип не
## называется, чтобы проверять набор можно было фикстурой без сети.
class_name VersionSet
extends RefCounted

## Какая версия копится. Ответ, назвавший другую, в набор не входит.
var version: int = 0

var _wanted: Array = []          # ключи клеток набора (level/cx/cz)
var _arrived := {}               # ключ клетки -> true: ответ получен
var _heights := {}               # ключ клетки -> {h, base_z, level, cx, cz}
var _clean := {}                 # ключ клетки -> true: чистая база, отсчётов нет
var _network: Dictionary = {}    # тело сети версии (JSON)
var _network_arrived := false
var _failed_reason := ""


## begin — начать копить версию v над списком клеток keys.
##
## keys — адреса УЖЕ НАХОДЯЩИХСЯ В ПАМЯТИ чанков: набор догружается для них,
## и только по их готовности видимая версия переключается.
func begin(v: int, keys: Array) -> void:
	version = v
	_wanted = keys.duplicate()
	_arrived = {}
	_heights = {}
	_clean = {}
	_network = {}
	_network_arrived = false
	_failed_reason = ""
	for k in keys:
		_arrived[k] = false


## accept_network — ответ сети набора. false значит «версия не та — мусор».
func accept_network(answer, v: int) -> bool:
	if v != version:
		return false
	if answer.failed():
		_fail(answer.reason)
		return true
	_network = answer.data as Dictionary
	_network_arrived = true
	return true


## accept_patch — ответ патча клетки key. false значит «версия не та — мусор».
##
## Повтор того же ответа — no-op: состояние клетки уже записано, и запись
## поверх не меняет набора (идемпотентность патча, спека §5.1).
func accept_patch(key: String, patch, v: int) -> bool:
	if v != version:
		return false
	if patch.failed():
		_fail(patch.reason)
		return true
	if not _arrived.has(key):
		# Ответ клетки, которой НЕТ в наборе, — нарушение протокола, а не дыра:
		# мир спрашивает ровно фиксированный список, и ответ мимо него значил
		# бы, что где-то смешались два набора. Молча принять его — согласиться
		# с этим смешением.
		_fail("ответ клетки %s вне набора версии %d" % [key, version])
		return true
	if _arrived[key]:
		return true
	_arrived[key] = true
	if patch.no_work():
		_clean[key] = true
		return true
	_heights[key] = {
		"h": patch.blob, "base_z": patch.base_z_m,
		"level": patch.level, "cx": patch.cx, "cz": patch.cz}
	return true


## fail — отказаться от набора сразу (не ходить за остальным). false — мусор.
func fail(reason: String, v: int) -> bool:
	if v != version:
		return false
	_fail(reason)
	return true


func _fail(reason: String) -> void:
	if _failed_reason == "":
		_failed_reason = reason


## ready — набор готов целиком: сеть, каждая клетка ответила, отказов нет.
func ready() -> bool:
	if _failed_reason != "" or not _network_arrived:
		return false
	for k in _wanted:
		if not _arrived[k]:
			return false
	return true


func failed() -> bool:
	return _failed_reason != ""


func failed_reason() -> String:
	return _failed_reason


## commit — ОТДАТЬ набор целиком. Зовётся только когда ready().
##
## Возвращает то, что вызывающий обязан применить ОДНИМ шагом:
##   version — какая версия набора;
##   network — тело сети;
##   heights — ключ клетки -> {h, base_z, level, cx, cz}: отсчёты версии;
##   clean   — ключ клетки -> true: чистая база (204), отсчётов нет.
## Элементы сети набор не несёт: разбор сети — дело мира (этот класс про
## состояние набора, а не про контракт сети).
func commit() -> Dictionary:
	return {"version": version, "network": _network,
		"heights": _heights, "clean": _clean}
