package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/track"
)

func newTestHandler(t *testing.T) (http.Handler, track.Manifest) {
	t.Helper()
	rg := &track.RenderGeometry{MapID: "ST_A", Revision: 1}
	man := track.Manifest{MapID: "ST_A", Revision: 1, RenderGeometryHash: "deadbeef"}
	return NewHandler(rg, man), man
}

func TestGeometryOK(t *testing.T) {
	h, man := newTestHandler(t)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/maps/ST_A/revisions/1/geometry", nil))
	if w.Code != 200 {
		t.Fatalf("код %d, ожидалось 200", w.Code)
	}
	if got, want := w.Header().Get("ETag"), `"`+man.RenderGeometryHash+`"`; got != want {
		t.Fatalf("ETag %q, ожидалось %q", got, want)
	}
	if cc := w.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Fatalf("Cache-Control %q не содержит immutable", cc)
	}
	if w.Body.Len() == 0 {
		t.Fatal("тело пустое, ожидалась геометрия")
	}
}

func TestGeometryNotModified(t *testing.T) {
	h, man := newTestHandler(t)
	r := httptest.NewRequest("GET", "/maps/ST_A/revisions/1/geometry", nil)
	r.Header.Set("If-None-Match", `"`+man.RenderGeometryHash+`"`)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 304 {
		t.Fatalf("код %d, ожидалось 304", w.Code)
	}
	if w.Body.Len() != 0 {
		t.Fatalf("304 с телом длиной %d", w.Body.Len())
	}
}

func TestGeometryRejects(t *testing.T) {
	h, _ := newTestHandler(t)
	cases := []struct {
		method, path string
		want         int
	}{
		{"GET", "/maps/OTHER/revisions/1/geometry", 404},
		{"GET", "/maps/ST_A/revisions/7/geometry", 404},
		{"POST", "/maps/ST_A/revisions/1/geometry", 405},
	}
	for _, c := range cases {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(c.method, c.path, nil))
		if w.Code != c.want {
			t.Fatalf("%s %s: код %d, ожидалось %d", c.method, c.path, w.Code, c.want)
		}
	}
}

func TestGeometryBadPath(t *testing.T) {
	h, _ := newTestHandler(t)
	for _, p := range []string{
		"/maps/ST_A/revisions/abc/geometry", // ревизия не число
		"/maps//revisions/1/geometry",       // пустой map_id
		"/maps/ST_A/revisions/1/",           // неверная форма
		"/",                                 // корень
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", p, nil))
		if w.Code != 404 {
			t.Fatalf("%s: код %d, ожидалось 404", p, w.Code)
		}
	}
}
