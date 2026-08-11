// Package httpapi отдаёт скомпилированные артефакты мира и принимает операции
// жизненного цикла карты.
//
// Путь остаётся на обычном GET, а не уезжает в сокет: он статичен и кэшируется
// по хешу (vertical-slice-design §7). Операции карты — новая, список, загрузка,
// сохранение — тоже обычные HTTP-ручки: стиль один, второй не изобретается.
//
// # Два корня и почему они разные
//
//	/regions/...  — то, что видит ИГРОК: манифест региона, путь региона, рельеф.
//	/manifest, /maps/new — АВТОРСКАЯ сторона: жизненный цикл карты.
//
// Корень игрока один — регион (бида ClearAhead-8kx). До неё их было два: путь
// лежал на /maps/{id}/revisions/{n}/geometry, рельеф — на /regions/{id}/chunks/…,
// и склеивало их соглашение «region == map_id» из одной строки
// worldgen.Bootstrap. Клиенту приходилось знать это соглашение, чтобы соединить
// путь с рельефом, а имя `geometry` называло формат ресурса, а не его
// содержимое. Адрес /maps/{id}/revisions/{n}/geometry УДАЛЁН, а не оставлен
// вторым: клиента не существует, ломать нечего, а два адреса одного ресурса —
// ровно та двусмысленность, которую бида убирает.
//
// Ручки не разбирают внешний вход: барьер валидации (internal/rpc) требует,
// чтобы до обработчика не доходили ни путь, ни тело. Здесь из HTTP-запроса
// механически извлекаются сегменты пути и тело, собирается protocol.Input, а
// диспетчер разбирает его в проверенный запрос. Вся логика ответа живёт за
// барьером — в обработчиках, зарегистрированных в rpc.Mux.
package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/mapstore"
	"github.com/shady2k/ClearAhead/server/internal/protocol"
	"github.com/shady2k/ClearAhead/server/internal/rpc"
	"github.com/shady2k/ClearAhead/server/internal/track"
)

// NewHandler собирает авторскую сторону: манифест карты и операции её
// жизненного цикла.
//
// Одна карта в памяти — сознательное ограничение В1: карт больше одной не
// существует, а обобщать до реестра сейчас значит писать код без потребителя.
// Сервер может стартовать без карты (пустой старт): манифест тогда отвечает
// 404, а карта появляется операцией «новая».
type api struct {
	store *mapstore.Store
	m     *rpc.Mux
}

func NewHandler(store *mapstore.Store) http.Handler {
	a := &api{store: store, m: rpc.NewMux()}

	// Манифест — «текущее состояние», а не immutable-артефакт: клиент идёт за
	// ним затем, чтобы УЗНАТЬ пару (map_id, ревизия). Отдаётся целиком —
	// хеши пригодятся для будущих проверок кэша.
	rpc.Register[protocol.ManifestRequest](a.m, "manifest",
		func(_ context.Context, _ protocol.ManifestRequest) (any, error) {
			st, ok := a.store.Current()
			if !ok {
				return nil, errUnknownResource // пустой старт
			}
			return st.ManifestBody, nil
		})

	rpc.Register[protocol.NewMapRequest](a.m, "maps.new",
		func(_ context.Context, _ protocol.NewMapRequest) (any, error) {
			st, err := a.store.New()
			if err != nil {
				return nil, err
			}
			return mapResult{Map: st.Map, Manifest: st.Manifest}, nil
		})

	// Ручек загрузки, сохранения и списка карт больше нет: карта не хранится
	// файлом, а присланную документом сервер не принимает — приём разбором
	// исчез вместе с парсером. Карта по-прежнему УЕЗЖАЕТ клиенту в ответе.

	return a
}

func (a *api) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == manifestPath {
		a.serveManifest(w, r)
		return
	}
	if a.serveMapOp(w, r) {
		return
	}
	http.NotFound(w, r)
}

func (a *api) serveManifest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "только GET и HEAD", http.StatusMethodNotAllowed)
		return
	}
	got, err := a.m.Dispatch(r.Context(), "manifest", protocol.Input{})
	if err != nil {
		// Единственная ошибка здесь — невалидное представление запроса (в
		// путь или тело что-то попало) или пустой старт: барьер не пропустил,
		// и до обработчика ничего не дошло. Для клиента это «нет такого
		// ресурса».
		http.NotFound(w, r)
		return
	}
	// Манифест — «текущее состояние», а не immutable-артефакт: его URL
	// не адресует ревизию, поэтому кэш обязан перепроверять, а не залипать
	// на прошлой карте.
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Write(got.([]byte))
}

// errUnknownResource — запрос разобран и проверен, но этой карты, региона или
// ревизии здесь нет (или карты нет вовсе — пустой старт). Ошибка рождается за
// барьером, в обработчике TrackRequest/ManifestRequest.
var errUnknownResource = errors.New("httpapi: нет такой карты, региона или ревизии")

// mapResult — ответ операции «новая»: документ карты и её манифест. Манифест
// несёт пару (map_id, ревизия), по которой клиент берёт артефакты; документ —
// то, что редактор показывает и правит.
type mapResult struct {
	Map      mapfmt.Map     `json:"map"`
	Manifest track.Manifest `json:"manifest"`
}

// manifestPath — путь манифеста загруженной карты. Адресовать манифест нечем:
// карта в памяти одна, а map_id и ревизию клиент узнаёт ИЗ манифеста, поэтому
// ни сегментов пути, ни адресной привязки у запроса нет.
const manifestPath = "/manifest"
