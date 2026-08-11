package httpapi

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/mapstore"
)

// networkURL — адрес сети региона: имя региона и ревизию клиент берёт из
// манифеста региона, как берёт их и здесь.
func networkURL(region string, rev int) string {
	return "/regions/" + region + "/revisions/" + strconv.Itoa(rev) + "/network"
}

// newNetworkTestHandler поднимает ручку сети над картой-затравкой в памяти.
//
// Состояние отдаётся наружу целиком, а не одним манифестом: проверять
// приходится не только заголовки, но и ТЕЛО — а сверять его можно лишь с теми
// байтами, по которым считан ETag (mapstore.State.RenderBody).
func newNetworkTestHandler(t *testing.T) (http.Handler, *mapstore.State) {
	t.Helper()
	s := mapstore.Open()
	st, err := s.New()
	if err != nil {
		t.Fatalf("новая карта: %v", err)
	}
	return NewNetworkHandler(s), st
}

func TestRegionNetworkIsServedWhole(t *testing.T) {
	h, st := newNetworkTestHandler(t)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", networkURL(st.Manifest.MapID, st.Manifest.Revision), nil))
	if w.Code != 200 {
		t.Fatalf("код %d, ожидалось 200", w.Code)
	}
	// ETag — network_hash, и никакой иной: он посчитан по ТЕМ САМЫМ
	// байтам, которые уходят клиенту (track.BuildManifest), поэтому описать
	// чужое тело не может.
	if got, want := w.Header().Get("ETag"), `"`+st.Manifest.NetworkHash+`"`; got != want {
		t.Fatalf("ETag %q, ожидалось %q", got, want)
	}
	// immutable здесь честен: адрес называет ревизию, а (регион, ревизия)
	// определяет ровно одно тело.
	if cc := w.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Fatalf("Cache-Control %q не содержит immutable", cc)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("Content-Type %q, ожидался application/json", ct)
	}
	// Переезд адреса не имел права тронуть тело: байт в байт то же, что лежит в
	// mapstore.RenderBody. Сравнение байтами законно именно здесь — это не
	// вычисленные float64, а ровно те байты, которые сериализованы один раз при
	// входе карты в память.
	if !bytes.Equal(w.Body.Bytes(), st.RenderBody) {
		t.Fatalf("тело сети разошлось с mapstore.RenderBody: %d байт против %d",
			w.Body.Len(), len(st.RenderBody))
	}
}

func TestNetworkIsServed304ForItsOwnETag(t *testing.T) {
	h, st := newNetworkTestHandler(t)
	r := httptest.NewRequest("GET", networkURL(st.Manifest.MapID, st.Manifest.Revision), nil)
	r.Header.Set("If-None-Match", `"`+st.Manifest.NetworkHash+`"`)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 304 {
		t.Fatalf("код %d, ожидалось 304", w.Code)
	}
	if w.Body.Len() != 0 {
		t.Fatalf("304 с телом длиной %d", w.Body.Len())
	}
}

func TestNetworkRejectsForeignRegionRevisionAndMethod(t *testing.T) {
	h, st := newNetworkTestHandler(t)
	id := st.Manifest.MapID
	cases := []struct {
		name, method, path string
		want               int
	}{
		{"чужой регион", "GET", networkURL("OTHER", st.Manifest.Revision), 404},
		{"не та ревизия", "GET", networkURL(id, st.Manifest.Revision+5), 404},
		{"ревизия ноль", "GET", networkURL(id, 0), 404},
		{"чужой метод", "POST", networkURL(id, st.Manifest.Revision), 405},
	}
	for _, c := range cases {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(c.method, c.path, nil))
		if w.Code != c.want {
			t.Fatalf("%s (%s %s): код %d, ожидалось %d", c.name, c.method, c.path, w.Code, c.want)
		}
	}
}

func TestNetworkBadAddressIsServed404(t *testing.T) {
	h, st := newNetworkTestHandler(t)
	id := st.Manifest.MapID
	rev := strconv.Itoa(st.Manifest.Revision)
	for name, p := range map[string]string{
		"ревизия не число":  "/regions/" + id + "/revisions/abc/network",
		"пустое имя":        "/regions//revisions/" + rev + "/network",
		"нехватка сегмента": "/regions/" + id + "/revisions/" + rev,
		"лишний сегмент":    "/regions/" + id + "/revisions/" + rev + "/network/elements",
		"старое имя":        "/regions/" + id + "/revisions/" + rev + "/geometry",
		"прежний корень":    "/maps/" + id + "/revisions/" + rev + "/geometry",
		"чужой префикс":     "/regionz/" + id + "/revisions/" + rev + "/network",
		"корень":            "/",
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", p, nil))
		if w.Code != 404 {
			t.Fatalf("%s (%s): код %d, ожидалось 404", name, p, w.Code)
		}
	}
}

// TestNetworkHEAD — HEAD обязателен наравне с GET (RFC 9110): на нём держится
// проверка кэша прокси и клиентов. Первая редакция ручки геометрии отвечала на
// него 405, и поймано это было не тестом, а живым curl -I на гейте эпика.
func TestNetworkHEAD(t *testing.T) {
	h, st := newNetworkTestHandler(t)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("HEAD", networkURL(st.Manifest.MapID, st.Manifest.Revision), nil))
	if w.Code != 200 {
		t.Fatalf("HEAD: код %d, ожидалось 200", w.Code)
	}
	if got, want := w.Header().Get("ETag"), `"`+st.Manifest.NetworkHash+`"`; got != want {
		t.Fatalf("HEAD: ETag %q, ожидалось %q", got, want)
	}
}

// TestNetworkEmptyStart — карты в памяти нет: 404, а не паника.
func TestNetworkEmptyStart(t *testing.T) {
	h := NewNetworkHandler(mapstore.Open())
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", networkURL("ST_A", 1), nil))
	if w.Code != 404 {
		t.Fatalf("пустой старт: код %d, ожидался 404", w.Code)
	}
}
