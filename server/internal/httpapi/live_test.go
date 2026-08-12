package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/match"
	"github.com/shady2k/ClearAhead/server/internal/netloc"
)

func liveFixture() *match.Match {
	return &match.Match{ID: "M1", Region: "ST_A", Units: []match.Unit{{
		ID: "LOCO_1", Type: "VL80",
		At: netloc.PointU{Element: "E_MAIN", U: 150, Direction: netloc.DirForward},
	}}}
}

func TestLiveServesUnits(t *testing.T) {
	rec := httptest.NewRecorder()
	NewLiveHandler(liveFixture()).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/regions/ST_A/live", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("код %d", rec.Code)
	}
	// no-store, а НЕ immutable и не ETag: это состояние, и повторяемость ответа
	// сегодня — временное свойство того, что ничего не движется.
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("Cache-Control %q, ожидалось no-store", cc)
	}
	var got struct {
		Region string `json:"region"`
		Match  string `json:"match"`
		Units  []struct {
			ID   string        `json:"id"`
			Type string        `json:"type"`
			At   netloc.PointU `json:"at"`
		} `json:"units"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if got.Region != "ST_A" || got.Match != "M1" {
		t.Fatalf("регион %q партия %q", got.Region, got.Match)
	}
	if len(got.Units) != 1 {
		t.Fatalf("единиц %d", len(got.Units))
	}
	u := got.Units[0]
	if u.ID != "LOCO_1" || u.Type != "VL80" {
		t.Fatalf("единица %+v", u)
	}
	if u.At.Element != "E_MAIN" || u.At.U != 150 || u.At.Direction != netloc.DirForward {
		t.Fatalf("адрес %+v: положение обязано доехать целиком, включая направление", u.At)
	}
}

// TestLiveEmptyMatchIsNotFound404 — партия без состава отвечает ПУСТЫМ СПИСКОМ,
// а не 404: партия существует, состава в ней нет. 404 означал бы, что мира нет.
func TestLiveEmptyMatchIsNotFound404(t *testing.T) {
	rec := httptest.NewRecorder()
	NewLiveHandler(&match.Match{ID: "M1", Region: "ST_A"}).
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/regions/ST_A/live", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("код %d, ожидался 200", rec.Code)
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("разбор: %v", err)
	}
	units, ok := got["units"].([]any)
	if !ok || len(units) != 0 {
		t.Fatalf("units = %v, ожидался пустой список, а не null", got["units"])
	}
}

func TestLiveRefusals(t *testing.T) {
	cases := []struct {
		name, method, path string
		want               int
	}{
		{"чужой регион", http.MethodGet, "/regions/ST_B/live", http.StatusNotFound},
		{"чужая форма адреса", http.MethodGet, "/regions/ST_A/live/units", http.StatusNotFound},
		{"ревизия в адресе", http.MethodGet, "/regions/ST_A/revisions/2/live", http.StatusNotFound},
		{"запись", http.MethodPost, "/regions/ST_A/live", http.StatusMethodNotAllowed},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			NewLiveHandler(liveFixture()).ServeHTTP(rec, httptest.NewRequest(c.method, c.path, nil))
			if rec.Code != c.want {
				t.Fatalf("код %d, ожидался %d", rec.Code, c.want)
			}
		})
	}
}
