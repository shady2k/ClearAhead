package httpapi

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/chunk"
	"github.com/shady2k/ClearAhead/server/internal/worldstore"
)

// newWorldsTestHandler поднимает засеянный мир: регион, голова версии 1, сеть
// версии 1 и один чанк уровня 0 — той же семьёй, какой их заводит бутстрап
// (worldstore.Seed), плюс прогрев одной клетки.
func newWorldsTestHandler(t *testing.T) (http.Handler, *worldstore.Store) {
	t.Helper()
	s, err := worldstore.Open(filepath.Join(t.TempDir(), "world.db"))
	if err != nil {
		t.Fatalf("база мира: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	if err := s.Seed(worldstore.Region{ID: testRegion, Frame: "{}", Epoch: 1, Rule: testRule, Domain: testDomain},
		worldstore.ProjectionHead{WorldVersion: 1, SourceJournalSeq: 0, NetworkVersion: 1, RegionRecipeHash: "r1"},
		[]byte(`{"region":"kuban","revision":1}`)); err != nil {
		t.Fatalf("засев: %v", err)
	}
	if err := s.PutChunk(worldstore.Chunk{
		Address:      chunk.Address{Region: testRegion, Level: 0, CX: 0, CZ: 0},
		WorldVersion: 1,
		Revision:     1,
		BaseZmm:      testBaseZmm,
		Heights:      testHeights(),
	}); err != nil {
		t.Fatalf("чанк: %v", err)
	}
	return NewWorldsHandler("M1", testRegion, s), s
}

// TestVersionedNetworkIsImmutable — сеть под версией неизменна по адресу:
// /matches/M1/worlds/1/network отдаёт тело с immutable (бида ClearAhead-5vr,
// ход 2 — «версия В АДРЕСЕ»), и ETag служит 304.
func TestVersionedNetworkIsImmutable(t *testing.T) {
	h, _ := newWorldsTestHandler(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/matches/M1/worlds/1/network", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("код %d, ожидался 200", rec.Code)
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Fatalf("Cache-Control %q: версионный адрес обязан быть immutable", cc)
	}
	if got := rec.Header().Get("ETag"); got == "" || got == `""` {
		t.Fatalf("ETag %q — хеш сети не доехал", got)
	}
	if got := rec.Body.String(); got != `{"region":"kuban","revision":1}` {
		t.Fatalf("тело сети %q, ожидалась засеянная сеть", got)
	}

	// Условный запрос с тем же ETag — 304: immutable не отменяет 304, он лишь
	// разрешает не ходить с ним; кто пришёл — получает дешёвый ответ.
	again := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/matches/M1/worlds/1/network", nil)
	req.Header.Set("If-None-Match", rec.Header().Get("ETag"))
	h.ServeHTTP(again, req)
	if again.Code != http.StatusNotModified {
		t.Fatalf("304: код %d, ожидался 304", again.Code)
	}
}

// TestVersionedPatchDistinguishesEmptyFromMissing — 204 на патч означает
// «чистая база» и отличим от 404: клетка без строки под версией — 204,
// несуществующий уровень, несуществующая версия и чужой матч — 404.
func TestVersionedPatchDistinguishesEmptyFromMissing(t *testing.T) {
	h, _ := newWorldsTestHandler(t)

	// Хранимая клетка — патч с базой высот, immutable.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/matches/M1/worlds/1/chunks/0/0/0/terrain-patch", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("хранимая клетка: код %d, ожидался 200", rec.Code)
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Fatalf("Cache-Control %q: версионный патч обязан быть immutable", cc)
	}
	if got := rec.Header().Get(HeaderChunkBaseZ); got == "" {
		t.Fatal("патч без опорной высоты — клиенту нечего отсчитывать")
	}
	if rec.Body.Len() != chunk.HeightsBytes {
		t.Fatalf("тело %d байт, ожидались высоты чанка %d", rec.Body.Len(), chunk.HeightsBytes)
	}

	// Клетка без единой строки — 204, чистая база.
	empty := httptest.NewRecorder()
	h.ServeHTTP(empty, httptest.NewRequest(http.MethodGet, "/matches/M1/worlds/1/chunks/0/5000/5000/terrain-patch", nil))
	if empty.Code != http.StatusNoContent {
		t.Fatalf("пустая клетка: код %d, ожидался 204", empty.Code)
	}
	if cc := empty.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Fatalf("Cache-Control %q у 204: факт пустоты тоже версиен", cc)
	}

	// Уровень выше последнего — 404, а не 204: патч там не появится никогда.
	badLevel := httptest.NewRecorder()
	h.ServeHTTP(badLevel, httptest.NewRequest(http.MethodGet, "/matches/M1/worlds/1/chunks/9/0/0/terrain-patch", nil))
	if badLevel.Code != http.StatusNotFound {
		t.Fatalf("уровень 9: код %d, ожидался 404", badLevel.Code)
	}

	// Версия за головой проекций — 404: такого мира ещё нет.
	future := httptest.NewRecorder()
	h.ServeHTTP(future, httptest.NewRequest(http.MethodGet, "/matches/M1/worlds/2/chunks/0/0/0/terrain-patch", nil))
	if future.Code != http.StatusNotFound {
		t.Fatalf("версия 2: код %d, ожидался 404", future.Code)
	}

	// Чужой матч — 404: мир принадлежит партии, а не адресу.
	wrongMatch := httptest.NewRecorder()
	h.ServeHTTP(wrongMatch, httptest.NewRequest(http.MethodGet, "/matches/OTHER/worlds/1/network", nil))
	if wrongMatch.Code != http.StatusNotFound {
		t.Fatalf("чужой матч: код %d, ожидался 404", wrongMatch.Code)
	}

	// Нечисловая версия — 404: неверный адрес, а не пустое содержимое.
	badVersion := httptest.NewRecorder()
	h.ServeHTTP(badVersion, httptest.NewRequest(http.MethodGet, "/matches/M1/worlds/abc/network", nil))
	if badVersion.Code != http.StatusNotFound {
		t.Fatalf("версия abc: код %d, ожидался 404", badVersion.Code)
	}
}

// TestUnversionedChunkStaysRevalidating — тот же чанк на неверсионном адресе
// остаётся на обязательной ревалидации: immutable возвращается ТОЛЬКО на
// версионный адрес (бида ClearAhead-5vr, требование брифа).
func TestUnversionedChunkStaysRevalidating(t *testing.T) {
	_, s := newWorldsTestHandler(t)
	h := NewChunksHandler(s, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, chunkURL(testRegion, 0, 0, 0), nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("код %d, ожидался 200", rec.Code)
	}
	requireRevalidation(t, rec.Header().Get("Cache-Control"))
}

// TestVersionedRoutesServeHead — HEAD на версионных адресах обязателен наравне
// с GET (RFC 9110) и несёт те же заголовки, включая ETag для условных запросов.
// Тело для HEAD подавляет net/http сам (тот же разбор, что у чанков в
// chunks_test.go), поэтому проверяются код и заголовки.
func TestVersionedRoutesServeHead(t *testing.T) {
	h, _ := newWorldsTestHandler(t)
	for _, p := range []string{
		"/matches/M1/worlds/1/network",
		"/matches/M1/worlds/1/chunks/0/0/0/terrain-patch",
	} {
		get := httptest.NewRecorder()
		h.ServeHTTP(get, httptest.NewRequest(http.MethodGet, p, nil))
		head := httptest.NewRecorder()
		h.ServeHTTP(head, httptest.NewRequest(http.MethodHead, p, nil))
		if head.Code != get.Code {
			t.Fatalf("%s: HEAD %d, GET %d", p, head.Code, get.Code)
		}
		for _, name := range []string{"ETag", "Cache-Control"} {
			if got, want := head.Header().Get(name), get.Header().Get(name); got != want {
				t.Fatalf("%s: HEAD %s = %q, у GET %q", p, name, got, want)
			}
		}
	}
}
