package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/shady2k/ClearAhead/server/internal/match"
)

// Живое состояние партии: GET /regions/{region}/live.
//
// # Почему отдельный ресурс, а не поле сети
//
// Сеть отдаётся с ревизией в адресе и Cache-Control: immutable — это законно
// ровно потому, что сеть неподвижна. Положение состава меняется. Положить его в
// ответ сети значило бы своими руками повторить ClearAhead-5vr: адрес обещает
// неизменность содержимому, которое меняется.
//
// # Почему no-store, а не ETag
//
// Потому что это состояние, а не документ. ETag имеет смысл там, где ответ
// повторяется; здесь повторяемость — временное свойство того, что сегодня
// ничего не движется, и опереться на неё значило бы построить кэш, который
// сломается в тот день, когда локомотив поедет.
//
// # Что тут будет вместо HTTP
//
// Веха В2 приносит один WebSocket с JSON-RPC 2.0, и тело этого ответа
// становится телом снапшота. Ручка заведена как ВРЕМЕННАЯ и названа так вслух:
// сегодня транспорт один — HTTP, и без неё стоящий локомотив не доехал бы до
// клиента вовсе.
//
// # Чего в теле нет
//
// Ни времени, ни скорости, ни ускорения, ни горизонта прогноза. Клиенту сегодня
// нечего экстраполировать, и поля, которых никто не считает, поехали бы нулями
// — то есть ложью, неотличимой от «не заполнено».
type liveAPI struct {
	m *match.Match
}

// NewLiveHandler собирает ручку живого состояния.
func NewLiveHandler(m *match.Match) http.Handler { return &liveAPI{m: m} }

// wireLive — тело ответа.
//
// Поле match называет партию, которой в адресе ещё нет: сущность объявлена
// раньше, чем адресована. Когда партий станет две, клиент уже знает слово и
// поле, а не узнаёт о них вместе со сменой адреса.
type wireLive struct {
	Region string       `json:"region"`
	Match  string       `json:"match"`
	Units  []match.Unit `json:"units"`
}

func (a *liveAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) != 3 || parts[0] != "regions" || parts[2] != "live" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "только GET и HEAD", http.StatusMethodNotAllowed)
		return
	}
	if parts[1] != a.m.Region {
		http.NotFound(w, r)
		return
	}
	// Единиц может не быть вовсе — это законное состояние мира, и отвечать на
	// него надо пустым списком, а не 404: партия существует, состава в ней нет.
	units := a.m.Units
	if units == nil {
		units = []match.Unit{}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(wireLive{Region: a.m.Region, Match: a.m.ID, Units: units})
}
