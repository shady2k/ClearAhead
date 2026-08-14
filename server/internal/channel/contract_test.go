package channel_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/shady2k/ClearAhead/server/internal/channel"
	"github.com/shady2k/ClearAhead/server/internal/contract"
	"github.com/shady2k/ClearAhead/server/internal/engine"
	"github.com/shady2k/ClearAhead/server/internal/match"
	"github.com/shady2k/ClearAhead/server/internal/netloc"
	"github.com/shady2k/ClearAhead/server/internal/protocol"
	"github.com/shady2k/ClearAhead/server/internal/rpc"
	"github.com/shady2k/ClearAhead/server/internal/seedmap"
	"github.com/shady2k/ClearAhead/server/internal/units"
	"github.com/shady2k/ClearAhead/server/internal/uuidv7"
)

// ДОГОВОР ПРОВЕРЯЕТСЯ С ДВУХ СТОРОН, и это здесь только одна из них.
//
// Файл contract/channel.v1.json объявляет имена полей, их виды и
// обязательность. Здесь с ним сверяется СЕРВЕР: настоящий разговор через
// настоящий сокет, каждое сообщение — против объявленного вида. Вторая сторона
// (клиент на GDScript) читает тот же файл и сверяет с ним свой разбор.
//
// Почему не сверка байт в байт с эталоном ответа: она в этом проекте уже была и
// снесена (коммит 4b4d6a7) — вычисленные float64 не переживают смены машины,
// расхождение ~1e-13 на неизменном коде. Договор о форме от порядка вычислений
// не зависит.

// contractPath — путь к договору от каталога пакета.
//
// Договор лежит В КОРНЕ РЕПОЗИТОРИЯ, а не внутри server/: его читает и клиент,
// а файл, лежащий у одной из сторон, эта сторона рано или поздно начнёт править
// под себя, не спросив вторую.
func contractPath() string { return filepath.Join("..", "..", "..", "contract", "channel.v1.json") }

func testMatch() *match.Match {
	return &match.Match{ID: "M1", Region: "ST_A", Units: []match.Unit{{
		ID: "01a3185c-6001-7242-8242-000000424242", Name: "LOCO_1", Type: "VL80",
		At: netloc.PointU{Element: seedmap.StationMain, U: 150, Direction: netloc.DirForward},
	}}}
}

// talk — поднятый сервер и открытый сокет.
type talk struct {
	t   *testing.T
	e   *engine.Engine
	ws  *websocket.Conn
	doc contract.Doc
	ctx context.Context
}

func dial(t *testing.T) *talk {
	t.Helper()
	doc, err := contract.Load(contractPath())
	if err != nil {
		t.Fatalf("договор не читается: %v", err)
	}
	e := engine.New(testMatch())
	srv := httptest.NewServer(channel.NewHandler(e, uuidv7.Deterministic()))
	t.Cleanup(srv.Close)

	// Срок на весь разговор: тест, зависший на чтении сокета, обязан упасть
	// сроком, а не висеть до тайм-аута всего пакета.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)

	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/regions/ST_A/" + channel.Path
	ws, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatalf("сокет не открылся: %v", err)
	}
	t.Cleanup(func() { ws.CloseNow() })
	return &talk{t: t, e: e, ws: ws, doc: doc, ctx: ctx}
}

func (c *talk) send(raw string) {
	c.t.Helper()
	if err := c.ws.Write(c.ctx, websocket.MessageText, []byte(raw)); err != nil {
		c.t.Fatalf("отправка не прошла: %v", err)
	}
}

// read читает следующее сообщение сервера.
func (c *talk) read() []byte {
	c.t.Helper()
	typ, raw, err := c.ws.Read(c.ctx)
	if err != nil {
		c.t.Fatalf("чтение не прошло: %v", err)
	}
	if typ != websocket.MessageText {
		c.t.Fatalf("сервер прислал не текстовый кадр: %v", typ)
	}
	return raw
}

// validate сверяет значение с объявленным видом договора.
func (c *talk) validate(kind string, raw json.RawMessage) {
	c.t.Helper()
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		c.t.Fatalf("сообщение не разбирается: %v (%s)", err, raw)
	}
	if err := c.doc.Validate(kind, v); err != nil {
		c.t.Fatalf("провод разошёлся с договором (%s): %v\nтело: %s", kind, err, raw)
	}
}

type wireReply struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *wireError      `json:"error"`
}

type wireError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

type wireNotification struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
	// ID объявлен НАРОЧНО, хотя его быть не должно: строгий разбор иначе не
	// заметил бы уведомления с id — то есть запроса, переодетого в уведомление.
	ID json.RawMessage `json:"id"`
}

// decodeStrict — разбор с DisallowUnknownFields: лишнее поле в кадре роняет
// тест, а не доезжает до клиента незамеченным. Тот же приём, что у контракта
// сети (httpapi/contract_test.go).
func decodeStrict(t *testing.T, raw []byte, into any) {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		t.Fatalf("кадр не укладывается в объявленный контракт: %v\nтело: %s", err, raw)
	}
	if dec.More() {
		t.Fatalf("после кадра есть лишние данные: %s", raw)
	}
}

func (c *talk) hello(params string) wireReply {
	c.t.Helper()
	c.send(`{"jsonrpc":"2.0","id":1,"method":"hello","params":` + params + `}`)
	raw := c.read()
	var r wireReply
	decodeStrict(c.t, raw, &r)
	if r.JSONRPC != "2.0" {
		c.t.Fatalf("ответ не помечен версией JSON-RPC: %s", raw)
	}
	return r
}

// TestHelloResultMatchesContract — ответ рукопожатия против объявленного вида.
func TestHelloResultMatchesContract(t *testing.T) {
	c := dial(t)
	r := c.hello(`{"protocol_version":1}`)
	if r.Error != nil {
		t.Fatalf("рукопожатие отказало: %+v", r.Error)
	}
	c.validate(c.doc.Methods["hello"].Result, r.Result)
}

// TestSnapshotNotificationMatchesContract — снапшот сверху вниз.
//
// Проверяется и ФОРМА кадра (уведомление без id), и тело против конверта.
func TestSnapshotNotificationMatchesContract(t *testing.T) {
	c := dial(t)
	if r := c.hello(`{"protocol_version":1}`); r.Error != nil {
		t.Fatalf("рукопожатие отказало: %+v", r.Error)
	}
	// Мир двигают шагами, а не ожиданием: Run здесь не пущен нарочно — тест,
	// ждущий настенного времени, проверяет таймер, а не канал.
	steps := int(channel.MaxSnapshotAge / engine.TickDuration)
	for range steps {
		c.e.Step()
	}
	var n wireNotification
	decodeStrict(t, c.read(), &n)
	if n.Method != channel.SnapshotMethod {
		t.Fatalf("сверху вниз пришёл метод %q", n.Method)
	}
	if len(n.ID) != 0 {
		t.Fatalf("уведомление пришло с id %s — это запрос, а не уведомление", n.ID)
	}
	c.validate(c.doc.Notifications[channel.SnapshotMethod].Params, n.Params)

	// Время едет СТРОКОЙ микросекунд, и это правило провода, а не деталь:
	// договор объявляет вид int_string, а здесь проверяется, что за строкой
	// стоит именно модельное время прожитых тиков.
	var env struct {
		Time        units.SimTime `json:"time"`
		SnapshotSeq uint64        `json:"snapshot_seq"`
	}
	if err := json.Unmarshal(n.Params, &env); err != nil {
		t.Fatalf("конверт не разбирается: %v", err)
	}
	if want := units.SimTime(steps) * engine.TickDuration; env.Time != want {
		t.Fatalf("во времени снапшота %s, ожидалось %s", env.Time, want)
	}
	// Рукопожатие отдало первый снапшот, этот — второй.
	if env.SnapshotSeq != 2 {
		t.Fatalf("номер снапшота %d, ожидался 2", env.SnapshotSeq)
	}
}

// bump — правка партии из теста: доменных правок в этой вехе нет, а правило
// рассылки «состояние изменилось — шли немедленно» проверить без изменения
// состояния нечем.
type bump struct{}

func (bump) Name() string { return "bump" }

func (bump) Apply(g *match.Match) error {
	g.Units[0].At.U += 1.5
	return nil
}

// TestSnapshotGoesOnChangeBeforeMaxAge — главное правило рассылки.
//
// Снапшот обязан уйти В ТОТ ЖЕ ТИК, когда состояние изменилось, а не по
// расписанию: иначе управление ощущалось бы вязким ровно настолько, насколько
// редко идут снапшоты. Проверка сильная: до максимального возраста ещё девять
// тиков, и по расписанию сообщения быть не могло.
func TestSnapshotGoesOnChangeBeforeMaxAge(t *testing.T) {
	c := dial(t)
	if r := c.hello(`{"protocol_version":1}`); r.Error != nil {
		t.Fatalf("рукопожатие отказало: %+v", r.Error)
	}
	// Порядок обязателен: исход правки рождается В ТИКЕ, и ожидание его до
	// шага — это ожидание того, чего никто не сделает.
	done := c.e.Submit(bump{})
	c.e.Step()
	if err := <-done; err != nil {
		t.Fatalf("правка отказала: %v", err)
	}

	var n wireNotification
	decodeStrict(t, c.read(), &n)
	c.validate(c.doc.Notifications[channel.SnapshotMethod].Params, n.Params)
	var env struct {
		Time  units.SimTime `json:"time"`
		Units []struct {
			At struct {
				U float64 `json:"u"`
			} `json:"at"`
		} `json:"units"`
	}
	if err := json.Unmarshal(n.Params, &env); err != nil {
		t.Fatalf("конверт не разбирается: %v", err)
	}
	if env.Time >= channel.MaxSnapshotAge {
		t.Fatalf("снапшот пришёл в %s — не раньше максимального возраста %s",
			env.Time, channel.MaxSnapshotAge)
	}
	// Число сверяется С ДОПУСКОМ, а не байт в байт: правило проекта про float64
	// действует и в тесте, который его же и защищает.
	if got, want := env.Units[0].At.U, 151.5; got < want-1e-6 || got > want+1e-6 {
		t.Fatalf("в снапшоте u = %v, ожидалось %v", got, want)
	}
}

// TestRefusalMatchesContract — отказ тоже часть договора: клиент разбирает
// data машинно, и форма причины обязана быть объявлена наравне с успехом.
func TestRefusalMatchesContract(t *testing.T) {
	c := dial(t)
	r := c.hello(`{"protocol_version":9000}`)
	if r.Error == nil {
		t.Fatal("чужая major версия принята")
	}
	if r.Error.Code != c.doc.Errors["refused"] {
		t.Fatalf("код отказа %d, договор объявляет %d", r.Error.Code, c.doc.Errors["refused"])
	}
	c.validate(c.doc.RefusalData, r.Error.Data)
	var ref protocol.Refusal
	if err := json.Unmarshal(r.Error.Data, &ref); err != nil {
		t.Fatalf("причина отказа не разбирается: %v", err)
	}
	if !slicesContains(c.doc.RefusalReasons, ref.Reason) {
		t.Fatalf("причина %q не объявлена в договоре: %v", ref.Reason, c.doc.RefusalReasons)
	}
}

// TestServerConstantsMatchContract — САМА СВЕРКА ДОГОВОРА С КОДОМ.
//
// Без неё договор — это файл рядом с кодом, который расходится с ним молча:
// сервер поменял код отказа, тесты провода прошли (они читают код из того же
// сервера), а клиент, читающий договор, остался с прежним числом.
func TestServerConstantsMatchContract(t *testing.T) {
	doc, err := contract.Load(contractPath())
	if err != nil {
		t.Fatalf("договор не читается: %v", err)
	}
	if doc.ProtocolVersion != protocol.ProtocolVersion {
		t.Fatalf("договор объявляет версию %d, сервер — %d", doc.ProtocolVersion, protocol.ProtocolVersion)
	}
	codes := map[string]int{
		"parse":            rpc.CodeParse,
		"invalid_request":  rpc.CodeInvalidRequest,
		"method_not_found": rpc.CodeMethodNotFound,
		"invalid_params":   rpc.CodeInvalidParams,
		"internal":         rpc.CodeInternal,
		"refused":          rpc.CodeRefused,
	}
	for name, want := range codes {
		if got, ok := doc.Errors[name]; !ok || got != want {
			t.Fatalf("договор объявляет код %s = %d (есть: %v), сервер — %d", name, got, ok, want)
		}
	}
	reasons := []string{
		protocol.ReasonUnsupportedProtocol,
		protocol.ReasonUnknownSession,
		protocol.ReasonNotGreeted,
		protocol.ReasonAlreadyGreeted,
	}
	for _, r := range reasons {
		if !slicesContains(doc.RefusalReasons, r) {
			t.Fatalf("причина отказа %q объявлена в сервере, но не в договоре", r)
		}
	}
	if len(doc.RefusalReasons) != len(reasons) {
		t.Fatalf("в договоре %d причин, в сервере %d — договор ушёл вперёд кода",
			len(doc.RefusalReasons), len(reasons))
	}
	if doc.Path != "/regions/{region}/"+channel.Path {
		t.Fatalf("договор объявляет адрес %q", doc.Path)
	}
}

// TestWrongRegionIsNotFound — адрес несуществующего региона обязан отвечать
// 404 ДО апгрейда: канал не открывается на то, чего нет.
func TestWrongRegionIsNotFound(t *testing.T) {
	e := engine.New(testMatch())
	srv := httptest.NewServer(channel.NewHandler(e, uuidv7.Deterministic()))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, resp, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/regions/ЧУЖОЙ/"+channel.Path, nil)
	if err == nil {
		t.Fatal("сокет открылся на чужом регионе")
	}
	if resp == nil || resp.StatusCode != http.StatusNotFound {
		t.Fatalf("ответ на чужой регион: %v", resp)
	}
}

func slicesContains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
