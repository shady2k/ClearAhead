extends SceneTree
func _process(_d: float) -> bool:
	var k := ((0 & 0xFFFFFFFF) << 32) | (0 & 0xFFFFFFFF)
	var c := ((0 & 0xFFFFFFFF) + Forest._GOLDEN)
	print("k=", k, " c=", c)
	print("mixc=", Forest._mix(c))
	print("jit=", Forest.jitter(0, 0, 0, 0))
	return true
