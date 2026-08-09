// Package mapstore — жизненный цикл карты: новая, список, загрузка, сохранение.
//
// Каталог карт — единственное место, куда операции пишут и откуда читают.
// Пути ограничены каталогом (требование безопасности, а не пожелание): имя
// файла — без разделителей и «..», итоговый путь проверяется после разрешения
// симлинков, запись атомарна через временный файл в том же каталоге. RPC,
// принимающий произвольный путь на запись, — дыра; ослаблять эти проверки
// нельзя ни под каким предлогом.
//
// Сохраняется только карта, прошедшая полный путь входа (разбор, валидация,
// компиляция, манифест): файл, который нельзя загрузить, не пишется — иначе
// следующая загрузка упрётся в него, и чинить будет нечем. Отказ на сохранении
// — это отказ, а не предупреждение.
package mapstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/track"
)

// Классы ошибок, по которым httpapi отличает плохой запрос (400) от
// отсутствующей карты (404).
var (
	// ErrName — имя файла отвергнуто безопасностью путей.
	ErrName = errors.New("mapstore: недопустимое имя файла")
	// ErrNoSuch — карты с таким именем в каталоге нет.
	ErrNoSuch = errors.New("mapstore: карта не найдена")
	// ErrInvalid — карта не прошла полный путь входа (разбор, валидация,
	// компиляция) и на диск не попала.
	ErrInvalid = errors.New("mapstore: карта не прошла проверку")
	// ErrUnnamed — у карты нет имени: сохранить можно только через «сохранить как».
	ErrUnnamed = errors.New("mapstore: у карты нет имени")
)

// State — текущее состояние карты сервера: документ и скомпилированные
// артефакты одной ревизии. Артефакты считаются один раз при входе карты в
// память (New, Load, SaveAs) — внутри запроса нет ни чтения с диска, ни
// перекомпиляции, ни сети.
//
// Состояние неизменяемо после создания: операции меняют указатель Store.cur,
// а не мутируют старое состояние, поэтому держать *State и читать его поля
// можно без блокировок.
type State struct {
	// Name — имя файла в каталоге карт; пустое, если карта ещё не сохранена
	// (новая затравка или карта, загруженная флагом -map).
	Name string
	// ModTime — время последней записи файла на диск; нулевое, если карта не
	// сохранена. Это и есть «время правки» в списке карт.
	ModTime time.Time

	Map          mapfmt.Map
	Track        *track.CompiledTrack
	Render       *track.RenderGeometry
	RenderBody   []byte // сериализованная геометрия — то же, что описывает ETag
	Manifest     track.Manifest
	ManifestBody []byte
}

// MapInfo — строка списка карт: имя файла, map_id, ревизия и время правки.
// Этого хватает, чтобы нарисовать экран выбора: без списка «загрузить» в
// интерфейсе означает ввод имени руками, то есть угадывание.
type MapInfo struct {
	Name     string    `json:"name"`
	MapID    string    `json:"map_id"`
	Revision int       `json:"map_revision"`
	Modified time.Time `json:"modified"`
}

// Store — каталог карт и одна карта в памяти.
type Store struct {
	dir     string // каталог, как задан при Open
	realDir string // EvalSymlinks(dir) — граница безопасности
	mu      sync.RWMutex
	cur     *State
}

// Open открывает каталог карт, создавая его при необходимости. Каталог
// разрешается от симлинков сразу: realDir — якорь, с которым сравнивается
// итоговый путь любой операции.
func Open(dir string) (*Store, error) {
	if dir == "" {
		return nil, fmt.Errorf("mapstore: пустой каталог карт")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("mapstore: создать каталог %s: %w", dir, err)
	}
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, fmt.Errorf("mapstore: каталог %s: %w", dir, err)
	}
	return &Store{dir: dir, realDir: real}, nil
}

// New создаёт карту-затравку и делает её текущей.
//
// Пустой карты не бывает: валидатор отвергает карту без якорей, а якорь
// ссылается на элемент — ноль элементов невалиден по построению. Ослаблять
// валидатор запрещено, поэтому «новая» — затравка: один прямой элемент
// известной длины, один якорь на нём, один конец map_boundary (оттуда придёт
// перегон), другой buffer_stop. Блок construction есть, иначе решётки не
// будет. Затравка проходит Validate и Compile с первой секунды — New сама
// прогоняет полный путь и не возвращает карту, которая его не прошла.
//
// Затравка безымянная: имя появится при «сохранить как».
func (s *Store) New() (*State, error) {
	st, err := buildState(seedMap(), "")
	if err != nil {
		return nil, fmt.Errorf("mapstore: затравка не проходит полный путь входа: %w", err)
	}
	s.mu.Lock()
	s.cur = st
	s.mu.Unlock()
	return st, nil
}

// Current возвращает текущее состояние карты и признак того, что карта
// загружена. Состояние неизменяемо (см. State): отдавать его можно без копии.
func (s *Store) Current() (*State, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.cur == nil {
		return nil, false
	}
	return s.cur, true
}

// List возвращает по записи на карту: имя файла, map_id, ревизию и время
// правки. Перечисление не выходит за каталог: симлинк, ведущий наружу,
// пропускается; файл, который не разбирается (мусор, временный файл), — тоже
// (он не карта). Ошибка одного файла не валит весь список. Записи
// отсортированы по имени.
func (s *Store) List() ([]MapInfo, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, fmt.Errorf("mapstore: чтение каталога %s: %w", s.dir, err)
	}
	out := make([]MapInfo, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if err := s.checkPath(name); err != nil {
			continue // симлинк наружу или недопустимое имя — мимо списка
		}
		f, err := os.Open(filepath.Join(s.dir, name))
		if err != nil {
			continue
		}
		m, err := mapfmt.Decode(f)
		f.Close()
		if err != nil {
			continue // не карта — в списке ей делать нечего
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, MapInfo{Name: name, MapID: m.MapID, Revision: m.MapRevision, Modified: info.ModTime()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Load загружает карту из каталога полным путём входа: разбор, валидация,
// компиляция, манифест. Карта могла прийти рукой, старой сборкой редактора
// или чужим клиентом; сервер не доверяет ей, даже если её записал он сам.
func (s *Store) Load(name string) (*State, error) {
	if err := s.checkPath(name); err != nil {
		return nil, err
	}
	f, err := os.Open(filepath.Join(s.dir, name))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrNoSuch, name)
		}
		return nil, fmt.Errorf("mapstore: %s: %w", name, err)
	}
	defer f.Close()
	m, err := mapfmt.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrInvalid, name, err)
	}
	st, err := buildState(*m, name)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrInvalid, name, err)
	}
	// Время правки берётся с открытого файла, а не по пути: повторного
	// разрешения имени (и гонки с заменой файла) не возникает.
	if info, err := f.Stat(); err == nil {
		st.ModTime = info.ModTime()
	}
	s.mu.Lock()
	s.cur = st
	s.mu.Unlock()
	return st, nil
}

// LoadPath загружает карту из произвольного пути — вход флага -map. Путь
// доверен оператору сервера, поэтому границы каталога карт на него не
// распространяются; полный путь входа (разбор, валидация, компиляция,
// манифест) — общий. Карта не считается сохранённой: имя ей даст «сохранить
// как».
func (s *Store) LoadPath(path string) (*State, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("mapstore: %s: %w", path, err)
	}
	defer f.Close()
	m, err := mapfmt.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("mapstore: %s: %v", path, err)
	}
	st, err := buildState(*m, "")
	if err != nil {
		return nil, fmt.Errorf("mapstore: %s: %v", path, err)
	}
	s.mu.Lock()
	s.cur = st
	s.mu.Unlock()
	return st, nil
}

// Save сохраняет карту под её текущим именем. Безымянную карту (новая
// затравка, загрузка флагом -map) сохранить нельзя — только «сохранить как».
// Карта проходит полный путь входа перед записью: не прошедшая валидатор на
// диск не попадает.
func (s *Store) Save(m *mapfmt.Map) (track.Manifest, error) {
	s.mu.RLock()
	cur := s.cur
	s.mu.RUnlock()
	if cur == nil || cur.Name == "" {
		return track.Manifest{}, fmt.Errorf("%w: используйте «сохранить как»", ErrUnnamed)
	}
	return s.saveAs(cur.Name, m)
}

// SaveAs сохраняет карту под новым именем и делает её текущей. map_id карты
// не переписывается: идентификатор — авторское свойство документа (в
// репозитории st_a.json несёт map_id ST_A), а имя файла — лишь место хранения.
func (s *Store) SaveAs(name string, m *mapfmt.Map) (track.Manifest, error) {
	return s.saveAs(name, m)
}

func (s *Store) saveAs(name string, m *mapfmt.Map) (track.Manifest, error) {
	if err := s.checkPath(name); err != nil {
		return track.Manifest{}, err
	}
	// Карта проходит полный путь входа, прежде чем коснуться диска.
	st, err := buildState(*m, name)
	if err != nil {
		return track.Manifest{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if err := s.writeFile(name, &st.Map); err != nil {
		return track.Manifest{}, err
	}
	if info, err := os.Stat(filepath.Join(s.dir, name)); err == nil {
		st.ModTime = info.ModTime()
	}
	// Текущим становится сохранённое состояние: его артефакты описывают ровно
	// то, что легло на диск.
	s.mu.Lock()
	s.cur = st
	s.mu.Unlock()
	return st.Manifest, nil
}

// buildState прогоняет карту через полный путь входа и собирает состояние с
// готовыми артефактами. Карта копируется (JSON-круговорот): состояние обязано
// владеть своим документом, а не разделять память с вызывающим.
func buildState(m mapfmt.Map, name string) (*State, error) {
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
	// render_geometry_hash: своя сериализация здесь означала бы, что ETag
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
		Name: name, Map: owned, Track: ct, Render: rg,
		RenderBody: body, Manifest: man, ManifestBody: manBody,
	}, nil
}

// cloneMap — глубокая копия карты через JSON-круговорот (тот же приём, что в
// internal/edit): точен для float64 и покрывает все поля формата.
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

// seedMap — затравка новой карты. См. New.
func seedMap() mapfmt.Map {
	return mapfmt.Map{
		FormatVersion: mapfmt.FormatVersion,
		MapID:         "NEW",
		MapRevision:   1,
		Anchors: map[string]mapfmt.Anchor{
			"N_WEST.P1": {X: 0, Y: 0, Z: 0, Heading: 0},
		},
		Topology: mapfmt.Topology{
			Nodes: []mapfmt.Node{
				{ID: "N_WEST", Ports: []mapfmt.Port{{ID: "P1", Purpose: "map_boundary"}}},
				{ID: "N_EAST", Ports: []mapfmt.Port{{ID: "P1", Purpose: "buffer_stop"}}},
			},
			Edges: []mapfmt.Edge{
				{ID: "E_MAIN", From: "N_WEST.P1", To: "N_EAST.P1"},
			},
			Turnouts:  []mapfmt.Turnout{},
			Trackside: []mapfmt.Trackside{},
		},
		Geometry: mapfmt.Geometry{
			Edges: map[string]mapfmt.Alignments{
				"E_MAIN": {Horizontal: []mapfmt.HPrim{{Kind: "straight", Length: 200}}},
			},
		},
		Construction: &mapfmt.Construction{
			DefaultType: "TRACK_MAIN_1435",
			Types: []mapfmt.TrackType{{
				ID:      "TRACK_MAIN_1435",
				Gauge:   1.435,
				Sleeper: mapfmt.TrackSleeper{Pitch: 0.6, Length: 2.5, Width: 0.28},
				Ballast: mapfmt.TrackBallast{HalfWidth: 1.75},
			}},
			Runs: []mapfmt.ConstructionRun{{
				ID: "RUN_MAIN", Coordinate: "u", Phase: 0,
				Spans: []mapfmt.RunSpan{{Element: "E_MAIN", From: 0, To: 200, Direction: "forward"}},
			}},
		},
	}
}

// checkName проверяет имя файла карты как строку: непустое, без разделителей
// пути, без «..» в любой форме. Имя — базовое имя файла, и только оно:
// относительный путь, абсолютный путь и обход каталога — отказ.
func checkName(name string) error {
	if name == "" {
		return fmt.Errorf("%w: пустое имя файла", ErrName)
	}
	if name == "." || name == ".." {
		return fmt.Errorf("%w: имя %q недопустимо", ErrName, name)
	}
	if strings.Contains(name, "..") {
		return fmt.Errorf("%w: имя %q содержит «..»", ErrName, name)
	}
	if strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("%w: имя %q содержит разделители пути", ErrName, name)
	}
	if strings.ContainsRune(name, 0) {
		return fmt.Errorf("%w: имя %q содержит нулевой байт", ErrName, name)
	}
	return nil
}

// checkPath проверяет итоговый путь имени ПОСЛЕ разрешения симлинков: имя без
// «..» может указывать наружу через симлинк, поэтому проверяется путь, а не
// строка. Файла ещё нет — он появится в каталоге карт (единственном каталоге
// в пути: у имени нет разделителей), а каталог проверен при Open.
func (s *Store) checkPath(name string) error {
	if err := checkName(name); err != nil {
		return err
	}
	full := filepath.Join(s.dir, name)
	resolved, err := filepath.EvalSymlinks(full)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("mapstore: %s: %w", full, err)
		}
		return nil
	}
	if !within(resolved, s.realDir) {
		return fmt.Errorf("%w: %s ведёт за пределы каталога карт", ErrName, full)
	}
	return nil
}

// within — путь p лежит внутри каталога dir (или совпадает с ним).
func within(p, dir string) bool {
	rel, err := filepath.Rel(dir, p)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// checkDir — каталог карт всё ещё разрешается туда же, куда при Open. Если его
// подменили симлинком наружу, временный файл и rename оказались бы записью
// наружу — проверка перед каждой записью сужает это окно до нуля для
// статических атак (см. writeFile).
func (s *Store) checkDir() error {
	real, err := filepath.EvalSymlinks(s.dir)
	if err != nil {
		return fmt.Errorf("mapstore: каталог %s: %w", s.dir, err)
	}
	if real != s.realDir {
		return fmt.Errorf("mapstore: каталог %s сменился", s.dir)
	}
	return nil
}

// writeFile пишет карту атомарно: временный файл в каталоге карт, fsync,
// rename. Проверка путей выполняется до создания файла; rename заменяет сам
// симлинк-файл, а не цель, на которую он указывает, поэтому подмена имени
// симлинком наружу между проверкой и заменой не уводит запись за каталог.
func (s *Store) writeFile(name string, m *mapfmt.Map) error {
	if err := s.checkPath(name); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("mapstore: сериализация карты: %w", err)
	}
	data = append(data, '\n')

	if err := s.checkDir(); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.dir, ".mapstore-*")
	if err != nil {
		return fmt.Errorf("mapstore: временный файл в %s: %w", s.dir, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op после успешного rename

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("mapstore: запись %s: %w", tmpName, err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("mapstore: fsync %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("mapstore: закрытие %s: %w", tmpName, err)
	}
	// CreateTemp ставит 0600; карта — данные пользователя, обычная видимость.
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return fmt.Errorf("mapstore: права %s: %w", tmpName, err)
	}
	if err := s.checkDir(); err != nil {
		return err
	}
	full := filepath.Join(s.dir, name)
	if err := os.Rename(tmpName, full); err != nil {
		return fmt.Errorf("mapstore: %s: %w", full, err)
	}
	return nil
}
