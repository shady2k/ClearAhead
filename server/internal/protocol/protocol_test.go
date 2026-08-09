package protocol

import (
	"encoding/json"
	"strings"
	"testing"
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
