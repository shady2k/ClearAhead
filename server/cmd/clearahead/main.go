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
	dbPath := flag.String("db", "world.db", "файл базы мира")
	addr := flag.String("addr", ":8080", "адрес прослушивания")
	flag.Parse()

	// Затравка одна и та же для карты в памяти и для мира в базе: иначе сеть
	// описывала бы одну станцию, а рельеф — другую.
	seed := seedmap.Station(seedmap.WithTerrain())

	store := mapstore.Open()
	st, err := store.Set(seed)
	if err != nil {
		log.Fatalf("затравочная карта не проходит вход: %v", err)
	}
	log.Printf("карта %s ревизия %d: %d элементов, сеть %s",
		st.Manifest.MapID, st.Manifest.Revision, len(st.Network.Elements), st.Manifest.NetworkHash[:12])

	world, err := worldstore.Open(*dbPath)
	if err != nil {
		log.Fatalf("база мира %s: %v", *dbPath, err)
	}
	defer world.Close()

	// Бутстрап идемпотентен: заполняет только пустую базу. Затравка строится
	// кодом, а не читается файлом, — карта, собранная кодом, не может разойтись
	// со схемой формата, потому что перестаёт компилироваться.
	rep, seeded, err := worldgen.Bootstrap(world, seed, 1)
	if err != nil {
		log.Fatalf("бутстрап мира: %v", err)
	}
	if seeded {
		log.Printf("мир засеян: регион %s, чанков %d, %.1f МБ",
			rep.Region, rep.TotalChunks, float64(rep.TotalBytes)/1e6)
	} else {
		log.Printf("мир уже есть: регион %s, база не тронута", rep.Region)
	}

	// КОМПОЗИЦИЯ ЖИВЁТ ЗДЕСЬ, а не внутри обработчиков.
	//
	// Каждая ручка знает ровно своё хранилище: сети — mapstore, чанков —
	// worldstore. Появление третьего хранилища добавит строку сюда и не тронет
	// ни одного существующего обработчика. Обратный порядок — передать второе
	// хранилище в обработчик карт и разветвлять путь внутри его ServeHTTP —
	// сделал бы его корнем композиции для всего мира.
	//
	// У корня /regions/ ровно поэтому такая форма: сеть региона лежит в
	// mapstore, рельеф — в worldstore, а собрать один корень из двух хранилищ
	// вправе только тот, кто открыл оба, — то есть main. Роутер получает готовые
	// подручки и не знает ни одного хранилища; манифест региона — единственный,
	// кому нужны оба, и оба приходят ему аргументами, а не через соседа.
	mux := http.NewServeMux()
	mux.Handle("/regions/", httpapi.NewRegionsHandler(
		httpapi.NewRegionManifestHandler(world, store),
		httpapi.NewNetworkHandler(store),
		httpapi.NewChunksHandler(world),
	))
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
