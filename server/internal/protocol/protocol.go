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
// нельзя (у встроенного запроса сигнатура была бы native() NetworkRequest, а
// требуется native() T), а определить свой нельзя — имя неэкспортировано.
// Реализовать Request может ровно тот тип, который объявлен внутри protocol.
type Request[T any] interface {
	*T
	sealed()
	Parse(Input) error
	native() T
}

// NetworkRequest — запрос сети региона: /regions/{region}/revisions/{n}/network.
//
// Здесь была GeometryRequest с полем map_id. Переименование не косметическое:
// у ресурса сменился корень. Сеть адресуется РЕГИОНОМ, а не картой, потому что
// корень у клиента должен быть один — рельеф региона и сеть региона обязаны
// называться одним именем, иначе клиенту приходится знать соглашение
// «region == map_id», которое сегодня живёт одной строкой в worldgen.Bootstrap.
// Про имя `network` — см. httpapi.NewNetworkHandler: оно называет КЛАСС, а не
// вид, и потому переживёт появление автомобильных дорог.
//
// Ширина идентификатора региона проверяется тем же лимитом, что и у карты
// (mapfmt.MaxIDLength): сегодня это одно и то же значение, и второй лимит
// разошёлся бы с первым молча.
type NetworkRequest struct {
	region   string
	revision int
}

func (*NetworkRequest) sealed() {}

// native — печать подлинности: возвращает сам запрос. См. комментарий к Request.
func (r *NetworkRequest) native() NetworkRequest { return *r }

// Parse разбирает и проверяет внешний вход. Единственный способ заполнить поля
// NetworkRequest: они неэкспортируемые, а других сеттеров нет.
func (r *NetworkRequest) Parse(in Input) error {
	region := in.Path["region"]
	if region == "" {
		return fmt.Errorf("protocol: пустой идентификатор региона")
	}
	if len(region) > mapfmt.MaxIDLength {
		return fmt.Errorf("protocol: идентификатор региона длиннее %d символов", mapfmt.MaxIDLength)
	}
	rev, err := strconv.Atoi(in.Path["rev"])
	if err != nil {
		return fmt.Errorf("protocol: ревизия не число: %q", in.Path["rev"])
	}
	if rev < 1 {
		return fmt.Errorf("protocol: ревизия должна быть положительной, получено %d", rev)
	}
	r.region, r.revision = region, rev
	return nil
}

// Region возвращает проверенный идентификатор региона.
func (r NetworkRequest) Region() string { return r.region }

// Revision возвращает проверенный номер ревизии.
func (r NetworkRequest) Revision() int { return r.revision }

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

// Запросов на список, загрузку и сохранение карты больше нет: карта не
// хранится файлом, а присланную документом сервер не принимает — строгий
// разбор карты удалён вместе с парсером. Барьер валидации внешних вызовов от
// этого не ослаб: он по-прежнему стоит перед каждым запросом, просто ни один
// из оставшихся не несёт документа карты.
