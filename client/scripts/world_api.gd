## WorldApi — ВОПРОС К МИРУ, а не запрос по адресу.
##
## Игра спрашивает каталог, манифест, сеть, рельеф, покров, лес и объекты. Она не
## знает ни одного адреса, ни одного кода ответа и ни одного имени заголовка:
## всё это живёт здесь, и здесь же исход получает ИМЯ.
##
## # Зачем слой, если труба уже есть
##
## NetClient объявлял себя «единственным местом, где клиент говорит с сервером»
## и был прав ровно про механику: соединение, таймаут, счётчики кодов. Протокол
## при этом жил в рендере. ЗАМЕР 2026-08-12 (ClearAhead-w6d, повторён перед этой
## правкой): шесть шаблонов адреса собирались вне трубы (app.gd — один,
## world.gd — пять), смысл кода решался в пяти местах world.gd, имя заголовка
## X-Chunk-Base-Z-Mm стояло в двух файлах, и семнадцать чтений нетипизированного
## словаря ["ok"], ["code"], ["body"], ["headers"] проходили сквозь игровую
## логику.
##
## Цена была названа самим клиентом: WebSocket придёт в В2 вместе с мутациями
## (ClearAhead-sjq), а кода 204 у WebSocket не существует — в этот день менялось
## бы каждое из перечисленных мест. Доказательство пришло раньше В2: агент,
## чинивший дыры в рельефе (ClearAhead-cg3), менял смысл 204 у чанка с «дыры
## нет покрытия» на «спускайся к грубому уровню» — и правил это В РЕНДЕРЕ.
##
## # 204 ЗНАЧИТ РАЗНОЕ, и потому у него РАЗНЫЕ ИМЕНА
##
##   чанк:   пусто → no_chunk()  — подробных соседей не приехало, место
##                                 достанется более грубому уровню;
##   покров: пусто → no_recipe() — у карты нет рецепта покрова, земля серая;
##   лес:    пусто → no_forest() — леса здесь нет вовсе.
##
## Это три РАЗНЫХ события мира, и на сегодняшнем проводе два из них выражены
## одним числом. Назвать их обязан слой — один раз, а не каждый вызывающий
## заново, каждый по-своему и каждый рискуя разойтись с остальными.
##
## # ЗАКОН КЛИЕНТА НЕ ОСЛАБЛЕН, А УСИЛЕН (ClearAhead-sjq)
##
## «Чего сервер не прислал, того на экране нет.» Слой не подставляет ничего
## вместо ответа: ни эталона при отказе, ни умолчания при пустом теле. Отказ
## доезжает до вызывающего отказом, с человеческой причиной, и показывается
## человеку. Усиление в том, что «показать отказ» перестало быть повторяющимся
## куском в каждом вызывающем: повторение — это место, где однажды забудут.
##
## Отсутствие и отказ РАЗВЕДЕНЫ намеренно. «Чанка нет» — законное состояние мира
## и часть правила подробности; «сервер ответил 500» — отказ. Слить их в одно
## «не получилось» значило бы либо рисовать дыры на месте законной пустоты, либо
## молча съедать поломку сервера.
##
## # ЧЕГО ЗДЕСЬ НЕТ
##
## Второго транспорта. WebSocket не заложен и не предугадан: абстракция,
## спроектированная под воображаемый транспорт, обычно оказывается неверной.
## Заложено ровно одно и другое — что СМЫСЛ ИСХОДА НЕ ЯВЛЯЕТСЯ КОДОМ. В день,
## когда придёт В2, меняется этот файл, а не двадцать мест в рендере.
class_name WorldApi
extends Node

## Коды HTTP — ЗДЕСЬ И БОЛЬШЕ НИГДЕ В КЛИЕНТЕ, кроме трубы, которая их считает,
## и проверки контракта, которая нарочно щупает провод.
const CODE_OK := 200
const CODE_EMPTY := 204

## Имя заголовка базовой отметки чанка. Тоже протокол, и тоже одно место:
## рендер видит ПОЛЕ base_z_m и про заголовки не знает вовсе.
const HEADER_BASE_Z := "X-Chunk-Base-Z-Mm"

## Адрес сервера. Ставится ДО входа в дерево — труба заводится в _ready, и слой,
## настроенный после, успел бы спросить не тот сервер. То же правило, по
## которому мир получает configure до add_child.
var base_url := ""

## Труба. Публична ради ОДНОГО потребителя — проверки контракта (tools/check.gd):
## ей положено уметь спросить провод напрямую, иначе «сервер отвечает 404 там,
## где договорились» проверить нечем. Игре она не нужна, и это не пожелание:
## та же проверка грепом убеждается, что в client/scripts/ имя NetClient не
## встречается нигде, кроме этого файла.
var net: NetClient


func _ready() -> void:
	net = NetClient.new()
	net.base_url = base_url
	add_child(net)


## transport_counts — счётчики кодов ДЛЯ ОТЧЁТА, а не для решений.
##
## Отчёт клиента печатает их числом, и это законно: «сколько раз сервер ответил
## чем» — свойство прогона, а не факт о мире. Решение по коду не принимает никто,
## кроме этого файла.
func transport_counts() -> Dictionary:
	return net.code_counts if net != null else {}


## --- ИСХОДЫ ------------------------------------------------------------------

## Answer — исход одного вопроса. Три состояния, и ни одно не выражено числом:
##
##   have()   — ответ есть, поля заполнены;
##   failed() — отказ, причина человеческая и лежит в reason;
##   отсутствие — называется СЛОВОМ в наследнике (no_chunk, no_recipe,
##                no_forest), потому что у каждого ресурса это своё событие.
##
## Поле _missing заполняет слой; читать его снаружи нечем и незачем — наружу
## смотрит слово.
class Answer extends RefCounted:
	var reason := ""
	var _missing := false

	func failed() -> bool:
		return reason != ""

	func have() -> bool:
		return reason == "" and not _missing


## Catalog — какие миры сервер держит. На нём стоит весь вход в игру.
class Catalog extends Answer:
	var regions: Array = []


## Manifest — паспорт региона: ревизия, хеши, правило подробности.
##
## Отдаётся СЛОВАРЁМ, а не разобранным типом, и это не лень. Мир сверяет ИМЕНА
## присланных полей с теми, что умеет читать (_check_fields), и отказывает на
## пропавшем известном поле — сверять можно только то, что не разобрано заранее.
## Разбор правила подробности живёт в ChunkRule, разбор сети — в TrackGeom, и
## слою тут делать нечего: он отвечает за провод, а не за смысл документа.
class Manifest extends Answer:
	var data: Dictionary = {}


## Network — сеть региона: элементы, сооружения, типы пути, прогоны.
class Network extends Answer:
	var data: Dictionary = {}


## Objects — третий ресурс региона: постройки и реки.
class Objects extends Answer:
	var data: Dictionary = {}


## Heights — отсчёты высот одного чанка ВМЕСТЕ С БАЗОЙ.
##
## base_z_m — ПОЛЕ, а не заголовок, и это половина смысла всей правки: до неё
## рендер сам читал X-Chunk-Base-Z-Mm, сам делил на 1000 и сам решал, что
## делать, когда заголовка нет. Теперь он получает метры.
##
## blob остаётся сырым телом (samples² значений int16 LE): разбирает его
## TerrainMesh — там же, где строится меш, и там же замеряется цена разбора
## (decode_usec). Разобрать здесь значило бы пройти по телу дважды.
class Heights extends Answer:
	var blob := PackedByteArray()
	var base_z_m := 0.0

	## no_chunk — чанка по этому адресу нет.
	##
	## Это НЕ дыра и не сбой: место, которое он занял бы, достаётся более грубому
	## уровню (контракт чанков §6). Выражается это тем, что у накрывающего узла не
	## ставится порог видимости — world.gd::_load_terrain. Земли нет только за
	## последним уровнем, и там адрес не спрашивают вовсе.
	func no_chunk() -> bool:
		return _missing

## Patch — отсчёты высот клетки НА НАЗВАННОЙ ВЕРСИИ мира.
##
## То же тело и та же база, что у чанка (samples² int16 LE относительно
## base_z, заголовок X-Chunk-Base-Z-Mm), но адрес версионный — строка
## хранилища ключуется версией, и тело под версией v не меняется никогда
## (immutable). Собирается сервером из базы и земляных работ версии: клиенту
## применять ничего не нужно — он рисует присланное (спека §5.1, sqym.6).
##
## level/cx/cz — адрес, пронесённый насквозь тем же шагом, что у Tile: набор
## версии запоминает, какой клетке принадлежит ответ.
class Patch extends Answer:
	var blob := PackedByteArray()
	var base_z_m := 0.0
	var level := 0
	var cx := 0
	var cz := 0

	## no_work — «чистая база»: земляных работ на этой версии в клетке нет.
	##
	## Это ПОЛНЫЙ ответ, а не ошибка и не дыра: 204 отличим от 404 нарочно —
	## несуществующий адрес (чужой матч, версия за головой, уровень выше
	## последнего) это неверный вопрос, а пустое содержимое версии — верный.
	## Путать их нельзя: 204 не переспрашивается, 404 показывается отказом.
	func no_work() -> bool:
		return _missing


## Cover — класс поверхности и сомкнутость на ячейку той же сетки, что квады.
class Cover extends Answer:
	var cells := PackedByteArray()

	## no_recipe — у КАРТЫ нет рецепта покрова.
	##
	## Законное состояние, а не сбой: земля тогда рисуется серой, как рисовалась
	## до появления покрова вовсе. Событие другой природы, чем no_chunk(), хотя на
	## сегодняшнем проводе оба выражены одним числом.
	func no_recipe() -> bool:
		return _missing


## ForestBits — битовая карта стволов: бит на ячейку покрова.
class ForestBits extends Answer:
	var bits := PackedByteArray()

	## no_forest — леса здесь нет.
	##
	## ЦЕНА НАЗВАНА: сегодня сюда попадает ЛЮБОЙ ответ, кроме 200. Так вело себя и
	## прежнее место в рендере (world.gd до этой правки: `if ok and code == 200`,
	## и ни одной ветки на остальные коды), и перенос обязан был сохранить это
	## дословно. Значит поломка сервера на этом ресурсе выглядит как безлесная
	## земля, а не как отказ. Отделить «леса нет» от «сервер сломался» — правка,
	## меняющая поведение, и она заведена отдельно (ClearAhead-v1u).
	func no_forest() -> bool:
		return _missing


## Tile — всё, что клиент спрашивает про ОДНО МЕСТО рельефа: высоты, покров, лес.
##
## Плитка существует потому, что три ресурса одного адреса связаны условием, а не
## лежат рядом случайно: покров и лес спрашиваются, только если по этому адресу
## приехали высоты. Держать это условие в рендере значило бы держать там же и
## порядок запросов — то есть ровно то, что мешает чинить ClearAhead-s42.
##
## address — запись ChunkRule (level, cx, cz), пронесённая насквозь: слой читает
## из неё три числа адреса и не трогает остального.
class Tile extends RefCounted:
	var address: Dictionary = {}
	## heights — ВЫСОТЫ МЕСТА: Heights на неверсионном проводе, Patch на
	## версионном (sqym.6). Тип не назван нарочно: у обоих один протокол
	## (have/no_work·no_chunk/failed), и называть один из них значило бы
	## запретить второй.
	var heights
	var cover: Cover
	var forest: ForestBits


## --- ВОПРОСЫ -----------------------------------------------------------------

## regions — каталог миров сервера.
func regions() -> Catalog:
	var a := Catalog.new()
	var r: Dictionary = await net.fetch_json("/regions")
	if not r["ok"]:
		a.reason = String(r["error"])
		return a
	a.regions = (r["data"] as Dictionary).get("regions", []) as Array
	return a


## manifest — паспорт региона.
func manifest(region: String) -> Manifest:
	var a := Manifest.new()
	var r: Dictionary = await net.fetch_json("/regions/%s" % region)
	if not r["ok"]:
		a.reason = String(r["error"])
		return a
	a.data = r["data"] as Dictionary
	return a


## network — сеть региона на названной ревизии.
func network(region: String, revision: int) -> Network:
	var a := Network.new()
	var r: Dictionary = await net.fetch_json("/regions/%s/revisions/%d/network" % [region, revision])
	if not r["ok"]:
		a.reason = String(r["error"])
		return a
	a.data = r["data"] as Dictionary
	return a

## world_network — сеть региона НА НАЗВАННОЙ ВЕРСИИ мира.
##
## Версионный адрес (sqym.5) неизменяем: тело, однажды отданное под версией v,
## не меняется никогда (immutable), и клиент, выбравший версию, видит ровно её
## сеть — а не последнюю, которая могла уехать вперёд между запросами.
## Несуществующая версия или чужой матч — отказ, как и всякий неверный адрес.
func world_network(match: String, version: int) -> Network:
	var a := Network.new()
	var r: Dictionary = await net.fetch_json("/matches/%s/worlds/%d/network" % [match, version])
	if not r["ok"]:
		a.reason = String(r["error"])
		return a
	a.data = r["data"] as Dictionary
	return a


## objects — постройки и реки региона на названной ревизии.
func objects(region: String, revision: int) -> Objects:
	var a := Objects.new()
	var r: Dictionary = await net.fetch_json("/regions/%s/revisions/%d/objects" % [region, revision])
	if not r["ok"]:
		a.reason = String(r["error"])
		return a
	a.data = r["data"] as Dictionary
	return a


## chunk — отсчёты высот по адресу, вместе с базовой отметкой.
##
## Отсутствие заголовка базы — ОТКАЗ, а не ноль: подставить ноль значило бы
## нарисовать чанк на неизвестной высоте, ровно тот случай, ради которого писан
## закон клиента. Формулировка причины перенесена из world.gd дословно.
func chunk(region: String, level: int, cx: int, cz: int) -> Heights:
	var a := Heights.new()
	var r: Dictionary = await net.fetch(_chunk_path(region, level, cx, cz))
	if not r["ok"]:
		a.reason = String(r["error"])
		return a
	var code := int(r["code"])
	if code == CODE_EMPTY:
		a._missing = true
		return a
	if code != CODE_OK:
		a.reason = "сервер ответил HTTP %d" % code
		return a
	var base := NetClient.header_value(r["headers"], HEADER_BASE_Z)
	if base == "":
		a.reason = "нет заголовка %s — высоты не к чему отложить" % HEADER_BASE_Z
		return a
	# Миллиметры целыми — в метры. Единственное место клиента, знающее единицу
	# этого заголовка; наружу уезжают метры, как и всюду в мире.
	a.base_z_m = float(base.to_int()) / 1000.0
	a.blob = r["body"] as PackedByteArray
	return a

## world_patch — отсчёты высот клетки НА НАЗВАННОЙ ВЕРСИИ мира.
##
## Тот же блоб и та же база, что у чанка, но адрес версионный: тело под
## версией v неизменяемо, и клиент, копящий набор версии, не рискует получить
## землю, уехавшую вперёд между запросами.
##
## ТРИ ИСХОДА, И ПУТАТЬ ИХ НЕЛЬЗЯ (sqym.6):
##   200 — have(): отсчёты версии, тело и база заполнены;
##   204 — no_work(): «чистая база» — земляных работ на этой версии в клетке
##         нет. ПОЛНЫЙ ответ, а не ошибка: 204 не переспрашивается;
##   прочее — failed(): отказ (чужой матч, версия за головой, уровень выше
##         последнего, транспорт), и набор версии при нём не собирается.
func world_patch(match: String, version: int, level: int, cx: int, cz: int) -> Patch:
	var a := Patch.new()
	a.level = level
	a.cx = cx
	a.cz = cz
	var r: Dictionary = await net.fetch(_world_patch_path(match, version, level, cx, cz))
	if not r["ok"]:
		a.reason = String(r["error"])
		return a
	var code := int(r["code"])
	if code == CODE_EMPTY:
		a._missing = true
		return a
	if code != CODE_OK:
		a.reason = "сервер ответил HTTP %d" % code
		return a
	var base := NetClient.header_value(r["headers"], HEADER_BASE_Z)
	if base == "":
		a.reason = "нет заголовка %s — высоты не к чему отложить" % HEADER_BASE_Z
		return a
	a.base_z_m = float(base.to_int()) / 1000.0
	a.blob = r["body"] as PackedByteArray
	return a


## cover — покров по тому же адресу.
##
## Отдельным телом, а не полем в чанке: блобы разной природы и разной длины (8450
## против 4096), и склеенные они заставили бы клиента без покрова возить покров, а
## клиента без рельефа — разбирать заголовок длины.
func cover(region: String, level: int, cx: int, cz: int) -> Cover:
	var a := Cover.new()
	var r: Dictionary = await net.fetch(_chunk_path(region, level, cx, cz) + "/cover")
	if not r["ok"]:
		a.reason = String(r["error"])
		return a
	var code := int(r["code"])
	if code == CODE_EMPTY:
		a._missing = true
		return a
	if code != CODE_OK:
		a.reason = "сервер ответил HTTP %d" % code
		return a
	a.cells = r["body"] as PackedByteArray
	return a


## forest — битовая карта стволов по тому же адресу.
func forest(region: String, level: int, cx: int, cz: int) -> ForestBits:
	var a := ForestBits.new()
	var r: Dictionary = await net.fetch(_chunk_path(region, level, cx, cz) + "/forest")
	if not r["ok"]:
		a.reason = String(r["error"])
		return a
	if int(r["code"]) != CODE_OK:
		a._missing = true
		return a
	a.bits = r["body"] as PackedByteArray
	return a


## tile — одно место рельефа целиком.
##
## ПОРЯДОК И УСЛОВИЯ ПЕРЕНЕСЕНЫ ИЗ world.gd ДОСЛОВНО, и каждое из них — решение,
## а не случайность:
##
##  • покров и лес спрашиваются, ТОЛЬКО когда по адресу приехали высоты. Иначе
##    клиент оплачивал бы трафик за место, которого не рисует;
##  • лес спрашивается ТОЛЬКО у уровня 0 (контракт чанков §5а). За коридором
##    деревья рассевает клиент по покрову: тождество дереву нужно затем, что его
##    рубят, а рубят только там, куда дотянулись. Сервер на этот вопрос отвечает
##    404, то есть тем же «леса нет», — но спрашивать его незачем.
##
## Непрошенные ресурсы возвращаются ОТКАЗОМ с честной причиной, а не пустышкой:
## вызывающий до них не доходит (он уходит раньше по отказу либо по «чанка нет»),
## а если однажды дойдёт — увидит на экране, что именно случилось, вместо тихого
## «покрова нет».
func tile(region: String, address: Dictionary) -> Tile:
	var t := Tile.new()
	t.address = address
	var level := int(address["level"])
	var cx := int(address["cx"])
	var cz := int(address["cz"])

	t.heights = await chunk(region, level, cx, cz)
	t.cover = Cover.new()
	t.forest = ForestBits.new()
	if not t.heights.have():
		t.cover.reason = "не спрашивали: высот по этому адресу нет"
		t.forest.reason = "не спрашивали: высот по этому адресу нет"
		return t

	t.cover = await cover(region, level, cx, cz)
	if level == 0:
		t.forest = await forest(region, level, cx, cz)
	else:
		t.forest._missing = true
	return t


## terrain — рельеф ЦЕЛЫМ УРОВНЕМ: по плитке на адрес, в том же порядке.
##
## # Почему список, а не вызов на адрес
##
## Здесь и только здесь лежит ПОРЯДОК ХОЖДЕНИЯ В СЕТЬ за рельефом. ЗАМЕР
## 2026-08-12 (снимок роли builder, затравка ST_A): 115 адресов, 260 запросов,
## загрузка рельефа 23.9 с — это ClearAhead-s42, и лечится он тем, что запросы
## идут разом, а не друг за другом. Пока вызывающий просил чанк ПО ОДНОМУ и сам
## писал `await` в цикле сборки мешей, распараллелить их было нельзя, не трогая
## рендер: цикл запросов был размазан по world.gd вместе со сборкой геометрии.
##
## Уровнем, а не всем рельефом сразу, — и с 2026-08-12 (вечер) это уже не
## обязанность, а привычка вызывающего. Прежде уровни были ЗАВИСИМЫ: пустой ответ
## на подробном разрешал грубому нарисовать это место самому, и адреса уровня L+1
## отбирались, зная, что приехало на уровне L. Теперь адреса называет один
## манифест, а «кто кого накрывает» решается на собранном наборе (world.gd), то
## есть параллельным вправе стать весь рельеф разом, а не только уровень.
##
## Сегодня плитки берутся подряд: правка границы не меняет поведения, это
## записано отдельным правилом переноса. Починка s42 — правка ЭТОЙ функции, и
## больше ничьей.
func terrain(region: String, addresses: Array) -> Array:
	var out: Array = []
	for a_raw in addresses:
		out.append(await tile(region, a_raw as Dictionary))
	return out


## _chunk_path — адрес клетки. Один шаблон на все три ресурса чанка: клетка одна,
## и два способа её назвать развели бы её тождество надвое.
func _chunk_path(region: String, level: int, cx: int, cz: int) -> String:
	return "/regions/%s/chunks/%d/%d/%d" % [region, level, cx, cz]

## world_tile — одно место рельефа версии v: патч из версионного адреса, покров
## и лес — из неверсионного.
##
## Покров и лес у версионного адреса НЕТ (sqym.5 отдаёт клиенту только сеть и
## патчи): они производные рецепта региона, а не земляных работ, и между
## версиями не меняются. Порядок и условия — те же, что у tile(): покров и лес
## спрашиваются, ТОЛЬКО когда по адресу приехали высоты, а лес — только у
## уровня 0.
func world_tile(match: String, version: int, region: String, address: Dictionary) -> Tile:
	var t := Tile.new()
	t.address = address
	var level := int(address["level"])
	var cx := int(address["cx"])
	var cz := int(address["cz"])

	t.heights = await world_patch(match, version, level, cx, cz)
	t.cover = Cover.new()
	t.forest = ForestBits.new()
	if not t.heights.have():
		t.cover.reason = "не спрашивали: высот по этому адресу нет"
		t.forest.reason = "не спрашивали: высот по этому адресу нет"
		return t

	t.cover = await cover(region, level, cx, cz)
	if level == 0:
		t.forest = await forest(region, level, cx, cz)
	else:
		t.forest._missing = true
	return t


## world_terrain — рельеф уровня версии v, по плитке на адрес (см. terrain —
## там же записан порядок хождения в сеть и его цена, ClearAhead-s42).
func world_terrain(match: String, version: int, region: String, addresses: Array) -> Array:
	var out: Array = []
	for a_raw in addresses:
		out.append(await world_tile(match, version, region, a_raw as Dictionary))
	return out


## _world_patch_path — версионный адрес патча клетки. Один шаблон, и он же в
## проверках контракта (checks/live/35_worlds.gd) — два способа собрать адрес
## разошлись бы при первой же правке формы.
func _world_patch_path(match: String, version: int, level: int, cx: int, cz: int) -> String:
	return "/matches/%s/worlds/%d/chunks/%d/%d/%d/terrain-patch" % [match, version, level, cx, cz]


## --- НАБОР КОНТЕНТА И ЖИВОЕ СОСТОЯНИЕ ----------------------------------------
##
## Три ресурса разной природы, и путать их сроки жизни нельзя:
##
##   /content                   какие машины БЫВАЮТ. Перепроверяется по ETag.
##   /assets/{alg}-{hex}        байты вида. Кэш навсегда: адрес есть содержимое.
##   /regions/{region}/live     что где СТОИТ. Не кэшируется вовсе.
##
## Первое — контент, второе — байты контента, третье — состояние партии. Первые
## два общие на сервер (ВЛ80 к карте отношения не имеет), третье принадлежит
## партии, идущей на этом регионе.


## Content — набор: паспорта подвижного состава и записи ассетов.
class Content extends Answer:
	var data: Dictionary = {}


## Live — живое состояние партии. Сегодня в нём только положения стоящих единиц:
## ни времени, ни скорости в ответе нет, потому что ничего не движется.
class Live extends Answer:
	var data: Dictionary = {}


## AssetBlob — байты одного ассета, СВЕРЕННЫЕ с адресом.
class AssetBlob extends Answer:
	var bytes := PackedByteArray()
	## digest — что насчитал клиент. Хранится рядом с причиной, чтобы отказ
	## показывал оба хеша, а не только слово «не совпал».
	var digest := ""


## content — набор контента сервера.
func content() -> Content:
	var a := Content.new()
	var r: Dictionary = await net.fetch_json("/content")
	if not r["ok"]:
		a.reason = String(r["error"])
		return a
	a.data = r["data"] as Dictionary
	return a


## live — живое состояние партии в регионе.
func live(region: String) -> Live:
	var a := Live.new()
	var r: Dictionary = await net.fetch_json("/regions/%s/live" % region)
	if not r["ok"]:
		a.reason = String(r["error"])
		return a
	a.data = r["data"] as Dictionary
	return a


## asset — байты ассета по адресу вида "sha256-<hex>".
##
## ХЕШ СВЕРЯЕТСЯ ВСЕГДА, и это не перестраховка. Адрес ассета ЕСТЬ хеш его
## содержимого, поэтому проверка стоит одного прохода по буферу и ловит то, чего
## иначе не поймать ничем: испорченный кэш, оборванную докачку, подменённый
## прокси. Замер на боевом файле (21.4 МБ, машина владельца): sha256 считается
## 12 мс — против секунд самой загрузки это ничто.
##
## Несовпадение — ОТКАЗ, а не «нарисуем что доехало»: меш, собранный из битых
## байт, выглядит как ошибка отрисовки, и искать её будут не там.
func asset(address: String) -> AssetBlob:
	var a := AssetBlob.new()
	if not address.begins_with("sha256-"):
		a.reason = "адрес ассета %s: клиент умеет только sha256" % address
		return a
	var r: Dictionary = await net.fetch("/assets/" + address)
	if not r["ok"]:
		a.reason = String(r["error"])
		return a
	var code: int = int(r["code"])
	if code != CODE_OK:
		a.reason = "ассет %s: код %d" % [address, code]
		return a
	var body: PackedByteArray = r["body"]
	var ctx := HashingContext.new()
	ctx.start(HashingContext.HASH_SHA256)
	ctx.update(body)
	a.digest = "sha256-" + ctx.finish().hex_encode()
	if a.digest != address:
		a.reason = "ассет приехал битым: адрес %s, у байт %s (%d Б)" % [address, a.digest, body.size()]
		return a
	a.bytes = body
	return a
