// Command mapexport — выгрузка карты фабрики в файл авторской карты.
//
// # Зачем существует
//
// Этой командой получен server/maps/st_a.json: боевая карта до 2026-08-12 жила
// кодом (seedmap.Station с рецептом рельефа), и переход на файл обязан был иметь
// ПРОИСХОЖДЕНИЕ. Иначе в репозиторий лёг бы файл на восемьсот строк, про который
// известно только то, что кто-то однажды его набрал.
//
// # Почему это НЕ сервер
//
// Отдельный бинарь, а не ключ у clearahead: сервер после перехода не знает
// фабрику вовсе — он читает файл. Ключ `-dump-seed` у сервера вернул бы seedmap
// в боевую сборку ровно тем чёрным ходом, который перекрывается.
//
// # Чего команда не умеет
//
// Выгружать карту ИЗ БАЗЫ. Документа карты в world.db нет: туда пишутся
// развёрнутые чанки, а топология, геометрия и рецепты — нет (бида
// ClearAhead-c0o). Поэтому источник у выгрузки один — фабрика.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/seedmap"
)

func main() {
	out := flag.String("out", "", "куда писать карту; пусто — в стандартный вывод")
	which := flag.String("map", "station", "какую карту фабрики выгрузить: station | blank | line")
	flag.Parse()

	var m *mapfmt.Map
	switch *which {
	case "station":
		// Ровно та затравка, которой сервер засевал мир до перехода на файл:
		// станция с рецептом рельефа. Меняя этот вызов, помните, что боевой
		// картой является ФАЙЛ, а не он: перевыгрузка затирает правки, сделанные
		// в файле руками.
		m = seedmap.Station(seedmap.WithTerrain())
	case "blank":
		m = seedmap.Blank()
	case "line":
		m = seedmap.Line(seedmap.WithTerrain())
	default:
		log.Fatalf("mapexport: неизвестная карта %q; известны station, blank, line", *which)
	}

	// Валидация ПЕРЕД записью, а не после. Файл, который сервер откажется
	// принять, не должен появляться на диске: правило «то, что нельзя загрузить,
	// не хранится» действует и в эту сторону.
	if err := mapfmt.Validate(m); err != nil {
		log.Fatalf("mapexport: карта фабрики не проходит валидацию: %v", err)
	}

	w := os.Stdout
	if *out != "" {
		f, err := os.Create(*out)
		if err != nil {
			log.Fatalf("mapexport: создание %s: %v", *out, err)
		}
		defer f.Close()
		w = f
	}
	if err := mapfmt.Encode(w, m); err != nil {
		log.Fatalf("mapexport: %v", err)
	}
	if *out != "" {
		fmt.Fprintf(os.Stderr, "карта %s ревизия %d записана в %s\n", m.MapID, m.MapRevision, *out)
	}
}
