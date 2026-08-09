package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/mapstore"
	"github.com/shady2k/ClearAhead/server/internal/protocol"
)

// Операции жизненного цикла карты: список, новая, загрузить, сохранить,
// сохранить как. Стиль тот же, что у геометрии и манифеста: ручка механически
// раскладывает путь и тело в protocol.Input, а разбор, проверка и решение
// живут за барьером — в обработчиках, зарегистрированных в rpc.Mux.
//
// Пути:
//
//	GET  /maps                 — список карт
//	POST /maps/new             — новая карта-затравка
//	POST /maps/load/{name}     — загрузить карту из каталога
//	POST /maps/save            — сохранить под текущим именем (тело — карта)
//	POST /maps/save-as/{name}  — сохранить под новым именем (тело — карта)
//
// Безопасность путей — требование, а не пожелание: имя проверяет mapstore
// (см. checkPath), сюда оно приходит одним сегментом пути и больше ничем.
func (a *api) serveMapOp(w http.ResponseWriter, r *http.Request) bool {
	switch {
	case r.URL.Path == "/maps":
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			methodNotAllowed(w, "GET, HEAD")
			return true
		}
		a.dispatch(w, r, "maps.list", protocol.Input{})
		return true

	case r.URL.Path == "/maps/new":
		if r.Method != http.MethodPost {
			methodNotAllowed(w, "POST")
			return true
		}
		a.dispatch(w, r, "maps.new", protocol.Input{})
		return true

	case r.URL.Path == "/maps/save":
		if r.Method != http.MethodPost {
			methodNotAllowed(w, "POST")
			return true
		}
		a.dispatch(w, r, "maps.save", protocol.Input{Body: readBody(r)})
		return true
	}

	op, name, ok := mapNamePath(r.URL.Path)
	if !ok {
		return false
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "POST")
		return true
	}
	switch op {
	case "load":
		a.dispatch(w, r, "maps.load", protocol.Input{Path: map[string]string{"name": name}})
	case "save-as":
		a.dispatch(w, r, "maps.save-as", protocol.Input{
			Path: map[string]string{"name": name},
			Body: readBody(r),
		})
	}
	return true
}

// dispatch разбирает вход диспетчером и пишет JSON-ответ. Ошибки операций
// карты отображаются на коды: карта не найдена — 404, всё остальное (имя
// отвергнуто безопасностью путей, карта не прошла валидатор, невалидное
// представление запроса) — 400: это плохой запрос, а не отсутствующий ресурс.
func (a *api) dispatch(w http.ResponseWriter, r *http.Request, method string, in protocol.Input) {
	got, err := a.m.Dispatch(r.Context(), method, in)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, mapstore.ErrNoSuch) {
			status = http.StatusNotFound
		}
		writeJSONError(w, status, err)
		return
	}
	writeJSON(w, got)
}

// mapNamePath раскладывает /maps/load/{name} и /maps/save-as/{name} на
// операцию и имя. Значения не проверяются: это работа диспетчера
// (protocol.Parse). Ручка лишь собирает protocol.Input.
func mapNamePath(p string) (op, name string, ok bool) {
	parts := strings.Split(strings.Trim(p, "/"), "/")
	if len(parts) != 3 || parts[0] != "maps" {
		return "", "", false
	}
	switch parts[1] {
	case "load", "save-as":
		return parts[1], parts[2], true
	}
	return "", "", false
}

// readBody механически вычитывает тело запроса. Значения не проверяются и не
// разбираются: это работа диспетчера. Размер ограничен лимитом документа
// карты (mapfmt.MaxDocumentBytes) — документ больше лимита отвергнет сам
// разбор.
func readBody(r *http.Request) json.RawMessage {
	b, _ := io.ReadAll(io.LimitReader(r.Body, mapfmt.MaxDocumentBytes+1))
	return json.RawMessage(b)
}

// writeJSON пишет ответ: готовые байты ([]byte) — как есть, значения —
// сериализацией. Геометрия и манифест приходят байтами из mapstore (они
// сериализованы один раз при входе карты в память), результаты операций —
// значениями.
func writeJSON(w http.ResponseWriter, got any) {
	var b []byte
	if raw, ok := got.([]byte); ok {
		b = raw
	} else {
		var err error
		if b, err = json.Marshal(got); err != nil {
			http.Error(w, "ошибка сериализации ответа", http.StatusInternalServerError)
			return
		}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Write(b)
}

// writeJSONError пишет ошибку операции как JSON — текст ошибки доезжает до
// клиента, а не тонет в пустом 4xx.
func writeJSONError(w http.ResponseWriter, status int, err error) {
	b, _ := json.Marshal(map[string]string{"error": err.Error()})
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	w.Write(b)
}

func methodNotAllowed(w http.ResponseWriter, allow string) {
	w.Header().Set("Allow", allow)
	http.Error(w, "метод не разрешён", http.StatusMethodNotAllowed)
}
