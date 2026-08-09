package mapfmt

import (
	"os"
	"strings"
	"testing"
)

func TestDefaultProfileHasVersion(t *testing.T) {
	p := DefaultProfile()
	if p.Version != ProfileVersion {
		t.Fatalf("версия профиля %d, ожидали %d", p.Version, ProfileVersion)
	}
	if p.MinRadiusM <= 0 || p.MaxGrade <= 0 || p.MinTrackSpacingM <= 0 {
		t.Fatalf("нормы профиля должны быть положительными: %+v", p)
	}
}

// loadTestMap разбирает карту из testdata, фаталя на любой ошибке: карта
// фикстуры обязана быть читаемой, иначе тест проверяет не то.
func loadTestMap(t *testing.T, path string) *Map {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("карта %s: %v", path, err)
	}
	defer f.Close()
	m, err := Decode(f)
	if err != nil {
		t.Fatalf("разбор %s: %v", path, err)
	}
	return m
}

func TestProfileRejectsTightRadius(t *testing.T) {
	m := loadTestMap(t, "testdata/red/radius_below_min.json")
	err := Validate(m)
	if err == nil {
		t.Fatal("карта с радиусом ниже минимума обязана быть отвергнута")
	}
	if !strings.Contains(err.Error(), "нормы:") {
		t.Fatalf("отказ обязан называть модуль «нормы», получено: %v", err)
	}
	if !strings.Contains(err.Error(), "радиус") {
		t.Fatalf("отказ обязан называть причину, получено: %v", err)
	}
}

func TestProfileRejectsSteepGrade(t *testing.T) {
	m := loadTestMap(t, "testdata/red/grade_above_max.json")
	err := Validate(m)
	if err == nil {
		t.Fatal("карта с уклоном выше предела обязана быть отвергнута")
	}
	if !strings.Contains(err.Error(), "нормы:") || !strings.Contains(err.Error(), "уклон") {
		t.Fatalf("отказ обязан называть модуль и причину, получено: %v", err)
	}
}
