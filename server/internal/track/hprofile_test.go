package track

import (
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/units"
)

func TestHProfileFromStraight(t *testing.T) {
	// Одиночная прямая: радиус должен быть 0.
	prims := []mapfmt.HPrim{
		{Kind: "straight", Length: 100},
	}
	p, err := HProfileFrom(prims)
	if err != nil {
		t.Fatalf("построение прямой: %v", err)
	}
	if len(p) != 1 {
		t.Fatalf("длина профиля: %d, ожидалось 1", len(p))
	}
	if p[0].LengthU != 100*units.Meter {
		t.Fatalf("длина сегмента: %s, ожидалось 100m", p[0].LengthU)
	}
	if p[0].Radius != 0 {
		t.Fatalf("радиус на прямой: %s, ожидалось 0", p[0].Radius)
	}
}

func TestHProfileFromArc(t *testing.T) {
	// Одиночная дуга: радиус 500м, угол 0.1 рад, длина = 500*0.1 = 50 м.
	prims := []mapfmt.HPrim{
		{Kind: "arc", Radius: 500, Angle: 0.1},
	}
	p, err := HProfileFrom(prims)
	if err != nil {
		t.Fatalf("построение дуги: %v", err)
	}
	if len(p) != 1 {
		t.Fatalf("длина профиля: %d, ожидалось 1", len(p))
	}
	if p[0].Radius != 500*units.Meter {
		t.Fatalf("радиус дуги: %s, ожидалось 500m", p[0].Radius)
	}
	want := 50 * units.Meter // L = R·φ = 500·0.1
	if p[0].LengthU != want {
		t.Fatalf("длина дуги: %s, ожидалось %s", p[0].LengthU, want)
	}
}

func TestHProfileFromMixed(t *testing.T) {
	// E_MAIN: straight 50м + arc R=500 angle 0.2 + straight 80м.
	// Длина дуги = R·φ = 500·0.2 = 100м.
	// Это затравочный элемент, на котором проверим все условия.
	prims := []mapfmt.HPrim{
		{Kind: "straight", Length: 50},
		{Kind: "arc", Radius: 500, Angle: 0.2},
		{Kind: "straight", Length: 80},
	}
	p, err := HProfileFrom(prims)
	if err != nil {
		t.Fatalf("построение: %v", err)
	}
	if len(p) != 3 {
		t.Fatalf("длина профиля: %d, ожидалось 3", len(p))
	}
	if p[0].Radius != 0 {
		t.Fatalf("радиус[0]: %s, ожидалось 0", p[0].Radius)
	}
	if p[1].Radius != 500*units.Meter {
		t.Fatalf("радиус[1]: %s, ожидалось 500m", p[1].Radius)
	}
	if p[2].Radius != 0 {
		t.Fatalf("радиус[2]: %s, ожидалось 0", p[2].Radius)
	}
	// Общая длина: 50 + 100 + 80 = 230.
	want := 50*units.Meter + 100*units.Meter + 80*units.Meter
	if got := p.LengthU(); got != want {
		t.Fatalf("длина профиля: %s, ожидалось %s", got, want)
	}
}

func TestHProfileAt(t *testing.T) {
	// E_MAIN: straight 50м + arc R=500 angle 0.2 + straight 80м.
	// Длина дуги = 500·0.2 = 100м.
	prims := []mapfmt.HPrim{
		{Kind: "straight", Length: 50},
		{Kind: "arc", Radius: 500, Angle: 0.2},
		{Kind: "straight", Length: 80},
	}
	p, err := HProfileFrom(prims)
	if err != nil {
		t.Fatalf("построение: %v", err)
	}

	tests := []struct {
		u    units.Distance
		want units.Distance
		name string
	}{
		// На первой прямой: [0, 50).
		{0 * units.Meter, 0, "начало прямой 1"},
		{25 * units.Meter, 0, "середина прямой 1"},
		{49999000 * units.Micrometer, 0, "конец прямой 1 (перед границей)"},
		// На дуге: [50, 150).
		{50 * units.Meter, 500 * units.Meter, "начало дуги (граница)"},
		{100 * units.Meter, 500 * units.Meter, "середина дуги"},
		{149999000 * units.Micrometer, 500 * units.Meter, "конец дуги (перед границей)"},
		// На третьей прямой: [150, 230].
		{150 * units.Meter, 0, "начало прямой 3 (граница)"},
		{190 * units.Meter, 0, "середина прямой 3"},
		{230 * units.Meter, 0, "конец прямой 3"},
	}

	for _, tt := range tests {
		got, err := p.At(tt.u)
		if err != nil {
			t.Errorf("%s: %v", tt.name, err)
			continue
		}
		if got != tt.want {
			t.Errorf("%s (u=%s): радиус %s, ожидалось %s", tt.name, tt.u, got, tt.want)
		}
	}
}

func TestHProfileAtOutOfBounds(t *testing.T) {
	prims := []mapfmt.HPrim{
		{Kind: "straight", Length: 100},
	}
	p, err := HProfileFrom(prims)
	if err != nil {
		t.Fatalf("построение: %v", err)
	}

	tests := []struct {
		u    units.Distance
		name string
	}{
		{-1 * units.Meter, "отрицательное смещение"},
		{101 * units.Meter, "выход за границу"},
	}

	for _, tt := range tests {
		_, err := p.At(tt.u)
		if err == nil {
			t.Errorf("%s: ошибка не возвращена", tt.name)
		}
	}
}

func TestHProfileEmptyError(t *testing.T) {
	// Пустой массив — ошибка.
	prims := []mapfmt.HPrim{}
	_, err := HProfileFrom(prims)
	if err == nil {
		t.Fatalf("пустая цепочка должна дать ошибку")
	}
}

func TestHProfileUnknownKindError(t *testing.T) {
	// Неизвестный kind.
	prims := []mapfmt.HPrim{
		{Kind: "unknown", Length: 50},
	}
	_, err := HProfileFrom(prims)
	if err == nil {
		t.Fatalf("неизвестный kind должен дать ошибку")
	}
}

func TestHProfileInvalidLengthError(t *testing.T) {
	// Длина, не преобразуемая в Distance: очень большое число.
	prims := []mapfmt.HPrim{
		{Kind: "straight", Length: 1e13}, // превышает лимит Distance
	}
	_, err := HProfileFrom(prims)
	if err == nil {
		t.Fatalf("невалидная длина должна дать ошибку")
	}
}

func TestHProfileInvalidRadiusError(t *testing.T) {
	// Радиус, не преобразуемый в Distance: очень большое число.
	prims := []mapfmt.HPrim{
		{Kind: "arc", Length: 50, Radius: 1e13, Angle: 0.1}, // превышает лимит Distance
	}
	_, err := HProfileFrom(prims)
	if err == nil {
		t.Fatalf("невалидный радиус должен дать ошибку")
	}
}
