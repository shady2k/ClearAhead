package mapfmt

import (
	"fmt"
	"math"
)

// MaxInitialAngle — потолок начального угла остряка, радианы: половина угла
// марки 1/6, то есть заведомо больше любого настоящего β0 и заведомо меньше
// самой пологой марки.
const MaxInitialAngle = 0.08

// turnouttype.go — КАТАЛОГ ТИПОВ УСТРОЙСТВ: разрешение ссылки и проверка.
//
// Заведён отдельным файлом, а не строкой в construction.go, по той же причине,
// по какой заведён сам каталог: тип пути и тип устройства отвечают на разные
// вопросы, и складывать их проверки в одну функцию значило бы снова их смешать.

// TurnoutTypeByID разрешает ссылку стрелки на каталог.
//
// Возвращает ошибку, а не признак: ссылка на несуществующий проект — не
// состояние, а поломка карты, и молчаливое умолчание здесь запрещено тем же
// правилом, что и везде.
func (m *Map) TurnoutTypeByID(id string) (TurnoutType, error) {
	if m.Construction == nil {
		return TurnoutType{}, fmt.Errorf("mapfmt: тип устройства %s: в карте нет блока construction", id)
	}
	for _, t := range m.Construction.TurnoutTypes {
		if t.ID == id {
			return t, nil
		}
	}
	return TurnoutType{}, fmt.Errorf("mapfmt: тип устройства %s не найден в каталоге", id)
}

// validateTurnoutTypes проверяет каталог и ссылки стрелок на него.
func validateTurnoutTypes(m *Map) error {
	if m.Construction == nil {
		// Каталога нет вместе со всем блоком. Стрелку без каталога поймает
		// проверка ссылок ниже — она работает и в этом случае.
		return validateTurnoutTypeRefs(m, nil)
	}
	seen := make(map[string]bool, len(m.Construction.TurnoutTypes))
	for i := range m.Construction.TurnoutTypes {
		t := m.Construction.TurnoutTypes[i]
		if t.ID == "" {
			return fmt.Errorf("mapfmt: тип устройства[%d]: пустой id", i)
		}
		if seen[t.ID] {
			return fmt.Errorf("mapfmt: тип устройства %s объявлен дважды", t.ID)
		}
		seen[t.ID] = true
		if err := validateTurnoutType(t); err != nil {
			return err
		}
	}
	return validateTurnoutTypeRefs(m, seen)
}

func validateTurnoutTypeRefs(m *Map, known map[string]bool) error {
	for _, t := range m.Topology.Turnouts {
		if t.TurnoutType == "" {
			return fmt.Errorf("mapfmt: стрелка %s: не указан turnout_type — марку перевода подставить нельзя",
				Labeled(t.Name, t.ID))
		}
		if !known[t.TurnoutType] {
			return fmt.Errorf("mapfmt: стрелка %s: тип устройства %s не найден в каталоге",
				Labeled(t.Name, t.ID), t.TurnoutType)
		}
		dt, err := m.TurnoutTypeByID(t.TurnoutType)
		if err != nil {
			return err
		}
		if err := checkTurnoutTurnAgreement(m, t, dt); err != nil {
			return err
		}
	}
	return nil
}

func validateTurnoutType(t TurnoutType) error {
	where := Labeled(t.Name, t.ID)
	bad := func(field string, got, lo, hi float64) error {
		return fmt.Errorf("mapfmt: тип устройства %s: %s = %v вне [%v, %v]", where, field, got, lo, hi)
	}
	if t.Frog == "" {
		return fmt.Errorf("mapfmt: тип устройства %s: не указана марка крестовины", where)
	}
	sw := t.Switch
	for field, v := range map[string]float64{
		"switch.blade_length_straight":  sw.BladeLengthStraight,
		"switch.blade_length_diverging": sw.BladeLengthDiverging,
	} {
		if !(v >= MinBladeLength && v <= MaxBladeLength) {
			return bad(field, v, MinBladeLength, MaxBladeLength)
		}
	}
	// Верхняя граница начального угла — половина марки 1/6: дальше это уже не
	// начальный угол, а сама марка. Ноль законен (остряк касательного типа).
	if !(sw.InitialAngle >= 0 && sw.InitialAngle <= MaxInitialAngle) {
		return bad("switch.initial_angle", sw.InitialAngle, 0, MaxInitialAngle)
	}
	if !(sw.Throw >= MinBladeThrow && sw.Throw <= MaxBladeThrow) {
		return bad("switch.throw", sw.Throw, MinBladeThrow, MaxBladeThrow)
	}
	// ОСТРЯКОВЫЙ РЕЛЬС проверяется теми же правилами, что и путевой: сечение
	// есть сечение, и второго набора порогов для него заводить не за что.
	if !(t.BladeRail.Height >= MinRailHeight && t.BladeRail.Height <= MaxRailHeight) {
		return bad("blade_rail.height", t.BladeRail.Height, MinRailHeight, MaxRailHeight)
	}
	if !(t.BladeRail.HeadWidth >= MinRailHeadWidth && t.BladeRail.HeadWidth <= MaxRailHeadWidth) {
		return bad("blade_rail.head_width", t.BladeRail.HeadWidth, MinRailHeadWidth, MaxRailHeadWidth)
	}
	if err := checkRailSectionOf("тип устройства "+where+", blade_rail", t.BladeRail); err != nil {
		return fmt.Errorf("mapfmt: %w", err)
	}
	// ОСТРЯК НЕ ВЫШЕ РАМНОГО РЕЛЬСА. Он лежит на стрелочной подушке толщиной в
	// разность высот, и профиль выше путевого означал бы подушку отрицательной
	// толщины — то есть остряк, утопленный в брус.
	//
	// Сравнить с путевым рельсом здесь не с чем: тип пути у стрелки свой и
	// приходит из другого места. Проверка живёт там, где видны оба
	// (checkBladeRailFitsTrack).
	return validateTrackFrog(where, t.FrogSet)
}

// TurnoutTotalTurn — угол марки: на сколько боковой проход отклоняется от
// прямого целиком, радианы.
//
// Марка записана дробью «1/N», и угол есть arctan(1/N). Разбирается ЗДЕСЬ,
// потому что число нужно и проверке согласия, и всем, кто спросит про марку;
// второго разбора дроби проект не заводит.
func TurnoutTotalTurn(frog string) (float64, error) {
	var n float64
	if _, err := fmt.Sscanf(frog, "1/%g", &n); err != nil || n <= 0 {
		return 0, fmt.Errorf("mapfmt: марка %q не читается как 1/N", frog)
	}
	return math.Atan(1 / n), nil
}

// TurnAngleTol — допуск согласия между начальным углом остряка и поворотом
// цепочки бокового прохода, радианы.
//
// Десять микрорадиан: на длине перевода в тридцать метров это 0.3 мм, то есть
// заведомо мельче любой правды о геометрии и заведомо крупнее шума разбора
// чисел из JSON. Тот же порядок, что у TolHeading в распространении поз, и это
// не совпадение — вопрос один и тот же.
const TurnAngleTol = 1e-5

// checkTurnoutTurnAgreement — НАЧАЛЬНЫЙ УГОЛ И ЦЕПОЧКА ВМЕСТЕ ДАЮТ МАРКУ.
//
// # Зачем это правило существует
//
// β0 живёт в каталоге типов устройств, а форма прохода — в geometry.turnouts.
// Это не дублирование: первое есть свойство ПОРТА (под каким углом проход
// выходит из острия), второе — свойство ТРАЕКТОРИИ. Но вместе они обязаны дать
// угол марки, и без проверки согласие держалось бы на честном слове автора.
//
// Замер, ради которого правило и заведено: у проекта 2434 β0 = 0.023921, две
// дуги дают 0.086737, сумма 0.110658 против arctan(1/9) = 0.110657. Разойдись
// они — и перевод пришёл бы в крестовину не под своей маркой, а узнать об этом
// было бы неоткуда: нитки всё равно пересекутся, просто не там.
func checkTurnoutTurnAgreement(m *Map, t Turnout, dt TurnoutType) error {
	want, err := TurnoutTotalTurn(dt.Frog)
	if err != nil {
		return fmt.Errorf("%s (стрелка %s)", err, Labeled(t.Name, t.ID))
	}
	g, ok := m.Geometry.Turnouts[t.ID]
	if !ok {
		return nil // геометрии нет — о согласии говорить не с чем; это ловит своя проверка
	}
	var turn float64
	for _, h := range g.Diverging.Horizontal {
		if h.Kind != "arc" {
			continue
		}
		a := h.Angle
		if a == 0 && h.Radius > 0 {
			a = h.Length / h.Radius
		}
		turn += math.Abs(a)
	}
	got := dt.Switch.InitialAngle + turn
	if math.Abs(got-want) > TurnAngleTol {
		return fmt.Errorf(
			"mapfmt: стрелка %s: начальный угол остряка %.6f плюс поворот бокового прохода %.6f дают %.6f, а марка %s требует %.6f — перевод придёт в крестовину не под своей маркой",
			Labeled(t.Name, t.ID), dt.Switch.InitialAngle, turn, got, dt.Frog, want)
	}
	return nil
}
