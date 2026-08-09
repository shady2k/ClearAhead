package mapfmt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// decodeFile разбирает карту и отдаёт ошибку разбора, не фаталя: красная
// карта может падать и на разборе (дубликат ключа, не конечное число) — это
// тоже проверяемая причина, а не сбой фикстуры.
func decodeFile(t *testing.T, path string) (*Map, error) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("карта %s: %v", path, err)
	}
	defer f.Close()
	return Decode(f)
}

// TestRedCorpus доказывает каждое правило красной картой: каждый файл
// testdata/red/*.json обязан быть отвергнут, и текст отказа обязан содержать
// подстроку из соседнего файла *.want. Удаление правила из валидатора делает
// красным ровно свою карту.
func TestRedCorpus(t *testing.T) {
	entries, err := os.ReadDir("testdata/red")
	if err != nil {
		t.Fatalf("каталог красных карт не читается: %v", err)
	}
	seen := 0
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		seen++
		t.Run(e.Name(), func(t *testing.T) {
			want, err := os.ReadFile(filepath.Join("testdata/red", strings.TrimSuffix(e.Name(), ".json")+".want"))
			if err != nil {
				t.Fatalf("нет файла .want с ожидаемой причиной отказа: %v", err)
			}
			m, derr := decodeFile(t, filepath.Join("testdata/red", e.Name()))
			var got string
			if derr != nil {
				got = derr.Error()
			} else if verr := Validate(m); verr != nil {
				got = verr.Error()
			} else {
				t.Fatal("красная карта прошла валидацию")
			}
			if !strings.Contains(got, strings.TrimSpace(string(want))) {
				t.Fatalf("отказ по не той причине:\n получено: %s\n ожидали подстроку: %s", got, want)
			}
		})
	}
	if seen == 0 {
		t.Fatal("корпус красных карт пуст — правило считается недоказанным")
	}
}
