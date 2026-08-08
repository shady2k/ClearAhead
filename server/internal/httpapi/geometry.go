// Package httpapi отдаёт скомпилированные артефакты карты.
//
// Геометрия остаётся на обычном GET, а не уезжает в сокет: она статична, велика
// и кэшируется по хешу (vertical-slice-design §7).
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/shady2k/ClearAhead/server/internal/protocol"
	"github.com/shady2k/ClearAhead/server/internal/rpc"
	"github.com/shady2k/ClearAhead/server/internal/track"
)

// NewHandler отдаёт геометрию одной ревизии одной карты.
//
// Одна карта в памяти — сознательное ограничение В1: карт больше одной не
// существует, а обобщать до реестра сейчас значит писать код без потребителя.
//
// Ручка не разбирает внешний вход: барьер валидации (internal/rpc) требует,
// чтобы до обработчика не доходили ни путь, ни тело. Здесь из HTTP-запроса
// механически извлекаются два сегмента пути, собирается protocol.Input, а
// диспетчер разбирает его в проверенный protocol.GeometryRequest. Вся логика
// ответа живёт за барьером — в обработчике, зарегистрированном в rpc.Mux.
//
// Тело геометрии сериализуется один раз при создании, дальше только пишется в
// сокет: внутри запроса нет ни чтения с диска, ни перекомпиляции, ни сети.
func NewHandler(rg *track.RenderGeometry, man track.Manifest) http.Handler {
	body, err := json.Marshal(rg)
	if err != nil {
		panic("httpapi: геометрия не сериализуется: " + err.Error())
	}
	etag := `"` + man.RenderGeometryHash + `"`

	m := rpc.NewMux()
	rpc.Register[protocol.GeometryRequest](m, "geometry",
		func(_ context.Context, req protocol.GeometryRequest) ([]byte, error) {
			if req.MapID() != man.MapID || req.Revision() != man.Revision {
				return nil, errUnknownResource
			}
			return body, nil
		})

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// HEAD обязателен наравне с GET: RFC 9110 требует его от сервера общего
		// назначения, и на нём держится проверка кэша прокси и клиентов. Тело
		// для HEAD подавляет net/http сам, поэтому дальше ветвление не нужно.
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "только GET и HEAD", http.StatusMethodNotAllowed)
			return
		}
		id, rev, ok := geometryPath(r.URL.Path)
		if !ok {
			http.NotFound(w, r)
			return
		}
		got, err := m.Dispatch(r.Context(), "geometry", protocol.Input{
			Path: map[string]string{"id": id, "rev": rev},
		})
		if err != nil {
			// Любая ошибка диспетчера — и неразобранный путь, и чужая карта или
			// ревизия — это «ресурса нет» с точки зрения клиента.
			http.NotFound(w, r)
			return
		}
		w.Header().Set("ETag", etag)
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if match := r.Header.Get("If-None-Match"); match == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Write(got.([]byte))
	})
}

// errUnknownResource — запрос разобран и проверен, но этой карты или ревизии
// здесь нет. Ошибка рождается за барьером, в обработчике GeometryRequest.
var errUnknownResource = errors.New("httpapi: нет такой карты или ревизии")

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
