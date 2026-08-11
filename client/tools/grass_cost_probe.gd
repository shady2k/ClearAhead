extends SceneTree
## ИЗ ЧЕГО СКЛАДЫВАЮТСЯ 9 МКС НА ПУЧОК. Замер по фазам показал, что 98 % времени
## пересадки травы — это цикл посадки в GDScript, и что дорого ПРИНИМАТЬ
## кандидата, а не отвергать. Но «принять» — это десяток разных операций, и без
## их раздельной цены любой выбор лечения будет угадыванием: выборка шума,
## хеш, построение Basis и смешения цвета лечатся совершенно по-разному.
##
## Ставить таймер внутрь горячего цикла нельзя — Time.get_ticks_usec на каждый
## пучок сам стоит больше, чем измеряемое. Поэтому здесь микро-замер: каждая
## операция гоняется N раз подряд отдельным циклом, и из суммы вычитается
## стоимость пустого цикла.
##
##   godot --headless --path client --script res://tools/grass_cost_probe.gd

const N := 400000

var _sink := 0.0

func _initialize() -> void:
	var noise := FastNoiseLite.new()
	noise.seed = 1
	noise.frequency = 0.02
	var base := _time_empty()
	print("GRASS COST: пустой цикл %.0f нс/итерация (вычтен из всех строк ниже)" % (base * 1000.0))
	_row("выборка шума get_noise_2d", _time_noise(noise), base)
	_row("хеш _hash01 (как в посадке)", _time_hash(), base)
	_row("Basis поворота + наклон", _time_basis(), base)
	_row("Color.lerp", _time_lerp(), base)
	_row("append в Array", _time_append(), base)
	print("GRASS COST: на ОДИН принятый пучок посадка делает 4 выборки шума, "
		+ "14 хешей, 2 Basis, 5 lerp цвета и 2 append")
	quit(0)

func _row(what: String, t: float, base: float) -> void:
	print("GRASS COST: %-28s %6.0f нс" % [what, (t - base) * 1000.0])

## Все замеры возвращают МИКРОСЕКУНДЫ НА ИТЕРАЦИЮ. _sink не даёт оптимизатору
## выбросить работу целиком.
func _time_empty() -> float:
	var t0 := Time.get_ticks_usec()
	for i in N:
		_sink += 1.0
	return float(Time.get_ticks_usec() - t0) / float(N)

func _time_noise(noise: FastNoiseLite) -> float:
	var t0 := Time.get_ticks_usec()
	for i in N:
		_sink += noise.get_noise_2d(float(i) * 0.37, float(i) * 0.11)
	return float(Time.get_ticks_usec() - t0) / float(N)

## Тот же вид, что в посадке: два целых индекса и номер канала.
func _time_hash() -> float:
	var t0 := Time.get_ticks_usec()
	for i in N:
		_sink += _hash01(i, i * 7, 3)
	return float(Time.get_ticks_usec() - t0) / float(N)

func _time_basis() -> float:
	var t0 := Time.get_ticks_usec()
	for i in N:
		var b := Basis(Vector3.UP, float(i) * 0.017)
		b = Basis(Vector3(cos(float(i)), 0.0, sin(float(i))), 0.3) * b
		_sink += b.x.x
	return float(Time.get_ticks_usec() - t0) / float(N)

func _time_lerp() -> float:
	var a := Color(0.4, 0.5, 0.2)
	var b := Color(0.6, 0.6, 0.3)
	var t0 := Time.get_ticks_usec()
	for i in N:
		_sink += a.lerp(b, float(i) / float(N)).r
	return float(Time.get_ticks_usec() - t0) / float(N)

func _time_append() -> float:
	var arr := []
	var t0 := Time.get_ticks_usec()
	for i in N:
		arr.append(Transform3D.IDENTITY)
	return float(Time.get_ticks_usec() - t0) / float(N)

func _hash01(a: int, b: int, c: int) -> float:
	var h := a * 374761393 + b * 668265263 + c * 1274126177
	h = (h ^ (h >> 13)) * 1274126177
	return float((h ^ (h >> 16)) & 0xFFFFFF) / float(0x1000000)
