package content

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
)

// ЧИСЛА ТЕЛА ЗДЕСЬ НЕ СВЕРЯЮТСЯ, и это нарочно. Высота станины и раствор
// балансира — решение автора набора, и тест, повторяющий их числом, проверял бы
// только то, что файл не менялся. Проверяется ФОРМА: что описание разбирается,
// что ссылки в нём разрешимы и что испорченное описание получает отказ.

// shippedModel — модель из боевого набора. Берётся с диска, а не из фикстуры:
// смысл проверки в том, что ОТГРУЖАЕМЫЙ файл разбирается сегодняшним читателем.
func shippedModel(t *testing.T, name string) *Model {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "assets", name+".model.json"))
	if err != nil {
		t.Fatalf("файл модели: %v", err)
	}
	m, err := ParseModel(name, raw)
	if err != nil {
		t.Fatalf("разбор модели %s: %v", name, err)
	}
	return m
}

// TestShippedModelsParse — обе отгружаемые модели разбираются и называют свой
// механизм. Это и есть та связь, ради которой перечня устройств заводить не
// пришлось: файл сам говорит, чьё он тело.
func TestShippedModelsParse(t *testing.T) {
	kinds := map[string]bool{}
	for _, name := range []string{"switch_stand_manual", "switch_stand_electric"} {
		m := shippedModel(t, name)
		if !mapfmt.KnownDrive(m.Device) {
			t.Fatalf("модель %s объявила механизм %q", name, m.Device)
		}
		if kinds[m.Device] {
			t.Fatalf("механизм %s объявлен двумя моделями", m.Device)
		}
		kinds[m.Device] = true
		if m.Title == "" {
			t.Fatalf("модель %s не назвала устройство человеку", name)
		}
	}
	// Обе руки механизма покрыты: карта вправе записать любую, и стрелка без
	// тела была бы пустым местом в мире.
	for _, kind := range mapfmt.Drives {
		if !kinds[kind] {
			t.Fatalf("у механизма %s нет отгружаемого тела", kind)
		}
	}
}

// TestModelDescribesTheIndicatorByISI — ПОКАЗАНИЯ УКАЗАТЕЛЯ ЕСТЬ В ДАННЫХ, а не
// в коде клиента.
//
// Проверяется не угол (он решение автора), а то, что подвижность указателя
// объявлена по положению остряка и что у обоих положений есть свой угол. Без
// этого клиент не смог бы показать положение вовсе, а с одним углом показывал бы
// оба положения одинаково.
func TestModelDescribesTheIndicatorByISI(t *testing.T) {
	m := shippedModel(t, "switch_stand_manual")
	var byState = map[string]map[string]float64{}
	var walk func(p Part)
	walk = func(p Part) {
		if p.Pivot != nil {
			byState[p.Pivot.By] = p.Pivot.States
		}
		for _, c := range p.Parts {
			walk(c)
		}
	}
	for _, p := range m.Parts {
		walk(p)
	}
	pos, ok := byState[StatePosition]
	if !ok {
		t.Fatal("у ручного механизма нет ни одной части, подвижной по положению остряка")
	}
	for _, want := range []string{"straight", "diverging"} {
		if _, ok := pos[want]; !ok {
			t.Fatalf("положение %s не имеет угла — указатель показал бы оба положения одинаково", want)
		}
	}
	if pos["straight"] == pos["diverging"] {
		t.Fatalf("оба положения дают один угол %v — показания не различаются", pos["straight"])
	}
	if _, ok := byState[StateHand]; !ok {
		t.Fatal("стрела указателя не привязана к стороне схода — по ИСИ она направлена в сторону бокового пути")
	}
	if _, ok := byState[StateSide]; !ok {
		t.Fatal("ни одна часть не привязана к стороне, с которой стоит механизм: табличка окажется в поле")
	}
}

// TestBrokenModelIsRefused — испорченное описание НЕ ЗАГРУЖАЕТСЯ.
//
// Каждый случай ловит опечатку, которая иначе доехала бы до клиента и стала
// предметом без половины деталей — то есть исправным на вид кадром.
func TestBrokenModelIsRefused(t *testing.T) {
	base := func(t *testing.T) map[string]any {
		t.Helper()
		raw, err := os.ReadFile(filepath.Join("..", "..", "assets", "switch_stand_manual.model.json"))
		if err != nil {
			t.Fatalf("файл модели: %v", err)
		}
		var doc map[string]any
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("фикстура: %v", err)
		}
		return doc
	}
	parts := func(doc map[string]any) []any { return doc["parts"].([]any) }
	first := func(doc map[string]any) map[string]any { return parts(doc)[0].(map[string]any) }

	cases := []struct {
		name   string
		want   string
		break_ func(map[string]any)
	}{
		{"версия формата", "версия формата", func(d map[string]any) { d["format_version"] = 2.0 }},
		{"чужие оси", "соглашение об осях", func(d map[string]any) { d["axes"] = "z_up" }},
		{"неизвестный механизм", "род механизма", func(d map[string]any) { d["device"] = "гидравлический" }},
		{"без имени устройства", "не названо имя устройства", func(d map[string]any) { d["title"] = "" }},
		{"имя файла не то", "лежит не под тем ассетом", func(d map[string]any) { d["model"] = "другое" }},
		{"неизвестная форма", "форма", func(d map[string]any) { first(d)["shape"] = "шар" }},
		{"краска не объявлена", "в палитре модели не объявлена", func(d map[string]any) {
			first(d)["material"] = "хаки"
		}},
		{"ящик о двух размерах", "ожидалось три", func(d map[string]any) {
			first(d)["size"] = []any{1.0, 2.0}
		}},
		{"поворот вокруг двух осей", "порядок поворотов", func(d map[string]any) {
			first(d)["rotate"] = []any{10.0, 20.0, 0.0}
		}},
		{"цвет не в шестнадцатеричном", "ожидается #rrggbb", func(d map[string]any) {
			d["materials"].(map[string]any)["iron"].(map[string]any)["colour"] = "чёрный"
		}},
		{"неизвестное поле", "unknown field", func(d map[string]any) { first(d)["shine"] = 1.0 }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			doc := base(t)
			c.break_(doc)
			raw, _ := json.Marshal(doc)
			_, err := ParseModel("switch_stand_manual", raw)
			if err == nil {
				t.Fatalf("испорченное описание принято")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("отказ %q не называет причину %q", err, c.want)
			}
		})
	}
}

// TestPivotOnUnknownStateIsRefused — подвижность по состоянию, которого мир не
// присылает, — отказ, а не часть, которая никогда не двинется.
func TestPivotOnUnknownStateIsRefused(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "assets", "switch_stand_manual.model.json"))
	if err != nil {
		t.Fatalf("файл модели: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("фикстура: %v", err)
	}
	for _, p := range doc["parts"].([]any) {
		part := p.(map[string]any)
		if pv, ok := part["pivot"].(map[string]any); ok {
			pv["by"] = "погода"
			break
		}
	}
	out, _ := json.Marshal(doc)
	_, err = ParseModel("switch_stand_manual", out)
	if err == nil || !strings.Contains(err.Error(), "которого мир не присылает") {
		t.Fatalf("подвижность по выдуманному состоянию принята: %v", err)
	}
}
