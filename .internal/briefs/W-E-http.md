# Воркер E — server/internal/httpapi и server/cmd: ручка геометрии и бинарь

Сначала прочитай `.internal/briefs/COMMON.md`. Go-модуль в `server/`,
модуль называется `github.com/shady2k/ClearAhead/server`.

## Ты владеешь
- `server/internal/httpapi/geometry.go`
- `server/internal/httpapi/geometry_test.go`
- `server/cmd/clearahead/main.go`

Каталогов нет — создай.

## Чужое, не трогать
`server/internal/{geom,units,mapfmt,protocol,rpc,track}` — готово, только читай.
`server/maps/st_a.json` — параллельно чинит воркер G. Карта сейчас **не
компилируется**, это известно и чинится не тобой. Твои тесты не должны её грузить.

## Что сделать
Задача 10 из `.internal/plans/2026-08-08-b1-server-half.md`. Полный код в плане.

## Главное ограничение — барьер валидации
Ручка **не читает путь и тело сама**. Она собирает `protocol.Input` и передаёт его
`rpc.Mux.Dispatch`; обработчик получает разобранный `protocol.GeometryRequest`.

Это не стилистика. В `server/internal/rpc/barrier_test.go` есть тест, который
обходит AST твоих файлов и **падает**, если встретит `PathValue`, `Unmarshal`,
`NewDecoder` или `ParseForm` вне `internal/rpc`. До сих пор он шёл вхолостую,
потому что каталогов `httpapi` и `cmd` не существовало. **С твоей работой он
впервые начнёт что-то проверять — и проверять он будет тебя.**

Значит `r.PathValue("id")` в обработчике писать нельзя. Путь разбирает диспетчер.

## Второе ограничение — ничего не блокируется
Тело геометрии сериализуется **один раз в `NewHandler`**, дальше только пишется в
сокет. Ни чтения с диска, ни перекомпиляции, ни сети внутри запроса.
`NewHandler` принимает уже готовые артефакты, а не путь к файлу.

## Третье — таймауты
`http.ListenAndServe` не ставит ни одного таймаута, и медленный клиент держит
горутину бесконечно. В бинаре собирай `http.Server` с `ReadHeaderTimeout`,
`ReadTimeout`, `WriteTimeout`, `IdleTimeout`, `MaxHeaderBytes` — значения в плане.

## Критерии приёмки
- `GET /maps/{id}/revisions/{n}/geometry` → 200 и JSON геометрии.
- `ETag` равен `render_geometry_hash` в кавычках; `Cache-Control` содержит `immutable`.
- Повторный запрос с `If-None-Match` → 304 **без тела**.
- Чужой `map_id` или чужая ревизия → 404. Метод не GET → 405.
- Бинарь читает карту из флага `-map`, валидирует, компилирует, поднимает сервер.

## Проверка (строго эта, не шире) — из каталога server/
```
cd server && go build ./internal/httpapi/ ./cmd/... && go vet ./internal/httpapi/ ./cmd/... && go test ./internal/httpapi/ -v && go test ./internal/rpc/ -run TestBarrier -v
```
Последняя команда обязательна: это тот самый барьерный тест, который теперь
увидит твои файлы. Если он упал — значит ты читаешь сырой вход мимо диспетчера.

В отчёте: прошёл ли барьерный тест и сколько файлов он теперь сканирует.
