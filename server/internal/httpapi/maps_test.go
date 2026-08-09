package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

// TestMapsListEmpty — пустой старт: список пуст, а не ошибка.
func TestMapsListEmpty(t *testing.T) {
	s, err := mapstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandler(s)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/maps", nil))
	if w.Code != 200 {
		t.Fatalf("код %d, ожидалось 200", w.Code)
	}
	if got := bytes.TrimSpace(w.Body.Bytes()); !bytes.Equal(got, []byte("[]")) {
		t.Fatalf("тело %q, ожидался пустой список", got)
	}
}

// TestMapsNew — «новая карта» возвращает карту, проходящую Validate и Compile,
// и её манифест.
func TestMapsNew(t *testing.T) {
	s, err := mapstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
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
	s, _ := mapstore.Open(t.TempDir())
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

// TestMapsSaveAsLoadRoundTrip — сохранить как → список → загрузить: карта
// доезжает до диска и возвращается загрузкой.
func TestMapsSaveAsLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s, err := mapstore.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandler(s)
	m, _ := decodeMapResult(t, postJSON(h, "/maps/new", nil))

	sw := postJSON(h, "/maps/save-as/new_map.json", m)
	if sw.Code != 200 {
		t.Fatalf("сохранить как: код %d, ожидалось 200", sw.Code)
	}
	var man track.Manifest
	if err := json.Unmarshal(sw.Body.Bytes(), &man); err != nil {
		t.Fatalf("ответ сохранения не манифест: %v", err)
	}

	lw := httptest.NewRecorder()
	h.ServeHTTP(lw, httptest.NewRequest("GET", "/maps", nil))
	if lw.Code != 200 {
		t.Fatalf("список: код %d", lw.Code)
	}
	var infos []mapstore.MapInfo
	if err := json.Unmarshal(lw.Body.Bytes(), &infos); err != nil {
		t.Fatal(err)
	}
	if len(infos) != 1 || infos[0].Name != "new_map.json" || infos[0].MapID != "NEW" || infos[0].Revision != 1 {
		t.Fatalf("список %+v, ожидалась new_map.json/NEW/1", infos)
	}

	l := postJSON(h, "/maps/load/new_map.json", nil)
	if l.Code != 200 {
		t.Fatalf("загрузить: код %d, ожидалось 200", l.Code)
	}
	got, gotMan := decodeMapResult(t, l)
	if gotMan != man {
		t.Fatalf("манифест загрузки %+v, ожидался %+v", gotMan, man)
	}
	if got.MapID != m.MapID {
		t.Fatalf("загруженная карта %q, ожидалась %q", got.MapID, m.MapID)
	}
}

// TestMapsSaveAsRejectsTraversal — имя «..» не проходит барьер и не создаёт
// файлов за пределами каталога.
func TestMapsSaveAsRejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	s, err := mapstore.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandler(s)
	m, _ := decodeMapResult(t, postJSON(h, "/maps/new", nil))
	w := postJSON(h, "/maps/save-as/..", m)
	if w.Code != 400 {
		t.Fatalf("save-as .. : код %d, ожидался 400", w.Code)
	}
	// Внутри каталога после отказа пусто.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("после отказа в каталоге %d записей", len(entries))
	}
}

// TestMapsSaveInvalid — невалидная карта через провод тоже не попадает на
// диск: отказ сохранения — отказ, а не предупреждение.
func TestMapsSaveInvalid(t *testing.T) {
	dir := t.TempDir()
	s, err := mapstore.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandler(s)
	m, _ := decodeMapResult(t, postJSON(h, "/maps/new", nil))
	m.Anchors = map[string]mapfmt.Anchor{} // без якорей валидатор отвергает
	w := postJSON(h, "/maps/save-as/bad.json", m)
	if w.Code != 400 {
		t.Fatalf("save-as невалидной карты: код %d, ожидался 400", w.Code)
	}
	if _, err := os.Stat(filepath.Join(dir, "bad.json")); !os.IsNotExist(err) {
		t.Fatalf("невалидная карта попала на диск: %v", err)
	}
}

// TestMapsSaveWithoutName — «сохранить» без имени карты — 400: безымянную
// карту сохранить можно только через «сохранить как».
func TestMapsSaveWithoutName(t *testing.T) {
	s, err := mapstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandler(s)
	m, _ := decodeMapResult(t, postJSON(h, "/maps/new", nil))
	w := postJSON(h, "/maps/save", m)
	if w.Code != 400 {
		t.Fatalf("save без имени: код %d, ожидался 400", w.Code)
	}
}

// TestMapsSaveUnderCurrentName — после «сохранить как» карта сохраняется под
// своим именем обычным «сохранить».
func TestMapsSaveUnderCurrentName(t *testing.T) {
	dir := t.TempDir()
	s, err := mapstore.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandler(s)
	m, _ := decodeMapResult(t, postJSON(h, "/maps/new", nil))
	if w := postJSON(h, "/maps/save-as/named.json", m); w.Code != 200 {
		t.Fatalf("save-as: код %d", w.Code)
	}
	if w := postJSON(h, "/maps/save", m); w.Code != 200 {
		t.Fatalf("save: код %d, ожидался 200", w.Code)
	}
	// Файл один — второй save перезаписал, а не создал копию.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("после двух сохранений в каталоге %d записей", len(entries))
	}
}

// TestMapsLoadMissing — загрузки нет — 404, а не 400.
func TestMapsLoadMissing(t *testing.T) {
	s, err := mapstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandler(s)
	w := postJSON(h, "/maps/load/nope.json", nil)
	if w.Code != 404 {
		t.Fatalf("load отсутствующей карты: код %d, ожидался 404", w.Code)
	}
}

// TestManifestEmptyStart — пустой старт: манифест отвечает осмысленным 404, а
// не паникой.
func TestManifestEmptyStart(t *testing.T) {
	s, err := mapstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandler(s)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/manifest", nil))
	if w.Code != 404 {
		t.Fatalf("манифест пустого старта: код %d, ожидался 404", w.Code)
	}
}

// TestGeometryEmptyStart — пустой старт: геометрия отвечает 404, а не паникой.
func TestGeometryEmptyStart(t *testing.T) {
	s, err := mapstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandler(s)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/maps/ST_A/revisions/1/geometry", nil))
	if w.Code != 404 {
		t.Fatalf("геометрия пустого старта: код %d, ожидался 404", w.Code)
	}
}
