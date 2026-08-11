package track

import (
	"reflect"
	"strings"
	"testing"

	"github.com/shady2k/ClearAhead/server/internal/units"
)

// Провод не должен содержать НИ ОДНОГО значения в координате s.
//
// Почему это сторож, а не очевидность. Позиций четыре, и две из них —
// авторская u (метры вдоль горизонтальной проекции) и скомпилированная s
// (целые микрометры пространственной длины оси). Клиент не имеет цепочки
// вертикального профиля и восстановить размещение по s не способен, поэтому на
// провод уходит только u.
//
// Риск завёлся вместе с общим типом протяжённости: netloc.LinearU и
// netloc.LinearS — одна форма, различающаяся параметром. Объявить проводное
// поле как LinearS вместо LinearU — правка в один символ, компилятор её
// пропустит (тип объявления сам себе непротиворечив), а JSON молча уедет
// целыми микрометрами там, где клиент ждёт метры. Отказа не будет — будет
// геометрия, промасштабированная в миллион раз.
//
// Присвоить LinearS в поле типа LinearU нельзя — это ловит система типов.
// Здесь ловится другое: неверно ОБЪЯВЛЕННОЕ поле.
func TestПроводНеСодержитКоординатуS(t *testing.T) {
	sType := reflect.TypeOf(units.Distance(0))

	var seen = map[reflect.Type]bool{}
	var walk func(typ reflect.Type, path string)
	walk = func(typ reflect.Type, path string) {
		if seen[typ] {
			return
		}
		seen[typ] = true

		if typ == sType {
			t.Errorf("координата s дошла до провода: %s имеет тип units.Distance", path)
			return
		}

		switch typ.Kind() {
		case reflect.Struct:
			for i := range typ.NumField() {
				f := typ.Field(i)
				name := f.Name
				if tag := f.Tag.Get("json"); tag != "" && tag != "-" {
					name = strings.Split(tag, ",")[0]
				}
				walk(f.Type, path+"."+name)
			}
		case reflect.Slice, reflect.Array, reflect.Pointer:
			walk(typ.Elem(), path+"[]")
		case reflect.Map:
			walk(typ.Key(), path+"{key}")
			walk(typ.Elem(), path+"{}")
		}
	}

	walk(reflect.TypeOf(RenderGeometry{}), "RenderGeometry")
}

// Обратное: скомпилированный артефакт — вход физики и безопасности — не должен
// протаскивать float туда, где живёт занятость.
//
// Правило проекта: в состоянии симуляции float запрещён, всё, что участвует в
// занятости, физике и захвате ресурсов, считается в целых микрометрах. Проверка
// узкая и намеренно такая: она стережёт длины и протяжённости, а не всё подряд.
// Профиль (уклон, кривизна) остаётся безразмерным float по построению, и
// требовать от него целых значило бы сломать вычисление.
func TestДлиныСкомпилированногоПутиЦелые(t *testing.T) {
	el := reflect.TypeOf(CompiledElement{})
	for _, name := range []string{"LengthU", "LengthS"} {
		f, ok := el.FieldByName(name)
		if !ok {
			t.Fatalf("CompiledElement.%s исчез — правило про целые микрометры осталось без предмета", name)
		}
		if f.Type != reflect.TypeOf(units.Distance(0)) {
			t.Errorf("CompiledElement.%s имеет тип %s, ожидались целые микрометры", name, f.Type)
		}
	}

	f, ok := reflect.TypeOf(CompiledTrack{}).FieldByName("Trackside")
	if !ok {
		t.Fatal("CompiledTrack.Trackside исчез")
	}
	// map[string]netloc.LinearS -> []netloc.Interval[units.Distance] -> поле From
	from, ok := f.Type.Elem().Elem().FieldByName("From")
	if !ok {
		t.Fatal("у интервала скомпилированной протяжённости нет поля From")
	}
	if from.Type != reflect.TypeOf(units.Distance(0)) {
		t.Errorf("протяжённость скомпилированного пути в %s, ожидались целые микрометры", from.Type)
	}
}
