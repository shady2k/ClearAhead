package content

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"regexp"

	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
)

// model.go — ТЕЛО ПРЕДМЕТА, ОПИСАННОЕ ДАННЫМИ, а не кодом клиента.
//
// # Зачем это заведено
//
// Решение владельца 2026-08-15: «убедись, что на клиенте ассетов нет. Наш сервер
// должен поддерживать любой рендер: godot, unity и что угодно ещё. Соответственно
// данных сервера должно хватать для полноценной отрисовки на клиенте».
//
// До этого дня переводной механизм РИСОВАЛ КЛИЕНТ по своим константам: два
// десятка сантиметровых чисел и палитра жили в switch_stand.gd. Оправдано это
// было записанной границей владения («пока привод рисуется, а не показывается
// ассетом, его размеры — решение художника, как длина крыла крестовины»), и
// граница была честной ровно до вопроса «а второй клиент?». Второй клиент
// нарисовал бы другой привод, и никто не смог бы сказать, который из двух верен:
// эталона не существовало нигде.
//
// Теперь эталон есть, и он на сервере — ОТДЕЛЬНЫМ ФАЙЛОМ НА КАЖДЫЙ АССЕТ
// (решение владельца там же: «хранить их в json я согласен, только не в
// content.json, а отдельный json для каждого ассета»). Файл едет клиенту теми же
// байтами по тому же адресу-хешу, что и glb: раздача, кэш и атрибуция у них
// общие, потому что это один и тот же класс вещи — ВИД ПРЕДМЕТА.
//
// # Почему примитивы, а не меш
//
// Потому что меша у нас нет, а выдумать его нельзя. Примитивы — это то же
// решение, что уже принято для пути: сервер шлёт РЕЦЕПТ (шаг шпал, полуширина
// балласта, длина бруса), клиент строит меш. Разница с glb только в том, ЧТО
// именно едет байтами; граница «клиент ничего не выдумывает» одна и та же.
//
// В тот день, когда приедет настоящая модель, у записи каталога меняется
// media_type и file — и всё. Сборщик примитивов остаётся для устройств, у
// которых модели нет, а клиент не правится вовсе.
//
// # Чего в формате НЕТ и почему
//
// СКЕЛЕТА И КЛЮЧЕВЫХ КАДРОВ. Подвижная часть здесь описывается ПОВОРОТОМ ПО
// СОСТОЯНИЮ (pivot), а не дорожкой анимации: у стрелки два положения, и между
// ними она не проигрывает движение — она в них стоит. Плавность перехода — забота
// рендера, и это ровно его зона (слово владельца: «на клиенте оставляем анимации,
// HUD и т. п.»).
//
// МАТЕРИАЛОВ СЛОЖНЕЕ ЦВЕТА. Ни текстур, ни карт нормалей: нечего в них класть.
// Появятся — приедут записью каталога, а не полем «на всякий случай».

// ModelMediaType — тип содержимого файла модели.
//
// Свой, а не application/json: по нему набор отличает описание ТЕЛА от любого
// другого json, который автор вздумает положить рядом, и по нему же клиент
// решает, чем разбирать доехавшие байты.
const ModelMediaType = "application/vnd.clearahead.model+json"

// ModelFormatVersion — версия формата модели. Неизвестная — отказ загрузки, как
// у карты и у набора: файл новее читателя не разбирается наполовину.
const ModelFormatVersion = 1

// MaxModelBytes — потолок на файл модели. Описание из примитивов — это десятки
// строк; мегабайт означает, что в файл попало не то.
const MaxModelBytes = 1 << 20

// Оси, единицы и углы ОБЪЯВЛЯЮТСЯ В ФАЙЛЕ, а не подразумеваются.
//
// Это и есть та часть, ради которой формат вообще пишется: «данных сервера
// должно хватать для полноценной отрисовки». Рендер, у которого свои оси
// (у Godot и Unity Y вверх, но Z смотрит в разные стороны), обязан прочитать
// соглашение, а не угадать его — и обязан ОТКАЗАТЬ на незнакомом, а не
// подставить своё.
const (
	// AxesXRightYUpZBack — ПРАВАЯ ТРОЙКА, та же, что у glTF: X вправо от хода,
	// Y вверх, Z НАЗАД (против хода), то есть вперёд смотрит −Z.
	//
	// Выбрана не по вкусу: в этом же соглашении лежит второй ассет проекта —
	// модель ВЛ80 в glb, — и клиент уже умеет его читать. Взяв «Z вперёд» (левую
	// тройку, как у Unity), мы получили бы два соглашения в одном каталоге и
	// зеркальное преобразование у половины ассетов; зеркало же переворачивает не
	// только оси, но и ЗНАКИ ПОВОРОТОВ, и ошибка в нём выглядит как деталь,
	// повёрнутая не туда, а не как поломка.
	//
	// Рендеру, у которого оси свои (Unity), зеркалить придётся — но он и так
	// зеркалит glb, то есть делает это ОДНИМ местом на весь каталог.
	AxesXRightYUpZBack = "x_right_y_up_z_back"
	UnitsMetres        = "m"
	AnglesDegrees      = "deg"
)

// Формы примитивов. Перечень ЗАКРЫТ: неизвестная форма — отказ, а не пропуск
// части. Предмет, у которого молча не нарисовалась половина, выглядит исправным.
const (
	// ShapeGroup — часть без тела: только поза и дети. Пустая строка — она же.
	ShapeGroup = ""
	// ShapeBox — прямоугольный ящик, size = [x, y, z].
	ShapeBox = "box"
	// ShapeCylinder — цилиндр вдоль axis: radius, height, sides.
	ShapeCylinder = "cylinder"
	// ShapeFrustum — усечённый конус вдоль axis: bottom, top, height, sides.
	// При sides = 4 это усечённая пирамида — чугунная станина ручного привода.
	ShapeFrustum = "frustum"
	// ShapePlate — плоский щиток: size = [ширина, высота], толщина thickness,
	// плоскость XY, лицо смотрит вдоль +Z. На него наносятся знак (mark) и
	// надпись (label).
	ShapePlate = "plate"
)

// Оси поворота и вытягивания.
const (
	AxisX = "x"
	AxisY = "y"
	AxisZ = "z"
)

// Ключи СОСТОЯНИЯ УСТРОЙСТВА, по которым поворачиваются подвижные части и
// подставляются надписи.
//
// # Почему словарь закрыт
//
// Потому что это ДОГОВОР между моделью и миром: «поверни на угол, отвечающий
// состоянию position». Открытый словарь означал бы модель, ссылающуюся на
// состояние, которого сервер не присылает, — то есть подвижную часть, которая
// никогда не двигается, и молчание вместо отказа.
//
// Значения этих состояний едут в снапшоте (match.TurnoutState) и в геометрии
// (track.RenderTurnoutDrive); имена здесь и там одни и те же нарочно.
const (
	// StatePosition — положение остряка: straight | diverging.
	StatePosition = "position"
	// StateHand — в какую сторону уходит боковой путь: left | right.
	StateHand = "hand"
	// StateSide — с какой стороны от оси пути стоит само устройство:
	// left | right. Им разворачиваются части, обращённые К ПУТИ, — номерная
	// табличка и рабочая тяга.
	StateSide = "side"
	// StateName — метка устройства. Не поворот, а ТЕКСТ: то, что написано на
	// табличке. Приходит из карты и показывается пультом.
	StateName = "name"
)

var (
	pivotStates = map[string]bool{StatePosition: true, StateHand: true, StateSide: true}
	labelStates = map[string]bool{StateName: true}
	colourRe    = regexp.MustCompile(`^#[0-9a-f]{6}$`)
)

// Model — описание тела предмета.
type Model struct {
	FormatVersion int `json:"format_version"`
	// Model — имя модели. Совпадает с именем записи каталога: расхождение
	// означало бы, что файл положили не под тем ассетом, и поймать это иначе
	// нечем.
	Name string `json:"model"`
	// Device — ЧЬЁ ЭТО ТЕЛО: род переводного механизма (mapfmt.DriveManual,
	// DriveElectric).
	//
	// # Почему объявляет сам файл, а не перечень рядом с ассетами
	//
	// Перечень «род → вид» здесь был, и владелец спросил ровно то, что следовало:
	// «а зачем Devices? почему они не могут быть ассетом?». Незачем. Ассет и так
	// есть у каждого тела, а связь с родом — это факт О ТЕЛЕ, и место ему в файле
	// тела. Отдельная запись рядом была третьей сущностью, которую пришлось бы
	// держать в согласии с двумя другими.
	//
	// Клиент ищет тело ПО ЭТОМУ ПОЛЮ: карта говорит «механизм ручной», клиент
	// берёт модель, у которой device = manual. Никакого соглашения об именах
	// файлов, никакого вывода на стороне клиента.
	Device string `json:"device"`
	// Title — как устройство называется человеку: «ручной перевод»,
	// «электропривод». Показывает пульт.
	//
	// ЗДЕСЬ, А НЕ В КЛИЕНТЕ, по тому же доводу, что и всё остальное в этом файле:
	// до 2026-08-15 обе строки были зашиты в turnout_panel.gd, и второй клиент
	// назвал бы то же устройство иначе.
	Title  string `json:"title"`
	Axes   string `json:"axes"`
	Units  string `json:"units"`
	Angles string `json:"angles"`
	// Materials — палитра модели. Ссылка по имени, а не цвет у каждой части:
	// одна краска на десяти деталях правится один раз, и десять деталей не
	// расходятся в оттенке.
	Materials map[string]Material `json:"materials"`
	Parts     []Part              `json:"parts"`
}

// Material — краска. Цвет в sRGB, потому что в нём его читает человек; перевод в
// линейное — работа рендера, и она у каждого своя.
type Material struct {
	Colour    string  `json:"colour"`
	Roughness float64 `json:"roughness"`
	Metallic  float64 `json:"metallic"`
}

// Part — часть тела: поза, форма и дети.
type Part struct {
	Name  string     `json:"name,omitempty"`
	Shape string     `json:"shape,omitempty"`
	At    [3]float64 `json:"at"`
	// Rotate — постоянный поворот части, градусы по осям X, Y, Z.
	//
	// Части этой модели поворачиваются ВОКРУГ ОДНОЙ ОСИ, и порядок применения
	// поэтому безразличен. Это НЕ общее свойство формата, а свойство сегодняшних
	// данных, и оно проверяется: две ненулевые оси разом — отказ, потому что
	// порядок Эйлера у рендеров разный, и молча выбрать свой значит нарисовать
	// разное.
	Rotate    [3]float64 `json:"rotate,omitempty"`
	Material  string     `json:"material,omitempty"`
	Size      []float64  `json:"size,omitempty"`
	Radius    float64    `json:"radius,omitempty"`
	Bottom    float64    `json:"bottom,omitempty"`
	Top       float64    `json:"top,omitempty"`
	Height    float64    `json:"height,omitempty"`
	Thickness float64    `json:"thickness,omitempty"`
	Sides     int        `json:"sides,omitempty"`
	Axis      string     `json:"axis,omitempty"`
	Mark      *Mark      `json:"mark,omitempty"`
	Label     *Label     `json:"label,omitempty"`
	Pivot     *Pivot     `json:"pivot,omitempty"`
	Parts     []Part     `json:"parts,omitempty"`
}

// Pivot — подвижность части: на какой угол её повернуть при каком состоянии.
type Pivot struct {
	Axis string `json:"axis"`
	// By — ЧТО спрашивать у мира: имя состояния устройства (State*).
	By string `json:"by"`
	// States — угол в градусах для каждого значения состояния. Значение, которого
	// здесь нет, часть не поворачивает вовсе: рендер обязан оставить её как есть,
	// а не подставить ноль. Состояние, которого мир не прислал, — не поломка:
	// так выглядит устройство до первого снапшота.
	States map[string]float64 `json:"states"`
}

// Mark — ЗНАК НА ЩИТКЕ: замкнутый многоугольник в долях щитка.
//
// Многоугольником, а не картинкой, по той же причине, по которой тело —
// примитивами: картинку пришлось бы нарисовать, а её у нас нет, и вдобавок она
// была бы растром с собственным разрешением. Доли щитка переживают любой размер
// и читаются любым рендером.
//
// Начало координат — левый нижний угол щитка, если смотреть на его лицо.
type Mark struct {
	Polygon   [][2]float64 `json:"polygon"`
	Material  string       `json:"material"`
	BothSides bool         `json:"both_sides"`
}

// Label — НАДПИСЬ НА ЩИТКЕ. Текст не в модели: он приходит из мира по имени
// состояния (StateName), потому что номер стрелки — факт о станции, а не о теле
// привода. Height — кегль долей высоты щитка.
type Label struct {
	By        string  `json:"by"`
	Material  string  `json:"material"`
	Height    float64 `json:"height"`
	BothSides bool    `json:"both_sides"`
}

// ParseModel читает и ПРОВЕРЯЕТ описание тела.
//
// Проверяется всё, что можно проверить без рендера: версия формата, соглашение
// об осях, разрешимость ссылок на краску, полнота чисел у каждой формы. Довод тот
// же, что у набора целиком: испорченное описание обязано НЕ ЗАГРУЗИТЬСЯ, а не
// доехать до клиента и превратиться там в предмет без половины деталей.
func ParseModel(name string, raw []byte) (*Model, error) {
	if len(raw) > MaxModelBytes {
		return nil, fmt.Errorf("content: модель %s: файл больше %d байт", name, MaxModelBytes)
	}
	var m Model
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("content: модель %s: разбор: %w", name, err)
	}
	if dec.More() {
		return nil, fmt.Errorf("content: модель %s: после документа есть лишние данные", name)
	}
	if m.FormatVersion != ModelFormatVersion {
		return nil, fmt.Errorf("content: модель %s: версия формата %d не поддерживается, ожидается %d",
			name, m.FormatVersion, ModelFormatVersion)
	}
	if m.Name != name {
		return nil, fmt.Errorf("content: модель %s: в файле записано имя %q — файл лежит не под тем ассетом",
			name, m.Name)
	}
	if !mapfmt.KnownDrive(m.Device) {
		return nil, fmt.Errorf("content: модель %s: род механизма %q неизвестен; знаю %v",
			name, m.Device, mapfmt.Drives)
	}
	if m.Title == "" {
		return nil, fmt.Errorf("content: модель %s: не названо имя устройства (title) — "+
			"пульту нечего показать человеку", name)
	}
	if m.Axes != AxesXRightYUpZBack {
		return nil, fmt.Errorf("content: модель %s: соглашение об осях %q неизвестно, знаю %q",
			name, m.Axes, AxesXRightYUpZBack)
	}
	if m.Units != UnitsMetres {
		return nil, fmt.Errorf("content: модель %s: единицы %q, ожидались %q", name, m.Units, UnitsMetres)
	}
	if m.Angles != AnglesDegrees {
		return nil, fmt.Errorf("content: модель %s: углы %q, ожидались %q", name, m.Angles, AnglesDegrees)
	}
	if len(m.Materials) == 0 {
		return nil, fmt.Errorf("content: модель %s: палитра пуста — красить тело нечем", name)
	}
	for id, mat := range m.Materials {
		if !colourRe.MatchString(mat.Colour) {
			return nil, fmt.Errorf("content: модель %s: краска %s: цвет %q, ожидается #rrggbb в строчных",
				name, id, mat.Colour)
		}
		for _, d := range []struct {
			field string
			v     float64
		}{{"roughness", mat.Roughness}, {"metallic", mat.Metallic}} {
			if math.IsNaN(d.v) || d.v < 0 || d.v > 1 {
				return nil, fmt.Errorf("content: модель %s: краска %s: %s = %v вне [0, 1]",
					name, id, d.field, d.v)
			}
		}
	}
	if len(m.Parts) == 0 {
		return nil, fmt.Errorf("content: модель %s: ни одной части — тела нет", name)
	}
	for i := range m.Parts {
		if err := m.checkPart(name, "", m.Parts[i]); err != nil {
			return nil, err
		}
	}
	return &m, nil
}

// checkPart проверяет часть и её детей.
func (m Model) checkPart(model, path string, p Part) error {
	at := path + "/" + p.Name
	where := fmt.Sprintf("content: модель %s: часть %s", model, at)
	for i, v := range p.At {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return fmt.Errorf("%s: at[%d] = %v", where, i, v)
		}
	}
	turns := 0
	for i, v := range p.Rotate {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return fmt.Errorf("%s: rotate[%d] = %v", where, i, v)
		}
		if v != 0 {
			turns++
		}
	}
	if turns > 1 {
		// Разбор — у поля Rotate: порядок Эйлера у рендеров разный.
		return fmt.Errorf("%s: постоянный поворот задан вокруг %d осей разом; "+
			"порядок поворотов формат не определяет — заведите вложенную часть", where, turns)
	}
	if p.Shape != ShapeGroup {
		if p.Material == "" {
			return fmt.Errorf("%s: у тела не названа краска", where)
		}
		if _, ok := m.Materials[p.Material]; !ok {
			return fmt.Errorf("%s: краска %q в палитре модели не объявлена", where, p.Material)
		}
	}
	switch p.Shape {
	case ShapeGroup:
		if len(p.Parts) == 0 && p.Pivot == nil {
			return fmt.Errorf("%s: часть без формы, без детей и без подвижности — рисовать нечего", where)
		}
	case ShapeBox:
		if len(p.Size) != 3 {
			return fmt.Errorf("%s: у ящика size из %d чисел, ожидалось три", where, len(p.Size))
		}
		for i, v := range p.Size {
			if !(v > 0) || math.IsInf(v, 0) {
				return fmt.Errorf("%s: size[%d] = %v, ожидалось положительное конечное", where, i, v)
			}
		}
	case ShapeCylinder:
		if err := checkAxis(where, p.Axis); err != nil {
			return err
		}
		if !(p.Radius > 0) || !(p.Height > 0) {
			return fmt.Errorf("%s: цилиндр radius = %v, height = %v", where, p.Radius, p.Height)
		}
		if p.Sides < 3 {
			return fmt.Errorf("%s: цилиндр в %d граней — меньше трёх граней не бывает", where, p.Sides)
		}
	case ShapeFrustum:
		if err := checkAxis(where, p.Axis); err != nil {
			return err
		}
		if !(p.Bottom > 0) || !(p.Top > 0) || !(p.Height > 0) {
			return fmt.Errorf("%s: усечённый конус bottom = %v, top = %v, height = %v",
				where, p.Bottom, p.Top, p.Height)
		}
		if p.Sides < 3 {
			return fmt.Errorf("%s: конус в %d граней — меньше трёх граней не бывает", where, p.Sides)
		}
	case ShapePlate:
		if len(p.Size) != 2 {
			return fmt.Errorf("%s: у щитка size из %d чисел, ожидалось два", where, len(p.Size))
		}
		for i, v := range p.Size {
			if !(v > 0) || math.IsInf(v, 0) {
				return fmt.Errorf("%s: size[%d] = %v, ожидалось положительное конечное", where, i, v)
			}
		}
		if !(p.Thickness > 0) {
			return fmt.Errorf("%s: толщина щитка %v", where, p.Thickness)
		}
	default:
		return fmt.Errorf("%s: форма %q неизвестна; знаю %q, %q, %q, %q и группу без формы",
			where, p.Shape, ShapeBox, ShapeCylinder, ShapeFrustum, ShapePlate)
	}
	if p.Mark != nil {
		if p.Shape != ShapePlate {
			return fmt.Errorf("%s: знак наносится на щиток, а форма части — %q", where, p.Shape)
		}
		if err := m.checkMark(where, *p.Mark); err != nil {
			return err
		}
	}
	if p.Label != nil {
		if p.Shape != ShapePlate {
			return fmt.Errorf("%s: надпись наносится на щиток, а форма части — %q", where, p.Shape)
		}
		if !labelStates[p.Label.By] {
			return fmt.Errorf("%s: надпись берётся из состояния %q, которого мир не присылает",
				where, p.Label.By)
		}
		if _, ok := m.Materials[p.Label.Material]; !ok {
			return fmt.Errorf("%s: краска надписи %q в палитре не объявлена", where, p.Label.Material)
		}
		if !(p.Label.Height > 0) || p.Label.Height > 1 {
			return fmt.Errorf("%s: кегль надписи %v вне (0, 1] долей высоты щитка", where, p.Label.Height)
		}
	}
	if p.Pivot != nil {
		if err := checkAxis(where, p.Pivot.Axis); err != nil {
			return err
		}
		if !pivotStates[p.Pivot.By] {
			return fmt.Errorf("%s: подвижность по состоянию %q, которого мир не присылает; знаю %q, %q, %q",
				where, p.Pivot.By, StatePosition, StateHand, StateSide)
		}
		if len(p.Pivot.States) == 0 {
			return fmt.Errorf("%s: подвижность объявлена, а углов нет — часть не двинется никогда", where)
		}
		for st, deg := range p.Pivot.States {
			if st == "" {
				return fmt.Errorf("%s: угол объявлен для пустого состояния", where)
			}
			if math.IsNaN(deg) || math.IsInf(deg, 0) {
				return fmt.Errorf("%s: угол состояния %s = %v", where, st, deg)
			}
		}
	}
	for i := range p.Parts {
		if err := m.checkPart(model, at, p.Parts[i]); err != nil {
			return err
		}
	}
	return nil
}

func (m Model) checkMark(where string, k Mark) error {
	if len(k.Polygon) < 3 {
		return fmt.Errorf("%s: знак из %d точек — многоугольника не выходит", where, len(k.Polygon))
	}
	for i, pt := range k.Polygon {
		for j, v := range pt {
			if math.IsNaN(v) || v < 0 || v > 1 {
				return fmt.Errorf("%s: точка знака %d[%d] = %v вне [0, 1] долей щитка", where, i, j, v)
			}
		}
	}
	if _, ok := m.Materials[k.Material]; !ok {
		return fmt.Errorf("%s: краска знака %q в палитре не объявлена", where, k.Material)
	}
	return nil
}

func checkAxis(where, axis string) error {
	switch axis {
	case AxisX, AxisY, AxisZ:
		return nil
	}
	return fmt.Errorf("%s: ось %q неизвестна; знаю %q, %q и %q", where, axis, AxisX, AxisY, AxisZ)
}

// loadModels разбирает и проверяет ОПИСАНИЯ ТЕЛ, лежащие в наборе.
//
// Зовётся после ассетов: разбирается то, что уже уложено и проверено по хешу.
//
// # Полноты здесь НЕ ТРЕБУЕТСЯ, и это решение, а не пропуск
//
// Набор без тела ручного механизма законен: набор описывает, ЧТО БЫВАЕТ, а
// нужен ли ручной механизм — вопрос к КАРТЕ, которой набор не видит. Требовать
// полноты здесь значило бы отказывать серверу, у которого в регионе нет ни одной
// стрелки.
//
// Пустое место в мире при этом не появляется молча: стрелка, для которой тела не
// нашлось, — ОТКАЗ НА ЭКРАНЕ у клиента, ровно как машина, у которой не объявлен
// вид («вид %s единицы %s в наборе не объявлен»). Место проверки выбрано там же,
// где оно уже стоит для подвижного состава, а не заведено второе.
//
// ДВА ТЕЛА НА ОДИН РОД — отказ: набор, в котором два файла говорят «я ручной
// механизм», не решает, какой из них верен, и молчаливый выбор первого зависел
// бы от порядка записей.
func (s *Set) loadModels() error {
	s.models = map[string]*Model{}
	for _, a := range s.Assets {
		if a.MediaType != ModelMediaType {
			continue
		}
		raw, ok := s.Blob(a.Hash)
		if !ok {
			return fmt.Errorf("content: модель %s: байтов по адресу %s нет", a.Name, a.Hash)
		}
		m, err := ParseModel(a.Name, raw)
		if err != nil {
			return err
		}
		if prev, dup := s.models[m.Device]; dup {
			return fmt.Errorf("content: у механизма %s два тела: %s и %s — какое из них верное, набор не решает",
				m.Device, prev.Name, m.Name)
		}
		s.models[m.Device] = m
	}
	return nil
}

// ModelOf — описание тела механизма такого рода.
//
// Отдаётся СЕРВЕРНЫМ читателям (проверки, будущий второй потребитель); клиент
// получает те же байты по адресу ассета и разбирает их сам — сервер модель ему
// не пересказывает.
func (s *Set) ModelOf(kind string) (*Model, bool) {
	m, ok := s.models[kind]
	return m, ok
}
