// Command clearahead — сервер карты.
package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/shady2k/ClearAhead/server/internal/httpapi"
	"github.com/shady2k/ClearAhead/server/internal/mapfmt"
	"github.com/shady2k/ClearAhead/server/internal/track"
)

func main() {
	mapPath := flag.String("map", "server/maps/st_a.json", "путь к файлу карты")
	addr := flag.String("addr", ":8080", "адрес прослушивания")
	flag.Parse()

	f, err := os.Open(*mapPath)
	if err != nil {
		log.Fatalf("карта %s: %v", *mapPath, err)
	}
	defer f.Close()

	m, err := mapfmt.Decode(f)
	if err != nil {
		log.Fatalf("разбор карты: %v", err)
	}
	if err := mapfmt.Validate(m); err != nil {
		log.Fatalf("проверка карты: %v", err)
	}
	ct, rg, err := track.Compile(m)
	if err != nil {
		log.Fatalf("компиляция карты: %v", err)
	}
	man, err := track.BuildManifest(m, ct, rg)
	if err != nil {
		log.Fatalf("манифест: %v", err)
	}

	log.Printf("карта %s ревизия %d: %d элементов, геометрия %s",
		man.MapID, man.Revision, len(ct.Elements), man.RenderGeometryHash[:12])
	// http.ListenAndServe не ставит ни одного таймаута: соединение, которое
	// медленно шлёт заголовки, держит горутину бесконечно. На LAN-игре это не
	// катастрофа, но таймауты стоят четыре строки и ставятся сразу.
	srv := &http.Server{
		Addr:              *addr,
		Handler:           httpapi.NewHandler(rg, man),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 16,
	}
	log.Printf("слушаю %s", *addr)
	log.Fatal(srv.ListenAndServe())
}
