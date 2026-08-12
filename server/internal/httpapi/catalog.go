package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"

	"github.com/shady2k/ClearAhead/server/internal/mapstore"
	"github.com/shady2k/ClearAhead/server/internal/worldstore"
)

// Каталог регионов: то, из чего игрок ВЫБИРАЕТ, прежде чем во что-то войти.
//
// # Зачем ресурс, если регион и так называется в адресе
//
// Клиент до сих пор узнавал имя региона из ключа запуска (--region=ST_A), то
// есть игрок обязан был знать наизусть то, что знает сервер. Экран «Где играем»
// снесённой оболочки спрашивал каталог у сервера и не держал ни одного литерала
// карты — и это не удобство, а то же правило, что и всюду: миры живут на
// сервере, и добавление мира не должно требовать пересборки клиента.
//
// # Почему регионы, а не карты
//
// У снесённой оболочки экран звался «карты» и ходил в GET /maps — авторскую
// ручку. С тех пор корень у игрока ровно один, и это РЕГИОН (регионы §, бида
// ClearAhead-8kx): сеть лежит в /regions/{id}/revisions/{n}/network, рельеф в
// /regions/{id}/chunks/…. Каталог, отдающий карты, заставил бы клиента знать
// соглашение «регион зовётся как карта» — то самое знание из одной строки
// worldgen.Bootstrap, ради устранения которого корень и сводили в один.
//
// # Чего здесь нет
//
// Ни числа чанков, ни размера, ни границ. Каталог отвечает на единственный
// вопрос — «во что можно войти», — и каждое поле оправдано СЛЕДУЮЩИМ запросом
// либо строкой на экране выбора:
//
//   - region — иначе нечего подставить в /regions/{region};
//   - epoch — поколение мира; клиенту это повод выбросить кэш;
//   - revision — подпись на кнопке, чтобы два одноимённых региона разных
//     ревизий не выглядели одним;
//   - playable — есть ли у региона сеть в памяти. Регион с рельефом и без сети
//     сегодня недостижим (main.go засевает оба одной затравкой), но снаружи он
//     неотличим от опечатки: манифест такого региона отвечает 404. Пусть
//     невозможность войти будет видна ДО того, как игрок нажал кнопку.
type regionCard struct {
	Region   string `json:"region"`
	Epoch    int64  `json:"epoch"`
	Revision int    `json:"revision,omitempty"`
	Playable bool   `json:"playable"`
}

type regionCatalog struct {
	Regions []regionCard `json:"regions"`
}

type catalogAPI struct {
	world *worldstore.Store
	maps  *mapstore.Store
}

// NewRegionCatalogHandler собирает ручку каталога регионов.
//
// Маршрут: GET /regions
//
// Без косой черты на конце — это ресурс-список, а не корень поддерева. Ветку с
// чертой разбирает regionsRouter, и разводит их main.go: у ServeMux "/regions"
// и "/regions/" — разные образцы, и это ровно то различие, которое здесь нужно.
func NewRegionCatalogHandler(world *worldstore.Store, maps *mapstore.Store) http.Handler {
	return &catalogAPI{world: world, maps: maps}
}

func (a *catalogAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "только GET и HEAD", http.StatusMethodNotAllowed)
		return
	}

	regions, err := a.world.ListRegions()
	if err != nil {
		http.Error(w, "хранилище недоступно", http.StatusInternalServerError)
		return
	}

	// Ревизия берётся у той карты, что сейчас в памяти, и только для СВОЕГО
	// региона. Подставить ревизию одной карты всем регионам значило бы соврать
	// в подписи на кнопке — ровно так же, как врал бы каталог, называющий
	// играбельным регион без сети.
	st, haveMap := a.maps.Current()

	cat := regionCatalog{Regions: make([]regionCard, 0, len(regions))}
	for _, reg := range regions {
		card := regionCard{Region: reg.ID, Epoch: reg.Epoch}
		if haveMap && st.Manifest.MapID == reg.ID {
			card.Revision = st.Manifest.Revision
			card.Playable = true
		}
		cat.Regions = append(cat.Regions, card)
	}

	body, err := json.Marshal(cat)
	if err != nil {
		http.Error(w, "каталог не собирается", http.StatusInternalServerError)
		return
	}

	// Пустой каталог — законный ответ 200 с пустым списком, а не 404: сервер без
	// регионов существует и отвечает исправно, а «регионов нет» — это сведение,
	// которое клиенту надо показать словами, а не кодом ошибки.
	sum := sha256.Sum256(body)
	etag := `"` + hex.EncodeToString(sum[:]) + `"`
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "public, no-cache")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if match := r.Header.Get("If-None-Match"); match == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Write(body)
}
