package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/shady2k/ClearAhead/server/internal/engine"
	"github.com/shady2k/ClearAhead/server/internal/match"
	"github.com/shady2k/ClearAhead/server/internal/track"
	"github.com/shady2k/ClearAhead/server/internal/units"
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
// Ни скорости, ни ускорения, ни горизонта прогноза. Клиенту сегодня нечего
// экстраполировать, и поля, которых никто не считает, поехали бы нулями — то
// есть ложью, неотличимой от «не заполнено».
//
// ВРЕМЯ ИЗ ЭТОГО СПИСКА ВЫБЫЛО 2026-08-13: у мира появился ход (engine), и
// модельное время — единственная величина, которую сегодня действительно
// считают. Оно и едет.
//
// # Почему обработчик спрашивает движок, а не держит партию
//
// Здесь стояло `m *match.Match`, и поля читались прямо в ServeHTTP. Пока партия
// неподвижна, это законно; движок делает её подвижной, и указатель на состояние,
// которое правит другая горутина, — это гонка. Читатель получает КОПИЮ на
// границе тика (шов 4: единственный владелец состояния).
type liveAPI struct {
	e *engine.Engine
	// net — сеть региона: положение машины живёт в s вдоль оси, а клиент читает
	// u вдоль карты. Перевод делает партия (match.States) — одна проекция на оба
	// транспорта, канал и эту ручку.
	net *track.CompiledNetwork
}

// NewLiveHandler собирает ручку живого состояния.
//
// Принимает движок, а не партию, и это не удобство вызова: партию у движка
// нельзя взять иначе как снимком, и обработчик, которому дали бы её напрямую,
// имел бы способ прочитать состояние мимо замка.
func NewLiveHandler(e *engine.Engine, net *track.CompiledNetwork) http.Handler {
	return &liveAPI{e: e, net: net}
}

// wireLive — тело ответа.
//
// Поле match называет партию, которой в адресе ещё нет: сущность объявлена
// раньше, чем адресована. Когда партий станет две, клиент уже знает слово и
// поле, а не узнаёт о них вместе со сменой адреса.
// Поле time — МОДЕЛЬНОЕ время партии, микросекунды строкой (units.SimTime).
// Номер тика рядом не едет нарочно: это внутренний счётчик координации, время
// выводится из него, и два написания одной величины на проводе разошлись бы у
// первого же клиента, который выбрал не то.
type wireLive struct {
	Region string            `json:"region"`
	Match  string            `json:"match"`
	Time   units.SimTime     `json:"time"`
	Units  []match.UnitState `json:"units"`
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
	// Снимок берётся ПОСЛЕ проверок адреса и метода: копировать состояние ради
	// ответа 404 незачем.
	snap := a.e.Snapshot()
	if parts[1] != snap.Match.Region {
		http.NotFound(w, r)
		return
	}
	// Единиц может не быть вовсе — это законное состояние мира, и отвечать на
	// него надо пустым списком, а не 404: партия существует, состава в ней нет.
	//
	// Переменная названа placed, а не units: имя units занято ПАКЕТОМ единиц
	// измерения, и локальная переменная затенила бы его ровно в том месте, где
	// рядом стоит units.SimTime.
	placed, err := snap.Match.States(a.net)
	if err != nil {
		http.Error(w, "состояние партии не проецируется на провод", http.StatusInternalServerError)
		return
	}
	if placed == nil {
		placed = []match.UnitState{}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(wireLive{
		Region: snap.Match.Region, Match: snap.Match.ID, Time: snap.Time, Units: placed})
}
