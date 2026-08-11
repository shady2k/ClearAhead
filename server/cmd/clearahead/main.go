// Command clearahead — сервер мира.
package main

import (
	"flag"
	"log"
	"net/http"
	"time"

	"github.com/shady2k/ClearAhead/server/internal/httpapi"
	"github.com/shady2k/ClearAhead/server/internal/mapstore"
	"github.com/shady2k/ClearAhead/server/internal/seedmap"
	"github.com/shady2k/ClearAhead/server/internal/worldgen"
	"github.com/shady2k/ClearAhead/server/internal/worldstore"
)

func main() {
	mapPath := flag.String("map", "", "путь к файлу карты; не указан — сервер стартует без карты")
	mapsDir := flag.String("maps", "server/maps", "каталог карт")
	dbPath := flag.String("db", "world.db", "файл базы мира")
	addr := flag.String("addr", ":8080", "адрес прослушивания")
	flag.Parse()

	store, err := mapstore.Open(*mapsDir)
	if err != nil {
		log.Fatalf("каталог карт %s: %v", *mapsDir, err)
	}

	// Старт без карты — норма: сервер поднимается пустым и ждёт «новую» или
	// «загрузить». Флаг -map необязателен: указали — грузим, не указали —
	// пустой старт.
	if *mapPath != "" {
		st, err := store.LoadPath(*mapPath)
		if err != nil {
			log.Fatalf("карта %s: %v", *mapPath, err)
		}
		log.Printf("карта %s ревизия %d: %d элементов, геометрия %s",
			st.Manifest.MapID, st.Manifest.Revision, len(st.Track.Elements), st.Manifest.RenderGeometryHash[:12])
	} else {
		log.Printf("пустой старт: карта не загружена, жду «новую» или «загрузить»")
	}

	world, err := worldstore.Open(*dbPath)
	if err != nil {
		log.Fatalf("база мира %s: %v", *dbPath, err)
	}
	defer world.Close()

	// Бутстрап идемпотентен: заполняет только пустую базу. Затравка строится
	// кодом, а не читается файлом, — карта, собранная кодом, не может разойтись
	// со схемой формата, потому что перестаёт компилироваться.
	rep, сделан, err := worldgen.Bootstrap(world, seedmap.Station(seedmap.WithTerrain()), 1)
	if err != nil {
		log.Fatalf("бутстрап мира: %v", err)
	}
	if сделан {
		log.Printf("мир засеян: регион %s, чанков %d, %.1f МБ",
			rep.Region, rep.TotalChunks, float64(rep.TotalBytes)/1e6)
	} else {
		log.Printf("мир уже есть: регион %s, база не тронута", rep.Region)
	}

	// КОМПОЗИЦИЯ ЖИВЁТ ЗДЕСЬ, а не внутри обработчиков.
	//
	// Каждая ручка знает ровно своё хранилище: карт — mapstore, чанков —
	// worldstore. Появление третьего хранилища добавит строку сюда и не тронет
	// ни одного существующего обработчика. Обратный порядок — передать второе
	// хранилище в обработчик карт и разветвлять путь внутри его ServeHTTP —
	// сделал бы его корнем композиции для всего мира.
	mux := http.NewServeMux()
	mux.Handle("/regions/", httpapi.NewChunksHandler(world))
	mux.Handle("/", httpapi.NewHandler(store))

	// http.ListenAndServe не ставит ни одного таймаута: соединение, которое
	// медленно шлёт заголовки, держит горутину бесконечно. На LAN-игре это не
	// катастрофа, но таймауты стоят четыре строки и ставятся сразу.
	srv := &http.Server{
		Addr:              *addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 16,
	}
	log.Printf("слушаю %s", *addr)
	log.Fatal(srv.ListenAndServe())
}
