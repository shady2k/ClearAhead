// Package track — компилятор пути: профиль, распространение поз, компиляция.
//
// Здесь встречаются две координаты вдоль пути и здесь же они расходятся:
//
//	u — авторский пикетаж вдоль горизонтальной проекции, приходит из mapfmt;
//	s — пространственная длина оси, уходит в CompiledNetwork и в TrackPos.
//
// Правило имён: в этом пакете каждая величина длины несёт букву в имени.
package track

import (
	"fmt"
	"math"

	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/units"
)

// Segment — звено вертикальной цепочки. Уклон меняется по u линейно от
// StartSlope до EndSlope; равенство даёт постоянный уклон.
type Segment struct {
	LengthU    units.Distance
	StartSlope float64 // безразмерное dz/du
	EndSlope   float64
}

// Profile — вертикальное выравнивание элемента.
type Profile []Segment

// ProfileFrom строит профиль из вертикальной цепочки. Пустая цепочка даёт
// плоский профиль объявленной длины: станция В1 плоская, и это законный случай,
// а не отсутствие данных.
func ProfileFrom(a mapfmt.Alignments, lengthU units.Distance) (Profile, error) {
	if len(a.Vertical) == 0 {
		return Profile{{LengthU: lengthU}}, nil
	}
	p := make(Profile, 0, len(a.Vertical))
	slope := 0.0
	for i, v := range a.Vertical {
		l, err := units.MetersToDistance(v.Length)
		if err != nil {
			return nil, fmt.Errorf("track: вертикаль[%d]: %w", i, err)
		}
		switch v.Kind {
		case "grade":
			slope = v.SlopePermille / 1000
			p = append(p, Segment{LengthU: l, StartSlope: slope, EndSlope: slope})
		case "vertical_curve":
			end := v.EndSlopePermille / 1000
			p = append(p, Segment{LengthU: l, StartSlope: slope, EndSlope: end})
			slope = end
		default:
			return nil, fmt.Errorf("track: вертикаль[%d]: неизвестный примитив %q", i, v.Kind)
		}
	}
	return p, nil
}

// LengthU возвращает длину профиля в авторской координате.
func (p Profile) LengthU() units.Distance {
	var u units.Distance
	for _, s := range p {
		u += s.LengthU
	}
	return u
}

// LengthS возвращает пространственную длину оси.
//
// Правило округления: длина каждого сегмента округляется отдельно, сумма берётся
// по целым. Округление математической суммы дало бы другой результат, и сумма
// частей перестала бы равняться целому.
//
// Ошибка возвращается, а не проглатывается: частичная сумма выглядит как
// правдоподобная длина, и поезд поехал бы по ней, не заметив.
func (p Profile) LengthS() (units.Distance, error) {
	var s units.Distance
	for i, seg := range p {
		d, err := units.MetersToDistance(seg.spatialLen())
		if err != nil {
			return 0, fmt.Errorf("track: сегмент профиля %d: %w", i, err)
		}
		s += d
	}
	return s, nil
}

// UToS переводит авторское смещение в пространственное.
func (p Profile) UToS(u units.Distance) (units.Distance, error) {
	if u < 0 {
		return 0, fmt.Errorf("track: отрицательное смещение %s", u)
	}
	var s units.Distance
	left := u
	for _, seg := range p {
		if left <= seg.LengthU {
			head := Segment{
				LengthU:    left,
				StartSlope: seg.StartSlope,
				EndSlope:   seg.slopeAt(left),
			}
			d, err := units.MetersToDistance(head.spatialLen())
			if err != nil {
				return 0, err
			}
			return s + d, nil
		}
		left -= seg.LengthU
		d, err := units.MetersToDistance(seg.spatialLen())
		if err != nil {
			return 0, err
		}
		s += d
	}
	return 0, fmt.Errorf("track: смещение %s выходит за длину профиля %s", u, p.LengthU())
}

// At возвращает подъём от начала профиля и уклон на смещении u.
func (p Profile) At(u units.Distance) (float64, float64, error) {
	if u < 0 {
		return 0, 0, fmt.Errorf("track: отрицательное смещение %s", u)
	}
	dz := 0.0
	left := u
	for _, seg := range p {
		if left <= seg.LengthU {
			return dz + seg.riseOver(left), seg.slopeAt(left), nil
		}
		dz += seg.riseOver(seg.LengthU)
		left -= seg.LengthU
	}
	return 0, 0, fmt.Errorf("track: смещение %s выходит за длину профиля %s", u, p.LengthU())
}

// slopeAt — уклон внутри сегмента: линейная интерполяция по u.
func (s Segment) slopeAt(u units.Distance) float64 {
	if s.LengthU == 0 {
		return s.StartSlope
	}
	t := float64(u) / float64(s.LengthU)
	return s.StartSlope + (s.EndSlope-s.StartSlope)*t
}

// riseOver — подъём на первых u сегмента. Уклон линеен, значит подъём — интеграл
// линейной функции, то есть площадь трапеции.
func (s Segment) riseOver(u units.Distance) float64 {
	return (s.StartSlope + s.slopeAt(u)) / 2 * u.Meters()
}

// spatialLen — пространственная длина сегмента в метрах.
//
// s = ∫₀ᴸ √(1+g²) du. При линейном g подстановка g = g₀ + k·u даёт замкнутую
// форму через первообразную ∫√(1+g²)dg = (g√(1+g²) + asinh g)/2. Численного
// интегрирования не требуется, поэтому детерминизм держится на математике, а не
// на шаге сетки.
func (s Segment) spatialLen() float64 {
	l := s.LengthU.Meters()
	if s.StartSlope == s.EndSlope {
		return l * math.Sqrt(1+s.StartSlope*s.StartSlope)
	}
	k := (s.EndSlope - s.StartSlope) / l
	f := func(g float64) float64 { return (g*math.Sqrt(1+g*g) + math.Asinh(g)) / 2 }
	return (f(s.EndSlope) - f(s.StartSlope)) / k
}
