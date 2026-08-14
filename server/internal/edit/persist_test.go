// Персистентность исходников (sqym.18): закоммиченное переживает перезапуск.
//
// Приёмка волны требует НАСТОЯЩИЙ перезапуск, а не перенос переменной: сервис
// правок создаётся ЗАНОВО над тем же хранилищем, и закоммиченное поднимается
// из него. Путь «хранилище -> сервис» — тот самый, что собирает main.
package edit

import (
	"path/filepath"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/sourcestore"
	"github.com/shady2k/ClearAhead/server/internal/terrain"
	"github.com/shady2k/ClearAhead/server/internal/uuidv7"
)

// gradedCell — клетка правки высот уровня 0: единственный законный уровень
// (terrain.Grading.Validate).
func gradedCell(cx, cz int, height int16) terrain.GradeCell {
	return terrain.GradeCell{Level: 0, CX: cx, CZ: cz, HeightCm: height}
}

// openSources открывает хранилище исходников в свежем временном каталоге.
func openSources(t *testing.T) *sourcestore.Store {
	t.Helper()
	s, err := sourcestore.Open(filepath.Join(t.TempDir(), "sources.db"))
	if err != nil {
		t.Fatalf("хранилище исходников: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// gradeAndCommit — строитель правит клетку (0, -2) и принимает правку.
func gradeAndCommit(t *testing.T, svc *Service) {
	t.Helper()
	sess := openBuilder(t, svc, "b1")
	if _, err := sess.Apply(Intent{Op: OpGrade, Grade: GradeIntent{
		Cells: []terrain.GradeCell{gradedCell(0, -2, 500)},
	}}); err != nil {
		t.Fatalf("правка высот: %v", err)
	}
	if err := sess.Commit(); err != nil {
		t.Fatalf("коммит: %v", err)
	}
}

// TestCommitPersistsSourcesToStore — коммит кладёт закоммиченное в хранилище
// исходников: карту с подросшей ревизией, правку высот, запись журнала.
func TestCommitPersistsSourcesToStore(t *testing.T) {
	store := openSources(t)
	svc, err := NewServiceStored(store, testBaseMap(), uuidv7.Deterministic())
	if err != nil {
		t.Fatalf("сервис: %v", err)
	}
	gradeAndCommit(t, svc)

	got, ok, err := store.GetCommittedMap()
	if err != nil || !ok {
		t.Fatalf("карта в хранилище: ok=%v err=%v", ok, err)
	}
	if got.MapRevision != 2 {
		t.Fatalf("ревизия в хранилище %d, ожидалась 2", got.MapRevision)
	}
	g, err := store.GetGrading()
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Cells) != 1 || g.Cells[0].CX != 0 || g.Cells[0].CZ != -2 || g.Cells[0].HeightCm != 500 {
		t.Fatalf("правки в хранилище: %+v", g.Cells)
	}
	recs, err := store.GetCommits()
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 || len(recs[0].Cells) != 1 || recs[0].Cells[0].CX != 0 || recs[0].Cells[0].CZ != -2 {
		t.Fatalf("журнал в хранилище: %+v", recs)
	}
}

// TestServiceRestartsFromStore — НАСТОЯЩИЙ перезапуск: сервис создаётся заново
// над тем же хранилищем, и закоммиченное (карта, правки, позиция журнала)
// поднимается из него. Это тот же класс перезапуска, что у боевого сервера
// после -reseed: хранилище исходников переживает, база проекций нет.
func TestServiceRestartsFromStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sources.db")

	// Первая жизнь: коммит кладёт закоммиченное в хранилище.
	store, err := sourcestore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	svc, err := NewServiceStored(store, testBaseMap(), uuidv7.Deterministic())
	if err != nil {
		t.Fatalf("сервис: %v", err)
	}
	gradeAndCommit(t, svc)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	// Вторая жизнь: новое хранилище, НОВЫЙ сервис, та же карта-затравка.
	store2, err := sourcestore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store2.Close()
	svc2, err := NewServiceStored(store2, testBaseMap(), uuidv7.Deterministic())
	if err != nil {
		t.Fatalf("сервис после перезапуска: %v", err)
	}

	if got := svc2.Grading(); len(got.Cells) != 1 || got.Cells[0].HeightCm != 500 {
		t.Fatalf("правки после перезапуска: %+v", got.Cells)
	}
	if got := svc2.Committed(); got.MapRevision != 2 {
		t.Fatalf("закоммиченное после перезапуска: ревизия %d, ожидалась 2", got.MapRevision)
	}
	if seq := svc2.JournalSeq(); seq != 1 {
		t.Fatalf("позиция журнала после перезапуска %d, ожидалась 1", seq)
	}
}

// TestEmptyStoreBehavesLikeMemoryService — хранилище первой сессии пусто, и
// сервис над ним обязан быть неотличим от прежнего сервиса без хранилища:
// закоммиченное — карта-затравка, правок и журнала нет.
func TestEmptyStoreBehavesLikeMemoryService(t *testing.T) {
	store := openSources(t)
	mem, err := NewService(testBaseMap(), uuidv7.Deterministic())
	if err != nil {
		t.Fatal(err)
	}
	stored, err := NewServiceStored(store, testBaseMap(), uuidv7.Deterministic())
	if err != nil {
		t.Fatal(err)
	}
	if got := stored.Grading(); len(got.Cells) != 0 {
		t.Fatalf("правки пустого хранилища: %+v", got.Cells)
	}
	if mem.Committed().MapRevision != stored.Committed().MapRevision {
		t.Fatal("закоммиченное пустого хранилища разошлось с памятью")
	}
	if seq := stored.JournalSeq(); seq != 0 {
		t.Fatalf("позиция журнала пустого хранилища %d, ожидалась 0", seq)
	}
}

// TestCommitRefusedWhenStoreFails — отказ хранилища — отказ коммита ЦЕЛИКОМ:
// память сервиса и макет автора не тронуты, частичного принятия нет.
// Закрытое хранилище — честный способ сломать запись: Begin вернёт ошибку.
func TestCommitRefusedWhenStoreFails(t *testing.T) {
	store := openSources(t)
	svc, err := NewServiceStored(store, testBaseMap(), uuidv7.Deterministic())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	sess := openBuilder(t, svc, "b1")
	if _, err := sess.Apply(Intent{Op: OpGrade, Grade: GradeIntent{
		Cells: []terrain.GradeCell{gradedCell(0, -2, 500)},
	}}); err != nil {
		t.Fatalf("правка высот: %v", err)
	}
	if err := sess.Commit(); err == nil {
		t.Fatal("коммит над сломанным хранилищем не отказал")
	}
	// Мир не принял правку: макет цел, правок нет нигде.
	if got := svc.Grading(); len(got.Cells) != 0 {
		t.Fatalf("коммит-отказ оставил правки: %+v", got.Cells)
	}
	if len(sess.Journal()) != 1 {
		t.Fatalf("макет после отказа: журнал %d операций, ожидалась 1 (возврат автору)", len(sess.Journal()))
	}
}
