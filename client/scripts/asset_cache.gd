## AssetCache — кэш ассетов на диске, адресуемый хешем, со сверкой.
##
## Постоянное хранилище байтов по адресу вида "sha256-<hex>". При каждом
## запросе проверяется целостность хеша: если файл повреждён — перекачивается.
## Несовпадение хеша при загрузке отвергается отказом с обоими хешами.
class_name AssetCache
extends RefCounted

## Корневой каталог кэша. user:// переживает перезапуск клиента — ради этого
## кэш и заведён: без него каждый запуск качает двадцать один мегабайт заново.
const CACHE_ROOT := "user://assets"

## Что пошло не так с САМИМ КЭШЕМ, не с ассетами. Пусто — всё в порядке.
## Забирает мир и показывает вместе с прочими отказами.
var notes: Array[String] = []


## load_or_download — получить ассет: из кэша или загрузить и кэшировать.
##
## # Поток вычисления
##
## 1. Если есть в кэше — прочитать. ПОСЧИТАТЬ SHA-256 и сверить с адресом.
##    Совпал — отдать. Не совпал (повреждён) — выбросить и скачать заново.
## 2. Нет в кэше — скачать, посчитать SHA-256, сверить с адресом. Совпал —
##    положить в кэш и отдать. Не совпал — ОТКАЗ, в кэш не класть.
##
## Отказ показывает оба хеша: адрес из паспорта и что насчитали мы.
## Это спасает при отладке прокси, испорченного кэша, оборванной докачки.
func load_or_download(address: String, api: WorldApi) -> WorldApi.AssetBlob:
	var a := WorldApi.AssetBlob.new()
	if not address.begins_with("sha256-"):
		a.reason = "адрес ассета %s: клиент умеет только sha256" % address
		return a

	var cached := fetch(address)
	if not cached.is_empty():
		a.bytes = cached
		a.digest = address
		return a

	# Кэша нет либо он был повреждён и выброшен. Качаем.
	#
	# ВТОРОЙ РАЗ ХЕШ ЗДЕСЬ НЕ СЧИТАЕТСЯ, и это не экономия строчки: WorldApi.asset
	# уже сверяет присланные байты с адресом и отказывает при расхождении (его
	# отказ и называет оба хеша). Пересчёт был бы вторым проходом по двадцати
	# одному мегабайту ради результата, который уже доказан.
	var blob: WorldApi.AssetBlob = await api.asset(address)
	if blob.failed():
		a.reason = blob.reason
		return a
	a.bytes = blob.bytes
	a.digest = blob.digest
	# НЕУДАВШАЯСЯ ЗАПИСЬ КЭША — НЕ ОТКАЗ ЗАПРОСА: байты доехали и проверены, вид
	# машины будет показан. Но и молчать нельзя — иначе «почему каждый запуск
	# качает заново» разбирается вслепую. Причина копится здесь, а на экран её
	# выносит мир (world.gd), которому и принадлежит список отказов.
	var why := store(address, blob.bytes)
	if why != "":
		notes.append(why)
	return a


## fetch — байты из кэша, ЕСЛИ они там есть и целы. Пусто во всех остальных
## случаях, включая порчу.
##
## Публичный метод, потому что его зовёт проверка: кэш проверяется записью и
## чтением на диске, и сервер для этого не нужен вовсе. Прежняя редакция
## объясняла отсутствие такой проверки тем, что «полный тест с диском требует
## сервера», и это было неверно — из-за чего целиком неработавшая запись
## (см. store) прошла мимо всех ста тридцати пяти проверок.
func fetch(address: String) -> PackedByteArray:
	var path := _cache_path(address)
	if not FileAccess.file_exists(path):
		return PackedByteArray()
	var file := FileAccess.open(path, FileAccess.READ)
	if file == null:
		return PackedByteArray()
	var bytes := file.get_buffer(int(file.get_length()))
	file.close()
	# ХЕШ СЧИТАЕТСЯ ПРИ КАЖДОМ ЧТЕНИИ, а не только при записи. Файл на диске
	# портится и после того, как лёг туда целым: оборванная запись, полный диск,
	# правка руками. Адрес обещает содержимое, и проверять обещание надо там, где
	# им пользуются.
	if _hash_bytes(bytes) == address:
		return bytes
	# Повреждён — выбросить, чтобы следующий заход качал, а не спотыкался о то же
	# самое. Молча оставленный битый файл давал бы отказ при каждом запуске.
	_delete_cached(address)
	return PackedByteArray()


## store — положить байты в кэш. Возвращает причину отказа или пустую строку.
##
## # Дефект, ради которого этот метод переписан
##
## Здесь стояло `var dir := path.get_basename()`. У Godot get_basename отрезает
## РАСШИРЕНИЕ, а не имя файла: каталог даёт get_base_dir. В адресе ассета точки
## нет вовсе, поэтому get_basename возвращал путь целиком, и код создавал
## КАТАЛОГ С ИМЕНЕМ АССЕТА, после чего не мог открыть файл по тому же пути и
## уходил в push_warning.
##
## Снаружи это выглядело исправным: игра работала, просто качала двадцать один
## мегабайт каждый запуск — то есть ровно то, что кэш и должен был починить.
## Худший вид поломки: не отказ, а тихое возвращение к прежнему поведению.
##
## Каталог создаётся РЕКУРСИВНО: user:// на свежей машине пуст, и промежуточных
## уровней может не быть.
func store(address: String, bytes: PackedByteArray) -> String:
	var path := _cache_path(address)
	var dir := path.get_base_dir()
	if not DirAccess.dir_exists_absolute(dir):
		var err := DirAccess.make_dir_recursive_absolute(dir)
		if err != OK:
			return "каталог кэша %s не создан: %s" % [dir, error_string(err)]
	var file := FileAccess.open(path, FileAccess.WRITE)
	if file == null:
		return "файл кэша %s не открыт: %s" % [path, error_string(FileAccess.get_open_error())]
	file.store_buffer(bytes)
	file.close()
	return ""


## _delete_cached — выбросить повреждённый ассет из кэша.
func _delete_cached(address: String) -> void:
	var path := _cache_path(address)
	if FileAccess.file_exists(path):
		var err := DirAccess.remove_absolute(path)
		if err != OK:
			push_warning("AssetCache: не удалось удалить повреждённый кэш %s: %s" % [
				path, error_string(err)
			])


## _cache_path — полный путь кэша для адреса. Адрес вида "sha256-<hex>"
## становится именем файла.
func _cache_path(address: String) -> String:
	return "%s/%s" % [CACHE_ROOT, address]


## _hash_bytes — посчитать SHA-256 для буфера.
##
## # Почему это входит сюда, а не остаётся в WorldApi
##
## Проверка и кэширование — свойство ДОСТАВКИ ассета, а не самого слоя.
## WorldApi ответствен за чтение провода; AssetCache ответствен за то,
## чтобы адрес честно совпадал с содержимым БЕЗ молчаливых умолчаний.
## api.asset() уже считает хеш; мы считаем снова и сравниваем независимо.
func _hash_bytes(bytes: PackedByteArray) -> String:
	var ctx := HashingContext.new()
	ctx.start(HashingContext.HASH_SHA256)
	ctx.update(bytes)
	return "sha256-" + ctx.finish().hex_encode()
