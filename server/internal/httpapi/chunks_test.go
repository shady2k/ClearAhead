package httpapi

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/chunk"
	"github.com/shady2k/ClearAhead/server/internal/worldstore"
)

// testRegion — регион, который заводит каждая проверка. Имя одно на весь файл
// затем, чтобы «неверный адрес» отличался от «верного» ровно одним признаком.
const testRegion = "kuban"

// testBaseZmm — опорная высота тестового чанка, целые миллиметры. Значение
// заведомо не круглое и не нулевое: нулевая база прошла бы и при потерянном
// заголовке.
const testBaseZmm = 143_720

// testHeights — отсчёты тестового чанка.
//
// Значения зависят от обоих индексов и уходят в минус: перепутанный порядок
// обхода (i вместо j), потерянный знак и обрезанный старший байт дают разный
// результат, и каждый из них виден.
func testHeights() []int16 {
	h := make([]int16, chunk.Samples*chunk.Samples)
	for j := 0; j < chunk.Samples; j++ {
		for i := 0; i < chunk.Samples; i++ {
			h[chunk.Index(i, j)] = int16(i*37 - j*11 - 500)
		}
	}
	return h
}

// newChunksTestStore поднимает свежую базу мира во временном каталоге: регион
// есть, в нём лежит один чанк уровня 0 в начале координат.
//
// База отдаётся наружу отдельно от ручки затем, что проверять приходится не
// только отдачу, но и изменение: чанк под тем же адресом обязан меняться, и
// без доступа к хранилищу это не разыграть.
func newChunksTestStore(t *testing.T) *worldstore.Store {
	t.Helper()
	s, err := worldstore.Open(filepath.Join(t.TempDir(), "world.db"))
	if err != nil {
		t.Fatalf("база мира: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	if err := s.PutRegion(worldstore.Region{ID: testRegion, Frame: "{}", Epoch: 1}); err != nil {
		t.Fatalf("регион: %v", err)
	}
	if err := s.PutChunk(worldstore.Chunk{
		Address:  chunk.Address{Region: testRegion, Level: 0, CX: 0, CZ: 0},
		Revision: 1,
		BaseZmm:  testBaseZmm,
		Heights:  testHeights(),
	}); err != nil {
		t.Fatalf("чанк: %v", err)
	}
	return s
}

// newChunksTestHandler — ручка над такой базой для проверок, которым само
// хранилище не нужно.
func newChunksTestHandler(t *testing.T) http.Handler {
	t.Helper()
	return NewChunksHandler(newChunksTestStore(t))
}

// requireRevalidation проверяет СМЫСЛ Cache-Control, а не его букву: «клиент
// обязан спросить, прежде чем отдать копию».
//
// Прибивать здесь одну строку гвоздями нельзя, и цена гвоздей известна —
// формулировок обязательной ревалидации несколько («no-cache»;
// «max-age=0, must-revalidate»), они равносильны, и тест, знающий одну, падал
// бы при переходе на другую, не найдя при этом ни одной настоящей поломки.
// Настоящих поломок ровно две, и ловятся они по имени: обещание не спрашивать
// и запрет хранить.
func requireRevalidation(t *testing.T, cc string) {
	t.Helper()
	d := make(map[string]string, 4)
	for _, part := range strings.Split(cc, ",") {
		name, value, _ := strings.Cut(strings.TrimSpace(strings.ToLower(part)), "=")
		if name != "" {
			d[name] = value
		}
	}
	// immutable (RFC 8246) — «год не шли условный запрос». При нём ETag
	// предъявить некому, и изменившийся чанк не доедет до того, кто его уже
	// загрузил: адрес чанка версии не называет и неизменность обещать не вправе.
	if _, ok := d["immutable"]; ok {
		t.Fatalf("Cache-Control %q: immutable отменяет условный запрос, а содержимое под этим адресом меняется", cc)
	}
	// Долгий max-age делает то же самое молча: пока свежесть не истекла, копия
	// отдаётся без вопроса. «Обязан спросить» — это max-age=0 или его отсутствие.
	if age, ok := d["max-age"]; ok && age != "0" {
		t.Fatalf("Cache-Control %q: max-age=%s отдаёт копию без вопроса всё это время", cc, age)
	}
	// no-store — не то же, что обязательная ревалидация, а её противоположность
	// по цене: копию хранить запрещено, значит каждый повтор стоит полного тела
	// вместо 304 в пару сотен байт.
	if _, ok := d["no-store"]; ok {
		t.Fatalf("Cache-Control %q: no-store убивает дешёвый 304, нужна ревалидация, а не запрет кэша", cc)
	}
	if _, ok := d["no-cache"]; ok {
		return
	}
	if _, ok := d["must-revalidate"]; ok {
		if age, hasAge := d["max-age"]; hasAge && age == "0" {
			return
		}
	}
	t.Fatalf("Cache-Control %q: ничто не обязывает клиента ревалидировать", cc)
}

// chunkURL собирает адрес чанка так же, как его соберёт клиент.
func chunkURL(region string, level, cx, cz int) string {
	return "/regions/" + region + "/chunks/" +
		strconv.Itoa(level) + "/" + strconv.Itoa(cx) + "/" + strconv.Itoa(cz)
}

func do(t *testing.T, h http.Handler, method, url string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, url, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestChunkIsServedAsFixedLengthBlob(t *testing.T) {
	h := newChunksTestHandler(t)
	rec := do(t, h, http.MethodGet, chunkURL(testRegion, 0, 0, 0), nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("код %d, ожидался 200", rec.Code)
	}
	// Размер блоба не зависит ни от содержимого, ни от уровня — это то, что
	// делает чанк единицей передачи.
	if got := rec.Body.Len(); got != chunk.HeightsBytes {
		t.Fatalf("тело %d байт, ожидалось %d", got, chunk.HeightsBytes)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/octet-stream" {
		t.Fatalf("Content-Type %q", got)
	}
	if got := rec.Header().Get("Content-Length"); got != strconv.Itoa(chunk.HeightsBytes) {
		t.Fatalf("Content-Length %q, ожидалось %d", got, chunk.HeightsBytes)
	}
	requireRevalidation(t, rec.Header().Get("Cache-Control"))
	if got := rec.Header().Get("ETag"); got == "" || got == `""` {
		t.Fatalf("ETag %q — хеш чанка не доехал", got)
	}
	// Без базы отсчёты не значат ничего, поэтому её отсутствие — отказ теста,
	// а не мелочь.
	if got := rec.Header().Get(HeaderChunkBaseZ); got != strconv.Itoa(testBaseZmm) {
		t.Fatalf("%s = %q, ожидалось %d", HeaderChunkBaseZ, got, testBaseZmm)
	}
}

func TestChunkBodyDecodesToStoredSamples(t *testing.T) {
	h := newChunksTestHandler(t)
	rec := do(t, h, http.MethodGet, chunkURL(testRegion, 0, 0, 0), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("код %d, ожидался 200", rec.Code)
	}

	got, err := chunk.DecodeHeights(rec.Body.Bytes())
	if err != nil {
		t.Fatalf("разбор блоба: %v", err)
	}
	want := testHeights()
	for k := range want {
		if got[k] != want[k] {
			t.Fatalf("отсчёт %d: %d, записано %d", k, got[k], want[k])
		}
	}
}

func TestMissingChunkIsServed204(t *testing.T) {
	h := newChunksTestHandler(t)
	// Регион существует, чанка в нём нет: разреженность — свойство хранилища,
	// а не сбой, и 404 здесь был бы неотличим от опечатки в имени региона.
	for _, url := range []string{
		chunkURL(testRegion, 0, 17, -42),      // пустота внутри региона
		chunkURL(testRegion, 3, 0, 0),         // другой уровень того же места
		chunkURL(testRegion, 0, 1_000_000, 0), // «за краем мира»
	} {
		rec := do(t, h, http.MethodGet, url, nil)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("%s: код %d, ожидался 204", url, rec.Code)
		}
		if rec.Body.Len() != 0 {
			t.Fatalf("%s: 204 с телом в %d байт", url, rec.Body.Len())
		}
	}
}

func TestBadAddressIsServed404(t *testing.T) {
	h := newChunksTestHandler(t)
	cases := map[string]string{
		"несуществующий регион": chunkURL("нетакого", 0, 0, 0),
		"пустое имя региона":    "/regions//chunks/0/0/0",
		"нечисловой cx":         "/regions/" + testRegion + "/chunks/0/восток/0",
		"дробный cz":            "/regions/" + testRegion + "/chunks/0/0/1.5",
		"переполнение cx":       "/regions/" + testRegion + "/chunks/0/99999999999999999999/0",
		"лишний сегмент":        chunkURL(testRegion, 0, 0, 0) + "/heights",
		"нехватка сегментов":    "/regions/" + testRegion + "/chunks/0/0",
		"чужой префикс":         "/regionz/" + testRegion + "/chunks/0/0/0",
	}
	for name, url := range cases {
		rec := do(t, h, http.MethodGet, url, nil)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s (%s): код %d, ожидался 404", name, url, rec.Code)
		}
	}
}

func TestLevelOutOfRangeIsServed404(t *testing.T) {
	h := newChunksTestHandler(t)
	// Уровня подробности вне [0, MaxLevel] не существует ни у одного региона:
	// это неверный адрес, а не пустое место, где чанк мог бы появиться.
	for _, level := range []string{"-1", strconv.Itoa(chunk.MaxLevel + 1), "99"} {
		url := "/regions/" + testRegion + "/chunks/" + level + "/0/0"
		rec := do(t, h, http.MethodGet, url, nil)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("уровень %s: код %d, ожидался 404", level, rec.Code)
		}
	}
	// Границы диапазона при этом обязаны быть адресуемы — иначе проверка выше
	// прошла бы и при сплошном 404.
	for _, level := range []int{0, chunk.MaxLevel} {
		rec := do(t, h, http.MethodGet, chunkURL(testRegion, level, 0, 0), nil)
		if rec.Code == http.StatusNotFound {
			t.Fatalf("уровень %d: 404, а он в диапазоне", level)
		}
	}
}

func TestRepeatedRequestWithIfNoneMatchIsServed304(t *testing.T) {
	h := newChunksTestHandler(t)
	url := chunkURL(testRegion, 0, 0, 0)

	first := do(t, h, http.MethodGet, url, nil)
	etag := first.Header().Get("ETag")
	if first.Code != http.StatusOK || etag == "" {
		t.Fatalf("первый запрос: код %d, ETag %q", first.Code, etag)
	}

	second := do(t, h, http.MethodGet, url, map[string]string{"If-None-Match": etag})
	if second.Code != http.StatusNotModified {
		t.Fatalf("код %d, ожидался 304", second.Code)
	}
	if second.Body.Len() != 0 {
		t.Fatalf("304 с телом в %d байт", second.Body.Len())
	}
	if got := second.Header().Get("ETag"); got != etag {
		t.Fatalf("304 без своего ETag: %q", got)
	}

	// Чужой ETag ревалидацию не проходит: тело обязано приехать.
	stale := do(t, h, http.MethodGet, url, map[string]string{"If-None-Match": `"устарело"`})
	if stale.Code != http.StatusOK || stale.Body.Len() != chunk.HeightsBytes {
		t.Fatalf("устаревший ETag: код %d, тело %d байт", stale.Code, stale.Body.Len())
	}
}

// TestChangedChunkReachesClientThatCachedIt — то, ради чего снят
// immutable, разыграно целиком: клиент загрузил чанк, чанк изменился, клиент
// пришёл со своим ETag.
//
// Проверка стоит рядом с заголовком не случайно: заголовок один тут ничего не
// доказывает — при immutable этот второй запрос не случился бы вовсе, и у
// игрока навсегда остался бы прежний холм на месте проложенного пути. Отказа
// при этом не видно ниоткуда, поэтому ловить такое обязан тест.
func TestChangedChunkReachesClientThatCachedIt(t *testing.T) {
	s := newChunksTestStore(t)
	h := NewChunksHandler(s)
	url := chunkURL(testRegion, 0, 0, 0)

	first := do(t, h, http.MethodGet, url, nil)
	etag := first.Header().Get("ETag")
	if first.Code != http.StatusOK || etag == "" {
		t.Fatalf("первый запрос: код %d, ETag %q", first.Code, etag)
	}
	requireRevalidation(t, first.Header().Get("Cache-Control"))

	// Земляные работы под новым путём: тот же адрес (region, level, cx, cz),
	// другие высоты. Адрес версии не называет, значит новое состояние от
	// прежнего отличает только ETag — и больше ничто.
	dug := testHeights()
	for k := range dug {
		dug[k] += 250
	}
	if err := s.PutChunk(worldstore.Chunk{
		Address:  chunk.Address{Region: testRegion, Level: 0, CX: 0, CZ: 0},
		Revision: 2,
		BaseZmm:  testBaseZmm,
		Heights:  dug,
	}); err != nil {
		t.Fatalf("перезапись чанка: %v", err)
	}

	second := do(t, h, http.MethodGet, url, map[string]string{"If-None-Match": etag})
	if second.Code != http.StatusOK {
		t.Fatalf("код %d, ожидался 200: клиент остался бы с прежним рельефом", second.Code)
	}
	if got := second.Header().Get("ETag"); got == etag {
		t.Fatalf("содержимое изменилось, а ETag прежний: %q", got)
	}
	got, err := chunk.DecodeHeights(second.Body.Bytes())
	if err != nil {
		t.Fatalf("разбор блоба: %v", err)
	}
	for k := range dug {
		if got[k] != dug[k] {
			t.Fatalf("отсчёт %d: %d, записано %d — приехал старый рельеф", k, got[k], dug[k])
		}
	}

	// А новый ETag снова даёт дешёвый 304: ревалидация — это «спроси», а не
	// «качай заново», и без этой половины снятие immutable было бы разменом
	// устаревших данных на лишний трафик.
	third := do(t, h, http.MethodGet, url, map[string]string{"If-None-Match": second.Header().Get("ETag")})
	if third.Code != http.StatusNotModified || third.Body.Len() != 0 {
		t.Fatalf("новый ETag: код %d, тело %d байт, ожидался 304 без тела", third.Code, third.Body.Len())
	}
}

func TestForeignMethodIsServed405(t *testing.T) {
	h := newChunksTestHandler(t)
	rec := do(t, h, http.MethodPost, chunkURL(testRegion, 0, 0, 0), nil)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("код %d, ожидался 405", rec.Code)
	}
	if got := rec.Header().Get("Allow"); got != "GET, HEAD" {
		t.Fatalf("Allow %q", got)
	}
}

func TestHEADServesHeadersWithoutBody(t *testing.T) {
	h := newChunksTestHandler(t)
	url := chunkURL(testRegion, 0, 0, 0)

	get := do(t, h, http.MethodGet, url, nil)
	head := do(t, h, http.MethodHead, url, nil)

	if head.Code != http.StatusOK {
		t.Fatalf("код %d, ожидался 200", head.Code)
	}
	// httptest.ResponseRecorder тело не подавляет — подавляет его сервер, — но
	// ручка обязана дать HEAD те же заголовки, иначе проверка кэша по HEAD
	// врёт.
	for _, name := range []string{"ETag", "Cache-Control", "Content-Type", "Content-Length", HeaderChunkBaseZ} {
		if got, want := head.Header().Get(name), get.Header().Get(name); got != want {
			t.Fatalf("HEAD %s = %q, у GET %q", name, got, want)
		}
	}
}
