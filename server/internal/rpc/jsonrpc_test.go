package rpc

import (
	"encoding/json"
	"strings"
	"testing"
)

// Разбор кадра — это ВХОД В БАРЬЕР со стороны сокета, и проверяется он так же
// строго, как разбор адреса: у кадра, дошедшего до диспетчера, не должно быть
// невалидного представления.

func TestParseFrameAcceptsRequest(t *testing.T) {
	f, err := ParseFrame([]byte(`{"jsonrpc":"2.0","id":7,"method":"hello","params":{"protocol_version":1}}`))
	if err != nil {
		t.Fatalf("законный кадр отвергнут: %v", err)
	}
	if f.Method != "hello" || string(f.ID) != "7" {
		t.Fatalf("разобрано неверно: %+v", f)
	}
	if f.CommandID != "" {
		t.Fatalf("у запроса без command_id взялся ключ %q", f.CommandID)
	}
}

// id возвращается БАЙТАМИ и в исходной записи: стандарт разрешает и строку, и
// число, а ответ обязан вернуть ровно то, что прислали. Перевод в строку и
// обратно вернул бы "7" вместо 7, и клиент не нашёл бы свой запрос.
func TestParseFrameKeepsIDVerbatim(t *testing.T) {
	for _, id := range []string{`7`, `"c-1"`, `-3`} {
		f, err := ParseFrame([]byte(`{"jsonrpc":"2.0","id":` + id + `,"method":"hello"}`))
		if err != nil {
			t.Fatalf("id %s отвергнут: %v", id, err)
		}
		if string(f.ID) != id {
			t.Fatalf("id пришёл как %s, ожидался %s", f.ID, id)
		}
	}
}

func TestParseFrameReadsCommandID(t *testing.T) {
	f, err := ParseFrame([]byte(`{"jsonrpc":"2.0","id":1,"method":"m","params":{"command_id":"c1","x":2}}`))
	if err != nil {
		t.Fatalf("кадр с command_id отвергнут: %v", err)
	}
	if f.CommandID != "c1" {
		t.Fatalf("ключ идемпотентности %q", f.CommandID)
	}
	// Остальные params транспорт НЕ разбирает: их читает Parse запроса за
	// барьером. Здесь проверяется, что они доехали как есть.
	var rest map[string]any
	if err := json.Unmarshal(f.Params, &rest); err != nil {
		t.Fatalf("params не доехали: %v", err)
	}
	if rest["x"] != float64(2) {
		t.Fatalf("params потеряли поля: %v", rest)
	}
}

// Три запрета подмножества (vertical-slice-design §6). У каждого своя причина, и
// проверяются они по отдельности, потому что чинить их придётся порознь.
func TestParseFrameRefusesUnsupportedForms(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		code int
		says string
	}{
		{"пакет", `[{"jsonrpc":"2.0","id":1,"method":"a"}]`, CodeInvalidRequest, "пакет"},
		{"уведомление снизу", `{"jsonrpc":"2.0","method":"a"}`, CodeInvalidRequest, "уведомление"},
		{"id null", `{"jsonrpc":"2.0","id":null,"method":"a"}`, CodeInvalidRequest, "уведомление"},
		{"позиционные params", `{"jsonrpc":"2.0","id":1,"method":"a","params":[1,2]}`, CodeInvalidParams, "объектом"},
		{"чужая версия", `{"jsonrpc":"1.0","id":1,"method":"a"}`, CodeInvalidRequest, "jsonrpc"},
		{"без метода", `{"jsonrpc":"2.0","id":1}`, CodeInvalidRequest, "метода"},
		{"id объектом", `{"jsonrpc":"2.0","id":{"a":1},"method":"a"}`, CodeInvalidRequest, "id"},
		{"не JSON", `не json вовсе`, CodeParse, ""},
		{"пусто", ``, CodeInvalidRequest, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ParseFrame([]byte(c.raw))
			if err == nil {
				t.Fatalf("кадр принят: %s", c.raw)
			}
			if err.Code != c.code {
				t.Fatalf("код %d, ожидался %d (%s)", err.Code, c.code, err.Message)
			}
			if c.says != "" && !strings.Contains(err.Message, c.says) {
				t.Fatalf("сообщение не называет причину (%q): %s", c.says, err.Message)
			}
		})
	}
}

func TestParseFrameRefusesOverlongStrings(t *testing.T) {
	long := strings.Repeat("м", MaxMethodLength+1)
	if _, err := ParseFrame([]byte(`{"jsonrpc":"2.0","id":1,"method":"` + long + `"}`)); err == nil {
		t.Fatal("метод сверх потолка принят")
	}
	cid := strings.Repeat("c", MaxCommandIDLength+1)
	if _, err := ParseFrame([]byte(`{"jsonrpc":"2.0","id":1,"method":"a","params":{"command_id":"` + cid + `"}}`)); err == nil {
		t.Fatal("command_id сверх потолка принят")
	}
}

func TestEncodeResultAndError(t *testing.T) {
	body, err := EncodeResult(json.RawMessage(`7`), map[string]int{"a": 1})
	if err != nil {
		t.Fatalf("ответ не собрался: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("ответ не разбирается: %v", err)
	}
	if got["jsonrpc"] != "2.0" || got["id"] != float64(7) {
		t.Fatalf("конверт ответа: %v", got)
	}
	if _, has := got["error"]; has {
		t.Fatalf("у успешного ответа есть error: %v", got)
	}

	fail := EncodeError(json.RawMessage(`7`), &Error{Code: CodeRefused, Message: "нет", Data: map[string]string{"reason": "x"}})
	// Карта заводится ЗАНОВО: json.Unmarshal в непустую карту дописывает в неё,
	// а не заменяет её, и проверка «у отказа нет result» прошла бы на поле,
	// оставшемся от прошлого разбора.
	var refused map[string]any
	if err := json.Unmarshal(fail, &refused); err != nil {
		t.Fatalf("отказ не разбирается: %v", err)
	}
	if _, has := refused["result"]; has {
		t.Fatalf("у отказа есть result: %v", refused)
	}
}

// Отказ обязан доехать ДАЖЕ если доменная причина не сворачивается в JSON:
// молчание оставило бы клиента ждать ответа на свой id вечно.
func TestEncodeErrorSurvivesUnserializableData(t *testing.T) {
	body := EncodeError(json.RawMessage(`1`), &Error{
		Code: CodeInternal, Message: "поломка", Data: make(chan int)})
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("отказ не собрался вовсе: %v (%s)", err, body)
	}
	e, ok := got["error"].(map[string]any)
	if !ok || e["message"] != "поломка" {
		t.Fatalf("отказ потерял причину: %v", got)
	}
}

func TestEncodeNotificationHasNoID(t *testing.T) {
	body, err := EncodeNotification("snapshot", map[string]int{"snapshot_seq": 3})
	if err != nil {
		t.Fatalf("уведомление не собралось: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("уведомление не разбирается: %v", err)
	}
	if _, has := got["id"]; has {
		t.Fatalf("у уведомления есть id: %v", got)
	}
	if got["method"] != "snapshot" {
		t.Fatalf("метод уведомления: %v", got)
	}
}
