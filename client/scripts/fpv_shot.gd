extends SceneTree
## СНИМОК С ВЫСОТЫ ГЛАЗ. Отдельный скрипт, а не флаг у spike_shot.gd: там камера
## задаётся фокусом, азимутом и высотой над плоскостью пути — то есть тем, чего у
## человека нет. Здесь камера ВООБЩЕ НЕ ЗАДАЁТСЯ: её ставит голова машиниста, а
## задаётся ПОЛОЖЕНИЕ ЧЕЛОВЕКА в координатах пути.
##
##   godot --rendering-method forward_plus --path client \
##       --script res://scripts/fpv_shot.gd -- --shot /tmp/fpv.png \
##       [--u 62] [--lat -6] [--yaw 0] [--pitch -3] [--mode 1]
##
## --mode: 1 — от первого лица (по умолчанию), 2 — от третьего (видно самого
## человека), 0 — обзорная камера родителя.
##
## Оси, чтобы не подбирать знаки заново: --lat отрицательная — сторона платформы;
## --yaw отсчитывается ОТ НАПРАВЛЕНИЯ ПУТИ (0 — вдоль него), плюс — влево; отметку
## под ногами ищет луч, поэтому --lat 0 ставит человека на путь, а --lat -3.2 на
## платформу, и ни то, ни другое не надо задавать высотой.
##
## КАДРОВ ЖДЁМ НЕ РАДИ РЕНДЕРА, А РАДИ ФИЗИКИ. Человек ставится на отметку из
## поля высот, а под ним может оказаться балласт или плита — тогда первые кадры
## он оседает. Двадцать кадров это треть секунды: больше, чем нужно любому
## оседанию, и меньше, чем заметно на времени сборки.

const FPV := preload("res://scripts/spike_fpv.gd")

var _shot := "/tmp/fpv.png"
var _frames := 20
var _elapsed := 0

func _initialize() -> void:
	_shot = _arg("--shot", _shot)
	_frames = int(_arg("--frames", str(_frames)))
	FPV.fpv_u = float(_arg("--u", str(FPV.fpv_u)))
	FPV.fpv_lat = float(_arg("--lat", str(FPV.fpv_lat)))
	FPV.fpv_yaw = float(_arg("--yaw", str(FPV.fpv_yaw)))
	FPV.fpv_pitch = float(_arg("--pitch", str(FPV.fpv_pitch)))
	FPV.fpv_mode = int(_arg("--mode", str(FPV.fpv_mode)))
	if _arg("--hide-loco", "0") == "1":
		FPV.hide_loco = true
	root.add_child((load("res://scenes/spike_fpv.tscn") as PackedScene).instantiate())

func _process(_delta: float) -> bool:
	_elapsed += 1
	if _elapsed < _frames:
		return false
	var img := root.get_texture().get_image()
	if img == null:
		printerr("FPV FAIL: окно не дало изображения")
		quit(1)
		return true
	if img.save_png(_shot) != OK:
		printerr("FPV FAIL: снимок не записан в %s" % _shot)
		quit(1)
		return true
	print("FPV OK: %s, %dx%d" % [_shot, img.get_width(), img.get_height()])
	quit(0)
	return true

func _arg(name: String, fallback: String) -> String:
	var args := OS.get_cmdline_user_args()
	var i := args.find(name)
	if i >= 0 and i + 1 < args.size():
		return args[i + 1]
	return fallback
