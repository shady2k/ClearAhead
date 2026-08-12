package httpapi

import (
	"bytes"
	"errors"
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

// testRule — правило подробности тестового региона.
//
// Числа те же, что у боевой карты репозитория, но взяты они ЗДЕСЬ, а не из
// пакета chunk: с 2026-08-12 охват — свойство карты, и константы, на которую
// можно сослаться, больше не существует. Регион без правила не записывается
// вовсе (worldstore.PutRegion), поэтому его называет каждая проверка.
var testRule = chunk.Rule{Level0RadiusM: 512, MaxLevel: 4}

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
	if err := s.PutRegion(worldstore.Region{ID: testRegion, Frame: "{}", Epoch: 1, Rule: testRule}); err != nil {
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
//
// БЕЗ СЧЁТЧИКА (nil): здесь проверяется отдача — коды, заголовки, кэш, форма
// адреса, — и она обязана оставаться той же независимо от того, откуда взялись
// байты. Порождение по требованию проверяется отдельно и явно, ручками, которые
// счётчик получают: иначе «204 на пустом месте» перестал бы что-либо значить,
// потому что пустого места у ручки со счётчиком почти не остаётся.
func newChunksTestHandler(t *testing.T) http.Handler {
	t.Helper()
	return NewChunksHandler(newChunksTestStore(t), nil)
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

// TestMissingChunkIsServed204 — пустота при базе как ЕДИНСТВЕННОМ источнике.
//
// Ручка здесь без счётчика: с ним часть этих адресов законно превратилась бы в
// посчитанную землю, и проверка говорила бы уже о другом. Что остаётся пустотой
// при включённом порождении — предмет TestEmptinessBeyondTheWorldStays204.
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
	// Уровня подробности вне [0, MaxLevel] у этого региона не существует: это
	// неверный адрес, а не пустое место, где чанк мог бы появиться. Последний
	// уровень свой у каждого региона (охват приезжает картой), поэтому граница
	// берётся у правила региона, а не у пакета.
	for _, level := range []string{"-1", strconv.Itoa(testRule.MaxLevel + 1), "99"} {
		url := "/regions/" + testRegion + "/chunks/" + level + "/0/0"
		rec := do(t, h, http.MethodGet, url, nil)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("уровень %s: код %d, ожидался 404", level, rec.Code)
		}
	}
	// Границы диапазона при этом обязаны быть адресуемы — иначе проверка выше
	// прошла бы и при сплошном 404.
	for _, level := range []int{0, testRule.MaxLevel} {
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
	h := NewChunksHandler(s, nil)
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

// makerFunc — ChunkMaker из замыкания.
//
// Настоящий счётчик (worldgen.Lazy) сюда не тянется нарочно: он требует карты,
// распространения поз и рельефа, и проверка отдачи начала бы падать от правки
// формата карты. Ручке нужен ровно один вопрос — «посчитай этот адрес», — и
// проверять её надо на том, что этот вопрос задаёт, а не на том, кто на него
// отвечает по-настоящему. Байт в байт совпадение посчитанного с прогретым
// проверяется там, где оно живёт, — в worldgen.
type makerFunc func(a chunk.Address) (worldstore.Chunk, bool, error)

func (f makerFunc) MakeChunk(a chunk.Address) (worldstore.Chunk, bool, error) { return f(a) }

// TestMissingChunkIsComputedAndCached — база стала кэшем.
//
// Промах базы перестал быть ответом: чанк считается, отдаётся и ложится в базу.
// Проверяется всё звено целиком, потому что порознь каждая половина ничего не
// стоит: посчитать и не положить — значит платить счётом на каждом запросе;
// положить и отдать другое — значит развести первый ответ со вторым.
func TestMissingChunkIsComputedAndCached(t *testing.T) {
	s := newChunksTestStore(t)
	// Адрес, которого в базе нет: та самая клетка, на которую до сих пор
	// отвечали 204.
	addr := chunk.Address{Region: testRegion, Level: 0, CX: 17, CZ: -42}
	made := testHeights()
	for k := range made {
		made[k] -= 3 // не те же байты, что у лежащего в базе чанка
	}

	calls := 0
	h := NewChunksHandler(s, makerFunc(func(a chunk.Address) (worldstore.Chunk, bool, error) {
		calls++
		if a != addr {
			return worldstore.Chunk{}, false, nil
		}
		// Ровно то, что делает настоящий счётчик: пишет и отдаёт прочитанное.
		if err := s.PutChunk(worldstore.Chunk{
			Address: a, Revision: 1, BaseZmm: testBaseZmm, Heights: made,
		}); err != nil {
			return worldstore.Chunk{}, false, err
		}
		c, ok, err := s.GetChunk(a)
		return c, ok, err
	}))

	url := chunkURL(testRegion, addr.Level, addr.CX, addr.CZ)
	first := do(t, h, http.MethodGet, url, nil)
	if first.Code != http.StatusOK {
		t.Fatalf("первый запрос: код %d, ожидался 200 — чанк не посчитан", first.Code)
	}
	if got := first.Body.Len(); got != chunk.HeightsBytes {
		t.Fatalf("первый запрос: тело %d байт, ожидалось %d", got, chunk.HeightsBytes)
	}
	// Посчитанный чанк неотличим от лежавшего: те же заголовки, тот же смысл.
	if got := first.Header().Get(HeaderChunkBaseZ); got != strconv.Itoa(testBaseZmm) {
		t.Fatalf("первый запрос: %s = %q", HeaderChunkBaseZ, got)
	}
	etag := first.Header().Get("ETag")
	if etag == "" || etag == `""` {
		t.Fatalf("первый запрос: ETag %q — посчитанный чанк приехал без хеша", etag)
	}
	requireRevalidation(t, first.Header().Get("Cache-Control"))

	// Второй запрос обязан прийти ИЗ БАЗЫ: счётчик больше не зовут.
	second := do(t, h, http.MethodGet, url, nil)
	if second.Code != http.StatusOK {
		t.Fatalf("второй запрос: код %d", second.Code)
	}
	if calls != 1 {
		t.Fatalf("счётчик позван %d раза: посчитанное не легло в базу, и платить придётся каждый раз", calls)
	}
	if !bytes.Equal(second.Body.Bytes(), first.Body.Bytes()) {
		t.Fatal("второй ответ отличается от первого — посчитанное и закэшированное разошлись")
	}
	if got := second.Header().Get("ETag"); got != etag {
		t.Fatalf("второй ответ с другим ETag: %q против %q", got, etag)
	}
}

// TestEmptinessBeyondTheWorldStays204 — граница мира пережила ленивое
// порождение.
//
// Счётчик, сказавший «земли здесь нет ни для кого», обязан дать тот же 204, что
// давала пустая база: за охватом карты мир не появляется от того, что его
// спросили.
func TestEmptinessBeyondTheWorldStays204(t *testing.T) {
	s := newChunksTestStore(t)
	h := NewChunksHandler(s, makerFunc(func(a chunk.Address) (worldstore.Chunk, bool, error) {
		return worldstore.Chunk{}, false, nil
	}))
	rec := do(t, h, http.MethodGet, chunkURL(testRegion, 0, 1_000_000, 0), nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("код %d, ожидался 204", rec.Code)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("204 с телом в %d байт", rec.Body.Len())
	}
}

// TestChunkThatCannotBeComputedIsServed500 — сбой остаётся сбоем.
//
// Отличие от проверки выше — в одном возвращаемом значении счётчика, и в этом
// вся суть: «здесь пусто» и «посчитать не смогли» обязаны быть различимы
// снаружи. Ответь сервер пустотой на сломанный рецепт — клиент нарисовал бы
// базовую поверхность и никогда не переспросил, а поломка выглядела бы краем
// мира.
func TestChunkThatCannotBeComputedIsServed500(t *testing.T) {
	s := newChunksTestStore(t)
	h := NewChunksHandler(s, makerFunc(func(a chunk.Address) (worldstore.Chunk, bool, error) {
		return worldstore.Chunk{}, false, errors.New("рецепт не посчитан")
	}))
	rec := do(t, h, http.MethodGet, chunkURL(testRegion, 0, 5, 5), nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("код %d, ожидался 500", rec.Code)
	}
}

// TestStoredChunkIsNotRecomputed — попадание в базу счётчика не тревожит.
//
// Иначе кэш перестал бы быть кэшем: 2.7 мс счёта на каждый запрос к уже
// лежащему чанку, и тем больше, чем лучше прогрет мир.
func TestStoredChunkIsNotRecomputed(t *testing.T) {
	s := newChunksTestStore(t)
	calls := 0
	h := NewChunksHandler(s, makerFunc(func(a chunk.Address) (worldstore.Chunk, bool, error) {
		calls++
		return worldstore.Chunk{}, false, nil
	}))
	if rec := do(t, h, http.MethodGet, chunkURL(testRegion, 0, 0, 0), nil); rec.Code != http.StatusOK {
		t.Fatalf("код %d", rec.Code)
	}
	if calls != 0 {
		t.Fatalf("лежащий в базе чанк пересчитан %d раз", calls)
	}
}

// TestNothingIsComputedForBadAddress — счётчик не зовут на адрес, который и так
// отвергнут.
//
// Порядок проверок здесь не украшение: неизвестный регион и уровень выше
// объявленного — это 404, и посчитать их значило бы завести землю там, где
// карта её не обещала, да ещё и записать в базу.
func TestNothingIsComputedForBadAddress(t *testing.T) {
	s := newChunksTestStore(t)
	calls := 0
	h := NewChunksHandler(s, makerFunc(func(a chunk.Address) (worldstore.Chunk, bool, error) {
		calls++
		return worldstore.Chunk{}, false, nil
	}))
	for name, url := range map[string]string{
		"несуществующий регион":     chunkURL("нетакого", 0, 0, 0),
		"уровень выше объявленного": chunkURL(testRegion, testRule.MaxLevel+1, 0, 0),
		"кривой путь":               "/regions/" + testRegion + "/chunks/0/0",
	} {
		if rec := do(t, h, http.MethodGet, url, nil); rec.Code != http.StatusNotFound {
			t.Fatalf("%s: код %d, ожидался 404", name, rec.Code)
		}
	}
	if calls != 0 {
		t.Fatalf("на заведомо неверный адрес счётчик позван %d раз", calls)
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
