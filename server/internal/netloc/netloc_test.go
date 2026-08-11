package netloc

import (
	"strings"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/units"
)

// Направление задаётся полем, а не перестановкой концов. Проверка отдельная,
// потому что именно перестановка — соблазнительная и неверная альтернатива:
// она делает вывернутый интервал неотличимым от опечатки автора.
func TestИнтервалНеВыворачивается(t *testing.T) {
	iv := IntervalU{Element: "E1", From: 100, To: 20, Direction: DirReverse}
	err := iv.Structural()
	if err == nil {
		t.Fatal("вывернутый интервал принят")
	}
	if !strings.Contains(err.Error(), "вывернут") {
		t.Fatalf("отказ не называет причину: %v", err)
	}
}

// Пустое направление — законное значение: у платформы направления нет. Это не
// то же самое, что forward, и умолчания здесь нет.
func TestПустоеНаправлениеЗаконно(t *testing.T) {
	iv := IntervalU{Element: "E1", From: 0, To: 300}
	if err := iv.Structural(); err != nil {
		t.Fatalf("интервал без направления отвергнут: %v", err)
	}
	if iv.Direction.Directed() {
		t.Fatal("пустое направление сочтено заданным")
	}
	if !iv.Direction.Valid() {
		t.Fatal("пустое направление сочтено недопустимым")
	}
}

func TestНеизвестноеНаправлениеОтвергается(t *testing.T) {
	iv := IntervalU{Element: "E1", From: 0, To: 10, Direction: Direction("вперёд")}
	if err := iv.Structural(); err == nil {
		t.Fatal("неизвестное направление принято")
	}
}

func TestПустаяПротяжённостьОтвергается(t *testing.T) {
	var l LinearU
	if err := l.Structural(); err == nil {
		t.Fatal("пустая протяжённость принята")
	}
}

// Directed требует направление у ВСЕХ интервалов: run решётки, у которого
// направление есть только у части спанов, недоописан, а не «частично
// ненаправлен».
func TestDirectedТребуетВсеИнтервалы(t *testing.T) {
	full := LinearU{
		{Element: "E1", From: 0, To: 10, Direction: DirForward},
		{Element: "E2", From: 0, To: 20, Direction: DirReverse},
	}
	if !full.Directed() {
		t.Fatal("протяжённость с направлением у всех сочтена ненаправленной")
	}
	partial := LinearU{
		{Element: "E1", From: 0, To: 10, Direction: DirForward},
		{Element: "E2", From: 0, To: 20},
	}
	if partial.Directed() {
		t.Fatal("протяжённость с пропущенным направлением сочтена направленной")
	}
	var empty LinearU
	if empty.Directed() {
		t.Fatal("пустая протяжённость сочтена направленной")
	}
}

// Интервалы могут быть несвязными: станция не обязана быть одним непрерывным
// диапазоном. Требование связности здесь повторило бы ошибку первой редакции
// формата, которая безусловно требовала связности карты и конфликтовала с
// тупиком отстоя.
func TestНесвязныеИнтервалыЗаконны(t *testing.T) {
	l := LinearU{
		{Element: "E1", From: 0, To: 10},
		{Element: "E9", From: 500, To: 600},
	}
	if err := l.Structural(); err != nil {
		t.Fatalf("несвязная протяжённость отвергнута: %v", err)
	}
}

// Смысл параметризации: u и s — разные величины, и смешать их нельзя. Тест
// фиксирует, что обе координаты живут в одной форме, оставаясь разными типами;
// присвоение одного другому не компилируется и потому здесь не записано.
func TestОбеКоординатыЖивутВОднойФорме(t *testing.T) {
	u := IntervalU{Element: "E1", From: 0, To: 14.731}
	s := IntervalS{Element: "E1", From: 0, To: 14731000 * units.Micrometer}

	if err := u.Structural(); err != nil {
		t.Fatalf("интервал в u отвергнут: %v", err)
	}
	if err := s.Structural(); err != nil {
		t.Fatalf("интервал в s отвергнут: %v", err)
	}
	if u.Element != s.Element {
		t.Fatal("форма разошлась между координатами")
	}
}
