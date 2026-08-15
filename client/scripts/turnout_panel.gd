## TurnoutPanel — ПУЛЬТ СТРЕЛКИ: какая стрелка на очереди, куда она переведена и
## чем её перевести.
##
## # Почему пульт, а не щелчок по стрелке в кадре
##
## Решение владельца 2026-08-15: «нужно в HUD добавить возможность переключить
## следующую стрелку, ну и показывать куда она сейчас переключена, мышью или
## клавишей». Луч по телу привода был отвергнут вместе с этим: он работает только
## пока стрелка в кадре, а машинисту она нужна ровно тогда, когда до неё ещё
## сотни метров и на экране она в три пикселя.
##
## # Показывается ОДНА стрелка, и её выбирает мир
##
## Выбор («ближайшая под рукой у пешехода, иначе следующая по ходу машины»)
## живёт в world.gd, потому что это вопрос о МЕСТЕ, а не о рисовании. Пульт
## получает готовое и ничего не ищет — иначе выбор оказался бы в двух местах,
## и подсказка у ног разошлась бы с панелью на экране.
##
## # Положение рисуется СХЕМОЙ, а не словом
##
## «Прямо» и «на боковой» на схеме читаются за долю секунды и не требуют языка;
## слово рядом остаётся для того, кто видит пульт впервые. Схема — та же развилка,
## что под ногами: общий путь слева, две ветви вправо, живая ветвь яркая.
class_name TurnoutPanel
extends Control

## Игрок просит перевести показанную стрелку. Панель САМА НИЧЕГО НЕ ШЛЁТ:
## отправка живёт там же, где и для клавиши (world.gd), иначе у одной команды
## стало бы два места отправки и два способа разойтись.
signal thrown

## Размер считается от ОКНА по той же причине, что у пульта машиниста: пиксельный
## размер выглядит одинаково только на одном разрешении.
const W_SHARE := 0.19
const W_MIN := 260.0
const H_SHARE := 0.62      ## доля ширины: панель шире, чем выше
const MARGIN := 16.0

const C_BG := Color(0.05, 0.06, 0.08, 0.88)
const C_RIM := Color(0.28, 0.31, 0.36)
const C_TEXT := Color(0.86, 0.89, 0.93)
const C_DIM := Color(0.53, 0.57, 0.64)
const C_LIVE := Color(0.49, 0.82, 0.63)     ## ветвь, по которой стоит остряк
const C_DEAD := Color(0.30, 0.33, 0.38)     ## и та, по которой не стоит
const C_WARN := Color(1.0, 0.62, 0.35)      ## занята составом
const C_KEY := Color(0.98, 0.82, 0.35)

## Что показывать. Кладёт мир одним словарём, а не пятью присваиваниями: панель
## обязана либо показывать целую стрелку, либо не показывать никакой — половина
## полей от прошлой стрелки рядом с половиной от новой врала бы обеим.
##
## Ключи: id, name, position, drive, occupied_by, distance_m (−1 — неизвестно),
## note (последний отказ сервера), key (какой клавишей переводить).
var target := {}

var _w := W_MIN
var _k := 1.0


func setup() -> void:
	mouse_filter = Control.MOUSE_FILTER_PASS
	set_anchors_preset(Control.PRESET_TOP_RIGHT)


func _ready() -> void:
	_resize()
	get_viewport().size_changed.connect(_resize)


func _resize() -> void:
	var vw: float = float(get_viewport().get_visible_rect().size.x)
	_w = maxf(vw * W_SHARE, W_MIN)
	_k = _w / W_MIN
	custom_minimum_size = Vector2(_w, _w * H_SHARE)
	size = custom_minimum_size
	offset_left = -_w - MARGIN
	offset_right = -MARGIN
	offset_top = MARGIN
	offset_bottom = MARGIN + _w * H_SHARE
	queue_redraw()


func px(v: float) -> float:
	return v * _k


## show_turnout — показать стрелку. Пустой словарь гасит панель целиком: пульт,
## показывающий прошлую стрелку после того, как игрок от неё ушёл, обещает
## клавишу, которая переведёт не то, что видно.
func show_turnout(t: Dictionary) -> void:
	if t == target:
		return
	target = t
	visible = not t.is_empty()
	queue_redraw()


func _draw() -> void:
	if target.is_empty():
		return
	var w := size.x
	var h := size.y
	draw_rect(Rect2(0, 0, w, h), C_BG)
	draw_rect(Rect2(0, 0, w, h), C_RIM, false, px(1.6))

	var pad := px(12.0)
	var label := String(target.get("name", ""))
	if label == "":
		# Метки нет — показываем идентификатор, а не выдумываем номер: имя
		# стрелки приходит из карты, и «стрелка 1» здесь было бы фактом о мире.
		label = String(target.get("id", "")).substr(0, 8)
	_text(Vector2(pad, pad + px(15.0)), "СТРЕЛКА %s" % label, C_TEXT, int(px(17.0)))

	# Расстояние и механизм — второй строкой, помельче. Расстояние есть только у
	# машиниста (его считает сервер по ходу машины); у пешехода его нет, и строка
	# тогда короче, а не заполнена нулём.
	var second := _drive_word(String(target.get("drive", "")))
	var dist := float(target.get("distance_m", -1.0))
	if dist >= 0.0:
		second = "%s  ·  %d м" % [second, int(round(dist))]
	_text(Vector2(pad, pad + px(33.0)), second, C_DIM, int(px(13.0)))

	# РАСКЛАДКА СВЕРХУ ВНИЗ, и место под каждую строку отмерено: заголовок, вид
	# механизма, схема, слово о положении, нижняя строка. Схеме достаётся то, что
	# осталось, — иначе она наезжает на слово, а слово на строку, и первый же
	# снимок показывает две надписи одна поверх другой (так и вышло).
	var word_y := h - pad - px(22.0)
	var pos := String(target.get("position", ""))
	_draw_fork(Rect2(pad, pad + px(44.0), w - pad * 2, word_y - pad - px(58.0)), pos)
	_text(Vector2(pad, word_y), _position_word(pos), C_TEXT, int(px(14.0)))

	# Нижняя строка: либо почему нельзя, либо чем перевести.
	var note := String(target.get("note", ""))
	var held := String(target.get("occupied_by", ""))
	var y := h - pad - px(4.0)
	if held != "":
		_text(Vector2(pad, y), "занята составом — не переводится", C_WARN, int(px(13.0)))
	elif note != "":
		_text(Vector2(pad, y), note, C_WARN, int(px(13.0)))
	else:
		var key := String(target.get("key", ""))
		_text(Vector2(pad, y), "%s или щелчок — перевести" % key, C_KEY, int(px(13.0)))


## _draw_fork — развилка: общий путь слева, прямая ветвь вверх-вправо, боковая
## вниз-вправо. Живая ветвь горит, мёртвая тусклая.
##
## Рисуется НЕ ПО ГЕОМЕТРИИ СТРЕЛКИ, и это нарочно: пульт показывает СОСТОЯНИЕ, а
## не план. Схема, повторяющая настоящие углы, у марки 1/9 отличалась бы от
## прямой на шесть градусов — то есть не отличалась бы вовсе.
func _draw_fork(r: Rect2, pos: String) -> void:
	var live := pos == LiveChannel.TURNOUT_STRAIGHT
	var wide := px(5.0)
	var thin := px(3.0)
	var x0 := r.position.x
	var x1 := r.position.x + r.size.x * 0.45
	var x2 := r.end.x
	var mid := r.position.y + r.size.y * 0.5
	var top := r.position.y + r.size.y * 0.18
	var bot := r.end.y - r.size.y * 0.18
	# Общий путь — всегда живой: по нему приходят в любом положении.
	draw_line(Vector2(x0, mid), Vector2(x1, mid), C_LIVE, wide)
	# Прямая ветвь.
	draw_line(Vector2(x1, mid), Vector2(x2, top),
		C_LIVE if live else C_DEAD, wide if live else thin)
	# Боковая.
	draw_line(Vector2(x1, mid), Vector2(x2, bot),
		C_DEAD if live else C_LIVE, thin if live else wide)


## _position_word — как назвать положение словом. Схема читается быстрее, но
## слово называет то же самое тому, кто видит пульт впервые.
##
## Неизвестное положение НЕ ПЕРЕВОДИТСЯ в «наверное, прямо»: сервер прислал то,
## чего клиент не знает, и назвать это прямым было бы враньём про остряк.
func _position_word(pos: String) -> String:
	match pos:
		LiveChannel.TURNOUT_STRAIGHT:
			return "стоит по прямому пути"
		LiveChannel.TURNOUT_DIVERGING:
			return "стоит на боковой путь"
		_:
			return "положение неизвестно: %s" % pos


func _text(at: Vector2, s: String, col: Color, size_px: int) -> void:
	draw_string(ThemeDB.fallback_font, at, s, HORIZONTAL_ALIGNMENT_LEFT, -1, size_px, col)


## _drive_word — как назвать механизм человеку. Неизвестное значение
## показывается КАК ЕСТЬ: сервер вправе завести третий вид, и клиент обязан
## показать его, а не спрятать под «ручной».
func _drive_word(kind: String) -> String:
	match kind:
		"manual":
			return "ручной перевод"
		"electric":
			return "электропривод"
		"":
			return "механизм не назван"
		_:
			return kind


## _gui_input — щелчок по панели переводит показанную стрелку.
##
## По ВСЕЙ панели, а не по нарисованной кнопке: показана ровно одна стрелка и
## ровно одно действие с ней, и рисовать под это отдельный прямоугольник значило
## бы завести место, мимо которого можно промахнуться.
func _gui_input(event: InputEvent) -> void:
	if target.is_empty() or not (event is InputEventMouseButton):
		return
	var mb := event as InputEventMouseButton
	if mb.button_index != MOUSE_BUTTON_LEFT or not mb.pressed:
		return
	accept_event()
	thrown.emit()
