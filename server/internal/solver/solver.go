// Package solver — решатель трассы как чистая функция.
//
// По смыслу: (поза, точка, намерение, профиль, каталог) → структурный результат.
// Ни состояния, ни обращения к карте, диску, сети или глобальным переменным:
// всё нужное приходит аргументами. Поэтому пакет тестируется таблицами без
// клиента и сервера.
//
// Это НЕ задача Дубинса: Дубинс ищет путь между двумя полностью заданными
// позами, а здесь задана только конечная точка — курс в ней свободен.
//
// Семейство трасс v1 — четыре формы: прямая; одна дуга; прямая + дуга;
// дуга + прямая. Для целей в передней полуплоскости начальной позы
// (проекция a на курс неотрицательна) прямая+дуга и дуга+прямая с радиусом не
// меньше MinRadiusM достигают ровно того же множества точек, что прямая и одна
// дуга: условие достижимости у всех форм одно (a² ≥ 2·|b|·Rmin − b², где b —
// боковое отклонение). Поэтому v1 строит простейшую форму. Задняя полуплоскость
// требует дуг более полуоборота или обратных кривых — вне семейства, честный
// отказ.
//
// Жёсткое правило: дуга меньше MinRadiusM не возвращается ни в каком статусе.
// Сервер не имеет права молча уменьшить радиус, чтобы дотянуться до курсора:
// не дотянулись — adjusted с причиной и трассой до ближайшей допустимой точки,
// либо infeasible с причиной и ближайшей допустимой точкой.
package solver

import (
	"fmt"
	"math"

	"github.com/shady2k/ClearAhead/server/internal/catalog"
	"github.com/shady2k/ClearAhead/server/internal/geom"
	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/units"
)

// Status — статус результата решателя. Строковые значения совпадают со
// значениями поля status проволочного контракта (спека map-editor §3).
type Status string

const (
	StatusFeasible   Status = "feasible"   // трасса достигла точки под курсором
	StatusAdjusted   Status = "adjusted"   // трасса построена до ближайшей допустимой точки, не до курсора
	StatusInfeasible Status = "infeasible" // семейство v1 не строит трассу; причина и ближайшая точка в результате
)

// Intent — намерение игрока (спека map-editor §5). В v1 достаточно первых трёх.
type Intent uint8

const (
	IntentExtend    Intent = iota // продолжить путь от порта
	IntentConnect                 // влить в существующий путь
	IntentTerminate               // закончить упором
)

// Violation — нарушение нормы или границы семейства: причина отказа либо то,
// чем пришлось поступиться. Машиночитаемый код + человеческая строка.
type Violation struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Коды нарушений. Стабильны: их сравнивает клиент и пишут в логи.
const (
	CodeRadiusBelowMinimum = "radius_below_minimum"
	CodeTargetBehind       = "target_behind"
	CodeReverseCurve       = "reverse_curve_required"
	CodeZeroLength         = "zero_length"
	CodeInvalidProfile     = "invalid_profile"
	CodeUnknownIntent      = "unknown_intent"
	CodeInvalidTarget      = "invalid_target"
)

// DeviceProposal — предложение устройства на трассе. В v1 всегда пусто:
// каталог устройств наполняется отдельной задачей.
type DeviceProposal struct {
	DeviceTypeID string    `json:"device_type_id"`
	Pose         geom.Pose `json:"pose"`
}

// Result — структурный ответ решателя (спека map-editor §3).
type Result struct {
	Status          Status           `json:"status"`
	Alignment       geom.Chain       `json:"alignment"`
	Terminal        geom.Pose        `json:"terminal_pose"`
	DeviceProposals []DeviceProposal `json:"device_proposals"`
	Warnings        []string         `json:"warnings"`
	Violations      []Violation      `json:"violations"`
	// NearestFeasible — ближайшая допустимая точка: граница достижимого
	// множества, к которой честно ведёт результат. Ненулевой при adjusted и
	// infeasible; при feasible равен nil (трасса дошла до курсора).
	NearestFeasible *geom.Pose `json:"nearest_feasible"`
}

// Допуски чистых чисел, метры. Микрометр — разрешение домена: ниже него вопрос
// «где точка» не имеет смысла.
const (
	collinearTolM = 1e-6 // точка на луче курса, если боковое отклонение меньше
	zeroDistTolM  = 1e-6 // цель, совпадающая с началом: нулевая трасса
)

// Solve — единственная точка входа пакета: чистая функция.
//
// start — поза, от которой строится трасса; target — точка под курсором
// (заголовок игнорируется: курс в цели свободен); intent — намерение игрока;
// profile — нормы (используется MinRadiusM); cat — каталог устройств (в v1 не
// используется, допустимо nil).
//
// Геометрия v1 общая для всех трёх намерений: продолжение от порта к точке.
// connect дополнительно помечается предупреждением (совпадение касательной с
// целевым путём не проверяется — пути в аргументах нет), terminate —
// предупреждением об упоре (предложение устройства появится с наполнением
// каталога).
func Solve(start geom.Pose, target geom.Pose, intent Intent, profile mapfmt.Profile, cat *catalog.Catalog) Result {
	start = geom.Normalize(start)

	// Чистая функция остаётся тотальной: NaN/Inf в аргументах — отказ с
	// причиной, а не паника в геометрии. Заголовок цели игнорируется
	// контрактом, поэтому проверяется только её положение.
	if !allFinite(start.X, start.Y, start.Heading) || !allFinite(target.X, target.Y) {
		anchor := start
		if !allFinite(anchor.X, anchor.Y, anchor.Heading) {
			anchor = geom.Pose{}
		}
		return infeasibleResult(CodeInvalidTarget,
			"недопустимые координаты: начало или цель содержат NaN/Inf", anchor)
	}

	switch intent {
	case IntentExtend, IntentConnect, IntentTerminate:
	default:
		return infeasibleResult(CodeUnknownIntent,
			fmt.Sprintf("неизвестное намерение %d", intent), start)
	}

	if !(profile.MinRadiusM > 0) || math.IsNaN(profile.MinRadiusM) || math.IsInf(profile.MinRadiusM, 0) {
		return infeasibleResult(CodeInvalidProfile,
			fmt.Sprintf("профиль не задан: MinRadiusM = %v", profile.MinRadiusM), start)
	}

	res := Result{
		Status:          StatusFeasible,
		DeviceProposals: []DeviceProposal{},
		Warnings:        []string{},
	}
	switch intent {
	case IntentConnect:
		res.Warnings = append(res.Warnings,
			"connect в v1: цель — точка; совпадение точки и касательной с целевым путём не проверяется")
	case IntentTerminate:
		res.Warnings = append(res.Warnings,
			"terminate: буферный упор в конце трассы; предложение устройства появится с наполнением каталога (v1 — пусто)")
	}

	res.Alignment, res.Terminal, res.NearestFeasible, res.Violations, res.Status =
		solveExtend(start, target, profile.MinRadiusM)
	return res
}

// solveExtend — геометрия «продолжить от порта к точке».
//
// Разложение цели в системе начальной позы: a — проекция на курс, b — знаковое
// боковое отклонение (влево положительное). Достижимое множество v1 — передняя
// полуплоскость (a ≥ 0) без «линзы»: двух кругов радиуса MinRadiusM,
// касательных к начальной позе. Внутри линзы единственная касательная дуга
// имеет радиус меньше MinRadiusM — это жёсткая граница нормы, не подгонка:
// дуга не возвращается, вместо неё — adjusted до границы линзы.
func solveExtend(start, target geom.Pose, minRadius float64) (geom.Chain, geom.Pose, *geom.Pose, []Violation, Status) {
	u := geom.Pose{X: math.Cos(start.Heading), Y: math.Sin(start.Heading)} // единичный вектор курса
	n := geom.Pose{X: -u.Y, Y: u.X}                                        // левая нормаль

	vx, vy := target.X-start.X, target.Y-start.Y
	a := vx*u.X + vy*u.Y
	b := vx*n.X + vy*n.Y
	d := math.Hypot(vx, vy)

	if d < zeroDistTolM {
		return nil, geom.Pose{}, nearestPose(start, 0, 0),
			[]Violation{{Code: CodeZeroLength, Message: "цель совпадает с начальной позой: нулевая трасса"}},
			StatusInfeasible
	}

	if math.Abs(b) <= collinearTolM {
		if a < 0 {
			// Точка на продолжении курса назад: прямая назад не входит в семейство.
			return nil, geom.Pose{}, nearestPose(start, 0, 0),
				[]Violation{{Code: CodeTargetBehind, Message: "точка позади начальной позы: семейство трасс v1 не строит пути назад"}},
				StatusInfeasible
		}
		// F1: прямая.
		chain := geom.Chain{straight(a)}
		return chain, chain.End(start), nil, nil, StatusFeasible
	}

	if a < 0 {
		// Задняя полуплоскость в стороне от курса: единственная касательная дуга
		// была бы длиннее полуоборота, а обратная (S-образная) кривая вне
		// семейства v1. Ближайшая допустимая точка — на границе передней
		// полуплоскости.
		return nil, geom.Pose{}, nearestRear(start, n, b, minRadius),
			[]Violation{{Code: CodeReverseCurve, Message: "точка требует обратной кривой (S-образная трасса) или дуги более полуоборота: вне семейства v1"}},
			StatusInfeasible
	}

	// F2: единственная касательная дуга через точку. Радиус выводится из хорды:
	// d = 2·R·sin(θ/2), θ = 2·atan2(b, a) → R = d²/(2·|b|).
	theta := 2 * math.Atan2(b, a)
	r := (a*a + b*b) / (2 * math.Abs(b))
	if r >= minRadius {
		chain := geom.Chain{arc(r, theta)}
		return chain, chain.End(start), nil, nil, StatusFeasible
	}

	// Линза: касательная дуга требует радиус меньше MinRadiusM. Ближайшая
	// допустимая точка — проекция цели на границу линзы, то есть на окружность
	// радиуса MinRadiusM на стороне цели. Дуга к ней имеет радиус ровно
	// MinRadiusM — не меньше.
	cx := start.X + math.Copysign(minRadius, b)*n.X
	cy := start.Y + math.Copysign(minRadius, b)*n.Y
	dx, dy := target.X-cx, target.Y-cy
	dist := math.Hypot(dx, dy)
	var px, py float64
	if dist < zeroDistTolM {
		// Цель — ровно в центре граничной окружности: проекция не определена,
		// любая точка окружности равноудалена. Берём детерминированную точку
		// границы вперёд по курсу: (Rmin, sign(b)·Rmin) в локальных координатах.
		px = start.X + minRadius*u.X + math.Copysign(minRadius, b)*n.X
		py = start.Y + minRadius*u.Y + math.Copysign(minRadius, b)*n.Y
	} else {
		px, py = cx+minRadius*dx/dist, cy+minRadius*dy/dist
	}

	if math.Hypot(px-start.X, py-start.Y) < zeroDistTolM {
		// Проекция совпала с началом: даже ближайшая допустимая точка даёт
		// нулевую трассу.
		return nil, geom.Pose{}, nearestPose(start, 0, 0),
			[]Violation{{Code: CodeRadiusBelowMinimum,
				Message: fmt.Sprintf("точка требует радиус %.1f м < минимального %.1f м; ближайшая допустимая точка — начало трассы", r, minRadius)}},
			StatusInfeasible
	}

	// Угол дуги — в локальных координатах (u, n) начальной позы: проекция
	// глобального смещения на курс и нормаль. Мировая разность координат тут
	// не годится — при ненулевом курсе начала она даст неверный угол.
	a2 := (px-start.X)*u.X + (py-start.Y)*u.Y
	b2 := (px-start.X)*n.X + (py-start.Y)*n.Y
	chain := geom.Chain{arc(minRadius, 2*math.Atan2(b2, a2))}
	terminal := chain.End(start)
	nearest := geom.Pose{X: px, Y: py, Heading: terminal.Heading}
	return chain, terminal, &nearest,
		[]Violation{{Code: CodeRadiusBelowMinimum,
			Message: fmt.Sprintf("точка требует радиус %.1f м < минимального %.1f м; трасса построена до ближайшей допустимой точки", r, minRadius)}},
		StatusAdjusted
}

// nearestRear — ближайшая допустимая точка к цели в задней полуплоскости.
// Достижимое множество начинается на перпендикуляре к курсу через начало:
// точка (0, b) на нём достижима полукругом радиуса |b|/2, если |b| ≥ 2·Rmin;
// иначе перпендикуляр накрыт линзой, и ближайшая граница — само начало.
func nearestRear(start, n geom.Pose, b, minRadius float64) *geom.Pose {
	if math.Abs(b) >= 2*minRadius {
		return nearestPose(start, b*n.X, b*n.Y)
	}
	return nearestPose(start, 0, 0)
}

// nearestPose — точка-граница в системе начальной позы: start плюс смещение
// (offU, offN) вдоль курса и левой нормали. Заголовок наследует начальный:
// у точки-границы направления нет.
func nearestPose(start geom.Pose, offU, offN float64) *geom.Pose {
	u := geom.Pose{X: math.Cos(start.Heading), Y: math.Sin(start.Heading)}
	n := geom.Pose{X: -u.Y, Y: u.X}
	p := geom.Pose{
		X:       start.X + offU*u.X + offN*n.X,
		Y:       start.Y + offU*u.Y + offN*n.Y,
		Heading: start.Heading,
	}
	return &p
}

// infeasibleResult собирает отказ: причина во violations, ближайшая точка —
// граница достижимого множества, трасса пуста. Срезы инициализированы пустыми,
// чтобы сериализация давала [], а не null (спека map-editor §3).
func infeasibleResult(code, message string, start geom.Pose) Result {
	return Result{
		Status:          StatusInfeasible,
		DeviceProposals: []DeviceProposal{},
		Warnings:        []string{},
		Violations:      []Violation{{Code: code, Message: message}},
		NearestFeasible: nearestPose(start, 0, 0),
	}
}

// allFinite — тотальность чистых чисел: ни одного NaN или ±Inf.
func allFinite(vals ...float64) bool {
	for _, v := range vals {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
	}
	return true
}

// straight и arc собирают примитив, чья невалидность исключена построением
// (длина ≥ zeroDistTolM, радиус ≥ minRadius > 0, |угол| ≤ π и ненулевой).
// Паника здесь — баг решателя, а не входных данных.
func straight(lengthM float64) geom.Primitive {
	l, err := units.MetersToDistance(lengthM)
	if err != nil {
		panic(fmt.Sprintf("solver: длина прямой %v м не помещается в Distance: %v", lengthM, err))
	}
	p, err := geom.Straight(l)
	if err != nil {
		panic(fmt.Sprintf("solver: прямая %v: %v", lengthM, err))
	}
	return p
}

func arc(radiusM, angleRad float64) geom.Primitive {
	p, err := geom.Arc(radiusM, angleRad)
	if err != nil {
		panic(fmt.Sprintf("solver: дуга R=%v θ=%v: %v", radiusM, angleRad, err))
	}
	return p
}
