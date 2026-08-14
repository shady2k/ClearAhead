## contract_doc.gd — ДОГОВОР ПРОВОДА, ВТОРАЯ СТОРОНА.
##
## Читает contract/channel.v1.json — тот же файл, с которым сверяется сервер
## (server/internal/contract), — и сверяет с ним то, что видит клиент.
##
## # Почему это вообще нужно
##
## Прежний эталон контракта (contract/render_geometry.golden.json) снесён
## 2026-08-11 по двум причинам, и вторая записана дословно: он «остался без
## второй стороны». Сверка, которую делает только сервер, доказывает согласие
## сервера с самим собой. Настоящее согласие — это когда обе реализации читают
## ОДНО объявление и каждая проверяет по нему СВОЙ провод.
##
## Первая причина сноса тоже учтена, и учтена выбором предмета: договор говорит
## про ИМЕНА и ВИДЫ, а не про числа. Сверка вычисленных float64 байт в байт не
## переживает смены машины — проект потерял на этом эталон при расхождении ~1e-13
## на неизменном коде.
##
## # Почему не JSON Schema
##
## Её пришлось бы реализовать здесь целиком — готового валидатора в Godot нет, а
## зависимость в клиенте стоит дороже, чем в сервере. Словарь видов ниже —
## восемь штук, и он ровно тот же, что на сервере: восемьдесят строк здесь
## против стандарта, из которого нужна одна десятая.
##
## ЦЕНА НАЗВАНА: договор не выражает ни диапазонов, ни перечислений, ни
## зависимостей между полями. Понадобится — доказательством станет случай,
## который словарь не выразил, а не рассуждение о полноте.
##
## # Где лежит файл и почему не в res://
##
## В КОРНЕ РЕПОЗИТОРИЯ, вне проекта Godot: файл, лежащий у одной из сторон, эта
## сторона рано или поздно начнёт править под себя, не спросив вторую. Отсюда
## globalize_path — единственный способ выйти за res:// честно.
extends RefCounted

## Путь к договору относительно корня проекта клиента.
const CHANNEL_CONTRACT := "../contract/channel.v1.json"

var name := ""
var protocol_version := 0
var path := ""
var types: Dictionary = {}
var methods: Dictionary = {}
var notifications: Dictionary = {}
var errors: Dictionary = {}
var refusal_data := ""
var refusal_reasons: Array = []

## reason — почему договор не прочитан. Пусто, если всё в порядке.
var reason := ""


func failed() -> bool:
	return reason != ""


## load_channel — прочитать договор канала.
static func load_channel() -> Object:
	var doc = new()
	var abs := ProjectSettings.globalize_path("res://").path_join(CHANNEL_CONTRACT)
	if not FileAccess.file_exists(abs):
		doc.reason = "договор не найден: %s" % abs
		return doc
	var text := FileAccess.get_file_as_string(abs)
	var parsed: Variant = JSON.parse_string(text)
	if typeof(parsed) != TYPE_DICTIONARY:
		doc.reason = "договор не разбирается как объект JSON: %s" % abs
		return doc
	var d := parsed as Dictionary
	doc.name = String(d.get("contract", ""))
	doc.protocol_version = int(d.get("protocol_version", 0))
	doc.path = String(d.get("path", ""))
	doc.types = d.get("types", {}) as Dictionary
	doc.methods = d.get("methods", {}) as Dictionary
	doc.notifications = d.get("notifications", {}) as Dictionary
	doc.errors = d.get("errors", {}) as Dictionary
	doc.refusal_data = String(d.get("refusal_data", ""))
	doc.refusal_reasons = d.get("refusal_reasons", []) as Array
	if doc.types.is_empty():
		doc.reason = "договор без объявленных видов"
	return doc


## validate — сверить значение с объявленным видом.
##
## Возвращает пустую строку, если сошлось, и причину расхождения иначе. Строкой,
## а не bool: «не сошлось» без указания места ищется глазами по всему телу.
func validate(kind: String, value: Variant, place: String = "корень") -> String:
	var optional := kind.ends_with("?")
	var want := kind.trim_suffix("?")
	if value == null:
		return "" if optional else "%s: пусто, ожидался %s" % [place, want]
	if want.begins_with("[]"):
		if typeof(value) != TYPE_ARRAY:
			return "%s: ожидался массив %s, пришло %s" % [place, want.substr(2), _kind_of(value)]
		var item_kind := want.substr(2)
		var i := 0
		for item in (value as Array):
			var bad := validate(item_kind, item, "%s[%d]" % [place, i])
			if bad != "":
				return bad
			i += 1
		return ""
	match want:
		"string":
			if typeof(value) != TYPE_STRING and typeof(value) != TYPE_STRING_NAME:
				return "%s: ожидалась строка, пришло %s" % [place, _kind_of(value)]
			return ""
		"bool":
			if typeof(value) != TYPE_BOOL:
				return "%s: ожидался bool, пришло %s" % [place, _kind_of(value)]
			return ""
		"float":
			if typeof(value) != TYPE_FLOAT and typeof(value) != TYPE_INT:
				return "%s: ожидалось число, пришло %s" % [place, _kind_of(value)]
			return ""
		"int", "uint":
			if typeof(value) != TYPE_FLOAT and typeof(value) != TYPE_INT:
				return "%s: ожидалось целое, пришло %s" % [place, _kind_of(value)]
			var num := float(value)
			if num != floor(num):
				return "%s: ожидалось целое, пришло %s" % [place, str(num)]
			if want == "uint" and num < 0.0:
				return "%s: ожидалось неотрицательное, пришло %s" % [place, str(num)]
			return ""
		"int_string":
			if typeof(value) != TYPE_STRING:
				return "%s: ожидалось целое СТРОКОЙ (правило провода), пришло %s" % [place, _kind_of(value)]
			if not (value as String).is_valid_int():
				return "%s: %s не целое в строке" % [place, value]
			return ""
	if not types.has(want):
		return "%s: вид %s в договоре не объявлен" % [place, want]
	if typeof(value) != TYPE_DICTIONARY:
		return "%s: ожидался объект %s, пришло %s" % [place, want, _kind_of(value)]
	var obj := value as Dictionary
	var fields := types[want] as Dictionary
	for field_name in fields:
		var decl := String(fields[field_name])
		if not obj.has(field_name):
			if decl.ends_with("?"):
				continue
			return "%s: нет обязательного поля %s (%s)" % [place, field_name, decl]
		var bad := validate(decl, obj[field_name], "%s.%s" % [place, field_name])
		if bad != "":
			return bad
	# НЕОБЪЯВЛЕННОЕ ПОЛЕ — расхождение. В бою клиент такие игнорирует (иначе
	# приборы машиниста поднимали бы major-версию), но проверка обязана их
	# ловить: поле, приехавшее и не объявленное, означает, что провод ушёл
	# вперёд договора, и та сторона, что о нём не знает, потеряет его молча.
	var extra: Array[String] = []
	for key in obj:
		if not fields.has(key):
			extra.append(String(key))
	if not extra.is_empty():
		extra.sort()
		return "%s: поля %s не объявлены в виде %s" % [place, str(extra), want]
	return ""


## sample — построить значение объявленного вида.
##
## Нужно чистой проверке: она обязана скормить разборщику клиента конверт,
## СОБРАННЫЙ ПО ДОГОВОРУ, а не по памяти автора проверки. Собранный по памяти
## образец согласится с клиентом в любой общей ошибке — ровно так клиент читал
## `network.trackside` после переименования поля и не падал.
##
## Значения берутся из overrides по имени поля, если заданы; иначе
## подставляется нейтральное значение вида.
func sample(kind: String, overrides: Dictionary = {}) -> Variant:
	var want := kind.trim_suffix("?")
	if want.begins_with("[]"):
		return [sample(want.substr(2), overrides)]
	match want:
		"string":
			return "образец"
		"bool":
			return false
		"float":
			return 1.5
		"int", "uint":
			return 1
		"int_string":
			return "1000"
	if not types.has(want):
		return null
	var out := {}
	var fields := types[want] as Dictionary
	for field_name in fields:
		if overrides.has(field_name):
			out[field_name] = overrides[field_name]
		else:
			out[field_name] = sample(String(fields[field_name]), overrides)
	return out


static func _kind_of(value: Variant) -> String:
	match typeof(value):
		TYPE_STRING, TYPE_STRING_NAME:
			return "строка"
		TYPE_FLOAT, TYPE_INT:
			return "число"
		TYPE_BOOL:
			return "bool"
		TYPE_ARRAY:
			return "массив"
		TYPE_DICTIONARY:
			return "объект"
		TYPE_NIL:
			return "null"
	return type_string(typeof(value))
