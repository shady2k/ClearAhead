package sourcestore

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/terrain"
	"github.com/shady2k/ClearAhead/server/internal/vegetation"
)

// openAt открывает хранилище в свежем временном каталоге: каждый тест — своя
// база, файлы между тестами не делятся.
func openAt(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "sources.db"))
	if err != nil {
		t.Fatalf("хранилище исходников: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// sampleMap — маленькая законная карта-заготовка: хранилищу нужен только
// сериализуемый документ, валидацию карты держит edit.
func sampleMap() *mapfmt.Map {
	return &mapfmt.Map{FormatVersion: 2, MapID: "T", MapRevision: 1}
}

// sampleVegetation — множество всех четырёх форм исходников растительности.
func sampleVegetation() vegetation.Sources {
	return vegetation.Sources{
		Cuts:      []vegetation.Cut{{CX: 3, CZ: 4, I: 10, J: 11}, {CX: 3, CZ: 4, I: 12, J: 13}},
		Planted:   []vegetation.Planted{{CX: 5, CZ: 6, I: 0, J: 1}},
		Clearings: []vegetation.Clearing{{MinX: 100, MinZ: 200, MaxX: 300, MaxZ: 400}},
		CutMasks:  []vegetation.CutMask{{CX: 7, CZ: 8, Bits: []byte{0x01, 0x80}}},
	}
}

// TestCommittedMapRoundTrip — карта, положенная транзакцией, читается как есть:
// хранилище обязано вернуть документ без потерь (это же основание, по которому
// edit поднимает закоммиченное при старте).
func TestCommittedMapRoundTrip(t *testing.T) {
	s := openAt(t)
	if _, ok, err := s.GetCommittedMap(); err != nil || ok {
		t.Fatalf("пустое хранилище: ok=%v err=%v", ok, err)
	}
	tx, err := s.Begin()
	if err != nil {
		t.Fatal(err)
	}
	m := sampleMap()
	m.MapRevision = 7
	if err := tx.PutCommittedMap(m); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.GetCommittedMap()
	if err != nil || !ok {
		t.Fatalf("карта после записи: ok=%v err=%v", ok, err)
	}
	if got.MapID != "T" || got.MapRevision != 7 {
		t.Fatalf("карта разошлась: %+v", got)
	}
}

// TestGradingRoundTrip — правки высот, положенные транзакцией, читаются в
// детерминированном порядке (по клетке): сравнение эталонов не зависит от
// плана запроса.
func TestGradingRoundTrip(t *testing.T) {
	s := openAt(t)
	tx, err := s.Begin()
	if err != nil {
		t.Fatal(err)
	}
	want := terrain.Grading{Cells: []terrain.GradeCell{
		{Level: 0, CX: 0, CZ: -2, HeightCm: -120},
		{Level: 0, CX: 2, CZ: 1, HeightCm: 500},
	}}
	if err := tx.PutGrading(want); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetGrading()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("правки разошлись:\n got:  %+v\n want: %+v", got, want)
	}
}

// TestGradingReplacementIsSet — повторная запись правок ЗАМЕНЯЕТ множество
// целиком: клетка, правленная первым коммитом и не тронутая вторым, из
// множества исчезает. Множество, а не список — семантика исходников.
func TestGradingReplacementIsSet(t *testing.T) {
	s := openAt(t)
	tx, err := s.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.PutGrading(terrain.Grading{Cells: []terrain.GradeCell{
		{Level: 0, CX: 0, CZ: 0, HeightCm: 100},
		{Level: 0, CX: 1, CZ: 1, HeightCm: 200},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	tx, err = s.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.PutGrading(terrain.Grading{Cells: []terrain.GradeCell{
		{Level: 0, CX: 1, CZ: 1, HeightCm: 250},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetGrading()
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Cells) != 1 || got.Cells[0].CX != 1 || got.Cells[0].CZ != 1 || got.Cells[0].HeightCm != 250 {
		t.Fatalf("правки после замены: %+v", got.Cells)
	}
}

// TestVegetationRoundTrip — все четыре формы растительности переживают запись
// и чтение без потерь.
func TestVegetationRoundTrip(t *testing.T) {
	s := openAt(t)
	want := sampleVegetation()
	if err := s.PutVegetation(want); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetVegetation()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("растительность разошлась:\n got:  %+v\n want: %+v", got, want)
	}
}

// TestVegetationReplacementIsSet — PutVegetation заменяет множество целиком:
// старая рубка не переживает новый набор.
func TestVegetationReplacementIsSet(t *testing.T) {
	s := openAt(t)
	if err := s.PutVegetation(vegetation.Sources{Cuts: []vegetation.Cut{{CX: 0, CZ: 0, I: 1, J: 1}}}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutVegetation(vegetation.Sources{Planted: []vegetation.Planted{{CX: 2, CZ: 2, I: 3, J: 3}}}); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetVegetation()
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Cuts) != 0 || len(got.Planted) != 1 {
		t.Fatalf("набор не заменён: рубок %d, посадок %d", len(got.Cuts), len(got.Planted))
	}
}

// TestCommitsAppendInOrder — история коммитов дописывается в конец, и позиции
// (seq) не имеют дыр: окно конфликта edit читает историю по индексу.
func TestCommitsAppendInOrder(t *testing.T) {
	s := openAt(t)
	tx, err := s.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.AppendCommit(CommitRecord{Elements: []string{"e2", "e1"}, Cells: []GradeCell{{CX: 1, CZ: 0}}}); err != nil {
		t.Fatal(err)
	}
	if err := tx.AppendCommit(CommitRecord{Elements: []string{"e3"}}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetCommits()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("записей журнала %d, ожидалось 2", len(got))
	}
	// Элементы и клетки хранятся отсортированными: запись детерминирована.
	if !reflect.DeepEqual(got[0].Elements, []string{"e1", "e2"}) {
		t.Fatalf("запись 0: %+v", got[0])
	}
	if len(got[1].Elements) != 1 || got[1].Elements[0] != "e3" {
		t.Fatalf("запись 1: %+v", got[1])
	}
}

// TestCommitPersistsAcrossReopen — НАСТОЯЩИЙ перезапуск хранилища: база
// закрывается и открывается заново, и всё, что положено транзакцией, на месте.
// Это тот же класс перезапуска, что переживает edit.Service (sqym.18).
func TestCommitPersistsAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sources.db")

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := s.Begin()
	if err != nil {
		t.Fatal(err)
	}
	m := sampleMap()
	if err := tx.PutCommittedMap(m); err != nil {
		t.Fatal(err)
	}
	if err := tx.PutGrading(terrain.Grading{Cells: []terrain.GradeCell{{Level: 0, CX: 0, CZ: -2, HeightCm: 500}}}); err != nil {
		t.Fatal(err)
	}
	if err := tx.AppendCommit(CommitRecord{Cells: []GradeCell{{CX: 0, CZ: -2}}}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	// Перезапуск: новое хранилище над тем же файлом.
	s2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	got, ok, err := s2.GetCommittedMap()
	if err != nil || !ok {
		t.Fatalf("карта после перезапуска: ok=%v err=%v", ok, err)
	}
	if got.MapID != "T" {
		t.Fatalf("карта после перезапуска: %+v", got)
	}
	g, err := s2.GetGrading()
	if err != nil || len(g.Cells) != 1 || g.Cells[0].HeightCm != 500 {
		t.Fatalf("правки после перезапуска: %+v err=%v", g.Cells, err)
	}
	recs, err := s2.GetCommits()
	if err != nil || len(recs) != 1 {
		t.Fatalf("журнал после перезапуска: %+v err=%v", recs, err)
	}
}

// TestRollbackLeavesNothing — откат транзакции не оставляет ни одной записи:
// частичной пачки «карта без правок» не существует.
func TestRollbackLeavesNothing(t *testing.T) {
	s := openAt(t)
	tx, err := s.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.PutCommittedMap(sampleMap()); err != nil {
		t.Fatal(err)
	}
	if err := tx.PutGrading(terrain.Grading{Cells: []terrain.GradeCell{{Level: 0, CX: 0, CZ: 0, HeightCm: 1}}}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.GetCommittedMap(); err != nil || ok {
		t.Fatalf("карта после отката: ok=%v err=%v", ok, err)
	}
	g, err := s.GetGrading()
	if err != nil || len(g.Cells) != 0 {
		t.Fatalf("правки после отката: %+v err=%v", g.Cells, err)
	}
}

// TestSecondTransactionAppendsAtRightPosition — транзакция поверх непустой
// истории дописывает запись в конец (seq = длина истории), а не с нуля:
// PRIMARY KEY истории — индекс окна конфликта, дыры в нём недопустимы.
func TestSecondTransactionAppendsAtRightPosition(t *testing.T) {
	s := openAt(t)
	tx, err := s.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.AppendCommit(CommitRecord{}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	tx, err = s.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.AppendCommit(CommitRecord{Elements: []string{"e"}}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetCommits()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || len(got[1].Elements) != 1 {
		t.Fatalf("история после второй транзакции: %+v", got)
	}
}
