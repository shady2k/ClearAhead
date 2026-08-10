extends SceneTree
## Разовый зонд: выгружает процедурные тайлы спайка мира в PNG, чтобы смотреть
## на них глазами, а не спорить по снимку сцены. Тайл — чистая функция кода,
## сцена и рендер для этого не нужны.
##
##   godot --headless --path client --script res://tools/tile_dump.gd -- --out /tmp

const World := preload("res://scripts/spike_world.gd")

func _init() -> void:
	var out := "/tmp"
	var args := OS.get_cmdline_user_args()
	var i := args.find("--out")
	if i >= 0 and i + 1 < args.size():
		out = args[i + 1]
	var w := World.new()
	w._setup_noise()
	var turf := w._turf_image()
	_stat("дернина", turf)
	turf.save_png(out.path_join("tile_turf.png"))
	for k in World.GRASS_KINDS.size():
		var img := w._grass_image(World.GRASS_KINDS[k], k)
		img.save_png(out.path_join("tile_blade_%d.png" % k))
		print("пучок %d: %dx%d" % [k, img.get_width(), img.get_height()])
	print("тайлы в %s" % out)
	quit()

func _stat(name: String, img: Image) -> void:
	var lo := 1.0
	var hi := 0.0
	var sum := 0.0
	var n := 0
	for y in range(0, img.get_height(), 2):
		for x in range(0, img.get_width(), 2):
			var v := img.get_pixel(x, y).r
			lo = minf(lo, v)
			hi = maxf(hi, v)
			sum += v
			n += 1
	print("%s: min %.3f max %.3f среднее %.3f" % [name, lo, hi, sum / float(n)])
