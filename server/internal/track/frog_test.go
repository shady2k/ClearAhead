package track

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/geom"
	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/units"
)

// frogEls строит элементы проходов стрелки SW1 с нулевой стартовой позой.
// Геометрия по умолчанию — ровно ST_A_SW_1: прямой проход — прямая 33.5 м,
// боковой — дуга R=300, угол −0.1107 (правая стрелка, проход 33.21 м).
func frogEls(straight, diverging geom.Chain) map[string]Element {
	return map[string]Element{
		"SW1:straight":  {ID: "SW1:straight", Start: PortPose{Plan: geom.Pose{}}, Plan: straight},
		"SW1:diverging": {ID: "SW1:diverging", Start: PortPose{Plan: geom.Pose{}}, Plan: diverging},
	}
}

func frogTypes() map[string]mapfmt.TrackType {
	return map[string]mapfmt.TrackType{
		"TRACK_MAIN": {ID: "TRACK_MAIN", Gauge: 1.435},
	}
}

func frogConstruction() *mapfmt.Construction {
	return &mapfmt.Construction{DefaultType: "TRACK_MAIN"}
}

func sw1Right() mapfmt.Turnout {
	return mapfmt.Turnout{ID: "SW1", Hand: "right"}
}

func mustChain(t *testing.T, prims ...geom.Primitive) geom.Chain {
	t.Helper()
	var c geom.Chain
	for _, p := range prims {
		if p.Length <= 0 {
			t.Fatalf("примитив нулевой длины в фикстуре")
		}
		c = append(c, p)
	}
	return c
}

// TestFrogST_A_SW_1 — проверяемое следствие спеки §5: для ST_A_SW_1 (R=300,
// колея 1.435) крестовина попадает на s ≈ 29.36 м при длине бокового прохода
// 33.21 м. Допуск — 0.05 м, как в плане волны 2a, задача 4.
func TestFrogST_A_SW_1(t *testing.T) {
	straight := mustChain(t, primStraight(t, 33.5))
	diverging := mustChain(t, primArc(t, 300, -0.1107))
	els := frogEls(straight, diverging)
	f, err := frogFeature(els, frogTypes(), frogConstruction(), sw1Right())
	if err != nil {
		t.Fatalf("крестовина: %v", err)
	}
	if f.Owner != "SW1" || f.Kind != "frog" {
		t.Fatalf("особенность %+v, ожидалась frog стрелки SW1", f)
	}
	if len(f.Addresses) != 2 {
		t.Fatalf("адресов %d, ожидалось 2", len(f.Addresses))
	}
	a0, a1 := f.Addresses[0], f.Addresses[1]
	if a0.Element != "SW1:straight" || a1.Element != "SW1:diverging" {
		t.Fatalf("порядок адресов не «прямой, затем боковой»: %s, %s", a0.Element, a1.Element)
	}
	// Принятое число: u бокового прохода ≈ 29.36 м, допуск 0.05 м.
	if d := math.Abs(a1.U - 29.36); d > 0.05 {
		t.Fatalf("крестовина на u=%g бокового прохода, ожидалось 29.36 ± 0.05", a1.U)
	}
	if a0.U <= 0 || a1.U <= 0 {
		t.Fatalf("адреса вне проходов: straight u=%g, diverging u=%g", a0.U, a1.U)
	}
	// Касательная бокового прохода согласуется с адресом: дуга повернула на
	// −u/R от начала прохода.
	wantX, wantY := math.Cos(-a1.U/300), math.Sin(-a1.U/300)
	if math.Abs(a1.Tangent.X-wantX) > 1e-6 || math.Abs(a1.Tangent.Y-wantY) > 1e-6 {
		t.Fatalf("касательная бокового прохода (%g, %g), ожидалась (%g, %g)",
			a1.Tangent.X, a1.Tangent.Y, wantX, wantY)
	}
	if f.Point.X <= 0 || f.Point.X > 33.5 {
		t.Fatalf("point.x=%g вне прямого прохода", f.Point.X)
	}
}

// TestFrogZeroIntersections — нитки не успевают сойтись внутри устройства:
// геометрия перевода неверна, это отказ, а не «особенность отсутствует».
func TestFrogZeroIntersections(t *testing.T) {
	straight := mustChain(t, primStraight(t, 33.5))
	diverging := mustChain(t, primArc(t, 300, -0.02)) // дуга всего 6 м — не доходит до колеи
	els := frogEls(straight, diverging)
	_, err := frogFeature(els, frogTypes(), frogConstruction(), sw1Right())
	if err == nil || !strings.Contains(err.Error(), "пересечений") {
		t.Fatalf("ожидался отказ по отсутствию пересечения, получено: %v", err)
	}
}

// TestFrogTwoIntersections — больше одного пересечения: канонического ответа
// нет, отказ. Боковой проход — S-образная кривая (дуга вправо, затем дуга
// влево): её нитка дважды пересекает нитку прямого прохода внутри проходов.
func TestFrogTwoIntersections(t *testing.T) {
	straight := mustChain(t, primStraight(t, 400))
	diverging := mustChain(t, primArc(t, 300, -0.3), primArc(t, 300, 0.9))
	els := frogEls(straight, diverging)
	_, err := frogFeature(els, frogTypes(), frogConstruction(), sw1Right())
	if err == nil || !strings.Contains(err.Error(), "ровно одно") {
		t.Fatalf("ожидался отказ по второму пересечению, получено: %v", err)
	}
}

// TestFrogTangentDetection — касательное касание без пересечения это отказ
// (спека §5). Сквозной случай для прямой и дуги невыразим: расстояние от
// центра нитки-окружности до нитки-прямой и радиус отличаются ровно на колею,
// касание требовало бы gauge = 0. Проверяем детектор касания на уровне
// сегментов: две окружности, касающиеся внешне.
func TestFrogTangentDetection(t *testing.T) {
	// Окружности с центрами (0,0) и (0,200), радиусы по 100: d = r1+r2 —
	// внешнее касание, точек пересечения две совпадают в одну.
	a := &threadSeg{line: false, cx: 0, cy: 0, r: 100, uFrom: 0, uTo: 628.3, aFrom: 0, aSweep: 2 * math.Pi}
	b := &threadSeg{line: false, cx: 0, cy: 200, r: 100, uFrom: 0, uTo: 628.3, aFrom: 0, aSweep: 2 * math.Pi}
	if _, tan, err := segIntersections(a, b); err != nil {
		t.Fatalf("сегменты: %v", err)
	} else if !tan {
		t.Fatal("касание не распознано")
	}
}

// TestFrogLeftHand — левая стрелка зеркальна: прямой проход +gauge/2 ×
// боковой −gauge/2, крестовина на той же u бокового прохода.
func TestFrogLeftHand(t *testing.T) {
	straight := mustChain(t, primStraight(t, 33.5))
	diverging := mustChain(t, primArc(t, 300, 0.1107)) // левая стрелка, дуга влево
	els := frogEls(straight, diverging)
	turnout := sw1Right()
	turnout.Hand = "left"
	f, err := frogFeature(els, frogTypes(), frogConstruction(), turnout)
	if err != nil {
		t.Fatalf("крестовина: %v", err)
	}
	if math.Abs(f.Addresses[1].U-29.36) > 0.05 {
		t.Fatalf("левая стрелка: u=%g, ожидалось 29.36 ± 0.05", f.Addresses[1].U)
	}
	if d := math.Abs(f.Point.Y - (1.435 / 2)); d > 0.05 {
		t.Fatalf("левая стрелка: point.y=%g, ожидалось +gauge/2 = %g", f.Point.Y, 1.435/2)
	}
}

// TestFrogDeviceType — gauge берётся из типа САМОГО устройства, а не из типа
// примыкающих run'ов (спека §4).
func TestFrogDeviceType(t *testing.T) {
	straight := mustChain(t, primStraight(t, 33.5))
	diverging := mustChain(t, primArc(t, 300, -0.1107))
	els := frogEls(straight, diverging)
	types := frogTypes()
	types["NARROW"] = mapfmt.TrackType{ID: "NARROW", Gauge: 1.0}
	c := frogConstruction()
	c.DefaultType = "NARROW"
	turnout := sw1Right()
	f, err := frogFeature(els, types, c, turnout)
	if err != nil {
		t.Fatalf("крестовина: %v", err)
	}
	// Колея 1.0: нитки сходятся при R(1−cos) = 1.0 → s ≈ 24.5 м, не 29.36.
	if math.Abs(f.Addresses[1].U-29.36) < 4 {
		t.Fatalf("крестовина посчитана по чужой колее: u=%g при gauge=1.0", f.Addresses[1].U)
	}
	// Явный type устройства перебивает умолчание.
	turnout.Type = "TRACK_MAIN"
	f, err = frogFeature(els, types, c, turnout)
	if err != nil {
		t.Fatalf("крестовина: %v", err)
	}
	if math.Abs(f.Addresses[1].U-29.36) > 0.05 {
		t.Fatalf("явный тип устройства не применён: u=%g, ожидалось 29.36 ± 0.05", f.Addresses[1].U)
	}
}

// TestFrogUnknownType — тип устройства не разрешается: крестовину считать не
// по чему, отказ (Compile можно позвать и минуя Validate).
func TestFrogUnknownType(t *testing.T) {
	straight := mustChain(t, primStraight(t, 33.5))
	diverging := mustChain(t, primArc(t, 300, -0.1107))
	els := frogEls(straight, diverging)
	turnout := sw1Right()
	turnout.Type = "NOPE"
	if _, err := frogFeature(els, frogTypes(), frogConstruction(), turnout); err == nil {
		t.Fatal("ожидался отказ по неизвестному типу устройства")
	}
}

// TestCompileFrogST_A — сквозная проверка на настоящей карте: крестовина
// ST_A_SW_1 попадает на s ≈ 29.36 м. Это тот же критерий, что
// TestFrogST_A_SW_1, но через весь компилятор и файл карты.
func TestCompileFrogST_A(t *testing.T) {
	rg := compileStation(t)
	byID := map[string]RenderFeature{}
	for _, f := range rg.Features {
		byID[f.Owner] = f
	}
	f, ok := byID["ST_A_SW_1"]
	if !ok {
		t.Fatal("в геометрии станции нет крестовины ST_A_SW_1")
	}
	if len(f.Addresses) != 2 || f.Addresses[1].Element != "ST_A_SW_1:diverging" {
		t.Fatalf("адреса крестовины %+v", f.Addresses)
	}
	if d := math.Abs(f.Addresses[1].U - 29.36); d > 0.05 {
		t.Fatalf("крестовина ST_A_SW_1 на u=%g, ожидалось 29.36 ± 0.05", f.Addresses[1].U)
	}
	if len(rg.Features) != 8 {
		t.Fatalf("крестовин %d, ожидалось 8 (по одной на стрелку)", len(rg.Features))
	}
}

// loadMapFile разбирает настоящий файл карты из репозитория.
func loadMapFile(t *testing.T) (*mapfmt.Map, error) {
	t.Helper()
	f, err := os.Open(filepath.Join("..", "..", "maps", "st_a.json"))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return mapfmt.Decode(f)
}

// compileStation компилирует настоящую карту станции.
func compileStation(t *testing.T) *RenderGeometry {
	t.Helper()
	m, err := loadMapFile(t)
	if err != nil {
		t.Fatalf("карта: %v", err)
	}
	_, rg, err := Compile(m)
	if err != nil {
		t.Fatalf("компиляция: %v", err)
	}
	return rg
}

func primStraight(t *testing.T, lengthM float64) geom.Primitive {
	t.Helper()
	d, err := units.MetersToDistance(lengthM)
	if err != nil {
		t.Fatalf("длина: %v", err)
	}
	p, err := geom.Straight(d)
	if err != nil {
		t.Fatalf("прямая: %v", err)
	}
	return p
}

func primArc(t *testing.T, radius, angle float64) geom.Primitive {
	t.Helper()
	p, err := geom.Arc(radius, angle)
	if err != nil {
		t.Fatalf("дуга: %v", err)
	}
	return p
}
