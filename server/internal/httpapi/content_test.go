package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/content"
)

// Набор берётся БОЕВОЙ, а не синтетический: ручка проверяется на том самом
// перечне, который уедет игроку, и заодно на том, что двадцатимегабайтный блоб
// отдаётся кусками.
func shippedSet(t *testing.T) *content.Set {
	t.Helper()
	set, err := content.Load(filepath.Join("..", "..", "assets"))
	if err != nil {
		t.Fatalf("боевой набор: %v", err)
	}
	return set
}

func TestContentServesCatalog(t *testing.T) {
	set := shippedSet(t)
	rec := httptest.NewRecorder()
	NewContentHandler(set).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/content", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("код %d", rec.Code)
	}
	// no-cache, а не immutable: адрес называет РЕСУРС, а не его состояние, и
	// immutable здесь был бы дословным повтором ClearAhead-5vr.
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "no-cache") {
		t.Fatalf("Cache-Control %q", cc)
	}
	var got struct {
		Hash  string `json:"hash"`
		Stock []struct {
			ID         string  `json:"id"`
			LengthM    float64 `json:"length"`
			Appearance string  `json:"appearance"`
		} `json:"stock"`
		Assets []map[string]any `json:"assets"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if got.Hash != set.Hash {
		t.Fatalf("хеш перечня в теле %q, у набора %q", got.Hash, set.Hash)
	}
	if len(got.Stock) == 0 || got.Stock[0].LengthM <= 0 {
		t.Fatalf("паспорта не доехали: %+v", got.Stock)
	}
	a := got.Assets[0]
	// Подробности УКЛАДКИ на провод не едут: клиенту нужен адрес байтов и
	// постановка, а как сервер их добыл — не его дело.
	for _, hidden := range []string{"file", "source_hash", "drop_nodes"} {
		if _, ok := a[hidden]; ok {
			t.Fatalf("на проводе поле %q: это подробность укладки", hidden)
		}
	}
	for _, need := range []string{"hash", "size", "anchor", "scale", "translation", "attribution"} {
		if _, ok := a[need]; !ok {
			t.Fatalf("на проводе нет поля %q", need)
		}
	}
	at := a["attribution"].(map[string]any)
	if at["license"] == "" || at["author"] == "" {
		t.Fatal("атрибуция уехала неполной: раздача ассета есть его распространение")
	}
}

// TestContentRevalidates — у перечня ETag, и повторный запрос с ним получает
// 304. Это не украшение: перечень перезапрашивается на каждом входе клиента.
func TestContentRevalidates(t *testing.T) {
	set := shippedSet(t)
	h := NewContentHandler(set)
	first := httptest.NewRecorder()
	h.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/content", nil))
	etag := first.Header().Get("ETag")
	if etag == "" {
		t.Fatal("ETag не выставлен: клиенту нечем перепроверять")
	}
	req := httptest.NewRequest(http.MethodGet, "/content", nil)
	req.Header.Set("If-None-Match", etag)
	second := httptest.NewRecorder()
	h.ServeHTTP(second, req)
	if second.Code != http.StatusNotModified {
		t.Fatalf("код %d, ожидался 304", second.Code)
	}
	if second.Body.Len() != 0 {
		t.Fatalf("на 304 приехало тело в %d байт", second.Body.Len())
	}
}

func TestBlobServesBytes(t *testing.T) {
	set := shippedSet(t)
	asset := set.Assets[0]
	rec := httptest.NewRecorder()
	NewBlobHandler(set).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/"+asset.Hash, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("код %d", rec.Code)
	}
	if got := rec.Body.Len(); got != asset.Size {
		t.Fatalf("байт %d, объявлено %d", got, asset.Size)
	}
	// Immutable здесь ЧЕСТЕН по построению: изменившиеся байты имеют другой
	// адрес. Это единственный ресурс проекта, у которого так.
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Fatalf("Cache-Control %q", cc)
	}
	if ct := rec.Header().Get("Content-Type"); ct != asset.MediaType {
		t.Fatalf("Content-Type %q, объявлено %q", ct, asset.MediaType)
	}
	if ar := rec.Header().Get("Accept-Ranges"); ar != "bytes" {
		t.Fatalf("Accept-Ranges %q: оборванная загрузка двадцати мегабайт "+
			"без возобновления начинается с нуля", ar)
	}
}

// TestBlobServesRange — возобновляемая загрузка работает, и проверяется она
// байтами, а не заголовком.
func TestBlobServesRange(t *testing.T) {
	set := shippedSet(t)
	asset := set.Assets[0]
	full, _ := set.Blob(asset.Hash)
	req := httptest.NewRequest(http.MethodGet, "/assets/"+asset.Hash, nil)
	req.Header.Set("Range", "bytes=10-19")
	rec := httptest.NewRecorder()
	NewBlobHandler(set).ServeHTTP(rec, req)
	if rec.Code != http.StatusPartialContent {
		t.Fatalf("код %d, ожидался 206", rec.Code)
	}
	if got, want := rec.Body.String(), string(full[10:20]); got != want {
		t.Fatalf("кусок %q, ожидался %q", got, want)
	}
}

// TestBlobUnknownHashIsNotFound — 404, а не 204. У чанка пустота законна
// («здесь ничего нет»), потому что он адресуется МЕСТОМ; здесь адрес — само
// содержимое, и «нет такого» означает выдуманный адрес.
func TestBlobUnknownHashIsNotFound(t *testing.T) {
	rec := httptest.NewRecorder()
	NewBlobHandler(shippedSet(t)).ServeHTTP(rec,
		httptest.NewRequest(http.MethodGet, "/assets/sha256-"+strings.Repeat("0", 64), nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("код %d, ожидался 404", rec.Code)
	}
}

func TestBlobRefusesWrite(t *testing.T) {
	set := shippedSet(t)
	rec := httptest.NewRecorder()
	NewBlobHandler(set).ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/assets/"+set.Assets[0].Hash, nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("код %d, ожидался 405", rec.Code)
	}
}
