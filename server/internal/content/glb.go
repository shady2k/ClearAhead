package content

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
)

// Подрезка сцены .glb: узлы выбрасываются из ДЕРЕВА СЦЕНЫ, байты геометрии
// остаются на месте.
//
// # Что здесь делается и чего НЕ делается
//
// Делается: из массива scenes[].nodes и из children каждого узла убираются
// ссылки на выброшенные узлы. Клиент строит сцену обходом от корня, поэтому
// недостижимый узел не создаётся вовсе.
//
// НЕ делается: сборка мусора. Меш выброшенного узла, его аксессоры, куски
// буфера и текстуры остаются в файле, и файл не худеет ни на байт. Полная
// переупаковка — обход достижимости nodes→meshes→accessors→bufferViews с
// перенумерацией ВСЕХ ссылок и пересборкой двоичного чанка; по замеру спеки
// каталога (§10.2) она сняла бы с ВЛ80 7.3 МБ из 21.4, то есть треть. Это своя
// работа со своим классом ошибок, и она отложена сознательно: сегодня решается
// «на кадре не должно быть двух кузовов, стоящих ни на чём», а не «первый вход
// игрока на треть короче».
//
// # Почему правится JSON, а не структура
//
// Файл .glb — двенадцать байт заголовка и чанки {длина, тип, данные}. Правится
// ТОЛЬКО чанк JSON, двоичный копируется как есть: так изменение ограничено тем,
// что мы понимаем, и никакая ошибка выравнивания не может испортить геометрию.
//
// Результат ДЕТЕРМИНИРОВАН: одни и те же входные байты и один и тот же перечень
// узлов дают побайтово одинаковый выход. Иначе адрес ассета (хеш содержимого)
// менялся бы при каждом перезапуске сервера, и immutable перестал бы быть
// честным по построению — то есть исчезло бы единственное свойство, ради
// которого адресация по содержимому и выбрана.

const (
	glbMagic     = 0x46546C67 // "glTF"
	glbVersion   = 2
	glbChunkJSON = 0x4E4F534A // "JSON"
	glbHeaderLen = 12
	glbChunkHdr  = 8
)

// dropSceneNodes убирает названные узлы из дерева сцены .glb.
//
// Узел, названный в перечне и не найденный в файле, — ОТКАЗ, а не пропуск:
// молчаливое «такого узла нет» означало бы, что опечатка в имени выглядит как
// выполненная работа, и на кадре снова появились бы лишние кузова.
func dropSceneNodes(raw []byte, names []string) ([]byte, error) {
	if len(raw) < glbHeaderLen+glbChunkHdr {
		return nil, fmt.Errorf("файл короче заголовка glb")
	}
	if binary.LittleEndian.Uint32(raw[0:4]) != glbMagic {
		return nil, fmt.Errorf("не glb: сигнатура не glTF")
	}
	if v := binary.LittleEndian.Uint32(raw[4:8]); v != glbVersion {
		return nil, fmt.Errorf("glb версии %d, поддерживается %d", v, glbVersion)
	}
	jsonLen := int(binary.LittleEndian.Uint32(raw[glbHeaderLen : glbHeaderLen+4]))
	if binary.LittleEndian.Uint32(raw[glbHeaderLen+4:glbHeaderLen+8]) != glbChunkJSON {
		return nil, fmt.Errorf("первый чанк glb не JSON")
	}
	start := glbHeaderLen + glbChunkHdr
	if start+jsonLen > len(raw) {
		return nil, fmt.Errorf("чанк JSON длиной %d не помещается в файл %d байт", jsonLen, len(raw))
	}
	head := raw[start : start+jsonLen]
	tail := raw[start+jsonLen:]

	// Разбираем в map, а не в структуру: у glTF десятки полей, и структура,
	// описывающая лишь часть, ВЫБРОСИЛА БЫ остальные при обратной записи —
	// то есть тихо испортила бы чужой файл. Общая карта переживает всё, чего
	// мы не понимаем.
	var doc map[string]any
	if err := json.Unmarshal(head, &doc); err != nil {
		return nil, fmt.Errorf("разбор чанка JSON: %w", err)
	}
	nodes, _ := doc["nodes"].([]any)
	if len(nodes) == 0 {
		return nil, fmt.Errorf("в файле нет узлов")
	}

	drop := map[int]bool{}
	for _, want := range names {
		found := -1
		for i, n := range nodes {
			obj, _ := n.(map[string]any)
			if obj != nil && obj["name"] == want {
				found = i
				break
			}
		}
		if found < 0 {
			return nil, fmt.Errorf("узел %q в сцене не найден: перечень узлов разошёлся с файлом", want)
		}
		drop[found] = true
	}

	// Ссылки убираются в двух местах, и оба обязательны: узел бывает и корнем
	// сцены, и ребёнком другого узла.
	removed := 0
	if scenes, ok := doc["scenes"].([]any); ok {
		for _, sc := range scenes {
			obj, _ := sc.(map[string]any)
			if obj == nil {
				continue
			}
			if kept, n := filterRefs(obj["nodes"], drop); n > 0 {
				obj["nodes"] = kept
				removed += n
			}
		}
	}
	for _, n := range nodes {
		obj, _ := n.(map[string]any)
		if obj == nil {
			continue
		}
		if kept, n := filterRefs(obj["children"], drop); n > 0 {
			obj["children"] = kept
			removed += n
		}
	}
	if removed != len(drop) {
		return nil, fmt.Errorf("узлов названо %d, ссылок убрано %d: узел достижим не оттуда, откуда ожидалось",
			len(drop), removed)
	}

	out, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("сборка чанка JSON: %w", err)
	}
	// Чанк выравнивается пробелами до кратности четырём — того требует формат,
	// и именно пробелами (0x20) для JSON, а не нулями.
	for len(out)%4 != 0 {
		out = append(out, ' ')
	}

	res := make([]byte, 0, glbHeaderLen+glbChunkHdr+len(out)+len(tail))
	res = append(res, raw[:glbHeaderLen]...)
	res = binary.LittleEndian.AppendUint32(res, uint32(len(out)))
	res = binary.LittleEndian.AppendUint32(res, glbChunkJSON)
	res = append(res, out...)
	res = append(res, tail...)
	// Общая длина файла записана в заголовке и обязана совпадать с настоящей:
	// расхождение — то самое «отказа нет, всё выглядит исправным».
	binary.LittleEndian.PutUint32(res[8:12], uint32(len(res)))
	return res, nil
}

// filterRefs убирает из массива ссылок на узлы те, что попали в drop, и
// возвращает сколько убрано.
func filterRefs(v any, drop map[int]bool) ([]any, int) {
	list, _ := v.([]any)
	if list == nil {
		return nil, 0
	}
	kept := make([]any, 0, len(list))
	removed := 0
	for _, item := range list {
		f, ok := item.(float64)
		if ok && drop[int(f)] {
			removed++
			continue
		}
		kept = append(kept, item)
	}
	return kept, removed
}
