package httpapi

import (
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/seedmap"
	"github.com/shady2k/ClearAhead/server/internal/terrain"
	"github.com/shady2k/ClearAhead/server/internal/track"
)

// Река едет в проводе с ЗАМЕРЕННЫМ урезом, а не с одной шириной на всю реку.
//
// Проверяется не «поле есть», а то, ради чего замер и заведён: полуширины
// положительны и не упираются в потолок поиска. Потолок означает, что земля
// нигде не поднялась до уреза, то есть вода ушла в поле, — и это единственный
// признак разлива, видимый снаружи.
func TestObjectsCarryRiverWithMeasuredWaterline(t *testing.T) {
	m := seedmap.Station(seedmap.WithTerrain())
	_, els, err := track.Propagate(m)
	if err != nil {
		t.Fatalf("распространение поз: %v", err)
	}
	f, err := terrain.New(m, els)
	if err != nil {
		t.Fatalf("рельеф: %v", err)
	}
	out := BuildObjects(m, f)
	if len(out.Rivers) != 1 {
		t.Fatalf("рек в проводе %d, ожидалась 1", len(out.Rivers))
	}
	r := out.Rivers[0]
	src := m.Objects.Rivers[0]
	if len(r.Axis) != len(src.Axis) {
		t.Fatalf("точек оси %d, в карте %d — провод их не досчитал", len(r.Axis), len(src.Axis))
	}
	cap := src.HalfWidthM + src.BankM
	for i, p := range r.Axis {
		if p.HalfLeft <= 0 || p.HalfRight <= 0 {
			t.Fatalf("точка %d: урез вырожден (%.2f, %.2f) — ленты не будет", i, p.HalfLeft, p.HalfRight)
		}
		if p.HalfLeft >= cap-1e-9 || p.HalfRight >= cap-1e-9 {
			t.Fatalf("точка %d: урез упёрся в потолок %.1f м — вода за бровкой", i, cap)
		}
	}
}

// Карта без рельефа: река едет, урез не выдуман.
func TestRiverWithoutTerrainHasNoWaterline(t *testing.T) {
	m := seedmap.Station()
	m.Objects = &mapfmt.Objects{Rivers: []mapfmt.River{{
		ID:         "RIV_T",
		Axis:       []mapfmt.RiverPoint{{X: 0, Y: 0, Z: 10}, {X: 100, Y: 0, Z: 9}},
		HalfWidthM: 10, BankM: 10, DepthM: 2, RimM: 1, ValleyM: 50,
	}}}
	out := BuildObjects(m, nil)
	if len(out.Rivers) != 1 {
		t.Fatalf("рек %d", len(out.Rivers))
	}
	for i, p := range out.Rivers[0].Axis {
		if p.HalfLeft != 0 || p.HalfRight != 0 {
			t.Errorf("точка %d: урез замерен без рельефа (%.2f, %.2f) — замерять было не по чему",
				i, p.HalfLeft, p.HalfRight)
		}
	}
}
