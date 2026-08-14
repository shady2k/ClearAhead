package terrain

import (
	"math"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
)

// withBuilding возвращает карту затравки с ДОБАВЛЕННОЙ постройкой.
//
// Поле строится из карты целиком (buildField), и валидация здесь не зовётся
// намеренно — ровно как у grading-тестов: тест проверяет КОМПИЛЯЦИЮ эффекта,
// а не форму карты. Оригинальная карта не мутируется — добавляется копия.
func withBuilding(t *testing.T, m *mapfmt.Map, b mapfmt.Building) *mapfmt.Map {
	t.Helper()
	clone := *m
	obj := *m.Objects
	clone.Objects = &obj
	clone.Objects.Buildings = append(append([]mapfmt.Building(nil), m.Objects.Buildings...), b)
	return &clone
}

// TestBuildingHardFootprintRefusesPathViolation — у постройки есть ЖЁСТКИЙ
// габарит: площадка под пятном на отметке природной поверхности в точке
// привязки. Путь, чья основная площадка пересекает пятно на ДРУГОЙ отметке, —
// два несовместимых жёстких габарита, отказ на компиляции (спека §4.2,
// пост-инвариант §2.2.2: поверхность обязана удовлетворять габариту КАЖДОГО
// объекта). До sqym.11 габарита у постройки не было вовсе, и инвариант был
// вакуумно истинен.
func TestBuildingHardFootprintRefusesPathViolation(t *testing.T) {
	m := loadMap(t)
	// Постройка на оси пути: пятно 30×30 м покрывает основную площадку, а
	// площадка постройки стоит на ПРИРОДНОЙ отметке в точке привязки — почти
	// наверняка не на отметке оси (датум s6v: земля под путём на высоту
	// конструкции ниже головки рельса).
	b := mapfmt.Building{ID: "bld-hard", X: 128, Y: 2, Heading: 0, Width: 30, Depth: 30, Height: 8}
	plain, _ := buildField(t, m)
	platform, err := plain.HeightCm(128, 2)
	if err != nil {
		t.Fatalf("отметка площадки пути: %v", err)
	}
	nat := plain.NaturalM(128, 2)
	natCm := math.Round((nat - plain.BaseZ()) * 100)
	if natCm == float64(platform) {
		t.Skipf("природная отметка (%d см) совпала с площадкой пути в точке — тест не различает, точка не та", int64(natCm))
	}
	f, _ := buildField(t, withBuilding(t, m, b))
	if _, err := f.HeightCm(128, 2); err == nil {
		t.Fatal("путь, нарушающий жёсткий габарит постройки, принят — два несовместимых жёстких габарита обязаны отказать")
	}
}

// TestBuildingEarthEffectFromNaturalSurfaceOrderIndependent — земляной эффект
// постройки есть ЧИСТАЯ ФУНКЦИЯ природной поверхности и собственного исходника
// (инвариант §3): эффект не читает рабочую поверхность и результат другого
// эффекта, поэтому порядок двух построек на результат не влияет. Два поля с
// постройками в разном порядке обязаны дать байт в байт одинаковые отметки в
// области, где откосы обеих построек перекрываются.
func TestBuildingEarthEffectFromNaturalSurfaceOrderIndependent(t *testing.T) {
	m := loadMap(t)
	a := mapfmt.Building{ID: "bld-a", X: 600, Y: -400, Heading: 0.3, Width: 30, Depth: 24, Height: 10}
	b := mapfmt.Building{ID: "bld-b", X: 648, Y: -392, Heading: -0.2, Width: 26, Depth: 22, Height: 9}
	fAB, _ := buildField(t, withBuilding(t, withBuilding(t, m, a), b))
	fBA, _ := buildField(t, withBuilding(t, withBuilding(t, m, b), a))

	for x := 560.0; x <= 690; x += 4 {
		for y := -440.0; y <= -360; y += 4 {
			hAB, errAB := fAB.HeightCm(x, y)
			hBA, errBA := fBA.HeightCm(x, y)
			if (errAB == nil) != (errBA == nil) {
				t.Fatalf("в (%v, %v) отказы разошлись: %v против %v — композиция зависит от порядка", x, y, errAB, errBA)
			}
			if errAB != nil {
				continue
			}
			if hAB != hBA {
				t.Fatalf("в (%v, %v) отметки разошлись: %d против %d см — порядок построек влияет на результат", x, y, hAB, hBA)
			}
		}
	}

	// Площадка постройки стоит на ПРИРОДНОЙ отметке точки привязки: внутри
	// пятна земля лежит на NaturalM(привязка), а не на рабочей поверхности.
	natA := fAB.NaturalM(a.X, a.Y)
	wantA := math.Round((natA - fAB.BaseZ()) * 100)
	for _, p := range [][2]float64{{a.X, a.Y}, {a.X + 10, a.Y - 8}, {a.X - 12, a.Y + 6}} {
		got, err := fAB.HeightCm(p[0], p[1])
		if err != nil {
			t.Fatalf("площадка постройки в (%v, %v): %v", p[0], p[1], err)
		}
		if float64(got) != wantA {
			t.Fatalf("площадка постройки в (%v, %v): %d см, ожидалась природная отметка %d см", p[0], p[1], got, int64(wantA))
		}
	}
}

// TestBuildingPlatformOutsidePathMatchesNatural — площадка постройки вдали от
// пути не конфликтует ни с чем: земля внутри пятна лежит ровно на природной
// отметке точки привязки, и откос за кромкой уходит к натуре, не проваливаясь
// сквозь неё (насыпь/выемка max/min, как у пути).
func TestBuildingPlatformOutsidePathMatchesNatural(t *testing.T) {
	m := loadMap(t)
	b := mapfmt.Building{ID: "bld-away", X: -800, Y: -900, Heading: 0, Width: 20, Depth: 20, Height: 8}
	f, _ := buildField(t, withBuilding(t, m, b))

	nat := f.NaturalM(b.X, b.Y)
	want := math.Round((nat - f.BaseZ()) * 100)
	got, err := f.HeightCm(b.X, b.Y)
	if err != nil {
		t.Fatalf("центр пятна: %v", err)
	}
	if float64(got) != want {
		t.Fatalf("центр пятна: %d см, ожидалась природная отметка %d см", got, int64(want))
	}
	// За кромкой пятна земля уходит к натуре: на удалении 60 м (втрое больше
	// полугабарита) отметка уже природная, а не площадка.
	gotFar, err := f.HeightCm(b.X+60, b.Y)
	if err != nil {
		t.Fatalf("за кромкой: %v", err)
	}
	natFar := math.Round((f.NaturalM(b.X+60, b.Y) - f.BaseZ()) * 100)
	if float64(gotFar) != natFar {
		t.Fatalf("за кромкой: %d см, ожидалась природная %d см — откос не вернулся к натуре", gotFar, int64(natFar))
	}
}
