extends SceneTree
## Разовая проверка: успевает ли NoiseTexture2D посчитаться к моменту съёмки.

func _init() -> void:
	var noise := FastNoiseLite.new()
	noise.seed = 0x6A55
	noise.noise_type = FastNoiseLite.TYPE_CELLULAR
	noise.frequency = 1.0 / 30.0
	var nt := NoiseTexture2D.new()
	nt.width = 512
	nt.height = 512
	nt.seamless = true
	nt.noise = noise
	print("сразу после создания: get_image() == null -> %s" % [nt.get_image() == null])
	# Синхронный путь — тот, на который надо переходить.
	var img := noise.get_seamless_image(512, 512)
	print("get_seamless_image: %dx%d формат %d" % [img.get_width(), img.get_height(), img.get_format()])
	var lo := 1.0
	var hi := 0.0
	var sum := 0.0
	for y in range(0, 512, 8):
		for x in range(0, 512, 8):
			var v := img.get_pixel(x, y).r
			lo = minf(lo, v)
			hi = maxf(hi, v)
			sum += v
	print("яркость: min %.3f max %.3f среднее %.3f" % [lo, hi, sum / (64 * 64)])
	quit()
