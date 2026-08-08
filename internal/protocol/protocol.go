// Package protocol — единственное место, где описан контракт между сервером и
// клиентом на Godot.
//
// Правило пакета: у запроса, пришедшего снаружи, НЕ СУЩЕСТВУЕТ невалидного
// представления. Поля неэкспортируемые, заполняет их только parse, вызывает
// parse только диспетчер. Обработчик получает значение, которое уже проверено —
// проверять ему нечего и забыть проверку негде.
package protocol

import (
	"encoding/json"
	"fmt"
	"strconv"
)

// Input — сырой внешний вход. Дальше диспетчера не уходит.
type Input struct {
	Path map[string]string
	Body json.RawMessage
}

// Request — запечатанный интерфейс запроса.
//
// sealed неэкспортируемый, поэтому реализовать Request можно только внутри
// этого пакета. Это и есть барьер: rpc.Register принимает лишь типы,
// удовлетворяющие Request, а значит лишь те, у кого есть Parse. Parse
// экспортирован, потому что вызывать его должен диспетчер из другого пакета;
// запечатанность держат sealed и native, а не он.
//
// native() T закрывает единственный обходной путь — встраивание: тип чужого
// пакета может унаследовать sealed() промоушеном от встроенного protocol-запроса
// и переопределить Parse заглушкой, и тогда Register примет его, а обработчик
// получит неразобранный вход. native упоминает T в сигнатуре: унаследовать его
// нельзя (у встроенного запроса сигнатура была бы native() GeometryRequest, а
// требуется native() T), а определить свой нельзя — имя неэкспортировано.
// Реализовать Request может ровно тот тип, который объявлен внутри protocol.
type Request[T any] interface {
	*T
	sealed()
	Parse(Input) error
	native() T
}

// GeometryRequest — запрос геометрии карты.
type GeometryRequest struct {
	mapID    string
	revision int
}

func (*GeometryRequest) sealed() {}

// native — печать подлинности: возвращает сам запрос. См. комментарий к Request.
func (r *GeometryRequest) native() GeometryRequest { return *r }

// Parse разбирает и проверяет внешний вход. Единственный способ заполнить поля
// GeometryRequest: они неэкспортируемые, а других сеттеров нет.
func (r *GeometryRequest) Parse(in Input) error {
	id := in.Path["id"]
	if id == "" {
		return fmt.Errorf("protocol: пустой map_id")
	}
	if len(id) > 128 {
		return fmt.Errorf("protocol: map_id длиннее 128 символов")
	}
	rev, err := strconv.Atoi(in.Path["rev"])
	if err != nil {
		return fmt.Errorf("protocol: ревизия не число: %q", in.Path["rev"])
	}
	if rev < 1 {
		return fmt.Errorf("protocol: ревизия должна быть положительной, получено %d", rev)
	}
	r.mapID, r.revision = id, rev
	return nil
}

// MapID возвращает проверенный идентификатор карты.
func (r GeometryRequest) MapID() string { return r.mapID }

// Revision возвращает проверенный номер ревизии.
func (r GeometryRequest) Revision() int { return r.revision }
