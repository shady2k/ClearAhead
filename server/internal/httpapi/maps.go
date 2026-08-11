package httpapi

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
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
// serveMapOp обслуживает то, что осталось от операций над картой: создание
// затравки. Список, загрузка, сохранение и «сохранить как» удалены вместе с
// файловым хранилищем.
func (a *api) serveMapOp(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/maps/new" {
		return false
	}
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "POST")
		return true
	}
	a.dispatch(w, r, "maps.new", protocol.Input{})
	return true
}

// dispatch разбирает вход диспетчером и пишет JSON-ответ. Ошибки операций
// карты отображаются на коды: карта не найдена — 404, всё остальное (имя
// отвергнуто безопасностью путей, карта не прошла валидатор, невалидное
// представление запроса) — 400: это плохой запрос, а не отсутствующий ресурс.
func (a *api) dispatch(w http.ResponseWriter, r *http.Request, method string, in protocol.Input) {
	got, err := a.m.Dispatch(r.Context(), method, in)
	if err != nil {
		// 404 «карта не найдена» исчезло вместе с поиском по имени: карту
		// больше нельзя спросить по имени, значит и не найтись она не может.
		writeJSONError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, got)
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
