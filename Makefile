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
MAP         ?= server/maps/st_a.json

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
	@echo 'Переменные: MAP, SERVER_ADDR'
	@echo
	@echo 'Примеры:'
	@echo '  make serve MAP=server/internal/mapfmt/testdata/fixture_station.json'
	@echo '  curl -s $(SERVER_URL)/maps'
	@echo
	@echo 'Клиента нет — см. шапку Makefile.'

## serve — только сервер. Держит порт, поэтому в переднем плане.
serve:
	$(GO) run ./server/cmd/clearahead -map $(MAP) -addr $(SERVER_ADDR)

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
