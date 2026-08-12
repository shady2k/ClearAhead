package content

import (
	"encoding/binary"
	"encoding/json"
	"strings"
	"testing"
)

// fakeGLB собирает крошечный .glb: три узла, корень сцены ссылается на все три.
//
// Синтетический файл, а не боевой ВЛ80: проверяется РАЗБОР И СБОРКА, и делать
// это на двадцати одном мегабайте значило бы платить секундами за то, что
// проверяется сотней байт. Боевой файл стережёт shipped_test.go.
func fakeGLB(t *testing.T) []byte {
	t.Helper()
	doc := map[string]any{
		"asset":  map[string]any{"version": "2.0"},
		"scene":  0,
		"scenes": []any{map[string]any{"nodes": []any{0}}},
		"nodes": []any{
			map[string]any{"name": "root", "children": []any{1, 2}},
			map[string]any{"name": "keep"},
			map[string]any{"name": "spare"},
		},
		// Поле, которого читатель не знает: оно обязано ПЕРЕЖИТЬ правку. Чужой
		// файл правится в одном месте, а не переписывается по нашей схеме.
		"extras": map[string]any{"whatever": "оставить как есть"},
	}
	js, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("фикстура: %v", err)
	}
	for len(js)%4 != 0 {
		js = append(js, ' ')
	}
	bin := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	out := make([]byte, 0, 12+8+len(js)+8+len(bin))
	out = binary.LittleEndian.AppendUint32(out, glbMagic)
	out = binary.LittleEndian.AppendUint32(out, glbVersion)
	out = binary.LittleEndian.AppendUint32(out, 0) // длина — ниже
	out = binary.LittleEndian.AppendUint32(out, uint32(len(js)))
	out = binary.LittleEndian.AppendUint32(out, glbChunkJSON)
	out = append(out, js...)
	out = binary.LittleEndian.AppendUint32(out, uint32(len(bin)))
	out = binary.LittleEndian.AppendUint32(out, 0x004E4942) // "BIN"
	out = append(out, bin...)
	binary.LittleEndian.PutUint32(out[8:12], uint32(len(out)))
	return out
}

func TestDropSceneNodeRemovesReference(t *testing.T) {
	got, err := dropSceneNodes(fakeGLB(t), []string{"spare"})
	if err != nil {
		t.Fatalf("подрезка: %v", err)
	}
	doc := parseGLBJSON(t, got)
	nodes := doc["nodes"].([]any)
	root := nodes[0].(map[string]any)
	kids := root["children"].([]any)
	if len(kids) != 1 || kids[0].(float64) != 1 {
		t.Fatalf("дети корня %v, ожидался только узел 1", kids)
	}
	// Сам узел из массива НЕ выброшен, и это не забывчивость: выбрасывание
	// потребовало бы перенумеровать все ссылки на узлы во всём файле. Он просто
	// недостижим, и клиент, строящий сцену обходом от корня, его не создаст.
	if len(nodes) != 3 {
		t.Fatalf("узлов %d, ожидалось 3: подрезается ДЕРЕВО, а не массив", len(nodes))
	}
	if doc["extras"] == nil {
		t.Fatal("незнакомое поле extras пропало: чужой файл нельзя переписывать по своей схеме")
	}
}

// TestDropSceneKeepsBinaryChunk — двоичный чанк обязан доехать байт в байт:
// правится только JSON, и это единственное, что мы понимаем.
func TestDropSceneKeepsBinaryChunk(t *testing.T) {
	src := fakeGLB(t)
	got, err := dropSceneNodes(src, []string{"spare"})
	if err != nil {
		t.Fatalf("подрезка: %v", err)
	}
	if want, have := binTail(t, src), binTail(t, got); want != have {
		t.Fatalf("двоичный чанк изменился: было %q, стало %q", want, have)
	}
	if l := binary.LittleEndian.Uint32(got[8:12]); int(l) != len(got) {
		t.Fatalf("длина в заголовке %d, файла %d", l, len(got))
	}
}

// TestDropSceneDeterministic — одни и те же входные байты дают побайтово
// одинаковый выход.
//
// Это не придирка к стилю: адрес ассета есть хеш его содержимого, и плавающий
// выход означал бы новый адрес при каждом перезапуске сервера. Immutable
// перестал бы быть честным по построению — то есть исчезло бы единственное
// свойство, ради которого адресация по содержимому и выбрана.
func TestDropSceneDeterministic(t *testing.T) {
	src := fakeGLB(t)
	a, err := dropSceneNodes(src, []string{"spare"})
	if err != nil {
		t.Fatalf("подрезка: %v", err)
	}
	for i := 0; i < 5; i++ {
		b, err := dropSceneNodes(src, []string{"spare"})
		if err != nil {
			t.Fatalf("подрезка %d: %v", i, err)
		}
		if string(a) != string(b) {
			t.Fatalf("проход %d дал другие байты", i)
		}
	}
}

// TestDropSceneUnknownNodeRefused — названный, но отсутствующий узел это отказ.
// Молчаливый пропуск означал бы, что опечатка в имени выглядит как выполненная
// работа, и на кадре снова стояли бы лишние кузова.
func TestDropSceneUnknownNodeRefused(t *testing.T) {
	_, err := dropSceneNodes(fakeGLB(t), []string{"нет такого"})
	if err == nil || !strings.Contains(err.Error(), "не найден") {
		t.Fatalf("ожидался отказ по отсутствию узла, получено: %v", err)
	}
}

func TestDropSceneRefusesNonGLB(t *testing.T) {
	if _, err := dropSceneNodes([]byte("это не glb вовсе, но длиннее двадцати байт"), []string{"x"}); err == nil {
		t.Fatal("чужой формат принят за glb")
	}
}

func parseGLBJSON(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	clen := int(binary.LittleEndian.Uint32(raw[12:16]))
	var doc map[string]any
	if err := json.Unmarshal(raw[20:20+clen], &doc); err != nil {
		t.Fatalf("разбор результата: %v", err)
	}
	return doc
}

func binTail(t *testing.T, raw []byte) string {
	t.Helper()
	clen := int(binary.LittleEndian.Uint32(raw[12:16]))
	return string(raw[20+clen:])
}
