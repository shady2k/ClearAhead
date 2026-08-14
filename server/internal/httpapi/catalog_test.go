package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/mapstore"
	"github.com/shady2k/ClearAhead/server/internal/worldstore"
)

// newEmptyWorld — хранилище БЕЗ регионов.
//
// Отдельно от newChunksTestStore нарочно: тот засевает регион и чанк, а каталог
// проверяется в том числе на пустоте, и «пустой» с готовым регионом внутри —
// это не пустой.
func newEmptyWorld(t *testing.T) *worldstore.Store {
	t.Helper()
	s, err := worldstore.Open(filepath.Join(t.TempDir(), "world.db"))
	if err != nil {
		t.Fatalf("база мира: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// decodeCatalog разбирает ответ каталога СТРОГО: лишнее поле роняет тест, а не
// доезжает до клиента незамеченным.
func decodeCatalog(t *testing.T, w *httptest.ResponseRecorder) regionCatalog {
	t.Helper()
	dec := json.NewDecoder(w.Body)
	dec.DisallowUnknownFields()
	var got regionCatalog
	if err := dec.Decode(&got); err != nil {
		t.Fatalf("разбор каталога: %v", err)
	}
	return got
}

// Каталог называет то, во что можно войти, — иначе игрок обязан знать имя
// региона наизусть, а клиент держать литерал карты.
func TestRegionCatalogNamesPlayableRegion(t *testing.T) {
	maps := mapstore.Open()
	st, err := maps.New()
	if err != nil {
		t.Fatalf("новая карта: %v", err)
	}
	world := newEmptyWorld(t)
	if err := world.PutRegion(worldstore.Region{ID: st.Manifest.MapID, Frame: "{}", Epoch: 7, Rule: testRule, Domain: testDomain}); err != nil {
		t.Fatalf("регион: %v", err)
	}
	h := NewRegionCatalogHandler(world, maps)

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/regions", nil))
	if w.Code != 200 {
		t.Fatalf("код %d, ожидалось 200", w.Code)
	}
	got := decodeCatalog(t, w)
	if len(got.Regions) != 1 {
		t.Fatalf("регионов %d, ожидался 1", len(got.Regions))
	}
	c := got.Regions[0]
	if c.Region != st.Manifest.MapID {
		t.Errorf("регион %q, ожидался %q", c.Region, st.Manifest.MapID)
	}
	if c.Epoch != 7 {
		t.Errorf("эпоха %d, ожидалась 7", c.Epoch)
	}
	if c.Revision != st.Manifest.Revision {
		t.Errorf("ревизия %d, ожидалась %d", c.Revision, st.Manifest.Revision)
	}
	if !c.Playable {
		t.Error("регион с сетью в памяти обязан быть играбельным")
	}
}

// Регион БЕЗ сети в памяти назван неиграбельным, а не спрятан и не выдан за
// исправный.
//
// Это не украшение: манифест такого региона отвечает 404, то есть снаружи он
// неотличим от опечатки в имени. Каталог — единственное место, где разница ещё
// видна, и невозможность войти обязана быть на экране ДО нажатия кнопки, а не
// отказом после него.
func TestRegionCatalogMarksRegionWithoutNetworkUnplayable(t *testing.T) {
	maps := mapstore.Open()
	world := newEmptyWorld(t)
	if err := world.PutRegion(worldstore.Region{ID: "ST_ORPHAN", Frame: "{}", Epoch: 3, Rule: testRule, Domain: testDomain}); err != nil {
		t.Fatalf("регион: %v", err)
	}
	h := NewRegionCatalogHandler(world, maps)

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/regions", nil))
	if w.Code != 200 {
		t.Fatalf("код %d, ожидалось 200", w.Code)
	}
	got := decodeCatalog(t, w)
	if len(got.Regions) != 1 {
		t.Fatalf("регионов %d, ожидался 1", len(got.Regions))
	}
	if got.Regions[0].Playable {
		t.Error("регион без сети объявлен играбельным — игрок упрётся в 404 манифеста")
	}
	if got.Regions[0].Revision != 0 {
		t.Errorf("ревизия %d у региона без карты — подставлена чужая", got.Regions[0].Revision)
	}
}

// Пустой каталог — 200 с пустым списком, а не 404: сервер без регионов
// существует и отвечает исправно, и «регионов нет» клиент показывает словами.
func TestEmptyRegionCatalogIsNotAnError(t *testing.T) {
	h := NewRegionCatalogHandler(newEmptyWorld(t), mapstore.Open())
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/regions", nil))
	if w.Code != 200 {
		t.Fatalf("код %d, ожидалось 200", w.Code)
	}
	if len(decodeCatalog(t, w).Regions) != 0 {
		t.Error("на пустом хранилище каталог не пуст")
	}
}

// Порядок задан, а не оставлен движку: регионы, меняющиеся местами от запроса к
// запросу, на экране выбора выглядели бы изменившимся миром.
func TestRegionCatalogIsOrderedByID(t *testing.T) {
	world := newEmptyWorld(t)
	for _, id := range []string{"ST_C", "ST_A", "ST_B"} {
		if err := world.PutRegion(worldstore.Region{ID: id, Frame: "{}", Epoch: 1, Rule: testRule, Domain: testDomain}); err != nil {
			t.Fatalf("регион %s: %v", id, err)
		}
	}
	h := NewRegionCatalogHandler(world, mapstore.Open())
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/regions", nil))
	got := decodeCatalog(t, w)
	want := []string{"ST_A", "ST_B", "ST_C"}
	for i, id := range want {
		if got.Regions[i].Region != id {
			t.Fatalf("на месте %d регион %q, ожидался %q", i, got.Regions[i].Region, id)
		}
	}
}

// Чужой метод — 405 с Allow, а не молчаливое выполнение GET.
func TestRegionCatalogRefusesOtherMethods(t *testing.T) {
	h := NewRegionCatalogHandler(newEmptyWorld(t), mapstore.Open())
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/regions", nil))
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("код %d, ожидалось 405", w.Code)
	}
	if w.Header().Get("Allow") == "" {
		t.Error("405 без заголовка Allow не говорит, что можно")
	}
}
