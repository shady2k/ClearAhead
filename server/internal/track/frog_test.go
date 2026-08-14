package track

import (
	"math"
	"strings"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/geom"
	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/seedmap"
	"github.com/shady2k/ClearAhead/server/internal/units"
)

// frogEls строит элементы проходов стрелки SW1 с нулевой стартовой позой.
// Геометрия по умолчанию — ровно ST_A_SW_1: прямой проход — прямая 33.5 м,
// боковой — дуга R=300, угол −0.1107 (правая стрелка, проход 33.21 м).
func frogEls(straight, diverging geom.Chain) map[string]Element {
	return map[string]Element{
		seedmap.StationSW1 + mapfmt.PassageStraight: {
			ID: seedmap.StationSW1 + mapfmt.PassageStraight, Start: PortPose{Plan: geom.Pose{}}, Plan: straight,
		},
		seedmap.StationSW1 + mapfmt.PassageDiverging: {
			ID: seedmap.StationSW1 + mapfmt.PassageDiverging, Start: PortPose{Plan: geom.Pose{}}, Plan: diverging,
		},
	}
}

func frogTypes() map[string]mapfmt.TrackType {
	return map[string]mapfmt.TrackType{
		seedmap.TrackTypeID: {ID: seedmap.TrackTypeID, Name: "TRACK_MAIN_1520", Gauge: 1.520},
	}
}

func frogConstruction() *mapfmt.Construction {
	return &mapfmt.Construction{DefaultType: seedmap.TrackTypeID}
}

func sw1Right() mapfmt.Turnout {
	return mapfmt.Turnout{ID: seedmap.StationSW1, Kind: mapfmt.KindRail, Hand: "right"}
}

// frogNarrowTypeID — ручной тип колеи 1.0 (метка NARROW). UUID взят из таблицы
// mapfmt/helpers_test.go (tID12, метка TYPE_X): фикстура свой UUID не выдумывает.
const frogNarrowTypeID = "01a3185c-500d-7242-8242-00000c424242"

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
// колея 1.520) крестовина попадает на s ≈ 30.21 м при длине бокового прохода
// 33.21 м. Допуск — 0.05 м, как в плане волны 2a, задача 4.
//
// Ожидание пересчитано, а не подогнано: боковой проход — дуга R=300, отход
// достигает колеи при cos θ = 1 − g/R, откуда s = R·arccos(1 − g/R). Для 1.435
// это давало 29.355, для 1.520 даёт 30.212 (ClearAhead-3ay, колея мира).
func TestFrogST_A_SW_1(t *testing.T) {
	straight := mustChain(t, primStraight(t, 33.5))
	diverging := mustChain(t, primArc(t, 300, -0.1107))
	els := frogEls(straight, diverging)
	f, err := frogFeature(els, frogTypes(), frogConstruction(), sw1Right())
	if err != nil {
		t.Fatalf("крестовина: %v", err)
	}
	if f.Owner != seedmap.StationSW1 || f.Kind != "frog" {
		t.Fatalf("особенность %+v, ожидалась frog стрелки %s", f, seedmap.StationSW1)
	}
	if len(f.Addresses) != 2 {
		t.Fatalf("адресов %d, ожидалось 2", len(f.Addresses))
	}
	a0, a1 := f.Addresses[0], f.Addresses[1]
	if a0.Element != seedmap.StationSW1+mapfmt.PassageStraight || a1.Element != seedmap.StationSW1+mapfmt.PassageDiverging {
		t.Fatalf("порядок адресов не «прямой, затем боковой»: %s, %s", a0.Element, a1.Element)
	}
	// Принятое число: u бокового прохода ≈ 30.21 м, допуск 0.05 м.
	if d := math.Abs(a1.U - 30.21); d > 0.05 {
		t.Fatalf("крестовина на u=%g бокового прохода, ожидалось 30.21 ± 0.05", a1.U)
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
	if math.Abs(f.Addresses[1].U-30.21) > 0.05 {
		t.Fatalf("левая стрелка: u=%g, ожидалось 30.21 ± 0.05", f.Addresses[1].U)
	}
	if d := math.Abs(f.Point.Y - (1.520 / 2)); d > 0.05 {
		t.Fatalf("левая стрелка: point.y=%g, ожидалось +gauge/2 = %g", f.Point.Y, 1.520/2)
	}
}

// TestFrogDeviceType — gauge берётся из типа САМОГО устройства, а не из типа
// примыкающих run'ов (спека §4).
func TestFrogDeviceType(t *testing.T) {
	straight := mustChain(t, primStraight(t, 33.5))
	diverging := mustChain(t, primArc(t, 300, -0.1107))
	els := frogEls(straight, diverging)
	types := frogTypes()
	types[frogNarrowTypeID] = mapfmt.TrackType{ID: frogNarrowTypeID, Name: "NARROW", Gauge: 1.0}
	c := frogConstruction()
	c.DefaultType = frogNarrowTypeID
	turnout := sw1Right()
	f, err := frogFeature(els, types, c, turnout)
	if err != nil {
		t.Fatalf("крестовина: %v", err)
	}
	// Колея 1.0: нитки сходятся при R(1−cos) = 1.0 → s ≈ 24.5 м, не 30.21.
	if math.Abs(f.Addresses[1].U-30.21) < 4 {
		t.Fatalf("крестовина посчитана по чужой колее: u=%g при gauge=1.0", f.Addresses[1].U)
	}
	// Явный type устройства перебивает умолчание.
	turnout.Type = seedmap.TrackTypeID
	f, err = frogFeature(els, types, c, turnout)
	if err != nil {
		t.Fatalf("крестовина: %v", err)
	}
	if math.Abs(f.Addresses[1].U-30.21) > 0.05 {
		t.Fatalf("явный тип устройства не применён: u=%g, ожидалось 30.21 ± 0.05", f.Addresses[1].U)
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

// TestCompileFrogFixture — сквозная проверка на карте фабрики: крестовина
// SW1 попадает на u ≈ 30.17 м бокового прохода. Это тот же критерий, что
// TestFrogST_A_SW_1, но через весь компилятор и целую карту.
//
// Число на 4 см меньше чистых 30.21 выше не от неточности: там дуга бралась
// одна, здесь проход идёт через настоящую цепочку примитивов карты. Разница
// была и до смены колеи (29.32 против 29.36) и осталась той же.
func TestCompileFrogFixture(t *testing.T) {
	rg := compileStation(t)
	byID := map[string]RenderFeature{}
	for _, f := range rg.Features {
		byID[f.Owner] = f
	}
	f, ok := byID[seedmap.StationSW1]
	if !ok {
		t.Fatalf("в геометрии станции нет крестовины %s", seedmap.StationSW1)
	}
	if len(f.Addresses) != 2 || f.Addresses[1].Element != seedmap.StationSW1+mapfmt.PassageDiverging {
		t.Fatalf("адреса крестовины %+v", f.Addresses)
	}
	if d := math.Abs(f.Addresses[1].U - 30.17); d > 0.05 {
		t.Fatalf("крестовина SW1 на u=%g, ожидалось 30.17 ± 0.05", f.Addresses[1].U)
	}
	if len(rg.Features) != 2 {
		t.Fatalf("крестовин %d, ожидалось 2 (по одной на стрелку горловины)", len(rg.Features))
	}
}

// compileStation компилирует станцию фабрики.
func compileStation(t *testing.T) *RenderGeometry {
	t.Helper()
	_, rg, err := Compile(seedmap.Station())
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
