package track

import (
	"strings"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/seedmap"
)

// Обобщение обязано быть настоящим, а не переименованием полей: скомпилированное
// устройство не должно быть привязано к трём портам и двум ветвям.
//
// Глухое пересечение — проверочный случай, который трёхпортовая модель выразить
// не могла в принципе: четыре порта и ДВА НЕПЕРЕСЕКАЮЩИХСЯ прохода, ни один из
// которых не начинается там же, где другой. Здесь оно строится напрямую в
// скомпилированной форме — файл карты такое устройство пока не записывает, и
// это разные вопросы: обобщена форма, которую видит код.
func TestСкомпилированноеУстройствоНеТрёхпортовое(t *testing.T) {
	diamond := CompiledDevice{
		ID:    "ST_A_DK_1",
		Ports: []string{"ST_A_DK_1.A", "ST_A_DK_1.B", "ST_A_DK_1.C", "ST_A_DK_1.D"},
		Traversals: []Traversal{
			{Passage: "ST_A_DK_1:ab", From: "ST_A_DK_1.A", To: "ST_A_DK_1.B"},
			{Passage: "ST_A_DK_1:cd", From: "ST_A_DK_1.C", To: "ST_A_DK_1.D"},
		},
		Resource: "RES_ST_A_DK_1",
	}

	if len(diamond.Ports) != 4 {
		t.Fatalf("портов %d", len(diamond.Ports))
	}
	// Ни один порт не общий для обоих переходов — этим глухое пересечение и
	// отличается от стрелки.
	a, b := diamond.Traversals[0], diamond.Traversals[1]
	for _, x := range []string{a.From, a.To} {
		for _, y := range []string{b.From, b.To} {
			if x == y {
				t.Fatalf("переходы делят порт %s — это уже не глухое пересечение", x)
			}
		}
	}

	// Все проходы делят один ресурс независимо от числа портов: занять
	// пересечение можно только целиком.
	if diamond.Resource == "" {
		t.Fatal("у устройства нет общего ресурса")
	}

	// Хеш обязан различать устройства по портам и переходам поимённо: позиционная
	// запись «общий, прямой, боковой» здесь неприменима.
	var sb strings.Builder
	writeDevice(&sb, diamond)
	body := sb.String()
	for _, want := range []string{"ST_A_DK_1.D", "ST_A_DK_1:cd"} {
		if !strings.Contains(body, want) {
			t.Fatalf("в теле хеша нет %q:\n%s", want, body)
		}
	}
}

// Стрелка из карты компилируется в устройство с тремя портами и двумя
// переходами, оба из общего порта.
func TestСтрелкаКомпилируетсяВУстройство(t *testing.T) {
	ct, _, err := Compile(seedmap.Station())
	if err != nil {
		t.Fatalf("компиляция: %v", err)
	}
	if len(ct.Devices) == 0 {
		t.Fatal("устройств нет")
	}
	for id, d := range ct.Devices {
		if len(d.Ports) != 3 {
			t.Fatalf("%s: портов %d, у обыкновенной стрелки три", id, len(d.Ports))
		}
		if len(d.Traversals) != 2 {
			t.Fatalf("%s: переходов %d, у обыкновенной стрелки два", id, len(d.Traversals))
		}
		if d.Traversals[0].From != d.Traversals[1].From {
			t.Fatalf("%s: переходы начинаются в разных портах", id)
		}
		if d.Resource == "" {
			t.Fatalf("%s: нет общего ресурса", id)
		}
	}
}
