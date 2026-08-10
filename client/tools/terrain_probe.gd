extends SceneTree
## Зонд поля высот спайка мира: статистика рельефа без сцены, окна и рендера.
##
## Зачем отдельный инструмент. Поле высот — это чистая функция координат, и
## спорить о ней на глаз по снимку бессмысленно: «земля выглядит плоской» может
## значить и клэмп в коде, и слабый шум, и неудачную камеру. Зонд отвечает
## числом. Именно им были найдены оба дефекта разбора 2026-08-10: 37 % карты,
## срезанных в плоскость, и эрозия, гасившая вклад октав на 2 % вместо заявленных
## гребней и промоин.
##
## Зонд создаёт НАСТОЯЩИЙ класс мира через .new() и зовёт его же методы. Копии
## формулы рельефа здесь нет и быть не должно — копия расходится с оригиналом на
## первой правке, и мерить начинаешь не то. Ради этого настройка шумов вынесена
## в spike_world.gd отдельным методом _setup_noise().
##
## Запуск: make terrain-probe   (или godot --headless --path client
##                               --script res://tools/terrain_probe.gd)

const World := preload("res://scripts/spike_world.gd")

const STEP := 10.0               # м — шаг выборки (мельче сетки рельефа не нужно)

## ОКНО СТАНЦИИ. Статистика по всей карте (1920 x 1540 м) отвечает на вопрос
## «каков мир вообще», а владелец смотрит на другое: на кусок вокруг пути, куда
## наведена камера общего плана (make world-shot, FOCUS=240,40, SIZE=420).
## Плоское пятно на дальнем краю карты в кадр не попадёт никогда, а плоское
## пятно у горловины испортит каждый снимок. Меряем оба и не путаем.
const VIEW_X0 := 0.0
const VIEW_X1 := 520.0
const VIEW_Z0 := -220.0
const VIEW_Z1 := 220.0

func _init() -> void:
	var w: Node = World.new()
	w._setup_noise()
	_measure(w, "ВСЯ КАРТА", World.W_X0, World.W_X1, World.W_Z0, World.W_Z1)
	_measure(w, "ОКНО СТАНЦИИ", VIEW_X0, VIEW_X1, VIEW_Z0, VIEW_Z1)
	w.free()
	quit()

func _measure(w: Node, title: String, x0: float, x1: float, z0: float, z1: float) -> void:
	var raw := PackedFloat32Array()
	var fin := PackedFloat32Array()
	var slopes := PackedFloat32Array()
	var rough := PackedFloat32Array()
	var flat := 0
	var below := 0
	var x := x0
	while x < x1:
		var z := z0
		while z < z1:
			var r: float = w._land_raw(x, z)
			var h: float = w._ground_natural(x, z)
			raw.append(r)
			fin.append(h)
			if r < World.LAND_SOFT:
				below += 1
			# Уклон на шаге сетки рельефа: именно он решает, читается ли место
			# землёй или простынёй. Берётся по обеим осям, как максимум.
			var hxp: float = w._ground_natural(x + World.W_STEP, z)
			var hxm: float = w._ground_natural(x - World.W_STEP, z)
			var hzp: float = w._ground_natural(x, z + World.W_STEP)
			var hzm: float = w._ground_natural(x, z - World.W_STEP)
			var g: float = maxf(absf(hxp - h), absf(hzp - h)) / World.W_STEP
			slopes.append(g)
			if g < 0.001:
				flat += 1
			# МЕЛКАЯ ДЕТАЛЬ — ЭТО ВТОРАЯ РАЗНОСТЬ, а не первая. Уклон меряет, наклонён
			# ли склон; кривизна меряет, есть ли на нём гребни и промоины. Эрозия по
			# построению трогает именно её: она гасит мелкие октавы на крутизне и не
			# трогает на пологом. По уклону этого не увидеть — им правит старшая
			# октава, которую эрозия почти не касается.
			rough.append((absf(hxp - 2.0 * h + hxm) + absf(hzp - 2.0 * h + hzm))
				/ (2.0 * World.W_STEP))
			z += STEP
		x += STEP

	raw.sort()
	fin.sort()
	slopes.sort()
	var n := raw.size()
	print("\n=== %s === узлов %d, шаг %.0f м" % [title, n, STEP])
	print("--- сырая суша (до пола и реки) ---")
	print(_percentiles(raw))
	print("ниже LAND_SOFT (%.1f): %d (%.1f%%) — попадёт в зону сжатия"
		% [World.LAND_SOFT, below, 100.0 * below / n])
	print("--- готовый рельеф ---")
	print(_percentiles(fin))
	print("--- уклон на шаге %.0f м ---" % World.W_STEP)
	print(_percentiles(slopes))
	print("практически плоских узлов (уклон < 0.001): %d (%.2f%%)"
		% [flat, 100.0 * flat / n])
	# РЕШАЮЩАЯ ПРОВЕРКА ЭРОЗИИ. Общая кривизна её эффект смазывает: эрозия не
	# убирает деталь везде, она убирает её ИЗБИРАТЕЛЬНО — на склоне гасит, на
	# пологом оставляет. Значит мерить надо не среднее, а РАЗНИЦУ между крутыми и
	# пологими местами. Если приём работает, отношение «кривизна на крутом к
	# кривизне на пологом» заметно меньше единицы; если оно около единицы,
	# эрозии в картинке нет, сколько бы ни стоило в коде.
	var pairs := []
	for k in slopes.size():
		pairs.append([slopes[k], rough[k]])
	pairs.sort_custom(func(a, b): return a[0] < b[0])
	var quart := pairs.size() / 4
	var flat_r := 0.0
	var steep_r := 0.0
	for k in quart:
		flat_r += pairs[k][1]
		steep_r += pairs[pairs.size() - 1 - k][1]
	flat_r /= quart
	steep_r /= quart
	rough.sort()
	print("--- кривизна (мелкая деталь, её и правит эрозия) ---")
	print(_percentiles(rough))
	print("  на пологой четверти %.4f, на крутой %.4f, отношение %.3f (1.0 = эрозии нет)"
		% [flat_r, steep_r, steep_r / flat_r])

func _percentiles(a: PackedFloat32Array) -> String:
	var n := a.size()
	var q := func(p: float) -> float: return a[clampi(int(p * n), 0, n - 1)]
	var sum := 0.0
	for v in a:
		sum += v
	return "  min %.2f  p05 %.2f  p25 %.2f  med %.2f  p75 %.2f  p95 %.2f  max %.2f  среднее %.2f" % [
		a[0], q.call(0.05), q.call(0.25), q.call(0.50), q.call(0.75), q.call(0.95), a[n - 1], sum / n]
