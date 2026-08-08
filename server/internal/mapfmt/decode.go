package mapfmt

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"reflect"
	"strings"
)

// Decode разбирает файл карты строго: всё неизвестное и всё неоднозначное —
// отказ. Три прохода по одному буферу, потому что каждый проверяет своё и
// смешивать их в один означало бы получать невнятные ошибки.
func Decode(r io.Reader) (*Map, error) {
	raw, err := io.ReadAll(io.LimitReader(r, MaxDocumentBytes+1))
	if err != nil {
		return nil, fmt.Errorf("mapfmt: чтение: %w", err)
	}
	if len(raw) > MaxDocumentBytes {
		return nil, fmt.Errorf("mapfmt: документ больше %d байт", MaxDocumentBytes)
	}

	if err := checkTokens(raw); err != nil {
		return nil, err
	}

	var m Map
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		if strings.Contains(err.Error(), "json: unknown field") {
			return nil, fmt.Errorf("mapfmt: неизвестное поле в документе: %v", err)
		}
		return nil, fmt.Errorf("mapfmt: разбор: %w", err)
	}
	if dec.More() {
		return nil, fmt.Errorf("mapfmt: после документа есть лишние данные")
	}

	if m.FormatVersion != FormatVersion {
		return nil, fmt.Errorf("mapfmt: версия формата %d не поддерживается, ожидается %d",
			m.FormatVersion, FormatVersion)
	}
	if err := checkFinite(reflect.ValueOf(m), "map"); err != nil {
		return nil, err
	}
	return &m, nil
}

// checkTokens ловит дублирующиеся ключи и превышение глубины. encoding/json
// принимает дубликаты молча и оставляет последний; разные библиотеки выбирают
// по-разному, и карта начала бы зависеть от версии парсера.
func checkTokens(raw []byte) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()

	type frame struct {
		isObject bool
		keys     map[string]bool
		pending  string
	}
	var stack []frame

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("mapfmt: разбор: %w", err)
		}
		switch t := tok.(type) {
		case json.Delim:
			switch t {
			case '{':
				stack = append(stack, frame{isObject: true, keys: map[string]bool{}})
			case '[':
				stack = append(stack, frame{})
			case '}', ']':
				if len(stack) == 0 {
					return fmt.Errorf("mapfmt: документ должен быть JSON-объектом")
				}
				stack = stack[:len(stack)-1]
				// Закрытие контейнера — это завершение значения: pending
				// родителя должен сброситься, иначе следующий за вложенным
				// объектом ключ будет принят за значение и дубликаты за ним
				// не проверятся вовсе.
			}
			if len(stack) > MaxNestingDepth {
				return fmt.Errorf("mapfmt: глубина вложенности больше %d", MaxNestingDepth)
			}
		case string:
			if len(stack) == 0 {
				// Корневой скаляр — не карта. Отказ вместо паники на пустом
				// стеке: Decode должен безопасно обработать любой вход.
				return fmt.Errorf("mapfmt: документ должен быть JSON-объектом")
			}
			// Ключ объекта отличается от строкового значения тем, что стоит на
			// нечётной позиции внутри объекта; json.Decoder отдаёт их вперемешку,
			// поэтому состояние держим сами.
			top := &stack[len(stack)-1]
			if top.isObject && top.pending == "" {
				if top.keys[t] {
					return fmt.Errorf("mapfmt: дублирующийся ключ %q", t)
				}
				top.keys[t] = true
				top.pending = t
				continue
			}
		default:
			// Число, bool, null на верхнем уровне — тоже не карта.
			if len(stack) == 0 {
				return fmt.Errorf("mapfmt: документ должен быть JSON-объектом")
			}
		}
		if len(stack) > 0 {
			stack[len(stack)-1].pending = ""
		}
	}
}

// checkFinite обходит разобранную модель и требует, чтобы каждое float-поле
// было конечным. Стандартный разборщик Go возвращает ошибку на 1e400, но
// спецификация не должна зависеть от поведения библиотеки.
func checkFinite(v reflect.Value, path string) error {
	switch v.Kind() {
	case reflect.Float64, reflect.Float32:
		if f := v.Float(); math.IsNaN(f) || math.IsInf(f, 0) {
			return fmt.Errorf("mapfmt: %s: не конечное число %v", path, f)
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			name := v.Type().Field(i).Name
			if err := checkFinite(v.Field(i), path+"."+name); err != nil {
				return err
			}
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			if err := checkFinite(v.Index(i), fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	case reflect.Map:
		for _, k := range v.MapKeys() {
			if err := checkFinite(v.MapIndex(k), fmt.Sprintf("%s[%v]", path, k)); err != nil {
				return err
			}
		}
	case reflect.Pointer:
		if !v.IsNil() {
			return checkFinite(v.Elem(), path)
		}
	}
	return nil
}
