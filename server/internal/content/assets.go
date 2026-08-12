package content

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
)

// HashAlg — единственный алгоритм адресации байтов. Имя стоит В АДРЕСЕ, а не
// подразумевается: смена алгоритма без имени была бы молчаливой, а два адреса
// одинаковой формы означали бы разное.
const HashAlg = "sha256"

// Разделитель адреса — ДЕФИС. Двоеточие в проекте занято под разделитель
// прохода стрелки (mapfmt.SepPassage) и запрещено внутри идентификаторов;
// заводить ему второй смысл — заготовленная путаница.
const hashSep = "-"

// spdxLicenses — перечень допустимых лицензий.
//
// SPDX, а не свободная строка: «CC-BY», «CC-BY-4.0» и «Creative Commons
// Attribution 4.0» — три написания одного, и по ним нельзя ни сгруппировать, ни
// проверить. Свободный текст здесь означал бы, что поле есть, а знания нет.
// Перечень открывается по мере надобности — новой лицензией, а не отменой
// проверки.
var spdxLicenses = map[string]bool{
	"CC-BY-4.0":    true,
	"CC0-1.0":      true,
	"CC-BY-SA-4.0": true,
}

// Asset — запись каталога: имя, байты, постановка и атрибуция.
//
// # Имя и адрес — разные работы
//
// Имя (Name) — логическое тождество: оно переживает перетекстурирование, на
// него ссылается паспорт. Адрес (Hash) — хеш СОДЕРЖИМОГО того, что отдаётся:
// immutable на нём честен не по договорённости, а по построению, потому что
// изменившиеся байты имеют другой адрес. Один хеш под двумя именами законен и
// есть дедупликация; расхождение атрибуции у одного хеша — отказ.
type Asset struct {
	Name string `json:"name"`
	// File — имя файла в каталоге набора. НЕ едет на провод: клиенту нужен
	// адрес байтов, а не то, как они лежат у сервера.
	File string `json:"file"`
	// MediaType — тип содержимого, он же Content-Type отдачи.
	MediaType string `json:"media_type"`
	// SourceHash — хеш ИСХОДНОГО файла, объявленный автором набора и
	// проверяемый при укладке. Это единственная проверка, которую нельзя
	// переложить на клиента: клиент проверяет, что доехало то, что отдали;
	// сервер — что отдаёт то, что объявил.
	SourceHash string `json:"source_hash"`
	// DropNodes — узлы сцены glTF, которые выбрасываются при укладке.
	//
	// Спека каталога (§10.2) отвергала include_nodes в записи и была права —
	// но отвергала для КЛИЕНТА: «каталог начинает знать внутренности чужого
	// файла, а клиент становится исполнителем инструкции по разбору, и два
	// клиента разберут её по-разному». Здесь инструкцию исполняет СЕРВЕР, один
	// раз, при укладке; клиенты получают одинаковые байты и ни о какой
	// инструкции не знают. Это путь (б) той же спеки — переупаковка при
	// укладке, — только в самой дешёвой его части.
	//
	// Почему это вообще нужно: у ВЛ80 в сцене ЧЕТЫРЕ кузова, два из которых
	// отставлены на 8.15 м вбок и не стоят ни на чём (замер узлов 39 и 49).
	// Клиент выбросить их не вправе — это был бы разбор чужого файла по именам.
	DropNodes []string `json:"drop_nodes,omitempty"`
	// Anchor — что означает начало координат ассета ПОСЛЕ постановки.
	Anchor string `json:"anchor"`
	// Scale — множитель, приводящий меш к размерам мира.
	//
	// Заводится потому, что чужие байты правке не подлежат: правка меняет хеш,
	// то есть создаёт другой ассет, и вдобавок требует объявления изменений по
	// CC-BY. Каталог правит ПОСТАНОВКУ, а не содержимое.
	Scale float64 `json:"scale"`
	// Translation — сдвиг в осях ассета (glTF: Y вверх), метры, ПОСЛЕ масштаба.
	// Порядок назван, потому что обратный порядок дал бы другой результат.
	Translation [3]float64  `json:"translation"`
	Attribution Attribution `json:"attribution"`

	// Hash — адрес отданных байтов: "sha256-<hex>". Считается при укладке от
	// того, что реально уедет клиенту, а не от исходного файла: если сцена
	// подрезана, это разные байты и разный адрес.
	Hash string `json:"-"`
	// Size — длина отданных байтов.
	Size int `json:"-"`
}

// Attribution — атрибуция. Поля ровно те четыре, которых требует CC-BY, плюс
// отметка об изменениях: «на всякий случай» здесь нет ничего.
//
// Обязательна вся: карта, где автор забыл поле, обязана получить отказ, а не
// правдоподобную подстановку. Отвергнут файл рядом с байтами (отцепляется при
// первом копировании) и отдельный ресурс атрибуции (второй запрос, который
// легко не сделать).
type Attribution struct {
	Title   string `json:"title"`
	Author  string `json:"author"`
	Source  string `json:"source"`
	License string `json:"license"`
	// Modified — вносились ли изменения. Раздача ассета есть его
	// распространение, поэтому отвечает на этот вопрос СЕРВЕР, а не тот, кто
	// однажды скачал файл.
	Modified bool `json:"modified"`
	// Modifications — какие именно. Обязательно при Modified: CC-BY требует
	// НАЗЫВАТЬ изменения, а не отмечать их факт галочкой.
	Modifications string `json:"modifications,omitempty"`
}

// Addr собирает адрес байтов из шестнадцатеричного хеша.
func Addr(hexSum string) string { return HashAlg + hashSep + hexSum }

// asset ищет запись по имени.
func (s *Set) asset(name string) (Asset, bool) {
	for _, a := range s.Assets {
		if a.Name == name {
			return a, true
		}
	}
	return Asset{}, false
}

// loadAssets читает файлы, проверяет объявленное и укладывает байты.
func (s *Set) loadAssets(dir string, assets []Asset) error {
	seen := map[string]bool{}
	byHash := map[string]Attribution{}
	out := make([]Asset, 0, len(assets))
	for i, a := range assets {
		if err := mapfmt.ValidID("ассет", a.Name); err != nil {
			return fmt.Errorf("content: ассет %d: %w", i, err)
		}
		if seen[a.Name] {
			return fmt.Errorf("content: ассет %s объявлен дважды", a.Name)
		}
		seen[a.Name] = true
		if err := a.checkDeclared(); err != nil {
			return err
		}

		// Имя файла не должно уводить из каталога набора: набор описывает свой
		// каталог, а не файловую систему сервера.
		if a.File == "" || strings.ContainsRune(a.File, os.PathSeparator) || strings.Contains(a.File, "..") {
			return fmt.Errorf("content: ассет %s: file %q должен быть именем файла в каталоге набора", a.Name, a.File)
		}
		raw, err := os.ReadFile(filepath.Join(dir, a.File))
		if err != nil {
			return fmt.Errorf("content: ассет %s: %w", a.Name, err)
		}
		gotSource := Addr(hashOf(raw))
		if gotSource != a.SourceHash {
			return fmt.Errorf("content: ассет %s: объявлен source_hash %s, у файла %s — "+
				"сервер обязан отдавать то, что объявил", a.Name, a.SourceHash, gotSource)
		}

		body := raw
		if len(a.DropNodes) > 0 {
			body, err = dropSceneNodes(raw, a.DropNodes)
			if err != nil {
				return fmt.Errorf("content: ассет %s: подрезка сцены: %w", a.Name, err)
			}
			if !a.Attribution.Modified {
				return fmt.Errorf("content: ассет %s: сцена подрезается, а modified=false; "+
					"изменения обязаны быть объявлены", a.Name)
			}
		}
		a.Hash = Addr(hashOf(body))
		a.Size = len(body)

		// Одни байты не могут иметь двух разных авторов: атрибуция принадлежит
		// содержимому, а не имени, иначе второй регион, сославшийся на тот же
		// хеш, получил бы байты без лицензии.
		if prev, ok := byHash[a.Hash]; ok && prev != a.Attribution {
			return fmt.Errorf("content: у хеша %s две разные атрибуции: одни байты — один автор", a.Hash)
		}
		byHash[a.Hash] = a.Attribution
		s.blobs[a.Hash] = body
		out = append(out, a)
	}
	s.Assets = out
	sortedByID(s.Assets, func(a Asset) string { return a.Name })
	return nil
}

// checkDeclared проверяет то, что записано в самой записи, до чтения файла.
func (a Asset) checkDeclared() error {
	if a.MediaType == "" {
		return fmt.Errorf("content: ассет %s: не указан media_type", a.Name)
	}
	if !strings.HasPrefix(a.SourceHash, HashAlg+hashSep) {
		return fmt.Errorf("content: ассет %s: source_hash %q не начинается с %s%s",
			a.Name, a.SourceHash, HashAlg, hashSep)
	}
	if a.Anchor == "" {
		return fmt.Errorf("content: ассет %s: не указан anchor; без него меш ставится наугад", a.Name)
	}
	if !(a.Scale > 0) || math.IsInf(a.Scale, 0) {
		return fmt.Errorf("content: ассет %s: scale = %v, ожидалось положительное конечное число",
			a.Name, a.Scale)
	}
	for i, v := range a.Translation {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return fmt.Errorf("content: ассет %s: translation[%d] = %v", a.Name, i, v)
		}
	}
	at := a.Attribution
	for _, f := range []struct{ name, v string }{
		{"title", at.Title}, {"author", at.Author}, {"source", at.Source}, {"license", at.License},
	} {
		if f.v == "" {
			return fmt.Errorf("content: ассет %s: атрибуция без поля %s; "+
				"не «автор неизвестен», а отказ", a.Name, f.name)
		}
	}
	if !spdxLicenses[at.License] {
		return fmt.Errorf("content: ассет %s: лицензия %q вне перечня SPDX", a.Name, at.License)
	}
	if at.Modified && at.Modifications == "" {
		return fmt.Errorf("content: ассет %s: modified=true при пустом modifications; "+
			"CC-BY требует называть изменения, а не отмечать их факт", a.Name)
	}
	if !at.Modified && at.Modifications != "" {
		return fmt.Errorf("content: ассет %s: modifications при modified=false", a.Name)
	}
	return nil
}

func hashOf(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
