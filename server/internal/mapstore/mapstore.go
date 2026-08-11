// Package mapstore держит текущую карту сервера и её скомпилированные
// артефакты.
//
// # Что здесь было и чего не стало
//
// Был каталог JSON-файлов: список, загрузка, сохранение, «сохранить как»,
// разрешение симлинков, проверка путей. Ничего этого нет — карта больше не
// хранится файлом. Мир живёт в базе (worldstore), а карта появляется кодом:
// затравкой из seedmap при бутстрапе или правкой через edit.
//
// Осталось ровно то, ради чего пакет и заводился: одна карта в памяти вместе с
// артефактами одной ревизии, посчитанными ОДИН РАЗ при входе. Внутри запроса
// нет ни чтения с диска, ни перекомпиляции, ни сети.
//
// # Что это отняло, названо честно
//
// Клиент больше не может прислать серверу карту документом: приём карты
// разбором исчез вместе с парсером. Карта по-прежнему УЕЗЖАЕТ клиенту в
// ответе — направление осталось одно.
package mapstore

import (
	"encoding/json"
	"errors"
	"sync"

	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/seedmap"
	"github.com/shady2k/ClearAhead/server/internal/track"
)

// ErrInvalid — карта не прошла полный путь входа (разбор, валидация,
// компиляция) и потому не стала текущей.
var ErrInvalid = errors.New("mapstore: карта не прошла проверку")

// State — текущее состояние карты: документ и скомпилированные артефакты одной
// ревизии.
//
// Состояние неизменяемо после создания: операции меняют указатель Store.cur, а
// не мутируют старое, поэтому держать *State и читать его поля можно без
// блокировок.
type State struct {
	Map          mapfmt.Map
	Track        *track.CompiledTrack
	Render       *track.RenderGeometry
	RenderBody   []byte // сериализованная геометрия — то же, что описывает ETag
	Manifest     track.Manifest
	ManifestBody []byte
}

// Store — одна карта в памяти.
type Store struct {
	mu  sync.RWMutex
	cur *State
}

// Open создаёт пустое хранилище. Аргументов нет: каталога больше не существует.
func Open() *Store { return &Store{} }

// New делает текущей карту-затравку.
//
// Пустой карты не бывает: валидатор отвергает карту без якорей, а якорь
// ссылается на элемент — ноль элементов невалиден по построению. Затравку
// строит seedmap, та же фабрика, что порождает фикстуры тестов: карта,
// собранная кодом, не может разойтись со схемой формата, потому что перестаёт
// компилироваться.
func (s *Store) New() (*State, error) {
	return s.Set(seedmap.Blank())
}

// Set проводит карту через полный путь входа и делает её текущей.
//
// Карта, не прошедшая путь, текущей НЕ становится: правило «то, что нельзя
// загрузить, не хранится» пережило удаление файлов.
func (s *Store) Set(m *mapfmt.Map) (*State, error) {
	st, err := buildState(*m)
	if err != nil {
		return nil, errors.Join(ErrInvalid, err)
	}
	s.mu.Lock()
	s.cur = st
	s.mu.Unlock()
	return st, nil
}

// Current возвращает текущее состояние и признак того, что карта есть.
func (s *Store) Current() (*State, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.cur == nil {
		return nil, false
	}
	return s.cur, true
}

func buildState(m mapfmt.Map) (*State, error) {
	owned := cloneMap(&m)
	if err := mapfmt.Validate(&owned); err != nil {
		return nil, err
	}
	ct, rg, err := track.Compile(&owned)
	if err != nil {
		return nil, err
	}
	man, err := track.BuildManifest(&owned, ct, rg)
	if err != nil {
		return nil, err
	}
	// Байты берутся из track.RenderBody — того же места, по которому считается
	// network_hash: своя сериализация здесь означала бы, что ETag
	// когда-нибудь опишет не то тело, которое ушло.
	body, err := track.RenderBody(rg)
	if err != nil {
		return nil, err
	}
	manBody, err := json.Marshal(man)
	if err != nil {
		return nil, err
	}
	return &State{
		Map: owned, Track: ct, Render: rg,
		RenderBody: body, Manifest: man, ManifestBody: manBody,
	}, nil
}

// cloneMap — глубокая копия карты через JSON-круговорот (тот же приём, что в
// internal/edit).
//
// Это способ копирования, а не формат хранения: карта нигде не лежит JSON'ом.
// Круговорот выбран потому, что точен для float64 и покрывает все поля формата
// без ручного перечисления, которое расходилось бы со схемой при каждом её
// расширении.
func cloneMap(m *mapfmt.Map) mapfmt.Map {
	b, err := json.Marshal(m)
	if err != nil {
		panic("mapstore: карта не сериализуется: " + err.Error())
	}
	var out mapfmt.Map
	if err := json.Unmarshal(b, &out); err != nil {
		panic("mapstore: копия карты не разбирается: " + err.Error())
	}
	return out
}
