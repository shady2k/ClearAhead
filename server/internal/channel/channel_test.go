package channel

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/shady2k/ClearAhead/server/internal/content"

	"github.com/shady2k/ClearAhead/server/internal/engine"
	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/match"
	"github.com/shady2k/ClearAhead/server/internal/netloc"
	"github.com/shady2k/ClearAhead/server/internal/protocol"
	"github.com/shady2k/ClearAhead/server/internal/rpc"
	"github.com/shady2k/ClearAhead/server/internal/seedmap"
	"github.com/shady2k/ClearAhead/server/internal/track"
	"github.com/shady2k/ClearAhead/server/internal/uuidv7"
)

// Проверки уровня КАДРА: сокет здесь не поднимается, потому что проверяется не
// он. Разговор целиком, через настоящее соединение, проверяет contract_test.go.

// loco1ID — идентификатор локомотива фикстуры.
const loco1ID = "01a3185c-6001-7242-8242-000000424242"

func testMatch(t *testing.T) *match.Match {
	t.Helper()
	net, set := station(t), testSet(t)
	m := &match.Match{ID: "M1", Region: "ST_A", Units: []match.Unit{{
		ID: loco1ID, Name: "LOCO_1", Type: "VL80",
		At: netloc.PointU{Element: seedmap.StationMain, U: 150, Direction: netloc.DirForward},
	}},
		// Органы в нуле — то же, что делает match.Start у машины с кабиной.
		// Фикстура собирается руками, мимо Start, и без этой строки локомотив
		// оказался бы без кабины вовсе.
		Controls: map[string]match.Controls{loco1ID: match.Stopped()},
	}
	// ОТРЕЗОК ПУТИ — ПО ТОЙ ЖЕ ПРИЧИНЕ, ЧТО И ОРГАНЫ. Фикстура собирается мимо
	// match.Start, а он заводит состояние физики вместе с машиной; без него
	// локомотив приехал бы на провод без отрезка, и договор проверялся бы о
	// форму, которой в бою не бывает.
	st, ok := set.StockType("VL80")
	if !ok {
		t.Fatal("в наборе фикстуры нет паспорта VL80")
	}
	mo, err := match.StartMotion(m.Units[0], st, net.Elements[seedmap.StationMain])
	if err != nil {
		t.Fatalf("начальное состояние фикстуры: %v", err)
	}
	m.SetMotion(loco1ID, mo)
	return m
}

// testSet — набор контента с одним типом VL80: тем же, что боевой, но без
// ассета-локомотива на двадцать мегабайт.
func testSet(t *testing.T) *content.Set {
	t.Helper()
	dir := t.TempDir()
	body := []byte("не glb, подрезка не запрашивается")
	sum := sha256.Sum256(body)
	if err := os.WriteFile(filepath.Join(dir, "x.bin"), body, 0o600); err != nil {
		t.Fatalf("фикстура: %v", err)
	}
	doc := map[string]any{
		"format_version": content.FormatVersion,
		"assets": []any{map[string]any{
			"name": "vid", "file": "x.bin", "media_type": "application/octet-stream",
			"source_hash": content.Addr(hex.EncodeToString(sum[:])),
			"anchor":      "rail_top_gauge_center", "scale": 1.0, "translation": []any{0, 0, 0},
			"attribution": map[string]any{"title": "T", "author": "A", "source": "S",
				"license": "CC0-1.0", "modified": false},
		}},
		"stock": []any{map[string]any{
			"id": "VL80", "length": 34.18, "bogie_base": 24.71, "width": 3.63, "height": 5.4,
			"mass": 192.0, "max_speed": 110.0,
			"resistance": map[string]any{"a": 1.9, "b": 0.01, "c": 0.0003},
			"brake":      map[string]any{"shoes": "cast_iron", "braked_axles": 8, "axle_force": 137.3},
			"traction": map[string]any{
				"adhesive_mass": 192.0, "continuous_force": 401.1, "continuous_speed": 53.6,
			},
			// Числа те же, что в боевом наборе: 33 ступени ЭКГ-8Ж и пять
			// ступеней торможения. Проверки пределов обязаны мерить о них.
			"controls": map[string]any{"traction_notches": 33, "brake_notches": 5,
				"keys": map[string]any{
					"traction": map[string]any{"name": "тяга", "up": []any{"W"}, "down": []any{"S"}},
					"reverser": map[string]any{"name": "реверсор", "up": []any{"R"}, "down": []any{"shift+R"}},
					"brake":    map[string]any{"name": "тормоз", "up": []any{"4"}, "down": []any{"3"}},
					"release":  map[string]any{"name": "экстренная остановка", "up": []any{"0"}},
				}},
			"appearance": "vid",
		}},
	}
	raw, _ := json.Marshal(doc)
	if err := os.WriteFile(filepath.Join(dir, content.FileName), raw, 0o600); err != nil {
		t.Fatalf("фикстура: %v", err)
	}
	s, err := content.Load(dir)
	if err != nil {
		t.Fatalf("набор: %v", err)
	}
	return s
}

// station — скомпилированная сеть затравки: та же, на которой стоит боевая
// расстановка. Нужна проекции состояния на провод (перевод s → u).
func station(t *testing.T) *track.CompiledNetwork {
	t.Helper()
	m := seedmap.Station()
	if err := mapfmt.Validate(m); err != nil {
		t.Fatalf("фикстура карты: %v", err)
	}
	cn, _, err := track.Compile(m)
	if err != nil {
		t.Fatalf("компиляция карты: %v", err)
	}
	return cn
}

func testHandler(t *testing.T) *Handler {
	t.Helper()
	return NewHandler(engine.New(testMatch(t), nil), uuidv7.Deterministic(), testSet(t), station(t))
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
		s.remember(string(rune('a'+i%26))+string(rune(i)), answer{result: json.RawMessage(`1`)})
	}
	if got := len(s.done); got > MaxCachedCommands {
		t.Fatalf("в памяти сессии %d ключей при потолке %d", got, MaxCachedCommands)
	}
	if len(s.order) != len(s.done) {
		t.Fatalf("порядок и карта разошлись: %d против %d", len(s.order), len(s.done))
	}
}

// --- ПЕРВАЯ ДОМЕННАЯ КОМАНДА: органы управления кабины -----------------------

// ticking крутит движок, пока обработчик ждёт фазы приёма.
//
// Без него проверка команды повисла бы навсегда, и это не оснастка ради
// оснастки, а прямое следствие устройства: правка применяется НА ГРАНИЦЕ ТИКА, и
// ответ клиенту рождается там же. В бою тики даёт engine.Run; здесь — эта
// горутина, потому что Run читает настенные часы, а тест на часах — мигающий
// тест.
func ticking(e *engine.Engine) func() {
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
				e.Step()
				time.Sleep(time.Millisecond)
			}
		}
	}()
	return func() { close(stop); <-done }
}

func greeted(t *testing.T, h *Handler) *connState {
	t.Helper()
	st := newConn(h)
	if r := ask(t, h, st, `{"jsonrpc":"2.0","id":1,"method":"hello","params":{"protocol_version":1}}`); r.Error != nil {
		t.Fatalf("рукопожатие отказало: %+v", r.Error)
	}
	return st
}

func setControls(t *testing.T, h *Handler, st *connState, id int, params string) reply {
	t.Helper()
	return ask(t, h, st, `{"jsonrpc":"2.0","id":`+strconv.Itoa(id)+
		`,"method":"`+MethodSetControls+`","params":`+params+`}`)
}

// TestSetControlsAppliesAndAnswersWithWhatStood — команда доезжает до партии, а
// ответ несёт положение, КОТОРОЕ ВСТАЛО.
func TestSetControlsAppliesAndAnswersWithWhatStood(t *testing.T) {
	e := engine.New(testMatch(t), nil)
	h := NewHandler(e, uuidv7.Deterministic(), testSet(t), station(t))
	stop := ticking(e)
	defer stop()
	st := greeted(t, h)

	r := setControls(t, h, st, 2, `{"command_id":"c1","unit":"`+loco1ID+
		`","traction":7,"brake":0,"reverser":"forward"}`)
	if r.Error != nil {
		t.Fatalf("команда отказала: %+v", r.Error)
	}
	var res struct {
		Unit     string         `json:"unit"`
		Controls match.Controls `json:"controls"`
	}
	if err := json.Unmarshal(r.Result, &res); err != nil {
		t.Fatalf("ответ команды не разбирается: %v", err)
	}
	if res.Unit != loco1ID {
		t.Fatalf("ответ про единицу %s", res.Unit)
	}
	want := match.Controls{Traction: 7, Brake: 0, Reverser: match.ReverserForward}
	if res.Controls != want {
		t.Fatalf("в ответе %+v, ожидалось %+v", res.Controls, want)
	}
	// И то же самое обязано стоять В ПАРТИИ, а не только в ответе: ответ,
	// собранный из запроса, был бы эхом.
	got, ok := e.Snapshot().Match.ControlsOf(loco1ID)
	if !ok || got != want {
		t.Fatalf("в партии %+v (есть: %v), ожидалось %+v", got, ok, want)
	}
}

// TestSetControlsRefusals — каждая причина отказа проверяется отдельно: клиент
// разбирает их машинно, и подмена одной другой изменила бы его поведение.
func TestSetControlsRefusals(t *testing.T) {
	cases := []struct {
		name   string
		params string
		reason string
	}{
		{"единицы нет", `{"unit":"01a3185c-6001-7242-8242-0000000ffff1","traction":0,"brake":0,"reverser":"neutral"}`,
			protocol.ReasonUnknownUnit},
		{"ступень тяги за пределом", `{"unit":"` + loco1ID + `","traction":34,"brake":0,"reverser":"forward"}`,
			protocol.ReasonNotchOutOfRange},
		{"ступень тормоза за пределом", `{"unit":"` + loco1ID + `","traction":0,"brake":6,"reverser":"neutral"}`,
			protocol.ReasonNotchOutOfRange},
		{"неизвестный реверсор", `{"unit":"` + loco1ID + `","traction":0,"brake":0,"reverser":"вперёд"}`,
			protocol.ReasonUnknownReverser},
		{"тяга при реверсоре в нуле", `{"unit":"` + loco1ID + `","traction":1,"brake":0,"reverser":"neutral"}`,
			protocol.ReasonTractionWithoutReverser},
	}
	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := engine.New(testMatch(t), nil)
			h := NewHandler(e, uuidv7.Deterministic(), testSet(t), station(t))
			stop := ticking(e)
			defer stop()
			st := greeted(t, h)
			r := setControls(t, h, st, i+2, c.params)
			refusal(t, r, rpc.CodeRefused, c.reason)
			// ОТКАЗ НЕ ОСТАВЛЯЕТ СЛЕДА: партия обязана остаться в прежнем
			// положении, иначе отказ был бы половиной применения.
			if got, _ := e.Snapshot().Match.ControlsOf(loco1ID); got != match.Stopped() {
				t.Fatalf("после отказа в партии %+v", got)
			}
		})
	}
}

// TestSetControlsRequiresAllOrgans — частичная команда отвергается формой, а не
// доезжает до партии с додуманными нулями.
func TestSetControlsRequiresAllOrgans(t *testing.T) {
	e := engine.New(testMatch(t), nil)
	h := NewHandler(e, uuidv7.Deterministic(), testSet(t), station(t))
	stop := ticking(e)
	defer stop()
	st := greeted(t, h)
	r := setControls(t, h, st, 2, `{"unit":"`+loco1ID+`","traction":3}`)
	refusal(t, r, rpc.CodeInvalidParams, "")
}

// TestRepeatedControlsCommandAppliesOnce — идемпотентность НА ДОМЕННОЙ команде.
//
// Проверка сильная: между двумя одинаковыми командами партия правится третьей.
// Без ключа идемпотентности повтор наложил бы прежнее положение поверх новой
// правки; с ключом он возвращает прежний ответ и партии не касается.
func TestRepeatedControlsCommandAppliesOnce(t *testing.T) {
	e := engine.New(testMatch(t), nil)
	h := NewHandler(e, uuidv7.Deterministic(), testSet(t), station(t))
	stop := ticking(e)
	defer stop()
	st := greeted(t, h)

	first := h.request(context.Background(), st, []byte(`{"jsonrpc":"2.0","id":2,"method":"`+
		MethodSetControls+`","params":{"command_id":"c1","unit":"`+loco1ID+
		`","traction":5,"brake":0,"reverser":"forward"}}`))
	setControls(t, h, st, 3, `{"command_id":"c2","unit":"`+loco1ID+
		`","traction":9,"brake":0,"reverser":"forward"}`)
	// Повтор идёт со СВОИМ id — так и бывает в жизни: клиент, не дождавшийся
	// ответа из-за разрыва, шлёт новый запрос с прежним command_id.
	repeat := h.request(context.Background(), st, []byte(`{"jsonrpc":"2.0","id":77,"method":"`+
		MethodSetControls+`","params":{"command_id":"c1","unit":"`+loco1ID+
		`","traction":5,"brake":0,"reverser":"forward"}}`))

	// ТЕЛО ТО ЖЕ, id СВОЙ. Первое — идемпотентность, второе — корреляция, и
	// путать их дорого: ответ с чужим id клиент не соотнесёт ни с чем и будет
	// ждать своего вечно (найдено пробником на живом сервере).
	var firstBody, repeatBody struct {
		ID     json.RawMessage `json:"id"`
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(first, &firstBody); err != nil {
		t.Fatalf("первый ответ не разбирается: %v", err)
	}
	if err := json.Unmarshal(repeat, &repeatBody); err != nil {
		t.Fatalf("повторный ответ не разбирается: %v", err)
	}
	if string(firstBody.Result) != string(repeatBody.Result) {
		t.Fatalf("повтор дал другое тело:\nпервый %s\nповтор %s", firstBody.Result, repeatBody.Result)
	}
	if string(repeatBody.ID) != "77" {
		t.Fatalf("повтор получил ответ с id %s, а спрашивал с 77", repeatBody.ID)
	}
	got, _ := e.Snapshot().Match.ControlsOf(loco1ID)
	if got.Traction != 9 {
		t.Fatalf("повтор переписал партию: ступень %d, ожидалась 9", got.Traction)
	}
}
