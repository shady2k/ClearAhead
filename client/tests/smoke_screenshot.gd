extends SceneTree
## Дымовой запуск: поднимает main.tscn как в игре, даёт ему N кадров на
## асинхронную загрузку геометрии и отрисовку, снимает окно в PNG и выходит.
## Это единственный способ увидеть «станцию целиком» на машине без человека
## за экраном — видимый критерий вехи В1 проверяется файлом, а не на глаз.
##
## Рендер настоящий, поэтому --headless НЕ годится, нужен дисплей:
##   DISPLAY=:99 godot --path client --script tests/smoke_screenshot.gd \
##       -- --shot /tmp/station.png [--frames 180]
## Аргументы после `--` уходят в клиент как обычно (--server, --geometry-file).

const DEFAULT_FRAMES := 180

var _shot := "/tmp/clearahead_smoke.png"
var _frames := DEFAULT_FRAMES
var _focus := ""
var _zoom := 0.0
var _elapsed := 0
var _main: Node

func _initialize() -> void:
	_shot = _arg_value("--shot", _shot)
	_frames = int(_arg_value("--frames", str(DEFAULT_FRAMES)))
	_focus = _arg_value("--focus", "")   # "x,y" в метрах СЕРВЕРА (Y вверх)
	_zoom = float(_arg_value("--zoom", "0"))  # экранных пикселей на метр
	_main = load("res://scenes/main.tscn").instantiate()
	root.add_child(_main)

func _process(_delta: float) -> bool:
	_elapsed += 1
	# Наводка ставится после охвата: геометрия приезжает асинхронно, и fit_to
	# зовётся уже после старта — раньше двух третей срока её бы затёрло.
	if _elapsed == _frames * 2 / 3:
		_aim()
	if _elapsed < _frames:
		return false
	var img := root.get_texture().get_image()
	if img == null:
		printerr("SMOKE FAIL: окно не дало изображения")
		quit(1)
		return true
	var err := img.save_png(_shot)
	if err != OK:
		printerr("SMOKE FAIL: снимок не записан в %s (ошибка %d)" % [_shot, err])
		quit(1)
		return true
	print("SMOKE OK: %s, %dx%d, кадров %d" % [_shot, img.get_width(), img.get_height(), _elapsed])
	quit(0)
	return true

## Наводка на участок вместо охвата всей станции: без неё крупный план не
## увидеть — станция ~1800 м в длину при ~18 м в ширину, и на общем плане
## рельсы, шпалы и балласт скрыты уровнем детализации.
func _aim() -> void:
	var camera: Camera2D = _main.get_node("Camera2D")
	if _focus != "":
		var parts := _focus.split(",")
		if parts.size() == 2:
			camera.position = Vector2(float(parts[0]), -float(parts[1]))
	if _zoom > 0.0:
		camera.zoom = Vector2.ONE * _zoom
		camera.zoom_changed.emit(_zoom)  # уровень детализации живёт на этом сигнале

func _arg_value(name: String, fallback: String) -> String:
	var args := OS.get_cmdline_user_args()
	var i := args.find(name)
	if i >= 0 and i + 1 < args.size():
		return args[i + 1]
	return fallback
