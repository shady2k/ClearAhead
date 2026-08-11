package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/mapstore"
	"github.com/shady2k/ClearAhead/server/internal/track"
)

// postJSON делает POST с JSON-телом (или без) на ручку.
func postJSON(h http.Handler, path string, body any) *httptest.ResponseRecorder {
	var r *http.Request
	if body == nil {
		r = httptest.NewRequest("POST", path, nil)
	} else {
		b, _ := json.Marshal(body)
		r = httptest.NewRequest("POST", path, bytes.NewReader(b))
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// decodeMapResult разбирает ответ {map, manifest}.
func decodeMapResult(t *testing.T, w *httptest.ResponseRecorder) (mapfmt.Map, track.Manifest) {
	t.Helper()
	var res mapResult
	dec := json.NewDecoder(w.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&res); err != nil {
		t.Fatalf("ответ не укладывается в {map, manifest}: %v", err)
	}
	return res.Map, res.Manifest
}

// TestMapsNew — «новая карта» возвращает карту, проходящую Validate и Compile,
// и её манифест.
func TestMapsNew(t *testing.T) {
	s := mapstore.Open()
	h := NewHandler(s)
	w := postJSON(h, "/maps/new", nil)
	if w.Code != 200 {
		t.Fatalf("код %d, ожидалось 200", w.Code)
	}
	m, man := decodeMapResult(t, w)
	if m.MapID != "NEW" {
		t.Fatalf("map_id %q, ожидался NEW", m.MapID)
	}
	if err := mapfmt.Validate(&m); err != nil {
		t.Fatalf("возвращённая карта не проходит валидатор: %v", err)
	}
	if _, _, err := track.Compile(&m); err != nil {
		t.Fatalf("возвращённая карта не компилируется: %v", err)
	}
	if man.MapID != "NEW" || man.Revision != 1 {
		t.Fatalf("манифест %+v, ожидался NEW ревизии 1", man)
	}
	// Геометрия новой карты доступна сразу по адресу из манифеста.
	gw := httptest.NewRecorder()
	h.ServeHTTP(gw, httptest.NewRequest("GET", geometryURL(man), nil))
	if gw.Code != 200 {
		t.Fatalf("геометрия после новой: код %d, ожидалось 200", gw.Code)
	}
	if got, want := gw.Header().Get("ETag"), `"`+man.RenderGeometryHash+`"`; got != want {
		t.Fatalf("ETag %q, ожидался %q", got, want)
	}
}

// TestMapsNewWrongMethod — операции карты принимают только свои методы.
func TestMapsNewWrongMethod(t *testing.T) {
	s := mapstore.Open()
	h := NewHandler(s)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/maps/new", nil))
	if w.Code != 405 {
		t.Fatalf("GET /maps/new: код %d, ожидался 405", w.Code)
	}
	if allow := w.Header().Get("Allow"); allow != "POST" {
		t.Fatalf("Allow %q, ожидался POST", allow)
	}
}

// TestManifestEmptyStart — пустой старт: манифест отвечает осмысленным 404, а
// не паникой.
func TestManifestEmptyStart(t *testing.T) {
	s := mapstore.Open()
	h := NewHandler(s)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/manifest", nil))
	if w.Code != 404 {
		t.Fatalf("манифест пустого старта: код %d, ожидался 404", w.Code)
	}
}

// TestGeometryEmptyStart — пустой старт: геометрия отвечает 404, а не паникой.
func TestGeometryEmptyStart(t *testing.T) {
	s := mapstore.Open()
	h := NewHandler(s)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/maps/ST_A/revisions/1/geometry", nil))
	if w.Code != 404 {
		t.Fatalf("геометрия пустого старта: код %d, ожидался 404", w.Code)
	}
}
