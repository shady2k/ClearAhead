package protocol

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
)

// TestManifestRequestParse — у запроса манифеста нет ни одного поля
// представления: единственный валидный вход — пустой. Сегменты пути или тело
// обязан отвергнуть сам Parse (барьер), а не обработчик.
func TestManifestRequestParse(t *testing.T) {
	var req ManifestRequest
	if err := req.Parse(Input{}); err != nil {
		t.Fatalf("пустой вход обязан приниматься: %v", err)
	}
	for name, in := range map[string]Input{
		"сегмент пути": {Path: map[string]string{"id": "ST_A"}},
		"тело":         {Body: json.RawMessage(`{}`)},
	} {
		if err := req.Parse(in); err == nil {
			t.Fatalf("%s: ожидался отказ барьера", name)
		}
	}
	// Ошибка обязана быть человекочитаемой и с префиксом пакета.
	err := req.Parse(Input{Path: map[string]string{"id": "ST_A"}})
	if err == nil || !strings.HasPrefix(err.Error(), "protocol:") {
		t.Fatalf("ошибка %v без префикса protocol:", err)
	}
}

// validMapBody — минимальный документ карты, проходящий строгий разбор
// (mapfmt.Decode): форма, а не валидация — её выполняет mapstore.
func validMapBody(t *testing.T) json.RawMessage {
	t.Helper()
	m := mapfmt.Map{
		FormatVersion: mapfmt.FormatVersion,
		MapID:         "T",
		MapRevision:   1,
		Anchors:       map[string]mapfmt.Anchor{"N1.P1": {X: 0, Y: 0, Z: 0, Heading: 0}},
		Topology: mapfmt.Topology{
			Nodes: []mapfmt.Node{
				{ID: "N1", Ports: []mapfmt.Port{{ID: "P1", Purpose: "map_boundary"}}},
				{ID: "N2", Ports: []mapfmt.Port{{ID: "P1", Purpose: "buffer_stop"}}},
			},
			Edges: []mapfmt.Edge{{ID: "E1", From: "N1.P1", To: "N2.P1"}},
		},
		Geometry: mapfmt.Geometry{
			Edges: map[string]mapfmt.Alignments{
				"E1": {Horizontal: []mapfmt.HPrim{{Kind: "straight", Length: 100}}},
			},
		},
	}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestEmptyMapRequestsParse — у запросов списка и новой карты нет ни одного
// поля представления: сегменты пути или тело обязан отвергнуть сам Parse
// (барьер), а не обработчик.
func TestEmptyMapRequestsParse(t *testing.T) {
	for name, req := range map[string]interface{ Parse(Input) error }{
		"список": &ListMapsRequest{},
		"новая":  &NewMapRequest{},
	} {
		if err := req.Parse(Input{}); err != nil {
			t.Fatalf("%s: пустой вход обязан приниматься: %v", name, err)
		}
		for what, in := range map[string]Input{
			"сегмент пути": {Path: map[string]string{"id": "x"}},
			"тело":         {Body: json.RawMessage(`{}`)},
		} {
			if err := req.Parse(in); err == nil {
				t.Fatalf("%s: %s: ожидался отказ барьера", name, what)
			}
		}
	}
}

// TestLoadMapRequestParse — имя — единственное поле загрузки: пустое или
// длинное имя и тело — отказ барьера.
func TestLoadMapRequestParse(t *testing.T) {
	var req LoadMapRequest
	if err := req.Parse(Input{Path: map[string]string{"name": "st.json"}}); err != nil {
		t.Fatalf("валидное имя: %v", err)
	}
	if req.Name() != "st.json" {
		t.Fatalf("имя %q, ожидалось st.json", req.Name())
	}
	if err := req.Parse(Input{Path: map[string]string{"name": ""}}); err == nil {
		t.Fatal("пустое имя обязано отвергаться")
	}
	if err := req.Parse(Input{Path: map[string]string{"name": strings.Repeat("a", 257)}}); err == nil {
		t.Fatal("слишком длинное имя обязано отвергаться")
	}
	if err := req.Parse(Input{Path: map[string]string{"name": "st.json"}, Body: json.RawMessage(`{}`)}); err == nil {
		t.Fatal("тело у загрузки обязано отвергаться")
	}
}

// TestSaveMapRequestParse — тело сохранения — документ карты: разбор строгий
// (mapfmt.Decode), путь запрещён.
func TestSaveMapRequestParse(t *testing.T) {
	var req SaveMapRequest
	if err := req.Parse(Input{Body: validMapBody(t)}); err != nil {
		t.Fatalf("валидное тело: %v", err)
	}
	if req.Map().MapID != "T" {
		t.Fatalf("map_id %q, ожидался T", req.Map().MapID)
	}
	if err := req.Parse(Input{}); err == nil {
		t.Fatal("пустое тело обязано отвергаться")
	}
	if err := req.Parse(Input{Body: json.RawMessage(`{`)}); err == nil {
		t.Fatal("битое тело обязано отвергаться")
	}
	if err := req.Parse(Input{Path: map[string]string{"name": "x"}, Body: validMapBody(t)}); err == nil {
		t.Fatal("сегменты пути у сохранения обязаны отвергаться")
	}
	// Ошибка разбора тела — с префиксом пакета, как все ошибки барьера.
	err := req.Parse(Input{Body: json.RawMessage(`{"format_version": 999}`)})
	if err == nil || !strings.HasPrefix(err.Error(), "protocol:") {
		t.Fatalf("ошибка %v без префикса protocol:", err)
	}
}

// TestSaveAsMapRequestParse — имя из пути, документ из тела; пустое имя —
// отказ барьера.
func TestSaveAsMapRequestParse(t *testing.T) {
	var req SaveAsMapRequest
	if err := req.Parse(Input{Path: map[string]string{"name": "st.json"}, Body: validMapBody(t)}); err != nil {
		t.Fatalf("валидный вход: %v", err)
	}
	if req.Name() != "st.json" || req.Map().MapID != "T" {
		t.Fatalf("разбор имени %q или карты %q", req.Name(), req.Map().MapID)
	}
	if err := req.Parse(Input{Path: map[string]string{"name": ""}, Body: validMapBody(t)}); err == nil {
		t.Fatal("пустое имя обязано отвергаться")
	}
	if err := req.Parse(Input{Path: map[string]string{"name": "st.json"}, Body: json.RawMessage(`{`)}); err == nil {
		t.Fatal("битое тело обязано отвергаться")
	}
}
