# ClearAhead — цели разработки.
#
# ГЛАВНОЕ ПРО GODOT, чтобы не наступить дважды.
#
# `--headless` НЕ РИСУЕТ. Документация Godot про выделенные серверы
# (tutorials/export/exporting_for_dedicated_servers) называет его единственным
# флагом безголового запуска, но про отрисовку молчит — а он отключает драйвер
# дисплея целиком. Поэтому:
#
#   * что-то показать или снять снимок  -> DISPLAY=:1, без --headless;
#   * чистая логика без сцены           -> --headless можно.
#
# И второе: КЛИЕНТА НЕЛЬЗЯ ПРОВЕРЯТЬ ЛОГОМ. Отказ HTTP приходит асинхронно и
# обрабатывается штатно — в интерфейсе «Сервер ответил HTTP 404» и кнопка
# «Повторить», а в логе при этом НИ ОДНОЙ ошибки. Проверено на своей ошибке
# 2026-08-09: клиент был объявлен рабочим по грепу лога, а показывал 404.
# Надёжна только цель `shot` и посмотреть на снимок.

# На NixOS /bin/bash не существует — берём тот, что реально на PATH.
SHELL := $(shell command -v bash 2>/dev/null || echo /bin/sh)

# Дисплей, который смотрит noVNC. Xvfb :1 -> x11vnc -> websockify :6080.
# :99 существует, но его никто не видит — туда клиента запускать бессмысленно.
DISPLAY_ID ?= :1
VNC_URL    ?= http://192.168.0.25:6080/vnc.html

SERVER_ADDR ?= :8080
SERVER_URL  ?= http://127.0.0.1$(SERVER_ADDR)
MAP         ?= server/maps/st_a.json

SHOT   ?= /tmp/clearahead.png
FRAMES ?= 180
FOCUS  ?=
ZOOM   ?=

GODOT ?= godot
GO    ?= go

# Параметры камеры эскиза 3D (только для sketch3d-shot).
SIZE ?=
AZ   ?=
ELEV ?=

# Порт отдельно от адреса: цель dev ждёт, пока он начнёт слушать.
# lastword, а не strip двоеточия: SERVER_ADDR может быть и "127.0.0.1:8080".
SERVER_PORT := $(lastword $(subst :, ,$(SERVER_ADDR)))

# Собранный сервер для цели dev. Собираем, а не `go run`: у `go run` свой
# процесс-обёртка, и убийство его PID оставляет настоящий сервер жить на порту.
DEV_BIN ?= /tmp/clearahead-dev

# Общий план ST_A — волосок: 1800 м на 18 м. Детали видны только через
# --focus/--zoom, поэтому они вынесены в переменные, а не зашиты.
SHOT_ARGS := --shot $(SHOT) --frames $(FRAMES)
ifneq ($(strip $(FOCUS)),)
SHOT_ARGS += --focus $(FOCUS)
endif
ifneq ($(strip $(ZOOM)),)
SHOT_ARGS += --zoom $(ZOOM)
endif

SK_ARGS :=
ifneq ($(strip $(FOCUS)),)
SK_ARGS += --focus $(FOCUS)
endif
ifneq ($(strip $(SIZE)),)
SK_ARGS += --size $(SIZE)
endif
ifneq ($(strip $(AZ)),)
SK_ARGS += --azimuth $(AZ)
endif
ifneq ($(strip $(ELEV)),)
SK_ARGS += --elev $(ELEV)
endif

.PHONY: help world world-shot terrain-probe tile-dump sketch3d sketch3d-shot dev dev-bin dev-shot serve client shot shot-offline contract test test-go test-client vnc build fmt clean

help:
	@echo 'ClearAhead — цели разработки'
	@echo
	@echo '  make dev        СЕРВЕР И КЛИЕНТ разом — обычный способ запустить редактор'
	@echo '  make dev-shot   то же, но снимок вместо показа, и оба гасятся'
	@echo '  make serve      только сервер на $(SERVER_ADDR) (передний план, Ctrl-C — стоп)'
	@echo '  make client     только клиент на $(DISPLAY_ID); смотреть: $(VNC_URL)'
	@echo '  make shot       снимок клиента в $(SHOT) — ЕДИНСТВЕННАЯ надёжная проверка'
	@echo '  make contract   контрактный тест клиента против эталона (без сервера)'
	@echo '  make test       всё: go test ./... + контрактный тест клиента'
	@echo '  make sketch3d   ЭСКИЗ: тот же путь мешами в 3D, орто-камера'
	@echo '  make world      СПАЙК МИРА: рельеф, лес, река, посёлок (нужен Forward+)'
	@echo '  make world-shot снимок спайка мира в $(SHOT)'
	@echo '  make terrain-probe  статистика поля высот спайка мира (без окна)'
	@echo '  make tile-dump      процедурные тайлы спайка мира в PNG (без окна)'
	@echo '  make vnc        напомнить, куда смотреть и где пароль'
	@echo
	@echo 'Переменные: MAP, SERVER_ADDR, SHOT, FOCUS="x,y", ZOOM=<пикс/м>, DISPLAY_ID'
	@echo
	@echo 'Примеры:'
	@echo '  make serve MAP=server/internal/mapfmt/testdata/fixture_station.json'
	@echo '  make shot FOCUS=400,0 ZOOM=30      # горловина крупно'

## dev — сервер И клиент одной командой. Обычный способ запустить редактор.
##
## Сервер уходит в фон, цель ждёт, пока порт начнёт слушать, и только потом
## поднимает клиента: иначе клиент успевает спросить раньше, чем есть кого
## спрашивать, и штатно показывает отказ — выглядит как поломка, но это гонка.
##
## trap на EXIT/INT/TERM: закрыли клиента или нажали Ctrl-C — сервер уходит
## следом. Осиротевший сервер держит порт, и следующий запуск падает на
## «address already in use», а причина уже не видна.
dev: dev-bin
	@echo 'Клиент на $(DISPLAY_ID). Смотреть: $(VNC_URL)'
	@$(DEV_BIN) -map $(MAP) -addr $(SERVER_ADDR) & \
	srv=$$!; \
	trap 'kill $$srv 2>/dev/null || true' EXIT INT TERM; \
	for i in $$(seq 1 50); do \
		(exec 3<>/dev/tcp/127.0.0.1/$(SERVER_PORT)) 2>/dev/null && break; \
		kill -0 $$srv 2>/dev/null || { echo 'сервер не поднялся — смотрите вывод выше'; exit 1; }; \
		sleep 0.1; \
	done; \
	DISPLAY=$(DISPLAY_ID) $(GODOT) --path client -- --server=$(SERVER_URL)

## dev-shot — то же, но вместо показа снимает снимок и гасит обоих.
## Годится для быстрой проверки «что там сейчас на экране».
dev-shot: dev-bin
	@$(DEV_BIN) -map $(MAP) -addr $(SERVER_ADDR) & \
	srv=$$!; \
	trap 'kill $$srv 2>/dev/null || true' EXIT INT TERM; \
	for i in $$(seq 1 50); do \
		(exec 3<>/dev/tcp/127.0.0.1/$(SERVER_PORT)) 2>/dev/null && break; \
		kill -0 $$srv 2>/dev/null || { echo 'сервер не поднялся'; exit 1; }; \
		sleep 0.1; \
	done; \
	DISPLAY=$(DISPLAY_ID) $(GODOT) --path client --script tests/smoke_screenshot.gd -- \
		--server=$(SERVER_URL) $(SHOT_ARGS)
	@echo "снимок: $(SHOT) — посмотрите на него, логу верить нельзя"

# Всегда пересобираем: файловая цель кэшировалась бы, и dev молча запускал бы
# вчерашний сервер. Go-сборка инкрементальна, это дёшево.
dev-bin:
	@cd server && $(GO) build -o $(DEV_BIN) ./cmd/clearahead

## serve — только сервер. Держит порт, поэтому в переднем плане.
serve:
	$(GO) run ./server/cmd/clearahead -map $(MAP) -addr $(SERVER_ADDR)

## client — клиент на видимом дисплее. Сервер должен быть уже поднят.
## Формат аргумента именно --server=URL: main.gd разбирает его через begins_with("--server=").
client:
	@echo 'Клиент на $(DISPLAY_ID). Смотреть: $(VNC_URL)'
	DISPLAY=$(DISPLAY_ID) $(GODOT) --path client -- --server=$(SERVER_URL)

## shot — снимок. Сервер нужен; без него клиент штатно покажет отказ,
## и это будет видно на снимке, а не в логе.
## Аргументы снимка разделены ПРОБЕЛОМ: smoke_screenshot.gd берёт их через
## _arg_value(name) = args[i+1], а не через "=".
shot:
	DISPLAY=$(DISPLAY_ID) $(GODOT) --path client --script tests/smoke_screenshot.gd -- \
		--server=$(SERVER_URL) $(SHOT_ARGS)
	@echo "снимок: $(SHOT) — посмотрите на него, логу верить нельзя"

## shot-offline — снимок из файла эталона, без сервера. Полезно, когда
## проверяется отрисовка, а не сеть.
shot-offline:
	DISPLAY=$(DISPLAY_ID) $(GODOT) --path client --script tests/smoke_screenshot.gd -- \
		--geometry-file=../contract/render_geometry.golden.json $(SHOT_ARGS)
	@echo "снимок: $(SHOT)"

## sketch3d — ЭСКИЗ: тот же путь, но мешами в 3D под ортографической камерой.
## Читает эталон напрямую, сервер не нужен. Камера статична: угол и охват
## задаются переменными, интерактивного вращения в эскизе нет.
##   make sketch3d                       общий план
##   make sketch3d FOCUS=160,-3.5 SIZE=46   горловина крупно
##   make sketch3d ELEV=80               почти сверху, для сравнения с 2D
## Живьём сцена запускается напрямую: скрипт снимка всегда снимает и выходит,
## режима показа у него нет. Камера тогда берёт умолчания сцены (автоохват,
## азимут 45, наклон 35) — переменные действуют только на sketch3d-shot.
sketch3d:
	@echo 'Эскиз 3D на $(DISPLAY_ID). Смотреть: $(VNC_URL) (Ctrl-C — стоп)'
	DISPLAY=$(DISPLAY_ID) $(GODOT) --path client res://scenes/sketch3d.tscn

## sketch3d-shot — снимок эскиза в $(SHOT).
sketch3d-shot:
	DISPLAY=$(DISPLAY_ID) $(GODOT) --path client --script res://scripts/sketch3d_shot.gd -- \
		--shot $(SHOT) $(SK_ARGS)
	@echo "снимок: $(SHOT)"

## world / world-shot — СПАЙК МИРА: тот же путь, но с рельефом, лесом, рекой и
## посёлком (client/scripts/spike_world.gd). Всё, кроме пути, выдумано клиентом.
##
## Нужен Forward+: спайк рассчитывает на тени, дымку и SSAO, которых в
## gl_compatibility нет. Метод передаётся флагом запуска, project.godot не
## трогается — на llvmpipe-машине Forward+ не поднимется, там RENDER=gl_compatibility.
##   make world                                живой просмотр, мышь крутит камеру
##   make world-shot                           общий план в $(SHOT)
##   make world-shot SIZE=150 ELEV=26 FOCUS=170,0   станция крупно
RENDER ?= forward_plus
WORLD_SCENE := res://scenes/spike_world.tscn
# Умолчания камеры общего плана; SIZE/ELEV/AZ/FOCUS перекрывают их по одному.
# Расписано через ifeq, а не через $(if ...): в FOCUS есть запятая, а она у make
# — разделитель аргументов функции.
W_SIZE  := 420
W_ELEV  := 40
W_AZ    := 205
W_FOCUS := 240,40
ifneq ($(strip $(SIZE)),)
W_SIZE := $(SIZE)
endif
ifneq ($(strip $(ELEV)),)
W_ELEV := $(ELEV)
endif
ifneq ($(strip $(AZ)),)
W_AZ := $(AZ)
endif
ifneq ($(strip $(FOCUS)),)
W_FOCUS := $(FOCUS)
endif
WORLD_ARGS := --size $(W_SIZE) --elev $(W_ELEV) --azimuth $(W_AZ) --focus $(W_FOCUS)

world:
	DISPLAY=$(DISPLAY_ID) $(GODOT) --rendering-method $(RENDER) --path client $(WORLD_SCENE)

world-shot:
	DISPLAY=$(DISPLAY_ID) $(GODOT) --rendering-method $(RENDER) --path client \
		--script res://scripts/spike_shot.gd -- --scene $(WORLD_SCENE) \
		--shot $(SHOT) --persp 1 $(WORLD_ARGS)
	@echo "снимок: $(SHOT)"

## contract — клиент разбирает эталон БОЕВЫМ парсером. Сцену не поднимает,
## поэтому здесь --headless законен.
contract:
	$(GODOT) --headless --path client --script tests/contract_check.gd

## terrain-probe — СТАТИСТИКА ПОЛЯ ВЫСОТ спайка мира, без окна и рендера.
## Поле высот — чистая функция координат, и спорить о ней по снимку бессмысленно:
## «земля выглядит плоской» может значить и клэмп в коде, и слабый шум, и
## неудачную камеру. Зонд отвечает числом. Им найдены оба дефекта разбора
## 2026-08-10: 37 % карты, срезанных клэмпом в плоскость, и эрозия, которая
## гасила деталь на 2 % вместо обещанных гребней и промоин.
terrain-probe:
	$(GODOT) --headless --path client --script res://tools/terrain_probe.gd

## tile-dump — ВЫГРУЗКА ПРОЦЕДУРНЫХ ТАЙЛОВ спайка мира в PNG, без окна и рендера.
## Тайл — чистая функция кода, и спорить о нём по снимку сцены бессмысленно: на
## общем плане мип съедает рисунок, и «трава убогая» может значить и материал, и
## свет, и сам тайл. Так найдена причина 2026-08-10: дернина рисовала РОЗЕТКИ —
## листья лучами из центра куртины, то есть одуванчики, — и на снимке это читалось
## брокколи. Видно это только на самом тайле.
##   make tile-dump OUT=/tmp   -> tile_turf.png, tile_blade_0..2.png
OUT ?= /tmp
tile-dump:
	$(GODOT) --headless --path client --script res://tools/tile_dump.gd -- --out $(OUT)

test-go:
	cd server && $(GO) build ./... && $(GO) vet ./... && $(GO) test ./...

test-client: contract
	$(GODOT) --headless --path client --script tests/test_geometry_math.gd
	$(GODOT) --headless --path client --script tests/test_geometry_parser.gd
	$(GODOT) --headless --path client --script tests/test_draw_helpers.gd

test: test-go test-client

build:
	cd server && $(GO) build ./...

fmt:
	cd server && gofmt -w .

vnc:
	@echo 'Дисплей:  $(DISPLAY_ID)  (Xvfb 1920x1080; :99 никто не видит)'
	@echo 'Браузер:  $(VNC_URL)'
	@echo 'Пароль:   /var/lib/x11vnc/passwd.txt'
	@for s in xvfb x11vnc novnc; do \
		printf '%-8s %s\n' "$$s" "$$(systemctl is-active $$s.service 2>/dev/null || echo '?')"; \
	done

clean:
	rm -f $(SHOT)
