// Package geom — примитивы кривых и вычисление позы.
//
// Здесь и только здесь живёт float: это presentation data (vertical-slice-design
// §7). Симуляция координат не знает, она работает на TrackPos и TrackSpan.
//
// Примитивы первой карты — прямая и дуга окружности. Общий решатель кривых не
// строится; замена примитивов остаётся содержимым этого модуля.
package geom

import (
	"fmt"
	"math"

	"github.com/shady2k/ClearAhead/internal/units"
)

// Pose — положение и направление точки пути. Метры и радианы.
type Pose struct {
	X, Y    float64
	Heading float64
}

// Kind различает примитивы кривой.
type Kind uint8

const (
	KindStraight Kind = iota
	KindArc
)

// Primitive — одно звено цепочки: прямая или дуга окружности.
//
// Дуга задаётся радиусом и знаковым углом поворота: положительный угол —
// поворот влево, отрицательный — вправо. Так центр дуги не приходится
// указывать отдельно, и стык с предыдущим звеном получается непрерывным по
// построению, а не по проверке.
type Primitive struct {
	Kind   Kind
	Length units.Distance // длина по оси пути
	Radius float64        // метры, > 0; только для дуги
	Angle  float64        // радианы со знаком; только для дуги
}

// Straight строит прямую заданной длины.
func Straight(length units.Distance) (Primitive, error) {
	if length <= 0 {
		return Primitive{}, fmt.Errorf("geom: длина прямой должна быть положительной, получено %s", length)
	}
	return Primitive{Kind: KindStraight, Length: length}, nil
}

// Arc строит дугу по радиусу в метрах и углу поворота в радианах.
//
// Длина дуги выводится из радиуса и угла, а не задаётся отдельно: две
// независимые записи одного и того же числа рано или поздно разойдутся.
func Arc(radiusM, angleRad float64) (Primitive, error) {
	if !(radiusM > 0) || math.IsInf(radiusM, 0) {
		return Primitive{}, fmt.Errorf("geom: радиус дуги должен быть положительным конечным числом, получено %v", radiusM)
	}
	if angleRad == 0 || math.IsNaN(angleRad) || math.IsInf(angleRad, 0) {
		return Primitive{}, fmt.Errorf("geom: угол дуги должен быть ненулевым конечным числом, получено %v", angleRad)
	}
	if math.Abs(angleRad) > 2*math.Pi {
		return Primitive{}, fmt.Errorf("geom: угол дуги больше полного оборота: %v рад", angleRad)
	}
	length, err := units.MetersToDistance(radiusM * math.Abs(angleRad))
	if err != nil {
		return Primitive{}, fmt.Errorf("geom: длина дуги: %w", err)
	}
	return Primitive{Kind: KindArc, Length: length, Radius: radiusM, Angle: angleRad}, nil
}

// Advance возвращает позу после прохождения t от начала примитива.
//
// t измеряется вдоль оси пути и обрезается по длине примитива вызывающим кодом;
// здесь значения вне [0, Length] допускаются и продолжают кривую аналитически —
// это удобно для стыков и для проверки непрерывности.
func (p Primitive) Advance(from Pose, t units.Distance) Pose {
	tm := t.Meters()
	switch p.Kind {
	case KindStraight:
		return Normalize(Pose{
			X:       from.X + tm*math.Cos(from.Heading),
			Y:       from.Y + tm*math.Sin(from.Heading),
			Heading: from.Heading,
		})
	case KindArc:
		// Угол берётся долей от сохранённого, а не пересчитывается как t/R.
		// Длина дуги округлена до целых микрометров, и обратный пересчёт через
		// радиус вернул бы угол с ошибкой около 3e-9 рад — на конце дуги это
		// доли микрометра, но конец дуги это стык, а стыки здесь проверяются
		// на схождение. При t == Length доля равна единице и угол выходит
		// ровно тем, что записал автор карты.
		theta := p.Angle * (float64(t) / float64(p.Length))
		// Знак радиуса переносит центр дуги на нужную сторону, и одна формула
		// покрывает оба направления поворота.
		rs := math.Copysign(p.Radius, p.Angle)
		h := from.Heading + theta
		return Normalize(Pose{
			X:       from.X + rs*(math.Sin(h)-math.Sin(from.Heading)),
			Y:       from.Y - rs*(math.Cos(h)-math.Cos(from.Heading)),
			Heading: h,
		})
	default:
		panic(fmt.Sprintf("geom: неизвестный примитив %d", p.Kind))
	}
}

// End возвращает позу в конце примитива.
func (p Primitive) End(from Pose) Pose {
	return p.Advance(from, p.Length)
}

// Chain — цепочка примитивов, образующая геометрию одного сегмента.
type Chain []Primitive

// Length возвращает суммарную длину цепочки.
func (c Chain) Length() units.Distance {
	var total units.Distance
	for _, p := range c {
		total += p.Length
	}
	return total
}

// PoseAt возвращает позу на смещении offset от начала цепочки.
//
// Смещение вне [0, Length] — отказ, а не обрезание: выход за пределы сегмента
// означает ошибку в вызывающем коде, и молчаливое приведение к границе спрятало
// бы её до того момента, когда поезд окажется не там, где считает движок.
func (c Chain) PoseAt(start Pose, offset units.Distance) (Pose, error) {
	if offset < 0 {
		return Pose{}, fmt.Errorf("geom: отрицательное смещение %s", offset)
	}
	cur := start
	left := offset
	for _, p := range c {
		if left <= p.Length {
			return Normalize(p.Advance(cur, left)), nil
		}
		left -= p.Length
		cur = p.End(cur)
	}
	if left == 0 {
		return Normalize(cur), nil
	}
	return Pose{}, fmt.Errorf("geom: смещение %s выходит за длину цепочки %s", offset, c.Length())
}

// End возвращает позу в конце цепочки.
func (c Chain) End(start Pose) Pose {
	cur := start
	for _, p := range c {
		cur = p.End(cur)
	}
	return Normalize(cur)
}

// Normalize приводит heading к полуинтервалу (-π, π].
//
// Без этого два одинаковых направления могут отличаться на 2π и разойтись при
// сравнении стыков — а стыки в этой карте проверяются на схождение.
func Normalize(p Pose) Pose {
	h := math.Mod(p.Heading, 2*math.Pi)
	if h > math.Pi {
		h -= 2 * math.Pi
	} else if h <= -math.Pi {
		h += 2 * math.Pi
	}
	p.Heading = h
	return p
}

// Reverse возвращает позу, развёрнутую на 180°: тот же камень, взгляд назад.
func Reverse(p Pose) Pose {
	p.Heading += math.Pi
	return Normalize(p)
}
