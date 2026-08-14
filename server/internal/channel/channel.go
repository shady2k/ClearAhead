package channel

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/coder/websocket"

	"github.com/shady2k/ClearAhead/server/internal/engine"
	"github.com/shady2k/ClearAhead/server/internal/match"
	"github.com/shady2k/ClearAhead/server/internal/protocol"
	"github.com/shady2k/ClearAhead/server/internal/rpc"
	"github.com/shady2k/ClearAhead/server/internal/units"
	"github.com/shady2k/ClearAhead/server/internal/uuidv7"
)

// Библиотека сокета: github.com/coder/websocket.
//
// ВТОРАЯ ЗАВИСИМОСТЬ ПРОЕКТА, и принимается она так же сознательно, как первая
// (modernc.org/sqlite, разбор — world-storage §5). Требование к ней ровно одно
// и оно же было у драйвера базы: не сломать обещание «сервер — один бинарник на
// машине игрока». Чистый Go, cgo нет, СВОИХ зависимостей ноль — кросс-сборка
// цела.
//
// ОТВЕРГНУТО:
//
//	golang.org/x/net/websocket — авторы сами объявили пакет ограниченно
//	  сопровождаемым и рекомендуют другие; у него нет контроля над ping/pong и
//	  над закрытием, а закрытие нам нужно явное: клиенту при отказе версии надо
//	  сказать причину, а не оборвать сокет.
//	СВОЙ КАДРОВЩИК RFC 6455 — порядка трёхсот строк своего кода на маскирование,
//	  сборку фрагментов, ping/pong и коды закрытия плюс свои тесты на всё это.
//	  Проект уже отвергал прикладной кодек (RLE в контракте чанков) доводом
//	  «общее решение делает это лучше»; здесь тот же довод, и он зеркально
//	  указывает на библиотеку.
//
// Контекстный API лёг на форму движка как есть: у соединения жизнь ограничена
// контекстом запроса, у рассылки — тем же контекстом, отмена одна на обоих.

// MaxIncomingFrameBytes — потолок кадра, приходящего в сокет.
//
// 64 КиБ с запасом покрывают любую команду: у команды несколько полей, и самая
// большая величина в ней — идентификатор. Потолок нужен не ради памяти одного
// кадра, а потому, что без него клиент вправе прислать гигабайт, и сервер
// обязан будет его прочитать, прежде чем узнает, что это не команда.
const MaxIncomingFrameBytes = 1 << 16

// MaxSnapshotAge — максимальный ВОЗРАСТ снапшота в МОДЕЛЬНОМ времени.
//
// Правило рассылки: снапшот идёт, когда изменился канонический хеш состояния
// ЛИБО когда с прошлой отправки прошло больше этого времени. Это приём
// IEEE 1278 (DIS) — «обновление по порогу ошибки плюс максимальный возраст», —
// записанный решением ClearAhead-t5h; сегодня порог ошибки вырожден в «состояние
// стало другим», потому что ничто не движется.
//
// Отказ от «слать каждый тик» — это отказ гонять 10 одинаковых сообщений в
// секунду по неподвижному миру. Отказ от «слать только при изменении» — это
// биение: клиент, не получающий ничего, не отличает неподвижный мир от
// умершего сервера.
//
// Время МОДЕЛЬНОЕ, а не настенное, нарочно: иначе в канале появились бы вторые
// часы, и правило рассылки начало бы зависеть от того, поспевает ли сервер за
// настенным временем. Мир, отставший вдвое, обязан слать биение вдвое реже —
// он и вправду прожил вдвое меньше.
const MaxSnapshotAge = 1 * units.Second

// SnapshotMethod — имя уведомления сверху вниз.
const SnapshotMethod = "snapshot"

// Path — хвост адреса канала: /regions/{region}/channel.
const Path = "channel"

// Handler — ручка канала.
type Handler struct {
	e   *engine.Engine
	reg *registry
}

// NewHandler собирает ручку канала.
//
// Принимает движок, а не партию, по той же причине, что и ручка живого
// состояния: партию у движка нельзя взять иначе как снимком, и тот, кому дали
// бы её напрямую, имел бы способ прочитать состояние мимо замка.
//
// Источник идентификаторов приходит ПАРАМЕТРОМ — правило пакета uuidv7:
// генератор обязан быть внедряемым, иначе тест не получит предсказуемых
// session_id и вынужден будет проверять «строка непустая» вместо равенства.
func NewHandler(e *engine.Engine, ids uuidv7.Source) *Handler {
	return &Handler{e: e, reg: newRegistry(ids)}
}

// connState — то, что принадлежит ОДНОМУ соединению.
//
// Сессия здесь указателем и заполняется рукопожатием: до него соединение не
// имеет ни имени, ни актёра, и команды от него не принимаются вовсе.
type connState struct {
	ws *websocket.Conn
	// writeMu сериализует запись. У сокета одновременный писатель обязан быть
	// один, а пишут двое: горутина рассылки и горутина чтения (ответами).
	writeMu sync.Mutex
	session *session
	mux     *rpc.Mux
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) != 3 || parts[0] != "regions" || parts[2] != Path {
		http.NotFound(w, r)
		return
	}
	// Регион сверяется ДО рукопожатия и отвечает 404, а не отказом в канале:
	// адрес несуществующего ресурса — это адрес, а не команда. Клиент, попавший
	// не в тот регион, должен узнать это тем же способом, что и на любом другом
	// адресе сервера.
	snap := h.e.Snapshot()
	if parts[1] != snap.Match.Region {
		http.NotFound(w, r)
		return
	}
	// Апгрейд — это GET. Метод проверяется до Accept, чтобы POST на канал
	// получил внятное 405, а не ошибку рукопожатия сокета.
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "канал открывается апгрейдом GET", http.StatusMethodNotAllowed)
		return
	}

	// СРОКИ HTTP-СЕРВЕРА ЗДЕСЬ НЕ СНИМАЮТСЯ, и это проверено, а не предположено.
	//
	// main.go ставит ReadTimeout 15 с и WriteTimeout 60 с — числа, правильные
	// для выдачи документов и смертельные для сокета, который молчит часами.
	// Напрашивается снять их через http.NewResponseController до апгрейда, и
	// такой код здесь стоял.
	//
	// ЗАМЕР ЕГО ОТМЕНИЛ (2026-08-14, живой сервер, клиент молчит после
	// рукопожатия): сокет жив 25.9 с и получает 27 биений — ОДИНАКОВО со
	// снятием сроков и без него. Причина в net/http: hijackLocked сам делает
	// rwc.SetDeadline(time.Time{}) при перехвате (go/src/net/http/server.go).
	// Библиотека сокета своих сроков не ставит вовсе — в conn.go, read.go и
	// write.go нет ни одного вызова SetDeadline.
	//
	// Оставленный код был бы не страховкой, а ложью о том, что он что-то
	// держит. Запись оставлена, чтобы его не завели снова: вопрос законный,
	// ответ на него — ссылка на строку в net/http и два одинаковых замера.
	//
	// Мера жизни у канала своя и не зависит от сроков: клиент, замолчавший
	// навсегда, обнаруживается неудачной отправкой снапшота — биение идёт не
	// реже раза в секунду модельного времени.

	// Accept по умолчанию НЕ разрешает межисточниковые запросы: браузер с чужой
	// страницы получит отказ. Клиенту на Godot это не мешает — WebSocketPeer
	// заголовка Origin не шлёт, а проверка срабатывает только когда он есть.
	// Ослаблять её нечем: браузерного клиента у проекта нет.
	ws, err := websocket.Accept(w, r, nil)
	if err != nil {
		// Accept уже ответил клиенту сам. Своего ответа здесь быть не может:
		// заголовки отправлены.
		return
	}
	// CloseNow — аварийный путь: обычное закрытие делает serve, отдав причину.
	defer ws.CloseNow()
	ws.SetReadLimit(MaxIncomingFrameBytes)

	// Контекст соединения СВОЙ, а не r.Context() напрямую: рассылка живёт в
	// отдельной горутине, и её надо погасить в тот же миг, когда чтение
	// закончилось, — иначе она переживёт соединение и будет писать в закрытый
	// сокет.
	ctx, stop := context.WithCancel(r.Context())
	defer stop()

	st := &connState{ws: ws}
	st.mux = h.newMux(st)
	h.serve(ctx, st)
}

// newMux — реестр методов ЭТОГО соединения.
//
// Реестр на соединение, а не один на сервер, потому что обработчик рукопожатия
// обязан положить сессию в состояние ИМЕННО ЭТОГО соединения. Общий реестр
// потребовал бы протаскивать соединение через контекст значением — то есть
// прятать обязательный аргумент в необязательное место.
//
// Барьер валидации от этого не слабеет: Register по-прежнему принимает только
// запечатанные protocol.Request, и сырой вход до обработчика не доходит.
func (h *Handler) newMux(st *connState) *rpc.Mux {
	m := rpc.NewMux()
	rpc.Register[protocol.HelloRequest](m, "hello",
		func(_ context.Context, req protocol.HelloRequest) (any, error) {
			return h.hello(st, req)
		})
	// Доменных команд здесь нет и в этой вехе не будет: см. шапку пакета.
	return m
}

// serve — цикл чтения соединения.
//
// Рукопожатие требуется ПЕРВЫМ кадром, и до него не рассылается ничего. Иначе
// сервер начал бы слать снапшоты, не зная, той ли он версии, что клиент, — то
// есть отправлять данные тому, кто их гарантированно не разберёт.
func (h *Handler) serve(ctx context.Context, st *connState) {
	var publishing bool
	for {
		typ, raw, err := st.ws.Read(ctx)
		if err != nil {
			// Разрыв, отмена, закрытие клиентом — всё это обычный конец
			// соединения, а не событие, о котором стоит говорить в лог:
			// сессия переживает разрыв, и клиент вернётся.
			return
		}
		if typ != websocket.MessageText {
			st.ws.Close(websocket.StatusUnsupportedData,
				"кадры канала текстовые: JSON-RPC 2.0")
			return
		}
		reply := h.request(ctx, st, raw)
		if reply != nil && !st.write(ctx, reply) {
			return
		}
		// Рассылка пускается ПОСЛЕ первого удачного рукопожатия и ровно один
		// раз. Проверка «сессия появилась» — это и есть признак удачи:
		// отказ рукопожатия её не ставит.
		if !publishing && st.session != nil {
			publishing = true
			go h.publish(ctx, st)
		}
	}
}

// request обслуживает один кадр и возвращает ответ (или nil, если ответа нет).
func (h *Handler) request(ctx context.Context, st *connState, raw []byte) []byte {
	f, ferr := rpc.ParseFrame(raw)
	if ferr != nil {
		// id неизвестен — по стандарту в ответе null. Клиент не соотнесёт этот
		// отказ ни с одним своим запросом, и это честно: кадр, который не
		// разобрался, не является запросом.
		return rpc.EncodeError(nil, ferr)
	}
	// ИДЕМПОТЕНТНОСТЬ ДО ВЫЗОВА, а не после: повтор обязан не применяться, а не
	// применяться и отбрасываться. Ответ отдаётся тот же самый, байт в байт,
	// включая id ПЕРВОГО запроса — id корреляционный, и подменять его в
	// запомненном ответе значило бы хранить не ответ, а его пересказ.
	if st.session != nil {
		if cached, ok := st.session.cachedReply(f.CommandID); ok {
			return cached
		}
	}
	if st.session == nil && f.Method != "hello" {
		return rpc.EncodeError(f.ID, &rpc.Error{Code: rpc.CodeRefused,
			Message: "первым кадром обязано быть рукопожатие hello",
			Data:    &protocol.Refusal{Reason: protocol.ReasonNotGreeted}})
	}

	result, err := st.mux.Dispatch(ctx, f.Method, protocol.Input{Body: f.Params})
	var reply []byte
	if err != nil {
		reply = rpc.EncodeError(f.ID, toError(err))
	} else {
		body, mErr := rpc.EncodeResult(f.ID, result)
		if mErr != nil {
			// Ответ обработчика не сворачивается в JSON — это дефект сервера,
			// и клиенту о нём говорится внутренней ошибкой, а не молчанием.
			reply = rpc.EncodeError(f.ID, &rpc.Error{Code: rpc.CodeInternal,
				Message: fmt.Sprintf("ответ метода %s не сериализуется", f.Method)})
		} else {
			reply = body
		}
	}
	if st.session != nil {
		st.session.remember(f.CommandID, reply)
	}
	return reply
}

// toError переводит ошибку обработчика в отказ провода.
//
// Три случая и ни одного «прочего»: доменный отказ едет машинной причиной,
// ошибки барьера (неизвестный метод, невалидные params) — своими кодами
// стандарта, всё остальное честно называется внутренней ошибкой. Смешать их
// значило бы отдать клиенту -32602 на поломку сервера и заставить его чинить
// свой запрос, который был верен.
func toError(err error) *rpc.Error {
	var ref *protocol.Refusal
	if errors.As(err, &ref) {
		return &rpc.Error{Code: rpc.CodeRefused, Message: ref.Error(), Data: ref}
	}
	switch {
	case errors.Is(err, rpc.ErrUnknownMethod):
		return &rpc.Error{Code: rpc.CodeMethodNotFound, Message: err.Error()}
	case errors.Is(err, rpc.ErrInvalidParams):
		return &rpc.Error{Code: rpc.CodeInvalidParams, Message: err.Error()}
	default:
		return &rpc.Error{Code: rpc.CodeInternal, Message: err.Error()}
	}
}

// hello — рукопожатие. Единственный метод канала в этой вехе.
func (h *Handler) hello(st *connState, req protocol.HelloRequest) (any, error) {
	if st.session != nil {
		// Рукопожатие одно на соединение. Второе меняло бы актёра посреди
		// разговора: команды, уже отвеченные от имени одной сессии, оказались
		// бы в одном сокете с командами другой, и ключ идемпотентности перестал
		// бы означать «этот клиент это уже просил».
		return nil, &protocol.Refusal{Reason: protocol.ReasonAlreadyGreeted, ResourceID: st.session.id}
	}
	if req.ProtocolVersion() != protocol.ProtocolVersion {
		// Отказ по MAJOR версии — явный. Молчаливая работа с чужой версией —
		// это когда обе стороны считают, что договорились, и расходятся в
		// первом же поле.
		//
		// В resource_id уезжает версия СЕРВЕРА, а не клиента: свою клиент
		// знает и сам, а действовать ему нужно по чужой. held_by не
		// заполняется вовсе — оно означает держателя ресурса в конфликте, а
		// несовпадение версий конфликтом за ресурс не является, и заполненное
		// поле читалось бы как захват.
		return nil, &protocol.Refusal{
			Reason:     protocol.ReasonUnsupportedProtocol,
			ResourceID: strconv.Itoa(protocol.ProtocolVersion),
			Text: fmt.Sprintf("версия конверта %d не поддерживается: сервер говорит на %d",
				req.ProtocolVersion(), protocol.ProtocolVersion),
		}
	}
	sess, err := h.reg.resume(req.SessionID())
	if err != nil {
		return nil, err
	}
	st.session = sess
	// На рукопожатие отвечаем ПОЛНЫМ снапшотом: клиент, только что открывший
	// сокет, иначе ждал бы первого изменения мира, чтобы узнать, что в нём
	// стоит. Разностных снапшотов не существует, поэтому last_snapshot_seq
	// клиента ни на что не влияет — см. protocol.HelloRequest.LastSnapshotSeq.
	return helloResult{
		ProtocolVersion: protocol.ProtocolVersion,
		SessionID:       sess.id,
		ActorID:         sess.actor,
		Snapshot:        h.envelope(sess, h.e.Snapshot()),
	}, nil
}

// publish — рассылка снапшотов этому соединению.
//
// Часов здесь нет ВОВСЕ: единственный источник событий — граница тика движка,
// единственная мера возраста — модельное время снапшота. Второй таймер в
// канале означал бы вторые часы в сервере, у которого их ровно одни (Run).
func (h *Handler) publish(ctx context.Context, st *connState) {
	sub, unsubscribe := h.e.Subscribe()
	defer unsubscribe()
	// Отсчёт ведётся от снапшота, отданного рукопожатием: он уже у клиента, и
	// повторять его немедленно незачем.
	sent := h.e.Snapshot()
	for {
		select {
		case <-ctx.Done():
			return
		case snap := <-sub:
			if snap.Hash == sent.Hash && snap.Time-sent.Time < MaxSnapshotAge {
				continue
			}
			body, err := rpc.EncodeNotification(SnapshotMethod, h.envelope(st.session, snap))
			if err != nil {
				// Снапшот не сворачивается — это поломка сервера, и молчать о
				// ней нельзя: клиент остался бы с неподвижной картинкой.
				st.ws.Close(websocket.StatusInternalError, "снапшот не сериализуется")
				return
			}
			if !st.write(ctx, body) {
				return
			}
			sent = snap
		}
	}
}

// write отправляет кадр. false означает «соединение кончилось».
func (st *connState) write(ctx context.Context, body []byte) bool {
	st.writeMu.Lock()
	defer st.writeMu.Unlock()
	return st.ws.Write(ctx, websocket.MessageText, body) == nil
}

// envelope собирает конверт вокруг УЖЕ ВЗЯТОГО снимка.
//
// Снимок приходит аргументом, а не берётся здесь: рассылка решает по снимку,
// слать ли его, и второй снимок, взятый при сборке конверта, был бы уже другим
// состоянием — клиент получил бы тело, о котором решение не принималось, а
// сервер запомнил бы отправленным то, чего не отправлял.
func (h *Handler) envelope(sess *session, snap engine.Snapshot) snapshotEnvelope {
	placed := snap.Match.Units
	if placed == nil {
		// Пустой список, а не null: партия без состава — законное состояние
		// мира, и клиент обязан увидеть «единиц нет», а не «поля нет».
		placed = []match.Unit{}
	}
	return snapshotEnvelope{
		ProtocolVersion: protocol.ProtocolVersion,
		SessionID:       sess.id,
		SnapshotSeq:     sess.nextSnapshotSeq(),
		Kind:            KindFull,
		Region:          snap.Match.Region,
		Match:           snap.Match.ID,
		Time:            snap.Time,
		Units:           placed,
	}
}

// KindFull — единственный сегодня вид снапшота.
//
// Поле kind заведено при том, что значение у него одно, и это не задел впрок:
// клиент обязан УМЕТЬ ОТЛИЧИТЬ полный снапшот от разностного до того, как
// разностные появятся, иначе первый же разностный будет применён как полный —
// то есть мир на экране схлопнется до одной изменившейся единицы.
const KindFull = "full"

// helloResult — ответ на рукопожатие.
type helloResult struct {
	ProtocolVersion int    `json:"protocol_version"`
	SessionID       string `json:"session_id"`
	// ActorID — от чьего имени сервер примет команды этого соединения. Клиент
	// его не выбирает и в params не присылает: иначе он командовал бы от
	// чужого имени (vertical-slice-design §6).
	ActorID  string           `json:"actor_id"`
	Snapshot snapshotEnvelope `json:"snapshot"`
}

// snapshotEnvelope — конверт снапшота (vertical-slice-design §6).
//
// ЧЕГО В КОНВЕРТЕ НЕТ И ПОЧЕМУ. Образец в спеке перечисляет поля, которых
// сегодня никто не считает: journal_seq, engine_tick, paused,
// scenario_time_scale, session_phase, role_assignments, control_assignments.
// Каждое из них уехало бы нулём или пустым списком, а ноль в контракте
// неотличим от «не заполнено» — то же правило, по которому в партии нет
// скорости. Поля приезжают вместе с тем, что их считает.
//
// engine_tick не едет ОТДЕЛЬНО: время выводится из номера тика, и два
// написания одной величины на проводе разошлись бы у первого же клиента,
// который выбрал не то.
//
// state_hash тоже не едет: он живёт в сервере как критерий рассылки (см.
// engine.StateHash), а клиенту не нужен — снапшот полный, и сверять ему нечего.
type snapshotEnvelope struct {
	ProtocolVersion int    `json:"protocol_version"`
	SessionID       string `json:"session_id"`
	// SnapshotSeq — сквозной номер полного снапшота в этой сессии. Пропуск
	// номера означает пропущенный снапшот и лечится сам собой: следующий
	// полный несёт всё.
	SnapshotSeq uint64 `json:"snapshot_seq"`
	Kind        string `json:"kind"`
	Region      string `json:"region"`
	Match       string `json:"match"`
	// Time — модельное время партии, микросекунды СТРОКОЙ (units.SimTime).
	Time  units.SimTime `json:"time"`
	Units []match.Unit  `json:"units"`
}

// ПРОВОД ЗДЕСЬ НЕ РАЗБИРАЕТСЯ — ни в одну сторону, кроме той, что идёт через
// rpc. Функции «разобрать конверт» у пакета нет нарочно: проверки контракта
// объявляют СВОЙ wire-тип и декодируют байты строго (contract_test.go), потому
// что тест, разбирающий ответ тем же кодом, которым он собран, проверяет
// согласие кода с самим собой. Заодно это держит правило барьера: сырой вход в
// канале не читается вовсе.
