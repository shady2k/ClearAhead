package match

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/content"
	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/seedmap"
	"github.com/shady2k/ClearAhead/server/internal/track"
)

// Идентификаторы локомотивов ручных расстановок — UUIDv7 (решение владельца
// «UUIDv7 везде»): у боевой расстановки (maps/st_a_placement.json) LOCO_1 =
// 01a3185c-6001-…, и фикстура продолжает тот же ряд; LOCO_2 в боевой
// расстановке нет, и его имя живёт в name.
const (
	loco1ID = "01a3185c-6001-7242-8242-000000424242" // метка LOCO_1
	loco2ID = "01a3185c-6002-7242-8242-000001424242" // метка LOCO_2
)

// station компилирует карту фабрики: расстановка проверяется об элементы и их
// длины, и брать их надо оттуда же, откуда берёт сервер.
func station(t *testing.T) *track.CompiledNetwork {
	t.Helper()
	m := seedmap.Station()
	if err := mapfmt.Validate(m); err != nil {
		t.Fatalf("фикстура: %v", err)
	}
	cn, _, err := track.Compile(m)
	if err != nil {
		t.Fatalf("компиляция: %v", err)
	}
	return cn
}

// set собирает набор контента с одним типом длиной 34.18 м — тем же, что боевой.
func set(t *testing.T) *content.Set {
	t.Helper()
	dir := t.TempDir()
	body := []byte("не glb, но подрезка не запрашивается")
	if err := os.WriteFile(filepath.Join(dir, "x.bin"), body, 0o600); err != nil {
		t.Fatalf("фикстура: %v", err)
	}
	doc := map[string]any{
		"format_version": content.FormatVersion,
		"assets": []any{map[string]any{
			"name": "vid", "file": "x.bin", "media_type": "application/octet-stream",
			"source_hash": content.Addr(sha256hex(body)),
			"anchor":      "rail_top_gauge_center", "scale": 1.0, "translation": []any{0, 0, 0},
			"attribution": map[string]any{"title": "T", "author": "A", "source": "S",
				"license": "CC0-1.0", "modified": false},
		}},
		"stock": []any{map[string]any{
			"id": "VL80", "length": 34.18, "bogie_base": 24.71, "width": 3.63, "height": 5.4,
			"mass": 192.0, "max_speed": 110.0,
			"resistance": map[string]any{"a": 1.9, "b": 0.01, "c": 0.0003},
			"brake":      map[string]any{"shoes": "cast_iron", "braked_axles": 8, "axle_force": 137.3},
			"traction": map[string]any{
				"adhesive_mass": 192.0, "continuous_force": 401.1, "continuous_speed": 53.6,
			},
			"appearance": "vid",
		}},
	}
	raw, _ := json.Marshal(doc)
	if err := os.WriteFile(filepath.Join(dir, content.FileName), raw, 0o600); err != nil {
		t.Fatalf("фикстура: %v", err)
	}
	s, err := content.Load(dir)
	if err != nil {
		t.Fatalf("набор: %v", err)
	}
	return s
}

func write(t *testing.T, doc map[string]any) string {
	t.Helper()
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("фикстура: %v", err)
	}
	p := filepath.Join(t.TempDir(), "placement.json")
	if err := os.WriteFile(p, raw, 0o600); err != nil {
		t.Fatalf("фикстура: %v", err)
	}
	return p
}

func good() map[string]any {
	return map[string]any{
		"format_version": FormatVersion,
		"region":         "ST_A",
		"units": []any{map[string]any{
			"id": loco1ID, "name": "LOCO_1", "type": "VL80",
			"at": map[string]any{"element": seedmap.StationMain, "u": 150.0, "direction": "forward"},
		}},
	}
}

func TestStartPlacesUnit(t *testing.T) {
	m, err := Start("M1", write(t, good()), station(t), set(t))
	if err != nil {
		t.Fatalf("расстановка: %v", err)
	}
	if len(m.Units) != 1 || m.Units[0].ID != loco1ID || m.Units[0].Name != "LOCO_1" {
		t.Fatalf("единицы %+v", m.Units)
	}
	if m.Region != "ST_A" || m.ID != "M1" {
		t.Fatalf("партия %s региона %s", m.ID, m.Region)
	}
}

// TestStartWithoutFile — партия без подвижного состава законна. Пустой ключ и
// отсутствующий файл — РАЗНЫЕ вещи, и вторая проверяется ниже.
func TestStartWithoutFile(t *testing.T) {
	m, err := Start("M1", "", station(t), set(t))
	if err != nil {
		t.Fatalf("пустая расстановка отвергнута: %v", err)
	}
	if len(m.Units) != 0 {
		t.Fatalf("единиц %d, ожидалось 0", len(m.Units))
	}
}

func TestStartMissingFileRefused(t *testing.T) {
	_, err := Start("M1", filepath.Join(t.TempDir(), "нет.json"), station(t), set(t))
	if err == nil {
		t.Fatal("названного файла нет, а старт прошёл")
	}
}

// TestPlacementRefusals — расстановка, которую нельзя проверить, не поднимается.
func TestPlacementRefusals(t *testing.T) {
	cases := []struct {
		name   string
		want   string
		broken func(d map[string]any)
	}{
		{"чужой регион", "а мир поднят с", func(d map[string]any) { d["region"] = "ST_B" }},
		{"тип не объявлен", "в наборе контента не объявлен", func(d map[string]any) {
			unit(d)["type"] = "ЧМЭ3"
		}},
		{"элемента нет", "в сети нет", func(d map[string]any) {
			// Чужой, но настоящий UUIDv7: ребро E1 карты Line в сети станции
			// не существует.
			at(d)["element"] = seedmap.LineEdgeID
		}},
		{"направление не задано", "направление не задано", func(d map[string]any) {
			delete(at(d), "direction")
		}},
		{"не помещается концом", "не помещается", func(d map[string]any) {
			// E_MAIN длиной 230 м; машина 34.18 м серединой на 220-м метре
			// вылезает за конец на 5 с лишним метров.
			at(d)["u"] = 220.0
		}},
		{"не помещается началом", "не помещается", func(d map[string]any) {
			at(d)["u"] = 5.0
		}},
		{"u за концом элемента", "больше длины элемента", func(d map[string]any) {
			at(d)["u"] = 400.0
		}},
		{"две единицы в одном месте", "накладывается", func(d map[string]any) {
			u2 := map[string]any{"id": loco2ID, "name": "LOCO_2", "type": "VL80",
				"at": map[string]any{"element": seedmap.StationMain, "u": 160.0, "direction": "forward"}}
			d["units"] = append(d["units"].([]any), u2)
		}},
		{"два одинаковых имени", "объявлена дважды", func(d map[string]any) {
			d["units"] = append(d["units"].([]any), unit(d))
		}},
		{"неизвестное поле", "unknown field", func(d map[string]any) {
			unit(d)["speed"] = 12.5
		}},
		{"версия формата", "версия формата", func(d map[string]any) {
			d["format_version"] = FormatVersion + 1
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := good()
			c.broken(d)
			_, err := Start("M1", write(t, d), station(t), set(t))
			if err == nil {
				t.Fatal("испорченная расстановка поднялась")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("отказ %q не называет причину %q", err, c.want)
			}
		})
	}
}

// TestUnitsTouchingEndsAllowed — интервалы занятости ПОЛУОТКРЫТЫЕ: две машины,
// стоящие впритык, наложением не считаются.
//
// Соглашение принято раньше и не здесь (ClearAhead-5zd), но применяется впервые:
// при целых микрометрах равенство концов достижимо, и решать по нему «занято ли»
// — ровно тот класс ошибок, ради которого соглашение и записывалось.
func TestUnitsTouchingEndsAllowed(t *testing.T) {
	d := good()
	at(d)["u"] = 100.0
	d["units"] = append(d["units"].([]any), map[string]any{
		"id": loco2ID, "name": "LOCO_2", "type": "VL80",
		// Ровно длина машины дальше: хвост первой и голова второй в одной точке.
		"at": map[string]any{"element": seedmap.StationMain, "u": 134.18, "direction": "forward"},
	})
	if _, err := Start("M1", write(t, d), station(t), set(t)); err != nil {
		t.Fatalf("машины впритык отвергнуты: %v", err)
	}
}

// TestShippedPlacementLoads — боевая расстановка репозитория проводится через
// полный вход. Тот же приём, что у карты и набора, и по той же причине: файл,
// который не читает ни один тест, расходится молча.
func TestShippedPlacementLoads(t *testing.T) {
	dir := filepath.Join("..", "..", "assets")
	s, err := content.Load(dir)
	if err != nil {
		t.Fatalf("боевой набор: %v", err)
	}
	m, err := Start("M1", filepath.Join("..", "..", "maps", "st_a_placement.json"), station(t), s)
	if err != nil {
		t.Fatalf("боевая расстановка не проходит вход: %v", err)
	}
	if len(m.Units) == 0 {
		t.Fatal("в боевой расстановке нет ни одной единицы: локомотива на станции не будет")
	}
}

func unit(d map[string]any) map[string]any { return d["units"].([]any)[0].(map[string]any) }
func at(d map[string]any) map[string]any   { return unit(d)["at"].(map[string]any) }

// sha256hex — хеш фикстуры. Пакет content считает его сам, тесту нужен только
// для того, чтобы объявить его в наборе заранее.
func sha256hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
