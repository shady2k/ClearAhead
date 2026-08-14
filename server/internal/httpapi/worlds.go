package httpapi

// worldsAPI — ВЕРСИОННЫЕ адреса мира (спека §6.3, sqym.5):
//
//	GET /matches/{m}/worlds/{v}/network
//	GET /matches/{m}/worlds/{v}/chunks/{level}/{cx}/{cz}/terrain-patch
//
// Адрес с версией НЕИЗМЕНЯЕМ: строка хранилища ключуется версией, и тело,
// однажды отданное под версией v, не меняется никогда. Только на таком адресе
// Cache-Control: immutable перестаёт лгать (бида ClearAhead-5vr): неверсионный
// адрес чанков остаётся на no-cache (см. chunksAPI).
//
// 204 на terrain-patch означает «для этой версии берётся чистая база»: клетка
// не несёт ни одной строки не старше v, то есть земляных работ в ней нет ни в
// одной публикации до v включительно. 404 отличается нарочно: несуществующий
// уровень, несуществующая версия или чужой матч — это неверный адрес, а не
// пустое содержимое.
//
// Версионный адрес НЕ порождает по требованию: он отдаёт замороженную
// публикацию, а источники старых версий не хранятся. Порождение живёт на
// неверсионном адресе (chunksAPI), который отдаёт ТЕКУЩИЙ мир.
//
// Зачем в адресе матч: мир принадлежит партии, а не региону — клиент входит
// в партию и спрашивает мир той версии, которую назвала голова. Партий пока
// одна (matchID), и матч->регион заводит main.go строкой композиции; каталог
// партий появится вместе со второй партией.

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/shady2k/ClearAhead/server/internal/chunk"
	"github.com/shady2k/ClearAhead/server/internal/worldstore"
)

// NewWorldsHandler собирает ручку версионных адресов мира.
//
// matchID и region приходят из main.go: сервер знает одну партию и один
// засеянный регион, и откуда взять их — знает композиция, а не ручка.
type worldsAPI struct {
	matchID string
	region  string
	store   *worldstore.Store
}

func NewWorldsHandler(matchID, region string, store *worldstore.Store) http.Handler {
	return &worldsAPI{matchID: matchID, region: region, store: store}
}

func (a *worldsAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Разбор пути идёт первым: путь приходит снаружи и недоверен, и
	// синтаксически чужой адрес — «нет такого ресурса» при любом методе.
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	switch {
	case len(parts) == 5 && parts[0] == "matches" && parts[2] == "worlds" && parts[4] == "network":
		a.serveNetwork(w, r, parts[1], parts[3])
	case len(parts) == 9 && parts[0] == "matches" && parts[2] == "worlds" &&
		parts[4] == "chunks" && parts[8] == "terrain-patch":
		a.servePatch(w, r, parts[1], parts[3], parts[5], parts[6]+"/"+parts[7])
	default:
		http.NotFound(w, r)
	}
}

func (a *worldsAPI) serveNetwork(w http.ResponseWriter, r *http.Request, match, version string) {
	if match != a.matchID {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "только GET и HEAD", http.StatusMethodNotAllowed)
		return
	}
	v, ok := worldVersion(r, w, a.store, a.region, version)
	if !ok {
		return
	}
	body, hash, ok, err := a.store.GetNetwork(a.region, v)
	if err != nil {
		http.Error(w, "хранилище недоступно", http.StatusInternalServerError)
		return
	}
	if !ok {
		// Сети под версией нет: база засеяна без сети (регион без бутстрапа).
		http.NotFound(w, r)
		return
	}
	etag := `"` + hash + `"`
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if match := r.Header.Get("If-None-Match"); match == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Write([]byte(body))
}

func (a *worldsAPI) servePatch(w http.ResponseWriter, r *http.Request, match, version, level, coord string) {
	if match != a.matchID {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "только GET и HEAD", http.StatusMethodNotAllowed)
		return
	}
	addr, ok := parsePatchAddress(a.region, level, coord)
	if !ok {
		http.NotFound(w, r)
		return
	}
	v, ok := worldVersion(r, w, a.store, a.region, version)
	if !ok {
		return
	}
	// Уровень вне правила региона — 404, а не 204: на несуществующем уровне
	// патч не появится никогда, и 204 обещал бы содержимое, которого не бывает.
	reg, ok, err := a.store.GetRegion(a.region)
	if err != nil {
		http.Error(w, "хранилище недоступно", http.StatusInternalServerError)
		return
	}
	if !ok || addr.Level > reg.Rule.MaxLevel {
		http.NotFound(w, r)
		return
	}

	c, ok, err := a.store.GetChunk(addr, v)
	if err != nil {
		http.Error(w, "хранилище недоступно", http.StatusInternalServerError)
		return
	}
	if !ok {
		// 204 — «для этой версии берётся чистая база»: ни одна строка не
		// старше v не несёт земляных работ на этой клетке. Сам факт пустоты
		// версиен и потому тоже неизменяем.
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	blob, err := chunk.EncodeHeights(c.Heights)
	if err != nil {
		http.Error(w, "чанк повреждён", http.StatusInternalServerError)
		return
	}
	etag := `"` + c.Hash + `"`
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set(HeaderChunkBaseZ, strconv.FormatInt(c.BaseZmm, 10))
	if match := r.Header.Get("If-None-Match"); match == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(blob)))
	w.Write(blob)
}

// worldVersion проверяет номер версии: нечисловый, нулевой или заглядывающий
// за голову проекций — 404, потому что такой версии мира не существует.
// Ответ пишется здесь, а не в вызывающем: проверок две, и разводить их по
// двум ручкам значило бы дублировать форму отказа.
func worldVersion(r *http.Request, w http.ResponseWriter, store *worldstore.Store, region, version string) (int64, bool) {
	v, err := strconv.ParseInt(version, 10, 64)
	if err != nil || v <= 0 {
		http.NotFound(w, r)
		return 0, false
	}
	head, ok, err := store.GetProjectionHead(region)
	if err != nil {
		http.Error(w, "хранилище недоступно", http.StatusInternalServerError)
		return 0, false
	}
	if !ok || v > head.WorldVersion {
		http.NotFound(w, r)
		return 0, false
	}
	return v, true
}

// parsePatchAddress раскладывает /chunks/{level}/{cx}/{cz}/terrain-patch на
// адрес чанка. Разбор формы — тот же, что у chunkPath: нечисловое и
// переполняющее — мусор в пути, а не адрес далёкого угла мира.
func parsePatchAddress(region, level, coord string) (chunk.Address, bool) {
	parts := strings.Split(coord, "/")
	if len(parts) != 2 {
		return chunk.Address{}, false
	}
	l, ok1 := parseInt32(level)
	cx, ok2 := parseInt32(parts[0])
	cz, ok3 := parseInt32(parts[1])
	if !ok1 || !ok2 || !ok3 {
		return chunk.Address{}, false
	}
	return chunk.Address{Region: region, Level: l, CX: cx, CZ: cz}, true
}
