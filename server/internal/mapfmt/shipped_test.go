package mapfmt_test

import (
	"path/filepath"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
)

// shippedMapPath — путь боевой карты из этого пакета. Сама строка живёт в
// mapfmt.ShippedMapPath: у неё два потребителя — сервер и тесты, — и второе
// написание разошлось бы с первым молча.
func shippedMapPath() string { return filepath.Join("..", "..", mapfmt.ShippedMapPath) }

// Карта, которая едет в репозитории, обязана проходить вход: строгий разбор и
// валидацию.
//
// # Почему этот тест — условие возврата файла, а не формальность
//
// Довод, которым JSON был удалён из проекта 2026-08-11 (96ccacf), звучал так:
// карта, собранная кодом, не может разойтись со схемой — она перестаёт
// компилироваться, а JSON расходится молча. И это ПРАВДА: единственную карту
// репозитория тогда не грузил ни один тест, вся проверка держалась на том, что
// кто-то поднимет сервер руками.
//
// Довод верен, но он не про формат — он про проверку. Файл, который читает тест,
// расходится с шумом: смена версии формата, новое правило валидатора или правка
// схемы роняют `go test ./...` в тот же день, а не сервер владельца через месяц.
// Этот тест и есть цена, которой возврат файла оплачен.
func TestShippedMapPassesEntry(t *testing.T) {
	checkShippedMap(t, shippedMapPath())
}

func checkShippedMap(t *testing.T, path string) {
	t.Helper()

	m, err := mapfmt.DecodeFile(path)
	if err != nil {
		t.Fatalf("разбор %s: %v", path, err)
	}
	if m.FormatVersion != mapfmt.FormatVersion {
		t.Fatalf("версия формата карты %d, код ожидает %d", m.FormatVersion, mapfmt.FormatVersion)
	}
	if err := mapfmt.Validate(m); err != nil {
		t.Fatalf("валидация %s: %v", path, err)
	}

	// Регион мира зовётся по map_id (соглашение worldgen.Bootstrap), и Makefile
	// с клиентом просят ST_A. Разошедшееся имя дало бы 404 на живом сервере при
	// зелёных тестах.
	if m.MapID != "ST_A" {
		t.Fatalf("карта репозитория зовётся %q, а мир и клиент ждут ST_A", m.MapID)
	}

	// Решётка есть и направлена: у run'а направление обязательно у каждого
	// спана, и это ровно то различие, ради которого заводился общий тип.
	if m.Construction == nil || len(m.Construction.Runs) == 0 {
		t.Fatal("в карте нет ни одного run'а решётки")
	}
	for _, r := range m.Construction.Runs {
		if !r.Spans.Directed() {
			t.Fatalf("run %s: не у всех спанов задано направление", r.ID)
		}
	}

	// У платформы направления нет по существу — проверяем, что общий тип это
	// допускает, а не требует заполнять поле ради формы.
	for _, st := range m.Topology.Structures {
		if st.Kind != "platform" {
			continue
		}
		for _, iv := range st.Span {
			if iv.Direction.Directed() {
				t.Fatalf("платформа %s: у интервала на %s задано направление %q, хотя у платформы его нет",
					st.ID, iv.Element, iv.Direction)
			}
		}
	}
}

// Содержимое мира на месте: рельеф, покров, посёлок, река.
//
// Проверяются ЧИСЛА ШТУК, а не координаты. Число — целое, оно не зависит от
// машины и от порядка вычислений; координаты вышли из формулы меандра и
// сравнивать их байтами проект уже пробовал (4b4d6a7). Цель у теста одна:
// поймать выгрузку, потерявшую целый блок, — а такая потеря видна счётом.
func TestShippedMapCarriesWorldContent(t *testing.T) {
	m, err := mapfmt.DecodeFile(shippedMapPath())
	if err != nil {
		t.Fatalf("разбор: %v", err)
	}

	if m.Terrain == nil {
		t.Fatal("у карты нет рецепта рельефа: мир нечем засеять")
	}
	if len(m.Terrain.Octaves) != 2 {
		t.Fatalf("октав рельефа %d, ожидалось 2", len(m.Terrain.Octaves))
	}
	if m.Terrain.Cover == nil {
		t.Fatal("у рельефа нет рецепта покрова: земля приедет клиенту голой")
	}
	if m.Objects == nil {
		t.Fatal("у карты нет объектов: ни посёлка, ни реки")
	}
	if got := len(m.Objects.Buildings); got != 12 {
		t.Fatalf("домов посёлка %d, ожидалось 12", got)
	}
	if got := len(m.Objects.Rivers); got != 1 {
		t.Fatalf("рек %d, ожидалась 1", got)
	}
	// Сто одна точка — это ось от −400 до 1600 шагом 20 м включительно.
	if got := len(m.Objects.Rivers[0].Axis); got != 101 {
		t.Fatalf("точек оси реки %d, ожидался 101", got)
	}
}
