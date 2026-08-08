package track

import (
	"math"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/units"
)

const um = units.Micrometer

func TestProfileFlat(t *testing.T) {
	p, err := ProfileFrom(mapfmt.Alignments{}, 1000*units.Meter)
	if err != nil {
		t.Fatalf("плоский профиль: %v", err)
	}
	got, err := p.LengthS()
	if err != nil {
		t.Fatalf("длина плоского профиля: %v", err)
	}
	if got != 1000*units.Meter {
		t.Fatalf("плоский профиль: s=%s, ожидалось %s", got, 1000*units.Meter)
	}
}

func TestProfileConstantGrade(t *testing.T) {
	a := mapfmt.Alignments{Vertical: []mapfmt.VPrim{
		{Kind: "grade", Length: 1000, SlopePermille: 40},
	}}
	p, err := ProfileFrom(a, 1000*units.Meter)
	if err != nil {
		t.Fatalf("уклон 40‰: %v", err)
	}
	want, _ := units.MetersToDistance(1000 * math.Sqrt(1+0.04*0.04))
	got, err := p.LengthS()
	if err != nil {
		t.Fatalf("длина: %v", err)
	}
	if absD(got-want) > um {
		t.Fatalf("уклон 40‰: s=%s, ожидалось %s", got, want)
	}
	// Golden: то самое расхождение, ради которого введены две координаты.
	if d := got - 1000*units.Meter; absD(d-799600*um) > 100*um {
		t.Fatalf("расхождение s-u на 40‰ и километре: %s, ожидалось ≈0.7996m", d)
	}
}

func TestProfileVerticalCurve(t *testing.T) {
	a := mapfmt.Alignments{Vertical: []mapfmt.VPrim{
		{Kind: "grade", Length: 100, SlopePermille: 0},
		{Kind: "vertical_curve", Length: 200, EndSlopePermille: 20},
		{Kind: "grade", Length: 100, SlopePermille: 20},
	}}
	p, err := ProfileFrom(a, 400*units.Meter)
	if err != nil {
		t.Fatalf("кривая: %v", err)
	}
	want := arcLen(0, 0) + arcLenLinear(0, 0.02, 200) + arcLen(0.02, 100)
	wd, _ := units.MetersToDistance(100 + want)
	got, err := p.LengthS()
	if err != nil {
		t.Fatalf("длина: %v", err)
	}
	if absD(got-wd) > 10*um {
		t.Fatalf("кривая: s=%s, ожидалось %s", got, wd)
	}
	dz, slope, err := p.At(400 * units.Meter)
	if err != nil {
		t.Fatalf("At в конце: %v", err)
	}
	if math.Abs(slope-0.02) > 1e-9 {
		t.Fatalf("уклон в конце %v, ожидалось 0.02", slope)
	}
	// Подъём: 0 на первых 100 м, среднее 0.01 на кривой, 0.02 на последних 100.
	if want := 0.0 + 0.01*200 + 0.02*100; math.Abs(dz-want) > 1e-6 {
		t.Fatalf("подъём %v м, ожидалось %v", dz, want)
	}
}

func TestProfileUToSMonotone(t *testing.T) {
	a := mapfmt.Alignments{Vertical: []mapfmt.VPrim{
		{Kind: "grade", Length: 100, SlopePermille: -10},
		{Kind: "vertical_curve", Length: 100, EndSlopePermille: 30},
	}}
	p, err := ProfileFrom(a, 200*units.Meter)
	if err != nil {
		t.Fatalf("профиль: %v", err)
	}
	prev := units.Distance(-1)
	for u := units.Distance(0); u <= 200*units.Meter; u += units.Meter {
		s, err := p.UToS(u)
		if err != nil {
			t.Fatalf("UToS(%s): %v", u, err)
		}
		if s < prev {
			t.Fatalf("UToS не монотонна: на %s получено %s после %s", u, s, prev)
		}
		prev = s
	}
}

func absD(d units.Distance) units.Distance {
	if d < 0 {
		return -d
	}
	return d
}

// arcLen — длина отрезка постоянного уклона на единицу длины (для читаемости
// теста; эталон считается независимой формулой, а не проверяемой функцией).
func arcLen(g float64, l float64) float64 { return l * math.Sqrt(1+g*g) }

// arcLenLinear — эталон для линейно меняющегося уклона, посчитанный по
// первообразной ∫√(1+g²)dg = (g√(1+g²) + asinh g)/2.
func arcLenLinear(g0, g1, l float64) float64 {
	k := (g1 - g0) / l
	f := func(g float64) float64 { return (g*math.Sqrt(1+g*g) + math.Asinh(g)) / 2 }
	return (f(g1) - f(g0)) / k
}
