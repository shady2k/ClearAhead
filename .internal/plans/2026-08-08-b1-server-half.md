# В1: серверная половина — карта на экране Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use beads-superpowers:subagent-driven-development (recommended) or beads-superpowers:executing-plans to implement this plan task-by-task. Each Task becomes a bead (`bd create -t task --parent <epic-id>`). Steps within tasks use checkbox (`- [ ]`) syntax for human readability.

**Goal:** Go-сервер читает рукописный `map.json` одной станции, компилирует его в `CompiledTrack` и `RenderGeometry` и отдаёт геометрию по HTTP с `ETag` и `Cache-Control: immutable`.

**Architecture:** Три пакета над уже написанными `internal/units` и `internal/geom`. `internal/mapfmt` — строгий разбор файла (контракт документа и числовой контракт). `internal/track` — компилятор: вертикальный профиль и отображение `u ↔ s`, распространение поз от якоря, замыкание циклов, компиляция в два артефакта, хеши и манифест. `internal/httpapi` — ручка геометрии. Данные текут в одну сторону: файл → проверенная модель → скомпилированные артефакты → HTTP.

**Tech Stack:** Go 1.26.5, модуль `github.com/shady2k/ClearAhead`. Только стандартная библиотека. Тесты — `go test`, golden-вектора как неизменяемые фикстуры.

## Global Constraints

- Спека: `.internal/specs/2026-08-08-map-format-design.md` (ревизия 2). При расхождении плана со спекой прав документ спеки.
- **`u` — авторский пикетаж вдоль горизонтальной проекции, метры, float, только в файле и в разборе. `s` — пространственная длина оси, целые микрометры, всё после компиляции.** `TrackPos` и `TrackSpan` живут в `s`. Смешивать запрещено; имена переменных и полей несут букву (`lengthU`, `lengthS`).
- Float запрещён в состоянии сима. В `mapfmt` и `geom` он разрешён как presentation data.
- Округление: к ближайшему, половины от нуля (`math.Round`, уже в `units.MetersToDistance`). Длина элемента — **сумма индивидуально округлённых длин примитивов**, а не округление суммы.
- Углы в радианах, уклон в файле в промилле (поле `slope_permille`), после разбора — безразмерное `dz/du`.
- Допуски замыкания: **1 мм** по положению и `z`, **1e-5 рад** по heading, **1e-4** по безразмерному уклону.
- Валидатор **отказывает, а не чинит**. Подгонки невязки нет нигде.
- Всё неизвестное — отказ: неизвестное поле, неизвестный `kind`, неизвестная `format_version`.
- Ошибки — по-русски, с указанием ID элемента и величины невязки, потому что читать их будет автор карты.
- **Внешний вход попадает в обработчик только через `internal/rpc`** (Задача 9). Обработчик не видит ни пути, ни тела, ни сырых байтов — только разобранный тип запроса. Обойти барьер нельзя по типам, а остаток закрывает тест на AST.
- **Ничто не блокируется на вводе-выводе.** На сервере: обработчик не делает работы, зависящей от диска или сети — всё, что можно посчитать, считается при старте и лежит готовым в памяти. Когда в В2 появится тик, правило ужесточается: цикл тика не ждёт сети никогда, команды кладутся в очередь и разбираются фазой тика. На клиенте: HTTP только асинхронный, главный поток Godot не ждёт ответа ни при каких условиях (правило клиентского плана, здесь записано, чтобы не потерялось).
- `internal/geom` и `internal/units` не переписываются. Единственная правка в `geom` — комментарий к `Primitive.Length` (Задача 1).

---

## Файловая структура

| Файл | Ответственность |
|---|---|
| `internal/geom/geom.go` | *правка комментария*: `Length` — длина в плане (`u`), не по оси пути |
| `internal/mapfmt/schema.go` | типы файла карты; ничего не делает, только описывает |
| `internal/mapfmt/limits.go` | лимиты парсера одним местом |
| `internal/mapfmt/decode.go` | строгий разбор: дубликаты ключей, глубина, конечность чисел |
| `internal/mapfmt/validate.go` | проверки формы: ID, домены выравниваний, ссылки |
| `internal/track/profile.go` | вертикальный профиль, `u → s` в замкнутой форме |
| `internal/track/propagate.go` | распространение поз от якоря, замыкание циклов |
| `internal/track/compile.go` | `CompiledTrack`, `RenderGeometry`, перевод объектов в `s` |
| `internal/track/hash.go` | нормализованная модель, хеши, манифест ревизии |
| `internal/httpapi/geometry.go` | `GET /maps/{id}/revisions/{n}/geometry` |
| `cmd/clearahead/main.go` | бинарь: загрузить карту, поднять сервер |
| `maps/st_a.json` | станция руками: четыре пути, две горловины, тупик, подъездной |

Разделение внутри `track` — по этапам конвейера: профиль ничего не знает о графе, распространение ничего не знает о хешах. Каждый файл читается отдельно.

---

## Задача 1: композиция поз и правка комментария в `geom`

**Files:**
- Modify: `internal/geom/geom.go:39`
- Modify: `internal/geom/geom.go` — добавить `Compose` и `Invert`
- Test: `internal/geom/geom_test.go` — дописать тесты композиции

**Interfaces:**
- Consumes: —
- Produces: `func Compose(a, b Pose) Pose`, `func Invert(p Pose) Pose`

**Acceptance Criteria:**
- Комментарий к `Primitive.Length` называет величину длиной в плане (`u`), а не «длиной по оси пути».
- `Compose(p, Invert(p))` даёт нулевую позу с точностью 1e-9.
- `Chain.End(start) == Compose(start, Chain.End(нулевая поза))` для цепочки из прямых и дуг с точностью 1e-9.
- `go test ./...` проходит.

**Зачем `Compose` и `Invert`.** Обход графа приходит к элементу с обеих сторон.
Если известен конец, а нужно начало, наивный путь — прокрутить цепочку задом
наперёд, меняя знак угла у каждой дуги. Это требует правила разворота для
каждого вида примитива и даёт зеркальную горловину при ошибке в знаке.

Вместо этого используется свойство, которое у цепочки есть и так: **цепочка —
жёсткое движение плоскости.** Сколько бы в ней ни было звеньев, суммарно она
сдвигает и поворачивает. Значит:

```
Δ = Chain.End(нулевая поза)      // суммарное движение цепочки
конец = Compose(начало, Δ)        // ровно то, что делает Chain.End
начало = Compose(конец, Invert(Δ))
```

Правила для отдельных кривых не нужны: клотоида, когда придёт, уже учтена в `Δ`.

- [ ] **Шаг 1: Правка комментария**

В `internal/geom/geom.go` заменить строку 39:

```go
	Length units.Distance // длина по оси пути
```

на:

```go
	// Length — длина примитива В ПЛАНЕ, то есть по горизонтальной проекции.
	// Это координата u спеки формата. Пространственная длина оси s больше на
	// множитель sqrt(1+g²) и считается в internal/track/profile.go.
	Length units.Distance
```

Там же поправить комментарий к `Advance` (строка 75): «t измеряется вдоль оси пути» → «t измеряется в плане».

- [ ] **Шаг 2: Падающий тест композиции**

```go
func TestComposeInvertRoundTrip(t *testing.T) {
	for _, p := range []Pose{
		{X: 100, Y: -50, Heading: 0.7},
		{X: -3, Y: 0.5, Heading: -3.0},
		{X: 0, Y: 0, Heading: 0},
	} {
		got := Compose(p, Invert(p))
		if math.Abs(got.X) > 1e-9 || math.Abs(got.Y) > 1e-9 || math.Abs(got.Heading) > 1e-9 {
			t.Fatalf("Compose(%v, Invert(%v)) = %v, ожидалась нулевая поза", p, p, got)
		}
	}
}

// TestChainIsRigidMotion — то свойство, ради которого Compose существует:
// цепочка целиком есть одно движение, поэтому её можно применить и отменить,
// не разбирая на звенья.
func TestChainIsRigidMotion(t *testing.T) {
	a, err := Straight(300 * units.Meter)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Arc(300, -0.1107)
	if err != nil {
		t.Fatal(err)
	}
	c, err := Arc(300, 0.1107)
	if err != nil {
		t.Fatal(err)
	}
	chain := Chain{a, b, c}
	start := Pose{X: 17, Y: -4, Heading: 0.35}

	delta := chain.End(Pose{})
	direct := chain.End(start)
	viaCompose := Compose(start, delta)
	if math.Abs(direct.X-viaCompose.X) > 1e-9 ||
		math.Abs(direct.Y-viaCompose.Y) > 1e-9 ||
		math.Abs(direct.Heading-viaCompose.Heading) > 1e-9 {
		t.Fatalf("цепочка не жёсткое движение: прямо %v, через Compose %v", direct, viaCompose)
	}
	// И обратно: по концу восстанавливается начало.
	back := Compose(direct, Invert(delta))
	if math.Abs(back.X-start.X) > 1e-9 || math.Abs(back.Y-start.Y) > 1e-9 {
		t.Fatalf("восстановленное начало %v, ожидалось %v", back, start)
	}
}
```

Run: `go test ./internal/geom/ -run "TestCompose|TestChainIsRigid" -v`
Expected: FAIL, `undefined: Compose`

- [ ] **Шаг 3: Реализация**

```go
// Compose применяет движение b в системе координат позы a.
//
// Цепочка примитивов — жёсткое движение плоскости, поэтому Chain.End(start)
// тождественно равно Compose(start, Chain.End(нулевая поза)). Это свойство и
// позволяет восстанавливать начало элемента по его концу, не разворачивая
// цепочку по звеньям.
func Compose(a, b Pose) Pose {
	c, s := math.Cos(a.Heading), math.Sin(a.Heading)
	return Normalize(Pose{
		X:       a.X + b.X*c - b.Y*s,
		Y:       a.Y + b.X*s + b.Y*c,
		Heading: a.Heading + b.Heading,
	})
}

// Invert возвращает обратное движение: Compose(p, Invert(p)) — нулевая поза.
func Invert(p Pose) Pose {
	c, s := math.Cos(p.Heading), math.Sin(p.Heading)
	return Normalize(Pose{
		X:       -(p.X*c + p.Y*s),
		Y:       p.X*s - p.Y*c,
		Heading: -p.Heading,
	})
}
```

- [ ] **Шаг 4: Тесты**

Run: `go test ./...`
Expected: ok, все пакеты

- [ ] **Шаг 5: Коммит**

```bash
git add internal/geom/
git commit -m "feat: композиция и обращение поз; Length - длина в плане [ClearAhead-0xc]"
```

---

## Задача 2: типы файла карты и лимиты

**Files:**
- Create: `internal/mapfmt/schema.go`
- Create: `internal/mapfmt/limits.go`

**Interfaces:**
- Consumes: —
- Produces: типы `Map`, `Georeference`, `Anchor`, `Topology`, `Node`, `Port`, `Turnout`, `TurnoutPorts`, `Edge`, `Trackside`, `SpanInterval`, `Geometry`, `TurnoutGeometry`, `Alignments`, `HPrim`, `VPrim`; константы лимитов.

**Acceptance Criteria:**
- Типы покрывают пример из §11 спеки целиком.
- Ни один тип не содержит логики; файл только описывает форму.
- `go build ./...` проходит.

- [ ] **Шаг 1: `limits.go`**

```go
// Package mapfmt — форма файла карты, строгий разбор и проверки формы.
//
// Здесь живёт float в метрах и радианах: это авторские данные. Всё, что уходит
// дальше в компилятор, переводится в целые микрометры (units.Distance).
//
// Формат внутренний: читатель один, всё неизвестное — отказ. Согласования
// возможностей и минорных версий нет (спека §2, правило 2).
package mapfmt

// FormatVersion — единственная поддерживаемая версия формата.
const FormatVersion = 1

// Лимиты парсера. Карта — недоверенный вход: она может прийти рукой, старой
// версией редактора, импортёром или, позже, командой от клиента.
const (
	MaxDocumentBytes = 32 << 20 // 32 МиБ
	MaxNestingDepth  = 32
	MaxIDLength      = 128
	MaxElements      = 100000
	MaxPrimitives    = 1000000
)
```

- [ ] **Шаг 2: `schema.go`**

```go
package mapfmt

// Map — файл карты как он записан. Метры, радианы, промилле.
type Map struct {
	FormatVersion int               `json:"format_version"`
	MapID         string            `json:"map_id"`
	MapRevision   int               `json:"map_revision"`
	Georeference  *Georeference     `json:"georeference,omitempty"`
	Anchors       map[string]Anchor `json:"anchors"`
	Topology      Topology          `json:"topology"`
	Geometry      Geometry          `json:"geometry"`
}

// Georeference — метаданные привязки. В В1 сохраняется и проверяется по форме,
// компилятором не используется (спека §4).
type Georeference struct {
	Datum            string            `json:"datum"`
	Origin           Origin            `json:"origin"`
	OriginHeightKind string            `json:"origin_height_kind"`
	XAxisAzimuthDeg  float64           `json:"x_axis_azimuth_deg"`
	GroundToGrid     float64           `json:"ground_to_grid"`
	Provenance       map[string]string `json:"provenance,omitempty"`
}

type Origin struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
	H   float64 `json:"h"`
}

// Anchor — единственная абсолютная поза связной компоненты. Позы остальных
// портов выводятся распространением (спека §7).
type Anchor struct {
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
	Z       float64 `json:"z"`
	Heading float64 `json:"heading"`
}

type Topology struct {
	Nodes     []Node      `json:"nodes"`
	Turnouts  []Turnout   `json:"turnouts"`
	Edges     []Edge      `json:"edges"`
	Trackside []Trackside `json:"trackside"`
}

type Node struct {
	ID    string `json:"id"`
	Ports []Port `json:"ports"`
}

// Port.Purpose пуст у обычного порта. "buffer_stop" и "map_boundary" делают
// висящее ребро законным (спека §10.3).
type Port struct {
	ID      string `json:"id"`
	Purpose string `json:"purpose,omitempty"`
}

type Turnout struct {
	ID    string       `json:"id"`
	Hand  string       `json:"hand"`
	Frog  string       `json:"frog,omitempty"`
	Ports TurnoutPorts `json:"ports"`
}

type TurnoutPorts struct {
	Common    string `json:"common"`
	Straight  string `json:"straight"`
	Diverging string `json:"diverging"`
}

type Edge struct {
	ID   string `json:"id"`
	From string `json:"from"`
	To   string `json:"to"`
}

type Trackside struct {
	ID   string         `json:"id"`
	Kind string         `json:"kind"`
	Span []SpanInterval `json:"span"`
	Side string         `json:"side,omitempty"`
}

// SpanInterval — интервал в координате u на одном элементе. Список интервалов
// позволяет объекту пересекать границы элементов и проходы стрелок.
type SpanInterval struct {
	Element string  `json:"element"`
	From    float64 `json:"from"`
	To      float64 `json:"to"`
}

type Geometry struct {
	Turnouts map[string]TurnoutGeometry `json:"turnouts"`
	Edges    map[string]Alignments      `json:"edges"`
}

type TurnoutGeometry struct {
	Straight  Alignments `json:"straight"`
	Diverging Alignments `json:"diverging"`
}

// Alignments — три функции над общим доменом [0, U]. Cant отложен целиком и
// поля под него не заводятся (спека §6).
type Alignments struct {
	Horizontal []HPrim `json:"horizontal"`
	Vertical   []VPrim `json:"vertical,omitempty"`
}

// HPrim — примитив плана. Kind: "straight" | "arc".
type HPrim struct {
	Kind   string  `json:"kind"`
	Length float64 `json:"length,omitempty"`
	Radius float64 `json:"radius,omitempty"`
	Angle  float64 `json:"angle,omitempty"`
}

// VPrim — примитив профиля. Kind: "grade" | "vertical_curve".
//
// У grade постоянный уклон SlopePermille. У vertical_curve уклон меняется по u
// линейно от уклона предыдущего элемента до EndSlopePermille — это парабола.
type VPrim struct {
	Kind             string  `json:"kind"`
	Length           float64 `json:"length"`
	SlopePermille    float64 `json:"slope_permille,omitempty"`
	EndSlopePermille float64 `json:"end_slope_permille,omitempty"`
}
```

- [ ] **Шаг 3: Сборка**

Run: `go build ./...`
Expected: без вывода

- [ ] **Шаг 4: Коммит**

```bash
git add internal/mapfmt/
git commit -m "feat: типы файла карты и лимиты парсера [ClearAhead-0xc]"
```

---

## Задача 3: строгий разбор

**Files:**
- Create: `internal/mapfmt/decode.go`
- Test: `internal/mapfmt/decode_test.go`

**Interfaces:**
- Consumes: `Map`, лимиты из Задачи 2.
- Produces: `func Decode(r io.Reader) (*Map, error)`

**Acceptance Criteria:**
- Дублирующийся ключ JSON — отказ.
- Неизвестное поле — отказ.
- Превышение глубины вложенности — отказ.
- Документ больше `MaxDocumentBytes` — отказ.
- Нечисло (`1e400`, `NaN` через любой путь) — отказ.
- Неизвестная `format_version` — отказ.
- Корректный минимальный документ разбирается.

Дублирующиеся ключи проверяются отдельным проходом: `encoding/json` их молча
принимает и оставляет последний, а разные библиотеки выбирают по-разному — карта
начала бы зависеть от версии парсера.

- [ ] **Шаг 1: Падающий тест**

```go
package mapfmt

import (
	"strings"
	"testing"
)

const minimalMap = `{
  "format_version": 1,
  "map_id": "T",
  "map_revision": 1,
  "anchors": { "N1.P1": { "x": 0, "y": 0, "z": 0, "heading": 0 } },
  "topology": {
    "nodes": [
      { "id": "N1", "ports": [ { "id": "P1", "purpose": "map_boundary" } ] },
      { "id": "N2", "ports": [ { "id": "P1", "purpose": "buffer_stop" } ] }
    ],
    "turnouts": [], "trackside": [],
    "edges": [ { "id": "E1", "from": "N1.P1", "to": "N2.P1" } ]
  },
  "geometry": {
    "turnouts": {},
    "edges": { "E1": { "horizontal": [ { "kind": "straight", "length": 100.0 } ] } }
  }
}`

func TestDecodeMinimal(t *testing.T) {
	m, err := Decode(strings.NewReader(minimalMap))
	if err != nil {
		t.Fatalf("разбор минимальной карты: %v", err)
	}
	if m.MapID != "T" || len(m.Topology.Edges) != 1 {
		t.Fatalf("разобрано неверно: %+v", m)
	}
}

func TestDecodeRejects(t *testing.T) {
	cases := []struct{ name, doc, want string }{
		{"дубликат ключа", `{"map_id":"A","map_id":"B"}`, "дублирующийся ключ"},
		{"неизвестное поле", `{"format_version":1,"nope":1}`, "неизвестн"},
		{"не число", `{"format_version":1,"map_revision":1,"map_id":"T",
			"anchors":{"N1.P1":{"x":1e400,"y":0,"z":0,"heading":0}},
			"topology":{"nodes":[],"turnouts":[],"edges":[],"trackside":[]},
			"geometry":{"turnouts":{},"edges":{}}}`, ""},
		{"чужая версия", `{"format_version":2}`, "версия"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := Decode(strings.NewReader(c.doc)); err == nil {
				t.Fatal("ожидался отказ, получен успех")
			} else if c.want != "" && !strings.Contains(err.Error(), c.want) {
				t.Fatalf("ожидалась ошибка про %q, получено: %v", c.want, err)
			}
		})
	}
}

func TestDecodeDepthLimit(t *testing.T) {
	doc := strings.Repeat(`{"a":`, MaxNestingDepth+2) + `1` + strings.Repeat(`}`, MaxNestingDepth+2)
	if _, err := Decode(strings.NewReader(doc)); err == nil {
		t.Fatal("ожидался отказ по глубине")
	}
}
```

- [ ] **Шаг 2: Убедиться, что тест падает**

Run: `go test ./internal/mapfmt/ -run TestDecode -v`
Expected: FAIL, `undefined: Decode`

- [ ] **Шаг 3: Реализация**

```go
package mapfmt

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"reflect"
)

// Decode разбирает файл карты строго: всё неизвестное и всё неоднозначное —
// отказ. Три прохода по одному буферу, потому что каждый проверяет своё и
// смешивать их в один означало бы получать невнятные ошибки.
func Decode(r io.Reader) (*Map, error) {
	raw, err := io.ReadAll(io.LimitReader(r, MaxDocumentBytes+1))
	if err != nil {
		return nil, fmt.Errorf("mapfmt: чтение: %w", err)
	}
	if len(raw) > MaxDocumentBytes {
		return nil, fmt.Errorf("mapfmt: документ больше %d байт", MaxDocumentBytes)
	}

	if err := checkTokens(raw); err != nil {
		return nil, err
	}

	var m Map
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("mapfmt: разбор: %w", err)
	}
	if dec.More() {
		return nil, fmt.Errorf("mapfmt: после документа есть лишние данные")
	}

	if m.FormatVersion != FormatVersion {
		return nil, fmt.Errorf("mapfmt: версия формата %d не поддерживается, ожидается %d",
			m.FormatVersion, FormatVersion)
	}
	if err := checkFinite(reflect.ValueOf(m), "map"); err != nil {
		return nil, err
	}
	return &m, nil
}

// checkTokens ловит дублирующиеся ключи и превышение глубины. encoding/json
// принимает дубликаты молча и оставляет последний; разные библиотеки выбирают
// по-разному, и карта начала бы зависеть от версии парсера.
func checkTokens(raw []byte) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()

	type frame struct {
		isObject bool
		keys     map[string]bool
		pending  string
	}
	var stack []frame

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("mapfmt: разбор: %w", err)
		}
		switch t := tok.(type) {
		case json.Delim:
			switch t {
			case '{':
				stack = append(stack, frame{isObject: true, keys: map[string]bool{}})
			case '[':
				stack = append(stack, frame{})
			case '}', ']':
				stack = stack[:len(stack)-1]
				continue
			}
			if len(stack) > MaxNestingDepth {
				return fmt.Errorf("mapfmt: глубина вложенности больше %d", MaxNestingDepth)
			}
		case string:
			// Ключ объекта отличается от строкового значения тем, что стоит на
			// нечётной позиции внутри объекта; json.Decoder отдаёт их вперемешку,
			// поэтому состояние держим сами.
			top := &stack[len(stack)-1]
			if top.isObject && top.pending == "" {
				if top.keys[t] {
					return fmt.Errorf("mapfmt: дублирующийся ключ %q", t)
				}
				top.keys[t] = true
				top.pending = t
				continue
			}
		}
		if len(stack) > 0 {
			stack[len(stack)-1].pending = ""
		}
	}
}

// checkFinite обходит разобранную модель и требует, чтобы каждое float-поле
// было конечным. Стандартный разборщик Go возвращает ошибку на 1e400, но
// спецификация не должна зависеть от поведения библиотеки.
func checkFinite(v reflect.Value, path string) error {
	switch v.Kind() {
	case reflect.Float64, reflect.Float32:
		if f := v.Float(); math.IsNaN(f) || math.IsInf(f, 0) {
			return fmt.Errorf("mapfmt: %s: не конечное число %v", path, f)
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			name := v.Type().Field(i).Name
			if err := checkFinite(v.Field(i), path+"."+name); err != nil {
				return err
			}
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			if err := checkFinite(v.Index(i), fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	case reflect.Map:
		for _, k := range v.MapKeys() {
			if err := checkFinite(v.MapIndex(k), fmt.Sprintf("%s[%v]", path, k)); err != nil {
				return err
			}
		}
	case reflect.Pointer:
		if !v.IsNil() {
			return checkFinite(v.Elem(), path)
		}
	}
	return nil
}
```

- [ ] **Шаг 4: Тесты проходят**

Run: `go test ./internal/mapfmt/ -run TestDecode -v`
Expected: PASS по всем подтестам

- [ ] **Шаг 5: Коммит**

```bash
git add internal/mapfmt/
git commit -m "feat: строгий разбор карты - дубликаты ключей, глубина, конечность [ClearAhead-0xc]"
```

---

## Задача 4: проверки формы

**Files:**
- Create: `internal/mapfmt/validate.go`
- Test: `internal/mapfmt/validate_test.go`

**Interfaces:**
- Consumes: `*Map` из `Decode`.
- Produces: `func Validate(m *Map) error`; `func (m *Map) ElementIDs() []string`; `func (m *Map) Alignments(elementID string) (Alignments, bool)`

**Acceptance Criteria:**
- ID непустые, уникальные, не длиннее `MaxIDLength`.
- Домены `horizontal` и `vertical` совпадают в пределах 1 мкм.
- Первый элемент вертикальной цепочки — `grade`.
- Неизвестный `kind` — отказ.
- Вырожденная геометрия (нулевая длина, нулевой радиус, пустая цепочка) — отказ.
- Ссылка `LinearElementRef` на несуществующий элемент — отказ; смещение вне `[0, U]` — отказ.
- Висящее ребро без `purpose` у порта — отказ; с `buffer_stop` или `map_boundary` — принимается.
- Ровно один якорь на связную компоненту; ноль или два — отказ.
- Блок геопривязки, если он есть, проверяется по форме: широта в [−90, 90],
  долгота в [−180, 180], азимут в [0, 360), `ground_to_grid` > 0,
  `origin_height_kind` — `ellipsoidal` или `orthometric`, датум непустой.
  Спека §4 требует «сохраняется и проверяется по форме»; без проверки «проверяется»
  было бы неправдой.

**Именование проходов стрелки:** проход адресуется как `<turnout_id>:straight` и
`<turnout_id>:diverging`. Это ID линейного элемента наравне с ID ребра.

- [ ] **Шаг 1: Падающий тест**

```go
package mapfmt

import (
	"strings"
	"testing"
)

func decodeOK(t *testing.T, doc string) *Map {
	t.Helper()
	m, err := Decode(strings.NewReader(doc))
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	return m
}

func TestValidateMinimal(t *testing.T) {
	if err := Validate(decodeOK(t, minimalMap)); err != nil {
		t.Fatalf("минимальная карта должна быть валидна: %v", err)
	}
}

func TestValidateRejects(t *testing.T) {
	cases := []struct{ name, doc, want string }{
		{
			"домены не совпадают",
			strings.Replace(minimalMap,
				`"horizontal": [ { "kind": "straight", "length": 100.0 } ]`,
				`"horizontal": [ { "kind": "straight", "length": 100.0 } ],
				 "vertical": [ { "kind": "grade", "length": 90.0, "slope_permille": 0 } ]`, 1),
			"домен",
		},
		{
			"вертикаль начинается не с grade",
			strings.Replace(minimalMap,
				`"horizontal": [ { "kind": "straight", "length": 100.0 } ]`,
				`"horizontal": [ { "kind": "straight", "length": 100.0 } ],
				 "vertical": [ { "kind": "vertical_curve", "length": 100.0, "end_slope_permille": 5 } ]`, 1),
			"grade",
		},
		{
			"нулевая длина",
			strings.Replace(minimalMap, `"length": 100.0`, `"length": 0.0`, 1),
			"длина",
		},
		{
			"неизвестный kind",
			strings.Replace(minimalMap, `"kind": "straight"`, `"kind": "spiral"`, 1),
			"неизвестный",
		},
		{
			"висящее ребро без назначения",
			strings.Replace(minimalMap, `"purpose": "buffer_stop"`, `"purpose": ""`, 1),
			"висящ",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, err := Decode(strings.NewReader(c.doc))
			if err != nil {
				if !strings.Contains(err.Error(), c.want) {
					t.Fatalf("отказ на разборе не про то: %v", err)
				}
				return
			}
			err = Validate(m)
			if err == nil {
				t.Fatal("ожидался отказ, получен успех")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("ожидалась ошибка про %q, получено: %v", c.want, err)
			}
		})
	}
}
```

- [ ] **Шаг 2: Убедиться, что тест падает**

Run: `go test ./internal/mapfmt/ -run TestValidate -v`
Expected: FAIL, `undefined: Validate`

- [ ] **Шаг 3: Реализация**

```go
package mapfmt

import (
	"fmt"
	"math"
	"strings"

	"github.com/shady2k/ClearAhead/internal/units"
)

// domainEpsilon — допуск на совпадение доменов трёх выравниваний. Один
// микрометр: домены задаются автором в одних и тех же метрах, расхождение
// больше округления означает опечатку.
const domainEpsilon = units.Micrometer

// PassageStraight и PassageDiverging — суффиксы ID проходов стрелки. Проход —
// адресуемый линейный элемент наравне с ребром (спека §8).
const (
	PassageStraight  = ":straight"
	PassageDiverging = ":diverging"
)

// Validate проверяет форму карты. Отказывает, не чинит.
func Validate(m *Map) error {
	if m.MapID == "" || len(m.MapID) > MaxIDLength {
		return fmt.Errorf("mapfmt: map_id пуст или длиннее %d", MaxIDLength)
	}
	if m.MapRevision < 1 {
		return fmt.Errorf("mapfmt: map_revision должен быть положительным, получено %d", m.MapRevision)
	}

	ports, err := m.collectPorts()
	if err != nil {
		return err
	}
	elements, err := m.collectElements()
	if err != nil {
		return err
	}
	if len(elements) > MaxElements {
		return fmt.Errorf("mapfmt: элементов больше %d", MaxElements)
	}

	for id, a := range m.AllAlignments() {
		if err := validateAlignments(id, a); err != nil {
			return err
		}
	}
	if err := m.validateEdgeEnds(ports); err != nil {
		return err
	}
	if err := m.validateTrackside(elements); err != nil {
		return err
	}
	if err := validateGeoreference(m.Georeference); err != nil {
		return err
	}
	return m.validateAnchors(ports)
}

// validateGeoreference проверяет блок привязки по форме. Компилятор его не
// использует (спека §4), но принимать заведомо бессмысленные числа и хранить их
// до того дня, когда они понадобятся, — способ получить неверную карту молча.
func validateGeoreference(g *Georeference) error {
	if g == nil {
		return nil
	}
	if g.Datum == "" {
		return fmt.Errorf("mapfmt: геопривязка: пустой датум")
	}
	switch g.OriginHeightKind {
	case "ellipsoidal", "orthometric":
	default:
		return fmt.Errorf("mapfmt: геопривязка: origin_height_kind должен быть ellipsoidal или orthometric, получено %q",
			g.OriginHeightKind)
	}
	if g.Origin.Lat < -90 || g.Origin.Lat > 90 {
		return fmt.Errorf("mapfmt: геопривязка: широта %v вне [-90, 90]", g.Origin.Lat)
	}
	if g.Origin.Lon < -180 || g.Origin.Lon > 180 {
		return fmt.Errorf("mapfmt: геопривязка: долгота %v вне [-180, 180]", g.Origin.Lon)
	}
	if g.XAxisAzimuthDeg < 0 || g.XAxisAzimuthDeg >= 360 {
		return fmt.Errorf("mapfmt: геопривязка: азимут %v вне [0, 360)", g.XAxisAzimuthDeg)
	}
	if !(g.GroundToGrid > 0) {
		return fmt.Errorf("mapfmt: геопривязка: ground_to_grid должен быть положительным, получено %v", g.GroundToGrid)
	}
	return nil
}

func validateAlignments(id string, a Alignments) error {
	if len(a.Horizontal) == 0 {
		return fmt.Errorf("mapfmt: %s: пустая горизонтальная цепочка", id)
	}
	nPrim := len(a.Horizontal)
	for i, p := range a.Horizontal {
		switch p.Kind {
		case "straight":
			if !(p.Length > 0) {
				return fmt.Errorf("mapfmt: %s[%d]: длина прямой должна быть положительной, получено %v", id, i, p.Length)
			}
		case "arc":
			if !(p.Radius > 0) {
				return fmt.Errorf("mapfmt: %s[%d]: радиус дуги должен быть положительным, получено %v", id, i, p.Radius)
			}
			if p.Angle == 0 || math.Abs(p.Angle) > 2*math.Pi {
				return fmt.Errorf("mapfmt: %s[%d]: угол дуги %v вне (0, 2π]", id, i, p.Angle)
			}
		default:
			return fmt.Errorf("mapfmt: %s[%d]: неизвестный примитив плана %q", id, i, p.Kind)
		}
	}
	// Длина считается единственной функцией — той же, что зовёт validateTrackside.
	uH, err := horizontalLengthU(a)
	if err != nil {
		return fmt.Errorf("mapfmt: %s: %w", id, err)
	}

	if len(a.Vertical) == 0 {
		return nil
	}
	if a.Vertical[0].Kind != "grade" {
		return fmt.Errorf("mapfmt: %s: первый элемент вертикальной цепочки обязан быть grade — он задаёт начальный уклон", id)
	}
	var uV units.Distance
	for i, p := range a.Vertical {
		nPrim++
		switch p.Kind {
		case "grade", "vertical_curve":
		default:
			return fmt.Errorf("mapfmt: %s: неизвестный примитив профиля %q", id, p.Kind)
		}
		d, err := units.MetersToDistance(p.Length)
		if err != nil || d <= 0 {
			return fmt.Errorf("mapfmt: %s: вертикаль[%d]: длина должна быть положительной, получено %v", id, i, p.Length)
		}
		uV += d
	}
	if diff := uH - uV; diff > domainEpsilon || diff < -domainEpsilon {
		return fmt.Errorf("mapfmt: %s: домены выравниваний не совпадают: план %s, профиль %s", id, uH, uV)
	}
	if nPrim > MaxPrimitives {
		return fmt.Errorf("mapfmt: %s: примитивов больше %d", id, MaxPrimitives)
	}
	return nil
}

// AllAlignments возвращает выравнивания всех линейных элементов — рёбер и
// проходов стрелок — под их ID.
func (m *Map) AllAlignments() map[string]Alignments {
	out := make(map[string]Alignments, len(m.Geometry.Edges)+2*len(m.Geometry.Turnouts))
	for id, a := range m.Geometry.Edges {
		out[id] = a
	}
	for id, tg := range m.Geometry.Turnouts {
		out[id+PassageStraight] = tg.Straight
		out[id+PassageDiverging] = tg.Diverging
	}
	return out
}

// Alignments возвращает выравнивания одного элемента.
func (m *Map) Alignments(elementID string) (Alignments, bool) {
	a, ok := m.AllAlignments()[elementID]
	return a, ok
}

// ElementIDs возвращает ID всех линейных элементов.
func (m *Map) ElementIDs() []string {
	all := m.AllAlignments()
	ids := make([]string, 0, len(all))
	for id := range all {
		ids = append(ids, id)
	}
	return ids
}

func (m *Map) collectPorts() (map[string]Port, error) {
	ports := map[string]Port{}
	add := func(full string, p Port) error {
		if p.ID == "" || len(full) > MaxIDLength {
			return fmt.Errorf("mapfmt: порт %q: пустой или слишком длинный ID", full)
		}
		if _, dup := ports[full]; dup {
			return fmt.Errorf("mapfmt: порт %q объявлен дважды", full)
		}
		ports[full] = p
		return nil
	}
	for _, n := range m.Topology.Nodes {
		if n.ID == "" {
			return nil, fmt.Errorf("mapfmt: узел с пустым ID")
		}
		for _, p := range n.Ports {
			switch p.Purpose {
			case "", "buffer_stop", "map_boundary":
			default:
				return nil, fmt.Errorf("mapfmt: порт %s.%s: неизвестное назначение %q", n.ID, p.ID, p.Purpose)
			}
			if err := add(n.ID+"."+p.ID, p); err != nil {
				return nil, err
			}
		}
	}
	for _, t := range m.Topology.Turnouts {
		if t.Hand != "left" && t.Hand != "right" {
			return nil, fmt.Errorf("mapfmt: стрелка %s: рукость должна быть left или right, получено %q", t.ID, t.Hand)
		}
		for _, p := range []string{t.Ports.Common, t.Ports.Straight, t.Ports.Diverging} {
			if p == "" {
				return nil, fmt.Errorf("mapfmt: стрелка %s: не заняты все три порта", t.ID)
			}
			if err := add(t.ID+"."+p, Port{ID: p}); err != nil {
				return nil, err
			}
		}
	}
	return ports, nil
}

func (m *Map) collectElements() (map[string]bool, error) {
	els := map[string]bool{}
	for _, e := range m.Topology.Edges {
		if e.ID == "" {
			return nil, fmt.Errorf("mapfmt: ребро с пустым ID")
		}
		if els[e.ID] {
			return nil, fmt.Errorf("mapfmt: ребро %q объявлено дважды", e.ID)
		}
		els[e.ID] = true
		if _, ok := m.Geometry.Edges[e.ID]; !ok {
			return nil, fmt.Errorf("mapfmt: у ребра %s нет геометрии", e.ID)
		}
	}
	for _, t := range m.Topology.Turnouts {
		if _, ok := m.Geometry.Turnouts[t.ID]; !ok {
			return nil, fmt.Errorf("mapfmt: у стрелки %s нет геометрии", t.ID)
		}
		els[t.ID+PassageStraight] = true
		els[t.ID+PassageDiverging] = true
	}
	for id := range m.Geometry.Edges {
		if !els[id] {
			return nil, fmt.Errorf("mapfmt: геометрия ребра %s без топологии", id)
		}
	}
	return els, nil
}

// validateEdgeEnds требует, чтобы каждый порт был либо занят ребром, либо нёс
// назначение, делающее висящий конец законным. Безусловной связности не
// требуется: изолированное депо или вторая станция законны (спека §10.3).
func (m *Map) validateEdgeEnds(ports map[string]Port) error {
	used := map[string]int{}
	for _, e := range m.Topology.Edges {
		for _, end := range []string{e.From, e.To} {
			if _, ok := ports[end]; !ok {
				return fmt.Errorf("mapfmt: ребро %s ссылается на несуществующий порт %s", e.ID, end)
			}
			used[end]++
			// Два ребра в одном порту — это обычный стык, и именно там
			// проверяется замыкание. Три и больше — развилка, а развилка обязана
			// быть оформлена стрелкой: у неё есть длина, остряк и конфликт
			// маршрутов, которых у голого узла нет.
			if used[end] > 2 {
				return fmt.Errorf("mapfmt: порт %s обслуживает %d рёбер — развилку нужно оформить стрелкой", end, used[end])
			}
		}
	}
	for full, p := range ports {
		if used[full] > 0 {
			continue
		}
		if strings.Contains(full, ":") {
			continue
		}
		isTurnoutPort := false
		for _, t := range m.Topology.Turnouts {
			if strings.HasPrefix(full, t.ID+".") {
				isTurnoutPort = true
			}
		}
		if isTurnoutPort {
			return fmt.Errorf("mapfmt: порт стрелки %s не соединён ребром", full)
		}
		if p.Purpose == "" {
			return fmt.Errorf("mapfmt: висящий конец в порту %s: нужен purpose buffer_stop или map_boundary", full)
		}
	}
	return nil
}

func (m *Map) validateTrackside(elements map[string]bool) error {
	all := m.AllAlignments()
	seen := map[string]bool{}
	for _, ts := range m.Topology.Trackside {
		if ts.ID == "" || seen[ts.ID] {
			return fmt.Errorf("mapfmt: путевой объект с пустым или повторным ID %q", ts.ID)
		}
		seen[ts.ID] = true
		switch ts.Kind {
		case "platform", "buffer_stop":
		default:
			return fmt.Errorf("mapfmt: путевой объект %s: неизвестный kind %q", ts.ID, ts.Kind)
		}
		if len(ts.Span) == 0 {
			return fmt.Errorf("mapfmt: путевой объект %s: пустой span", ts.ID)
		}
		for _, iv := range ts.Span {
			if !elements[iv.Element] {
				return fmt.Errorf("mapfmt: путевой объект %s ссылается на несуществующий элемент %s", ts.ID, iv.Element)
			}
			u, err := horizontalLengthU(all[iv.Element])
			if err != nil {
				return fmt.Errorf("mapfmt: путевой объект %s: длина элемента %s: %w", ts.ID, iv.Element, err)
			}
			from, err := units.MetersToDistance(iv.From)
			if err != nil {
				return fmt.Errorf("mapfmt: путевой объект %s: начало интервала: %w", ts.ID, err)
			}
			to, err := units.MetersToDistance(iv.To)
			if err != nil {
				return fmt.Errorf("mapfmt: путевой объект %s: конец интервала: %w", ts.ID, err)
			}
			if from < 0 || to > u || from > to {
				return fmt.Errorf("mapfmt: путевой объект %s: интервал [%v, %v] вне элемента %s длиной %s",
					ts.ID, iv.From, iv.To, iv.Element, u)
			}
		}
	}
	return nil
}

// horizontalLengthU — единственное место, где считается длина горизонтальной
// цепочки. И validateAlignments, и validateTrackside зовут её: два независимых
// расчёта одного и того же числа рано или поздно разойдутся.
//
// Правило округления спеки §3: сумма индивидуально округлённых длин.
func horizontalLengthU(a Alignments) (units.Distance, error) {
	var u units.Distance
	for i, p := range a.Horizontal {
		var (
			d   units.Distance
			err error
		)
		switch p.Kind {
		case "straight":
			d, err = units.MetersToDistance(p.Length)
		case "arc":
			d, err = units.MetersToDistance(p.Radius * math.Abs(p.Angle))
		default:
			return 0, fmt.Errorf("неизвестный примитив плана %q на позиции %d", p.Kind, i)
		}
		if err != nil {
			return 0, fmt.Errorf("примитив %d: %w", i, err)
		}
		if d <= 0 {
			return 0, fmt.Errorf("примитив %d: вырожденная длина", i)
		}
		u += d
	}
	return u, nil
}

// validateAnchors требует ровно один якорь на связную компоненту.
func (m *Map) validateAnchors(ports map[string]Port) error {
	if len(m.Anchors) == 0 {
		return fmt.Errorf("mapfmt: нет ни одного якоря")
	}
	for id := range m.Anchors {
		if _, ok := ports[id]; !ok {
			return fmt.Errorf("mapfmt: якорь ссылается на несуществующий порт %s", id)
		}
	}
	return nil
}
```

> Проверка «ровно один якорь на компоненту» завершается в Задаче 5: компоненты
> известны только после обхода графа. Здесь проверяется существование портов.

- [ ] **Шаг 4: Тесты проходят**

Run: `go test ./internal/mapfmt/ -v`
Expected: PASS

- [ ] **Шаг 5: Коммит**

```bash
git add internal/mapfmt/
git commit -m "feat: проверки формы карты - домены, kind, ссылки, висящие концы [ClearAhead-0xc]"
```

---

## Задача 5: вертикальный профиль и отображение `u → s`

**Files:**
- Create: `internal/track/profile.go`
- Test: `internal/track/profile_test.go`

**Interfaces:**
- Consumes: `mapfmt.Alignments`, `units.Distance`.
- Produces:
  - `type Segment struct { LengthU units.Distance; StartSlope, EndSlope float64 }`
  - `type Profile []Segment`
  - `func ProfileFrom(a mapfmt.Alignments, lengthU units.Distance) (Profile, error)`
  - `func (p Profile) LengthU() units.Distance`
  - `func (p Profile) LengthS() units.Distance`
  - `func (p Profile) UToS(u units.Distance) (units.Distance, error)`
  - `func (p Profile) At(u units.Distance) (dz float64, slope float64, err error)`

**Acceptance Criteria:**
- Плоский профиль: `s == u` точно.
- Постоянный уклон `g`: `s == u·√(1+g²)` в пределах микрометра.
- Параболическая кривая: `s` совпадает с замкнутой формой в пределах микрометра.
- `UToS` монотонно неубывает.
- Отсутствующая вертикальная цепочка даёт плоский профиль длиной `lengthU`.
- Golden-вектор: уклон 40 ‰ на километре даёт `s − u ≈ 0,7996 м` — то самое
  расхождение, ради которого различие введено.

**Замкнутая форма.** На отрезке с линейно меняющимся уклоном `g(u) = g₀ + k·u`:

```
s = ∫₀ᴸ √(1 + g²) du = (1/k)·[ (g·√(1+g²) + asinh(g)) / 2 ] от g₀ до g₁
```

при `k ≠ 0`, и `s = L·√(1+g₀²)` при `k = 0`. Численное интегрирование не нужно —
и это снимает единственное место спеки, где детерминизм держался бы на
дисциплине реализации, а не на математике.

- [ ] **Шаг 1: Падающий тест**

```go
package track

import (
	"math"
	"testing"

	"github.com/shady2k/ClearAhead/internal/mapfmt"
	"github.com/shady2k/ClearAhead/internal/units"
)

const um = units.Micrometer

func TestProfileFlat(t *testing.T) {
	p, err := ProfileFrom(mapfmt.Alignments{}, 1000*units.Meter)
	if err != nil {
		t.Fatalf("плоский профиль: %v", err)
	}
	got, err := p.LengthS()
	if err != nil {
		t.Fatalf("длина плоского профиля: %v", err)
	}
	if got != 1000*units.Meter {
		t.Fatalf("плоский профиль: s=%s, ожидалось %s", got, 1000*units.Meter)
	}
}

func TestProfileConstantGrade(t *testing.T) {
	a := mapfmt.Alignments{Vertical: []mapfmt.VPrim{
		{Kind: "grade", Length: 1000, SlopePermille: 40},
	}}
	p, err := ProfileFrom(a, 1000*units.Meter)
	if err != nil {
		t.Fatalf("уклон 40‰: %v", err)
	}
	want, _ := units.MetersToDistance(1000 * math.Sqrt(1+0.04*0.04))
	got, err := p.LengthS()
	if err != nil {
		t.Fatalf("длина: %v", err)
	}
	if absD(got-want) > um {
		t.Fatalf("уклон 40‰: s=%s, ожидалось %s", got, want)
	}
	// Golden: то самое расхождение, ради которого введены две координаты.
	if d := got - 1000*units.Meter; absD(d-799600*um) > 100*um {
		t.Fatalf("расхождение s-u на 40‰ и километре: %s, ожидалось ≈0.7996m", d)
	}
}

func TestProfileVerticalCurve(t *testing.T) {
	a := mapfmt.Alignments{Vertical: []mapfmt.VPrim{
		{Kind: "grade", Length: 100, SlopePermille: 0},
		{Kind: "vertical_curve", Length: 200, EndSlopePermille: 20},
		{Kind: "grade", Length: 100, SlopePermille: 20},
	}}
	p, err := ProfileFrom(a, 400*units.Meter)
	if err != nil {
		t.Fatalf("кривая: %v", err)
	}
	want := arcLen(0, 0) + arcLenLinear(0, 0.02, 200) + arcLen(0.02, 100)
	wd, _ := units.MetersToDistance(100 + want)
	got, err := p.LengthS()
	if err != nil {
		t.Fatalf("длина: %v", err)
	}
	if absD(got-wd) > 10*um {
		t.Fatalf("кривая: s=%s, ожидалось %s", got, wd)
	}
	dz, slope, err := p.At(400 * units.Meter)
	if err != nil {
		t.Fatalf("At в конце: %v", err)
	}
	if math.Abs(slope-0.02) > 1e-9 {
		t.Fatalf("уклон в конце %v, ожидалось 0.02", slope)
	}
	// Подъём: 0 на первых 100 м, среднее 0.01 на кривой, 0.02 на последних 100.
	if want := 0.0 + 0.01*200 + 0.02*100; math.Abs(dz-want) > 1e-6 {
		t.Fatalf("подъём %v м, ожидалось %v", dz, want)
	}
}

func TestProfileUToSMonotone(t *testing.T) {
	a := mapfmt.Alignments{Vertical: []mapfmt.VPrim{
		{Kind: "grade", Length: 100, SlopePermille: -10},
		{Kind: "vertical_curve", Length: 100, EndSlopePermille: 30},
	}}
	p, err := ProfileFrom(a, 200*units.Meter)
	if err != nil {
		t.Fatalf("профиль: %v", err)
	}
	prev := units.Distance(-1)
	for u := units.Distance(0); u <= 200*units.Meter; u += units.Meter {
		s, err := p.UToS(u)
		if err != nil {
			t.Fatalf("UToS(%s): %v", u, err)
		}
		if s < prev {
			t.Fatalf("UToS не монотонна: на %s получено %s после %s", u, s, prev)
		}
		prev = s
	}
}

func absD(d units.Distance) units.Distance {
	if d < 0 {
		return -d
	}
	return d
}

// arcLen — длина отрезка постоянного уклона на единицу длины (для читаемости
// теста; эталон считается независимой формулой, а не проверяемой функцией).
func arcLen(g float64, l float64) float64 { return l * math.Sqrt(1+g*g) }

// arcLenLinear — эталон для линейно меняющегося уклона, посчитанный по
// первообразной ∫√(1+g²)dg = (g√(1+g²) + asinh g)/2.
func arcLenLinear(g0, g1, l float64) float64 {
	k := (g1 - g0) / l
	f := func(g float64) float64 { return (g*math.Sqrt(1+g*g) + math.Asinh(g)) / 2 }
	return (f(g1) - f(g0)) / k
}
```

- [ ] **Шаг 2: Убедиться, что тест падает**

Run: `go test ./internal/track/ -run TestProfile -v`
Expected: FAIL, `undefined: ProfileFrom`

- [ ] **Шаг 3: Реализация**

```go
// Package track — компилятор пути: профиль, распространение поз, компиляция.
//
// Здесь встречаются две координаты вдоль пути и здесь же они расходятся:
//   u — авторский пикетаж вдоль горизонтальной проекции, приходит из mapfmt;
//   s — пространственная длина оси, уходит в CompiledTrack и в TrackPos.
// Правило имён: в этом пакете каждая величина длины несёт букву в имени.
package track

import (
	"fmt"
	"math"

	"github.com/shady2k/ClearAhead/internal/mapfmt"
	"github.com/shady2k/ClearAhead/internal/units"
)

// Segment — звено вертикальной цепочки. Уклон меняется по u линейно от
// StartSlope до EndSlope; равенство даёт постоянный уклон.
type Segment struct {
	LengthU    units.Distance
	StartSlope float64 // безразмерное dz/du
	EndSlope   float64
}

// Profile — вертикальное выравнивание элемента.
type Profile []Segment

// ProfileFrom строит профиль из вертикальной цепочки. Пустая цепочка даёт
// плоский профиль объявленной длины: станция В1 плоская, и это законный случай,
// а не отсутствие данных.
func ProfileFrom(a mapfmt.Alignments, lengthU units.Distance) (Profile, error) {
	if len(a.Vertical) == 0 {
		return Profile{{LengthU: lengthU}}, nil
	}
	p := make(Profile, 0, len(a.Vertical))
	slope := 0.0
	for i, v := range a.Vertical {
		l, err := units.MetersToDistance(v.Length)
		if err != nil {
			return nil, fmt.Errorf("track: вертикаль[%d]: %w", i, err)
		}
		switch v.Kind {
		case "grade":
			slope = v.SlopePermille / 1000
			p = append(p, Segment{LengthU: l, StartSlope: slope, EndSlope: slope})
		case "vertical_curve":
			end := v.EndSlopePermille / 1000
			p = append(p, Segment{LengthU: l, StartSlope: slope, EndSlope: end})
			slope = end
		default:
			return nil, fmt.Errorf("track: вертикаль[%d]: неизвестный примитив %q", i, v.Kind)
		}
	}
	return p, nil
}

// LengthU возвращает длину профиля в авторской координате.
func (p Profile) LengthU() units.Distance {
	var u units.Distance
	for _, s := range p {
		u += s.LengthU
	}
	return u
}

// LengthS возвращает пространственную длину оси.
//
// Правило округления: длина каждого сегмента округляется отдельно, сумма берётся
// по целым. Округление математической суммы дало бы другой результат, и сумма
// частей перестала бы равняться целому.
//
// Ошибка возвращается, а не проглатывается: частичная сумма выглядит как
// правдоподобная длина, и поезд поехал бы по ней, не заметив.
func (p Profile) LengthS() (units.Distance, error) {
	var s units.Distance
	for i, seg := range p {
		d, err := units.MetersToDistance(seg.spatialLen())
		if err != nil {
			return 0, fmt.Errorf("track: сегмент профиля %d: %w", i, err)
		}
		s += d
	}
	return s, nil
}

// UToS переводит авторское смещение в пространственное.
func (p Profile) UToS(u units.Distance) (units.Distance, error) {
	if u < 0 {
		return 0, fmt.Errorf("track: отрицательное смещение %s", u)
	}
	var s units.Distance
	left := u
	for _, seg := range p {
		if left <= seg.LengthU {
			head := Segment{
				LengthU:    left,
				StartSlope: seg.StartSlope,
				EndSlope:   seg.slopeAt(left),
			}
			d, err := units.MetersToDistance(head.spatialLen())
			if err != nil {
				return 0, err
			}
			return s + d, nil
		}
		left -= seg.LengthU
		d, err := units.MetersToDistance(seg.spatialLen())
		if err != nil {
			return 0, err
		}
		s += d
	}
	return 0, fmt.Errorf("track: смещение %s выходит за длину профиля %s", u, p.LengthU())
}

// At возвращает подъём от начала профиля и уклон на смещении u.
func (p Profile) At(u units.Distance) (float64, float64, error) {
	if u < 0 {
		return 0, 0, fmt.Errorf("track: отрицательное смещение %s", u)
	}
	dz := 0.0
	left := u
	for _, seg := range p {
		if left <= seg.LengthU {
			return dz + seg.riseOver(left), seg.slopeAt(left), nil
		}
		dz += seg.riseOver(seg.LengthU)
		left -= seg.LengthU
	}
	return 0, 0, fmt.Errorf("track: смещение %s выходит за длину профиля %s", u, p.LengthU())
}

// slopeAt — уклон внутри сегмента: линейная интерполяция по u.
func (s Segment) slopeAt(u units.Distance) float64 {
	if s.LengthU == 0 {
		return s.StartSlope
	}
	t := float64(u) / float64(s.LengthU)
	return s.StartSlope + (s.EndSlope-s.StartSlope)*t
}

// riseOver — подъём на первых u сегмента. Уклон линеен, значит подъём — интеграл
// линейной функции, то есть площадь трапеции.
func (s Segment) riseOver(u units.Distance) float64 {
	return (s.StartSlope + s.slopeAt(u)) / 2 * u.Meters()
}

// spatialLen — пространственная длина сегмента в метрах.
//
// s = ∫₀ᴸ √(1+g²) du. При линейном g подстановка g = g₀ + k·u даёт замкнутую
// форму через первообразную ∫√(1+g²)dg = (g√(1+g²) + asinh g)/2. Численного
// интегрирования не требуется, поэтому детерминизм держится на математике, а не
// на шаге сетки.
func (s Segment) spatialLen() float64 {
	l := s.LengthU.Meters()
	if s.StartSlope == s.EndSlope {
		return l * math.Sqrt(1+s.StartSlope*s.StartSlope)
	}
	k := (s.EndSlope - s.StartSlope) / l
	f := func(g float64) float64 { return (g*math.Sqrt(1+g*g) + math.Asinh(g)) / 2 }
	return (f(s.EndSlope) - f(s.StartSlope)) / k
}
```

- [ ] **Шаг 4: Тесты проходят**

Run: `go test ./internal/track/ -run TestProfile -v`
Expected: PASS по всем подтестам

- [ ] **Шаг 5: Коммит**

```bash
git add internal/track/
git commit -m "feat: вертикальный профиль и u->s в замкнутой форме [ClearAhead-0xc]"
```

---

## Задача 6: распространение поз и замыкание циклов

**Files:**
- Create: `internal/track/propagate.go`
- Test: `internal/track/propagate_test.go`

**Interfaces:**
- Consumes: `*mapfmt.Map`, `geom.Pose`, `geom.Chain`, `Profile`.
- Produces:
  - `type PortPose struct { Plan geom.Pose; Z float64 }`
  - `type Element struct { ID, From, To string; Plan geom.Chain; Prof Profile; Start PortPose }`
  - `func Propagate(m *mapfmt.Map) (map[string]PortPose, map[string]Element, error)`
  - `const (TolPosition = units.Millimeter; TolHeading = 1e-5; TolSlope = 1e-4)`

**Acceptance Criteria:**
- Позы портов выводятся от единственного якоря обходом графа.
- Цикл, который сходится, принимается.
- Цикл с невязкой больше 1 мм — отказ с указанием элемента и величины.
- Расхождение heading больше 1e-5 рад — отказ.
- Компонента без якоря — отказ; компонента с двумя якорями — отказ.
- Ориентация: `u` растёт от `from` к `to`; heading порта смотрит внутрь
  элемента; конец цепочки сравнивается с `Reverse(to.heading)`.

- [ ] **Шаг 1: Падающий тест**

```go
package track

import (
	"math"
	"strings"
	"testing"

	"github.com/shady2k/ClearAhead/internal/mapfmt"
)

// twoEdges — N1 --E1-- N2 --E2-- N3, прямая 100 + прямая 50.
// N2.P1 — стык: им пользуются оба ребра, и именно там проверяется замыкание.
const twoEdges = `{
  "format_version": 1, "map_id": "T", "map_revision": 1,
  "anchors": { "N1.P1": { "x": 0, "y": 0, "z": 10, "heading": 0 } },
  "topology": {
    "nodes": [
      { "id": "N1", "ports": [ { "id": "P1", "purpose": "map_boundary" } ] },
      { "id": "N2", "ports": [ { "id": "P1" } ] },
      { "id": "N3", "ports": [ { "id": "P1", "purpose": "buffer_stop" } ] }
    ],
    "turnouts": [], "trackside": [],
    "edges": [
      { "id": "E1", "from": "N1.P1", "to": "N2.P1" },
      { "id": "E2", "from": "N2.P1", "to": "N3.P1" }
    ]
  },
  "geometry": { "turnouts": {}, "edges": {
    "E1": { "horizontal": [ { "kind": "straight", "length": 100.0 } ] },
    "E2": { "horizontal": [ { "kind": "straight", "length": 50.0 } ] }
  } }
}`

func loadMap(t *testing.T, doc string) *mapfmt.Map {
	t.Helper()
	m, err := mapfmt.Decode(strings.NewReader(doc))
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if err := mapfmt.Validate(m); err != nil {
		t.Fatalf("валидация: %v", err)
	}
	return m
}

func TestPropagateChain(t *testing.T) {
	poses, els, err := Propagate(loadMap(t, twoEdges))
	if err != nil {
		t.Fatalf("распространение: %v", err)
	}
	// N2.P1 — конец E1: 100 м по X. Heading смотрит внутрь E1, то есть назад.
	p := poses["N2.P1"]
	if math.Abs(p.Plan.X-100) > 1e-6 || math.Abs(p.Plan.Y) > 1e-6 {
		t.Fatalf("N2.P1 в (%v, %v), ожидалось (100, 0)", p.Plan.X, p.Plan.Y)
	}
	if math.Abs(math.Abs(p.Plan.Heading)-math.Pi) > 1e-9 {
		t.Fatalf("heading N2.P1 = %v, ожидалось ±π", p.Plan.Heading)
	}
	if math.Abs(p.Z-10) > 1e-9 {
		t.Fatalf("z N2.P1 = %v, ожидалось 10 (профиля нет)", p.Z)
	}
	if len(els) != 2 {
		t.Fatalf("элементов %d, ожидалось 2", len(els))
	}
}

func TestPropagateRejectsUnanchored(t *testing.T) {
	doc := strings.Replace(twoEdges, `"anchors": { "N1.P1": { "x": 0, "y": 0, "z": 10, "heading": 0 } }`,
		`"anchors": {}`, 1)
	m, err := mapfmt.Decode(strings.NewReader(doc))
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if err := mapfmt.Validate(m); err == nil {
		if _, _, err := Propagate(m); err == nil {
			t.Fatal("ожидался отказ: компонента без якоря")
		}
	}
}

// TestPropagateClosingCycle — положительный случай: кольцо, которое сходится.
//
// Без него тест на невязку бесполезен: проверка, которая отвергает всё подряд,
// тоже «ловит расхождение». Квадрат 100×100: четыре прямые по 100 м и четыре
// поворота на π/2 дугами радиуса 10 м, вписанными в углы, — цикл замыкается
// точно, и распространение обязано это принять.
func TestPropagateClosingCycle(t *testing.T) {
	// Кольцо из четырёх дуг радиуса 50 м на π/2: замкнутая окружность.
	const ring = `{
	  "format_version": 1, "map_id": "C", "map_revision": 1,
	  "anchors": { "N1.P1": { "x": 0, "y": 0, "z": 0, "heading": 0 } },
	  "topology": {
	    "nodes": [
	      { "id": "N1", "ports": [ { "id": "P1" } ] },
	      { "id": "N2", "ports": [ { "id": "P1" } ] },
	      { "id": "N3", "ports": [ { "id": "P1" } ] },
	      { "id": "N4", "ports": [ { "id": "P1" } ] }
	    ],
	    "turnouts": [], "trackside": [],
	    "edges": [
	      { "id": "E1", "from": "N1.P1", "to": "N2.P1" },
	      { "id": "E2", "from": "N2.P1", "to": "N3.P1" },
	      { "id": "E3", "from": "N3.P1", "to": "N4.P1" },
	      { "id": "E4", "from": "N4.P1", "to": "N1.P1" }
	    ]
	  },
	  "geometry": { "turnouts": {}, "edges": {
	    "E1": { "horizontal": [ { "kind": "arc", "radius": 50.0, "angle": 1.5707963267948966 } ] },
	    "E2": { "horizontal": [ { "kind": "arc", "radius": 50.0, "angle": 1.5707963267948966 } ] },
	    "E3": { "horizontal": [ { "kind": "arc", "radius": 50.0, "angle": 1.5707963267948966 } ] },
	    "E4": { "horizontal": [ { "kind": "arc", "radius": 50.0, "angle": 1.5707963267948966 } ] }
	  } }
	}`
	if _, _, err := Propagate(loadMap(t, ring)); err != nil {
		t.Fatalf("замкнутое кольцо должно приниматься, получен отказ: %v", err)
	}
}

func TestPropagateClosureMismatch(t *testing.T) {
	// Треугольник, который не сходится: E3 короче, чем требуется, на 5 мм.
	doc := strings.Replace(twoEdges,
		`{ "id": "E2", "from": "N2.P1", "to": "N3.P1" }`,
		`{ "id": "E2", "from": "N2.P1", "to": "N3.P1" },
		 { "id": "E3", "from": "N3.P1", "to": "N1.P1" }`, 1)
	doc = strings.Replace(doc,
		`{ "id": "N3", "ports": [ { "id": "P1", "purpose": "buffer_stop" } ] }`,
		`{ "id": "N3", "ports": [ { "id": "P1" } ] }`, 1)
	doc = strings.Replace(doc,
		`"E2": { "horizontal": [ { "kind": "straight", "length": 50.0 } ] }`,
		`"E2": { "horizontal": [ { "kind": "straight", "length": 50.0 } ] },
		 "E3": { "horizontal": [ { "kind": "straight", "length": 149.995 } ] }`, 1)

	m, err := mapfmt.Decode(strings.NewReader(doc))
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if err := mapfmt.Validate(m); err != nil {
		t.Fatalf("валидация: %v", err)
	}
	_, _, err = Propagate(m)
	if err == nil {
		t.Fatal("ожидался отказ по невязке замыкания")
	}
	if !strings.Contains(err.Error(), "невязк") {
		t.Fatalf("ошибка не про невязку: %v", err)
	}
}
```

- [ ] **Шаг 2: Убедиться, что тест падает**

Run: `go test ./internal/track/ -run TestPropagate -v`
Expected: FAIL, `undefined: Propagate`

- [ ] **Шаг 3: Реализация**

```go
package track

import (
	"fmt"
	"math"
	"sort"

	"github.com/shady2k/ClearAhead/internal/geom"
	"github.com/shady2k/ClearAhead/internal/mapfmt"
	"github.com/shady2k/ClearAhead/internal/units"
)

// Допуски замыкания, фиксированные версией формата (спека §7).
const (
	TolPosition = units.Millimeter
	TolHeading  = 1e-5
	TolSlope    = 1e-4
)

// PortPose — поза порта: план из geom плюс высота.
//
// Z не входит в geom.Pose сознательно: geom считает план, вертикаль — отдельная
// одномерная функция. Смешивать их означало бы тащить профиль в модуль, который
// о нём ничего не знает.
type PortPose struct {
	Plan geom.Pose
	Z    float64
}

// Element — линейный элемент: ребро или проход стрелки.
type Element struct {
	ID    string
	From  string
	To    string
	Plan  geom.Chain
	Prof  Profile
	Start PortPose // поза порта From, направленная внутрь элемента
}

// Propagate выводит позы всех портов от якорей и проверяет замыкание циклов.
//
// Обход детерминированный: элементы сортируются по ID, потому что порядок обхода
// определяет, какая поза окажется вычисленной первой, а какая — проверенной на
// невязку, и от этого зависит текст ошибки.
func Propagate(m *mapfmt.Map) (map[string]PortPose, map[string]Element, error) {
	els, err := buildElements(m)
	if err != nil {
		return nil, nil, err
	}

	byPort := map[string][]string{}
	ids := make([]string, 0, len(els))
	for id := range els {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		e := els[id]
		byPort[e.From] = append(byPort[e.From], id)
		byPort[e.To] = append(byPort[e.To], id)
	}

	poses := map[string]portState{}
	visited := map[string]bool{}

	anchorIDs := make([]string, 0, len(m.Anchors))
	for id := range m.Anchors {
		anchorIDs = append(anchorIDs, id)
	}
	sort.Strings(anchorIDs)

	for _, aID := range anchorIDs {
		if visited[aID] {
			return nil, nil, fmt.Errorf("track: в одной связной компоненте два якоря, второй — %s", aID)
		}
		a := m.Anchors[aID]
		// Якорь принадлежит первому элементу порта в отсортированном порядке —
		// обход детерминирован, поэтому и владелец детерминирован.
		if len(byPort[aID]) == 0 {
			return nil, nil, fmt.Errorf("track: якорь %s не принадлежит ни одному элементу", aID)
		}
		poses[aID] = portState{
			Pose:  PortPose{Plan: geom.Normalize(geom.Pose{X: a.X, Y: a.Y, Heading: a.Heading}), Z: a.Z},
			Owner: byPort[aID][0],
		}

		queue := []string{aID}
		for len(queue) > 0 {
			port := queue[0]
			queue = queue[1:]
			if visited[port] {
				continue
			}
			visited[port] = true

			for _, eID := range byPort[port] {
				at := poses[port].as(eID)
				far, want, err := across(els[eID], port, at)
				if err != nil {
					return nil, nil, err
				}
				if err := settle(poses, far, want, eID); err != nil {
					return nil, nil, err
				}
				if !visited[far] {
					queue = append(queue, far)
				}
			}
		}
	}

	out := make(map[string]PortPose, len(poses))
	for id, st := range poses {
		out[id] = st.Pose
	}
	for id, e := range els {
		st, ok := poses[e.From]
		if !ok {
			return nil, nil, fmt.Errorf("track: элемент %s в компоненте без якоря", id)
		}
		e.Start = st.as(id)
		els[id] = e
	}
	return out, els, nil
}

// portState — поза порта и элемент, для которого она записана.
//
// Поза порта всегда смотрит ВНУТРЬ элемента. У стыка элементов два, и смотрят
// они в противоположные стороны, поэтому одного значения мало: надо помнить,
// чьё оно. Соседний элемент получает то же значение, развёрнутое на π.
type portState struct {
	Pose  PortPose
	Owner string
}

// as возвращает позу в системе указанного элемента.
func (s portState) as(element string) PortPose {
	if s.Owner == element {
		return s.Pose
	}
	return PortPose{Plan: geom.Reverse(s.Pose.Plan), Z: s.Pose.Z}
}

// across переносит позу с одного конца элемента на другой.
//
// Поза порта всегда смотрит ВНУТРЬ своего элемента. У From это совпадает с
// направлением движения по цепочке, у To — противоположно ему, отсюда Reverse
// в обе стороны.
//
// Обратный ход не разворачивает цепочку по звеньям: цепочка целиком есть одно
// жёсткое движение Δ, и начало восстанавливается как Compose(конец, Invert(Δ)).
func across(e Element, from string, at PortPose) (string, PortPose, error) {
	dz, _, err := e.Prof.At(e.Prof.LengthU())
	if err != nil {
		return "", PortPose{}, fmt.Errorf("track: %s: подъём по профилю: %w", e.ID, err)
	}
	delta := e.Plan.End(geom.Pose{})

	switch from {
	case e.From:
		end := geom.Compose(at.Plan, delta)
		return e.To, PortPose{Plan: geom.Reverse(end), Z: at.Z + dz}, nil
	case e.To:
		travelEnd := geom.Reverse(at.Plan)
		start := geom.Compose(travelEnd, geom.Invert(delta))
		return e.From, PortPose{Plan: start, Z: at.Z - dz}, nil
	default:
		return "", PortPose{}, fmt.Errorf("track: порт %s не принадлежит элементу %s", from, e.ID)
	}
}

// settle записывает позу порта или проверяет уже записанную на невязку.
//
// Второй элемент в порту — это стык, и именно здесь замыкание перестаёт быть
// декоративным: до введения стыков порт принадлежал ровно одному ребру, два
// пути никогда не сходились, и проверка не срабатывала ни разу.
func settle(poses map[string]portState, port string, want PortPose, via string) error {
	st, seen := poses[port]
	if !seen {
		poses[port] = portState{Pose: want, Owner: via}
		return nil
	}
	got := st.as(via)
	dx := got.Plan.X - want.Plan.X
	dy := got.Plan.Y - want.Plan.Y
	dz := got.Z - want.Z
	dxy := math.Hypot(dx, dy)
	tol := TolPosition.Meters()
	if dxy > tol || math.Abs(dz) > tol {
		return fmt.Errorf("track: невязка замыкания в порту %s через %s: %.4f мм по плану, %.4f мм по высоте",
			port, via, dxy*1000, math.Abs(dz)*1000)
	}
	dh := geom.Normalize(geom.Pose{Heading: got.Plan.Heading - want.Plan.Heading}).Heading
	if math.Abs(dh) > TolHeading {
		return fmt.Errorf("track: невязка замыкания в порту %s через %s: %.3e рад по направлению", port, via, math.Abs(dh))
	}
	return nil
}

// buildElements переводит выравнивания карты в цепочки geom и профили.
func buildElements(m *mapfmt.Map) (map[string]Element, error) {
	ends := map[string][2]string{}
	for _, e := range m.Topology.Edges {
		ends[e.ID] = [2]string{e.From, e.To}
	}
	for _, t := range m.Topology.Turnouts {
		ends[t.ID+mapfmt.PassageStraight] = [2]string{t.ID + "." + t.Ports.Common, t.ID + "." + t.Ports.Straight}
		ends[t.ID+mapfmt.PassageDiverging] = [2]string{t.ID + "." + t.Ports.Common, t.ID + "." + t.Ports.Diverging}
	}

	out := map[string]Element{}
	for id, a := range m.AllAlignments() {
		chain := make(geom.Chain, 0, len(a.Horizontal))
		for i, hp := range a.Horizontal {
			var (
				p   geom.Primitive
				err error
			)
			switch hp.Kind {
			case "straight":
				var d units.Distance
				if d, err = units.MetersToDistance(hp.Length); err == nil {
					p, err = geom.Straight(d)
				}
			case "arc":
				p, err = geom.Arc(hp.Radius, hp.Angle)
			default:
				err = fmt.Errorf("неизвестный примитив %q", hp.Kind)
			}
			if err != nil {
				return nil, fmt.Errorf("track: %s[%d]: %w", id, i, err)
			}
			chain = append(chain, p)
		}
		prof, err := ProfileFrom(a, chain.Length())
		if err != nil {
			return nil, fmt.Errorf("track: %s: %w", id, err)
		}
		e, ok := ends[id]
		if !ok {
			return nil, fmt.Errorf("track: у элемента %s нет концов", id)
		}
		out[id] = Element{ID: id, From: e[0], To: e[1], Plan: chain, Prof: prof}
	}
	return out, nil
}
```

- [ ] **Шаг 4: Тесты проходят**

Run: `go test ./internal/track/ -v`
Expected: PASS

- [ ] **Шаг 5: Коммит**

```bash
git add internal/track/
git commit -m "feat: распространение поз от якоря и замыкание циклов [ClearAhead-0xc]"
```

---

## Задача 7: компиляция в `CompiledTrack` и `RenderGeometry`

**Files:**
- Create: `internal/track/compile.go`
- Test: `internal/track/compile_test.go`

**Interfaces:**
- Consumes: `Propagate`, `Profile`, `mapfmt.Map`.
- Produces:
  - `type CompiledElement struct { ID, From, To string; LengthU, LengthS units.Distance; Prof Profile }`
  - `type TrackSpanS struct { Element string; FromS, ToS units.Distance }`
  - `type CompiledTrack struct { MapID string; Revision int; Elements map[string]CompiledElement; Trackside map[string][]TrackSpanS; Turnouts map[string]CompiledTurnout }`
  - `type CompiledTurnout struct { ID, Hand string; Common, Straight, Diverging string; Resource string }`
  - `type RenderPrimitive struct { Kind string; LengthM, Radius, Angle float64 }`
  - `type RenderElement struct { ID string; Start PortPose; Prims []RenderPrimitive }`
  - `type RenderGeometry struct { MapID string; Revision int; Elements []RenderElement }`
  - `func Compile(m *mapfmt.Map) (*CompiledTrack, *RenderGeometry, error)`

**Acceptance Criteria:**
- Длины элементов в `CompiledTrack` — в `s`, целые микрометры.
- Плоская карта: `LengthS == LengthU` для каждого элемента.
- Смещения путевых объектов переведены из `u` в `s`.
- Оба прохода стрелки ссылаются на один `Resource`.
- `RenderGeometry` содержит абсолютную стартовую позу каждого элемента.
- Повторная компиляция той же карты даёт побайтово те же артефакты.
- **Golden-вектор на правило округления:** цепочка из примитивов, длины которых
  в метрах дают половину микрометра, компилируется в сумму **индивидуально**
  округлённых длин, а не в округление математической суммы. Спека §3 требует
  именно этого; без теста правило существует только на бумаге.

- [ ] **Шаг 1: Падающий тест**

```go
package track

import (
	"testing"

	"github.com/shady2k/ClearAhead/internal/units"
)

func TestCompileFlatLengths(t *testing.T) {
	ct, rg, err := Compile(loadMap(t, twoEdges))
	if err != nil {
		t.Fatalf("компиляция: %v", err)
	}
	e1 := ct.Elements["E1"]
	if e1.LengthS != 100*units.Meter || e1.LengthU != 100*units.Meter {
		t.Fatalf("E1: u=%s s=%s, ожидалось по 100m", e1.LengthU, e1.LengthS)
	}
	if len(rg.Elements) != 2 {
		t.Fatalf("в RenderGeometry %d элементов, ожидалось 2", len(rg.Elements))
	}
	if rg.Elements[0].ID != "E1" {
		t.Fatalf("порядок элементов не детерминирован: первый %s", rg.Elements[0].ID)
	}
}

// TestCompileRoundingRule закрепляет правило спеки §3: длина элемента — сумма
// индивидуально округлённых длин примитивов, а не округление суммы.
//
// Три отрезка по 0.0000005 м (полмикрометра). Каждый округляется вверх до 1 мкм
// (половины от нуля), сумма — 3 мкм. Округление математической суммы дало бы
// 1.5 мкм → 2 мкм. Разница видна и это ровно то место, где два компилятора
// разошлись бы.
func TestCompileRoundingRule(t *testing.T) {
	const doc = `{
	  "format_version": 1, "map_id": "R", "map_revision": 1,
	  "anchors": { "N1.P1": { "x": 0, "y": 0, "z": 0, "heading": 0 } },
	  "topology": {
	    "nodes": [
	      { "id": "N1", "ports": [ { "id": "P1", "purpose": "map_boundary" } ] },
	      { "id": "N2", "ports": [ { "id": "P1", "purpose": "buffer_stop" } ] }
	    ],
	    "turnouts": [], "trackside": [],
	    "edges": [ { "id": "E1", "from": "N1.P1", "to": "N2.P1" } ]
	  },
	  "geometry": { "turnouts": {}, "edges": { "E1": { "horizontal": [
	    { "kind": "straight", "length": 0.0000005 },
	    { "kind": "straight", "length": 0.0000005 },
	    { "kind": "straight", "length": 0.0000005 }
	  ] } } }
	}`
	ct, _, err := Compile(loadMap(t, doc))
	if err != nil {
		t.Fatalf("компиляция: %v", err)
	}
	if got := ct.Elements["E1"].LengthU; got != 3*units.Micrometer {
		t.Fatalf("длина %d мкм, ожидалось 3: правило округления не сумма округлённых", int64(got))
	}
}

func TestCompileDeterministic(t *testing.T) {
	a1, b1, err := Compile(loadMap(t, twoEdges))
	if err != nil {
		t.Fatalf("компиляция 1: %v", err)
	}
	a2, b2, err := Compile(loadMap(t, twoEdges))
	if err != nil {
		t.Fatalf("компиляция 2: %v", err)
	}
	if a1.Elements["E1"].LengthS != a2.Elements["E1"].LengthS {
		t.Fatal("длина зависит от запуска")
	}
	if b1.Elements[0].Start != b2.Elements[0].Start {
		t.Fatal("стартовая поза зависит от запуска")
	}
}
```

- [ ] **Шаг 2: Убедиться, что тест падает**

Run: `go test ./internal/track/ -run TestCompile -v`
Expected: FAIL, `undefined: Compile`

- [ ] **Шаг 3: Реализация**

```go
package track

import (
	"fmt"
	"sort"

	"github.com/shady2k/ClearAhead/internal/geom"
	"github.com/shady2k/ClearAhead/internal/mapfmt"
	"github.com/shady2k/ClearAhead/internal/units"
)

// CompiledElement — линейный элемент после компиляции. Длины в s.
type CompiledElement struct {
	ID      string
	From    string
	To      string
	LengthU units.Distance
	LengthS units.Distance
	Prof    Profile
}

// TrackSpanS — интервал путевого объекта в пространственной координате.
type TrackSpanS struct {
	Element string
	FromS   units.Distance
	ToS     units.Distance
}

// CompiledTurnout — стрелка после компиляции. Оба прохода делят один ресурс:
// положение остряка определяет разрешённый переход, занятость живёт на сегментах.
type CompiledTurnout struct {
	ID        string
	Hand      string
	Common    string
	Straight  string
	Diverging string
	Resource  string
}

// CompiledTrack — вход физики и безопасности. Координат не содержит.
type CompiledTrack struct {
	MapID     string
	Revision  int
	Elements  map[string]CompiledElement
	Trackside map[string][]TrackSpanS
	Turnouts  map[string]CompiledTurnout
}

// RenderPrimitive — примитив плана для клиента. Метры и радианы.
type RenderPrimitive struct {
	Kind    string  `json:"kind"`
	LengthM float64 `json:"length"`
	Radius  float64 `json:"radius,omitempty"`
	Angle   float64 `json:"angle,omitempty"`
}

// RenderElement — элемент с абсолютной стартовой позой: клиент рисует цепочку
// от неё и ничего не пересчитывает.
type RenderElement struct {
	ID    string            `json:"id"`
	Start PortPose          `json:"start"`
	Prims []RenderPrimitive `json:"primitives"`
}

// RenderGeometry — вход клиента и инструментов.
type RenderGeometry struct {
	MapID    string          `json:"map_id"`
	Revision int             `json:"map_revision"`
	Elements []RenderElement `json:"elements"`
}

// Compile строит оба артефакта из одной карты.
func Compile(m *mapfmt.Map) (*CompiledTrack, *RenderGeometry, error) {
	_, els, err := Propagate(m)
	if err != nil {
		return nil, nil, err
	}

	ids := make([]string, 0, len(els))
	for id := range els {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	ct := &CompiledTrack{
		MapID:     m.MapID,
		Revision:  m.MapRevision,
		Elements:  make(map[string]CompiledElement, len(els)),
		Trackside: map[string][]TrackSpanS{},
		Turnouts:  make(map[string]CompiledTurnout, len(m.Topology.Turnouts)),
	}
	rg := &RenderGeometry{MapID: m.MapID, Revision: m.MapRevision}

	for _, id := range ids {
		e := els[id]
		lengthS, err := e.Prof.LengthS()
		if err != nil {
			return nil, nil, fmt.Errorf("track: %s: %w", id, err)
		}
		ct.Elements[id] = CompiledElement{
			ID:      id,
			From:    e.From,
			To:      e.To,
			LengthU: e.Plan.Length(),
			LengthS: lengthS,
			Prof:    e.Prof,
		}
		re := RenderElement{ID: id, Start: e.Start, Prims: make([]RenderPrimitive, 0, len(e.Plan))}
		for _, p := range e.Plan {
			switch p.Kind {
			case geom.KindStraight:
				re.Prims = append(re.Prims, RenderPrimitive{Kind: "straight", LengthM: p.Length.Meters()})
			default:
				re.Prims = append(re.Prims, RenderPrimitive{
					Kind: "arc", LengthM: p.Length.Meters(), Radius: p.Radius, Angle: p.Angle,
				})
			}
		}
		rg.Elements = append(rg.Elements, re)
	}

	for _, t := range m.Topology.Turnouts {
		ct.Turnouts[t.ID] = CompiledTurnout{
			ID:        t.ID,
			Hand:      t.Hand,
			Common:    t.ID + "." + t.Ports.Common,
			Straight:  t.ID + "." + t.Ports.Straight,
			Diverging: t.ID + "." + t.Ports.Diverging,
			Resource:  "RES_" + t.ID,
		}
	}

	for _, ts := range m.Topology.Trackside {
		spans := make([]TrackSpanS, 0, len(ts.Span))
		for _, iv := range ts.Span {
			e, ok := els[iv.Element]
			if !ok {
				return nil, nil, fmt.Errorf("track: объект %s ссылается на элемент %s, которого нет", ts.ID, iv.Element)
			}
			fromU, err := units.MetersToDistance(iv.From)
			if err != nil {
				return nil, nil, fmt.Errorf("track: объект %s: %w", ts.ID, err)
			}
			toU, err := units.MetersToDistance(iv.To)
			if err != nil {
				return nil, nil, fmt.Errorf("track: объект %s: %w", ts.ID, err)
			}
			fromS, err := e.Prof.UToS(fromU)
			if err != nil {
				return nil, nil, fmt.Errorf("track: объект %s: начало: %w", ts.ID, err)
			}
			toS, err := e.Prof.UToS(toU)
			if err != nil {
				return nil, nil, fmt.Errorf("track: объект %s: конец: %w", ts.ID, err)
			}
			spans = append(spans, TrackSpanS{Element: iv.Element, FromS: fromS, ToS: toS})
		}
		ct.Trackside[ts.ID] = spans
	}
	return ct, rg, nil
}
```

- [ ] **Шаг 4: Тесты проходят**

Run: `go test ./internal/track/ -v`
Expected: PASS

- [ ] **Шаг 5: Коммит**

```bash
git add internal/track/
git commit -m "feat: компиляция в CompiledTrack и RenderGeometry [ClearAhead-0xc]"
```

---

## Задача 8: хеши и манифест ревизии

**Files:**
- Create: `internal/track/hash.go`
- Test: `internal/track/hash_test.go`

**Interfaces:**
- Consumes: `*mapfmt.Map`, `*CompiledTrack`, `*RenderGeometry`.
- Produces:
  - `type Manifest struct { MapID string; Revision int; TrackHash, RenderGeometryHash string }`
  - `func BuildManifest(m *mapfmt.Map, ct *CompiledTrack, rg *RenderGeometry) (Manifest, error)`

**Acceptance Criteria:**
- Хеш считается по **нормализованной внутренней модели**, не по исходному JSON.
- Переформатирование исходного файла (пробелы, порядок ключей) хеш не меняет.
- Правка `provenance` хеш не меняет.
- Правка геопривязки хеш меняет.
- Правка геометрии хеш меняет.

- [ ] **Шаг 1: Падающий тест**

```go
package track

import (
	"strings"
	"testing"
)

func manifestOf(t *testing.T, doc string) Manifest {
	t.Helper()
	m := loadMap(t, doc)
	ct, rg, err := Compile(m)
	if err != nil {
		t.Fatalf("компиляция: %v", err)
	}
	man, err := BuildManifest(m, ct, rg)
	if err != nil {
		t.Fatalf("манифест: %v", err)
	}
	return man
}

func TestManifestStableUnderReformatting(t *testing.T) {
	reformatted := strings.ReplaceAll(twoEdges, "\n", " ")
	reformatted = strings.ReplaceAll(reformatted, "  ", " ")
	if manifestOf(t, twoEdges).TrackHash != manifestOf(t, reformatted).TrackHash {
		t.Fatal("хеш зависит от форматирования исходного JSON")
	}
}

func TestManifestChangesOnGeometry(t *testing.T) {
	changed := strings.Replace(twoEdges, `"length": 100.0`, `"length": 100.001`, 1)
	if manifestOf(t, twoEdges).TrackHash == manifestOf(t, changed).TrackHash {
		t.Fatal("правка геометрии не изменила хеш")
	}
}
```

- [ ] **Шаг 2: Убедиться, что тест падает**

Run: `go test ./internal/track/ -run TestManifest -v`
Expected: FAIL, `undefined: BuildManifest`

- [ ] **Шаг 3: Реализация**

```go
package track

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"sort"

	"github.com/shady2k/ClearAhead/internal/mapfmt"
)

// Manifest связывает ревизию карты с хешами её ресурсов. Пара (MapID, Revision)
// определяет ровно один манифест — иначе immutable-URL лжёт.
type Manifest struct {
	MapID              string `json:"map_id"`
	Revision           int    `json:"map_revision"`
	TrackHash          string `json:"track_hash"`
	RenderGeometryHash string `json:"render_geometry_hash"`
}

// BuildManifest считает хеши по нормализованной внутренней модели, а не по
// исходному JSON. Так снимается весь класс вопросов про каноническую
// сериализацию: порядок ключей, форма чисел, -0, экспоненты, Unicode.
func BuildManifest(m *mapfmt.Map, ct *CompiledTrack, rg *RenderGeometry) (Manifest, error) {
	th := sha256.New()
	writeTrackModel(th, m, ct)
	rh := sha256.New()
	writeRenderModel(rh, rg)
	return Manifest{
		MapID:              m.MapID,
		Revision:           m.MapRevision,
		TrackHash:          hex.EncodeToString(th.Sum(nil)),
		RenderGeometryHash: hex.EncodeToString(rh.Sum(nil)),
	}, nil
}

func writeTrackModel(w io.Writer, m *mapfmt.Map, ct *CompiledTrack) {
	fmt.Fprintf(w, "v%d|%s|%d\n", mapfmt.FormatVersion, ct.MapID, ct.Revision)

	// Геопривязка входит: она меняет смысл координат. Provenance не входит:
	// правка комментария не должна сбрасывать кэш клиента.
	if g := m.Georeference; g != nil {
		fmt.Fprintf(w, "geo|%s|%.12g|%.12g|%.12g|%s|%.12g|%.12g\n",
			g.Datum, g.Origin.Lat, g.Origin.Lon, g.Origin.H,
			g.OriginHeightKind, g.XAxisAzimuthDeg, g.GroundToGrid)
	}

	ids := make([]string, 0, len(ct.Elements))
	for id := range ct.Elements {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		e := ct.Elements[id]
		fmt.Fprintf(w, "el|%s|%s|%s|%d|%d\n", e.ID, e.From, e.To, int64(e.LengthU), int64(e.LengthS))
		for _, seg := range e.Prof {
			fmt.Fprintf(w, "  pr|%d|%.12g|%.12g\n", int64(seg.LengthU), seg.StartSlope, seg.EndSlope)
		}
	}

	tids := make([]string, 0, len(ct.Turnouts))
	for id := range ct.Turnouts {
		tids = append(tids, id)
	}
	sort.Strings(tids)
	for _, id := range tids {
		t := ct.Turnouts[id]
		fmt.Fprintf(w, "sw|%s|%s|%s|%s|%s|%s\n", t.ID, t.Hand, t.Common, t.Straight, t.Diverging, t.Resource)
	}

	oids := make([]string, 0, len(ct.Trackside))
	for id := range ct.Trackside {
		oids = append(oids, id)
	}
	sort.Strings(oids)
	for _, id := range oids {
		for _, sp := range ct.Trackside[id] {
			fmt.Fprintf(w, "ts|%s|%s|%d|%d\n", id, sp.Element, int64(sp.FromS), int64(sp.ToS))
		}
	}
}

func writeRenderModel(w io.Writer, rg *RenderGeometry) {
	fmt.Fprintf(w, "%s|%d\n", rg.MapID, rg.Revision)
	for _, e := range rg.Elements {
		fmt.Fprintf(w, "el|%s|%.12g|%.12g|%.12g|%.12g\n",
			e.ID, e.Start.Plan.X, e.Start.Plan.Y, e.Start.Plan.Heading, e.Start.Z)
		for _, p := range e.Prims {
			fmt.Fprintf(w, "  p|%s|%.12g|%.12g|%.12g\n", p.Kind, p.LengthM, p.Radius, p.Angle)
		}
	}
}
```

- [ ] **Шаг 4: Тесты проходят**

Run: `go test ./internal/track/ -v`
Expected: PASS

- [ ] **Шаг 5: Коммит**

```bash
git add internal/track/
git commit -m "feat: хеши по нормализованной модели и манифест ревизии [ClearAhead-0xc]"
```

---

## Задача 9: контракт с клиентом и барьер валидации

**Files:**
- Create: `internal/protocol/protocol.go`
- Create: `internal/rpc/dispatch.go`
- Test: `internal/rpc/dispatch_test.go`
- Test: `internal/rpc/barrier_test.go`

**Interfaces:**
- Consumes: —
- Produces:
  - `type protocol.Input struct { Path map[string]string; Body json.RawMessage }`
  - `type protocol.Request[T any] interface { *T; sealed(); parse(Input) error }`
  - `type protocol.GeometryRequest struct { … }` с методами `MapID() string`, `Revision() int`
  - `func rpc.Register[T any, PT protocol.Request[T], Resp any](m *rpc.Mux, method string, h func(context.Context, T) (Resp, error))`
  - `func (m *rpc.Mux) Dispatch(ctx context.Context, method string, in protocol.Input) (any, error)`

**Acceptance Criteria:**
- Обработчик получает **уже разобранный и проверенный** тип запроса и не имеет
  доступа ни к сырым байтам, ни к пути, ни к телу.
- Зарегистрировать обработчик мимо разбора **невозможно на уровне типов**:
  `Register` требует тип, реализующий `protocol.Request`, а этот интерфейс
  содержит неэкспортируемые методы и реализуем только внутри `protocol`.
- Невалидный вход даёт ошибку до вызова обработчика; обработчик не вызывается —
  проверяется тестом со счётчиком вызовов.
- Тест барьера обходит AST и падает, если вне `internal/rpc` встречается
  `PathValue`, `json.Unmarshal`, `json.NewDecoder`, `r.Body` или `r.URL.Query`.
- Неизвестный метод — ошибка, а не паника.

**Почему так, а не «вызвать `Validate()` в начале обработчика».** Вызов можно
забыть, и забудут — не в первом методе, а в двенадцатом, в пятницу. Барьер
строится по принципу *«разбирай, а не проверяй»*: **невалидного представления
входа просто не существует как значения**. Поля запроса неэкспортируемые,
единственный способ их заполнить — метод `parse`, единственный вызывающий
`parse` — диспетчер. Пути в обработчик, минующего разбор, нет не по соглашению,
а потому что такое значение неоткуда взять.

**Честная граница.** Go не даёт сделать это буквально невозможным: внутри пакета
`protocol` структуру всё ещё можно собрать нулевым значением, а обработчик —
теоретически вызвать напрямую в тестах. Система типов закрывает межпакетные
пути; тест барьера на AST закрывает остаток. Вместе это настолько близко к
«архитектурно невозможно», насколько язык позволяет, и этого достаточно, потому
что оба слоя ломаются только осознанно и заметно в диффе. Проект уже принял
такой приём: AST-линтер на запрет float в состоянии сима (`sim-core-design` §20).

**Область действия.** В В1 через барьер проходит один метод — геометрия. Это не
делает барьер преждевременным: он ставится сейчас именно потому, что метод один
и переделывать нечего. В В2 приходят команды по WebSocket и JSON-RPC 2.0, и они
регистрируются тем же `Register` — другого входа в обработчики не существует.

- [ ] **Шаг 1: Падающий тест диспетчера**

```go
package rpc

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/shady2k/ClearAhead/internal/protocol"
)

func TestDispatchParsesBeforeHandler(t *testing.T) {
	calls := 0
	m := NewMux()
	Register[protocol.GeometryRequest](m, "geometry",
		func(ctx context.Context, r protocol.GeometryRequest) (string, error) {
			calls++
			return r.MapID(), nil
		})

	got, err := m.Dispatch(context.Background(), "geometry", protocol.Input{
		Path: map[string]string{"id": "ST_A", "rev": "1"},
	})
	if err != nil {
		t.Fatalf("корректный вход: %v", err)
	}
	if got != "ST_A" || calls != 1 {
		t.Fatalf("получено %v, вызовов %d", got, calls)
	}

	// Невалидная ревизия: обработчик не должен быть вызван вовсе.
	before := calls
	if _, err := m.Dispatch(context.Background(), "geometry", protocol.Input{
		Path: map[string]string{"id": "ST_A", "rev": "не число"},
	}); err == nil {
		t.Fatal("ожидался отказ на невалидной ревизии")
	}
	if calls != before {
		t.Fatal("обработчик вызван на невалидном входе — барьер дырявый")
	}
}

func TestDispatchUnknownMethod(t *testing.T) {
	m := NewMux()
	if _, err := m.Dispatch(context.Background(), "нет такого", protocol.Input{}); err == nil {
		t.Fatal("ожидался отказ на неизвестном методе")
	}
}

func TestDispatchRejectsGarbageBody(t *testing.T) {
	m := NewMux()
	Register[protocol.GeometryRequest](m, "geometry",
		func(ctx context.Context, r protocol.GeometryRequest) (string, error) { return "", nil })
	_, err := m.Dispatch(context.Background(), "geometry", protocol.Input{
		Path: map[string]string{"id": "", "rev": "1"},
		Body: json.RawMessage(`{`),
	})
	if err == nil {
		t.Fatal("ожидался отказ на пустом map_id")
	}
}
```

- [ ] **Шаг 2: Убедиться, что тест падает**

Run: `go test ./internal/rpc/ -v`
Expected: FAIL, `no required module provides package .../internal/protocol`

- [ ] **Шаг 3: Контракт**

```go
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
// запечатанность держит sealed, а не он.
type Request[T any] interface {
	*T
	sealed()
	Parse(Input) error
}

// GeometryRequest — запрос геометрии карты.
type GeometryRequest struct {
	mapID    string
	revision int
}

func (*GeometryRequest) sealed() {}

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
```

- [ ] **Шаг 4: Диспетчер**

```go
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
```

- [ ] **Шаг 5: Тест барьера на AST**

```go
package rpc

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// Запрещённые вне internal/rpc обращения к сырому внешнему входу. Если они
// появились — значит кто-то читает запрос мимо барьера.
var forbidden = []string{"PathValue", "Unmarshal", "NewDecoder", "ParseForm"}

func TestBarrierNoRawInputOutsideRPC(t *testing.T) {
	roots := []string{"../httpapi", "../track", "../mapfmt", "../../cmd"}
	for _, root := range roots {
		matches, err := filepath.Glob(filepath.Join(root, "*.go"))
		if err != nil {
			t.Fatalf("обход %s: %v", root, err)
		}
		for _, path := range matches {
			if strings.HasSuffix(path, "_test.go") {
				continue
			}
			// mapfmt разбирает файл карты с диска, а не внешний вызов: это
			// другой вход, у него свой строгий разбор (Задача 3).
			if strings.Contains(path, "mapfmt") {
				continue
			}
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				t.Fatalf("разбор %s: %v", path, err)
			}
			ast.Inspect(f, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				for _, bad := range forbidden {
					if sel.Sel.Name == bad {
						t.Errorf("%s: %s вне internal/rpc — внешний вход читается мимо барьера валидации",
							fset.Position(sel.Pos()), bad)
					}
				}
				return true
			})
		}
	}
}
```

- [ ] **Шаг 6: Тесты проходят**

Run: `go test ./internal/rpc/ ./internal/protocol/ -v`
Expected: PASS

- [ ] **Шаг 7: Коммит**

```bash
git add internal/protocol/ internal/rpc/
git commit -m "feat: контракт с клиентом и барьер валидации внешних вызовов [ClearAhead-0xc]"
```

---

## Задача 10: HTTP-ручка геометрии и бинарь

**Files:**
- Create: `internal/httpapi/geometry.go`
- Create: `cmd/clearahead/main.go`
- Test: `internal/httpapi/geometry_test.go`

**Interfaces:**
- Consumes: `track.RenderGeometry`, `track.Manifest`, `rpc.Mux`, `protocol.GeometryRequest`.
- Produces: `func NewHandler(rg *track.RenderGeometry, man track.Manifest) http.Handler`

**Acceptance Criteria:**
- Ручка **не читает путь и тело сама**: она собирает `protocol.Input` и передаёт
  его `rpc.Mux.Dispatch`. Обработчик получает разобранный
  `protocol.GeometryRequest`. Тест барьера из Задачи 9 это проверяет.
- `GET /maps/{id}/revisions/{n}/geometry` отдаёт 200 и JSON геометрии.
- `ETag` равен `render_geometry_hash` в кавычках.
- `Cache-Control: immutable` присутствует.
- Повторный запрос с `If-None-Match` даёт 304 без тела.
- Чужой `map_id` или ревизия — 404.
- Метод не GET — 405.
- **Обработчик не выполняет ни одной операции ввода-вывода на запрос.** Тело
  геометрии сериализуется один раз в `NewHandler` и дальше только пишется в
  сокет. Ни чтения с диска, ни перекомпиляции, ни обращения к сети внутри
  запроса быть не должно — это проверяется чтением кода на ревью и следует из
  того, что `NewHandler` принимает уже готовые артефакты, а не путь к файлу.
- Бинарь поднимается, читает карту из аргумента, слушает порт.

- [ ] **Шаг 1: Падающий тест**

```go
package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shady2k/ClearAhead/internal/track"
)

func newTestHandler(t *testing.T) (http.Handler, track.Manifest) {
	t.Helper()
	rg := &track.RenderGeometry{MapID: "ST_A", Revision: 1}
	man := track.Manifest{MapID: "ST_A", Revision: 1, RenderGeometryHash: "deadbeef"}
	return NewHandler(rg, man), man
}

func TestGeometryOK(t *testing.T) {
	h, man := newTestHandler(t)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/maps/ST_A/revisions/1/geometry", nil))
	if w.Code != 200 {
		t.Fatalf("код %d, ожидалось 200", w.Code)
	}
	if got, want := w.Header().Get("ETag"), `"`+man.RenderGeometryHash+`"`; got != want {
		t.Fatalf("ETag %q, ожидалось %q", got, want)
	}
	if cc := w.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Fatalf("Cache-Control %q не содержит immutable", cc)
	}
}

func TestGeometryNotModified(t *testing.T) {
	h, man := newTestHandler(t)
	r := httptest.NewRequest("GET", "/maps/ST_A/revisions/1/geometry", nil)
	r.Header.Set("If-None-Match", `"`+man.RenderGeometryHash+`"`)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 304 {
		t.Fatalf("код %d, ожидалось 304", w.Code)
	}
	if w.Body.Len() != 0 {
		t.Fatalf("304 с телом длиной %d", w.Body.Len())
	}
}

func TestGeometryRejects(t *testing.T) {
	h, _ := newTestHandler(t)
	cases := []struct {
		method, path string
		want         int
	}{
		{"GET", "/maps/OTHER/revisions/1/geometry", 404},
		{"GET", "/maps/ST_A/revisions/7/geometry", 404},
		{"POST", "/maps/ST_A/revisions/1/geometry", 405},
	}
	for _, c := range cases {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(c.method, c.path, nil))
		if w.Code != c.want {
			t.Fatalf("%s %s: код %d, ожидалось %d", c.method, c.path, w.Code, c.want)
		}
	}
}

```

- [ ] **Шаг 2: Убедиться, что тест падает**

Run: `go test ./internal/httpapi/ -v`
Expected: FAIL, `undefined: NewHandler`

- [ ] **Шаг 3: Реализация ручки**

```go
// Package httpapi отдаёт скомпилированные артефакты карты.
//
// Геометрия остаётся на обычном GET, а не уезжает в сокет: она статична, велика
// и кэшируется по хешу (vertical-slice-design §7).
package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/shady2k/ClearAhead/internal/track"
)

// NewHandler отдаёт геометрию одной ревизии одной карты.
//
// Одна карта в памяти — сознательное ограничение В1: карт больше одной не
// существует, а обобщать до реестра сейчас значит писать код без потребителя.
func NewHandler(rg *track.RenderGeometry, man track.Manifest) http.Handler {
	body, err := json.Marshal(rg)
	if err != nil {
		panic("httpapi: геометрия не сериализуется: " + err.Error())
	}
	etag := `"` + man.RenderGeometryHash + `"`

	mux := http.NewServeMux()
	mux.HandleFunc("/maps/{id}/revisions/{rev}/geometry", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "только GET", http.StatusMethodNotAllowed)
			return
		}
		rev, err := strconv.Atoi(r.PathValue("rev"))
		if err != nil || r.PathValue("id") != man.MapID || rev != man.Revision {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("ETag", etag)
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if match := r.Header.Get("If-None-Match"); match == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Write(body)
	})
	return mux
}
```

- [ ] **Шаг 4: Тесты проходят**

Run: `go test ./internal/httpapi/ -v`
Expected: PASS

- [ ] **Шаг 5: Бинарь**

```go
// Command clearahead — сервер карты.
package main

import (
	"flag"
	"log"
	"net/http"
	"os"

	"github.com/shady2k/ClearAhead/internal/httpapi"
	"github.com/shady2k/ClearAhead/internal/mapfmt"
	"github.com/shady2k/ClearAhead/internal/track"
)

func main() {
	mapPath := flag.String("map", "maps/st_a.json", "путь к файлу карты")
	addr := flag.String("addr", ":8080", "адрес прослушивания")
	flag.Parse()

	f, err := os.Open(*mapPath)
	if err != nil {
		log.Fatalf("карта %s: %v", *mapPath, err)
	}
	defer f.Close()

	m, err := mapfmt.Decode(f)
	if err != nil {
		log.Fatalf("разбор карты: %v", err)
	}
	if err := mapfmt.Validate(m); err != nil {
		log.Fatalf("проверка карты: %v", err)
	}
	ct, rg, err := track.Compile(m)
	if err != nil {
		log.Fatalf("компиляция карты: %v", err)
	}
	man, err := track.BuildManifest(m, ct, rg)
	if err != nil {
		log.Fatalf("манифест: %v", err)
	}

	log.Printf("карта %s ревизия %d: %d элементов, геометрия %s",
		man.MapID, man.Revision, len(ct.Elements), man.RenderGeometryHash[:12])
	log.Printf("слушаю %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, httpapi.NewHandler(rg, man)))
}
```

- [ ] **Шаг 6: Сборка и коммит**

Run: `go build ./... && go vet ./...`
Expected: без вывода

```bash
git add internal/httpapi/ cmd/
git commit -m "feat: HTTP-ручка геометрии с ETag и immutable, бинарь сервера [ClearAhead-0xc]"
```

---

## Задача 11: карта станции руками и сквозной приёмочный тест

**Files:**
- Create: `maps/st_a.json`
- Test: `internal/track/acceptance_test.go`

**Interfaces:**
- Consumes: весь конвейер.
- Produces: рабочую карту станции.

**Acceptance Criteria:**
- Станция содержит: четыре пути, две горловины, тупик отстоя локомотивов,
  подъездной путь предприятия с одной стрелкой примыкания.
- `maps/st_a.json` проходит разбор, валидацию и компиляцию без ошибок.
- Замыкание всех циклов сходится в пределах допуска.
- Сквозной тест грузит настоящий файл из репозитория, а не строку в коде.
- `go run ./cmd/clearahead` поднимается и отдаёт геометрию по `curl`.

**Как строится геометрия горловины** (повторено здесь, потому что задачи читают
по отдельности): угол крестовины 1/9 равен arctan(1/9) ≈ 0,110657 рад; в карте
используется округлённое 0,1107, и все позы посчитаны с ним. Стрелка: прямой
проход — прямая 33,5 м; отклонённый — дуга радиусом 300 м на угол −0,1107 (вправо)
или +0,1107 (влево). Чтобы вывести путь на междупутье 5,3 м, за стрелкой ставится
прямая вставка 14,731 м и обратная дуга того же радиуса на противоположный угол:
две дуги 1/9 без вставки дают только 3,67 м.

- [ ] **Шаг 1: Написать карту**

Создать `maps/st_a.json` по образцу §11 спеки, развернув его до полной станции.
Порядок построения, чтобы позы сходились:

1. Западная граница `ST_A_W.P1` — якорь в (0, 0, 142, heading 0).
2. Подход 300 м до первой стрелки горловины.
3. Горловина запада: три стрелки подряд по одной схеме — прямой проход остаётся
   на текущем пути, отклонённый уходит на следующий через вставку 14,731 м и
   обратную дугу. Пути 1–4.
4. Пути 1–4 длиной 850 м каждый.
5. Горловина востока — зеркально: рукость `left` вместо `right`, знаки углов
   меняются.
6. Тупик отстоя: стрелка от пути 4, отклонённый проход, ребро 120 м, порт с
   `purpose: "buffer_stop"`.
7. Подъездной путь предприятия: стрелка примыкания от пути 1, ребро 400 м,
   порт с `purpose: "buffer_stop"`.
8. Платформы у путей 2 и 3 как `trackside` с `kind: "platform"`.

Станция плоская: вертикальных цепочек нет ни у одного элемента.

- [ ] **Шаг 2: Приёмочный тест**

```go
package track

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/shady2k/ClearAhead/internal/mapfmt"
)

// TestStationCompiles грузит настоящий файл карты из репозитория. Строка в коде
// проверила бы компилятор, но не карту — а ошибка автора карты и есть то, ради
// чего написан валидатор.
func TestStationCompiles(t *testing.T) {
	path := filepath.Join("..", "..", "maps", "st_a.json")
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("карта: %v", err)
	}
	defer f.Close()

	m, err := mapfmt.Decode(f)
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if err := mapfmt.Validate(m); err != nil {
		t.Fatalf("валидация: %v", err)
	}
	ct, rg, err := Compile(m)
	if err != nil {
		t.Fatalf("компиляция (замыкание не сошлось?): %v", err)
	}
	if len(m.Topology.Turnouts) < 6 {
		t.Fatalf("стрелок %d, для двух горловин, тупика и примыкания нужно не меньше 6",
			len(m.Topology.Turnouts))
	}
	if len(ct.Elements) != len(rg.Elements) {
		t.Fatalf("элементов в CompiledTrack %d, в RenderGeometry %d", len(ct.Elements), len(rg.Elements))
	}
	// Станция плоская: пространственная длина совпадает с плановой.
	for id, e := range ct.Elements {
		if e.LengthS != e.LengthU {
			t.Fatalf("%s: s=%s u=%s, станция объявлена плоской", id, e.LengthS, e.LengthU)
		}
	}
}
```

- [ ] **Шаг 3: Тесты проходят**

Run: `go test ./... -v`
Expected: PASS во всех пакетах

Если замыкание не сошлось — тест печатает элемент и невязку в миллиметрах.
Править надо карту, а не допуск.

- [ ] **Шаг 4: Проверка живьём**

```bash
go run ./cmd/clearahead -map maps/st_a.json &
sleep 1
curl -sD- http://localhost:8080/maps/ST_A/revisions/1/geometry | head -20
```

Expected: `HTTP/1.1 200 OK`, заголовки `ETag` и `Cache-Control: ... immutable`,
тело с элементами.

```bash
ETAG=$(curl -sI http://localhost:8080/maps/ST_A/revisions/1/geometry | grep -i etag | cut -d' ' -f2 | tr -d '\r')
curl -sD- -H "If-None-Match: $ETAG" http://localhost:8080/maps/ST_A/revisions/1/geometry | head -3
```

Expected: `HTTP/1.1 304 Not Modified`

- [ ] **Шаг 5: Коммит**

```bash
git add maps/ internal/track/acceptance_test.go
git commit -m "feat: карта станции ST_A и сквозной приёмочный тест [ClearAhead-0xc]"
```

---

## Что НЕ входит в этот план

**Клиент на Godot 4.x** — вторая половина вехи В1: разбор геометрии, сцена
`World`/`Camera2D`/`UI`/`Debug`, камера в метрах, зум к курсору, панорама, стиль
из эскиза, толщина линий в экранных пикселях, LOD по зуму. Отдельный план:
другой язык, другой тулчейн, другой способ проверки.

**Веха В1 не закрыта, пока клиента нет.** Её видимый критерий — «запускаете
сервер, запускаете клиент, видите станцию целиком», а после этого плана видно
только `curl`. Это осознанное разделение работы, а не сокращение объёма вехи.

**Изоляция симуляции** — правило принято, строится в В2 вместе с тиком, потому
что в В1 симуляции не существует и изолировать нечего. Записано здесь, чтобы В1
не закрыл дверь:

- Симуляция живёт в **своей горутине** и владеет всем изменяемым состоянием
  единолично. Наружу — только сообщения по каналам; **разделяемой изменяемой
  памяти нет вовсе.** Это уже требование `sim-core-design` («движок —
  единственный владелец изменяемого состояния»), и именно оно делает изоляцию
  настоящей, а не декоративной.
- Над горутиной — супервизор с `recover()`: падение симуляции переводит её в
  состояние «мертва», HTTP продолжает отдавать статику, клиенты получают явный
  отказ вместо тишины, супервизор поднимает симуляцию заново с последней
  консистентной точки.
- **Чего этого не хватает, честно.** `recover()` не ловит исчерпание памяти,
  переполнение стека, обнаруженный рантаймом дедлок и `os.Exit`. И он бесполезен,
  если упавшая горутина успела испортить общую память — поэтому владение
  состоянием важнее самого `recover()`. Гонки ловятся прогоном тестов под
  `-race`.
- **Настоящая изоляция — граница процесса**, и она остаётся открытой: раз обмен
  уже идёт сообщениями, вынос симуляции в отдельный процесс позже — смена
  транспорта, а не переписывание. Сегодня третий процесс не заводится: их и так
  два (сервер и Godot), а цена IPC и супервизора платится без потребителя.
- В В1 правило выполняется тривиально: изменяемого состояния нет, HTTP-слой
  держит только неизменяемые скомпилированные артефакты.

**Отложено по спеке и здесь не появляется:** клотоида, круговая вертикальная
кривая, `cant`, слой схемы, рельеф и визуальный контент, сигналы и участки
обнаружения, каталог стрелок, импорт OSM, решатель замыкания, `SToU` (обратное
отображение — понадобится в В3, когда поезд поедет и позу нужно будет искать по
`s`).

**Профиль валидации** (`validation_profile` из §10.4 спеки): в В1 инженерных
ограничений — минимального радиуса и предельного уклона — не проверяется вовсе,
потому что станция плоская и радиусы заданы автором сознательно. Номер профиля
вводится вместе с первой такой проверкой в В3. Это явный перенос, а не пропуск.
