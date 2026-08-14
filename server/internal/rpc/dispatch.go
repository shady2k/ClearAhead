// Package rpc — единственный вход внешних вызовов в обработчики.
//
// Пакет держит барьер валидации: Register не принимает обработчик, у типа
// запроса которого нет разбора, а Dispatch не вызывает обработчик, пока разбор
// не прошёл. Другого способа добраться до обработчика в кодовой базе нет.
package rpc

import (
	"context"
	"errors"
	"fmt"

	"github.com/shady2k/ClearAhead/server/internal/protocol"
)

// Два исхода барьера, которые вызывающий обязан РАЗЛИЧАТЬ.
//
// До канала команд их различать было незачем: HTTP-ручка на любой отказ
// барьера отвечала 404, потому что снаружи неверный адрес и неверное тело
// одинаково означают «такого ресурса нет». В JSON-RPC у них разные коды
// (-32601 против -32602), и разница не косметическая: первый говорит «такого
// метода не существует», второй — «метод есть, а запрос неверен». Клиент,
// получивший не тот код, чинил бы не то.
//
// Сентинелы, а не типы ошибок: различать нужно ровно два случая, а сообщение и
// без того едет строкой. Тип завёлся бы вместе с полем, которое надо прочитать
// машинно, — у доменных отказов такое поле есть (protocol.Refusal), у ошибок
// барьера его нет.
var (
	// ErrUnknownMethod — метода нет в реестре.
	ErrUnknownMethod = errors.New("rpc: неизвестный метод")
	// ErrInvalidParams — метод есть, но разбор запроса отказал.
	ErrInvalidParams = errors.New("rpc: запрос не разобран")
)

type route func(context.Context, protocol.Input) (any, error)

// Mux — реестр методов.
type Mux struct {
	routes map[string]route
}

// NewMux создаёт пустой реестр.
func NewMux() *Mux {
	return &Mux{routes: map[string]route{}}
}

// Register связывает имя метода с обработчиком.
//
// Ограничение PT protocol.Request[T] — это и есть барьер: интерфейс запечатан
// неэкспортируемыми методами, поэтому подставить тип без разбора нельзя, а
// обойти Register нельзя, потому что routes неэкспортируемо.
func Register[T any, PT protocol.Request[T], Resp any](
	m *Mux, method string, h func(context.Context, T) (Resp, error),
) {
	if _, dup := m.routes[method]; dup {
		panic("rpc: метод " + method + " зарегистрирован дважды")
	}
	m.routes[method] = func(ctx context.Context, in protocol.Input) (any, error) {
		var req T
		if err := PT(&req).Parse(in); err != nil {
			// Обёрнуты ОБЕ ошибки: сентинел даёт вызывающему код ответа, а
			// исходная ошибка — причину, которую читает человек. Потерять
			// вторую ради первой значило бы отдать клиенту «запрос не
			// разобран» без единого слова о том, какое поле виновато.
			return nil, fmt.Errorf("%w: %s: %w", ErrInvalidParams, method, err)
		}
		return h(ctx, req)
	}
}

// Dispatch разбирает вход и вызывает обработчик. При ошибке разбора обработчик
// не вызывается вовсе.
func (m *Mux) Dispatch(ctx context.Context, method string, in protocol.Input) (any, error) {
	r, ok := m.routes[method]
	if !ok {
		return nil, fmt.Errorf("%w %q", ErrUnknownMethod, method)
	}
	return r(ctx, in)
}

// Methods возвращает имена зарегистрированных методов — для теста барьера и для
// генерации клиентской стороны контракта.
func (m *Mux) Methods() []string {
	out := make([]string, 0, len(m.routes))
	for k := range m.routes {
		out = append(out, k)
	}
	return out
}
