package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/shady2k/ClearAhead/server/internal/content"
)

// zeroTime — «времени правки нет», и это осознанный ответ, а не заглушка.
// http.ServeContent при нулевом времени не ставит Last-Modified и не отвечает
// на If-Modified-Since. Так и надо: у набора есть ETag по хешу, у байтов адрес
// сам является их версией, а дата правки файла на диске сервера не говорит о
// содержимом ничего — два сервера с одними байтами дали бы разные даты.
var zeroTime = time.Time{}

// Набор контента и байты ассетов — два ресурса разной природы, и разница у них
// не в размере, а в ЧЕСТНОСТИ КЭША.
//
//	GET /content                 перечень: паспорта и записи ассетов
//	GET /assets/{alg}-{hex}       байты одного ассета
//
// Перечень адресуется ИМЕНЕМ ресурса и потому меняется под своим адресом:
// Cache-Control у него no-cache (перепроверять всегда), ETag — хеш перечня.
// Обещать ему immutable значило бы дословно повторить ClearAhead-5vr, где
// адрес называет место, а не состояние места.
//
// Байты адресуются СОДЕРЖИМЫМ, и immutable на них честен по построению:
// изменившиеся байты имеют другой адрес. Это единственный ресурс проекта, у
// которого так; сеть честна по договорённости (ревизия в адресе), чанк нечестен
// вовсе.

type contentAPI struct {
	set *content.Set
	// body — тело ответа, собранное ОДИН раз при старте. Набор неизменен за
	// время жизни процесса, и сериализовать его на каждый запрос значило бы
	// платить за то, что не меняется.
	body []byte
	etag string
}

// wireAsset — запись ассета на проводе.
//
// Отличается от content.Asset ровно тем, чего клиенту знать НЕ НАДО: имени
// файла, объявленного исходного хеша и перечня выброшенных узлов. Всё это —
// подробности укладки: клиент получает адрес байтов и постановку, а как сервер
// эти байты добыл, его не касается. Атрибуция при этом едет целиком и всегда:
// раздача ассета есть его распространение.
type wireAsset struct {
	Name        string     `json:"name"`
	MediaType   string     `json:"media_type"`
	Hash        string     `json:"hash"`
	Size        int        `json:"size"`
	Anchor      string     `json:"anchor"`
	Scale       float64    `json:"scale"`
	Translation [3]float64 `json:"translation"`
	// Cabs — посты машиниста, в осях ассета и ДО постановки, ровно как в наборе.
	// Уезжают клиенту вместе с масштабом и сдвигом, потому что без них
	// бесполезны: постановку применяет тот же, кто ставит вершины.
	//
	// omitempty здесь ЗАКОННО, а не экономия байтов: машина без кабины — вагон и
	// платформа — постов не имеет вовсе, и пустой список на проводе означал бы
	// то же самое более длинным способом.
	Cabs        [][3]float64        `json:"cabs,omitempty"`
	Attribution content.Attribution `json:"attribution"`
}

type wireContent struct {
	FormatVersion int                 `json:"format_version"`
	Hash          string              `json:"hash"`
	Stock         []content.StockType `json:"stock"`
	Assets        []wireAsset         `json:"assets"`
}

// NewContentHandler собирает ручку набора.
func NewContentHandler(set *content.Set) http.Handler {
	doc := wireContent{
		FormatVersion: content.FormatVersion,
		Hash:          set.Hash,
		Stock:         set.Stock,
		Assets:        make([]wireAsset, 0, len(set.Assets)),
	}
	for _, a := range set.Assets {
		doc.Assets = append(doc.Assets, wireAsset{
			Name: a.Name, MediaType: a.MediaType, Hash: a.Hash, Size: a.Size,
			Anchor: a.Anchor, Scale: a.Scale, Translation: a.Translation,
			Cabs: a.Cabs, Attribution: a.Attribution,
		})
	}
	body, err := json.Marshal(doc)
	if err != nil {
		// Сериализация перечня строк и чисел не падает: паника здесь означала бы
		// смену типов, а не входные данные.
		panic("httpapi: сериализация набора контента: " + err.Error())
	}
	return &contentAPI{set: set, body: body, etag: `"` + set.Hash + `"`}
}

func (a *contentAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/content" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "только GET и HEAD", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "public, no-cache")
	w.Header().Set("ETag", a.etag)
	if match := r.Header.Get("If-None-Match"); match != "" && strings.Contains(match, a.etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	http.ServeContent(w, r, "", zeroTime, bytes.NewReader(a.body))
}

type blobAPI struct {
	set *content.Set
}

// NewBlobHandler собирает ручку байтов ассета.
func NewBlobHandler(set *content.Set) http.Handler { return &blobAPI{set: set} }

func (a *blobAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) != 2 || parts[0] != "assets" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "только GET и HEAD", http.StatusMethodNotAllowed)
		return
	}
	body, ok := a.set.Blob(parts[1])
	if !ok {
		// 404, а не 204: пустоты в пространстве хешей не существует. У чанка
		// пустота законна («здесь ничего нет, рисуй базовую поверхность»),
		// потому что он адресуется МЕСТОМ; здесь адрес — само содержимое, и
		// «нет такого» означает выдуманный адрес.
		http.NotFound(w, r)
		return
	}
	asset, _ := a.assetByHash(parts[1])
	w.Header().Set("Content-Type", asset.MediaType)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("ETag", `"`+parts[1]+`"`)
	// ServeContent сам ставит Accept-Ranges, разбирает Range и отвечает на HEAD.
	// Возобновляемая загрузка здесь не удобство: первый вход качает десятки
	// мегабайт, и оборванная загрузка без возобновления начинается с нуля.
	http.ServeContent(w, r, "", zeroTime, bytes.NewReader(body))
}

func (a *blobAPI) assetByHash(hash string) (content.Asset, bool) {
	for _, as := range a.set.Assets {
		if as.Hash == hash {
			return as, true
		}
	}
	return content.Asset{}, false
}
