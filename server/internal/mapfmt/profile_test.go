package mapfmt_test

import (
	"strings"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/seedmap"
)

func TestУПрофиляЕстьВерсия(t *testing.T) {
	p := mapfmt.DefaultProfile()
	if p.Version != mapfmt.ProfileVersion {
		t.Fatalf("версия профиля %d, ожидали %d", p.Version, mapfmt.ProfileVersion)
	}
	if p.MinRadiusM <= 0 || p.MaxGrade <= 0 || p.MinTrackSpacingM <= 0 {
		t.Fatalf("нормы профиля должны быть положительными: %+v", p)
	}
}

// TestНормыОтвергаютТесныйРадиус — модуль норм обязан назвать себя в тексте
// отказа: карту отвергают по невозможности постройки, а не по форме, и автор
// карты должен видеть разницу.
//
// Рецепт решётки снят: дуга короче прямой, и покрытие ребра run'ом сломалось бы
// заодно — отказ пришёл бы от модуля отрисовки, который зовётся раньше.
func TestНормыОтвергаютТесныйРадиус(t *testing.T) {
	m := seedmap.Line(seedmap.WithoutConstruction(),
		геометрияПерегона([]mapfmt.HPrim{дуга(100, 0.2)}, nil))
	текст := отказ(t, m)
	if !strings.Contains(текст, "нормы:") {
		t.Fatalf("отказ обязан называть модуль «нормы», получено: %s", текст)
	}
	if !strings.Contains(текст, "радиус") {
		t.Fatalf("отказ обязан называть причину, получено: %s", текст)
	}
}

func TestНормыОтвергаютКрутойУклон(t *testing.T) {
	m := seedmap.Line(геометрияПерегона(
		[]mapfmt.HPrim{прямая(seedmap.LineLengthM)},
		[]mapfmt.VPrim{{Kind: "grade", Length: seedmap.LineLengthM, SlopePermille: 50}}))
	текст := отказ(t, m)
	if !strings.Contains(текст, "нормы:") || !strings.Contains(текст, "уклон") {
		t.Fatalf("отказ обязан называть модуль и причину, получено: %s", текст)
	}
}
