package units

import (
	"encoding/json"
	"testing"
)

// TestSimTimeGoesOverWireAsString — правило провода: целочисленные величины
// домена едут строками. Разбор — у SimTime.MarshalJSON.
func TestSimTimeGoesOverWireAsString(t *testing.T) {
	b, err := json.Marshal(struct {
		Time SimTime `json:"time"`
	}{Time: 8 * Hour})
	if err != nil {
		t.Fatalf("запись: %v", err)
	}
	const want = `{"time":"28800000000"}`
	if string(b) != want {
		t.Fatalf("записано %s, ожидалось %s", b, want)
	}
}

func TestSimTimeReadsBackWhatItWrote(t *testing.T) {
	for _, want := range []SimTime{0, Microsecond, 100 * Millisecond, 8 * Hour, -Second} {
		b, err := json.Marshal(want)
		if err != nil {
			t.Fatalf("%s: запись: %v", want, err)
		}
		var got SimTime
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatalf("%s: чтение %s: %v", want, b, err)
		}
		if got != want {
			t.Fatalf("прочитано %s, записано %s", got, want)
		}
	}
}

// TestSimTimeRefusesNumberOnWire — число вместо строки ОТКАЗ, а не молчаливое
// чтение. Клиент, приславший число, потерял бы точность у себя, и молчание
// здесь скрыло бы это до первых больших значений.
func TestSimTimeRefusesNumberOnWire(t *testing.T) {
	var got SimTime
	if err := json.Unmarshal([]byte("28800000000"), &got); err == nil {
		t.Fatalf("число принято как %s, ожидался отказ", got)
	}
}

func TestSimTimePrintsSeconds(t *testing.T) {
	cases := []struct {
		in   SimTime
		want string
	}{
		{0, "0.000s"},
		{100 * Millisecond, "0.100s"},
		{1500 * Millisecond, "1.500s"},
		{-Second, "-1.000s"},
	}
	for _, c := range cases {
		if got := c.in.String(); got != c.want {
			t.Fatalf("%d мкс печатается %q, ожидалось %q", int64(c.in), got, c.want)
		}
	}
}

// TestSimTimeScalesAreConsistent — ряд единиц сходится сам с собой. Проверка
// дешёвая и ловит опечатку в разряде, которая иначе разъехалась бы по всем
// формулам сразу.
func TestSimTimeScalesAreConsistent(t *testing.T) {
	if Millisecond != 1000*Microsecond || Second != 1000*Millisecond || Hour != 3600*Second {
		t.Fatalf("ряд единиц разошёлся: мс %d, с %d, ч %d", Millisecond, Second, Hour)
	}
}
