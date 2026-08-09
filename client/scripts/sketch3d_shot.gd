extends SceneTree
## Снимок эскиза W3D. Godot --headless не рисует, нужен дисплей:
##
##   DISPLAY=:1 godot --path client --script res://scripts/sketch3d_shot.gd \
##       -- --shot /tmp/sk3d_1.png [--focus x,y] [--size м] [--azimuth deg] \
##          [--elev deg] [--frames N] [--geometry <путь к эталону>]
##
## По образцу client/tests/smoke_screenshot.gd: дать сцене кадры на сборку и
## отрисовку, снять окно в PNG, выйти. size — половина видимой высоты в метрах
## (0 — автофит по всей фикстуре); azimuth — азимут камеры от +X в градусах;
## elev — угол над плоскостью пути (89 ≈ сверху).

const Sketch := preload("res://scripts/sketch3d.gd")

var _shot := "/tmp/sk3d.png"
var _frames := 10
var _focus := ""
var _size := 0.0
var _azimuth := 45.0
var _elev := 35.0
var _elapsed := 0

func _initialize() -> void:
	_shot = _arg("--shot", _shot)
	_frames = int(_arg("--frames", "10"))
	_focus = _arg("--focus", "")
	_size = float(_arg("--size", "0"))
	_azimuth = float(_arg("--azimuth", "45"))
	_elev = float(_arg("--elev", "35"))
	var geo := _arg("--geometry", "")
	if geo != "":
		Sketch.geometry_path = geo
	var focus := Vector2.INF
	if _focus != "":
		var parts := _focus.split(",")
		if parts.size() == 2:
			focus = Vector2(float(parts[0]), float(parts[1]))
	Sketch.shot_focus = focus
	Sketch.shot_size = _size
	Sketch.shot_azimuth = _azimuth
	Sketch.shot_elev = _elev
	root.add_child((load("res://scenes/sketch3d.tscn") as PackedScene).instantiate())

func _process(_delta: float) -> bool:
	_elapsed += 1
	if _elapsed < _frames:
		return false
	var img := root.get_texture().get_image()
	if img == null:
		printerr("SK3D FAIL: окно не дало изображения")
		quit(1)
		return true
	var err := img.save_png(_shot)
	if err != OK:
		printerr("SK3D FAIL: снимок не записан в %s (ошибка %d)" % [_shot, err])
		quit(1)
		return true
	print("SK3D OK: %s, %dx%d, кадров %d" % [_shot, img.get_width(), img.get_height(), _elapsed])
	quit(0)
	return true

func _arg(name: String, fallback: String) -> String:
	var args := OS.get_cmdline_user_args()
	var i := args.find(name)
	if i >= 0 and i + 1 < args.size():
		return args[i + 1]
	return fallback
