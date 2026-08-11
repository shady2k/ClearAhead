package httpapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/shady2k/ClearAhead/server/internal/mapstore"
	"github.com/shady2k/ClearAhead/server/internal/protocol"
	"github.com/shady2k/ClearAhead/server/internal/rpc"
)

// networkAPI — отдача сети региона.
//
// Хранилище приходит аргументом, а не берётся из глобала, и своё: путь лежит в
// mapstore, рельеф — в worldstore, и ни один из обработчиков не знает чужого.
// Композицию /regions/… из обоих собирает main.go (см. «КОМПОЗИЦИЯ ЖИВЁТ
// ЗДЕСЬ»).
//
// Свой rpc.Mux на одну ручку — не расточительство: Mux это реестр методов
// обработчика, а не общий пакетный уровень. Общий на всю программу означал бы
// глобальное изменяемое состояние там, где хватает поля структуры.
type networkAPI struct {
	store *mapstore.Store
	m     *rpc.Mux
}

// NewNetworkHandler собирает ручку сети региона над каталогом карт.
//
// Маршрут: GET /regions/{region}/revisions/{n}/network
//
// # Почему ресурс называется `network`, а не `track` и не `geometry`
//
// Имя обязано называть КЛАСС содержимого, а не его вид и не его формат.
//
//   - `geometry` называло ФОРМАТ («render geometry» — то, из чего рисуется), а
//     не содержание. На нём спотыкались: «что такое geometry и почему она
//     отдельно от чанка».
//   - `track` называет ОДИН ВИД пути. Решением владельца автомобильные дороги
//     приедут В ЭТОТ ЖЕ ответ, а не вторым ресурсом, — и `track` пришлось бы
//     переименовывать второй раз.
//
// Один ресурс на все виды, а не /network/rail и /network/road: замер, которым
// принято всё решение о доставке (см. ниже), даёт 27 КБ на регион — делить
// нечего, а деление вернуло бы два запроса за одним понятием и заставило бы
// решать, куда отнести общее: типы решётки, путевые объекты, особенности.
//
// Поле `kind` у элемента ЕСТЬ уже сегодня, хотя значение у него пока одно —
// "rail". Первая редакция этого комментария запрещала его ссылкой на
// map-format-design §8 («форма без потребителя»), и это была ошибка: §8 говорит
// про ВОЗМОЖНОСТИ, а различитель вида в персистентных и контрактных данных
// подчиняется обратному правилу — дешевле всего завести вслепую, дороже всего
// мигрировать. Разбор целиком лежит там, где поле объявлено: mapfmt.KindRail.
// Здесь важно одно следствие для клиента: вид элемента приходит полем, а не
// выводится из адреса, и разбирать его надо с первого дня.
//
// # Почему сеть отдаётся целиком, а не почанково
//
// Замер (track-delivery-design §1): элемент — рецепт, а не полилиния, в среднем
// 203 байта. Вся сеть региона со стороной 50 км — ~27 КБ против 6.1 МБ рельефа
// того же региона. Рельеф тяжелее в 220 раз, и резать нечего: режется тяжёлое,
// лёгкое отдаётся целиком. Список покрытия сервер не отдаёт — клиент держит
// сеть региона и выводит покрытие из неё сам.
//
// # Что здесь было
//
// GET /maps/{id}/revisions/{n}/geometry. Тело, ETag и кэш — те же байт в байт;
// сменились корень (регион вместо карты) и имя ресурса. Старый адрес УДАЛЁН, а
// не оставлен вторым: клиента не существует, ломать нечего, а два адреса одного
// ресурса — ровно та двусмысленность, которую бида убирает.
func NewNetworkHandler(store *mapstore.Store) http.Handler {
	a := &networkAPI{store: store, m: rpc.NewMux()}

	// Сеть адресуется ревизией и immutable-кэшируется: тело сериализуется один
	// раз при входе карты в память (mapstore), дальше только пишется в сокет —
	// внутри запроса нет ни чтения с диска, ни перекомпиляции, ни сети. Байты
	// берутся из mapstore.RenderBody — того же места, по которому считается
	// network_hash: своя сериализация означала бы, что ETag когда-нибудь
	// опишет не то тело, которое ушло.
	rpc.Register[protocol.NetworkRequest](a.m, "network",
		func(_ context.Context, req protocol.NetworkRequest) (any, error) {
			st, ok := a.store.Current()
			if !ok {
				return nil, errUnknownResource // пустой старт — ресурса нет
			}
			// Регион сверяется с map_id, и это ЕДИНСТВЕННЫЙ шов между двумя
			// хранилищами: worldstore ключует рельеф регионом, mapstore держит
			// карту, а равенство одного другому заводит worldgen.Bootstrap
			// строкой `region := m.MapID`. Это не совпадение и не костыль:
			// world-storage §3 принял, что map_id ОБОЗНАЧАЕТ РЕГИОН, а станция
			// — именованная область внутри него. Общего типа идентификатора у
			// двух хранилищ по-прежнему нет, поэтому равенство и проверяется
			// здесь, на границе, а не подразумевается клиентом.
			if req.Region() != st.Manifest.MapID || req.Revision() != st.Manifest.Revision {
				return nil, errUnknownResource
			}
			return networkResult{body: st.RenderBody, etag: `"` + st.Manifest.NetworkHash + `"`}, nil
		})

	return a
}

// networkResult — тело сети и ETag одной ревизии. Оба берутся из одного и того
// же состояния карты: ETag не может описать чужое тело.
type networkResult struct {
	body []byte
	etag string
}

func (a *networkAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Разбор пути идёт первым: путь приходит снаружи и недоверен, поэтому
	// сначала выясняется, адресована ли вообще сеть региона, и лишь потом —
	// можно ли его так спрашивать. Синтаксически чужой адрес — «нет такого
	// ресурса» при любом методе, как у чанков.
	region, rev, ok := networkPath(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}

	// HEAD обязателен наравне с GET: RFC 9110 требует его от сервера общего
	// назначения, и на нём держится проверка кэша прокси и клиентов. Тело
	// для HEAD подавляет net/http сам, поэтому дальше ветвление не нужно.
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "только GET и HEAD", http.StatusMethodNotAllowed)
		return
	}

	got, err := a.m.Dispatch(r.Context(), "network", protocol.Input{
		Path: map[string]string{"region": region, "rev": rev},
	})
	if err != nil {
		// Любая ошибка диспетчера — и неразобранный путь, и чужой регион или
		// ревизия, и пустой старт — это «ресурса нет» с точки зрения клиента.
		http.NotFound(w, r)
		return
	}
	res := got.(networkResult)
	res.write(w, r)
}

// write отдаёт тело с immutable-кэшем.
//
// immutable (RFC 8246) здесь ЧЕСТЕН, в отличие от чанков (см. chunks.go): адрес
// называет ревизию, а (регион, ревизия) — состояние, а не место. Пара определяет
// ровно одно тело: иначе лгал бы и network_hash, по которому считается
// ETag. Новая ревизия — новый адрес, и старый ответ никому не мешает.
func (res networkResult) write(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("ETag", res.etag)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if match := r.Header.Get("If-None-Match"); match == res.etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Write(res.body)
}

// networkPath механически раскладывает адрес сети
// /regions/{region}/revisions/{rev}/network на сегменты. Значения не
// проверяются: это работа диспетчера (protocol.Parse). Ручка лишь собирает
// protocol.Input, который он разберёт.
func networkPath(p string) (region, rev string, ok bool) {
	parts := strings.Split(strings.Trim(p, "/"), "/")
	if len(parts) != 5 || parts[0] != "regions" || parts[2] != "revisions" || parts[4] != "network" {
		return "", "", false
	}
	return parts[1], parts[3], true
}
