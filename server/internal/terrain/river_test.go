package terrain

import (
	"math"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/chunk"
	"github.com/shady2k/ClearAhead/server/internal/seedmap"
	"github.com/shady2k/ClearAhead/server/internal/track"
)

// riverField ST_A и её единственная река — общая оснастка проверок воды.
func riverFixture(t *testing.T) (*Field, riverField) {
	t.Helper()
	m := loadMap(t)
	f, _ := buildField(t, m)
	if len(f.rivers) != 1 {
		t.Fatalf("рек в поле %d, ожидалась 1", len(f.rivers))
	}
	return f, f.rivers[0]
}

// Точки оси русла с их отметкой уреза — то, относительно чего проверяется всё
// остальное.
func riverAxisPoints(t *testing.T) []axisPoint {
	t.Helper()
	m := loadMap(t)
	if m.Objects == nil || len(m.Objects.Rivers) != 1 {
		t.Fatal("в затравке нет ровно одной реки")
	}
	return densifyRiver(m.Objects.Rivers[0].Axis)
}

// ГЛАВНЫЙ ИНВАРИАНТ ВОДЫ: земля под урезом только внутри русла.
//
// Он и есть причина, по которой у реки появилась долина. Первая редакция
// обходилась берегом, и замер показал разлив: урез упирался в потолок поиска у
// трети точек оси, то есть вода уходила в поле. Проверка стоит здесь, а не на
// глаз по снимку, потому что разлив виден только с воздуха и только если знать,
// куда смотреть.
func TestRiverValleyKeepsWaterInChannel(t *testing.T) {
	f, r := riverFixture(t)
	outside := r.halfWidthM + r.bankM
	worst := math.Inf(1)
	for _, p := range riverAxisPoints(t) {
		// Нормаль берём поперёк меандра грубо — по обе стороны от точки оси
		// в восьми направлениях: русло узкое, и любое направление, уводящее
		// дальше берега, обязано выйти из воды.
		for k := 0; k < 8; k++ {
			a := float64(k) / 8 * 2 * math.Pi
			nx, ny := math.Cos(a), math.Sin(a)
			x, y := p.X+nx*outside, p.Y+ny*outside
			// Точка могла оказаться ближе к соседнему витку меандра — тогда она
			// законно ниже уреза, и мерить по ней нечего.
			if d, _, ok := r.grid.nearest(x, y, outside); ok && d < outside-1e-6 {
				continue
			}
			worst = math.Min(worst, f.WorkedM(x, y)-p.Z)
		}
	}
	if worst < 0 {
		t.Errorf("за бровкой земля на %.2f м НИЖЕ уреза — вода уходит в поле", -worst)
	}
}

// Дно лежит ровно на глубину ниже уреза: иначе река либо мельче объявленного,
// либо прорезает дно шумом.
func TestRiverBedSitsAtDeclaredDepth(t *testing.T) {
	f, r := riverFixture(t)
	for _, p := range riverAxisPoints(t) {
		got := f.NaturalM(p.X, p.Y)
		want := p.Z - r.depthM
		// Допуск, а не побитовое равенство: правило проекта про float64.
		if math.Abs(got-want) > 1e-6 {
			t.Fatalf("дно в (%.1f, %.1f): %.4f, ожидалось %.4f", p.X, p.Y, got, want)
		}
	}
}

// Урез ПАДАЕТ по течению. Река, текущая в гору, — ошибка карты, а не рисунка,
// и увидеть её на кадре нельзя.
func TestRiverSurfaceFallsDownstream(t *testing.T) {
	pts := riverAxisPoints(t)
	// Течение на запад: x убывает — z обязан убывать тоже.
	for i := 1; i < len(pts); i++ {
		if pts[i].X < pts[i-1].X && pts[i].Z > pts[i-1].Z+1e-9 {
			t.Fatalf("урез растёт против течения: x %.0f -> %.0f, z %.3f -> %.3f",
				pts[i-1].X, pts[i].X, pts[i-1].Z, pts[i].Z)
		}
	}
}

// Песок ТОЛЬКО у русла. Класс объявлен в проводе с самого начала и до появления
// воды не порождался ни разу; теперь он обязан порождаться — и обязан не
// вылезать в поле, где воды нет. Это ровно та ошибка, которую спайк нашёл у
// себя сам: у него пляж проверялся по высоте, и поясок появлялся на ровном
// месте.
func TestSandAppearsOnlyNearRiver(t *testing.T) {
	f, r := riverFixture(t)
	band := r.halfWidthM + r.sandBandM
	sand := 0
	for y := -200.0; y <= 600.0; y += 8 {
		for x := -100.0; x <= 900.0; x += 8 {
			cls, _ := f.CoverAt(x, y)
			if cls != chunk.SurfaceSand {
				continue
			}
			sand++
			d, _, ok := r.grid.nearest(x, y, band+1)
			if !ok || d > band+1e-6 {
				t.Fatalf("песок в (%.0f, %.0f) на %.1f м от русла при поясе %.1f м", x, y, d, band)
			}
		}
	}
	if sand == 0 {
		t.Error("песка нет нигде: класс объявлен и снова не порождается")
	}
}

// Лес не растёт в русле и на мокром берегу.
func TestForestDoesNotGrowInTheRiver(t *testing.T) {
	f, r := riverFixture(t)
	for _, p := range riverAxisPoints(t) {
		cls, _ := f.CoverAt(p.X, p.Y)
		if cls == chunk.SurfaceForestConifer || cls == chunk.SurfaceForestBroad {
			t.Fatalf("лес прямо в русле, в (%.1f, %.1f)", p.X, p.Y)
		}
	}
	// И на мокром берегу — до бровки включительно.
	for _, p := range riverAxisPoints(t) {
		for k := 0; k < 4; k++ {
			a := float64(k) / 4 * math.Pi
			d := r.halfWidthM + r.bankM*0.5
			cls, _ := f.CoverAt(p.X+math.Cos(a)*d, p.Y+math.Sin(a)*d)
			if cls == chunk.SurfaceForestConifer || cls == chunk.SurfaceForestBroad {
				t.Fatalf("лес на мокром берегу в %.0f м от оси русла", d)
			}
		}
	}
}

// Карта БЕЗ реки строится и считается: река — не обязательный блок.
func TestFieldWithoutRiversStillBuilds(t *testing.T) {
	m := loadMap(t)
	m.Objects.Rivers = nil
	_, els, err := track.Propagate(m)
	if err != nil {
		t.Fatal(err)
	}
	f, err := New(m, els)
	if err != nil {
		t.Fatalf("поле без реки не построилось: %v", err)
	}
	if len(f.rivers) != 0 {
		t.Error("реки взялись из ниоткуда")
	}
	if math.IsNaN(f.NaturalM(0, 0)) {
		t.Error("натура без реки не считается")
	}
}

// seedmap — фабрика, и её река обязана быть валидной вместе со всей картой.
func TestSeedRiverIsValid(t *testing.T) {
	m := seedmap.Station(seedmap.WithTerrain())
	if m.Objects == nil || len(m.Objects.Rivers) == 0 {
		t.Fatal("в затравке нет реки")
	}
	r := m.Objects.Rivers[0]
	if r.RimM <= 0 || r.ValleyM <= 0 {
		t.Errorf("бровка %.2f и долина %.1f — река без них воду не держит", r.RimM, r.ValleyM)
	}
}
