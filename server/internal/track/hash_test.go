package track

import (
	"crypto/sha256"
	"encoding/hex"

	"github.com/shady2k/ClearAhead/server/internal/geom"
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
			}},
		}
	}
	h0 := renderHashOf(t, base())

	mutations := map[string]func(*RenderGeometry){
		"map_id":             func(g *RenderGeometry) { g.MapID = "Y" },
		"map_revision":       func(g *RenderGeometry) { g.Revision = 2 },
		"element id":         func(g *RenderGeometry) { g.Elements[0].ID = "E2" },
		"start.plan.x":       func(g *RenderGeometry) { g.Elements[0].Start.Plan.X = 9 },
		"start.plan.y":       func(g *RenderGeometry) { g.Elements[0].Start.Plan.Y = 9 },
		"start.plan.heading": func(g *RenderGeometry) { g.Elements[0].Start.Plan.Heading = 0.9 },
		"start.z":            func(g *RenderGeometry) { g.Elements[0].Start.Z = 9 },
		"start.slope":        func(g *RenderGeometry) { g.Elements[0].Start.Slope = 0.04 },
		"primitive kind":     func(g *RenderGeometry) { g.Elements[0].Prims[0].Kind = "straight" },
		"primitive length":   func(g *RenderGeometry) { g.Elements[0].Prims[0].LengthM = 11 },
		"primitive radius":   func(g *RenderGeometry) { g.Elements[0].Prims[0].Radius = 400 },
		"primitive angle":    func(g *RenderGeometry) { g.Elements[0].Prims[0].Angle = -0.11 },
		"добавлен element":   func(g *RenderGeometry) { g.Elements = append(g.Elements, g.Elements[0]) },
		"добавлен primitive": func(g *RenderGeometry) { g.Elements[0].Prims = append(g.Elements[0].Prims, g.Elements[0].Prims[0]) },
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
