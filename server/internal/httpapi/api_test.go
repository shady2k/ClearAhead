package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/mapstore"
	"github.com/shady2k/ClearAhead/server/internal/track"
)

// newTestHandler поднимает авторскую сторону над картой-затравкой в памяти:
// манифест обслуживает ровно её.
func newTestHandler(t *testing.T) (http.Handler, track.Manifest) {
	t.Helper()
	s := mapstore.Open()
	st, err := s.New()
	if err != nil {
		t.Fatalf("новая карта: %v", err)
	}
	return NewHandler(s), st.Manifest
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

// TestRemovedGeometryAddressDoesNotRespond — ресурс geometry убран, а не оставлен
// вторым адресом сети (бида ClearAhead-8kx).
//
// Проверка стоит отдельно и намеренно: два адреса одного тела — ровно та
// двусмысленность, ради снятия которой затевался переезд, и вернуть старый
// адрес «на всякий случай» проще всего молча. Клиента не существует, ломать
// нечего.
func TestRemovedGeometryAddressDoesNotRespond(t *testing.T) {
	h, man := newTestHandler(t)
	for _, p := range []string{
		"/maps/" + man.MapID + "/revisions/1/geometry",
		"/maps/" + man.MapID + "/revisions/2/geometry",
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", p, nil))
		if w.Code != 404 {
			t.Fatalf("%s: код %d, ожидалось 404 — адрес geometry удалён", p, w.Code)
		}
	}
}
