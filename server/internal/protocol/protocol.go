// Package protocol — единственное место, где описан контракт между сервером и
// клиентом на Godot.
//
// Правило пакета: у запроса, пришедшего снаружи, НЕ СУЩЕСТВУЕТ невалидного
// представления. Поля неэкспортируемые, заполняет их только parse, вызывает
// parse только диспетчер. Обработчик получает значение, которое уже проверено —
// проверять ему нечего и забыть проверку негде.
package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
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

// ManifestRequest — запрос манифеста загруженной карты.
//
// Манифест не адресуется ни картой, ни ревизией: клиент идёт за ним именно
// затем, чтобы УЗНАТЬ пару (map_id, ревизия), а сервер держит в памяти одну
// карту (см. httpapi.NewHandler). У представления запроса нет ни одного поля:
// любой сегмент пути или тело — невалидное представление, и Parse его
// отвергает. Обработчик, зарегистрированный в rpc.Mux, получает значение,
// которому проверять нечего.
type ManifestRequest struct{}

func (*ManifestRequest) sealed() {}

func (r *ManifestRequest) native() ManifestRequest { return *r }

// Parse разбирает внешний вход. Единственное валидное представление запроса
// манифеста — пустое: ни пути, ни тела.
func (r *ManifestRequest) Parse(in Input) error {
	if len(in.Path) != 0 {
		return fmt.Errorf("protocol: запрос манифеста не адресуется сегментами пути")
	}
	if len(in.Body) != 0 {
		return fmt.Errorf("protocol: у запроса манифеста нет тела")
	}
	return nil
}

// ListMapsRequest — список карт в каталоге. У запроса нет ни одного поля
// представления: любой сегмент пути или тело — невалидное представление, и
// Parse его отвергает (как у ManifestRequest).
type ListMapsRequest struct{}

func (*ListMapsRequest) sealed() {}

func (r *ListMapsRequest) native() ListMapsRequest { return *r }

// Parse разбирает внешний вход. Единственное валидное представление — пустое.
func (r *ListMapsRequest) Parse(in Input) error {
	if len(in.Path) != 0 {
		return fmt.Errorf("protocol: список карт не адресуется сегментами пути")
	}
	if len(in.Body) != 0 {
		return fmt.Errorf("protocol: у запроса списка нет тела")
	}
	return nil
}

// NewMapRequest — создание карты-затравки. Как и список, запрос не несёт ни
// полей, ни тела: имя карте даст «сохранить как», а не момент создания.
type NewMapRequest struct{}

func (*NewMapRequest) sealed() {}

func (r *NewMapRequest) native() NewMapRequest { return *r }

// Parse разбирает внешний вход. Единственное валидное представление — пустое.
func (r *NewMapRequest) Parse(in Input) error {
	if len(in.Path) != 0 {
		return fmt.Errorf("protocol: новая карта не адресуется сегментами пути")
	}
	if len(in.Body) != 0 {
		return fmt.Errorf("protocol: у запроса новой карты нет тела")
	}
	return nil
}

// LoadMapRequest — загрузка карты из каталога по имени файла. Имя — один
// сегмент пути: представление с разделителями не существует по построению
// HTTP-разложения, а «..» в имени отвергает безопасность путей mapstore.
type LoadMapRequest struct {
	name string
}

func (*LoadMapRequest) sealed() {}

func (r *LoadMapRequest) native() LoadMapRequest { return *r }

// Parse разбирает внешний вход: имя из сегмента пути, тело запрещено.
func (r *LoadMapRequest) Parse(in Input) error {
	name := in.Path["name"]
	if name == "" {
		return fmt.Errorf("protocol: пустое имя карты")
	}
	if len(name) > 256 {
		return fmt.Errorf("protocol: имя карты длиннее 256 символов")
	}
	if len(in.Body) != 0 {
		return fmt.Errorf("protocol: у запроса загрузки нет тела")
	}
	r.name = name
	return nil
}

// Name возвращает проверенное имя файла карты.
func (r LoadMapRequest) Name() string { return r.name }

// SaveMapRequest — сохранение карты под текущим именем. Тело — документ
// карты: разбор строгий (mapfmt.Decode), как у файла. Валидацию и компиляцию
// выполняет mapstore как часть операции сохранения — карта, не прошедшая
// полный путь входа, на диск не попадает.
type SaveMapRequest struct {
	m mapfmt.Map
}

func (*SaveMapRequest) sealed() {}

func (r *SaveMapRequest) native() SaveMapRequest { return *r }

// Parse разбирает внешний вход: тело — документ карты, путь запрещён.
func (r *SaveMapRequest) Parse(in Input) error {
	if len(in.Path) != 0 {
		return fmt.Errorf("protocol: сохранение не адресуется сегментами пути")
	}
	m, err := decodeMapBody(in.Body)
	if err != nil {
		return err
	}
	r.m = *m
	return nil
}

// Map возвращает проверенный по форме документ карты.
func (r SaveMapRequest) Map() mapfmt.Map { return r.m }

// SaveAsMapRequest — сохранение карты под новым именем. Имя — сегмент пути,
// тело — документ карты.
type SaveAsMapRequest struct {
	name string
	m    mapfmt.Map
}

func (*SaveAsMapRequest) sealed() {}

func (r *SaveAsMapRequest) native() SaveAsMapRequest { return *r }

// Parse разбирает внешний вход: имя из сегмента пути, документ из тела.
func (r *SaveAsMapRequest) Parse(in Input) error {
	name := in.Path["name"]
	if name == "" {
		return fmt.Errorf("protocol: пустое имя карты")
	}
	if len(name) > 256 {
		return fmt.Errorf("protocol: имя карты длиннее 256 символов")
	}
	m, err := decodeMapBody(in.Body)
	if err != nil {
		return err
	}
	r.name, r.m = name, *m
	return nil
}

// Name возвращает проверенное имя файла карты.
func (r SaveAsMapRequest) Name() string { return r.name }

// Map возвращает проверенный по форме документ карты.
func (r SaveAsMapRequest) Map() mapfmt.Map { return r.m }

// decodeMapBody разбирает документ карты из тела запроса. Разбор — тот же,
// что у файла на диске (mapfmt.Decode): строгий, с лимитами, дубликатами и
// неизвестными полями. Тело пустое или не-документ — отказ барьера.
func decodeMapBody(body json.RawMessage) (*mapfmt.Map, error) {
	if len(body) == 0 {
		return nil, fmt.Errorf("protocol: пустое тело карты")
	}
	m, err := mapfmt.Decode(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("protocol: %w", err)
	}
	return m, nil
}
