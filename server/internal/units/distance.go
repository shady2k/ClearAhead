// Package units держит единицы измерения домена.
//
// Правило проекта: метр — единица интерфейса, микрометр — представление
// (vertical-slice-design §7). В состоянии симуляции float запрещён; всё, что
// участвует в занятости, физике и захвате ресурсов, считается в целых
// микрометрах.
package units

import (
	"fmt"
	"math"
	"strconv"
)

// Distance — расстояние в микрометрах.
//
// int64 микрометров покрывает ±9.2e12 м, то есть около 9 миллиардов километров:
// запаса хватает на любой мыслимый размер мира, а точность остаётся абсолютной
// и одинаковой в любой точке карты — в отличие от float64 в метрах, где шаг
// растёт вместе с координатой.
type Distance int64

const (
	Micrometer Distance = 1
	Millimeter          = 1000 * Micrometer
	Meter               = 1000 * Millimeter
	Kilometer           = 1000 * Meter
)

// MetersToDistance переводит метры в микрометры с округлением к ближайшему.
//
// Это единственная разрешённая точка входа float → Distance, и она существует
// ради разбора карты. Дальше по конвейеру float в домен не попадает.
func MetersToDistance(m float64) (Distance, error) {
	if math.IsNaN(m) || math.IsInf(m, 0) {
		return 0, fmt.Errorf("units: недопустимое значение метров: %v", m)
	}
	const maxMeters = float64(math.MaxInt64) / float64(Meter)
	if m > maxMeters || m < -maxMeters {
		return 0, fmt.Errorf("units: %v м не помещается в Distance", m)
	}
	return Distance(math.Round(m * float64(Meter))), nil
}

// Meters возвращает расстояние в метрах для представления и рендера.
//
// Обратно в домен результат не годится: округление уже произошло.
func (d Distance) Meters() float64 {
	return float64(d) / float64(Meter)
}

// String печатает расстояние в метрах с миллиметровой точностью.
func (d Distance) String() string {
	return strconv.FormatFloat(d.Meters(), 'f', 3, 64) + "m"
}

// MarshalJSON пишет Distance строкой.
//
// Микрометры не передаются JSON-числом сознательно: разборщики на стороне
// клиента (в частности JSON.parse_string в GDScript) читают числа как float64
// и молча теряют точность на больших значениях. Правило зафиксировано в
// revision-7-plan §4, веха В2.
func (d Distance) MarshalJSON() ([]byte, error) {
	return []byte(`"` + strconv.FormatInt(int64(d), 10) + `"`), nil
}

// UnmarshalJSON читает Distance из строки микрометров.
func (d *Distance) UnmarshalJSON(b []byte) error {
	if len(b) < 2 || b[0] != '"' || b[len(b)-1] != '"' {
		return fmt.Errorf("units: Distance ожидается строкой микрометров, получено %s", b)
	}
	v, err := strconv.ParseInt(string(b[1:len(b)-1]), 10, 64)
	if err != nil {
		return fmt.Errorf("units: разбор Distance: %w", err)
	}
	*d = Distance(v)
	return nil
}
