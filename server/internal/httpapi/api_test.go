package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/mapstore"
	"github.com/shady2k/ClearAhead/server/internal/track"
)

// geometryURL — URL геометрии текущей карты: id и ревизия берутся из
// манифеста, как их берёт клиент.
func geometryURL(man track.Manifest) string {
	return "/maps/" + man.MapID + "/revisions/" + strconv.Itoa(man.Revision) + "/geometry"
}

// newTestHandler поднимает сервер над картой-затравкой
// в памяти: ручки геометрии и манифеста обслуживают ровно её.
func newTestHandler(t *testing.T) (http.Handler, track.Manifest) {
	t.Helper()
	s := mapstore.Open()
	st, err := s.New()
	if err != nil {
		t.Fatalf("новая карта: %v", err)
	}
	return NewHandler(s), st.Manifest
}

func TestGeometryOK(t *testing.T) {
	h, man := newTestHandler(t)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", geometryURL(man), nil))
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
	r := httptest.NewRequest("GET", geometryURL(man), nil)
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

// TestGeometryHead — HEAD обязателен наравне с GET (RFC 9110): на нём держится
// проверка кэша прокси и клиентов. Первая редакция отвечала на него 405, и
// поймано это было не тестом, а живым curl -I на гейте эпика.
func TestGeometryHead(t *testing.T) {
	h, man := newTestHandler(t)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("HEAD", geometryURL(man), nil))
	if w.Code != 200 {
		t.Fatalf("HEAD: код %d, ожидалось 200", w.Code)
	}
	if got, want := w.Header().Get("ETag"), `"`+man.RenderGeometryHash+`"`; got != want {
		t.Fatalf("HEAD: ETag %q, ожидалось %q", got, want)
	}
}

// TestManifestOK — клиент узнаёт map_id и ревизию из манифеста, поэтому ручка
// обязана отдать их точь-в-точь, и через барьер: путь /manifest не несёт ни
// одного сегмента, диспетчер разбирает пустой вход в проверенный
// protocol.ManifestRequest.
func TestManifestOK(t *testing.T) {
	h, man := newTestHandler(t)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/manifest", nil))
	if w.Code != 200 {
		t.Fatalf("код %d, ожидалось 200", w.Code)
	}
	if cc := w.Header().Get("Cache-Control"); !strings.Contains(cc, "no-cache") {
		t.Fatalf("Cache-Control %q не содержит no-cache: манифест — не immutable-артефакт", cc)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("Content-Type %q, ожидался application/json", ct)
	}
	var got track.Manifest
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("тело манифеста не JSON: %v", err)
	}
	if got != man {
		t.Fatalf("манифест %+v, ожидался %+v", got, man)
	}
}

func TestManifestHead(t *testing.T) {
	h, _ := newTestHandler(t)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("HEAD", "/manifest", nil))
	if w.Code != 200 {
		t.Fatalf("HEAD /manifest: код %d, ожидалось 200", w.Code)
	}
}

func TestManifestRejects(t *testing.T) {
	h, _ := newTestHandler(t)
	cases := []struct {
		method, path string
		want         int
	}{
		{"POST", "/manifest", 405},
		{"GET", "/manifest/extra", 404}, // сегменты пути — невалидное представление
		{"GET", "/maps/ST_A/revisions/1/manifest", 404},
	}
	for _, c := range cases {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(c.method, c.path, nil))
		if w.Code != c.want {
			t.Fatalf("%s %s: код %d, ожидалось %d", c.method, c.path, w.Code, c.want)
		}
	}
}
