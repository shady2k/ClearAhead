// Package httpapi отдаёт скомпилированные артефакты карты и принимает
// операции её жизненного цикла.
//
// Геометрия остаётся на обычном GET, а не уезжает в сокет: она статична,
// велика и кэшируется по хешу (vertical-slice-design §7). Операции карты —
// новая, список, загрузка, сохранение — тоже обычные HTTP-ручки: стиль один,
// второй не изобретается.
package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/mapstore"
	"github.com/shady2k/ClearAhead/server/internal/protocol"
	"github.com/shady2k/ClearAhead/server/internal/rpc"
	"github.com/shady2k/ClearAhead/server/internal/track"
)

// NewHandler собирает диспетчер и HTTP-обёртку над каталогом карт.
//
// Одна карта в памяти — сознательное ограничение В1: карт больше одной не
// существует, а обобщать до реестра сейчас значит писать код без потребителя.
// Сервер может стартовать без карты (пустой старт): ручки геометрии и
// манифеста тогда отвечают 404, а карта появляется операциями «новая» или
// «загрузить».
//
// Ручки не разбирают внешний вход: барьер валидации (internal/rpc) требует,
// чтобы до обработчика не доходили ни путь, ни тело. Здесь из HTTP-запроса
// механически извлекаются сегменты пути и тело, собирается protocol.Input, а
// диспетчер разбирает его в проверенный запрос. Вся логика ответа живёт за
// барьером — в обработчиках, зарегистрированных в rpc.Mux.
type api struct {
	store *mapstore.Store
	m     *rpc.Mux
}

func NewHandler(store *mapstore.Store) http.Handler {
	a := &api{store: store, m: rpc.NewMux()}

	// Геометрия адресуется ревизией и immutable-кэшируется: тело сериализуется
	// один раз при входе карты в память (mapstore), дальше только пишется в
	// сокет — внутри запроса нет ни чтения с диска, ни перекомпиляции, ни
	// сети. Байты берутся из mapstore.RenderBody — того же места, по которому
	// считается render_geometry_hash: своя сериализация означала бы, что ETag
	// когда-нибудь опишет не то тело, которое ушло.
	rpc.Register[protocol.GeometryRequest](a.m, "geometry",
		func(_ context.Context, req protocol.GeometryRequest) (any, error) {
			st, ok := a.store.Current()
			if !ok {
				return nil, errUnknownResource // пустой старт — ресурса нет
			}
			if req.MapID() != st.Manifest.MapID || req.Revision() != st.Manifest.Revision {
				return nil, errUnknownResource
			}
			return geometryResult{body: st.RenderBody, etag: `"` + st.Manifest.RenderGeometryHash + `"`}, nil
		})

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

	rpc.Register[protocol.ListMapsRequest](a.m, "maps.list",
		func(_ context.Context, _ protocol.ListMapsRequest) (any, error) {
			return a.store.List()
		})

	rpc.Register[protocol.NewMapRequest](a.m, "maps.new",
		func(_ context.Context, _ protocol.NewMapRequest) (any, error) {
			st, err := a.store.New()
			if err != nil {
				return nil, err
			}
			return mapResult{Map: st.Map, Manifest: st.Manifest}, nil
		})

	rpc.Register[protocol.LoadMapRequest](a.m, "maps.load",
		func(_ context.Context, req protocol.LoadMapRequest) (any, error) {
			st, err := a.store.Load(req.Name())
			if err != nil {
				return nil, err
			}
			return mapResult{Map: st.Map, Manifest: st.Manifest}, nil
		})

	rpc.Register[protocol.SaveMapRequest](a.m, "maps.save",
		func(_ context.Context, req protocol.SaveMapRequest) (any, error) {
			mm := req.Map()
			return a.store.Save(&mm)
		})

	rpc.Register[protocol.SaveAsMapRequest](a.m, "maps.save-as",
		func(_ context.Context, req protocol.SaveAsMapRequest) (any, error) {
			mm := req.Map()
			return a.store.SaveAs(req.Name(), &mm)
		})

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
	if id, rev, ok := geometryPath(r.URL.Path); ok {
		a.serveGeometry(w, r, id, rev)
		return
	}
	http.NotFound(w, r)
}

// geometryResult — тело геометрии и ETag одной ревизии. Оба берутся из одного
// и того же состояния карты: ETag не может описать чужое тело.
type geometryResult struct {
	body []byte
	etag string
}

func (a *api) serveGeometry(w http.ResponseWriter, r *http.Request, id, rev string) {
	// HEAD обязателен наравне с GET: RFC 9110 требует его от сервера общего
	// назначения, и на нём держится проверка кэша прокси и клиентов. Тело
	// для HEAD подавляет net/http сам, поэтому дальше ветвление не нужно.
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "только GET и HEAD", http.StatusMethodNotAllowed)
		return
	}
	got, err := a.m.Dispatch(r.Context(), "geometry", protocol.Input{
		Path: map[string]string{"id": id, "rev": rev},
	})
	if err != nil {
		// Любая ошибка диспетчера — и неразобранный путь, и чужая карта или
		// ревизия, и пустой старт — это «ресурса нет» с точки зрения клиента.
		http.NotFound(w, r)
		return
	}
	res := got.(geometryResult)
	w.Header().Set("ETag", res.etag)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if match := r.Header.Get("If-None-Match"); match == res.etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Write(res.body)
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

// errUnknownResource — запрос разобран и проверен, но этой карты или ревизии
// здесь нет (или карты нет вовсе — пустой старт). Ошибка рождается за
// барьером, в обработчике GeometryRequest/ManifestRequest.
var errUnknownResource = errors.New("httpapi: нет такой карты или ревизии")

// mapResult — ответ операций «новая» и «загрузить»: документ карты и её
// манифест. Манифест несёт пару (map_id, ревизия), по которой клиент берёт
// геометрию; документ — то, что редактор показывает и правит.
type mapResult struct {
	Map      mapfmt.Map     `json:"map"`
	Manifest track.Manifest `json:"manifest"`
}

// geometryPath механически раскладывает путь /maps/{id}/revisions/{rev}/geometry
// на сегменты. Значения не проверяются: это работа диспетчера (protocol.Parse).
// Ручка лишь собирает protocol.Input, который он разберёт.
func geometryPath(p string) (id, rev string, ok bool) {
	parts := strings.Split(strings.Trim(p, "/"), "/")
	if len(parts) != 5 || parts[0] != "maps" || parts[2] != "revisions" || parts[4] != "geometry" {
		return "", "", false
	}
	return parts[1], parts[3], true
}

// manifestPath — путь манифеста загруженной карты. Адресовать манифест нечем:
// карта в памяти одна, а map_id и ревизию клиент узнаёт ИЗ манифеста, поэтому
// ни сегментов пути, ни адресной привязки у запроса нет.
const manifestPath = "/manifest"
