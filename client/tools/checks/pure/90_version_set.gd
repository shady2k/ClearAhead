## VERSION SET — накопление одной версии и атомарная фиксация (sqym.6).
##
## Чистая проверка: сеть не нужна, потому что проверяется СОСТОЯНИЕ набора, а не
## провод. Ответы приходят фикстурой — тем же утиным протоколом (have/no_work/
## failed), каким их отдаёт WorldApi.Patch.
##
## Проверяются пять обязательных поведений брифа:
##   • набор не готов, пока не пришли ВСЕ части (сеть и каждая клетка);
##   • опоздавший ответ старой версии отброшен, а не применён;
##   • 204 «чистая база» — полный ответ клетки, а не ошибка;
##   • повтор того же патча ничего не меняет (идемпотентность);
##   • отказ части не делает набор готовым — видимая версия остаётся старой.
##
## Куда делся «новые чанки спрашиваются у зафиксированной версии»: это про
## АДРЕС запроса, а не про состояние набора, и проверяется сетевой половиной
## (checks/live/35_worlds.gd) — фикстура здесь согласилась бы сама с собой.
##
## Наследование через путь, а не через class_name: бегун проверок не держит
## кэша глобальных классов (шапка check_suite.gd), и глобальное имя VersionSet
## в этом контексте не существует.
extends "res://tools/check_suite.gd"

const VersionSetScript := preload("res://scripts/version_set.gd")


func _new_set():
	return VersionSetScript.new()


## Фикстурный ответ патча: повторяет форму WorldApi.Patch без сети.
class FakePatch:
	var blob := PackedByteArray()
	var base_z_m := 0.0
	var level := 0
	var cx := 0
	var cz := 0
	var _missing := false
	var reason := ""

	func failed() -> bool:
		return reason != ""

	func have() -> bool:
		return reason == "" and not _missing

	func no_work() -> bool:
		return _missing


## Отсчёты «отметки» — байты-маркер: проверке важна НЕИЗМЕННОСТЬ набора при
## повторе, а не смысл отсчётов (их разбирает TerrainMesh, своя проверка).
func _patch(level: int, cx: int, cz: int, mark: int) -> FakePatch:
	var p := FakePatch.new()
	p.level = level
	p.cx = cx
	p.cz = cz
	p.blob = PackedByteArray([mark, mark, mark])
	p.base_z_m = 100.0 + mark
	return p


func _clean_patch(level: int, cx: int, cz: int) -> FakePatch:
	var p := FakePatch.new()
	p.level = level
	p.cx = cx
	p.cz = cz
	p._missing = true
	return p


func _failed_patch(level: int, cx: int, cz: int, why: String) -> FakePatch:
	var p := FakePatch.new()
	p.level = level
	p.cx = cx
	p.cz = cz
	p.reason = why
	return p


## Фикстурный ответ сети.
class FakeNetwork:
	var data := {}
	var reason := ""

	func failed() -> bool:
		return reason != ""

	func have() -> bool:
		return reason == ""


func _network(rev: int) -> FakeNetwork:
	var n := FakeNetwork.new()
	n.data = {"elements": [], "revision": rev}
	return n


func run() -> void:
	var keys := ["0/0/0", "0/1/0", "1/0/0"]

	# --- НАБОР НЕ ГОТОВ, ПОКА НЕ ПРИШЛИ ВСЕ ЧАСТИ ---------------------------
	var s = _new_set()
	s.begin(2, keys)
	_ok("набор версии 2 начат пустым", not s.ready() and not s.failed())
	_ok("одна клетка — ещё не набор", s.accept_patch("0/0/0", _patch(0, 0, 0, 7), 2) and not s.ready())
	_ok("сеть без клеток — ещё не набор", s.accept_network(_network(1), 2) and not s.ready())
	s.accept_patch("0/1/0", _patch(0, 1, 0, 8), 2)
	_ok("две клетки и сеть — всё ещё не набор", not s.ready())
	s.accept_patch("1/0/0", _patch(1, 0, 0, 9), 2)
	_ok("последняя клетка — набор готов", s.ready())

	# --- COMMIT ОТДАЁТ НАБОР ЦЕЛИКОМ, А НЕ ПОЛОВИНУ --------------------------
	var set = s.commit()
	_ok("commit несёт версию набора", int(set["version"]) == 2)
	_ok("commit несёт сеть и все три клетки",
		not (set["network"] as Dictionary).is_empty()
		and (set["heights"] as Dictionary).size() == 3)

	# --- ОПОЗДАВШИЙ ОТВЕТ СТАРОЙ ВЕРСИИ — МУСОР ------------------------------
	var late = _new_set()
	late.begin(5, keys)
	late.accept_network(_network(1), 5)
	late.accept_patch("0/0/0", _patch(0, 0, 0, 1), 5)
	# Ответ версии 4 (старой, уже уехавшей) в набор версии 5 не входит.
	_ok("патч старой версии отброшен", not late.accept_patch("0/1/0", _patch(0, 1, 0, 42), 4))
	_ok("сеть старой версии отброшена", not late.accept_network(_network(1), 4))
	_ok("отказ старой версии тоже мусор", not late.fail("старое", 4))
	_ok("набор после мусора не готов и не упал", not late.ready() and not late.failed())
	late.accept_patch("0/1/0", _patch(0, 1, 0, 2), 5)
	late.accept_patch("1/0/0", _patch(1, 0, 0, 3), 5)
	_ok("мусор не помешал набору собраться", late.ready())
	var late_set: Dictionary = late.commit()
	var late_h := late_set["heights"] as Dictionary
	_ok("в наборе отсчёты версии 5, а не версии 4",
		late_h.has("0/1/0") and float(late_h["0/1/0"]["base_z"]) == 102.0)

	# --- 204 «ЧИСТАЯ БАЗА» — ПОЛНЫЙ ОТВЕТ, А НЕ ОШИБКА ------------------------
	var base = _new_set()
	base.begin(3, keys)
	base.accept_network(_network(1), 3)
	base.accept_patch("0/0/0", _patch(0, 0, 0, 4), 3)
	_ok("204 принят как ответ", base.accept_patch("0/1/0", _clean_patch(0, 1, 0), 3))
	_ok("204 не делает набор упавшим", not base.failed())
	base.accept_patch("1/0/0", _patch(1, 0, 0, 5), 3)
	_ok("204 не мешает набору стать готовым", base.ready())
	var base_set: Dictionary = base.commit()
	_ok("клетка 204 помечена чистой, а не пустой дырой",
		bool((base_set["clean"] as Dictionary).get("0/1/0", false))
		and not (base_set["heights"] as Dictionary).has("0/1/0")
		and (base_set["heights"] as Dictionary).size() == 2)

	# --- ПОВТОР ТОГО ЖЕ ПАТЧА НИЧЕГО НЕ МЕНЯЕТ -------------------------------
	var rep = _new_set()
	rep.begin(4, keys)
	rep.accept_network(_network(1), 4)
	for k in keys:
		rep.accept_patch(k, _patch(0, 0, 0, 6), 4)
	var before: Dictionary = rep.commit()
	rep.accept_patch("0/0/0", _patch(0, 0, 0, 6), 4)
	rep.accept_network(_network(1), 4)
	var after: Dictionary = rep.commit()
	var bh := before["heights"] as Dictionary
	var ah := after["heights"] as Dictionary
	_ok("повтор того же патча ничего не меняет",
		bh.size() == ah.size() and float(bh["0/0/0"]["base_z"]) == 106.0
		and float(ah["0/0/0"]["base_z"]) == 106.0)

	# А патч ДРУГОЙ клетки набор меняет — иначе набор не собрал бы ничего:
	# идемпотентность — про ту же клетку той же версии, а не про набор вообще.
	# commit() отдаёт ЖИВЫЕ ссылки на словари: «до» словарём не запомнить —
	# второй accept дописал бы тот же словарь. Снимаются размеры в момент.
	var other = _new_set()
	other.begin(4, keys)
	other.accept_network(_network(1), 4)
	other.accept_patch("0/0/0", _patch(0, 0, 0, 6), 4)
	var before_one: int = (other.commit()["heights"] as Dictionary).size()
	other.accept_patch("0/1/0", _patch(0, 1, 0, 6), 4)
	var after_one: int = (other.commit()["heights"] as Dictionary).size()
	_ok("патч ДРУГОЙ клетки набор меняет (идемпотентность — про ту же клетку)",
		after_one == 2 and before_one == 1)

	# --- ОТКАЗ ЧАСТИ НЕ ДЕЛАЕТ НАБОР ГОТОВЫМ --------------------------------
	var broken = _new_set()
	broken.begin(6, keys)
	broken.accept_network(_network(1), 6)
	broken.accept_patch("0/0/0", _patch(0, 0, 0, 1), 6)
	_ok("отказ клетки принят, но набор упал",
		broken.accept_patch("0/1/0", _failed_patch(0, 1, 0, "сервер ответил HTTP 404"), 6)
		and broken.failed() and not broken.ready())
	_ok("причина отказа названа", broken.failed_reason().contains("404"))
	# Остальные ответы после отказа — тоже мусор: набор уже не соберётся.
	_ok("после отказа набор не добирают",
		broken.accept_patch("1/0/0", _patch(1, 0, 0, 2), 6) and not broken.ready())

	# --- ОТВЕТ КЛЕТКИ ВНЕ НАБОРА — НАРУШЕНИЕ, А НЕ ДЫРА ----------------------
	var rogue = _new_set()
	rogue.begin(7, keys)
	rogue.accept_network(_network(1), 7)
	_ok("клетка вне набора валит набор (не смешиваются два набора)",
		rogue.accept_patch("9/9/9", _patch(9, 9, 9, 1), 7) and rogue.failed())
