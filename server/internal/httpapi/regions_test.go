package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/chunk"
	"github.com/shady2k/ClearAhead/server/internal/mapstore"
	"github.com/shady2k/ClearAhead/server/internal/worldstore"
)

// newRegionsTestHandler поднимает корень /regions/ целиком — так, как его
// собирает main.go: из двух хранилищ и трёх подручек.
//
// Регион в базе называется так же, как карта в памяти, и это не удобство теста,
// а действующее соглашение: worldgen.Bootstrap заводит регион строкой
// `region := m.MapID`. Ручка сети сверяет одно с другим, значит и проверка
// обязана играть на том же равенстве.
func newRegionsTestHandler(t *testing.T) (http.Handler, *mapstore.State, *worldstore.Store) {
	t.Helper()
	maps := mapstore.Open()
	st, err := maps.New()
	if err != nil {
		t.Fatalf("новая карта: %v", err)
	}
	world := newChunksTestStore(t)
	if err := world.PutRegion(worldstore.Region{ID: st.Manifest.MapID, Frame: "{}", Epoch: 7}); err != nil {
		t.Fatalf("регион: %v", err)
	}
	h := NewRegionsHandler(
		NewRegionManifestHandler(world, maps),
		NewNetworkHandler(maps),
		NewChunksHandler(world),
	)
	return h, st, world
}

// TestМанифестРегионаНесётЧислаПравилаПодробности — то, ради чего манифест и
// заведён наполовину: клиент обязан САМ выбрать уровень чанка по расстоянию до
// оси (chunk.LevelFor), а числа правила он берёт отсюда. Зашитая в клиент копия
// разошлась бы с сервером молча — не отказом, а сплошными 204 на уровень,
// которого нет.
func TestМанифестРегионаНесётЧислаПравилаПодробности(t *testing.T) {
	h, st, _ := newRegionsTestHandler(t)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/regions/"+st.Manifest.MapID, nil))
	if w.Code != 200 {
		t.Fatalf("код %d, ожидалось 200", w.Code)
	}

	// Строгий разбор: лишнее поле в ответе роняет тест, а не доезжает до клиента
	// незамеченным.
	dec := json.NewDecoder(w.Body)
	dec.DisallowUnknownFields()
	var got regionManifest
	if err := dec.Decode(&got); err != nil {
		t.Fatalf("манифест региона не укладывается в объявленную форму: %v", err)
	}

	if got.Region != st.Manifest.MapID {
		t.Fatalf("регион %q, ожидался %q", got.Region, st.Manifest.MapID)
	}
	if got.Epoch != 7 {
		t.Fatalf("эпоха %d, в базе 7", got.Epoch)
	}
	// Ревизия — то единственное число, без которого нечего подставить в адрес
	// сети.
	if got.Revision != st.Manifest.Revision {
		t.Fatalf("ревизия %d, у карты %d", got.Revision, st.Manifest.Revision)
	}
	if got.TrackHash != st.Manifest.TrackHash || got.NetworkHash != st.Manifest.NetworkHash {
		t.Fatalf("хеши манифеста региона разошлись с манифестом карты: %+v", got)
	}
	want := chunkRule{
		SideM:         chunk.SideM0,
		StepM:         chunk.StepM0,
		Samples:       chunk.Samples,
		Level0RadiusM: chunk.Level0RadiusM,
		MaxLevel:      chunk.MaxLevel,
	}
	if got.Chunks != want {
		t.Fatalf("правило подробности %+v, в пакете chunk %+v", got.Chunks, want)
	}
	// Числа обязаны быть теми же, по которым сервер сам порождает чанки: не
	// «похожими», а буквально из пакета chunk. Отдельно проверяется, что клиент
	// с ними досчитает до того же уровня, что и сервер.
	if lvl, ok := chunk.LevelFor(got.Chunks.Level0RadiusM - 1); !ok || lvl != 0 {
		t.Fatalf("уровень внутри радиуса нулевого уровня: %d, ok=%v", lvl, ok)
	}

	// Ревизию клиент подставляет в адрес сети — и та обязана ответить.
	url := networkURL(got.Region, got.Revision)
	wn := httptest.NewRecorder()
	h.ServeHTTP(wn, httptest.NewRequest("GET", url, nil))
	if wn.Code != 200 {
		t.Fatalf("%s: код %d — манифест назвал ревизию, по которой ничего нет", url, wn.Code)
	}
}

// TestМанифестРегионаНеОтдаётПереченьПокрытия — список имеющихся чанков в
// манифест не входит и входить не должен: он устареет быстрее, чем доедет, как
// только появится режим строителя (ClearAhead-vo0). Проверяется по форме
// ответа: строгий разбор выше уже не пустил бы лишнее поле, но здесь названа
// причина, а не только факт.
func TestМанифестРегионаНеОтдаётПереченьПокрытия(t *testing.T) {
	h, st, _ := newRegionsTestHandler(t)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/regions/"+st.Manifest.MapID, nil))
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatalf("манифест не JSON: %v", err)
	}
	for _, name := range []string{"coverage", "chunk_list", "chunks_available", "tiles"} {
		if _, ok := raw[name]; ok {
			t.Fatalf("в манифесте появилось поле %q — перечень покрытия устаревает по дороге", name)
		}
	}
	// Поле chunks — это ПРАВИЛО, а не перечень: объект с числами.
	var rule chunkRule
	if err := json.Unmarshal(raw["chunks"], &rule); err != nil {
		t.Fatalf("chunks — не правило подробности: %v", err)
	}
}

// TestМанифестРегионаРевалидируется — адрес не называет версию, значит
// immutable был бы ложью ценой в навсегда устаревшую ревизию у того, кто
// манифест уже загрузил.
func TestМанифестРегионаРевалидируется(t *testing.T) {
	h, st, _ := newRegionsTestHandler(t)
	url := "/regions/" + st.Manifest.MapID

	first := do(t, h, http.MethodGet, url, nil)
	if first.Code != 200 {
		t.Fatalf("код %d, ожидалось 200", first.Code)
	}
	requireRevalidation(t, first.Header().Get("Cache-Control"))

	etag := first.Header().Get("ETag")
	if etag == "" || etag == `""` {
		t.Fatalf("ETag %q — манифесту нечего предъявить на условный запрос", etag)
	}
	second := do(t, h, http.MethodGet, url, map[string]string{"If-None-Match": etag})
	if second.Code != http.StatusNotModified || second.Body.Len() != 0 {
		t.Fatalf("условный запрос: код %d, тело %d байт, ожидался 304 без тела",
			second.Code, second.Body.Len())
	}
	stale := do(t, h, http.MethodGet, url, map[string]string{"If-None-Match": `"устарело"`})
	if stale.Code != 200 || stale.Body.Len() == 0 {
		t.Fatalf("устаревший ETag: код %d, тело %d байт", stale.Code, stale.Body.Len())
	}
}

func TestМанифестНесуществующегоРегиона404(t *testing.T) {
	h, _, _ := newRegionsTestHandler(t)
	for _, p := range []string{"/regions/нетакого", "/regions/" + testRegion} {
		// testRegion существует в базе мира, но карты с таким именем в памяти
		// нет: сети у него нет, а без неё манифест не выполняет своей работы.
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", p, nil))
		if w.Code != 404 {
			t.Fatalf("%s: код %d, ожидалось 404", p, w.Code)
		}
	}
}

func TestМанифестРегионаЧужойМетод405(t *testing.T) {
	h, st, _ := newRegionsTestHandler(t)
	rec := do(t, h, http.MethodPost, "/regions/"+st.Manifest.MapID, nil)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("код %d, ожидался 405", rec.Code)
	}
	if got := rec.Header().Get("Allow"); got != "GET, HEAD" {
		t.Fatalf("Allow %q", got)
	}
}

func TestМанифестРегионаHEAD(t *testing.T) {
	h, st, _ := newRegionsTestHandler(t)
	url := "/regions/" + st.Manifest.MapID
	get := do(t, h, http.MethodGet, url, nil)
	head := do(t, h, http.MethodHead, url, nil)
	if head.Code != 200 {
		t.Fatalf("HEAD: код %d, ожидалось 200", head.Code)
	}
	for _, name := range []string{"ETag", "Cache-Control", "Content-Type"} {
		if got, want := head.Header().Get(name), get.Header().Get(name); got != want {
			t.Fatalf("HEAD %s = %q, у GET %q", name, got, want)
		}
	}
}

// TestГеопривязкаРегионаДоезжаетКакЕсть — frame отдаётся без перепаковки, а
// пустой не отдаётся вовсе: пустой объект в ответе клиент был бы обязан
// отличать от заданной привязки с нулями.
func TestГеопривязкаРегионаДоезжаетКакЕсть(t *testing.T) {
	h, st, world := newRegionsTestHandler(t)
	url := "/regions/" + st.Manifest.MapID

	var without map[string]json.RawMessage
	if err := json.Unmarshal(do(t, h, http.MethodGet, url, nil).Body.Bytes(), &without); err != nil {
		t.Fatalf("манифест не JSON: %v", err)
	}
	if _, ok := without["frame"]; ok {
		t.Fatal("frame отдан при пустой геопривязке региона")
	}

	const frame = `{"datum":"WGS84","origin":{"lat":45.03,"lon":38.97,"h":25}}`
	if err := world.PutRegion(worldstore.Region{ID: st.Manifest.MapID, Frame: frame, Epoch: 7}); err != nil {
		t.Fatalf("регион: %v", err)
	}
	var with map[string]json.RawMessage
	rec := do(t, h, http.MethodGet, url, nil)
	if err := json.Unmarshal(rec.Body.Bytes(), &with); err != nil {
		t.Fatalf("манифест не JSON: %v", err)
	}
	got, ok := with["frame"]
	if !ok {
		t.Fatal("геопривязка региона не доехала")
	}
	// Сверяются значения, а не байты: перепаковка пробелов не меняет смысла, а
	// потерянное поле меняет.
	var wantMap, gotMap map[string]any
	if err := json.Unmarshal([]byte(frame), &wantMap); err != nil {
		t.Fatalf("эталон не JSON: %v", err)
	}
	if err := json.Unmarshal(got, &gotMap); err != nil {
		t.Fatalf("frame в ответе не JSON: %v", err)
	}
	if len(gotMap) != len(wantMap) {
		t.Fatalf("frame %s, ожидался %s", got, frame)
	}
}

// TestКореньРегионаРазводитПодресурсы — все три ресурса живут под одним корнем,
// и чужая форма адреса даёт 404 при любом методе.
func TestКореньРегионаРазводитПодресурсы(t *testing.T) {
	h, st, _ := newRegionsTestHandler(t)
	region := st.Manifest.MapID
	rev := strconv.Itoa(st.Manifest.Revision)

	ok := map[string]string{
		"манифест": "/regions/" + region,
		"сеть":     "/regions/" + region + "/revisions/" + rev + "/network",
		"рельеф":   "/regions/" + testRegion + "/chunks/0/0/0",
	}
	for name, p := range ok {
		rec := do(t, h, http.MethodGet, p, nil)
		if rec.Code != 200 {
			t.Fatalf("%s (%s): код %d, ожидалось 200", name, p, rec.Code)
		}
	}

	bad := map[string]string{
		"корень без региона":  "/regions/",
		"пустое имя региона":  "/regions//chunks/0/0/0",
		"неизвестный ресурс":  "/regions/" + region + "/streets",
		"старое имя ресурса":  "/regions/" + region + "/revisions/" + rev + "/geometry",
		"лишний сегмент":      "/regions/" + region + "/revisions/" + rev + "/network/extra",
		"чужой подресурс":     "/regions/" + region + "/chunks/0/0",
		"ревизия без ресурса": "/regions/" + region + "/revisions/" + rev,
	}
	for name, p := range bad {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			rec := do(t, h, method, p, nil)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("%s (%s %s): код %d, ожидалось 404", name, method, p, rec.Code)
			}
		}
	}
}
