extends SceneTree
## СПАЙК-ОДНОДНЕВКА, не часть клиента. Снимок вида с рельефом и земляными
## работами. Всё, что касается высоты, ВЫДУМАНО в spike_relief.gd — контракт
## даёт z=0. Спайк отвечает на один вопрос: читается ли это железной дорогой.
##
##   DISPLAY=:1 godot --path client --script res://scripts/spike_shot.gd \
##       -- --shot /tmp/s1.png [--focus x,y] [--size м] [--azimuth deg] [--elev deg]

const Spike := preload("res://scripts/spike_relief.gd")

var _shot := "/tmp/spike.png"
var _frames := 12
var _elapsed := 0

func _initialize() -> void:
	_shot = _arg("--shot", _shot)
	_frames = int(_arg("--frames", "12"))
	var focus := Vector2.INF
	var f := _arg("--focus", "")
	if f != "":
		var parts := f.split(",")
		if parts.size() == 2:
			focus = Vector2(float(parts[0]), float(parts[1]))
	Spike.shot_focus = focus
	Spike.shot_size = float(_arg("--size", "0"))
	Spike.shot_azimuth = float(_arg("--azimuth", "45"))
	Spike.shot_elev = float(_arg("--elev", "35"))
	if _arg("--persp", "0") == "1":
		Spike.shot_persp = true
	if _arg("--hide-loco", "0") == "1":
		Spike.hide_loco = true
	root.add_child((load("res://scenes/spike_relief.tscn") as PackedScene).instantiate())

func _process(_delta: float) -> bool:
	_elapsed += 1
	if _elapsed < _frames:
		return false
	var img := root.get_texture().get_image()
	if img == null:
		printerr("SPIKE FAIL: окно не дало изображения")
		quit(1)
		return true
	if img.save_png(_shot) != OK:
		printerr("SPIKE FAIL: снимок не записан в %s" % _shot)
		quit(1)
		return true
	print("SPIKE OK: %s, %dx%d" % [_shot, img.get_width(), img.get_height()])
	quit(0)
	return true

func _arg(name: String, fallback: String) -> String:
	var args := OS.get_cmdline_user_args()
	var i := args.find(name)
	if i >= 0 and i + 1 < args.size():
		return args[i + 1]
	return fallback
