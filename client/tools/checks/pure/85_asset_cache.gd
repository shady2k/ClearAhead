## КЭШ АССЕТОВ — запись, чтение, обнаружение порчи.
##
## # Почему это ЧИСТАЯ проверка, хотя она трогает диск
##
## Прежняя редакция объясняла отсутствие такой проверки тем, что «полный тест с
## диском требует сервера», и это было неверно: user:// пишется и читается без
## единого запроса. Цена ошибки оказалась ровно та, о которой правило проекта и
## предупреждает: запись в кэш НЕ РАБОТАЛА ВОВСЕ (каталог создавался с именем
## файла), и все сто тридцать пять проверок при этом были зелёными — потому что
## щупали HashingContext самого движка и то, что класс создаётся.
##
## Сервер здесь не нужен ни на что: скачивание проверяется отдельно, живой
## проверкой, а здесь проверяется ХРАНИЛИЩЕ.
extends "res://tools/check_suite.gd"

## Байты, на которых всё проверяется. Не пустые нарочно: пустой буфер прошёл бы
## и через сломанную запись — файл нулевой длины неотличим от «ничего не
## записали».
const BODY := "локомотив, которого нет"


func run() -> void:
	var cache := AssetCache.new()
	var bytes := BODY.to_utf8_buffer()
	var address := _digest(bytes)

	# Чистое место: адрес зависит от содержимого, поэтому прошлый прогон мог
	# оставить тот же файл. Проверка обязана начинаться с пустоты, иначе она
	# проверяет вчерашний прогон.
	DirAccess.remove_absolute("%s/%s" % [AssetCache.CACHE_ROOT, address])

	_ok("пустого кэша нет", cache.fetch(address).is_empty(),
		"файл остался от прошлого прогона")

	var why := cache.store(address, bytes)
	_ok("запись в кэш проходит", why == "", why)

	var back := cache.fetch(address)
	_ok("прочитанное равно записанному", back == bytes,
		"записали %d Б, прочитали %d Б" % [bytes.size(), back.size()])

	_test_corruption_detected(cache, address)
	_test_foreign_address(cache)


## _test_corruption_detected — испорченный файл обязан быть УВИДЕН и ВЫБРОШЕН.
##
## Порча подделывается записью чужих байтов под тем же адресом — ровно то, что
## даёт оборванная запись или полный диск. Кэш обязан заметить это по хешу,
## отдать пустоту (то есть заставить скачать заново) и убрать файл, чтобы
## следующий запуск не спотыкался о то же самое.
func _test_corruption_detected(cache: AssetCache, address: String) -> void:
	var path := "%s/%s" % [AssetCache.CACHE_ROOT, address]
	var file := FileAccess.open(path, FileAccess.WRITE)
	if file == null:
		_ok("подделка порчи возможна", false, "не открыть %s" % path)
		return
	file.store_buffer("совсем другие байты".to_utf8_buffer())
	file.close()

	_ok("повреждённый кэш не отдаётся", cache.fetch(address).is_empty(),
		"битые байты отданы как исправные")
	_ok("повреждённый кэш выброшен", not FileAccess.file_exists(path),
		"битый файл остался на диске и будет мешать каждому запуску")


## _test_foreign_address — адрес не по нашему алгоритму отвергается сразу.
## Клиент умеет один алгоритм, и молчаливое согласие на чужой означало бы, что
## содержимое не проверено вовсе.
func _test_foreign_address(cache: AssetCache) -> void:
	_ok("чужой алгоритм адреса не читается из кэша",
		cache.fetch("md5-0123456789abcdef").is_empty())


func _digest(bytes: PackedByteArray) -> String:
	var ctx := HashingContext.new()
	ctx.start(HashingContext.HASH_SHA256)
	ctx.update(bytes)
	return "sha256-" + ctx.finish().hex_encode()
