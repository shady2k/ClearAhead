# ClearAhead — цели разработки.
#
# КЛИЕНТА СЕЙЧАС НЕТ. Он снесён целиком 2026-08-11 вместе со спайками: тот
# клиент выдумывал больше, чем получал, — рельеф, лес, посёлок, реку,
# продольный профиль пути и размеры платформ. Новый пишется как ТУПОЙ РЕНДЕР
# (решение владельца, см. биду про границу клиента), и он вернётся сюда своими
# целями, когда сервер начнёт отдавать то, что раньше выдумывалось.
#
# Всё, что относилось к Godot, — снимки, зонды, DISPLAY, noVNC — уехало вместе
# с ним. Знание, добытое на нём, не выброшено: оно записано в bd, потому что
# ошибку «клиент проверен грепом лога, а показывал 404» дешевле прочитать, чем
# повторить.

# На NixOS /bin/bash не существует — берём тот, что реально на PATH.
SHELL := $(shell command -v bash 2>/dev/null || echo /bin/sh)

SERVER_ADDR ?= :8080
SERVER_URL  ?= http://127.0.0.1$(SERVER_ADDR)

# Карту серверу больше не передают файлом: затравка строится кодом
# (seedmap.Station), а на диске живёт база мира. DB — её файл; удалить его
# значит засеять мир заново при следующем запуске.
#
# Путь абсолютный намеренно: модуль лежит в server/, поэтому serve заходит туда
# (как build и test-go), и относительный путь означал бы server/world.db, а не
# то, что написано в переменной.
DB          ?= $(CURDIR)/world.db

GO ?= go

.PHONY: help serve test test-go build fmt

help:
	@echo 'ClearAhead — цели разработки'
	@echo
	@echo '  make serve   сервер на $(SERVER_ADDR) (передний план, Ctrl-C — стоп)'
	@echo '  make test    go build + go vet + go test ./...'
	@echo '  make build   сборка сервера'
	@echo '  make fmt     gofmt по серверу'
	@echo
	@echo 'Переменные: DB, SERVER_ADDR'
	@echo
	@echo 'Примеры:'
	@echo '  make serve SERVER_ADDR=:8091 DB=/tmp/world.db'
	@echo '  curl -s $(SERVER_URL)/regions/ST_A'
	@echo '  curl -s $(SERVER_URL)/regions/ST_A/revisions/2/network'
	@echo '  curl -sD- -o /dev/null $(SERVER_URL)/regions/ST_A/chunks/0/0/0'
	@echo '  curl -s $(SERVER_URL)/manifest'
	@echo
	@echo 'Корень игрока один — регион: манифест региона называет ревизию,'
	@echo 'хеши и числа правила подробности, по ним берут сеть и рельеф.'
	@echo 'Ресурс geometry удалён: сеть живёт на /revisions/{n}/network.'
	@echo 'Ручки /maps без пути нет — она снесена вместе с JSON (96ccacf).'
	@echo 'Регион совпадает с map_id: на затравке это ST_A.'
	@echo 'Чанк — двоичный, поэтому в примере показаны заголовки, а не тело.'
	@echo
	@echo 'Клиента нет — см. шапку Makefile.'

## serve — только сервер. Держит порт, поэтому в переднем плане.
## go.mod лежит в server/, а не в корне: без cd команда не собирается вовсе.
serve:
	cd server && $(GO) run ./cmd/clearahead -db $(DB) -addr $(SERVER_ADDR)

test-go:
	cd server && $(GO) build ./... && $(GO) vet ./... && $(GO) test ./...

## test — пока это ровно test-go: клиентских проверок не существует, потому что
## не существует клиента. Цель оставлена отдельной, чтобы клиентская половина
## вернулась сюда, а не в память того, кто будет её писать.
test: test-go

build:
	cd server && $(GO) build ./...

fmt:
	cd server && gofmt -w .
