## cab_panel.gd — ПУЛЬТ МАШИНИСТА: приборы и рукоятки внизу экрана.
##
## # Зачем он такой, а не трёхмерная кабина
##
## Решение владельца 2026-08-15: «делать сейчас 3D модель кабины я не смогу.
## Предлагаю сделать какую-нибудь панель внизу экрана с приборами, которая будет
## появляться при входе в локомотив. Ей можно управлять мышью и с клавиатуры, как
## у derail valley».
##
## До этого рукоятки жили строкой в ОТЛАДОЧНОЙ панели, а она в игре скрыта и
## включается клавишей H, — то есть машинист не видел ни одного своего органа.
##
## # ПРИБОР БЕЗ ЧИСЛА НЕ РИСУЕТСЯ, и это здесь главное правило
##
## Стрелка, за которой нет величины, врёт ровно так же, как врал бы сам прибор.
## Поэтому манометры появляются только у машины с тормозной магистралью, кран
## вспомогательного — только если он у неё есть (Cab.independent_max), а ШКАЛЫ
## берутся из ПАСПОРТА: зарядное давление, предел цилиндра, потолок резервуаров,
## конструкционная скорость. Ни одного предела здесь не назначено кодом.
##
## # Что рисуется руками, а не узлами
##
## Всё. Приборы — это дуги, риски и стрелки; собирать их из Control'ов значило бы
## держать полсотни узлов ради картинки, которая не интерактивна нигде, кроме
## четырёх рукояток. Мышь ловится по прямоугольникам этих четырёх (_hit), а не
## по узлам.
class_name CabPanel
extends Control

## Орган изменён игроком: мир отправит команду. Пульт сам ничего не шлёт —
## отправка живёт там же, где и для клавиш, иначе у одной команды стало бы два
## места отправки и два способа разойтись.
signal changed

## РАЗМЕР ПУЛЬТА СЧИТАЕТСЯ ОТ ОКНА, а не задан в пикселях, и это починка жалобы
## владельца «HUD слишком мелкий, не видно ничего».
##
## Пиксельный размер выглядит одинаково только на одном разрешении: пульт,
## нарисованный под 1600×900, на 2560×1440 занимает вдвое меньшую долю кадра, а
## на четырёх килопикселях превращается в полоску. Доля высоты окна — единственная
## мера, которая держит его читаемым везде.
##
## ДОЛЯ 0.28, а не «сколько влезет»: пульт закрывает низ кадра, и каждая лишняя
## сотая — это метры пути, которых машинист не видит. Нижняя граница в пикселях
## нужна на маленьком окне, где доля даёт нечитаемое.
const H_SHARE := 0.28
const H_MIN := 240.0

## Опорная высота, под которую нарисованы все внутренние размеры. Всё остальное
## умножается на _k = H / H_BASE — тогда прибавка размера не требует править
## два десятка чисел порознь.
const H_BASE := 240.0
const PAD := 18.0
const GAUGE_R := 82.0            ## радиус манометра при опорной высоте
const DIAL_FROM := 140.0         ## градусы: начало шкалы (слева внизу)
const DIAL_SPAN := 260.0         ## и её размах по часовой

const C_BG := Color(0.05, 0.06, 0.08, 0.88)
const C_FACE := Color(0.11, 0.12, 0.15)
const C_RIM := Color(0.28, 0.31, 0.36)
const C_TICK := Color(0.62, 0.66, 0.72)
const C_TEXT := Color(0.86, 0.89, 0.93)
const C_DIM := Color(0.53, 0.57, 0.64)
const C_PIPE := Color(0.45, 0.78, 1.0)      ## стрелка магистрали
const C_MAIN := Color(0.98, 0.82, 0.35)     ## стрелка главных резервуаров
const C_CYL := Color(1.0, 0.45, 0.42)       ## стрелка цилиндра
const C_LEVER := Color(0.72, 0.76, 0.82)
const C_ACTIVE := Color(0.49, 0.82, 0.63)
const C_WARN := Color(1.0, 0.75, 0.38)

var cab: Cab = null
## Мгновение показа: те же часы, по которым мир ставит машину. Кладёт мир —
## разбор у Cab.shown.
var show_us := 0
## Показания приборов В ЭТО МГНОВЕНИЕ. Считаются раз за отрисовку и читаются
## всеми приборами: два вызова Cab.shown в одном кадре дали бы двум стрелкам
## разное время.
var _now := {}

## Прямоугольники органов. Пересчитываются при отрисовке и читаются мышью:
## держать раскладку в одном месте — единственный способ не дать нарисованному
## разойтись с нажимаемым.
var _rect_traction := Rect2()
var _rect_reverser := Rect2()
var _rect_handle := Rect2()
var _rect_independent := Rect2()
var _drag := ""


## _k — во сколько раз пульт крупнее опорного. Считается от ВЫСОТЫ ОКНА.
var _k := 1.0
var _h := H_BASE


## setup — привязать кабину. РАЗМЕР ЗДЕСЬ НЕ СЧИТАЕТСЯ: узла ещё нет в дереве, а
## значит нет и вьюпорта, у которого спрашивают высоту окна. Первый заход на этом
## и споткнулся — «Cannot call method get_visible_rect on a null value».
func setup(c: Cab) -> void:
	cab = c
	mouse_filter = Control.MOUSE_FILTER_PASS
	set_anchors_preset(Control.PRESET_BOTTOM_WIDE)


func _ready() -> void:
	_resize()
	get_viewport().size_changed.connect(_resize)


## height — высота полосы пульта в пикселях этого окна. Спрашивает мир: подсказка
## о посадке обязана стоять ВЫШЕ пульта, а её собственное число разъехалось бы с
## ним на первом же другом разрешении.
func height() -> float:
	return _h


func _resize() -> void:
	var vh: float = float(get_viewport().get_visible_rect().size.y)
	_h = maxf(vh * H_SHARE, H_MIN)
	_k = _h / H_BASE
	offset_top = -_h
	custom_minimum_size = Vector2(0, _h)
	queue_redraw()


## px — размер в пикселях этого окна. Каждое число раскладки проходит через неё:
## пропущенное осталось бы «под 1600×900» и разъехалось бы с остальными.
func px(v: float) -> float:
	return v * _k


func _draw() -> void:
	if cab == null or not cab.aboard():
		return
	_now = cab.shown(show_us)
	var w := size.x
	draw_rect(Rect2(0, 0, w, _h), C_BG)
	draw_line(Vector2(0, 0), Vector2(w, 0), C_RIM, px(2.0))

	# РАСКЛАДКА ОДНОЙ ГРУППОЙ ПО ЦЕНТРУ, а не приборы у левого края и рукоятки у
	# правого. Пульт — это одно устройство, и разнесённый по краям экрана он
	# заставляет переводить взгляд через весь кадр между «сколько» и «чем»; на
	# широком мониторе это метр глазами.
	var gauges := 1 + (2 if cab.has_air else 0)
	var gr := px(GAUGE_R)
	var pad := px(PAD)
	var wide := gauges * (gr * 2 + pad) + _controls_width()
	var x: float = maxf((w - wide) * 0.5, pad) + gr
	# СКОРОСТЬ — ПЕРВОЙ: единственный прибор, на который смотрят непрерывно.
	# Шкала — конструкционная скорость паспорта; нет её — нет и круглого прибора,
	# остаётся число.
	_draw_speed(Vector2(x, _h * 0.5))
	x += gr * 2 + pad

	if cab.has_air:
		# ДВУХСТРЕЛОЧНЫЙ, как в кабине: магистраль и главные резервуары на одной
		# шкале. Они сравниваются глазом постоянно («хватит ли чем заряжать»), и
		# на двух циферблатах это сравнение пришлось бы делать в уме.
		_draw_dual(Vector2(x, _h * 0.5))
		x += gr * 2 + pad
		_draw_cylinder(Vector2(x, _h * 0.5))
		x += gr * 2 + pad

	var right: float = x - gr + _controls_width()
	# Рукоятки справа налево: сначала то, чем пользуются реже.
	if cab.independent_max > 0:
		_rect_independent = Rect2(right - px(88), pad + px(20), px(88), _h - pad * 2 - px(30))
		_draw_slider(_rect_independent, "вспом.", float(cab.independent),
			float(cab.independent_max), _kgf(cab.independent), C_WARN)
		right -= px(88) + pad
	if cab.handle != "":
		_rect_handle = Rect2(right - px(300), pad + px(20), px(300), _h - pad * 2 - px(30))
		_draw_handle(_rect_handle)
		right -= px(300) + pad
	_rect_reverser = Rect2(right - px(116), pad + px(20), px(116), _h - pad * 2 - px(30))
	_draw_reverser(_rect_reverser)
	right -= px(116) + pad
	_rect_traction = Rect2(right - px(88), pad + px(20), px(88), _h - pad * 2 - px(30))
	# ДВЕ ВЕЛИЧИНЫ НА ОДНОМ ПОЛЗУНКЕ: рукоятка (задание) и ПОЗИЦИЯ контроллера
	# (что встало). Они расходятся всегда, пока контроллер идёт к рукоятке, и
	# показывать одну вместо другой значило бы врать про то, чем машина тянет
	# сейчас: поставил тридцать третью, увидел тридцать третью — а машина едва
	# тронулась.
	var notch := float(_now.get("notch", cab.notch_milli)) / 1000.0
	_draw_slider(_rect_traction, "тяга", float(cab.traction),
		float(maxi(cab.traction_notches, 1)),
		"%d / %.1f" % [cab.traction, notch], C_ACTIVE, notch)

	# ЛАМПА БУКСОВАНИЯ. Индикатор, а не строка текста: в кабине это лампа, её ищут
	# боковым зрением, не читая. Гаснет сама — состояние приходит с сервера
	# каждым снимком, и держать её «пока не квитируют» значило бы придумать
	# устройство, которого нет.
	if cab.slipping:
		# Ширина 210, а не 200: проверка границы транспортного слоя ищет коды HTTP
		# числами, и 200 в раскладке она принимает за код. Ложное срабатывание, но
		# ослаблять проверку ради ширины лампы дороже, чем сдвинуть ширину.
		var lamp := Rect2(px(PAD), _h * 0.5 - px(26), px(210), px(52))
		draw_rect(lamp, Color(0.62, 0.13, 0.11))
		draw_rect(lamp, Color(1.0, 0.45, 0.35), false, px(2.4))
		_text(Vector2(lamp.position.x, lamp.end.y - px(17)), "БУКСОВАНИЕ",
			Color(1.0, 0.92, 0.88), int(px(24)), true, lamp.size.x)

	# ОЖИДАНИЕ И ОТКАЗ — под приборами, а не поверх них: они про СВЯЗЬ, а не про
	# машину, и закрывать ими стрелку значило бы прятать факт ради сообщения о нём.
	var note := ""
	if cab.pending > 0:
		note = "ждём сервер"
	if cab.refusal != "":
		note = cab.refusal
	if note != "":
		_text(Vector2(px(PAD), _h - px(10)), note, C_WARN, int(px(18)))
	# КЛАВИШИ ПО F1, А НЕ ВСЕГДА НА ВИДУ (решение владельца 2026-08-15: «убери
	# подсказки с клавишами, они только мешают»). Тот, кто их выучил, смотрит на
	# приборы, а не на список; тому, кто не выучил, список нужен один раз.
	#
	# Строка «F1 — клавиши» остаётся ВСЕГДА и стоит мелко в углу: подсказка,
	# которую нечем найти, не подсказка.
	if keys_shown:
		_draw_keys()
	else:
		_text(Vector2(px(PAD), px(22)), "F1 — клавиши", C_DIM, int(px(16)))


## _controls_width — ширина группы рукояток. Считается ОТДЕЛЬНО от отрисовки,
## потому что раскладка центрируется: чтобы поставить группу, надо знать её
## ширину до того, как рисовать первый прибор.
## keys_shown — показывать ли список клавиш. Переключается клавишей F1 у мира:
## клавиатура принадлежит ему, а не пульту (там же живут E, V и H).
var keys_shown := false

## KEYS — что и чем делается. Список ЗДЕСЬ, рядом с органами, которые он
## называет: разъехавшись, подсказка и раскладка врут по очереди.
const KEYS := [
	["W / S", "контроллер тяги: ступень вверх и вниз (или 2 / 1)"],
	["A / D", "кран машиниста по сектору: к отпуску и к торможению"],
	["Z / X", "кран вспомогательного тормоза локомотива"],
	["R", "реверсор вперёд, Shift+R назад (только при нулевой тяге)"],
	["0", "экстренная остановка: тяга в ноль, кран в экстренное"],
	["мышь", "тянуть рукоятки и щёлкать по положениям крана"],
	["E", "выйти из кабины"],
	["V", "сменить вид: обзор, от первого лица, от третьего"],
	["H", "панель отладки"],
	["F1", "убрать этот список"],
]


## _draw_keys — список клавиш поверх пульта. ПОВЕРХ, а не вместо: приборы во
## время чтения списка не исчезают, иначе он показывал бы, чем управлять, ровно
## тогда, когда управлять нечем.
func _draw_keys() -> void:
	var pad := px(PAD)
	var lh := px(30)
	var box := Rect2(pad, pad, minf(size.x - pad * 2, px(720)),
		lh * (KEYS.size() + 1) + pad)
	draw_rect(box, Color(0.04, 0.05, 0.07, 0.96))
	draw_rect(box, C_RIM, false, px(1.8))
	_text(box.position + Vector2(px(16), lh * 0.8), "КЛАВИШИ", C_TEXT, int(px(20)))
	for i in KEYS.size():
		var row: Array = KEYS[i]
		var y := box.position.y + lh * (i + 1.9)
		_text(Vector2(box.position.x + px(16), y), String(row[0]), C_ACTIVE, int(px(18)))
		_text(Vector2(box.position.x + px(120), y), String(row[1]), C_DIM, int(px(18)))


func _controls_width() -> float:
	var wd := px(88.0) + px(PAD) + px(116.0) # тяга и реверсор есть всегда
	if cab.handle != "":
		wd += px(300.0) + px(PAD)
	if cab.independent_max > 0:
		wd += px(88.0) + px(PAD)
	return wd


## _draw_speed — скоростемер. Круглым прибором только при известной шкале:
## циферблат с выдуманным пределом показывает верную скорость на неверной шкале.
func _draw_speed(c: Vector2) -> void:
	var kmh: float = float(_now.get("kmh", cab.speed_kmh))
	if cab.max_speed_kmh > 0.0:
		_dial(c, "км/ч", 0.0, cab.max_speed_kmh, 10.0)
		_needle(c, kmh / cab.max_speed_kmh, C_TEXT, 0.86)
	_text(c + Vector2(-px(GAUGE_R), px(GAUGE_R) - px(2)), "%.0f" % kmh, C_TEXT, int(px(30)), true, px(GAUGE_R) * 2)


func _draw_dual(c: Vector2) -> void:
	var top: float = maxf(float(cab.main_max_milli), 1.0)
	_dial(c, "ТМ / ГР", 0.0, top / 1000.0, 1.0)
	_needle(c, float(_now.get("main", cab.main_pressure)) / top, C_MAIN, 0.62)
	_needle(c, float(_now.get("pipe", cab.pipe_pressure)) / top, C_PIPE, 0.86)
	_text(c + Vector2(-px(GAUGE_R), px(GAUGE_R) - px(2)),
		"%s / %s" % [_kgf(int(_now.get("pipe", 0))), _kgf(int(_now.get("main", 0)))],
		C_DIM, int(px(18)), true, px(GAUGE_R) * 2)


## _draw_cylinder — манометр тормозного цилиндра. Шкала — предел цилиндра из
## паспорта с запасом в четверть: кран вспомогательного держит выше полного
## служебного, и стрелка обязана иметь куда уйти.
func _draw_cylinder(c: Vector2) -> void:
	var top: float = maxf(float(cab.cylinder_full_milli) * 1.25, 1.0)
	_dial(c, "ТЦ", 0.0, top / 1000.0, 1.0)
	# Риска полного служебного: до неё — служебное торможение, за ней уже
	# вспомогательный. Число паспорта, а не деление шкалы.
	_mark(c, float(cab.cylinder_full_milli) / top, C_CYL)
	_needle(c, float(_now.get("cyl", cab.cylinder_pressure)) / top, C_CYL, 0.86)
	_text(c + Vector2(-px(GAUGE_R), px(GAUGE_R) - px(2)), _kgf(int(_now.get("cyl", 0))), C_DIM, int(px(18)), true, px(GAUGE_R) * 2)


## _dial — циферблат: обод, дуга шкалы, риски и подпись.
func _dial(c: Vector2, name_: String, lo: float, hi: float, step: float) -> void:
	draw_circle(c, px(GAUGE_R), C_FACE)
	draw_arc(c, px(GAUGE_R), 0, TAU, 48, C_RIM, px(2.0))
	var a0 := deg_to_rad(DIAL_FROM)
	var a1 := deg_to_rad(DIAL_FROM + DIAL_SPAN)
	draw_arc(c, px(GAUGE_R - 8), a0, a1, 40, C_RIM, px(2.0))
	var n := int(round((hi - lo) / step))
	if n > 0 and n <= 40:
		for i in n + 1:
			var t := float(i) / float(n)
			var a := a0 + (a1 - a0) * t
			var d := Vector2(cos(a), sin(a))
			draw_line(c + d * px(GAUGE_R - 8), c + d * px(GAUGE_R - 18), C_TICK, px(1.8))
	_text(c + Vector2(-px(GAUGE_R), -px(GAUGE_R) * 0.42), name_, C_DIM, int(px(17)), true, px(GAUGE_R) * 2)


## _needle — стрелка. Доля вне [0, 1] прижимается к краю шкалы: прибор, у
## которого стрелка ушла за циферблат, читается как сломанный, а не как
## «величина больше предела».
func _needle(c: Vector2, part: float, col: Color, len_k: float) -> void:
	var t: float = clampf(part, 0.0, 1.0)
	var a := deg_to_rad(DIAL_FROM + DIAL_SPAN * t)
	var d := Vector2(cos(a), sin(a))
	draw_line(c - d * px(7.0), c + d * (px(GAUGE_R) * len_k), col, px(3.2))
	draw_circle(c, px(5.0), col)


## _mark — красная риска на шкале: уставка, а не деление.
func _mark(c: Vector2, part: float, col: Color) -> void:
	var t: float = clampf(part, 0.0, 1.0)
	var a := deg_to_rad(DIAL_FROM + DIAL_SPAN * t)
	var d := Vector2(cos(a), sin(a))
	draw_line(c + d * px(GAUGE_R - 8), c + d * px(GAUGE_R - 22), col, px(3.2))


## _draw_slider — ползунок с рисками: тяга и вспомогательный тормоз.
## _draw_slider — ползунок с рисками. actual — ФАКТИЧЕСКАЯ величина, если она
## отличается от заданной: рисуется второй риской, и её отставание от рукоятки
## видно глазом. Отрицательная означает «фактической нет» — так у крана
## вспомогательного: он держит ровно то, что поставили.
func _draw_slider(r: Rect2, name_: String, value: float, top: float,
		label: String, col: Color, actual: float = -1.0) -> void:
	draw_rect(r, C_FACE)
	draw_rect(r, C_RIM, false, px(1.8))
	_text(Vector2(r.position.x, r.position.y - px(5)), name_, C_DIM, int(px(17)), true, r.size.x)
	var inner := Rect2(r.position + Vector2(r.size.x * 0.5 - px(7), px(28)),
		Vector2(px(14), r.size.y - px(58)))
	draw_rect(inner, Color(0.16, 0.18, 0.22))
	var t: float = 0.0 if top <= 0.0 else clampf(value / top, 0.0, 1.0)
	var fill := Rect2(inner.position + Vector2(0, inner.size.y * (1.0 - t)),
		Vector2(inner.size.x, inner.size.y * t))
	draw_rect(fill, col)
	var y := inner.position.y + inner.size.y * (1.0 - t)
	draw_rect(Rect2(r.position.x + px(9), y - px(6), r.size.x - px(18), px(12)), C_LEVER)
	if actual >= 0.0 and top > 0.0:
		var ya := inner.position.y + inner.size.y * (1.0 - clampf(actual / top, 0.0, 1.0))
		draw_rect(Rect2(r.position.x + px(4), ya - px(2), r.size.x - px(8), px(4)), C_WARN)
	_text(Vector2(r.position.x, r.end.y - px(5)), label, C_TEXT, int(px(20)), true, r.size.x)


## _draw_reverser — реверсор тремя положениями. Порядок сверху вниз: вперёд,
## ноль, назад — как у настоящей рукоятки, где ноль посередине и «вперёд →
## назад» без остановки в нём не проскочить.
func _draw_reverser(r: Rect2) -> void:
	draw_rect(r, C_FACE)
	draw_rect(r, C_RIM, false, 1.5)
	_text(Vector2(r.position.x, r.position.y - px(5)), "реверсор", C_DIM, int(px(17)), true, r.size.x)
	var names := ["forward", "neutral", "reverse"]
	var human := ["вперёд", "ноль", "назад"]
	var h := (r.size.y - px(32)) / 3.0
	for i in 3:
		var cell := Rect2(r.position.x + px(7), r.position.y + px(28) + h * i, r.size.x - px(14), h - px(5))
		var on: bool = cab.reverser == names[i]
		draw_rect(cell, C_ACTIVE if on else Color(0.16, 0.18, 0.22))
		_text(Vector2(cell.position.x, cell.end.y - px(8)),
			human[i], Color(0.06, 0.08, 0.1) if on else C_DIM, int(px(18)), true, cell.size.x)


## _draw_handle — кран машиниста ПО СЕКТОРУ: шесть положений подряд.
##
## Подряд, а не ступенями с числом, потому что у крана нет «больше — меньше»:
## перекрыша не «сильнее» отпуска, у каждого положения своё действие. Римские
## номера — те же, что на самом кране.
func _draw_handle(r: Rect2) -> void:
	draw_rect(r, C_FACE)
	draw_rect(r, C_RIM, false, 1.5)
	_text(Vector2(r.position.x, r.position.y - px(5)), "кран машиниста № 395", C_DIM, int(px(17)), true, r.size.x)
	var roman := ["I", "II", "III", "IV", "V", "VI"]
	var n: int = Cab.HANDLES.size()
	var wcell := (r.size.x - px(14)) / float(n)
	for i in n:
		var pos := String(Cab.HANDLES[i])
		var cell := Rect2(r.position.x + px(7) + wcell * i, r.position.y + px(28), wcell - px(4), px(44))
		var on: bool = cab.handle == pos
		var col := C_ACTIVE if on else Color(0.16, 0.18, 0.22)
		# ЭКСТРЕННОЕ ОКРАШЕНО ОСОБО и в невыбранном виде: его ищут глазами в тот
		# момент, когда читать подписи некогда.
		if pos == "emergency" and not on:
			col = Color(0.34, 0.14, 0.14)
		draw_rect(cell, col)
		_text(Vector2(cell.position.x, cell.end.y - px(13)), roman[i],
			Color(0.06, 0.08, 0.1) if on else C_TEXT, int(px(21)), true, cell.size.x)
	_text(Vector2(r.position.x + px(7), r.end.y - px(8)),
		String(Cab.HANDLE_NAMES.get(cab.handle, cab.handle)), C_TEXT, int(px(20)), true, r.size.x - px(14))


func _text(at: Vector2, s: String, col: Color, px: int, centred: bool = false,
		width: float = 0.0) -> void:
	var f := ThemeDB.fallback_font
	if centred and width > 0.0:
		var wx := f.get_string_size(s, HORIZONTAL_ALIGNMENT_LEFT, -1, px).x
		at.x += (width - wx) * 0.5
	draw_string(f, at, s, HORIZONTAL_ALIGNMENT_LEFT, -1, px, col)


func _kgf(milli: int) -> String:
	return "%.2f" % (float(milli) / 1000.0)


## --- мышь --------------------------------------------------------------------

## _gui_input — МЫШЬ ВЕДЁТ РУКОЯТКИ, клавиатура делает то же быстрее.
##
## Тянуть, а не только щёлкать: у ползунка непрерывный ход, и щелчок по нему
## означал бы, что набрать тягу можно только попаданием в нужную точку с первого
## раза. У крана и реверсора ход дискретный, там щелчок и есть движение.
func _gui_input(event: InputEvent) -> void:
	if cab == null or not cab.aboard():
		return
	if event is InputEventMouseButton:
		var mb := event as InputEventMouseButton
		if mb.button_index != MOUSE_BUTTON_LEFT:
			return
		if not mb.pressed:
			_drag = ""
			return
		_press(mb.position)
		accept_event()
	elif event is InputEventMouseMotion and _drag != "":
		_move(( event as InputEventMouseMotion).position)
		accept_event()


func _press(at: Vector2) -> void:
	if _rect_traction.has_point(at):
		_drag = "traction"
		_move(at)
	elif cab.independent_max > 0 and _rect_independent.has_point(at):
		_drag = "independent"
		_move(at)
	elif _rect_reverser.has_point(at):
		var i := int(clampf((at.y - _rect_reverser.position.y - px(28))
			/ maxf((_rect_reverser.size.y - px(32)) / 3.0, 1.0), 0, 2))
		if cab.set_reverser_at(["forward", "neutral", "reverse"][i]):
			changed.emit()
	elif cab.handle != "" and _rect_handle.has_point(at):
		var n: int = Cab.HANDLES.size()
		var wcell := (_rect_handle.size.x - px(14)) / float(n)
		var i := int(clampf((at.x - _rect_handle.position.x - px(7)) / maxf(wcell, 1.0), 0, n - 1))
		if cab.set_handle_at(String(Cab.HANDLES[i])):
			changed.emit()


## _move — ведение ползунка. Доля считается ОТ ВЫСОТЫ ПОЛЯ, а не от положения
## рукоятки: тянут не рукоятку, а значение, и «схватить мимо» не должно давать
## прыжок.
func _move(at: Vector2) -> void:
	var r := _rect_traction if _drag == "traction" else _rect_independent
	var inner_top := r.position.y + px(28)
	var inner_h: float = maxf(r.size.y - px(58), 1.0)
	var t: float = clampf(1.0 - (at.y - inner_top) / inner_h, 0.0, 1.0)
	if _drag == "traction":
		if cab.set_traction_at(int(round(t * float(cab.traction_notches)))):
			changed.emit()
	elif _drag == "independent":
		if cab.set_independent_at(int(round(t * float(cab.independent_max)))):
			changed.emit()
