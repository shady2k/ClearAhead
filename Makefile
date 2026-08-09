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

# Общий план ST_A — волосок: 1800 м на 18 м. Детали видны только через
# --focus/--zoom, поэтому они вынесены в переменные, а не зашиты.
SHOT_ARGS := --shot $(SHOT) --frames $(FRAMES)
ifneq ($(strip $(FOCUS)),)
SHOT_ARGS += --focus $(FOCUS)
endif
ifneq ($(strip $(ZOOM)),)
SHOT_ARGS += --zoom $(ZOOM)
endif

.PHONY: help serve client shot contract test test-go test-client vnc build fmt clean

help:
	@echo 'ClearAhead — цели разработки'
	@echo
	@echo '  make serve      сервер карты на $(SERVER_ADDR) (передний план, Ctrl-C — стоп)'
	@echo '  make client     клиент на $(DISPLAY_ID); смотреть в браузере: $(VNC_URL)'
	@echo '  make shot       снимок клиента в $(SHOT) — ЕДИНСТВЕННАЯ надёжная проверка'
	@echo '  make contract   контрактный тест клиента против эталона (без сервера)'
	@echo '  make test       всё: go test ./... + контрактный тест клиента'
	@echo '  make vnc        напомнить, куда смотреть и где пароль'
	@echo
	@echo 'Переменные: MAP, SERVER_ADDR, SHOT, FOCUS="x,y", ZOOM=<пикс/м>, DISPLAY_ID'
	@echo
	@echo 'Примеры:'
	@echo '  make serve MAP=server/internal/mapfmt/testdata/fixture_station.json'
	@echo '  make shot FOCUS=400,0 ZOOM=30      # горловина крупно'

## serve — сервер карты. Держит порт, поэтому в переднем плане.
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

## contract — клиент разбирает эталон БОЕВЫМ парсером. Сцену не поднимает,
## поэтому здесь --headless законен.
contract:
	$(GODOT) --headless --path client --script tests/contract_check.gd

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
