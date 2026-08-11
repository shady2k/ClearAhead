package track

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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

	// RenderGeometryHash считается по ТЕМ САМЫМ БАЙТАМ, которые уедут клиенту,
	// а не по рукописной модели рядом с ними.
	//
	// Первая редакция писала свою текстовую выжимку и не включала в неё Slope,
	// хотя он сериализуется в JSON. Тело менялось, хеш — нет, и клиент с
	// Cache-Control: immutable получал 304 и НАВСЕГДА сохранял устаревшую
	// геометрию. Ошибка не давала отказа: сервер стартовал, всё выглядело
	// исправным.
	//
	// Пока хешируется выжимка, любое новое поле провода надо не забыть добавить
	// и туда. Забудут. Байты ответа забыть нельзя: они и есть ответ.
	body, err := renderBody(rg)
	if err != nil {
		return Manifest{}, err
	}
	rh := sha256.Sum256(body)
	return Manifest{
		MapID:              m.MapID,
		Revision:           m.MapRevision,
		TrackHash:          hex.EncodeToString(th.Sum(nil)),
		RenderGeometryHash: hex.EncodeToString(rh[:]),
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

	// Порты и переходы сериализуются поимённо и по порядку: их число не
	// фиксировано, поэтому позиционная запись «общий, прямой, боковой» больше
	// невозможна. Порядок задан Passages() и PortIDs() и потому детерминирован.
	tids := make([]string, 0, len(ct.Devices))
	for id := range ct.Devices {
		tids = append(tids, id)
	}
	sort.Strings(tids)
	for _, id := range tids {
		writeDevice(w, ct.Devices[id])
	}

	oids := make([]string, 0, len(ct.Trackside))
	for id := range ct.Trackside {
		oids = append(oids, id)
	}
	sort.Strings(oids)
	for _, id := range oids {
		for _, sp := range ct.Trackside[id] {
			fmt.Fprintf(w, "ts|%s|%s|%d|%d|%s\n", id, sp.Element, int64(sp.From), int64(sp.To), sp.Direction)
		}
	}
}

// RenderBody сериализует геометрию ровно так, как её отдаёт ручка.
//
// Единственное место сериализации провода: и хеш, и HTTP-ответ обязаны брать
// байты отсюда, иначе они разойдутся, а ETag начнёт лгать.
func RenderBody(rg *RenderGeometry) ([]byte, error) { return renderBody(rg) }

func renderBody(rg *RenderGeometry) ([]byte, error) {
	b, err := json.Marshal(rg)
	if err != nil {
		return nil, fmt.Errorf("track: сериализация геометрии: %w", err)
	}
	return b, nil
}

// writeDevice сериализует устройство для хеша: заголовок, порты, переходы.
// Вынесено отдельно, потому что число портов и переходов не фиксировано и
// правило записи должно быть в одном месте.
func writeDevice(w io.Writer, d CompiledDevice) {
	fmt.Fprintf(w, "dev|%s|%s|%s\n", d.ID, d.Hand, d.Resource)
	for _, p := range d.Ports {
		fmt.Fprintf(w, "dev.port|%s|%s\n", d.ID, p)
	}
	for _, tr := range d.Traversals {
		fmt.Fprintf(w, "dev.tr|%s|%s|%s|%s\n", d.ID, tr.Passage, tr.From, tr.To)
	}
}
