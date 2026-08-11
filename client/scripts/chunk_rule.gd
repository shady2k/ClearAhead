## ChunkRule — правило подробности, ЦЕЛИКОМ ИЗ МАНИФЕСТА.
##
## Ни одного из этих чисел нет в коде: side_m, step_m, samples, level0_radius_m,
## max_level приезжают в /regions/{region}. Зашитая копия молча разошлась бы с
## сервером при первой смене сетки, и разошлась бы не отказом, а сплошными 204.
##
## Что клиент обязан вывести САМ (в манифесте этого нет):
##   • по какой точке клетки мерить расстояние — сервер меряет по ЦЕНТРУ чанка;
##   • с каким шагом выбирать точки оси.
## Оба соглашения записаны здесь явно, чтобы расхождение с сервером искали в
## одном месте, а не по всему клиенту.
class_name ChunkRule
extends RefCounted

var side_m: int = 0
var step_m: int = 0
var samples: int = 0
var level0_radius_m: float = 0.0
var max_level: int = 0


static func from_manifest(chunks: Dictionary) -> ChunkRule:
	var r := ChunkRule.new()
	r.side_m = int(chunks.get("side_m", 0))
	r.step_m = int(chunks.get("step_m", 0))
	r.samples = int(chunks.get("samples", 0))
	r.level0_radius_m = float(chunks.get("level0_radius_m", 0.0))
	r.max_level = int(chunks.get("max_level", 0))
	return r


func rule_text() -> String:
	return "side=%d м step=%d м samples=%d r0=%.0f м max_level=%d" % [
		side_m, step_m, samples, level0_radius_m, max_level]


func valid() -> bool:
	return side_m > 0 and step_m > 0 and samples > 1 and level0_radius_m > 0.0 and max_level >= 0


func side_of(level: int) -> float:
	return float(side_m << level)


func step_of(level: int) -> float:
	return float(step_m << level)


func radius_of(level: int) -> float:
	return level0_radius_m * pow(2.0, float(level))


## level_for — уровень по расстоянию до оси. -1 значит «дальше интереса»:
## там чанков не хранится, и это не сбой.
func level_for(dist_m: float) -> int:
	if dist_m < 0.0:
		return -1
	var r := level0_radius_m
	for level in range(0, max_level + 1):
		if dist_m < r:
			return level
		r *= 2.0
	return -1


## candidates — адреса чанков, которые ИМЕЮТ ПРАВО существовать по правилу.
##
## Перебор ограничен полосой уровня: клетка уровня L обязана лежать ближе
## radius_of(L) к оси, а radius_of(L) / side_of(L) — величина постоянная, отчего
## обход каждого уровня остаётся размером с габарит пути плюс пара клеток.
## Иначе пришлось бы перебирать всю область охвата на каждом уровне.
func candidates(axis: PackedVector2Array, bbox: Rect2) -> Array:
	var out: Array = []
	if axis.is_empty():
		return out
	for level in range(0, max_level + 1):
		var side := side_of(level)
		var reach := radius_of(level)
		var c0x := int(floor((bbox.position.x - reach) / side))
		var c1x := int(floor((bbox.end.x + reach) / side))
		var c0z := int(floor((bbox.position.y - reach) / side))
		var c1z := int(floor((bbox.end.y + reach) / side))
		for cx in range(c0x, c1x + 1):
			for cz in range(c0z, c1z + 1):
				var centre := Vector2(float(cx) * side + side * 0.5, float(cz) * side + side * 0.5)
				var d := nearest_axis_dist(axis, centre)
				if level_for(d) != level:
					continue
				out.append({"level": level, "cx": cx, "cz": cz})
	return out


static func nearest_axis_dist(axis: PackedVector2Array, p: Vector2) -> float:
	var best := INF
	for a in axis:
		var d := p.distance_squared_to(a)
		if d < best:
			best = d
	return sqrt(best)
