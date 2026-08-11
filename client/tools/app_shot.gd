extends SceneTree
## Снимок ОБОЛОЧКИ: поднимает app.tscn как в игре, при надобности проходит по
## пунктам меню, даёт кадры на асинхронную загрузку и постройку мира, снимает
## окно в PNG и выходит.
##
## Зачем отдельно от spike_shot.gd: тот снимает ОДНУ сцену мира с заданной
## камерой, а здесь проверяется ровно то, чего в спайке нет — экраны, переходы
## между ними и то, что роль действительно строит свой мир. Клиента нельзя
## проверять логом (см. шапку Makefile), а меню — тем более: кнопка, которая не
## нарисовалась, ошибки не даёт.
##
##   godot --path client --script res://tools/app_shot.gd -- \
##       --shot /tmp/app.png [--frames 120] [--step role|schema] [--offline] [--role dsp]
##
## Роль задаётся ТЕМ ЖЕ аргументом --role, который разбирает сама оболочка:
## аргументы после `--` видны обоим, и вход в роль идёт её собственной дорогой —
## после загрузки геометрии, а не поперёк неё. Здесь остаются шаги, которых у
## оболочки нет флагом: `map` — экран выбора карты, `role` — экран выбора роли,
## `schema` — уже в роли ДСП переключиться в схему.
##
## ШАГ БЫВАЕТ ДВУХТАКТНЫМ. С появлением выбора карты «Новая игра» открывает не
## роли, а каталог, и каталог приезжает по сети. Поэтому до экрана ролей два
## действия с паузой между ними: нажать «Новая игра», дождаться списка, взять
## первую карту. Одним тактом снимок заставал экран карт и молча выдавал его за
## экран ролей.

const DEFAULT_FRAMES := 120

var _shot := "/tmp/clearahead_app.png"
var _frames := DEFAULT_FRAMES
var _step := ""
var _elapsed := 0
var _app: Node
var _acted := false
var _acted2 := false

func _initialize() -> void:
	_shot = _arg_value("--shot", _shot)
	_frames = int(_arg_value("--frames", str(DEFAULT_FRAMES)))
	_step = _arg_value("--step", "")
	_app = load("res://scenes/app.tscn").instantiate()
	root.add_child(_app)

func _process(_delta: float) -> bool:
	_elapsed += 1
	# Действие — на четверти срока: геометрия приезжает асинхронно, а мир роли
	# строится почти секунду, и обоим нужен запас кадров до снимка. Второй такт
	# на половине — между ними должен успеть приехать каталог карт.
	if not _acted and _elapsed == maxi(2, _frames / 4):
		_acted = true
		_act()
	if not _acted2 and _elapsed == maxi(3, _frames / 2):
		_acted2 = true
		_act2()
	if _elapsed < _frames:
		return false
	var img := root.get_texture().get_image()
	if img == null:
		printerr("APPSHOT FAIL: окно не дало изображения")
		quit(1)
		return true
	var err := img.save_png(_shot)
	if err != OK:
		printerr("APPSHOT FAIL: снимок не записан в %s (ошибка %d)" % [_shot, err])
		quit(1)
		return true
	print("APPSHOT OK: %s, %dx%d, шаг «%s», кадров %d" % [
		_shot, img.get_width(), img.get_height(), _step, _elapsed])
	quit(0)
	return true

## Шаги подаются ЧЕРЕЗ ТЕ ЖЕ СИГНАЛЫ, по которым живут кнопки: зонд обязан
## ходить дорогой игрока, иначе он проверяет не то, что игрок нажимает.
func _act() -> void:
	match _step:
		"":
			return
		"map", "role":
			(_app.get_node("Shell") as Node).new_game_requested.emit()
		"schema":
			_app.call("_toggle_schematic")
		"pause":
			_app.call("_pause")
		"chunks":
			return  # ждёт второго такта: мира на первом ещё нет
		_:
			printerr("APPSHOT: неизвестный шаг «%s»" % _step)

## Второй такт: «дождался сети — сделай следующее».
##
## chunks здесь, а не в первом такте, по той же причине, что и выбор карты:
## отладочный слой цепляется к УЖЕ ПОСТРОЕННОМУ миру (высоту он спрашивает у
## него), а до автостарта роли мир проходит три запроса и почти секунду сборки.
## На первом такте _chunks ещё null, и переключатель молча ничего не делал бы.
func _act2() -> void:
	match _step:
		"role":
			_app.call("_choose_first_map")
		"chunks":
			_app.call("_toggle_chunk_debug")

func _arg_value(name: String, fallback: String) -> String:
	var args := OS.get_cmdline_user_args()
	var i := args.find(name)
	if i >= 0 and i + 1 < args.size():
		return args[i + 1]
	return fallback
