package mapfmt

import (
	"fmt"
	"math"
)

// MaxInitialAngle — потолок начального угла остряка, радианы: половина угла
// марки 1/6, то есть заведомо больше любого настоящего β0 и заведомо меньше
// самой пологой марки.
const MaxInitialAngle = 0.08

// turnouttype.go — ССЫЛКА КАРТЫ НА КАТАЛОГ и ПРАВИЛА, КОТОРЫМ КАТАЛОГ ОБЯЗАН
// ОТВЕЧАТЬ.
//
// Сам каталог — в turnout_catalog.go. Здесь два разных вопроса, и разделены они
// нарочно:
//
//	о КАРТЕ — «ссылается ли стрелка на существующий проект»: спрашивается при
//	  всякой валидации, потому что ошибиться может автор;
//	о КАТАЛОГЕ — «правдоподобны ли числа проекта»: спрашивается тестом, потому
//	  что ошибиться может только тот, кто правит код каталога, и спрашивать это у
//	  каждой карты значило бы проверять сборку данными пользователя.

// validateTurnoutTypes проверяет ссылки стрелок на каталог сервера.
func validateTurnoutTypes(m *Map) error {
	for _, t := range m.Topology.Turnouts {
		if t.TurnoutType == "" {
			return fmt.Errorf("mapfmt: стрелка %s: не указан turnout_type — марку перевода подставить нельзя",
				Labeled(t.Name, t.ID))
		}
		if _, err := TurnoutProjectByID(t.TurnoutType); err != nil {
			return fmt.Errorf("%w (стрелка %s)", err, Labeled(t.Name, t.ID))
		}
	}
	return nil
}

// ValidateTurnoutProject — правила, которым обязана отвечать запись каталога.
//
// Экспортирована ради теста каталога: проверяемое здесь — свойство КОДА, а не
// карты, и вызывать её на каждой валидации значило бы гонять одни и те же числа
// сотни раз, получая один и тот же ответ.
func ValidateTurnoutProject(t TurnoutType) error {
	where := Labeled(t.Name, t.ID)
	bad := func(field string, got, lo, hi float64) error {
		return fmt.Errorf("mapfmt: проект перевода %s: %s = %v вне [%v, %v]", where, field, got, lo, hi)
	}
	if t.Frog == "" {
		return fmt.Errorf("mapfmt: проект перевода %s: не указана марка крестовины", where)
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
	if err := checkRailSectionOf("проект перевода "+where+", blade_rail", t.BladeRail); err != nil {
		return fmt.Errorf("mapfmt: %w", err)
	}
	// ОСТРЯК НЕ ВЫШЕ РАМНОГО РЕЛЬСА. Он лежит на стрелочной подушке толщиной в
	// разность высот, и профиль выше путевого означал бы подушку отрицательной
	// толщины — то есть остряк, утопленный в брус.
	//
	// Сравнить с путевым рельсом здесь не с чем: тип пути у стрелки свой и
	// приходит из карты. Проверка живёт там, где видны оба
	// (checkBladeRailFitsTrack).
	if err := validateTrackFrog(where, t.FrogSet); err != nil {
		return err
	}
	return checkProjectTurnAgreement(t)
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

// checkProjectTurnAgreement — НАЧАЛЬНЫЙ УГОЛ И ЦЕПОЧКА ВМЕСТЕ ДАЮТ МАРКУ.
//
// # Почему это проверяется, хотя источник теперь один
//
// До 2026-08-17 β0 лежал в каталоге карты, а форма прохода — в geometry.turnouts
// той же карты, и правило сверяло два независимых авторских числа. Теперь оба
// живут в одной записи каталога, и сверять авторов нечего.
//
// Правило осталось потому, что проверяет оно НЕ СОГЛАСИЕ ИСТОЧНИКОВ, а
// внутреннюю правду записи: марка объявлена дробью, и она обязана быть тем же
// самым углом, который перевод действительно набирает остряком и кривыми.
// Разойдись они — перевод придёт в крестовину не под своей маркой, а узнать об
// этом было бы неоткуда: нитки всё равно пересекутся, просто не там.
//
// Замер, ради которого правило заведено: у проекта 2434 β0 = 0.023921, две дуги
// дают 0.086737, сумма 0.110658 против arctan(1/9) = 0.110657.
func checkProjectTurnAgreement(t TurnoutType) error {
	want, err := TurnoutTotalTurn(t.Frog)
	if err != nil {
		return fmt.Errorf("%s (проект %s)", err, Labeled(t.Name, t.ID))
	}
	var turn float64
	for _, h := range t.Passages.Diverging {
		if h.Kind != "arc" {
			continue
		}
		a := h.Angle
		if a == 0 && h.Radius > 0 {
			a = h.Length / h.Radius
		}
		turn += math.Abs(a)
	}
	got := t.Switch.InitialAngle + turn
	if math.Abs(got-want) > TurnAngleTol {
		return fmt.Errorf(
			"mapfmt: проект перевода %s: начальный угол остряка %.6f плюс поворот бокового прохода %.6f дают %.6f, а марка %s требует %.6f — перевод придёт в крестовину не под своей маркой",
			Labeled(t.Name, t.ID), t.Switch.InitialAngle, turn, got, t.Frog, want)
	}
	return nil
}
