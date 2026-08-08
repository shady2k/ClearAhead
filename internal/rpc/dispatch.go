// Package rpc — единственный вход внешних вызовов в обработчики.
//
// Пакет держит барьер валидации: Register не принимает обработчик, у типа
// запроса которого нет разбора, а Dispatch не вызывает обработчик, пока разбор
// не прошёл. Другого способа добраться до обработчика в кодовой базе нет.
package rpc

import (
	"context"
	"fmt"

	"github.com/shady2k/ClearAhead/internal/protocol"
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
			return nil, fmt.Errorf("rpc: %s: %w", method, err)
		}
		return h(ctx, req)
	}
}

// Dispatch разбирает вход и вызывает обработчик. При ошибке разбора обработчик
// не вызывается вовсе.
func (m *Mux) Dispatch(ctx context.Context, method string, in protocol.Input) (any, error) {
	r, ok := m.routes[method]
	if !ok {
		return nil, fmt.Errorf("rpc: неизвестный метод %q", method)
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
