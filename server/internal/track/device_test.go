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
func TestCompiledDeviceIsNotThreePorted(t *testing.T) {
	// diamondID — глухое пересечение теста. UUID взят из таблицы
	// mapfmt/helpers_test.go (tID06, метка SW): фикстура свой UUID не выдумывает.
	const diamondID = "01a3185c-5007-7242-8242-000006424242"
	diamond := CompiledDevice{
		ID:    diamondID,
		Ports: []string{diamondID + ".A", diamondID + ".B", diamondID + ".C", diamondID + ".D"},
		Traversals: []Traversal{
			{Passage: diamondID + ":ab", From: diamondID + ".A", To: diamondID + ".B"},
			{Passage: diamondID + ":cd", From: diamondID + ".C", To: diamondID + ".D"},
		},
		Resource: "RES_" + diamondID,
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
	for _, want := range []string{diamondID + ".D", diamondID + ":cd"} {
		if !strings.Contains(body, want) {
			t.Fatalf("в теле хеша нет %q:\n%s", want, body)
		}
	}
}

// Стрелка из карты компилируется в устройство с тремя портами и двумя
// переходами, оба из общего порта.
func TestTurnoutCompilesToDevice(t *testing.T) {
	cn, _, err := Compile(seedmap.Station())
	if err != nil {
		t.Fatalf("компиляция: %v", err)
	}
	if len(cn.Devices) == 0 {
		t.Fatal("устройств нет")
	}
	for id, d := range cn.Devices {
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
