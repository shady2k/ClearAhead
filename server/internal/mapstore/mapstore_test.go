package mapstore

import (
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/seedmap"
	"github.com/shady2k/ClearAhead/server/internal/track"
)

func openStore(t *testing.T) *Store {
	t.Helper()
	s := Open()
	return s
}

// TestSeedPassesValidateAndCompile — затравка обязана проходить полный путь
// входа с первой секунды: New сама его прогоняет, а тест проверяет и форму, и
// вердикты валидатора и компилятора.
func TestSeedPassesValidateAndCompile(t *testing.T) {
	s := openStore(t)
	st, err := s.New()
	if err != nil {
		t.Fatalf("новая карта: %v", err)
	}
	if err := mapfmt.Validate(&st.Map); err != nil {
		t.Fatalf("затравка не проходит валидатор: %v", err)
	}
	if _, _, err := track.Compile(&st.Map); err != nil {
		t.Fatalf("затравка не компилируется: %v", err)
	}
	// Форма затравки: один прямой элемент, один якорь на нём, один конец
	// map_boundary (оттуда придёт перегон), другой buffer_stop, блок
	// construction с одним run'ом — иначе решётки не будет.
	if len(st.Map.Topology.Edges) != 1 {
		t.Fatalf("рёбер %d, ожидалось 1", len(st.Map.Topology.Edges))
	}
	if len(st.Map.Anchors) != 1 {
		t.Fatalf("якорей %d, ожидалось 1", len(st.Map.Anchors))
	}
	if st.Map.Construction == nil || len(st.Map.Construction.Runs) != 1 {
		t.Fatal("у затравки нет блока construction с run'ом")
	}
	purposes := map[string]string{}
	for _, n := range st.Map.Topology.Nodes {
		for _, p := range n.Ports {
			purposes[n.ID+"."+p.ID] = p.Purpose
		}
	}
	if purposes[seedmap.BlankWest+".P1"] != "map_boundary" || purposes[seedmap.BlankEast+".P1"] != "buffer_stop" {
		t.Fatalf("концы затравки без назначений: %+v", purposes)
	}
}

// TestEmptyStart — сервер поднимается без карты: пустой старт — норма.
func TestEmptyStart(t *testing.T) {
	s := openStore(t)
	if _, ok := s.Current(); ok {
		t.Fatal("пустой старт обязан не иметь карты")
	}
}

// TestNewSetsCurrent — «новая» делает затравку текущей картой.
func TestNewSetsCurrent(t *testing.T) {
	s := openStore(t)
	st, err := s.New()
	if err != nil {
		t.Fatal(err)
	}
	cur, ok := s.Current()
	if !ok || cur != st {
		t.Fatal("текущей картой стала не затравка")
	}
}
