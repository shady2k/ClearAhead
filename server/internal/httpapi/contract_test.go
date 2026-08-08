package httpapi_test

import (
	"bytes"
	"encoding/json"
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
	MapID    string        `json:"map_id"`
	Revision int           `json:"map_revision"`
	Elements []wireElement `json:"elements"`
}

type wireElement struct {
	ID    string          `json:"id"`
	Start wireStart       `json:"start"`
	Prims []wirePrimitive `json:"primitives"`
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
