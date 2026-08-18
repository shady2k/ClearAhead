// Package content — набор контента сервера: паспорта подвижного состава и
// ассеты, на которые они ссылаются.
//
// # Почему это не карта
//
// Решение владельца 2026-08-13: «ВЛ80 — общий контент, к карте отношения не
// имеет». Довод сильный и стоит записи: паспорт машины — факт о ТЕХНИКЕ, а не о
// станции Ст-А. Положи его в карту — и вторая карта с тем же локомотивом
// повторит его габарит и тяговую характеристику у себя, а исправление придётся
// вносить в оба места. Это ровно тот двойной источник истины, который проект
// ловит везде.
//
// Отсюда и адрес: набор один на сервер, а не на регион. Регион сегодня на него
// не ссылается вовсе — набор просто есть. Цена названа: когда наборов станет
// два, регион обязан будет называть свой, и у ресурса появится имя набора в
// адресе.
//
// # Почему один ресурс, а не два
//
// Паспорта и ассеты лежат в одном файле и отдаются одним ответом, хотя вещи
// разные: числа машины и её вид. Довод — они СВЯЗАНЫ ссылкой, и разъехавшаяся
// пара (паспорт есть, ассета нет) обязана быть невозможной, а не проверяемой.
// Здесь она невозможна: набор либо загрузился целиком, либо не загрузился.
//
// # Что здесь НЕ лежит
//
// Расстановка: какой локомотив где стоит — состояние партии, а не контент
// (пакет match). Набор отвечает «какие машины бывают», а не «какие есть».
package content

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// FormatVersion — версия формата файла набора. Неизвестная — отказ загрузки, как
// у карты: набор новее читателя не разбирается наполовину.
const FormatVersion = 1

// FileName — имя файла набора внутри каталога контента.
const FileName = "content.json"

// MaxDocumentBytes — потолок на сам файл набора (не на ассеты). Набор — это
// перечень, а не хранилище: мегабайт перечня означает опечатку либо чужой файл.
const MaxDocumentBytes = 1 << 20

// Set — загруженный набор: паспорта, записи ассетов и сами байты.
//
// Порядок перечней КАНОНИЧЕСКИЙ (по идентификатору), а не авторский. Тело
// набора хешируется, и порядок, зависящий от обхода map или от того, как автор
// набрал файл, дал бы разный хеш при неизменном содержимом — та же причина, по
// которой канонизирован провод геометрии.
type Set struct {
	Stock  []StockType
	Assets []Asset
	// models — разобранные описания тел ПО ИМЕНИ модели. Имя есть у всякого тела,
	// род механизма — только у устройства, и потому основной ключ здесь имя.
	//
	// До 2026-08-18 ключом был род привода, и это делало формат стрелочным: дом
	// с пустым родом столкнулся бы с елью на пустом ключе. Разбор — model.go.
	models map[string]*Model
	// drives — те же тела, но по роду механизма: карта называет «ручной» или
	// «электрический», а не имя ассета. Второй индекс, а не вторая коллекция:
	// значения те же указатели.
	drives map[string]*Model
	// blobs — байты по адресу, то есть по хешу СОДЕРЖИМОГО того, что отдаётся.
	blobs map[string][]byte
	// Hash — хеш перечня (не байтов ассетов). Меняется при правке любого числа
	// паспорта, якоря или атрибуции; при неизменном перечне не меняется.
	Hash string
}

// Load читает набор из каталога.
//
// Всё, что можно проверить при укладке, проверяется здесь: испорченный набор
// обязан НЕ СОБРАТЬСЯ, а не доехать до клиента. Проверка на отдаче была бы
// проверкой не того — байты к тому моменту уже объявлены.
func Load(dir string) (*Set, error) {
	path := filepath.Join(dir, FileName)
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("content: файл набора: %w", err)
	}
	if len(raw) > MaxDocumentBytes {
		return nil, fmt.Errorf("content: файл набора больше %d байт", MaxDocumentBytes)
	}

	var doc document
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("content: разбор %s: %w", path, err)
	}
	if dec.More() {
		return nil, fmt.Errorf("content: после документа %s есть лишние данные", path)
	}
	if doc.FormatVersion != FormatVersion {
		return nil, fmt.Errorf("content: версия формата %d не поддерживается, ожидается %d",
			doc.FormatVersion, FormatVersion)
	}

	set := &Set{blobs: map[string][]byte{}}
	if err := set.loadAssets(dir, doc.Assets); err != nil {
		return nil, err
	}
	if err := set.loadStock(doc.Stock); err != nil {
		return nil, err
	}
	if err := set.loadModels(); err != nil {
		return nil, err
	}
	set.Hash = set.digest()
	return set, nil
}

// document — файл набора как он записан.
type document struct {
	FormatVersion int         `json:"format_version"`
	Stock         []StockType `json:"stock"`
	Assets        []Asset     `json:"assets"`
}

// Blob возвращает байты по адресу вида "sha256-<hex>".
//
// Пустоты в пространстве хешей не существует: «нет содержимого с таким хешем»
// означает выдуманный адрес, а не законную пустоту места. Поэтому здесь нет
// третьего исхода — только «есть» и «нет».
func (s *Set) Blob(addr string) ([]byte, bool) {
	b, ok := s.blobs[addr]
	return b, ok
}

// StockType возвращает паспорт по идентификатору.
func (s *Set) StockType(id string) (StockType, bool) {
	for _, t := range s.Stock {
		if t.ID == id {
			return t, true
		}
	}
	return StockType{}, false
}

// digest — хеш перечня. Считается по каноническому JSON тех же полей, что едут
// на провод: хеш обязан меняться ровно тогда, когда меняется то, что видит
// клиент.
func (s *Set) digest() string {
	body, err := json.Marshal(struct {
		Stock  []StockType `json:"stock"`
		Assets []Asset     `json:"assets"`
	}{s.Stock, s.Assets})
	if err != nil {
		// Marshal перечня из строк и чисел не падает; паника здесь означала бы
		// изменение типов, а не входные данные.
		panic(fmt.Sprintf("content: сериализация набора: %v", err))
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

// sortedByID — общий канонический порядок для обоих перечней.
func sortedByID[T any](items []T, id func(T) string) {
	sort.SliceStable(items, func(i, j int) bool { return id(items[i]) < id(items[j]) })
}
