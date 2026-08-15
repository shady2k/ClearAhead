package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

const turnoutIDInParams = "018bcfe5-680a-7242-8242-00000a424242"

// TestTurnoutRequestParse — барьер команды перевода: что он обязан принять и
// что отвергнуть ДО обработчика.
//
// Положение здесь не сверяется с перечнем НАРОЧНО (см. Parse): перечень живёт в
// партии, и вторая его копия разошлась бы с первой. Барьер отвечает за форму —
// за то, что поле названо, что лишних полей нет и что идентификатор похож на
// идентификатор.
func TestTurnoutRequestParse(t *testing.T) {
	var req TurnoutRequest
	body := `{"turnout":"` + turnoutIDInParams + `","position":"diverging","command_id":"c1"}`
	if err := req.Parse(Input{Body: json.RawMessage(body)}); err != nil {
		t.Fatalf("законная команда отвергнута: %v", err)
	}
	if req.Turnout() != turnoutIDInParams || req.Position() != "diverging" {
		t.Fatalf("разобрано как %q / %q", req.Turnout(), req.Position())
	}
	// НЕИЗВЕСТНОЕ ПОЛОЖЕНИЕ ПРОХОДИТ БАРЬЕР: отказать обязана партия, и с
	// перечнем известных в тексте. Проверяется явно, чтобы правило не «починили»
	// вторым перечнем здесь.
	if err := req.Parse(Input{Body: json.RawMessage(
		`{"turnout":"` + turnoutIDInParams + `","position":"боком"}`)}); err != nil {
		t.Fatalf("неизвестное положение обязан отвергать домен, а не барьер: %v", err)
	}

	for name, in := range map[string]Input{
		"сегмент пути": {Path: map[string]string{"id": "SW"},
			Body: json.RawMessage(body)},
		"пустые params": {},
		"нет положения": {Body: json.RawMessage(`{"turnout":"` + turnoutIDInParams + `"}`)},
		"нет стрелки":   {Body: json.RawMessage(`{"position":"straight"}`)},
		"лишнее поле": {Body: json.RawMessage(
			`{"turnout":"` + turnoutIDInParams + `","position":"straight","drive":"manual"}`)},
		"мусор после params": {Body: json.RawMessage(
			`{"turnout":"` + turnoutIDInParams + `","position":"straight"} {}`)},
	} {
		var r TurnoutRequest
		err := r.Parse(in)
		if err == nil {
			t.Fatalf("%s: ожидался отказ барьера", name)
		}
		if !strings.HasPrefix(err.Error(), "protocol:") {
			t.Fatalf("%s: ошибка %v без префикса protocol:", name, err)
		}
	}
}
