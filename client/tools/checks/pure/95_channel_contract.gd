## ДОГОВОР КАНАЛА, СТОРОНА КЛИЕНТА — чистая проверка: сети не касается вовсе.
##
## Сервер сверяет с contract/channel.v1.json свой провод; здесь тот же файл
## сверяется с ТЕМ, ЧТО УМЕЕТ КЛИЕНТ. Разделение честное: живая проверка
## (checks/live/15_channel.gd) смотрит на настоящий разговор, а эта — на разбор,
## и падает на голой машине, если клиент разошёлся с объявлением.
##
## # Почему конверт строится ПО ДОГОВОРУ, а не руками
##
## Образец, написанный по памяти автора проверки, согласится с клиентом в любой
## общей ошибке. Именно так клиент читал `network.trackside` после того, как поле
## переименовали в `structures`: get(имя, умолчание) молча давал умолчание, и
## HUD показывал ноль вместо единицы. Поэтому конверт собирается из объявления
## (ContractDoc.sample), и если в договоре появится поле, которого клиент не
## читает, — это будет видно здесь.
extends "res://tools/check_suite.gd"

const ContractDoc := preload("res://tools/contract_doc.gd")


func run() -> void:
	var doc = ContractDoc.load_channel()
	_ok("договор канала прочитан", not doc.failed(), doc.reason)
	if doc.failed():
		return

	_ok("договор называет канал", doc.name == "channel", doc.name)
	# ВЕРСИЯ КОНВЕРТА — то место, где расхождение сторон дороже всего: обе
	# считают, что договорились, и расходятся в первом же поле.
	_ok("версия конверта у клиента совпадает с договором",
		LiveChannel.PROTOCOL_VERSION == doc.protocol_version,
		"клиент %d, договор %d" % [LiveChannel.PROTOCOL_VERSION, doc.protocol_version])
	_ok("адрес канала у клиента совпадает с договором",
		LiveChannel.CHANNEL_PATH % "{region}" == doc.path,
		"клиент %s, договор %s" % [LiveChannel.CHANNEL_PATH % "{region}", doc.path])
	_ok("уведомление снапшота объявлено",
		doc.notifications.has(LiveChannel.SNAPSHOT_METHOD), str(doc.notifications.keys()))
	_ok("рукопожатие объявлено методом", doc.methods.has("hello"), str(doc.methods.keys()))

	# Причины, на которые клиент ДЕЙСТВУЕТ, обязаны быть в договоре. Обратное
	# неверно и не проверяется: сервер вправе иметь причины, которых клиент не
	# различает, — такие он показывает как есть.
	for reason in [LiveChannel.REASON_UNKNOWN_SESSION, LiveChannel.REASON_UNSUPPORTED_PROTOCOL]:
		_ok("причина %s объявлена в договоре" % reason, doc.refusal_reasons.has(reason))

	_check_display_kinematics(doc)
	_check_envelope(doc)
	_check_unknown_field_survives(doc)
	_check_validator_is_not_blind(doc)


## КИНЕМАТИКА ПОКАЗА ОБЪЯВЛЕНА, а не «клиент как-то сглаживает».
##
## Правило досчёта между снапшотами — клиентское, но версионированное и
## записанное в договоре (ClearAhead-t5h §6): сервер по нему будет мерить
## расхождение своей копии с картинкой игрока. Разойдись версия или буфер — и он
## мерил бы не ту картинку.
func _check_display_kinematics(doc) -> void:
	var decl: Dictionary = doc.raw.get("display_kinematics", {}) as Dictionary
	_ok("кинематика показа объявлена в договоре", not decl.is_empty())
	if decl.is_empty():
		return
	_ok("версия кинематики совпадает с договором",
		DisplayMotion.VERSION == int(decl.get("version", -1)),
		"клиент %d, договор %s" % [DisplayMotion.VERSION, str(decl.get("version"))])
	_ok("буфер показа совпадает с договором",
		DisplayMotion.BUFFER_US == int(decl.get("buffer_ms", -1)) * 1000,
		"клиент %d мкс, договор %s мс" % [DisplayMotion.BUFFER_US, str(decl.get("buffer_ms"))])


## Конверт, собранный ПО ДОГОВОРУ, обязан быть разобран клиентом полностью.
func _check_envelope(doc) -> void:
	var env_kind := String((doc.notifications[LiveChannel.SNAPSHOT_METHOD] as Dictionary).get("params", ""))
	var env: Variant = doc.sample(env_kind, {
		"protocol_version": LiveChannel.PROTOCOL_VERSION,
		"kind": "full",
		"snapshot_seq": 7,
		"region": "ST_A",
		"match": "M1",
		"time": "8100000",
	})
	_ok("конверт собран из договора", typeof(env) == TYPE_DICTIONARY, env_kind)
	if typeof(env) != TYPE_DICTIONARY:
		return
	# Собранный образец обязан сойтись с собственным договором: иначе проверка
	# ниже проверяла бы разбор кривого образца.
	var mismatch: String = doc.validate(env_kind, env)
	_ok("образец конверта сходится с договором", mismatch == "", mismatch)

	var ch := LiveChannel.new()
	var seen: Array[LiveChannel.Snapshot] = []
	ch.snapshot.connect(func(s: LiveChannel.Snapshot) -> void: seen.append(s))
	var broke: Array[String] = []
	ch.broke.connect(func(why: String) -> void: broke.append(why))
	ch.greeted = true
	ch._take_envelope(env as Dictionary)

	_ok("клиент разобрал конверт договора", seen.size() == 1, str(broke))
	if seen.is_empty():
		return
	var snap := seen[0]
	_ok("прочитан номер снапшота", snap.snapshot_seq == 7, str(snap.snapshot_seq))
	_ok("прочитан регион и партия", snap.region == "ST_A" and snap.match_id == "M1",
		"%s / %s" % [snap.region, snap.match_id])
	# Время СТРОКОЙ микросекунд — правило провода, и читается оно целым, а не
	# float: 8 100 000 мкс обязаны остаться ровно ими.
	_ok("время прочитано целым из строки", snap.time_us == 8100000, str(snap.time_us))
	_ok("прочитан вид снапшота", snap.full(), snap.kind)
	_ok("прочитаны единицы", snap.units.size() == 1, str(snap.units.size()))


## В БОЮ неизвестное поле игнорируется — иначе приборы машиниста поднимали бы
## major-версию. Строгость живёт в проверке, а не в разборе.
func _check_unknown_field_survives(doc) -> void:
	var env_kind := String((doc.notifications[LiveChannel.SNAPSHOT_METHOD] as Dictionary).get("params", ""))
	var env: Dictionary = doc.sample(env_kind, {
		"protocol_version": LiveChannel.PROTOCOL_VERSION,
		"kind": "full",
		"time": "1",
	}) as Dictionary
	env["brake_cylinder_kpa"] = 340.0

	var ch := LiveChannel.new()
	var seen: Array[LiveChannel.Snapshot] = []
	ch.snapshot.connect(func(s: LiveChannel.Snapshot) -> void: seen.append(s))
	ch.greeted = true
	ch._take_envelope(env)
	_ok("неизвестное поле конверта не мешает разбору", seen.size() == 1)
	# И оно же обязано быть замечено ПРОВЕРКОЙ: два разных требования к одному
	# полю, и оба нужны.
	var noticed: String = doc.validate(env_kind, env)
	_ok("проверка ловит необъявленное поле", noticed != "", noticed)

	# Чужая major версия — отказ, а не молчание: конверт собран по другим
	# правилам, и читать его своими значит выдумывать.
	var alien: Dictionary = env.duplicate()
	alien.erase("brake_cylinder_kpa")
	alien["protocol_version"] = LiveChannel.PROTOCOL_VERSION + 1
	var ch2 := LiveChannel.new()
	var got: Array[LiveChannel.Snapshot] = []
	var why: Array[String] = []
	ch2.snapshot.connect(func(s: LiveChannel.Snapshot) -> void: got.append(s))
	ch2.broke.connect(func(reason: String) -> void: why.append(reason))
	ch2.greeted = true
	ch2._take_envelope(alien)
	_ok("конверт чужой версии отвергнут", got.is_empty() and not why.is_empty(), str(why))


## ПРОВЕРКА ПРОВЕРКИ: валидатор, который согласен со всем, зелен по той же
## причине, по которой зелена выключенная лампочка.
func _check_validator_is_not_blind(doc) -> void:
	var env_kind := String((doc.notifications[LiveChannel.SNAPSHOT_METHOD] as Dictionary).get("params", ""))
	var good: Dictionary = doc.sample(env_kind, {"time": "5"}) as Dictionary

	var no_field: Dictionary = good.duplicate(true)
	no_field.erase("session_id")
	var missing: String = doc.validate(env_kind, no_field)
	_ok("валидатор видит пропущенное поле", missing != "")

	var wrong_kind: Dictionary = good.duplicate(true)
	wrong_kind["time"] = 5
	var numeric: String = doc.validate(env_kind, wrong_kind)
	_ok("валидатор видит целое числом вместо строки", numeric != "")

	var wrong_nested: Dictionary = good.duplicate(true)
	var units: Array = (wrong_nested["units"] as Array).duplicate(true)
	(units[0] as Dictionary).erase("type")
	wrong_nested["units"] = units
	var bad: String = doc.validate(env_kind, wrong_nested)
	_ok("валидатор доходит до дна массива", bad != "" and bad.contains("units[0]"), bad)
