package track

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"sort"

	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
)

// Manifest связывает ревизию карты с хешами её ресурсов. Пара (MapID, Revision)
// определяет ровно один манифест — иначе immutable-URL лжёт.
type Manifest struct {
	MapID              string `json:"map_id"`
	Revision           int    `json:"map_revision"`
	TrackHash          string `json:"track_hash"`
	RenderGeometryHash string `json:"render_geometry_hash"`
}

// BuildManifest считает хеши по нормализованной внутренней модели, а не по
// исходному JSON. Так снимается весь класс вопросов про каноническую
// сериализацию: порядок ключей, форма чисел, -0, экспоненты, Unicode.
func BuildManifest(m *mapfmt.Map, ct *CompiledTrack, rg *RenderGeometry) (Manifest, error) {
	th := sha256.New()
	writeTrackModel(th, m, ct)
	rh := sha256.New()
	writeRenderModel(rh, rg)
	return Manifest{
		MapID:              m.MapID,
		Revision:           m.MapRevision,
		TrackHash:          hex.EncodeToString(th.Sum(nil)),
		RenderGeometryHash: hex.EncodeToString(rh.Sum(nil)),
	}, nil
}

func writeTrackModel(w io.Writer, m *mapfmt.Map, ct *CompiledTrack) {
	fmt.Fprintf(w, "v%d|%s|%d\n", mapfmt.FormatVersion, ct.MapID, ct.Revision)

	// Геопривязка входит: она меняет смысл координат. Provenance не входит:
	// правка комментария не должна сбрасывать кэш клиента.
	if g := m.Georeference; g != nil {
		fmt.Fprintf(w, "geo|%s|%.12g|%.12g|%.12g|%s|%.12g|%.12g\n",
			g.Datum, g.Origin.Lat, g.Origin.Lon, g.Origin.H,
			g.OriginHeightKind, g.XAxisAzimuthDeg, g.GroundToGrid)
	}

	ids := make([]string, 0, len(ct.Elements))
	for id := range ct.Elements {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		e := ct.Elements[id]
		fmt.Fprintf(w, "el|%s|%s|%s|%d|%d\n", e.ID, e.From, e.To, int64(e.LengthU), int64(e.LengthS))
		for _, seg := range e.Prof {
			fmt.Fprintf(w, "  pr|%d|%.12g|%.12g\n", int64(seg.LengthU), seg.StartSlope, seg.EndSlope)
		}
	}

	tids := make([]string, 0, len(ct.Turnouts))
	for id := range ct.Turnouts {
		tids = append(tids, id)
	}
	sort.Strings(tids)
	for _, id := range tids {
		t := ct.Turnouts[id]
		fmt.Fprintf(w, "sw|%s|%s|%s|%s|%s|%s\n", t.ID, t.Hand, t.Common, t.Straight, t.Diverging, t.Resource)
	}

	oids := make([]string, 0, len(ct.Trackside))
	for id := range ct.Trackside {
		oids = append(oids, id)
	}
	sort.Strings(oids)
	for _, id := range oids {
		for _, sp := range ct.Trackside[id] {
			fmt.Fprintf(w, "ts|%s|%s|%d|%d\n", id, sp.Element, int64(sp.FromS), int64(sp.ToS))
		}
	}
}

func writeRenderModel(w io.Writer, rg *RenderGeometry) {
	fmt.Fprintf(w, "%s|%d\n", rg.MapID, rg.Revision)
	for _, e := range rg.Elements {
		fmt.Fprintf(w, "el|%s|%.12g|%.12g|%.12g|%.12g\n",
			e.ID, e.Start.Plan.X, e.Start.Plan.Y, e.Start.Plan.Heading, e.Start.Z)
		for _, p := range e.Prims {
			fmt.Fprintf(w, "  p|%s|%.12g|%.12g|%.12g\n", p.Kind, p.LengthM, p.Radius, p.Angle)
		}
	}
}
