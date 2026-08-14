package contract

import (
	"encoding/json"
	"strings"
	"testing"
)

// Валидатор договора проверяется САМ, и это не педантизм: сверка, которая
// молча пропускает нарушение, хуже отсутствия сверки — она даёт уверенность
// вместо проверки. Каждый вид нарушения ниже — тот, ради которого договор и
// заведён.

const doc = `{
  "contract": "проба",
  "protocol_version": 1,
  "types": {
    "outer": {"name": "string", "count": "uint", "time": "int_string", "tag": "string?", "items": "[]inner"},
    "inner": {"id": "string", "u": "float"}
  }
}`

func load(t *testing.T) Doc {
	t.Helper()
	var d Doc
	if err := json.Unmarshal([]byte(doc), &d); err != nil {
		t.Fatalf("проба договора не разбирается: %v", err)
	}
	return d
}

func value(t *testing.T, raw string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		t.Fatalf("проба значения не разбирается: %v", err)
	}
	return v
}

func TestValidateAcceptsDeclaredValue(t *testing.T) {
	d := load(t)
	v := value(t, `{"name":"a","count":3,"time":"1200000","items":[{"id":"x","u":1.5}]}`)
	if err := d.Validate("outer", v); err != nil {
		t.Fatalf("объявленное значение отвергнуто: %v", err)
	}
}

// Необязательное поле вправе отсутствовать и обязано проверяться, когда пришло.
func TestOptionalFieldMayBeAbsentButNotWrong(t *testing.T) {
	d := load(t)
	ok := value(t, `{"name":"a","count":0,"time":"0","items":[],"tag":"метка"}`)
	if err := d.Validate("outer", ok); err != nil {
		t.Fatalf("необязательное поле верного вида отвергнуто: %v", err)
	}
	bad := value(t, `{"name":"a","count":0,"time":"0","items":[],"tag":7}`)
	if err := d.Validate("outer", bad); err == nil {
		t.Fatal("необязательное поле неверного вида пропущено")
	}
}

// ГЛАВНАЯ ПРОВЕРКА: поле, которого нет в договоре, обязано ронять сверку.
// Именно ради неё договор существует — сервер, ушедший вперёд, ловится здесь, а
// не тем, что клиент однажды не нашёл поля.
func TestUndeclaredFieldIsRefused(t *testing.T) {
	d := load(t)
	v := value(t, `{"name":"a","count":1,"time":"5","items":[],"speed":12.5}`)
	err := d.Validate("outer", v)
	if err == nil {
		t.Fatal("необъявленное поле speed пропущено")
	}
	if !strings.Contains(err.Error(), "speed") {
		t.Fatalf("отказ не называет виновное поле: %v", err)
	}
}

func TestMissingRequiredFieldIsRefused(t *testing.T) {
	d := load(t)
	v := value(t, `{"name":"a","count":1,"items":[]}`)
	err := d.Validate("outer", v)
	if err == nil {
		t.Fatal("пропущенное обязательное поле time не замечено")
	}
	if !strings.Contains(err.Error(), "time") {
		t.Fatalf("отказ не называет пропущенное поле: %v", err)
	}
}

// Правило провода «int64 строкой» проверяется в обе стороны: число вместо
// строки — отказ, потому что именно так ошибётся тот, кто напишет вторую
// реализацию по памяти.
func TestIntStringRefusesNumber(t *testing.T) {
	d := load(t)
	v := value(t, `{"name":"a","count":1,"time":1200000,"items":[]}`)
	err := d.Validate("outer", v)
	if err == nil {
		t.Fatal("целое числом вместо строки пропущено")
	}
	if !strings.Contains(err.Error(), "time") {
		t.Fatalf("отказ не называет поле: %v", err)
	}
}

func TestUintRefusesNegativeAndFractional(t *testing.T) {
	d := load(t)
	for _, raw := range []string{
		`{"name":"a","count":-1,"time":"0","items":[]}`,
		`{"name":"a","count":1.5,"time":"0","items":[]}`,
	} {
		if err := d.Validate("outer", value(t, raw)); err == nil {
			t.Fatalf("недопустимое uint пропущено: %s", raw)
		}
	}
}

// Вложенность проверяется до дна: нарушение внутри элемента массива обязано
// называть индекс, иначе искать его придётся глазами по всему телу.
func TestNestedViolationNamesItsPlace(t *testing.T) {
	d := load(t)
	v := value(t, `{"name":"a","count":1,"time":"0","items":[{"id":"x","u":1},{"id":"y"}]}`)
	err := d.Validate("outer", v)
	if err == nil {
		t.Fatal("нарушение внутри массива пропущено")
	}
	if !strings.Contains(err.Error(), "items[1]") {
		t.Fatalf("отказ не называет место: %v", err)
	}
}

func TestUnknownKindIsRefused(t *testing.T) {
	d := load(t)
	if err := d.Validate("нетакого", value(t, `{}`)); err == nil {
		t.Fatal("сверка с необъявленным видом прошла")
	}
}
