package httpapi

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/chunk"
	"github.com/shady2k/ClearAhead/server/internal/worldstore"
)

// Замеры отдачи чанка. Меряется полный путь ручки через httptest: разбор
// адреса, проверка региона, чтение из базы, кодирование блоба, заголовки.
// Клиента и сети здесь нет — это нижняя граница, а не пропускная способность
// сервера.

func benchStore(tb testing.TB) *worldstore.Store {
	tb.Helper()
	s, err := worldstore.Open(filepath.Join(tb.TempDir(), "world.db"))
	if err != nil {
		tb.Fatalf("база мира: %v", err)
	}
	tb.Cleanup(func() { s.Close() })
	if err := s.PutRegion(worldstore.Region{ID: testRegion, Frame: "{}", Epoch: 1, Rule: testRule}); err != nil {
		tb.Fatalf("регион: %v", err)
	}
	if err := s.PutChunk(worldstore.Chunk{
		Address:  chunk.Address{Region: testRegion, Level: 0, CX: 0, CZ: 0},
		Revision: 1,
		BaseZmm:  testBaseZmm,
		Heights:  testHeights(),
	}); err != nil {
		tb.Fatalf("чанк: %v", err)
	}
	return s
}

func BenchmarkServeChunk(b *testing.B) {
	s := benchStore(b)
	h := NewChunksHandler(s, nil)

	// ETag берётся из самой ручки, а не собирается здесь: 304 обязан отвечать
	// на тот хеш, который сервер и выдал.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/regions/"+testRegion+"/chunks/0/0/0", nil))
	if rec.Code != http.StatusOK {
		b.Fatalf("подготовка: код %d", rec.Code)
	}
	etag := rec.Header().Get("ETag")
	body := rec.Body.Len()
	if body != chunk.HeightsBytes {
		b.Fatalf("подготовка: тело %d байт, ожидалось %d", body, chunk.HeightsBytes)
	}

	cases := []struct {
		name   string
		path   string
		header string
		code   int
	}{
		{"200", "/regions/" + testRegion + "/chunks/0/0/0", "", http.StatusOK},
		{"304", "/regions/" + testRegion + "/chunks/0/0/0", etag, http.StatusNotModified},
		{"204_пустота", "/regions/" + testRegion + "/chunks/0/999/999", "", http.StatusNoContent},
		{"404_нет_региона", "/regions/неттакого/chunks/0/0/0", "", http.StatusNotFound},
		{"404_кривой_путь", "/regions/" + testRegion + "/chunks/0/0", "", http.StatusNotFound},
	}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			req := httptest.NewRequest(http.MethodGet, c.path, nil)
			if c.header != "" {
				req.Header.Set("If-None-Match", c.header)
			}
			b.ReportAllocs()
			for b.Loop() {
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, req)
				if rec.Code != c.code {
					b.Fatalf("код %d, ожидался %d", rec.Code, c.code)
				}
			}
		})
	}
}

// BenchmarkServeChunkOverNetwork — то же самое, но через настоящий сокет
// httptest.Server: к цене ручки прибавляется цена HTTP-стека и петли.
func BenchmarkServeChunkOverNetwork(b *testing.B) {
	s := benchStore(b)
	srv := httptest.NewServer(NewChunksHandler(s, nil))
	defer srv.Close()
	cl := srv.Client()

	resp, err := cl.Get(srv.URL + "/regions/" + testRegion + "/chunks/0/0/0")
	if err != nil {
		b.Fatal(err)
	}
	etag := resp.Header.Get("ETag")
	resp.Body.Close()

	for _, c := range []struct {
		name   string
		header string
		code   int
	}{
		{"200", "", http.StatusOK},
		{"304", etag, http.StatusNotModified},
	} {
		b.Run(c.name, func(b *testing.B) {
			b.ReportAllocs()
			var bytes int64
			for b.Loop() {
				req, err := http.NewRequest(http.MethodGet, srv.URL+"/regions/"+testRegion+"/chunks/0/0/0", nil)
				if err != nil {
					b.Fatal(err)
				}
				if c.header != "" {
					req.Header.Set("If-None-Match", c.header)
				}
				resp, err := cl.Do(req)
				if err != nil {
					b.Fatal(err)
				}
				n, err := resp.Body.Read(make([]byte, chunk.HeightsBytes))
				if err != nil && err.Error() != "EOF" {
					b.Fatal(err)
				}
				bytes += int64(n)
				resp.Body.Close()
				if resp.StatusCode != c.code {
					b.Fatalf("код %d, ожидался %d", resp.StatusCode, c.code)
				}
			}
			b.SetBytes(int64(chunk.HeightsBytes))
		})
	}
}
