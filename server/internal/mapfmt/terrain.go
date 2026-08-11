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
