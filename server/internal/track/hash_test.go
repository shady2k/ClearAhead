package track

import (
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
