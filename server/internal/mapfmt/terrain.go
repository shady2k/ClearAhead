package mapfmt

import (
	"fmt"
	"math"
)

// Лимиты рельефа. Октав немного по существу: каждая следующая вдвое мельче, и
// восьми хватает на диапазон от километра до метра.
const (
	MaxTerrainOctaves = 8
	// MaxTerrainAmplitudeM — суммарный размах шума. Ограничен так, чтобы
	// отсчёты гарантированно помещались в int16 сантиметров относительно
	// base_z (±327,67 м) с запасом на земляные работы.
	MaxTerrainAmplitudeM = 250.0
)

// validateTerrain проверяет рецепт рельефа. Отказывает, не чинит.
func validateTerrain(t *Terrain) error {
	if t == nil {
		// Карта без рельефа законна: отсчётов просто нет.
		return nil
	}
	if err := checkFiniteFloat("рельеф: base_z", t.BaseZ); err != nil {
		return err
	}
	if len(t.Octaves) == 0 {
		return fmt.Errorf("mapfmt: рельеф: нет ни одной октавы; карта без рельефа записывается отсутствием блока, а не пустым блоком")
	}
	if len(t.Octaves) > MaxTerrainOctaves {
		return fmt.Errorf("mapfmt: рельеф: октав больше %d", MaxTerrainOctaves)
	}
	total := 0.0
	prev := math.Inf(1)
	for i, o := range t.Octaves {
		if err := checkFiniteFloat(fmt.Sprintf("рельеф: октава %d: длина волны", i), o.WavelengthM); err != nil {
			return err
		}
		if err := checkFiniteFloat(fmt.Sprintf("рельеф: октава %d: размах", i), o.AmplitudeM); err != nil {
			return err
		}
		if !(o.WavelengthM > 0) {
			return fmt.Errorf("mapfmt: рельеф: октава %d: длина волны должна быть положительной, получено %v", i, o.WavelengthM)
		}
		if !(o.AmplitudeM >= 0) {
			return fmt.Errorf("mapfmt: рельеф: октава %d: размах должен быть неотрицательным, получено %v", i, o.AmplitudeM)
		}
		// Порядок от крупного к мелкому — не вкусовщина: сумма считается в
		// записанном порядке, и требование порядка делает рецепт канонической
		// записью одного и того же рельефа, а не одной из перестановок.
		if o.WavelengthM >= prev {
			return fmt.Errorf("mapfmt: рельеф: октава %d: длина волны %v не меньше предыдущей %v — октавы записываются от крупной к мелкой",
				i, o.WavelengthM, prev)
		}
		prev = o.WavelengthM
		total += o.AmplitudeM
	}
	if err := validateCover(t.Cover); err != nil {
		return err
	}
	if total > MaxTerrainAmplitudeM {
		return fmt.Errorf("mapfmt: рельеф: суммарный размах %v м больше %v — отсчёты перестанут помещаться в целые сантиметры относительно base_z",
			total, MaxTerrainAmplitudeM)
	}

	e := t.Earthworks
	if err := checkFiniteFloat("рельеф: полуширина площадки", e.FormationHalfWidth); err != nil {
		return err
	}
	if err := checkFiniteFloat("рельеф: заложение откоса", e.SideSlope); err != nil {
		return err
	}
	if !(e.FormationHalfWidth > 0) {
		return fmt.Errorf("mapfmt: рельеф: полуширина основной площадки должна быть положительной, получено %v", e.FormationHalfWidth)
	}
	if !(e.SideSlope > 0) {
		return fmt.Errorf("mapfmt: рельеф: заложение откоса должно быть положительным, получено %v", e.SideSlope)
	}
	return nil
}

func checkFiniteFloat(what string, v float64) error {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return fmt.Errorf("mapfmt: %s: значение не конечно (%v)", what, v)
	}
	return nil
}

// Диапазоны рецепта покрова. Числа ПРЕДВАРИТЕЛЬНЫЕ и отвергают заведомо
// невозможное, а не проверяют замысел: источник не назначен, как и у профиля
// норм.
//
// Пороги проверяются диапазоном [-1, 1] потому, что маска — значение шума
// terrain.valueNoise, а он по построению лежит в этих границах. Порог вне их
// означает «леса нет никогда» либо «лес всюду»: и то и другое выразимо
// осмысленнее — отсутствием блока cover либо порогом на самой границе, — а
// молча принятый недостижимый порог выглядел бы как исправная карта с пустым
// лесом, и автор искал бы ошибку в другом месте.
const (
	MinCoverWavelengthM, MaxCoverWavelengthM = 1.0, 100000.0
	MaxCoverClearHalfWidthM                  = 1000.0
)

// validateCover проверяет рецепт покрова. Отказывает, не чинит.
func validateCover(c *Cover) error {
	if c == nil {
		// Карта без покрова законна: ресурс покрова просто не существует.
		return nil
	}
	wave := func(name string, v float64) error {
		if !(v >= MinCoverWavelengthM && v <= MaxCoverWavelengthM) {
			return fmt.Errorf("mapfmt: покров: %s %v вне [%v, %v] м", name, v, MinCoverWavelengthM, MaxCoverWavelengthM)
		}
		return nil
	}
	if err := wave("длина волны леса", c.ForestWavelengthM); err != nil {
		return err
	}
	if err := wave("длина волны породы", c.SpeciesWavelengthM); err != nil {
		return err
	}
	if err := wave("длина волны низового покрова", c.VegWavelengthM); err != nil {
		return err
	}
	thr := func(name string, v float64) error {
		if !(v >= -1 && v <= 1) {
			return fmt.Errorf("mapfmt: покров: %s %v вне [-1, 1] — маска есть значение шума и других значений не принимает", name, v)
		}
		return nil
	}
	if err := thr("порог леса", c.ForestThreshold); err != nil {
		return err
	}
	if err := thr("порог голой почвы", c.BareThreshold); err != nil {
		return err
	}
	if !(c.ClearHalfWidthM >= 0 && c.ClearHalfWidthM <= MaxCoverClearHalfWidthM) {
		return fmt.Errorf("mapfmt: покров: полуширина отчуждения %v вне [0, %v] м", c.ClearHalfWidthM, MaxCoverClearHalfWidthM)
	}
	return nil
}

// Диапазоны построек. Числа предварительные и отвергают заведомо невозможное.
const (
	MinBuildingSizeM   = 2.0
	MaxBuildingSizeM   = 500.0
	MinBuildingHeightM = 2.0
	MaxBuildingHeightM = 200.0
	MaxBuildings       = 100000
)

// validateObjects проверяет семантические объекты региона. Отказывает, не чинит.
func (m *Map) validateObjects() error {
	o := m.Objects
	if o == nil {
		return nil
	}
	if len(o.Buildings) > MaxBuildings {
		return fmt.Errorf("mapfmt: объекты: построек больше %d", MaxBuildings)
	}
	seen := make(map[string]bool, len(o.Buildings))
	for i := range o.Buildings {
		b := &o.Buildings[i]
		if err := ValidID("постройка", b.ID); err != nil {
			return err
		}
		if seen[b.ID] {
			return fmt.Errorf("mapfmt: постройка %q объявлена дважды", b.ID)
		}
		seen[b.ID] = true
		if err := checkFiniteFloat(fmt.Sprintf("постройка %s: x", b.ID), b.X); err != nil {
			return err
		}
		if err := checkFiniteFloat(fmt.Sprintf("постройка %s: y", b.ID), b.Y); err != nil {
			return err
		}
		if err := checkFiniteFloat(fmt.Sprintf("постройка %s: heading", b.ID), b.Heading); err != nil {
			return err
		}
		// Диапазоны, а не знак: дом шириной 0.001 м проходит «строго
		// положительно» и рисуется невидимой щепкой, а миллион таких кладёт
		// клиент.
		bad := func(what string, v, min, max float64) error {
			return fmt.Errorf("mapfmt: постройка %q: %s %g вне [%g, %g] м", b.ID, what, v, min, max)
		}
		if !(b.Width >= MinBuildingSizeM && b.Width <= MaxBuildingSizeM) {
			return bad("width", b.Width, MinBuildingSizeM, MaxBuildingSizeM)
		}
		if !(b.Depth >= MinBuildingSizeM && b.Depth <= MaxBuildingSizeM) {
			return bad("depth", b.Depth, MinBuildingSizeM, MaxBuildingSizeM)
		}
		if !(b.Height >= MinBuildingHeightM && b.Height <= MaxBuildingHeightM) {
			return bad("height", b.Height, MinBuildingHeightM, MaxBuildingHeightM)
		}
	}
	return nil
}
