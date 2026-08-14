package channel

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/engine"
	"github.com/shady2k/ClearAhead/server/internal/match"
	"github.com/shady2k/ClearAhead/server/internal/netloc"
	"github.com/shady2k/ClearAhead/server/internal/protocol"
	"github.com/shady2k/ClearAhead/server/internal/rpc"
	"github.com/shady2k/ClearAhead/server/internal/seedmap"
	"github.com/shady2k/ClearAhead/server/internal/uuidv7"
)

// Проверки уровня КАДРА: сокет здесь не поднимается, потому что проверяется не
// он. Разговор целиком, через настоящее соединение, проверяет contract_test.go.

func testMatch() *match.Match {
	return &match.Match{ID: "M1", Region: "ST_A", Units: []match.Unit{{
		ID: "01a3185c-6001-7242-8242-000000424242", Name: "LOCO_1", Type: "VL80",
		At: netloc.PointU{Element: seedmap.StationMain, U: 150, Direction: netloc.DirForward},
	}}}
}

func testHandler(t *testing.T) *Handler {
	t.Helper()
	return NewHandler(engine.New(testMatch()), uuidv7.Deterministic())
}

// newConn — соединение без сокета: h.request сокета не касается вовсе, а
// поднимать его ради разбора кадра значило бы проверять библиотеку.
func newConn(h *Handler) *connState {
	st := &connState{}
	st.mux = h.newMux(st)
	return st
}

type reply struct {
	ID     json.RawMessage `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int               `json:"code"`
		Message string            `json:"message"`
		Data    *protocol.Refusal `json:"data"`
	} `json:"error"`
}

func ask(t *testing.T, h *Handler, st *connState, raw string) reply {
	t.Helper()
	body := h.request(context.Background(), st, []byte(raw))
	if body == nil {
		t.Fatalf("на кадр %s ответа не было", raw)
	}
	var r reply
	if err := json.Unmarshal(body, &r); err != nil {
		t.Fatalf("ответ не разбирается: %v (%s)", err, body)
	}
	return r
}

func refusal(t *testing.T, r reply, code int, reason string) {
	t.Helper()
	if r.Error == nil {
		t.Fatalf("ожидался отказ %s, пришёл успех: %s", reason, r.Result)
	}
	if r.Error.Code != code {
		t.Fatalf("код отказа %d, ожидался %d (%s)", r.Error.Code, code, r.Error.Message)
	}
	if reason == "" {
		return
	}
	if r.Error.Data == nil || r.Error.Data.Reason != reason {
		t.Fatalf("причина отказа %+v, ожидалась %q", r.Error.Data, reason)
	}
}

func TestHelloAnswersWithSessionAndSnapshot(t *testing.T) {
	h := testHandler(t)
	st := newConn(h)
	r := ask(t, h, st, `{"jsonrpc":"2.0","id":1,"method":"hello","params":{"protocol_version":1}}`)
	if r.Error != nil {
		t.Fatalf("рукопожатие отказало: %+v", r.Error)
	}
	var res struct {
		ProtocolVersion int    `json:"protocol_version"`
		SessionID       string `json:"session_id"`
		ActorID         string `json:"actor_id"`
		Snapshot        struct {
			SnapshotSeq uint64 `json:"snapshot_seq"`
			Kind        string `json:"kind"`
			Region      string `json:"region"`
			Units       []struct {
				ID string `json:"id"`
			} `json:"units"`
		} `json:"snapshot"`
	}
	if err := json.Unmarshal(r.Result, &res); err != nil {
		t.Fatalf("ответ рукопожатия не разбирается: %v", err)
	}
	if res.ProtocolVersion != protocol.ProtocolVersion {
		t.Fatalf("версия в ответе %d", res.ProtocolVersion)
	}
	if res.SessionID == "" || res.ActorID == "" {
		t.Fatalf("сессия или актёр не выданы: %+v", res)
	}
	if res.SessionID == res.ActorID {
		// Разные сущности обязаны иметь разные идентификаторы: совпадение
		// прошло бы незамеченным ровно до появления второго соединения в одной
		// сессии.
		t.Fatal("сессия и актёр получили один идентификатор")
	}
	// Рукопожатие отдаёт ПЕРВЫЙ снапшот сессии — иначе клиент ждал бы первого
	// изменения мира, чтобы узнать, что в нём стоит.
	if res.Snapshot.SnapshotSeq != 1 {
		t.Fatalf("номер первого снапшота %d, ожидался 1", res.Snapshot.SnapshotSeq)
	}
	if res.Snapshot.Kind != KindFull || res.Snapshot.Region != "ST_A" || len(res.Snapshot.Units) != 1 {
		t.Fatalf("снапшот рукопожатия: %+v", res.Snapshot)
	}
}

// TestCommandBeforeHelloIsRefused — до рукопожатия сервер не знает ни версии
// собеседника, ни его имени; принять команду значило бы применить её от имени
// никого.
func TestCommandBeforeHelloIsRefused(t *testing.T) {
	h := testHandler(t)
	st := newConn(h)
	r := ask(t, h, st, `{"jsonrpc":"2.0","id":1,"method":"throttle.set","params":{"command_id":"c1"}}`)
	refusal(t, r, rpc.CodeRefused, protocol.ReasonNotGreeted)
}

// TestUnknownMethodAndBadParamsDifferInCode — разница, ради которой в барьере
// завелись сентинелы: «метода нет» и «метод есть, запрос неверен» — разные
// новости для клиента, и чинит он по ним разное.
func TestUnknownMethodAndBadParamsDifferInCode(t *testing.T) {
	h := testHandler(t)
	st := newConn(h)
	ask(t, h, st, `{"jsonrpc":"2.0","id":1,"method":"hello","params":{"protocol_version":1}}`)

	unknown := ask(t, h, st, `{"jsonrpc":"2.0","id":2,"method":"нетакого","params":{}}`)
	refusal(t, unknown, rpc.CodeMethodNotFound, "")

	// Неизвестное поле в params — отказ, а не молчаливое пропускание: клиент с
	// опечаткой в имени поля обязан узнать о ней здесь, а не искать, почему
	// сервер «не слушается».
	bad := ask(t, h, st, `{"jsonrpc":"2.0","id":3,"method":"hello","params":{"protocol_versoin":1}}`)
	refusal(t, bad, rpc.CodeInvalidParams, "")
}

func TestUnsupportedProtocolVersionIsRefusedWithBothNumbers(t *testing.T) {
	h := testHandler(t)
	st := newConn(h)
	r := ask(t, h, st, `{"jsonrpc":"2.0","id":1,"method":"hello","params":{"protocol_version":9000}}`)
	refusal(t, r, rpc.CodeRefused, protocol.ReasonUnsupportedProtocol)
	// В машинной причине — версия СЕРВЕРА: свою клиент знает и сам, а
	// действовать ему нужно по чужой. Держателя ресурса здесь нет: несовпадение
	// версий не конфликт за ресурс.
	if r.Error.Data.ResourceID != "1" || r.Error.Data.HeldBy != "" {
		t.Fatalf("отказ версии называет не то: %+v", r.Error.Data)
	}
	// Обе стороны названы человеку — иначе он читает «не поддерживается» и не
	// знает, на что переходить.
	if !strings.Contains(r.Error.Message, "9000") || !strings.Contains(r.Error.Message, "сервер говорит на 1") {
		t.Fatalf("текст отказа не называет обе версии: %q", r.Error.Message)
	}
	if st.session != nil {
		t.Fatal("отказанное рукопожатие всё равно завело сессию")
	}
}

// TestUnknownSessionIsRefusedInsteadOfSilentlyReplaced — сервер перезапустили,
// и ключи идемпотентности клиента больше ничего не значат. Тихо выдать новую
// сессию значило бы скрыть это: клиент продолжил бы считать, что его повторы
// защищены.
func TestUnknownSessionIsRefused(t *testing.T) {
	h := testHandler(t)
	st := newConn(h)
	r := ask(t, h, st,
		`{"jsonrpc":"2.0","id":1,"method":"hello","params":{"protocol_version":1,"session_id":"нет-такой"}}`)
	refusal(t, r, rpc.CodeRefused, protocol.ReasonUnknownSession)
}

func TestSecondHelloOnSameConnectionIsRefused(t *testing.T) {
	h := testHandler(t)
	st := newConn(h)
	ask(t, h, st, `{"jsonrpc":"2.0","id":1,"method":"hello","params":{"protocol_version":1}}`)
	second := ask(t, h, st, `{"jsonrpc":"2.0","id":2,"method":"hello","params":{"protocol_version":1}}`)
	refusal(t, second, rpc.CodeRefused, protocol.ReasonAlreadyGreeted)
}

// TestSessionSurvivesReconnect — сессия переживает соединение.
//
// Второе соединение, назвавшее ту же сессию, обязано её ПРОДОЛЖИТЬ: тот же
// актёр, тот же счётчик снапшотов. Иначе повтор команды после разрыва стал бы
// вторым применением — то есть идемпотентность отказывала бы ровно там, ради
// чего заведена.
func TestSessionSurvivesReconnect(t *testing.T) {
	h := testHandler(t)
	first := newConn(h)
	r := ask(t, h, first, `{"jsonrpc":"2.0","id":1,"method":"hello","params":{"protocol_version":1}}`)
	var res struct {
		SessionID string `json:"session_id"`
		ActorID   string `json:"actor_id"`
		Snapshot  struct {
			SnapshotSeq uint64 `json:"snapshot_seq"`
		} `json:"snapshot"`
	}
	if err := json.Unmarshal(r.Result, &res); err != nil {
		t.Fatalf("ответ рукопожатия не разбирается: %v", err)
	}

	second := newConn(h)
	back := ask(t, h, second,
		`{"jsonrpc":"2.0","id":1,"method":"hello","params":{"protocol_version":1,"session_id":"`+res.SessionID+`","last_snapshot_seq":1}}`)
	if back.Error != nil {
		t.Fatalf("переподключение отказало: %+v", back.Error)
	}
	var again struct {
		SessionID string `json:"session_id"`
		ActorID   string `json:"actor_id"`
		Snapshot  struct {
			SnapshotSeq uint64 `json:"snapshot_seq"`
		} `json:"snapshot"`
	}
	if err := json.Unmarshal(back.Result, &again); err != nil {
		t.Fatalf("ответ переподключения не разбирается: %v", err)
	}
	if again.SessionID != res.SessionID || again.ActorID != res.ActorID {
		t.Fatalf("переподключение выдало другую сессию или актёра: %+v против %+v", again, res)
	}
	// Счётчик снапшотов принадлежит СЕССИИ и продолжается, а не начинается
	// заново: клиент по нему видит пропуски, и обнуление означало бы, что после
	// каждого разрыва пропусков как будто не было.
	if again.Snapshot.SnapshotSeq != res.Snapshot.SnapshotSeq+1 {
		t.Fatalf("после переподключения номер снапшота %d, ожидался %d",
			again.Snapshot.SnapshotSeq, res.Snapshot.SnapshotSeq+1)
	}
}

// TestRepeatedCommandIDGetsTheSameAnswerByteForByte — идемпотентность.
//
// Проверяется на рукопожатии, потому что доменных команд в этой вехе нет
// нарочно, а свойство от этого не меняется: повтор с тем же command_id обязан
// вернуть ТОТ ЖЕ ответ, а не быть применённым второй раз.
//
// Проверка сильная, а не формальная: без ключа идемпотентности второе
// рукопожатие получило бы отказ already_greeted (см. соседний тест). Совпадение
// байт доказывает, что до обработчика повтор не дошёл вовсе.
func TestRepeatedCommandIDGetsTheSameAnswerByteForByte(t *testing.T) {
	h := testHandler(t)
	st := newConn(h)
	const frame = `{"jsonrpc":"2.0","id":1,"method":"hello","params":{"protocol_version":1,"command_id":"c1"}}`
	first := h.request(context.Background(), st, []byte(frame))
	second := h.request(context.Background(), st, []byte(frame))
	if string(first) != string(second) {
		t.Fatalf("повтор команды дал другой ответ:\nпервый %s\nвторой %s", first, second)
	}
}

// TestDifferentCommandIDIsNotDeduplicated — обратная сторона: дедупликация не
// имеет права склеить две РАЗНЫЕ команды. Иначе клиент, пославший две ступени
// торможения подряд, получил бы одну.
func TestDifferentCommandIDIsNotDeduplicated(t *testing.T) {
	h := testHandler(t)
	st := newConn(h)
	ask(t, h, st, `{"jsonrpc":"2.0","id":1,"method":"hello","params":{"protocol_version":1,"command_id":"c1"}}`)
	other := ask(t, h, st,
		`{"jsonrpc":"2.0","id":2,"method":"hello","params":{"protocol_version":1,"command_id":"c2"}}`)
	refusal(t, other, rpc.CodeRefused, protocol.ReasonAlreadyGreeted)
}

// TestCommandCacheHasCeiling — потолок памяти сессии соблюдается, и старые
// ключи выбрасываются первыми.
func TestCommandCacheHasCeiling(t *testing.T) {
	s := newSession("s", "a")
	for i := range MaxCachedCommands + 10 {
		s.remember(string(rune('a'+i%26))+string(rune(i)), []byte("x"))
	}
	if got := len(s.done); got > MaxCachedCommands {
		t.Fatalf("в памяти сессии %d ключей при потолке %d", got, MaxCachedCommands)
	}
	if len(s.order) != len(s.done) {
		t.Fatalf("порядок и карта разошлись: %d против %d", len(s.order), len(s.done))
	}
}
