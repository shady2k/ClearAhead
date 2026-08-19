package uuidv7

import (
	"encoding/hex"
	"strings"
	"testing"
	"time"
)

// decode превращает каноническую строку обратно в 16 байт: тест разбирает
// биты, и разбор обязан идти через ту же форму, что выдаёт генератор.
func decode(t *testing.T, s string) [16]byte {
	t.Helper()
	if len(s) != 36 {
		t.Fatalf("длина %d, ожидалась 36", len(s))
	}
	raw, err := hex.DecodeString(strings.ReplaceAll(s, "-", ""))
	if err != nil {
		t.Fatalf("не шестнадцатеричная строка %q: %v", s, err)
	}
	var out [16]byte
	copy(out[:], raw)
	return out
}

// TestLayout закрепляет разбор битов по RFC 9562 §5.7: старшие 48 бит —
// миллисекундная метка unix, ниббл версии — 7, вариант — 10 в битах 64..65.
func TestLayout(t *testing.T) {
	ms := int64(1_700_000_000_123)
	id, err := from(func(b []byte) error {
		for i := range b {
			b[i] = 0xAB
		}
		return nil
	}, time.UnixMilli(ms))
	if err != nil {
		t.Fatalf("from: %v", err)
	}
	raw := decode(t, id)

	gotMS := int64(raw[0])<<40 | int64(raw[1])<<32 | int64(raw[2])<<24 |
		int64(raw[3])<<16 | int64(raw[4])<<8 | int64(raw[5])
	if gotMS != ms {
		t.Errorf("метка времени %d, ожидалась %d", gotMS, ms)
	}
	if v := raw[6] >> 4; v != 7 {
		t.Errorf("версия %d, ожидалась 7", v)
	}
	if v := raw[8] >> 6; v != 0b10 {
		t.Errorf("вариант %d, ожидался 2 (RFC 4122/9562)", v)
	}
	// Случайные биты обязаны дойти до нижних 62 бит: источник выше заполнил
	// все байты 0xAB, и от них должны остаться только биты версии и варианта.
	if raw[7] != 0xAB {
		t.Errorf("байт 7 %#x, ожидался 0xAB", raw[7])
	}
	if raw[9] != 0xAB {
		t.Errorf("байт 9 %#x, ожидался 0xAB", raw[9])
	}
	if raw[15] != 0xAB {
		t.Errorf("байт 15 %#x, ожидался 0xAB", raw[15])
	}
}

// TestTimestampsGrow проверяет временной порядок системного источника: метка
// времени в подряд выданных идентификаторах не убывает. Равные метки (два
// вызова в одну миллисекунду) законны — порядок в этом случае несёт
// случайная часть.
func TestTimestampsGrow(t *testing.T) {
	prev := int64(-1)
	for range 1000 {
		id, err := New()
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		raw := decode(t, id)
		ms := int64(raw[0])<<40 | int64(raw[1])<<32 | int64(raw[2])<<24 |
			int64(raw[3])<<16 | int64(raw[4])<<8 | int64(raw[5])
		if ms < prev {
			t.Fatalf("метка %d упала относительно %d", ms, prev)
		}
		prev = ms
	}
}

// TestDeterministic воспроизводит результат: внедряемый источник даёт одну и
// ту же последовательность на двух прогонах, внутри прогона идентификаторы
// не совпадают и упорядочены по времени. Без внутреннего состояния источника
// (постоянная метка и постоянные случайные байты) этот тест падает: все три
// идентификатора прогона совпали бы.
func TestDeterministic(t *testing.T) {
	run := func() []string {
		src := Deterministic()
		out := make([]string, 3)
		for i := range out {
			id, err := src()
			if err != nil {
				t.Fatalf("source: %v", err)
			}
			out[i] = id
		}
		return out
	}
	a, b := run(), run()
	prev := int64(-1)
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("прогон 2 разошёлся на %d: %q против %q", i, a[i], b[i])
		}
		if len(a[i]) != 36 {
			t.Fatalf("длина %d, ожидалась 36", len(a[i]))
		}
		for j := range i {
			if a[i] == a[j] {
				t.Fatalf("повтор %q внутри прогона", a[i])
			}
		}
		raw := decode(t, a[i])
		ms := int64(raw[0])<<40 | int64(raw[1])<<32 | int64(raw[2])<<24 |
			int64(raw[3])<<16 | int64(raw[4])<<8 | int64(raw[5])
		if ms <= prev {
			t.Fatalf("метка %d не выросла относительно %d", ms, prev)
		}
		prev = ms
	}
}

// TestUnique — подряд выданные идентификаторы не совпадают: 16 байт, из
// которых 62 случайные, делают коллизию на тысячи прогонов невозможной.
func TestUnique(t *testing.T) {
	seen := map[string]bool{}
	for range 1000 {
		id, err := New()
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		if seen[id] {
			t.Fatalf("повтор %q", id)
		}
		seen[id] = true
	}
}

// ВЫВЕДЕННЫЙ ИДЕНТИФИКАТОР ПОВТОРЯЕТСЯ, А ВЕРСИЯ У НЕГО ВОСЬМАЯ.
//
// Повторяется — потому что на этом стоит воспроизводимость мира: сцеп,
// родившийся от удара, обязан называться одинаково в двух прогонах одной
// расстановки, иначе канонический хеш состояния разошёлся бы между загрузками.
//
// Восьмая — потому что седьмая ОБЕЩАЕТ метку времени в старших битах, а там
// хеш. Версия — это не украшение записи, а слово, сказанное всякому разбору.
func TestDerivedRepeatsAndSaysVersionEight(t *testing.T) {
	got := Derived("consist.auto", "A", "B")
	if again := Derived("consist.auto", "A", "B"); again != got {
		t.Fatalf("два вызова дали разное: %s и %s", got, again)
	}
	if other := Derived("consist.auto", "A", "C"); other == got {
		t.Fatalf("разные части дали одно имя: %s", other)
	}
	// ДЛИНА ЧАСТЕЙ ЗНАЧИМА, а не только их склейка: без префикса длины пары
	// («AB», «») и («A», «B») дали бы один идентификатор.
	if glued := Derived("consist.auto", "AB", ""); glued == Derived("consist.auto", "A", "B") {
		t.Fatal("части склеились без учёта длины: («AB», «») совпало с («A», «B»)")
	}
	raw, err := hex.DecodeString(strings.ReplaceAll(got, "-", ""))
	if err != nil || len(raw) != 16 {
		t.Fatalf("форма идентификатора %q: %v", got, err)
	}
	if v := raw[6] >> 4; v != 8 {
		t.Fatalf("версия %d, ожидалась восьмая (RFC 9562 §5.8, custom)", v)
	}
	if raw[8]&0xc0 != 0x80 {
		t.Fatalf("вариант %#x, ожидался RFC 4122/9562", raw[8]&0xc0)
	}
}
