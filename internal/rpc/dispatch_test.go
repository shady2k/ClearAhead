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
