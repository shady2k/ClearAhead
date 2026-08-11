package mapfmt_test

import (
	"strings"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/seedmap"
)

func TestProfileHasVersion(t *testing.T) {
	p := mapfmt.DefaultProfile()
	if p.Version != mapfmt.ProfileVersion {
		t.Fatalf("версия профиля %d, ожидали %d", p.Version, mapfmt.ProfileVersion)
	}
	if p.MinRadiusM <= 0 || p.MaxGrade <= 0 || p.MinTrackSpacingM <= 0 {
		t.Fatalf("нормы профиля должны быть положительными: %+v", p)
	}
}

// TestProfileRejectsTightRadius — модуль норм обязан назвать себя в тексте
// отказа: карту отвергают по невозможности постройки, а не по форме, и автор
// карты должен видеть разницу.
//
// Рецепт решётки снят: дуга короче прямой, и покрытие ребра run'ом сломалось бы
// заодно — отказ пришёл бы от модуля отрисовки, который зовётся раньше.
func TestProfileRejectsTightRadius(t *testing.T) {
	m := seedmap.Line(seedmap.WithoutConstruction(),
		lineGeometry([]mapfmt.HPrim{arc(100, 0.2)}, nil))
	text := refusal(t, m)
	if !strings.Contains(text, "нормы:") {
		t.Fatalf("отказ обязан называть модуль «нормы», получено: %s", text)
	}
	if !strings.Contains(text, "радиус") {
		t.Fatalf("отказ обязан называть причину, получено: %s", text)
	}
}

func TestProfileRejectsSteepGrade(t *testing.T) {
	m := seedmap.Line(lineGeometry(
		[]mapfmt.HPrim{straight(seedmap.LineLengthM)},
		[]mapfmt.VPrim{{Kind: "grade", Length: seedmap.LineLengthM, SlopePermille: 50}}))
	text := refusal(t, m)
	if !strings.Contains(text, "нормы:") || !strings.Contains(text, "уклон") {
		t.Fatalf("отказ обязан называть модуль и причину, получено: %s", text)
	}
}
