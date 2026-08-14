package rpc

import (
	"encoding/json"
	"fmt"
)

// Кадры JSON-RPC 2.0 разбираются ЗДЕСЬ, а не в пакете транспорта, и это то же
// решение, что и весь барьер: сырой внешний вход читает один пакет.
//
// # Почему разбор кадра — это тоже барьер
//
// У HTTP-ручки внешний вход раскладывается механически: сегменты пути и тело
// уезжают в protocol.Input, не будучи прочитанными. У сокета так не выходит —
// чтобы узнать, КАКОЙ метод звать, кадр придётся разобрать. Разбор внешнего
// JSON — ровно то действие, ради запрета которого заведён чёрный список
// (Unmarshal, NewDecoder), и если бы его делал пакет канала, список пришлось бы
// в нём отключить, то есть отменить барьер там, где он впервые нужен по-
// настоящему: у команд, а не у адресов.
//
// Поэтому сюда приезжают БАЙТЫ КАДРА, а наружу отдаётся Frame, у которого
// невалидного представления не существует: имя метода непусто, id есть, params
// либо отсутствуют, либо являются объектом. Дальше работает прежний механизм —
// Dispatch зовёт Parse запроса, и до обработчика недоразобранное не доходит.
//
// # Подмножество: что не поддерживается и почему
//
// Пакетные запросы (массив в корне) и уведомления снизу вверх ЗАПРЕЩЕНЫ —
// решение вертикального среза §6, и оба запрета имеют одну причину: у команды
// обязан быть ответ, адресуемый её id. Пакет размывает соответствие «запрос —
// ответ» на список, а уведомление объявляет, что ответ не нужен вовсе; и то и
// другое ломает идемпотентность, которая опирается на то, что повтор команды
// возвращает ТОТ ЖЕ ответ.
//
// Позиционные params (массив) тоже отвергнуты: имя поля переживает вставку
// нового параметра, номер — нет.

// Коды отказов.
//
// Первые пять — стандарт JSON-RPC 2.0. CodeRefused — ЛОКАЛЬНАЯ КОНВЕНЦИЯ
// проекта в зарезервированном под реализацию диапазоне -32000…-32099
// (вертикальный срез §6): один код на все доменные отказы, а причина машинная и
// лежит в data. Второй код завели бы ровно затем, чтобы клиент выбирал ветку по
// числу вместо reason, — и таблица кодов начала бы расти вместе с доменом.
const (
	CodeParse          = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternal       = -32603
	CodeRefused        = -32001
)

// MaxMethodLength и MaxCommandIDLength — потолки на недоверенные строки кадра.
//
// Потолки стоят не ради памяти (кадр уже прочитан целиком), а ради журнала и
// логов: имя метода и command_id попадают в сообщения об отказе, а строка на
// мегабайт в логе — это отказ в обслуживании чтением логов.
const (
	MaxMethodLength    = 64
	MaxCommandIDLength = 64
)

// Error — отказ, пригодный для отправки клиенту как есть.
//
// Message человеческое и по-русски: его читает тот, кто смотрит в лог или в
// консоль клиента. Машинное решение принимается по Code и по Data — см.
// protocol.Refusal.
type Error struct {
	Code    int
	Message string
	Data    any
}

func (e *Error) Error() string { return fmt.Sprintf("jsonrpc %d: %s", e.Code, e.Message) }

// Frame — разобранный запрос из сокета. Невалидного представления не имеет:
// заполняет его только ParseFrame.
type Frame struct {
	// ID — корреляционный идентификатор запроса как он пришёл, БАЙТАМИ.
	//
	// Не строкой и не числом: стандарт разрешает оба, а ответ обязан вернуть
	// ровно то, что прислали. Перевод в строку и обратно однажды вернул бы "7"
	// вместо 7, и клиент не нашёл бы свой запрос.
	ID json.RawMessage
	// Method — имя метода для rpc.Mux.
	Method string
	// CommandID — ключ идемпотентности, часть тройки (session_id, actor_id,
	// command_id).
	//
	// Пустой означает ЗАПРОС БЕЗ ПОБОЧНЫХ ДЕЙСТВИЙ. Это правило провода, и оно
	// заведено здесь вместо списка «какие методы являются командами»: список
	// живёт в двух местах (транспорт и домен) и расходится на первом же новом
	// методе, а признак, приезжающий с самим запросом, разойтись не может.
	CommandID string
	// Params — именованные параметры как есть. Разбирает их Parse запроса.
	Params json.RawMessage
}

// wireRequest — кадр как он лежит на проводе.
//
// Поля-указатели там, где отсутствие и пустое значение — разные вещи: id: null
// стандартом разрешён, но у нас запрещён (это уведомление в переодетом виде), и
// отличить его от отсутствующего id можно только указателем.
type wireRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

// commandEnvelope — то единственное, что транспорт читает в params сам.
//
// Остальные поля params не разбираются здесь НАРОЧНО: их читает Parse запроса
// за барьером. Транспорту нужен ровно ключ идемпотентности, потому что решение
// «этот повтор уже обслужен» принимается ДО вызова обработчика — иначе повтор
// применился бы дважды и вопрос об идемпотентности стал бы бессмысленным.
type commandEnvelope struct {
	CommandID string `json:"command_id"`
}

// ParseFrame разбирает кадр. Ошибка отправляется клиенту как есть.
func ParseFrame(raw []byte) (Frame, *Error) {
	// Пакетный запрос отвергается ДО разбора и по первому непробельному байту:
	// json.Unmarshal массива в структуру дал бы «cannot unmarshal array», то
	// есть отказ формы вместо отказа по существу — а по существу здесь отказ
	// поддержки, и клиенту надо сказать именно это.
	if b, ok := firstToken(raw); !ok || b == '[' {
		return Frame{}, &Error{Code: CodeInvalidRequest,
			Message: "пакетные запросы не поддерживаются: у команды обязан быть свой ответ"}
	}
	var wire wireRequest
	if err := json.Unmarshal(raw, &wire); err != nil {
		return Frame{}, &Error{Code: CodeParse, Message: "кадр не разбирается как JSON-RPC 2.0"}
	}
	if wire.JSONRPC != "2.0" {
		return Frame{}, &Error{Code: CodeInvalidRequest,
			Message: fmt.Sprintf("поле jsonrpc должно быть \"2.0\", получено %q", wire.JSONRPC)}
	}
	if len(wire.ID) == 0 || string(wire.ID) == "null" {
		return Frame{}, &Error{Code: CodeInvalidRequest,
			Message: "запрос без id — уведомление; снизу вверх они запрещены"}
	}
	if !idIsScalar(wire.ID) {
		return Frame{}, &Error{Code: CodeInvalidRequest,
			Message: "id обязан быть строкой или числом"}
	}
	if wire.Method == "" {
		return Frame{}, &Error{Code: CodeInvalidRequest, Message: "имя метода пусто"}
	}
	if len(wire.Method) > MaxMethodLength {
		return Frame{}, &Error{Code: CodeInvalidRequest,
			Message: fmt.Sprintf("имя метода длиннее %d символов", MaxMethodLength)}
	}
	f := Frame{ID: wire.ID, Method: wire.Method, Params: wire.Params}
	if len(wire.Params) != 0 {
		if b, ok := firstToken(wire.Params); !ok || b != '{' {
			return Frame{}, &Error{Code: CodeInvalidParams,
				Message: "params обязаны быть объектом: имя поля переживает вставку параметра, номер — нет"}
		}
		var env commandEnvelope
		if err := json.Unmarshal(wire.Params, &env); err != nil {
			return Frame{}, &Error{Code: CodeInvalidParams, Message: "params не разбираются"}
		}
		if len(env.CommandID) > MaxCommandIDLength {
			return Frame{}, &Error{Code: CodeInvalidParams,
				Message: fmt.Sprintf("command_id длиннее %d символов", MaxCommandIDLength)}
		}
		f.CommandID = env.CommandID
	}
	return f, nil
}

// firstToken возвращает первый непробельный байт документа.
//
// Своя функция, а не json.Decoder.Token: нужен именно СЫРОЙ признак формы до
// разбора, чтобы отказ назвал причину («пакеты не поддерживаются»), а не
// пересказал ошибку декодера.
func firstToken(raw []byte) (byte, bool) {
	for _, b := range raw {
		switch b {
		case ' ', '\t', '\r', '\n':
			continue
		default:
			return b, true
		}
	}
	return 0, false
}

// idIsScalar проверяет, что id — строка или число.
//
// Объект и массив стандарт не запрещает прямо (он лишь «SHOULD NOT»), но у нас
// id уезжает в ключ карты ожидающих ответа, а ключом обязано быть сравнимое
// значение с однозначной записью. {"a":1,"b":2} и {"b":2,"a":1} — один id или
// два? Ответа нет, поэтому нет и таких id.
func idIsScalar(id json.RawMessage) bool {
	b, ok := firstToken(id)
	if !ok {
		return false
	}
	return b == '"' || b == '-' || (b >= '0' && b <= '9')
}

// wireResponse — ответ на проводе. Result и Error взаимоисключающи по
// стандарту, поэтому оба указателями: пустой result у отказа означал бы, что
// команда выполнилась и вернула ничто.
type wireResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *wireError      `json:"error,omitempty"`
}

type wireError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// wireNotification — то, что идёт СВЕРХУ ВНИЗ: у снапшота нет id, потому что
// ответа на него не бывает.
type wireNotification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params"`
}

// EncodeResult собирает успешный ответ.
func EncodeResult(id json.RawMessage, result any) ([]byte, error) {
	return json.Marshal(wireResponse{JSONRPC: "2.0", ID: id, Result: result})
}

// EncodeError собирает отказ.
//
// Отказ обязан доехать всегда, поэтому ошибка сериализации здесь не
// возвращается наружу, а лечится на месте: data — единственное поле, которое
// может не свернуться (в него кладут доменную причину), и без него ответ
// остаётся законным отказом с кодом и текстом. Молчание вместо отказа было бы
// хуже: клиент ждал бы ответа на свой id вечно.
func EncodeError(id json.RawMessage, e *Error) []byte {
	body, err := json.Marshal(wireResponse{JSONRPC: "2.0", ID: id,
		Error: &wireError{Code: e.Code, Message: e.Message, Data: e.Data}})
	if err == nil {
		return body
	}
	body, err = json.Marshal(wireResponse{JSONRPC: "2.0", ID: id,
		Error: &wireError{Code: e.Code, Message: e.Message}})
	if err == nil {
		return body
	}
	// Сюда попасть нельзя: id уже проверен как скаляр, а message — строка.
	// Ветка существует затем, чтобы у функции не было пути «вернуть nil».
	return []byte(`{"jsonrpc":"2.0","id":null,"error":{"code":-32603,"message":"отказ не сериализуется"}}`)
}

// EncodeNotification собирает уведомление сверху вниз.
func EncodeNotification(method string, params any) ([]byte, error) {
	return json.Marshal(wireNotification{JSONRPC: "2.0", Method: method, Params: params})
}
