// Package contract — объявленный договор провода и сверка с ним.
//
// # Зачем договор отдельным файлом, а не только wire-типом в тесте
//
// Wire-тип в серверном тесте (httpapi/contract_test.go) проверяет ОДНУ
// сторону: что сервер отдаёт то, что объявил сам сервер. Этого достаточно,
// пока вторая сторона читает провод глазами автора. Как только у провода
// появляется вторая реализация — клиент на GDScript, — согласие нужно между
// НИМИ, а не между сервером и его собственным тестом.
//
// Поэтому договор вынесен в файл (contract/*.json), который читают обе
// стороны, и каждая сверяет с ним свой провод. Расхождение ловится у того, кто
// разошёлся, и ловится на его языке.
//
// # Почему договор о ФОРМЕ, а не о числах
//
// Потому что эталон о числах в этом проекте уже был и был снесён (коммит
// 4b4d6a7): сверка вычисленных float64 байт в байт не пережила смены машины —
// расхождение ~1e-13 при неизменном коде. Имя поля и его вид от порядка
// вычислений не зависят и потому переживают всё, что переживает сам провод.
//
// # Почему свой словарь видов, а не JSON Schema
//
// Стандарт пришлось бы реализовать дважды: здесь и в клиенте на GDScript, где
// готового валидатора нет, а зависимость в клиенте стоит дороже, чем в
// сервере. Словарь ниже — восемь видов, покрывающих то, что действительно едет
// по проводу, и он умещается в полсотни строк на каждой стороне. Цена названа:
// договор не выражает ни диапазонов, ни перечислений, ни зависимостей между
// полями. Когда понадобится — доказательством станет случай, который словарь не
// выразил, а не рассуждение о полноте.
package contract

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
)

// Doc — прочитанный договор.
type Doc struct {
	Name            string                       `json:"contract"`
	ProtocolVersion int                          `json:"protocol_version"`
	Path            string                       `json:"path"`
	Types           map[string]map[string]string `json:"types"`
	Methods         map[string]MethodDecl        `json:"methods"`
	Notifications   map[string]NotificationDecl  `json:"notifications"`
	Errors          map[string]int               `json:"errors"`
	RefusalData     string                       `json:"refusal_data"`
	RefusalReasons  []string                     `json:"refusal_reasons"`
}

// MethodDecl — объявление метода: вид params и вид result.
type MethodDecl struct {
	Params string `json:"params"`
	Result string `json:"result"`
}

// NotificationDecl — объявление уведомления сверху вниз.
type NotificationDecl struct {
	Params string `json:"params"`
}

// Load читает договор с диска.
//
// Разбор НЕ строгий к верхнеуровневым полям нарочно: в файле рядом с
// объявлениями лежат разделы для человека (why, kinds, forbidden_up,
// idempotency), и требовать от кода знать их значило бы запретить дописывать в
// договор объяснения — то есть ровно то, ради чего он и заведён.
//
// Строгость лежит там, где она работает: в сверке ЗНАЧЕНИЯ с объявленным
// видом (Validate).
func Load(path string) (Doc, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Doc{}, fmt.Errorf("contract: %w", err)
	}
	var d Doc
	if err := json.Unmarshal(raw, &d); err != nil {
		return Doc{}, fmt.Errorf("contract: разбор %s: %w", path, err)
	}
	if d.Name == "" {
		return Doc{}, fmt.Errorf("contract: %s: договор без имени", path)
	}
	if len(d.Types) == 0 {
		return Doc{}, fmt.Errorf("contract: %s: договор без объявленных видов", path)
	}
	return d, nil
}

// Validate сверяет значение с объявленным видом.
//
// Значение — то, что вышло из json.Unmarshal в any: map[string]any, []any,
// float64, string, bool, nil. Проверяется провод, а не структура Go, поэтому
// сверять надо именно разобранный JSON: структура Go уже потеряла бы лишнее
// поле (оно просто не попало бы в неё) — то самое лишнее поле, ради поимки
// которого сверка и делается.
func (d Doc) Validate(kind string, value any) error {
	return d.check("", kind, value)
}

func (d Doc) check(path, kind string, value any) error {
	optional := strings.HasSuffix(kind, "?")
	kind = strings.TrimSuffix(kind, "?")
	if value == nil {
		if optional {
			return nil
		}
		return fmt.Errorf("%s: пусто (null), ожидался %s", at(path), kind)
	}
	if rest, ok := strings.CutPrefix(kind, "[]"); ok {
		list, ok := value.([]any)
		if !ok {
			return fmt.Errorf("%s: ожидался массив %s, пришло %s", at(path), rest, typeName(value))
		}
		for i, item := range list {
			if err := d.check(fmt.Sprintf("%s[%d]", path, i), rest, item); err != nil {
				return err
			}
		}
		return nil
	}
	switch kind {
	case "string":
		if _, ok := value.(string); !ok {
			return fmt.Errorf("%s: ожидалась строка, пришло %s", at(path), typeName(value))
		}
		return nil
	case "bool":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("%s: ожидался bool, пришло %s", at(path), typeName(value))
		}
		return nil
	case "float":
		if _, ok := value.(float64); !ok {
			return fmt.Errorf("%s: ожидалось число, пришло %s", at(path), typeName(value))
		}
		return nil
	case "int", "uint":
		n, ok := value.(float64)
		if !ok {
			return fmt.Errorf("%s: ожидалось целое, пришло %s", at(path), typeName(value))
		}
		if n != float64(int64(n)) {
			return fmt.Errorf("%s: ожидалось целое, пришло %v", at(path), n)
		}
		if kind == "uint" && n < 0 {
			return fmt.Errorf("%s: ожидалось неотрицательное, пришло %v", at(path), n)
		}
		return nil
	case "int_string":
		s, ok := value.(string)
		if !ok {
			return fmt.Errorf("%s: ожидалось целое СТРОКОЙ (правило провода), пришло %s",
				at(path), typeName(value))
		}
		if _, err := strconv.ParseInt(s, 10, 64); err != nil {
			return fmt.Errorf("%s: %q не целое в строке", at(path), s)
		}
		return nil
	}
	fields, ok := d.Types[kind]
	if !ok {
		return fmt.Errorf("%s: вид %q в договоре не объявлен", at(path), kind)
	}
	obj, ok := value.(map[string]any)
	if !ok {
		return fmt.Errorf("%s: ожидался объект %s, пришло %s", at(path), kind, typeName(value))
	}
	for name, decl := range fields {
		v, present := obj[name]
		if !present {
			if strings.HasSuffix(decl, "?") {
				continue
			}
			return fmt.Errorf("%s: нет обязательного поля %q (%s)", at(path), name, decl)
		}
		if err := d.check(join(path, name), decl, v); err != nil {
			return err
		}
	}
	// НЕОБЪЯВЛЕННОЕ ПОЛЕ — ОТКАЗ. Провод, ушедший вперёд договора, снаружи
	// неотличим от провода, договор нарушившего: обе стороны читают договор, и
	// та из них, что о поле не знает, молча его потеряет.
	var extra []string
	for name := range obj {
		if _, declared := fields[name]; !declared {
			extra = append(extra, name)
		}
	}
	if len(extra) > 0 {
		sort.Strings(extra)
		return fmt.Errorf("%s: поля %v не объявлены в виде %s", at(path), extra, kind)
	}
	return nil
}

// at печатает место в значении — корень называется корнем, а не пустотой.
func at(path string) string {
	if path == "" {
		return "корень"
	}
	return path
}

func join(path, name string) string {
	if path == "" {
		return name
	}
	return path + "." + name
}

// typeName называет пришедший вид словами провода, а не словами Go: читателю
// отказа нужно знать, что в JSON лежала строка, а не что в Go это был string.
func typeName(v any) string {
	switch v.(type) {
	case string:
		return "строка"
	case float64:
		return "число"
	case bool:
		return "bool"
	case []any:
		return "массив"
	case map[string]any:
		return "объект"
	case nil:
		return "null"
	default:
		return fmt.Sprintf("%T", v)
	}
}
