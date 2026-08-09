package mapstore

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/track"
)

func openStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("каталог карт: %v", err)
	}
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
	if purposes["N_WEST.P1"] != "map_boundary" || purposes["N_EAST.P1"] != "buffer_stop" {
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

// TestSaveAsThenList — список возвращает имя файла, map_id, ревизию и время
// правки: этого хватает, чтобы нарисовать экран выбора.
func TestSaveAsThenList(t *testing.T) {
	s := openStore(t)
	st, err := s.New()
	if err != nil {
		t.Fatal(err)
	}
	man, err := s.SaveAs("new_map.json", &st.Map)
	if err != nil {
		t.Fatalf("сохранить как: %v", err)
	}
	if man.MapID != "NEW" || man.Revision != 1 {
		t.Fatalf("манифест %+v, ожидалась NEW ревизии 1", man)
	}
	infos, err := s.List()
	if err != nil {
		t.Fatalf("список: %v", err)
	}
	if len(infos) != 1 {
		t.Fatalf("в списке %d карт, ожидалась 1", len(infos))
	}
	got := infos[0]
	if got.Name != "new_map.json" || got.MapID != "NEW" || got.Revision != 1 {
		t.Fatalf("запись %+v, ожидалась new_map.json/NEW/1", got)
	}
	if since := time.Since(got.Modified); since < 0 || since > time.Minute {
		t.Fatalf("время правки %v вне разумного окна", got.Modified)
	}
}

// TestSaveThenLoadRoundTrip — документ, ушедший на диск, возвращается
// загрузкой без искажений.
func TestSaveThenLoadRoundTrip(t *testing.T) {
	s := openStore(t)
	st, err := s.New()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveAs("m.json", &st.Map); err != nil {
		t.Fatal(err)
	}
	loaded, err := s.Load("m.json")
	if err != nil {
		t.Fatalf("загрузка: %v", err)
	}
	a, _ := json.Marshal(loaded.Map)
	b, _ := json.Marshal(st.Map)
	if !bytes.Equal(a, b) {
		t.Fatal("документ изменился при круговороте сохранить→загрузить")
	}
	if loaded.Name != "m.json" {
		t.Fatalf("имя %q, ожидалось m.json", loaded.Name)
	}
}

// TestSaveWithoutName — безымянную карту (новая, ещё не сохранённая) можно
// сохранить только через «сохранить как».
func TestSaveWithoutName(t *testing.T) {
	s := openStore(t)
	st, err := s.New()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save(&st.Map); !errors.Is(err, ErrUnnamed) {
		t.Fatalf("сохранение безымянной карты: %v, ожидался ErrUnnamed", err)
	}
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("после отказа в каталоге %d записей", len(entries))
	}
}

// TestSaveAsRejectsTraversal — пути ограничены каталогом карт. Отвергается
// «..» в любой форме, абсолютный путь и имя с разделителями; после отказов
// ничего не создано — ни внутри, ни снаружи.
func TestSaveAsRejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	st, err := s.New()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"../evil.json",  // обход через «..»
		"a/../b.json",   // «..» внутри имени
		"/etc/passwd",   // абсолютный путь
		"sub/evil.json", // разделитель пути
		`a\b.json`,      // разделитель в другой нотации
		"..",            // сам обход
		".",             // сам каталог
		"",              // пустое имя
	} {
		if _, err := s.SaveAs(name, &st.Map); !errors.Is(err, ErrName) {
			t.Fatalf("имя %q: ошибка %v, ожидался ErrName", name, err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("после отказов в каталоге %d записей", len(entries))
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "evil.json")); !os.IsNotExist(err) {
		t.Fatalf("файл создан за пределами каталога: %v", err)
	}
}

// TestSaveAsRejectsSymlinkOutside — симлинк, ведущий за пределы каталога,
// отвергается и на сохранении, и на загрузке. Проверяется итоговый путь после
// разрешения симлинков, а не строка до него.
func TestSaveAsRejectsSymlinkOutside(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	st, err := s.New()
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.json")
	if err := os.WriteFile(outside, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "evil.json")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}

	if _, err := s.SaveAs("evil.json", &st.Map); !errors.Is(err, ErrName) {
		t.Fatalf("сохранение в симлинк наружу: %v, ожидался ErrName", err)
	}
	if _, err := s.Load("evil.json"); !errors.Is(err, ErrName) {
		t.Fatalf("загрузка через симлинк наружу: %v, ожидался ErrName", err)
	}
	// Симлинк на месте, цель не тронута: запись не ушла за каталог.
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("симлинк заменён: %v", err)
	}
	b, err := os.ReadFile(outside)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "{}" {
		t.Fatalf("цель симлинка изменена: %q", b)
	}
}

// TestSaveRejectsInvalidMap — невалидная карта на диск не попадает: отказ на
// сохранении — это отказ, а не предупреждение, иначе следующая загрузка
// упрётся в неё, и чинить будет нечем.
func TestSaveRejectsInvalidMap(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	st, err := s.New()
	if err != nil {
		t.Fatal(err)
	}
	bad := st.Map
	bad.Anchors = map[string]mapfmt.Anchor{} // без якорей валидатор отвергает
	if _, err := s.SaveAs("bad.json", &bad); !errors.Is(err, ErrInvalid) {
		t.Fatalf("сохранение невалидной карты: %v, ожидался ErrInvalid", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "bad.json")); !os.IsNotExist(err) {
		t.Fatalf("невалидная карта попала на диск: %v", err)
	}
	// И текущей картой не стала.
	cur, _ := s.Current()
	if cur.Manifest.MapID != st.Manifest.MapID {
		t.Fatal("текущая карта сменилась на отвергнутую")
	}
}

// TestSaveRejectsUncompilableMap — карта, прошедшая валидатор, но не
// компилирующаяся, тоже не попадает на диск: загрузка проходит полный путь
// входа и такую карту не поднимет.
func TestSaveRejectsUncompilableMap(t *testing.T) {
	s := openStore(t)
	st, err := s.New()
	if err != nil {
		t.Fatal(err)
	}
	bad := st.Map
	// Вторая связная компонента без якоря: валидатор её пропускает (якоря
	// проверяются на существование, а не на покрытие компонент), а компилятор
	// отвергает — компонента без якоря не позиционируется.
	bad.Topology.Nodes = append(bad.Topology.Nodes,
		mapfmt.Node{ID: "N_SIDE_A", Ports: []mapfmt.Port{{ID: "P1", Purpose: "map_boundary"}}},
		mapfmt.Node{ID: "N_SIDE_B", Ports: []mapfmt.Port{{ID: "P1", Purpose: "buffer_stop"}}},
	)
	bad.Topology.Edges = append(bad.Topology.Edges,
		mapfmt.Edge{ID: "E_SIDE", From: "N_SIDE_A.P1", To: "N_SIDE_B.P1"})
	bad.Geometry.Edges["E_SIDE"] = mapfmt.Alignments{
		Horizontal: []mapfmt.HPrim{{Kind: "straight", Length: 100}},
	}
	bad.Construction.Runs = append(bad.Construction.Runs, mapfmt.ConstructionRun{
		ID: "RUN_SIDE", Coordinate: "u", Phase: 0,
		Spans: []mapfmt.RunSpan{{Element: "E_SIDE", From: 0, To: 100, Direction: "forward"}},
	})
	if err := mapfmt.Validate(&bad); err != nil {
		t.Fatalf("предусловие: карта обязана проходить валидатор: %v", err)
	}
	if _, _, err := track.Compile(&bad); err == nil {
		t.Fatal("предусловие: карта обязана не компилироваться")
	}
	if _, err := s.SaveAs("bad2.json", &bad); !errors.Is(err, ErrInvalid) {
		t.Fatalf("сохранение некомпилируемой карты: %v, ожидался ErrInvalid", err)
	}
	if _, err := os.Stat(filepath.Join(s.dir, "bad2.json")); !os.IsNotExist(err) {
		t.Fatalf("некомпилируемая карта попала на диск: %v", err)
	}
}

// TestLoadMissing — загрузки нет — отказ ErrNoSuch.
func TestLoadMissing(t *testing.T) {
	s := openStore(t)
	if _, err := s.Load("nope.json"); !errors.Is(err, ErrNoSuch) {
		t.Fatalf("загрузка отсутствующей карты: %v, ожидался ErrNoSuch", err)
	}
}

// TestLoadCorrupt — мусорный файл в каталоге — карта, которую нельзя
// загрузить: полный путь входа её отвергает.
func TestLoadCorrupt(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "garbage.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Load("garbage.json"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("загрузка мусора: %v, ожидался ErrInvalid", err)
	}
}

// TestListSkipsJunkAndSymlinkOutside — перечисление не выходит за каталог:
// симлинк наружу и некарта в списке не появляются, карта — появляется.
func TestListSkipsJunkAndSymlinkOutside(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "x.json")
	if err := os.WriteFile(outside, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "evil.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "junk.txt"), []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	st, err := s.New()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveAs("good.json", &st.Map); err != nil {
		t.Fatal(err)
	}
	infos, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 1 || infos[0].Name != "good.json" {
		t.Fatalf("список %+v, ожидалась одна карта good.json", infos)
	}
}

// TestSaveOverwrites — «сохранить как» поверх существующего имени заменяет
// файл, а не отказывает.
func TestSaveOverwrites(t *testing.T) {
	s := openStore(t)
	st, err := s.New()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveAs("m.json", &st.Map); err != nil {
		t.Fatal(err)
	}
	st2 := st
	st2.Map.MapID = "OTHER"
	if _, err := s.SaveAs("m.json", &st2.Map); err != nil {
		t.Fatalf("перезапись: %v", err)
	}
	loaded, err := s.Load("m.json")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Manifest.MapID != "OTHER" {
		t.Fatalf("после перезаписи map_id %q, ожидался OTHER", loaded.Manifest.MapID)
	}
}

// TestOpenCreatesDir — каталог карт создаётся, если его нет.
func TestOpenCreatesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sub", "maps")
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("каталог уже существует")
	}
	if _, err := Open(dir); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		t.Fatalf("каталог не создан: %v", err)
	}
}

// TestOpenRejectsEmptyDir — пустой каталог карт — ошибка оператора.
func TestOpenRejectsEmptyDir(t *testing.T) {
	if _, err := Open(""); err == nil {
		t.Fatal("Open(\"\") обязан отказать")
	}
}
