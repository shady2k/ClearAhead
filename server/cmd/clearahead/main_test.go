package main

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/shady2k/ClearAhead/server/internal/worldstore"
)

// openProjectionDB заводит базу проекций, как её заводит боевой сервер:
// схемой worldstore.
func openProjectionDB(t *testing.T, path string) *worldstore.Store {
	t.Helper()
	s, err := worldstore.Open(path)
	if err != nil {
		t.Fatalf("база проекций: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// addForeignTable кладёт в базу таблицу, которую схема проекций не знает.
func addForeignTable(t *testing.T, db *sql.DB, name string) {
	t.Helper()
	if _, err := db.Exec("CREATE TABLE " + name + " (x INTEGER)"); err != nil {
		t.Fatalf("таблица %s: %v", name, err)
	}
}

// TestAssertProjectionsOnlyAcceptsProjectionDB — база, несущая ровно проекции
// (схема worldstore), проходит сторож: пересев вправе её сносить.
func TestAssertProjectionsOnlyAcceptsProjectionDB(t *testing.T) {
	path := filepath.Join(t.TempDir(), "world.db")
	openProjectionDB(t, path)
	if err := assertProjectionsOnly(path); err != nil {
		t.Fatalf("база проекций не прошла сторож: %v", err)
	}
}

// TestAssertProjectionsOnlyRefusesSources — ПРИЁМОЧНЫЙ КРИТЕРИЙ sqym.18:
// исходники в базу проекций не ложатся, и сторож это держит. Таблица правок
// высот (grading — форма sourcestore) в базе проекций — отказ с именем
// таблицы, а не молчаливый снос: снос уничтожил бы работу игрока.
func TestAssertProjectionsOnlyRefusesSources(t *testing.T) {
	path := filepath.Join(t.TempDir(), "world.db")
	s := openProjectionDB(t, path)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	addForeignTable(t, db, "grading")
	addForeignTable(t, db, "vegetation_cuts")

	err = assertProjectionsOnly(path)
	if err == nil {
		t.Fatal("сторож пропустил базу с таблицами-исходниками")
	}
	if !strings.Contains(err.Error(), "grading") || !strings.Contains(err.Error(), "vegetation_cuts") {
		t.Fatalf("отказ не назвал таблицы-исходники: %v", err)
	}
}

// TestDropWorldRefusesSourcesDB — сам пересев, а не только сторож, отказывает
// базе проекций с исходниками внутри: -reseed не стёр бы работу игрока даже
// под силой. (dropWorld зовёт assertProjectionsOnly первым делом.)
func TestDropWorldRefusesSourcesDB(t *testing.T) {
	path := filepath.Join(t.TempDir(), "world.db")
	s := openProjectionDB(t, path)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	addForeignTable(t, db, "grading")
	db.Close()

	if err := dropWorld(path); err == nil {
		t.Fatal("пересев снёс базу с таблицей-исходником")
	}
	if _, err := sql.Open("sqlite", "file:"+path); err != nil {
		t.Fatalf("база после отказа: %v", err)
	}
}
