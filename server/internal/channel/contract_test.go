package channel_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/shady2k/ClearAhead/server/internal/channel"
	"github.com/shady2k/ClearAhead/server/internal/content"
	"github.com/shady2k/ClearAhead/server/internal/contract"
	"github.com/shady2k/ClearAhead/server/internal/engine"
	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/match"
	"github.com/shady2k/ClearAhead/server/internal/netloc"
	"github.com/shady2k/ClearAhead/server/internal/protocol"
	"github.com/shady2k/ClearAhead/server/internal/rpc"
	"github.com/shady2k/ClearAhead/server/internal/seedmap"
	"github.com/shady2k/ClearAhead/server/internal/track"
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

const loco1ID = "01a3185c-6001-7242-8242-000000424242"

func testMatch(t *testing.T) *match.Match {
	t.Helper()
	net, set := station(t), shippedSet(t)
	m := &match.Match{ID: "M1", Region: "ST_A", Units: []match.Unit{{
		ID: loco1ID, Name: "LOCO_1", Type: "VL80",
		At: netloc.PointU{Element: seedmap.StationMain, U: 150, Direction: netloc.DirForward},
	}},
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

// shippedSet — БОЕВОЙ набор контента, а не фикстура.
//
// Договор проверяется о то, что игрок получит на самом деле: пределы ступеней
// приезжают клиенту из паспорта, и фикстура со своими числами доказывала бы
// согласие теста с тестом. Побочно это проверяет, что боевой content.json
// вообще собирается — включая блок controls, добавленный вместе с командой.
func shippedSet(t *testing.T) *content.Set {
	t.Helper()
	set, err := content.Load(filepath.Join("..", "..", "assets"))
	if err != nil {
		t.Fatalf("боевой набор контента не читается: %v", err)
	}
	return set
}

// talk — поднятый сервер и открытый сокет.
type talk struct {
	t   *testing.T
	e   *engine.Engine
	ws  *websocket.Conn
	doc contract.Doc
	ctx context.Context
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

func dial(t *testing.T) *talk { return dialMatch(t, testMatch(t)) }

// dialMatch — то же соединение, но с ЗАДАННОЙ партией: занятость стрелки
// проверяется на машине, стоящей на устройстве, а фикстура по умолчанию ставит
// её на главный путь.
func dialMatch(t *testing.T, m *match.Match) *talk {
	t.Helper()
	doc, err := contract.Load(contractPath())
	if err != nil {
		t.Fatalf("договор не читается: %v", err)
	}
	e := engine.New(m, nil)
	srv := httptest.NewServer(channel.NewHandler(e, uuidv7.Deterministic(), shippedSet(t), station(t)))
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

// bumpS — на сколько поддельная правка двигает машину, микрометры.
//
// ДВИГАЕТСЯ ОТРЕЗОК ПУТИ, А НЕ ЗАПИСЬ РАССТАНОВКИ. Прежняя редакция правила
// g.Units[0].At.U — то есть ДОКУМЕНТ, — и это перестало быть движением в тот
// день, когда у машины появилось состояние физики: на провод уезжает положение
// ИЗ СОСТОЯНИЯ, а документ говорит лишь, где машину поставили. Правка документа
// меняла хеш и не меняла картинку — то есть проверяла рассылку и не проверяла,
// что доехало.
const bumpS = units.Distance(1_500_000)

func (bump) Apply(g *match.Match) error {
	mo, ok := g.MotionOf(loco1ID)
	if !ok {
		return fmt.Errorf("у машины %s нет состояния физики", loco1ID)
	}
	sp := append(track.Span(nil), mo.Span...)
	for i := range sp {
		sp[i].From += bumpS
		sp[i].To += bumpS
	}
	if err := mo.SetSpan(sp); err != nil {
		return err
	}
	g.SetMotion(loco1ID, mo)
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
	//
	// ОЖИДАЕМОЕ ВЫВЕДЕНО, А НЕ ЗАПИСАНО ЧИСЛОМ: правка двигает машину в s, а на
	// провод уезжает u, и совпадают они только на элементе без уклона. Фикстура
	// сегодня плоская — но записать 151.5 значило бы спрятать это условие в
	// константу, которая переживёт первый же уклон в фикстуре и соврёт.
	want := 150.0 + bumpS.Meters()
	if got := env.Units[0].At.U; got < want-1e-6 || got > want+1e-6 {
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
		// Причины первой доменной команды (ClearAhead-6ygr).
		protocol.ReasonUnknownUnit,
		protocol.ReasonNoControls,
		protocol.ReasonNotchOutOfRange,
		protocol.ReasonUnknownReverser,
		protocol.ReasonUnknownHandle,
		protocol.ReasonTractionWithoutReverser,
		// Причины команды перевода стрелки (ClearAhead-duf).
		protocol.ReasonUnknownTurnout,
		protocol.ReasonUnknownTurnoutPosition,
		protocol.ReasonTurnoutOccupied,
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
	// Команда объявлена договором ровно под тем именем, под которым
	// зарегистрирована: разойдись они, и клиент звал бы метод, которого нет.
	if _, ok := doc.Methods[channel.MethodSetControls]; !ok {
		t.Fatalf("договор не объявляет команду %s: %v", channel.MethodSetControls, doc.Methods)
	}
	if _, ok := doc.Methods[channel.MethodSetTurnout]; !ok {
		t.Fatalf("договор не объявляет команду %s: %v", channel.MethodSetTurnout, doc.Methods)
	}
}

// TestWrongRegionIsNotFound — адрес несуществующего региона обязан отвечать
// 404 ДО апгрейда: канал не открывается на то, чего нет.
func TestWrongRegionIsNotFound(t *testing.T) {
	e := engine.New(testMatch(t), nil)
	srv := httptest.NewServer(channel.NewHandler(e, uuidv7.Deterministic(), shippedSet(t), station(t)))
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

// readReplyAndSnapshot читает кадры, пока не увидит ОБА: ответ на свой запрос и
// уведомление о снапшоте.
//
// ПОРЯДОК МЕЖДУ НИМИ НЕ ОПРЕДЕЛЁН, и это не небрежность сервера, а его
// устройство: ответ пишет горутина соединения, снапшот — горутина рассылки, и
// оба рождаются в одном тике — том, где правка применилась. Тест, требующий
// определённого порядка, проверял бы удачу планировщика; клиенту порядок тоже не
// нужен — ответ он находит по id, снапшот по отсутствию id.
func readReplyAndSnapshot(c *talk) (wireReply, wireNotification) {
	c.t.Helper()
	var reply wireReply
	var note wireNotification
	var gotReply, gotNote bool
	for !gotReply || !gotNote {
		raw := c.read()
		var probe struct {
			ID json.RawMessage `json:"id"`
		}
		if err := json.Unmarshal(raw, &probe); err != nil {
			c.t.Fatalf("кадр не разбирается: %v (%s)", err, raw)
		}
		if len(probe.ID) != 0 {
			decodeStrict(c.t, raw, &reply)
			gotReply = true
			continue
		}
		decodeStrict(c.t, raw, &note)
		gotNote = true
	}
	return reply, note
}

// TestControlsCommandOverSocketMatchesContract — ПЕРВАЯ ДОМЕННАЯ КОМАНДА через
// настоящий сокет, от кадра до снапшота.
//
// Проверяется вся дорога разом, потому что порознь она уже проверена, а вместе —
// нет: команда уходит кадром, применяется на границе тика, ответ несёт вставшее
// положение, а следом САМ приходит снапшот — потому что состояние изменилось, а
// не потому, что настал срок биения.
func TestControlsCommandOverSocketMatchesContract(t *testing.T) {
	c := dial(t)
	if r := c.hello(`{"protocol_version":1}`); r.Error != nil {
		t.Fatalf("рукопожатие отказало: %+v", r.Error)
	}
	// Движок крутится своей горутиной: команда применяется на границе тика, а
	// тик здесь никто больше не даёт (Run не пущен — он читает настенные часы).
	stop := make(chan struct{})
	ticked := make(chan struct{})
	go func() {
		defer close(ticked)
		for {
			select {
			case <-stop:
				return
			default:
				c.e.Step()
				time.Sleep(2 * time.Millisecond)
			}
		}
	}()
	defer func() { close(stop); <-ticked }()

	c.send(`{"jsonrpc":"2.0","id":2,"method":"` + channel.MethodSetControls +
		`","params":{"command_id":"c1","unit":"` + loco1ID +
		`","traction":7,"brake":0,"reverser":"forward",` +
		// КРАН МАШИНИСТА В КОМАНДЕ ОБЯЗАТЕЛЕН у машины с магистралью: команда
		// ставит положение ВСЕХ органов разом, и опустить ручку значило бы
		// сделать «не трогать» неотличимым от «поставить». Отказ на пустом
		// положении проверяется отдельно (match).
		`"handle":"run","independent":"0"}}`)

	r, n := readReplyAndSnapshot(c)
	if r.Error != nil {
		t.Fatalf("команда отказала: %+v", r.Error)
	}
	c.validate(c.doc.Methods[channel.MethodSetControls].Result, r.Result)

	// СНАПШОТ ПРИХОДИТ ОТ КОМАНДЫ, а не по расписанию: биение идёт раз в
	// секунду модельного времени, а этот обязан прийти в тик применения.
	if n.Method != channel.SnapshotMethod {
		t.Fatalf("после команды пришло %q", n.Method)
	}
	c.validate(c.doc.Notifications[channel.SnapshotMethod].Params, n.Params)
	var env struct {
		Time  units.SimTime `json:"time"`
		Units []struct {
			Controls *struct {
				Traction int    `json:"traction"`
				Brake    int    `json:"brake"`
				Reverser string `json:"reverser"`
				Handle   string `json:"handle"`
			} `json:"controls"`
			// Air — давления пневматики. Указатель, потому что у машины без
			// магистрали блока нет вовсе, и это законно.
			Air *struct {
				Main     string `json:"main"`
				Pipe     string `json:"pipe"`
				Cylinder string `json:"cylinder"`
			} `json:"air"`
		} `json:"units"`
	}
	if err := json.Unmarshal(n.Params, &env); err != nil {
		t.Fatalf("конверт не разбирается: %v", err)
	}
	if env.Time >= channel.MaxSnapshotAge {
		t.Fatalf("снапшот пришёл в %s — не раньше срока биения %s", env.Time, channel.MaxSnapshotAge)
	}
	if len(env.Units) != 1 || env.Units[0].Controls == nil {
		t.Fatalf("в снапшоте нет органов управления: %s", n.Params)
	}
	got := *env.Units[0].Controls
	if got.Traction != 7 || got.Brake != 0 || got.Reverser != "forward" {
		t.Fatalf("в снапшоте органы %+v, ожидалась ступень 7 вперёд", got)
	}
}

// TestControlsRefusalMatchesContract — отказ доменной команды разбирается
// машинно и объявлен договором.
func TestControlsRefusalMatchesContract(t *testing.T) {
	c := dial(t)
	if r := c.hello(`{"protocol_version":1}`); r.Error != nil {
		t.Fatalf("рукопожатие отказало: %+v", r.Error)
	}
	stop := make(chan struct{})
	ticked := make(chan struct{})
	go func() {
		defer close(ticked)
		for {
			select {
			case <-stop:
				return
			default:
				c.e.Step()
				time.Sleep(2 * time.Millisecond)
			}
		}
	}()
	defer func() { close(stop); <-ticked }()

	// Ступень заведомо за пределом паспорта: у ВЛ80 их 33.
	c.send(`{"jsonrpc":"2.0","id":2,"method":"` + channel.MethodSetControls +
		`","params":{"unit":"` + loco1ID + `","traction":100,"brake":0,"reverser":"forward"}}`)
	var r wireReply
	decodeStrict(t, c.read(), &r)
	if r.Error == nil {
		t.Fatalf("ступень за пределом принята: %s", r.Result)
	}
	if r.Error.Code != c.doc.Errors["refused"] {
		t.Fatalf("код отказа %d, договор объявляет %d", r.Error.Code, c.doc.Errors["refused"])
	}
	c.validate(c.doc.RefusalData, r.Error.Data)
	var ref protocol.Refusal
	if err := json.Unmarshal(r.Error.Data, &ref); err != nil {
		t.Fatalf("причина не разбирается: %v", err)
	}
	if ref.Reason != protocol.ReasonNotchOutOfRange {
		t.Fatalf("причина %q, ожидалась %q", ref.Reason, protocol.ReasonNotchOutOfRange)
	}
	if !slicesContains(c.doc.RefusalReasons, ref.Reason) {
		t.Fatalf("причина %q не объявлена в договоре", ref.Reason)
	}
	// Отказ НЕ ПОРОЖДАЕТ СНАПШОТА: состояние не изменилось, и хеш тот же.
	// Проверяется тем, что число ступени в партии осталось нулевым.
	if got, _ := c.e.Snapshot().Match.ControlsOf(loco1ID); got != match.Stopped() {
		t.Fatalf("после отказа в партии %+v", got)
	}
}

// TestTurnoutCommandOverSocketMatchesContract — ВТОРАЯ ДОМЕННАЯ КОМАНДА через
// настоящий сокет: перевод стрелки от кадра до снапшота.
//
// Проверяется то же, что у кабины, и ровно потому же: порознь дорога проверена,
// вместе — нет. Плюс одно своё: положение стрелки обязано попасть в снапшот
// НЕМЕДЛЕННО, а не секундным биением. Это четвёртый по счёту случай, когда
// состояние забывали положить в канонический хеш (engine.StateHash), и здесь он
// закрыт замком, а не памятью.
func TestTurnoutCommandOverSocketMatchesContract(t *testing.T) {
	c := dial(t)
	if r := c.hello(`{"protocol_version":1}`); r.Error != nil {
		t.Fatalf("рукопожатие отказало: %+v", r.Error)
	}
	stop := make(chan struct{})
	ticked := make(chan struct{})
	go func() {
		defer close(ticked)
		for {
			select {
			case <-stop:
				return
			default:
				c.e.Step()
				time.Sleep(2 * time.Millisecond)
			}
		}
	}()
	defer func() { close(stop); <-ticked }()

	c.send(`{"jsonrpc":"2.0","id":2,"method":"` + channel.MethodSetTurnout +
		`","params":{"command_id":"t1","turnout":"` + seedmap.StationSW1 +
		`","position":"diverging"}}`)

	r, n := readReplyAndSnapshot(c)
	if r.Error != nil {
		t.Fatalf("команда отказала: %+v", r.Error)
	}
	c.validate(c.doc.Methods[channel.MethodSetTurnout].Result, r.Result)
	if n.Method != channel.SnapshotMethod {
		t.Fatalf("после команды пришло %q", n.Method)
	}
	c.validate(c.doc.Notifications[channel.SnapshotMethod].Params, n.Params)
	var env struct {
		Time     units.SimTime `json:"time"`
		Turnouts []struct {
			ID       string  `json:"id"`
			Name     string  `json:"name"`
			Position string  `json:"position"`
			Moving   bool    `json:"moving"`
			To       string  `json:"to"`
			Progress float64 `json:"progress"`
			Drive    string  `json:"drive"`
		} `json:"turnouts"`
	}
	if err := json.Unmarshal(n.Params, &env); err != nil {
		t.Fatalf("конверт не разбирается: %v", err)
	}
	if env.Time >= channel.MaxSnapshotAge {
		t.Fatalf("снапшот пришёл в %s — не раньше срока биения %s", env.Time, channel.MaxSnapshotAge)
	}
	// Стрелок в конверте столько же, сколько на станции: список полный, а не
	// «те, кого трогали».
	if len(env.Turnouts) != 2 {
		t.Fatalf("в снапшоте %d стрелок, на станции 2: %s", len(env.Turnouts), n.Params)
	}
	var moved, other string
	var pos string
	drives := map[string]string{}
	for _, sw := range env.Turnouts {
		drives[sw.ID] = sw.Drive
		if sw.ID == seedmap.StationSW1 {
			pos = sw.Position
			if sw.Moving {
				moved = sw.To
			}
		} else {
			other = sw.Position
		}
	}
	// ПОСЛЕ КОМАНДЫ ОСТРЯК ИДЁТ, А НЕ СТОИТ. С 2026-08-16 перевод — процесс
	// (слово владельца: «стрелка, когда переключается, это не должна делать
	// резко»), и снапшот обязан показать ХОД: положения нет, зато названа цель.
	//
	// Проверяется здесь именно НЕМЕДЛЕННОСТЬ: до этой правки в хеш состояния
	// клали положение, а идущий остряк туда не попадал — и снапшоты во время
	// хода шли бы только секундным биением. Это ПЯТЫЙ случай того же рода.
	if moved != "diverging" {
		t.Fatalf("стрелка идёт в %q, а команда просила diverging", moved)
	}
	if pos != "" {
		t.Fatalf("стрелка в переводе стоит %q — идущий остряк не стоит нигде", pos)
	}
	if other != "straight" {
		t.Fatalf("нетронутая стрелка стоит %q — команда задела чужой остряк", other)
	}
	// Механизм едет вместе с положением: пульт узнаёт вид стрелки из снапшота, а
	// не вторым запросом за геометрией.
	if drives[seedmap.StationSW1] != mapfmt.DriveManual || drives[seedmap.StationSW2] != mapfmt.DriveElectric {
		t.Fatalf("механизмы приехали как %v, а на станции ручная SW1 и электрическая SW2", drives)
	}
}

// TestTurnoutUnderTrainIsRefused — под составом стрелка не переводится, и отказ
// называет ДЕРЖАТЕЛЯ.
//
// Локомотив ставится точкой отсчёта на прямой проход SW1 — то есть на само
// устройство. Отказ обязан прийти доменной причиной, а не внутренней ошибкой:
// клиент показывает игроку, кто держит стрелку, и держатель для этого едет в
// held_by.
func TestTurnoutUnderTrainIsRefused(t *testing.T) {
	m := testMatch(t)
	// ЕДИНИЦА КЛАДЁТСЯ НА УСТРОЙСТВО ОТРЕЗКОМ, а не точкой, и целиком она туда
	// не помещается: прямой проход SW1 длиной 33.5 м против машины в 34.18 м.
	// Это не неудобство фикстуры, а сам случай — тело на стрелке ВСЕГДА лежит и
	// на соседнем элементе, и занятость обязана видеть его так же.
	//
	// Отрезок собирается руками: довезти сюда машину физикой значило бы поднять
	// в договорном тесте ещё и мир движения. Связность при этом объявлена, а не
	// выдумана — проход SW1 выходит портом SW1.S, тем же, которым входит главный
	// путь.
	passage := seedmap.StationSW1 + mapfmt.PassageStraight
	m.Units[0].At = netloc.PointU{Element: passage, U: 10, Direction: netloc.DirForward}
	net := station(t)
	span := track.Span{
		{Element: passage, From: 10 * units.Meter, To: net.Elements[passage].LengthS,
			Direction: netloc.DirForward},
		{Element: seedmap.StationMain, From: 0,
			To:        34*units.Meter + 180*units.Millimeter - (net.Elements[passage].LengthS - 10*units.Meter),
			Direction: netloc.DirForward},
	}
	if err := span.Connected(net); err != nil {
		t.Fatalf("фикстурный отрезок несвязен: %v", err)
	}
	var mo match.Motion
	if err := mo.SetSpan(span); err != nil {
		t.Fatalf("фикстурное состояние: %v", err)
	}
	// ЧЕРЕЗ SetMotion, а не записью в карту: обратный индекс занятости
	// перекладывает он, и фикстура, писавшая мимо, оставила бы стрелку свободной
	// — то есть проверка «под составом не переводится» прошла бы, ничего не
	// проверив.
	m.SetMotion(loco1ID, mo)
	c := dialMatch(t, m)
	if r := c.hello(`{"protocol_version":1}`); r.Error != nil {
		t.Fatalf("рукопожатие отказало: %+v", r.Error)
	}
	stop := make(chan struct{})
	ticked := make(chan struct{})
	go func() {
		defer close(ticked)
		for {
			select {
			case <-stop:
				return
			default:
				c.e.Step()
				time.Sleep(2 * time.Millisecond)
			}
		}
	}()
	defer func() { close(stop); <-ticked }()

	c.send(`{"jsonrpc":"2.0","id":2,"method":"` + channel.MethodSetTurnout +
		`","params":{"command_id":"t2","turnout":"` + seedmap.StationSW1 +
		`","position":"diverging"}}`)
	var r wireReply
	decodeStrict(t, c.read(), &r)
	if r.Error == nil {
		t.Fatalf("стрелка под составом переведена: %s", r.Result)
	}
	var ref struct {
		Reason string `json:"reason"`
		HeldBy string `json:"held_by"`
	}
	if err := json.Unmarshal(r.Error.Data, &ref); err != nil {
		t.Fatalf("причина отказа не разбирается: %v (%s)", err, r.Error.Data)
	}
	if ref.Reason != protocol.ReasonTurnoutOccupied {
		t.Fatalf("причина отказа %q, ожидалась %q", ref.Reason, protocol.ReasonTurnoutOccupied)
	}
	if ref.HeldBy != loco1ID {
		t.Fatalf("держатель %q, а стрелку занимает %q", ref.HeldBy, loco1ID)
	}
}
