package mapfmt

import "fmt"

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
	if !(sw.Throw >= MinBladeThrow && sw.Throw <= MaxBladeThrow) {
		return bad("switch.throw", sw.Throw, MinBladeThrow, MaxBladeThrow)
	}
	// Крестовинный комплект проверяется тем же кодом, что и раньше: правила у
	// него не изменились, изменился только владелец.
	return validateTrackFrog(where, t.FrogSet)
}
