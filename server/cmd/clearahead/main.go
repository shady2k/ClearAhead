// Command clearahead — сервер карты.
package main

import (
	"flag"
	"log"
	"net/http"
	"time"

	"github.com/shady2k/ClearAhead/server/internal/httpapi"
	"github.com/shady2k/ClearAhead/server/internal/mapstore"
)

func main() {
	mapPath := flag.String("map", "", "путь к файлу карты; не указан — сервер стартует без карты")
	mapsDir := flag.String("maps", "server/maps", "каталог карт")
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

	// http.ListenAndServe не ставит ни одного таймаута: соединение, которое
	// медленно шлёт заголовки, держит горутину бесконечно. На LAN-игре это не
	// катастрофа, но таймауты стоят четыре строки и ставятся сразу.
	srv := &http.Server{
		Addr:              *addr,
		Handler:           httpapi.NewHandler(store),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 16,
	}
	log.Printf("слушаю %s", *addr)
	log.Fatal(srv.ListenAndServe())
}
