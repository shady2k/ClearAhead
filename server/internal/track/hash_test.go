package track

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/shady2k/ClearAhead/server/internal/geom"
	"github.com/shady2k/ClearAhead/server/internal/netloc"
	"strings"
	"testing"
)

func manifestOf(t *testing.T, doc string) Manifest {
	t.Helper()
	m := loadMap(t, doc)
	ct, rg, err := Compile(m)
	if err != nil {
		t.Fatalf("компиляция: %v", err)
	}
	man, err := BuildManifest(m, ct, rg)
	if err != nil {
		t.Fatalf("манифест: %v", err)
	}
	return man
}

func TestManifestStableUnderReformatting(t *testing.T) {
	reformatted := strings.ReplaceAll(twoEdges, "\n", " ")
	reformatted = strings.ReplaceAll(reformatted, "  ", " ")
	if manifestOf(t, twoEdges).TrackHash != manifestOf(t, reformatted).TrackHash {
		t.Fatal("хеш зависит от форматирования исходного JSON")
	}
}

func TestManifestChangesOnGeometry(t *testing.T) {
	changed := strings.Replace(twoEdges, `"length": 100.0`, `"length": 100.001`, 1)
	if manifestOf(t, twoEdges).TrackHash == manifestOf(t, changed).TrackHash {
		t.Fatal("правка геометрии не изменила хеш")
	}
}

// withGeoref вставляет валидный блок геопривязки в документ: хеш должен
// зависеть от привязки (она меняет смысл координат), но не от provenance —
// правка комментария автора не должна сбрасывать кэш клиента.
const georef = `"georeference": { "datum": "WGS84",
  "origin": { "lat": 55.75, "lon": 37.62, "h": 150.0 },
  "origin_height_kind": "ellipsoidal", "x_axis_azimuth_deg": 0.0,
  "ground_to_grid": 1.0002 }`

func withGeoref(doc string, prov bool) string {
	out := strings.Replace(doc, `"map_revision": 1,`,
		`"map_revision": 1,`+"\n  "+georef+",", 1)
	if !prov {
		return out
	}
	return strings.Replace(out, `"ground_to_grid": 1.0002 }`,
		`"ground_to_grid": 1.0002, "provenance": { "author": "тест", "note": "правка" } }`, 1)
}

func TestManifestIgnoresProvenance(t *testing.T) {
	if manifestOf(t, withGeoref(twoEdges, false)).TrackHash != manifestOf(t, withGeoref(twoEdges, true)).TrackHash {
		t.Fatal("правка provenance изменила хеш")
	}
}

func TestManifestChangesOnGeoreference(t *testing.T) {
	if manifestOf(t, twoEdges).TrackHash == manifestOf(t, withGeoref(twoEdges, false)).TrackHash {
		t.Fatal("правка геопривязки не изменила хеш")
	}
}

// TestRenderHashCoversEveryWireField — ETag обязан описывать ВСЁ отдаваемое тело.
//
// Первая редакция хешировала рукописную текстовую выжимку, в которую не входил
// Slope, хотя он сериализуется в JSON. Тело менялось, хеш — нет, и клиент с
// Cache-Control: immutable получал 304 и навсегда сохранял устаревшую геометрию.
// Отказа при этом не было: сервер стартовал, всё выглядело исправным.
//
// Тест перебирает поля провода и требует, чтобы правка каждого меняла хеш.
func TestRenderHashCoversEveryWireField(t *testing.T) {
	base := func() *RenderGeometry {
		return &RenderGeometry{
			MapID: "X", Revision: 1,
			Elements: []RenderElement{{
				ID:    "E1",
				Start: PortPose{Plan: geom.Pose{X: 1, Y: 2, Heading: 0.3}, Z: 4, Slope: 0.005},
				Prims: []RenderPrimitive{{Kind: "arc", LengthM: 10, Radius: 300, Angle: 0.11}},
				Role:  &RenderRole{Turnout: "SW1", Branch: "diverging", Hand: "right", Frog: "1/9"},
			}},
			Trackside: []RenderTrackside{{
				ID: "TSP", Kind: "platform", Side: "right",
				Spans: []netloc.IntervalU{{Element: "E1", From: 10, To: 150}},
			}},
			TrackTypes: []RenderTrackType{{
				ID: "TRACK_MAIN", Gauge: 1.435,
				Sleeper: RenderSleeper{Pitch: 0.6, Length: 2.5, Width: 0.28},
				Ballast: RenderBallast{HalfWidth: 1.75},
			}},
			ConstructionRuns: []RenderRun{{
				ID: "RUN_1", Type: "TRACK_MAIN", Coordinate: "u", Phase: 0.15,
				Spans: []netloc.IntervalU{{Element: "E1", From: 0, To: 100, Direction: "forward"}},
			}},
			Features: []RenderFeature{{
				Owner: "SW1", Kind: "frog",
				Point: RenderPoint{X: 29.34, Y: -0.72},
				Addresses: []RenderAddress{{
					Element: "SW1:straight", U: 29.3,
					Tangent: RenderVec{X: 1, Y: 0},
				}},
			}},
			PlacementAlgorithm: "placement-v1",
		}
	}
	h0 := renderHashOf(t, base())

	mutations := map[string]func(*RenderGeometry){
		"map_id":                func(g *RenderGeometry) { g.MapID = "Y" },
		"map_revision":          func(g *RenderGeometry) { g.Revision = 2 },
		"element id":            func(g *RenderGeometry) { g.Elements[0].ID = "E2" },
		"start.plan.x":          func(g *RenderGeometry) { g.Elements[0].Start.Plan.X = 9 },
		"start.plan.y":          func(g *RenderGeometry) { g.Elements[0].Start.Plan.Y = 9 },
		"start.plan.heading":    func(g *RenderGeometry) { g.Elements[0].Start.Plan.Heading = 0.9 },
		"start.z":               func(g *RenderGeometry) { g.Elements[0].Start.Z = 9 },
		"start.slope":           func(g *RenderGeometry) { g.Elements[0].Start.Slope = 0.04 },
		"primitive kind":        func(g *RenderGeometry) { g.Elements[0].Prims[0].Kind = "straight" },
		"primitive length":      func(g *RenderGeometry) { g.Elements[0].Prims[0].LengthM = 11 },
		"primitive radius":      func(g *RenderGeometry) { g.Elements[0].Prims[0].Radius = 400 },
		"primitive angle":       func(g *RenderGeometry) { g.Elements[0].Prims[0].Angle = -0.11 },
		"добавлен element":      func(g *RenderGeometry) { g.Elements = append(g.Elements, g.Elements[0]) },
		"добавлен primitive":    func(g *RenderGeometry) { g.Elements[0].Prims = append(g.Elements[0].Prims, g.Elements[0].Prims[0]) },
		"role.turnout":          func(g *RenderGeometry) { g.Elements[0].Role.Turnout = "SW2" },
		"role.branch":           func(g *RenderGeometry) { g.Elements[0].Role.Branch = "straight" },
		"role.hand":             func(g *RenderGeometry) { g.Elements[0].Role.Hand = "left" },
		"role.frog":             func(g *RenderGeometry) { g.Elements[0].Role.Frog = "1/7" },
		"роль снята":            func(g *RenderGeometry) { g.Elements[0].Role = nil },
		"trackside id":          func(g *RenderGeometry) { g.Trackside[0].ID = "TSP2" },
		"trackside kind":        func(g *RenderGeometry) { g.Trackside[0].Kind = "buffer_stop" },
		"trackside side":        func(g *RenderGeometry) { g.Trackside[0].Side = "left" },
		"span element":          func(g *RenderGeometry) { g.Trackside[0].Spans[0].Element = "E2" },
		"span from":             func(g *RenderGeometry) { g.Trackside[0].Spans[0].From = 11 },
		"span to":               func(g *RenderGeometry) { g.Trackside[0].Spans[0].To = 151 },
		"добавлен span":         func(g *RenderGeometry) { g.Trackside[0].Spans = append(g.Trackside[0].Spans, g.Trackside[0].Spans[0]) },
		"тип id":                func(g *RenderGeometry) { g.TrackTypes[0].ID = "TRACK_SIDING" },
		"тип gauge":             func(g *RenderGeometry) { g.TrackTypes[0].Gauge = 1.520 },
		"тип pitch":             func(g *RenderGeometry) { g.TrackTypes[0].Sleeper.Pitch = 0.7 },
		"тип sleeper.length":    func(g *RenderGeometry) { g.TrackTypes[0].Sleeper.Length = 2.6 },
		"тип sleeper.width":     func(g *RenderGeometry) { g.TrackTypes[0].Sleeper.Width = 0.29 },
		"тип ballast":           func(g *RenderGeometry) { g.TrackTypes[0].Ballast.HalfWidth = 1.8 },
		"добавлен тип":          func(g *RenderGeometry) { g.TrackTypes = append(g.TrackTypes, g.TrackTypes[0]) },
		"run id":                func(g *RenderGeometry) { g.ConstructionRuns[0].ID = "RUN_2" },
		"run type":              func(g *RenderGeometry) { g.ConstructionRuns[0].Type = "TRACK_SIDING" },
		"run coordinate":        func(g *RenderGeometry) { g.ConstructionRuns[0].Coordinate = "s" },
		"run phase":             func(g *RenderGeometry) { g.ConstructionRuns[0].Phase = 0.3 },
		"run span element":      func(g *RenderGeometry) { g.ConstructionRuns[0].Spans[0].Element = "E2" },
		"run span from":         func(g *RenderGeometry) { g.ConstructionRuns[0].Spans[0].From = 1 },
		"run span to":           func(g *RenderGeometry) { g.ConstructionRuns[0].Spans[0].To = 99 },
		"run span direction":    func(g *RenderGeometry) { g.ConstructionRuns[0].Spans[0].Direction = "reverse" },
		"добавлен run":          func(g *RenderGeometry) { g.ConstructionRuns = append(g.ConstructionRuns, g.ConstructionRuns[0]) },
		"feature owner":         func(g *RenderGeometry) { g.Features[0].Owner = "SW2" },
		"feature kind":          func(g *RenderGeometry) { g.Features[0].Kind = "type_seam" },
		"feature point.x":       func(g *RenderGeometry) { g.Features[0].Point.X = 30 },
		"feature point.y":       func(g *RenderGeometry) { g.Features[0].Point.Y = -1 },
		"feature адрес element": func(g *RenderGeometry) { g.Features[0].Addresses[0].Element = "SW1:diverging" },
		"feature адрес u":       func(g *RenderGeometry) { g.Features[0].Addresses[0].U = 30 },
		"feature адрес tangent": func(g *RenderGeometry) { g.Features[0].Addresses[0].Tangent.X = 0.99 },
		"добавлен feature":      func(g *RenderGeometry) { g.Features = append(g.Features, g.Features[0]) },
		"placement_algorithm":   func(g *RenderGeometry) { g.PlacementAlgorithm = "placement-v2" },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			g := base()
			mutate(g)
			if h := renderHashOf(t, g); h == h0 {
				t.Fatalf("правка поля %q не изменила render_geometry_hash — ETag описывает не всё тело", name)
			}
		})
	}
}

// renderHashOf берёт хеш ровно тех байт, которые уедут клиенту.
func renderHashOf(t *testing.T, rg *RenderGeometry) string {
	t.Helper()
	b, err := RenderBody(rg)
	if err != nil {
		t.Fatalf("сериализация: %v", err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// TestManifestHashIsBodyHash — BuildManifest обязан брать хеш тех же байт.
// Без этой связки хеш и ответ снова разъедутся, просто чуть позже.
func TestManifestHashIsBodyHash(t *testing.T) {
	m := loadMap(t, twoEdges)
	ct, rg, err := Compile(m)
	if err != nil {
		t.Fatalf("компиляция: %v", err)
	}
	man, err := BuildManifest(m, ct, rg)
	if err != nil {
		t.Fatalf("манифест: %v", err)
	}
	if man.RenderGeometryHash != renderHashOf(t, rg) {
		t.Fatal("render_geometry_hash не является хешем отдаваемого тела")
	}
}
