extends SceneTree
## ЗОНД ИМПОРТИРОВАННОЙ МОДЕЛИ. Печатает дерево узлов и габарит каждого меша в
## метрах — по этим числам модель ставится на путь.
##
## Зачем. Про чужой .glb нельзя знать ничего: ни где у него ноль, ни какая ось
## вдоль машины, ни в метрах ли он вообще. Ставить его «на глаз» по снимку —
## это подгонка вслепую, где ошибка масштаба неотличима от ошибки высоты. У
## vl80.glb вдобавок ЭКСПОРТ ПОТЕРЯЛ ТРАНСФОРМАЦИИ УЗЛОВ: кузова, тележки и
## токоприёмники лежат в одной точке друг в друге, и собирать секцию нужно
## руками — а для этого надо знать габарит каждой детали отдельно.
##
##   godot --headless --path client --script res://tools/glb_probe.gd \
##       [-- --res res://assets/vl80.glb]

func _initialize() -> void:
	var path := _arg("--res", "res://assets/vl80.glb")
	var packed := load(path) as PackedScene
	if packed == null:
		printerr("GLB PROBE: не загрузилось %s" % path)
		quit(1)
		return
	var node := packed.instantiate()
	print("GLB PROBE: %s" % path)
	_walk(node, 0, Transform3D.IDENTITY)
	quit(0)

func _walk(n: Node, depth: int, parent_xf: Transform3D) -> void:
	var xf := parent_xf
	if n is Node3D:
		xf = parent_xf * (n as Node3D).transform
	var line := "%s%s [%s]" % ["  ".repeat(depth), n.name, n.get_class()]
	if n is MeshInstance3D:
		var mesh := (n as MeshInstance3D).mesh
		if mesh != null:
			var b := xf * mesh.get_aabb()
			var tri := 0
			for s in mesh.get_surface_count():
				tri += mesh.surface_get_array_len(s)
			line += "  X[%.2f %.2f] Y[%.2f %.2f] Z[%.2f %.2f]  %d поверхн." % [
				b.position.x, b.end.x, b.position.y, b.end.y, b.position.z, b.end.z,
				mesh.get_surface_count()]
	elif n is Node3D:
		var o := xf.origin
		if o != Vector3.ZERO:
			line += "  начало (%.2f %.2f %.2f)" % [o.x, o.y, o.z]
	print(line)
	for c in n.get_children():
		_walk(c, depth + 1, xf)

func _arg(name: String, fallback: String) -> String:
	var args := OS.get_cmdline_user_args()
	var i := args.find(name)
	if i >= 0 and i + 1 < args.size():
		return args[i + 1]
	return fallback
