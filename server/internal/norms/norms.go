// Package norms — пределы земляных работ: матрица применимости.
//
// # Что это
//
// Ответ на один вопрос строителя: допустим ли этот уклон, этот радиус, эта
// высота насыпи — для ЛИНИИ с известной категорией, назначением, расчётной
// скоростью и условиями. Ответ — один из пяти исходов матрицы (см. Outcome),
// и каждый исход несёт русский текст отказа с причиной и числом.
//
// Форма — МАТРИЦА, а не пороги (бриф W1-B, спека §2.1.6). Предел не записан
// одной константой: у каждого сочетания ключа своя строка, и сочетание, для
// которого строки нет, обязано получить отказ UnknownContext, а НЕ предел
// похожей строки. Валидатор отказывает, а не чинит.
//
// Ключ несёт четыре измерения: категория линии, назначение пути, расчётная
// скорость, условия. Строка матрицы объявляет, какие измерения она различает,
// а какие для неё безразличны; «безразлично» — данные из нормы (СТН Ц-01-95
// про международные магистральные: «не более 12,5 ‰ независимо от
// грузонапряжённости»), а не подбор. Сегодня все строки безразличны к
// скорости, но измерение остаётся в ключе: оно появится, а не потому, что уже
// работает (решение координатора, ОТВЕТ 2).
//
// # Откуда числа
//
// Руководящие уклоны — СТН Ц-01-95, п. 4.1 (спека §2.1.1). Ряд радиусов —
// СП 119.13330, п. 4.4 (спека §2.1.4). Высота насыпи — СП 32-104-98 и
// СП 119.13330, приведено владельцем 2026-08-13 (спека §2.1.5). Числа не
// пишутся по памяти; источник указан у каждой строки.
//
// # Чего здесь нет (объявлено, а не подставлено)
//
// Наибольшая глубина выемки и уклон станционной площадки отсутствуют: первое
// не нормировано, второе ждёт подтверждения по своду правил (спека §2.1.7).
// Ветви «в трудных условиях» (уклон до 40 ‰ на подъездных IV) и «менее 300 м с
// обоснованием» не реализуются: обоснование — проект, экспертиза и человек, а
// не поле в команде. Матрица обязана дать явный исход на каждое сочетание
// ключа, поэтому ветви объявлены исходом RequiresIndividualDesign, а не
// подменены самой мягкой строкой таблицы (§2.1.6).
//
// НАИБОЛЬШИЙ УКЛОН С УСИЛЕННОЙ ТЯГОЙ (СТН Ц-01-95, п. 4.2: 18 ‰ на
// особогрузонапряжённых и I, 20 ‰ на II, 30 ‰ на III, 40 ‰ на IV) в матрице
// НЕ ПРЕДСТАВЛЕН, и это объявленное отсутствие, а не забытая строка. Довод:
// усиленная тяга — режим ведения поезда (подталкивающий или двойная тяга), и
// потребителя у неё в игре сегодня нет: строитель прокладывает путь, не зная,
// каким составом по нему поедут. Поле без потребителя оказалось бы неверным.
// Ссылка на п. 4.2 из шапки убрана нарочно: цитировать пункт, чьих чисел
// матрица не несёт, — это обещать покрытие, которого нет.
// Условие входа: появится режим ведения, объявляющий усиленную тягу как факт о
// поезде, — тогда у строки матрицы станет два предела вместо одного, а у ключа
// прибавится измерение.
//
// Смягчение ограничивающего уклона из-за снижения сцепления в кривых
// (R ≤ 500 м при электрической тяге, R < 800 м при тепловозной) — отдельная
// работа про Adhesion; здесь замечено и не сделано (§2.1.3).
package norms

import (
	"fmt"

	"github.com/shady2k/ClearAhead/server/internal/physics"
	"github.com/shady2k/ClearAhead/server/internal/units"
)

// Outcome — исход проверки предела: ровно пять, и они не сливаются (§2.1.6).
type Outcome int

const (
	// Allowed — принять.
	Allowed Outcome = iota
	// AllowedWith — принять ТОЛЬКО при наличии поддерживаемого сооружения или
	// ограничения; имя меры лежит в Result.Measure.
	AllowedWith
	// RequiresIndividualDesign — отказ сегодня: норма допускает предел только
	// при индивидуальном обосновании, а в игре обосновать нечем (§2.1.6).
	RequiresIndividualDesign
	// Forbidden — отказ всегда: норма запрещает.
	Forbidden
	// UnknownContext — отказ: сочетания ключа в матрице нет, подбор похожей
	// строки запрещён.
	UnknownContext
)

// Result — ответ матрицы на один вопрос.
type Result struct {
	// Outcome — исход; для Allowed всё остальное пусто.
	Outcome Outcome
	// Measure — имя меры для AllowedWith (сооружение или ограничение);
	// в остальных исходах пусто.
	Measure string
	// Reason — русский текст отказа с причиной и числом; для Allowed пуст.
	Reason string
}

// Context — ключ матрицы: сочетание, для которого спрашивается предел.
//
// Значения категорий и назначений — из нормы (СТН Ц-01-95, п. 4.1);
// непрочитанное сочетание даёт UnknownContext, а не предел соседней строки.
type Context struct {
	Category   Category
	Purpose    Purpose
	Speed      units.Speed // расчётная скорость; строки матрицы её не различают
	Conditions Conditions
}

// Category — категория линии, свойство исходника, а не мира (§2.1.2).
type Category string

const (
	CategoryExtraHeavy    Category = "extra_heavy" // особогрузонапряжённая
	CategoryI             Category = "I"
	CategoryII            Category = "II"
	CategoryIII           Category = "III"
	CategoryIV            Category = "IV"
	CategoryHighSpeed     Category = "high_speed"    // скоростная магистраль
	CategoryInternational Category = "international" // международная магистраль
)

// Purpose — назначение пути.
type Purpose string

const (
	// PurposeMainLine — перегонный главный путь.
	PurposeMainLine Purpose = "main_line"
	// PurposeAccess — подъездной путь IV категории: в норме у него своя
	// строка («в трудных и особо трудных условиях — до 40 ‰»), и в матрице он
	// живёт отдельной строкой, а не переиспользует предел IV категории.
	PurposeAccess Purpose = "access_iv"
)

// Conditions — условия местности. Трудные и особо трудные — известная норма
// ветвь: норма их допускает при обосновании, обосновывать в игре нечем, исход
// RequiresIndividualDesign (решение координатора, ОТВЕТ 2б). Любое другое
// значение — норма не прочитана, исход UnknownContext.
type Conditions string

const (
	ConditionsNormal              Conditions = "normal"
	ConditionsDifficult           Conditions = "difficult"            // трудные
	ConditionsEspeciallyDifficult Conditions = "especially_difficult" // особо трудные
)

// row — одна строка матрицы: сочетание (категория, назначение), для которого
// норма прочитана. Строка различает категорию и назначение; условия
// различаются на уровне ключа для всех строк; скорость строка объявляет
// безразличной (СТН не связывает предел со скоростью).
type row struct {
	category Category
	purpose  Purpose
	// gradientMilliPermille — руководящий уклон новой линии в грузовом
	// направлении, тысячные промилле (СТН Ц-01-95, п. 4.1; спека §2.1.1).
	// Наибольший уклон с усиленной тягой (п. 4.2) здесь не хранится — разбор в
	// шапке пакета.
	gradientMilliPermille int64
}

// matrix — матрица применимости. Строки только там, где норма прочитана;
// сегодняшний маленький состав — это правильно (решение координатора, ОТВЕТ 2).
var matrix = []row{
	{CategoryExtraHeavy, PurposeMainLine, 9000},     // 9 ‰
	{CategoryI, PurposeMainLine, 12000},             // 12 ‰
	{CategoryII, PurposeMainLine, 15000},            // 15 ‰
	{CategoryIII, PurposeMainLine, 20000},           // 20 ‰
	{CategoryIV, PurposeMainLine, 30000},            // 30 ‰
	{CategoryHighSpeed, PurposeMainLine, 20000},     // скоростные магистральные: не более 20 ‰
	{CategoryInternational, PurposeMainLine, 12500}, // международные: не более 12,5 ‰ независимо от грузонапряжённости
	{CategoryIV, PurposeAccess, 30000},              // подъездной IV, нормальные условия: предел IV
}

// radiusSeries — ряд допустимых радиусов кривых в метрах, а НЕ диапазон:
// 450 м не является допустимым радиусом, хотя лежит между 400 и 500
// (СП 119.13330, п. 4.4; спека §2.1.4).
var radiusSeries = []units.Distance{
	4000 * units.Meter,
	3000 * units.Meter,
	2500 * units.Meter,
	2000 * units.Meter,
	1800 * units.Meter,
	1500 * units.Meter,
	1200 * units.Meter,
	1000 * units.Meter,
	800 * units.Meter,
	700 * units.Meter,
	600 * units.Meter,
	500 * units.Meter,
	400 * units.Meter,
	350 * units.Meter,
	300 * units.Meter,
	250 * units.Meter,
	200 * units.Meter,
	180 * units.Meter,
}

// embankmentHighLimit — верхняя граница «высокой» насыпи в метрах: низкая до
// 1 м, средняя 1–6 м, высокая 6–12 м, особо высокая свыше 12 м
// (СП 32-104-98, СП 119.13330, приведено владельцем 2026-08-13; спека §2.1.5).
const embankmentHighLimit = 12 * units.Meter

// Gradient — допустим ли уклон milliPermille (тысячные промилле, шкала
// CompiledElement.AlignmentAt) на участке в кривой радиуса radius
// (0 — прямая: радиуса у примитива straight нет вовсе).
//
// Предел — руководящий уклон строки, а в кривой он УМЕНЬШАЕТСЯ на величину,
// эквивалентную дополнительному сопротивлению от кривой: ровно ту, что даёт
// physics.CurveResistance (w = 700/R), — ту же функцию, что зовёт интегратор
// движения. Своей копии формулы здесь нет (§2.1.3): CurveResistance отдаёт
// результат в тех же тысячных долях Н/кН, в которых выражен уклон, и вычитание
// законно потому, что Н/кН, кгс/тс и промилле — одна безразмерная величина.
//
// Признак «затяжной подъём» в ключ матрицы не входит, и судить о длине
// подъёма здесь нечем: поправка применяется к любому участку в кривой.
// Сравнивается модуль уклона: направление движения в ключ не входит, а спуск
// крутизной g — это подъём крутизной g для встречного направления.
func Gradient(ctx Context, milliPermille int64, radius units.Distance) Result {
	r, ok := lookupRow(ctx)
	if !ok {
		return unknownResult(ctx)
	}
	if res, done := conditionsVerdict(ctx.Conditions, r); done {
		return res
	}
	limit := r.gradientMilliPermille - int64(physics.CurveResistance(radius))
	if milliPermille < 0 {
		milliPermille = -milliPermille
	}
	if milliPermille > limit {
		reason := fmt.Sprintf("уклон %s при допустимом для %s %s",
			formatPermille(milliPermille), refusalName(r.category, r.purpose), formatPermille(limit))
		if radius > 0 {
			reason += fmt.Sprintf(" (в кривой руководящий %s уменьшен на сопротивление кривой %s)",
				formatPermille(r.gradientMilliPermille), formatPermille(int64(physics.CurveResistance(radius))))
		}
		return Result{Outcome: Forbidden, Reason: reason}
	}
	return Result{Outcome: Allowed}
}

// Radius — допустим ли радиус кривой.
//
// Радиус — ЧЛЕН РЯДА, а не число из диапазона (СП 119.13330, п. 4.4):
// строитель не принимает произвольное число, он выбирает из ряда. Члены ряда
// меньше 300 м норма допускает только при технико-экономическом обосновании,
// которого в игре нет, — исход RequiresIndividualDesign, а не Forbidden: ряд
// их знает. Радиус вне ряда — Forbidden. Радиус 0 — прямая по соглашению
// формата карты, ограничение неприменимо.
func Radius(ctx Context, radius units.Distance) Result {
	r, ok := lookupRow(ctx)
	if !ok {
		return unknownResult(ctx)
	}
	if res, done := conditionsVerdict(ctx.Conditions, r); done {
		return res
	}
	if radius <= 0 {
		return Result{Outcome: Allowed} // прямая: радиуса нет
	}
	if !memberOfSeries(radius) {
		return Result{Outcome: Forbidden, Reason: fmt.Sprintf(
			"радиус %s не входит в ряд допустимых значений (СП 119.13330, п. 4.4)",
			formatMeters(radius))}
	}
	if radius < 300*units.Meter {
		return Result{Outcome: RequiresIndividualDesign, Reason: fmt.Sprintf(
			"радиус %s — член ряда, но менее 300 м допускается только при технико-экономическом обосновании; в игре обосновать нечем (СП 119.13330, п. 4.4, спека §2.1.4)",
			formatMeters(radius))}
	}
	return Result{Outcome: Allowed}
}

// Embankment — допустима ли высота насыпи.
//
// Насыпь нормируется не одним числом, а порогом: низкая до 1 м и средняя
// 1–6 м принимаются; высокая 6–12 м — только с бермами либо уменьшением
// крутизны откоса в нижней части (исход AllowedWith, мера названа); особо
// высокая свыше 12 м требует индивидуального проектирования, которого в игре
// нет (СП 32-104-98, СП 119.13330; спека §2.1.5, §2.1.6).
func Embankment(ctx Context, height units.Distance) Result {
	r, ok := lookupRow(ctx)
	if !ok {
		return unknownResult(ctx)
	}
	if res, done := conditionsVerdict(ctx.Conditions, r); done {
		return res
	}
	switch {
	case height <= 6*units.Meter:
		return Result{Outcome: Allowed}
	case height <= embankmentHighLimit:
		return Result{
			Outcome: AllowedWith,
			Measure: "бермы либо уменьшение крутизны откоса в нижней части",
			Reason: fmt.Sprintf("насыпь высотой %s — высокая (6–12 м): нормативы требуют менять геометрию откосов (СП 32-104-98, СП 119.13330)",
				formatMeters(height)),
		}
	default:
		return Result{Outcome: RequiresIndividualDesign, Reason: fmt.Sprintf(
			"насыпь высотой %s — особо высокая (свыше 12 м): требуется индивидуальное проектирование, а в игре его нет (СП 32-104-98, СП 119.13330, спека §2.1.6)",
			formatMeters(height))}
	}
}

// lookupRow находит строку матрицы по (категория, назначение). Скорость и
// условия в поиске не участвуют: скорость для всех строк безразлична, условия
// обрабатываются conditionsVerdict отдельно. Непрочитанного сочетания нет —
// возвращаем ложь, и вопрос получает UnknownContext, а не предел соседней
// строки.
func lookupRow(ctx Context) (row, bool) {
	for _, r := range matrix {
		if r.category == ctx.Category && r.purpose == ctx.Purpose {
			return r, true
		}
	}
	return row{}, false
}

// conditionsVerdict возвращает готовый отказ, если условия не нормальные:
// трудные и особо трудные — известная норма ветвь, допускаемая при
// обосновании, которого в игре нет, — RequiresIndividualDesign (не
// UnknownContext и не Allowed, решение координатора, ОТВЕТ 2б); непрочитанное
// значение — UnknownContext.
func conditionsVerdict(c Conditions, r row) (Result, bool) {
	switch c {
	case ConditionsNormal:
		return Result{}, false
	case ConditionsDifficult, ConditionsEspeciallyDifficult:
		return Result{Outcome: RequiresIndividualDesign, Reason: fmt.Sprintf(
			"для %s в трудных условиях норма допускает предел только при индивидуальном обосновании; в игре обосновать нечем (спека §2.1.6)",
			refusalName(r.category, r.purpose))}, true
	default:
		return Result{Outcome: UnknownContext, Reason: fmt.Sprintf(
			"условия «%s» в матрице применимости отсутствуют: норма для них не прочитана", c)}, true
	}
}

// unknownResult — отказ для сочетания, которого в матрице нет: норму для него
// никто не прочитал, и подбор похожей строки запрещён.
func unknownResult(ctx Context) Result {
	return Result{Outcome: UnknownContext, Reason: fmt.Sprintf(
		"сочетание категории «%s», назначения «%s» и условий «%s» отсутствует в матрице применимости: норма для него не прочитана (спека §2.1.6)",
		ctx.Category, ctx.Purpose, ctx.Conditions)}
}

// memberOfSeries — радиус является ЧЛЕНОМ ряда допустимых значений.
func memberOfSeries(radius units.Distance) bool {
	for _, member := range radiusSeries {
		if radius == member {
			return true
		}
	}
	return false
}

// refusalName — имя строки матрицы в родительном падеже для текста отказа:
// «для линии II категории», «для подъездного пути IV категории». Категория
// обязана попасть в отказ: без неё игрок не поймёт, почему тот же уклон вчера
// прошёл (бриф W1-B).
func refusalName(c Category, p Purpose) string {
	switch {
	case c == CategoryIV && p == PurposeAccess:
		return "подъездного пути IV категории"
	case c == CategoryExtraHeavy:
		return "особогрузонапряжённой линии"
	case c == CategoryHighSpeed:
		return "скоростной магистрали"
	case c == CategoryInternational:
		return "международной магистрали"
	default:
		return "линии " + string(c) + " категории"
	}
}

// formatPermille печатает уклон из тысячных промилле в промилле с одним
// десятичным знаком: норма округляет уклон до 0,1 ‰, а шкала проекта — в сто
// раз мельче, поэтому авторская величина ложится без потерь (§2.1.3).
func formatPermille(milliPermille int64) string {
	return fmt.Sprintf("%d,%d ‰", milliPermille/1000, (milliPermille%1000)/100)
}

// formatMeters печатает расстояние в метрах целым числом: ряд радиусов и
// границы насыпи заданы целыми метрами.
func formatMeters(d units.Distance) string {
	return fmt.Sprintf("%d м", d/units.Meter)
}
