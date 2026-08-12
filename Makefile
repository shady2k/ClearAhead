# ClearAhead — цели разработки.
#
# КЛИЕНТ ВЕРНУЛСЯ 2026-08-11, но другим. Прежний снесён целиком за то, что
# выдумывал больше, чем получал: рельеф, лес, посёлок, реку, продольный профиль
# пути и размеры платформ. Новый — ТУПОЙ РЕНДЕР (решение владельца, биды
# ClearAhead-sjq и ClearAhead-0xc): чего сервер не прислал, того на экране нет.
#
# ПРОВЕРКА КЛИЕНТА ДЕЛИТСЯ НАДВОЕ, и это не удобство, а два записанных урока:
#
#   client-check — всё, что не пиксели: коды ответов, разбор сети, декодирование
#     чанка, выбор уровня, сходимость чисел. Идёт под --headless, окна нет.
#   client-shot  — снимок экрана. --headless в нём НЕВОЗМОЖЕН: он подменяет
#     растеризатор заглушкой, get_image() возвращает null, а frame_post_draw не
#     наступает никогда. Поэтому окно настоящее, но уведено за экран
#     (--position -9000,-9000), чтобы не мешало.
#
# Первый урок: клиента НЕЛЬЗЯ проверять грепом лога — отказ приходит асинхронно
# и обрабатывается штатно, в логе ни одной ошибки, а на экране 404 (bd recall
# godot-client-check). Второй: кадр надо ДОЖДАТЬСЯ, два раза, а не форсировать —
# force_draw сразу после сборки сцены даёт пустую картинку.

# На NixOS /bin/bash не существует — берём тот, что реально на PATH.
SHELL := $(shell command -v bash 2>/dev/null || echo /bin/sh)

SERVER_ADDR ?= :8080
SERVER_URL  ?= http://127.0.0.1$(SERVER_ADDR)

# Порт отдельно от адреса: цель dev ждёт, пока он начнёт слушать, а из
# ':8080' номер надо ещё выковырять.
SERVER_PORT ?= $(patsubst :%,%,$(SERVER_ADDR))

# Собранный сервер для цели dev. Собираем, а не `go run`: у `go run` свой
# процесс-обёртка, и kill по его PID оставил бы сервер жить, держа порт.
DEV_BIN ?= /tmp/clearahead-dev

# Карту серверу больше не передают файлом: затравка строится кодом
# (seedmap.Station), а на диске живёт база мира. DB — её файл; удалить его
# значит засеять мир заново при следующем запуске.
#
# Путь абсолютный намеренно: модуль лежит в server/, поэтому serve заходит туда
# (как build и test-go), и относительный путь означал бы server/world.db, а не
# то, что написано в переменной.
DB          ?= $(CURDIR)/world.db

GO ?= go

# Клиент. GODOT — движок (4.x), CLIENT — каталог проекта, REGION — что смотреть,
# SHOT — куда класть снимок. Адрес сервера клиент берёт из SERVER_URL: зашивать
# его в клиент нельзя, как и всё остальное про мир.
GODOT  ?= godot
CLIENT ?= $(CURDIR)/client
REGION ?= ST_A
## Снимки лежат ВНЕ проекта Godot, и это починка, а не вкус: пока они писались
## в client/shots/, движок импортировал их как текстуры (кэш .godot/imported/
## распухал доказательствами работы). У client/assets/ от этого был .gdignore, у
## shots/ его забыли. Дешевле не иметь их внутри проекта вовсе.
SHOT   ?= $(CURDIR)/shots/station.png
VIEW   ?= station

# Запятая переменной, и это не причуда. $(call …) режет свои аргументы ПО
# ЗАПЯТЫМ, поэтому «--position -9000,-9000» внутри $(call with_server,…) уезжало
# вторым аргументом: до Godot доходило «--position -9000», и он отвечал «Invalid
# position '-9000'». Снимок не делался вовсе, а виноватым выглядел клиент.
COMMA := ,

.PHONY: help dev dev-bin dev-check dev-shot serve test test-go build fmt client client-check client-shot client-import

help:
	@echo 'ClearAhead — цели разработки'
	@echo
	@echo '  make dev        СЕРВЕР И КЛИЕНТ разом — обычный способ запустить всё'
	@echo '  make dev-check  то же, но проверки без окна, и оба гасятся'
	@echo '  make dev-shot   то же, но снимок вместо показа, и оба гасятся'
	@echo
	@echo '  make serve   сервер на $(SERVER_ADDR) (передний план, Ctrl-C — стоп)'
	@echo '  make test    go build + go vet + go test ./...'
	@echo '  make build   сборка сервера'
	@echo '  make fmt     gofmt по серверу'
	@echo
	@echo '  make client        клиент окном (сервер должен уже работать)'
	@echo '  make client-check  проверки клиента без окна (--headless)'
	@echo '  make client-shot   снимок экрана в $(SHOT), окно за экраном'
	@echo
	@echo 'Переменные: DB, SERVER_ADDR, GODOT, REGION, SHOT, VIEW'
	@echo 'VIEW: station (вся станция), throat (горловина со стрелками), wide (весь рельеф).'
	@echo 'Горловина наводится по габаритам элементов с role.turnout, а не по числу.'
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
	@echo 'Клиент — тупой рендер: рельеф мешем, путь — отсыпка, решётка, нитки.'
	@echo 'Нитью рисуется то, чему сервер не прислал ширины, — так видно нехватку.'
	@echo 'Всё плоское, на отметке оси: от чего отсчитывается z, контракт не говорит.'
	@echo 'Снимок и проверки — разные цели: почему, написано в шапке Makefile.'

## with_server — общий пролог целей dev*: поднять сервер, дождаться порта,
## запустить переданную команду, погасить сервер при любом исходе.
##
## Три вещи здесь не для красоты, и каждая куплена ошибкой:
##
##  * СОБРАННЫЙ БИНАРЬ, а не `go run`. У `go run` свой процесс-обёртка, и kill
##    по её PID оставил бы сервер жить, держа порт: следующий запуск падал бы
##    с «address already in use», а виноватым выглядел бы Makefile.
##  * TRAP на EXIT/INT/TERM. Закрыли окно клиента или нажали Ctrl-C — сервер
##    уходит следом. Без этого за день работы накапливается десяток сирот
##    (этой сессией накопилось восемь, и снимал их владелец руками).
##  * ОЖИДАНИЕ ПОРТА, а не sleep. Клиент, стартовавший раньше сервера, получит
##    отказ соединения и честно покажет его красным — то есть цель врала бы про
##    клиента, а не про гонку. Проверка kill -0 рядом: если сервер умер при
##    старте, ждать его десять секунд незачем, надо сказать сразу.
define with_server
	@$(DEV_BIN) -db $(DB) -addr $(SERVER_ADDR) & \
	srv=$$!; \
	trap 'kill $$srv 2>/dev/null || true' EXIT INT TERM; \
	for i in $$(seq 1 100); do \
		(exec 3<>/dev/tcp/127.0.0.1/$(SERVER_PORT)) 2>/dev/null && break; \
		kill -0 $$srv 2>/dev/null || { echo 'сервер не поднялся — смотрите вывод выше'; exit 1; }; \
		sleep 0.1; \
	done; \
	$(1)
endef

## dev-bin — сервер бинарём для целей dev*.
##
## Цель ФОНОВАЯ (.PHONY), а не файловая, СПЕЦИАЛЬНО: файловая кэшировалась бы по
## времени правки, и `make dev` после правки сервера молча поднимал бы прежний
## бинарь. Отладка такого стоит часа и заканчивается словами «да он же не
## пересобрался».
dev-bin:
	@cd server && $(GO) build -o $(DEV_BIN) ./cmd/clearahead

## dev — СЕРВЕР И КЛИЕНТ одной командой. Обычный способ запустить всё.
dev: dev-bin client-import
	$(call with_server,$(GODOT) --path $(CLIENT) -- --server=$(SERVER_URL) --region=$(REGION))

## dev-check — сервер и проверки клиента, без единого окна. То, что стоит
## гонять после правки контракта: расхождение клиента с сервером видно числами.
dev-check: dev-bin client-import
	$(call with_server,$(GODOT) --headless --path $(CLIENT) --script res://tools/check.gd -- --server=$(SERVER_URL) --region=$(REGION))

## dev-shot — сервер, снимок, и оба гаснут. Окно настоящее (иначе нечего
## снимать), но уведено за экран.
dev-shot: dev-bin client-import
	@mkdir -p $(dir $(SHOT))
	$(call with_server,$(GODOT) --path $(CLIENT) --position -9000$(COMMA)-9000 --resolution 1600x900 -- --server=$(SERVER_URL) --region=$(REGION) --shot=$(SHOT) --view=$(VIEW) --quit-when-done)
	@echo "снимок: $(SHOT)"

## serve — только сервер. Держит порт, поэтому в переднем плане.
## go.mod лежит в server/, а не в корне: без cd команда не собирается вовсе.
serve:
	cd server && $(GO) run ./cmd/clearahead -db $(DB) -addr $(SERVER_ADDR)

test-go:
	cd server && $(GO) build ./... && $(GO) vet ./... && $(GO) test ./...

## test — серверные проверки. Клиентские сюда НЕ включены нарочно: им нужен
## поднятый сервер, а test обязан идти на голой машине. Зовите client-check
## отдельно, когда сервер работает.
test: test-go

build:
	cd server && $(GO) build ./...

fmt:
	cd server && gofmt -w .

## client-import — пересобрать кэш импорта Godot, если его нет.
##
## Кэш производный, но НЕ ОДНОРАЗОВЫЙ, и это куплено ошибкой 2026-08-12: в
## .godot/global_script_class_cache.cfg живёт таблица `class_name` → скрипт, а
## `--headless --script` её не строит — он её ЧИТАЕТ. Без неё разбор check.gd
## падает двумя десятками «Identifier "TrackBuild" not declared», и выглядит это
## как поломка клиента, а не как отсутствие кэша.
##
## Отсюда же следует, что на СВЕЖЕМ КЛОНЕ (где .godot/ нет по .gitignore) любая
## цель client* падала бы тем же способом. Условие — на файле таблицы, а не на
## каталоге: каталог создаёт и пустой запуск.
## Условие — СВЕЖЕСТЬ, а не наличие, и это вторая половина той же ошибки.
## Первая редакция проверяла `test -f`: кэш есть — значит годен. Он не годен,
## если после его сборки появился скрипт с новым `class_name`: имя в таблицу не
## попало, и вызов падает «Nonexistent function» — то есть выглядит как ошибка
## в коде, которого не касались. Поймано в тот же день на forest.gd.
##
## find -newer вместо сравнения времён руками: он и обходит подкаталоги, и не
## требует stat-разбора, а -quit останавливает обход на первом же свежем файле.
client-import:
	@if [ ! -f $(CLIENT)/.godot/global_script_class_cache.cfg ] || \
		[ -n "$$(find $(CLIENT)/scripts $(CLIENT)/tools -name '*.gd' \
			-newer $(CLIENT)/.godot/global_script_class_cache.cfg -print -quit 2>/dev/null)" ]; then \
		$(GODOT) --headless --path $(CLIENT) --import >/dev/null 2>&1 || true; \
	fi

## client — клиент окном. Сервер должен быть уже поднят (make serve рядом):
## клиент ничего не подставляет вместо ответа и покажет отказ красным.
client: client-import
	$(GODOT) --path $(CLIENT) -- --server=$(SERVER_URL) --region=$(REGION)

## client-check — всё, что не пиксели. Окна нет вовсе.
client-check: client-import
	$(GODOT) --headless --path $(CLIENT) --script res://tools/check.gd -- \
		--server=$(SERVER_URL) --region=$(REGION)

## client-shot — единственное доказательство, что нарисовано, а не «не упало».
## Окно настоящее (без него нечего снимать), но уведено за экран.
client-shot: client-import
	@mkdir -p $(dir $(SHOT))
	$(GODOT) --path $(CLIENT) --position -9000,-9000 --resolution 1600x900 -- \
		--server=$(SERVER_URL) --region=$(REGION) --shot=$(SHOT) --view=$(VIEW) \
		--quit-when-done
	@echo "снимок: $(SHOT)"
