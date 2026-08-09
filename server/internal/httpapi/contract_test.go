package httpapi_test

import (
	"bytes"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/track"
)

// wireGeometry — контракт с клиентом, записанный отдельным типом.
//
// Это не копия track.RenderGeometry ради копии: доменный тип может меняться, а
// провод — нет без осознанного решения. Декодирование идёт с
// DisallowUnknownFields, поэтому лишнее поле в ответе сервера роняет тест, а не
// доезжает до клиента незамеченным.
type wireGeometry struct {
	MapID              string          `json:"map_id"`
	Revision           int             `json:"map_revision"`
	Elements           []wireElement   `json:"elements"`
	Trackside          []wireTrackside `json:"trackside"`
	TrackTypes         []wireTrackType `json:"track_types"`
	ConstructionRuns   []wireRun       `json:"construction_runs"`
	Features           []wireFeature   `json:"features"`
	PlacementAlgorithm string          `json:"placement_algorithm"`
}

type wireTrackType struct {
	ID      string      `json:"id"`
	Gauge   float64     `json:"gauge"`
	Sleeper wireSleeper `json:"sleeper"`
	Ballast wireBallast `json:"ballast"`
}

type wireSleeper struct {
	Pitch  float64 `json:"pitch"`
	Length float64 `json:"length"`
	Width  float64 `json:"width"`
}

type wireBallast struct {
	HalfWidth float64 `json:"half_width"`
}

type wireRun struct {
	ID         string        `json:"id"`
	Type       string        `json:"type"`
	Coordinate string        `json:"coordinate"`
	Phase      float64       `json:"phase"`
	Spans      []wireRunSpan `json:"spans"`
}

type wireRunSpan struct {
	Element   string  `json:"element"`
	From      float64 `json:"from"`
	To        float64 `json:"to"`
	Direction string  `json:"direction"`
}

type wireFeature struct {
	Owner     string        `json:"owner"`
	Kind      string        `json:"kind"`
	Point     wirePoint     `json:"point"`
	Addresses []wireAddress `json:"addresses"`
}

type wirePoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type wireAddress struct {
	Element string    `json:"element"`
	U       float64   `json:"u"`
	Tangent wirePoint `json:"tangent"`
}

type wireElement struct {
	ID    string          `json:"id"`
	Start wireStart       `json:"start"`
	Prims []wirePrimitive `json:"primitives"`
	Role  *wireRole       `json:"role"`
}

type wireRole struct {
	Turnout string `json:"turnout"`
	Branch  string `json:"branch"`
	Hand    string `json:"hand"`
	Frog    string `json:"frog,omitempty"`
}

type wireTrackside struct {
	ID     string     `json:"id"`
	Kind   string     `json:"kind"`
	Side   string     `json:"side"`
	Offset float64    `json:"offset,omitempty"`
	Width  float64    `json:"width,omitempty"`
	Spans  []wireSpan `json:"spans"`
}

type wireSpan struct {
	Element string  `json:"element"`
	From    float64 `json:"from"`
	To      float64 `json:"to"`
}

type wireStart struct {
	Plan  wirePose `json:"plan"`
	Z     float64  `json:"z"`
	Slope float64  `json:"slope"`
}

type wirePose struct {
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
	Heading float64 `json:"heading"`
}

type wirePrimitive struct {
	Kind   string  `json:"kind"`
	Length float64 `json:"length"`
	Radius float64 `json:"radius,omitempty"`
	Angle  float64 `json:"angle,omitempty"`
}

const goldenPath = "../../../contract/render_geometry.golden.json"

// TestWireContractMatchesGolden — контракт исполняют обе стороны.
//
// Эталон в contract/ — объявленная форма провода. Здесь она проверяется со
// стороны сервера: артефакт обязан декодироваться в wire-тип строго и совпасть
// с эталоном байт в байт. Клиент на Godot читает ТОТ ЖЕ файл и проверяет, что
// умеет его разобрать. Так расхождение формы ловится с обеих сторон, и ни одной
// внешней зависимости для этого не нужно.
//
// Тест упал — это не «обновить эталон». Сначала решите, намеренно ли изменился
// провод, и если да — обновите эталон И клиента.
func TestWireContractMatchesGolden(t *testing.T) {
	got := renderStation(t)

	// Обновление эталона — осознанное действие, а не побочный эффект прогона:
	// UPDATE_GOLDEN=1 go test ./internal/httpapi/ -run TestWireContract
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(filepath.Clean(goldenPath), append(got, '\n'), 0o644); err != nil {
			t.Fatalf("запись эталона: %v", err)
		}
		t.Log("эталон контракта обновлён — проверьте диффом, что изменение намеренное")
		return
	}

	want, err := os.ReadFile(filepath.Clean(goldenPath))
	if err != nil {
		t.Fatalf("эталон контракта не читается: %v (создайте его: см. TestWireContractDecodesStrictly)", err)
	}
	if !bytes.Equal(bytes.TrimSpace(got), bytes.TrimSpace(want)) {
		t.Errorf("форма провода разошлась с эталоном contract/render_geometry.golden.json.\n"+
			"Это контракт с клиентом на Godot: если изменение намеренное, обновите эталон И клиента.\n"+
			"получено %d байт, в эталоне %d байт", len(got), len(want))
	}
}

// TestWireContractDecodesStrictly — ответ сервера обязан укладываться в
// объявленный wire-тип без остатка.
func TestWireContractDecodesStrictly(t *testing.T) {
	raw := renderStation(t)
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var w wireGeometry
	if err := dec.Decode(&w); err != nil {
		t.Fatalf("ответ сервера не укладывается в объявленный контракт: %v", err)
	}
	if w.MapID == "" || len(w.Elements) == 0 {
		t.Fatalf("контракт декодировался, но пуст: %+v", w)
	}
	for _, e := range w.Elements {
		if e.ID == "" || len(e.Prims) == 0 {
			t.Fatalf("элемент без ID или без примитивов: %+v", e)
		}
		for _, p := range e.Prims {
			switch p.Kind {
			case "straight":
			case "arc":
				if p.Radius <= 0 || p.Angle == 0 {
					t.Fatalf("дуга без радиуса или угла: %+v", p)
				}
			default:
				t.Fatalf("неизвестный примитив в проводе: %q", p.Kind)
			}
			if p.Length <= 0 {
				t.Fatalf("примитив нулевой длины в проводе: %+v", p)
			}
		}
	}
	// Роли: у ветвей стрелки роль с полными данными, у обычных путей её нет.
	// Клиент не должен разбирать ID — значит, контракт обязан нести всё.
	var roles int
	for _, e := range w.Elements {
		if e.Role == nil {
			continue
		}
		roles++
		r := e.Role
		if r.Turnout == "" || r.Branch == "" || r.Hand == "" {
			t.Fatalf("роль без стрелки/ветви/руки: %+v", r)
		}
		switch r.Branch {
		case "straight", "diverging":
		default:
			t.Fatalf("неизвестная ветвь в роли: %q", r.Branch)
		}
		switch r.Hand {
		case "right", "left":
		default:
			t.Fatalf("неизвестная рукость в роли: %q", r.Hand)
		}
	}
	if roles == 0 {
		t.Fatal("ни один элемент не получил роль ветви стрелки")
	}

	// Путевые объекты: спаны в координате u, как в карте. Unknown kind не
	// пройдёт — клиент рисует только то, что знает.
	if len(w.Trackside) == 0 {
		t.Fatal("в контракте нет trackside")
	}
	for _, ts := range w.Trackside {
		if ts.ID == "" {
			t.Fatal("путевой объект без ID")
		}
		switch ts.Kind {
		case "platform", "buffer_stop":
		default:
			t.Fatalf("неизвестный kind путевого объекта: %q", ts.Kind)
		}
		if ts.Kind == "platform" && (ts.Offset <= 0 || ts.Width <= 0) {
			t.Fatalf("платформа %s без размеров: offset %g, width %g", ts.ID, ts.Offset, ts.Width)
		}
		if len(ts.Spans) == 0 {
			t.Fatalf("путевой объект %s без спанов", ts.ID)
		}
		for _, s := range ts.Spans {
			if s.Element == "" || s.From < 0 || s.To < s.From {
				t.Fatalf("путевой объект %s: неверный спан %+v", ts.ID, s)
			}
		}
	}

	// Рецепт решётки (спека §3–4): типы и run'ы с ЯВНЫМ типом, версия
	// алгоритма размещения. Клиент скрытого умолчания не применяет никогда.
	if w.PlacementAlgorithm == "" {
		t.Fatal("в контракте нет placement_algorithm")
	}
	if len(w.TrackTypes) == 0 {
		t.Fatal("в контракте нет типов путевой конструкции")
	}
	for _, tt := range w.TrackTypes {
		if tt.ID == "" || tt.Gauge <= 0 || tt.Sleeper.Pitch <= 0 ||
			tt.Sleeper.Length <= 0 || tt.Sleeper.Width <= 0 || tt.Ballast.HalfWidth <= 0 {
			t.Fatalf("тип без формы: %+v", tt)
		}
	}
	if len(w.ConstructionRuns) == 0 {
		t.Fatal("в контракте нет run'ов размещения")
	}
	for _, r := range w.ConstructionRuns {
		if r.ID == "" || r.Type == "" || r.Coordinate != "u" {
			t.Fatalf("run без явного типа или не в координате u: %+v", r)
		}
		if r.Phase < 0 || len(r.Spans) == 0 {
			t.Fatalf("run с неверной фазой или без спанов: %+v", r)
		}
		for _, s := range r.Spans {
			if s.Element == "" || s.From < 0 || s.To <= s.From {
				t.Fatalf("run %s: неверный спан %+v", r.ID, s)
			}
			switch s.Direction {
			case "forward", "reverse":
			default:
				t.Fatalf("run %s: направление %q", r.ID, s.Direction)
			}
		}
	}

	// Особенности уровня 2 (спека §5): крестовины с адресами и касательными.
	if len(w.Features) == 0 {
		t.Fatal("в контракте нет особенностей")
	}
	for _, f := range w.Features {
		if f.Owner == "" || f.Kind != "frog" || len(f.Addresses) != 2 {
			t.Fatalf("особенность без формы: %+v", f)
		}
		for _, a := range f.Addresses {
			if a.Element == "" || a.U < 0 {
				t.Fatalf("адрес особенности без формы: %+v", a)
			}
			if norm := a.Tangent.X*a.Tangent.X + a.Tangent.Y*a.Tangent.Y; math.Abs(norm-1) > 1e-6 {
				t.Fatalf("касательная адреса не единичная: %+v (норма %g)", a.Tangent, norm)
			}
		}
	}
}

// renderStation компилирует карту станции и сериализует геометрию так же, как
// это делает ручка.
func renderStation(t *testing.T) []byte {
	t.Helper()
	f, err := os.Open(filepath.Join("..", "..", "maps", "st_a.json"))
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
	_, rg, err := track.Compile(m)
	if err != nil {
		t.Fatalf("компиляция: %v", err)
	}
	// Байты берутся там же, где их берёт ручка и где считается ETag. Отступы
	// добавляются только для читаемости диффа: эталон обязан быть выводим из
	// отдаваемого тела, иначе он описывает не то, что уходит клиенту.
	body, err := track.RenderBody(rg)
	if err != nil {
		t.Fatalf("сериализация: %v", err)
	}
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, body, "", "  "); err != nil {
		t.Fatalf("форматирование: %v", err)
	}
	return pretty.Bytes()
}
