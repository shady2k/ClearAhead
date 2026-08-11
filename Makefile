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

# ОКНО НА ВРЕМЯ СЪЁМКИ И ЗОНДОВ. Godot рисует только в настоящее окно, поэтому
# снимок и зонды его создают — и на маке оно всплывает поверх работы, отбирая
# фокус. Увести его за пределы экрана дешевле, чем терпеть: снимок читается из
# текстуры вьюпорта, а не с экрана, и от положения окна не зависит (проверено:
# кадр байт в байт тот же).
#
# Пусто — окно там, где его поставит система: make fpv-shot WINDOW=
# Живые цели (world, fpv, client) окно НЕ прячут: их смысл в том, чтобы смотреть.
WINDOW ?= --position -6000,-6000

.PHONY: help game game-shot schema shell-probe world world-shot fpv fpv-shot walk-probe terrain-probe camera-probe glb-probe perf-probe tile-dump sketch3d sketch3d-shot dev dev-bin dev-shot serve client shot shot-offline contract test test-go test-client vnc build fmt clean

help:
	@echo 'ClearAhead — цели разработки'
	@echo
	@echo '  make game       ИГРА: меню, выбор роли, мир (эталон вместо сервера)'
	@echo '  make game-shot  снимок оболочки в $(SHOT): ROLE=, STEP='
	@echo '  make dev        СЕРВЕР И ИГРА разом — обычный способ запустить всё'
	@echo '  make dev-shot   то же, но снимок вместо показа, и оба гасятся'
	@echo '  make serve      только сервер на $(SERVER_ADDR) (передний план, Ctrl-C — стоп)'
	@echo '  make client     только игра на $(DISPLAY_ID); смотреть: $(VNC_URL)'
	@echo '  make schema     ТОЛЬКО 2D-схема (оснастка контракта, без оболочки)'
	@echo '  make shot       снимок 2D-схемы в $(SHOT) — надёжная проверка контракта'
	@echo '  make contract   контрактный тест клиента против эталона (без сервера)'
	@echo '  make test       всё: go test ./... + контрактный тест клиента'
	@echo '  make sketch3d   ЭСКИЗ: тот же путь мешами в 3D, орто-камера'
	@echo '  make world      СПАЙК МИРА: рельеф, лес, река, посёлок (нужен Forward+)'
	@echo '  make world-shot снимок спайка мира в $(SHOT)'
	@echo '  make fpv        СПАЙК МАШИНИСТА: человек в мире, вид с высоты его глаз'
	@echo '  make fpv-shot   снимок с высоты глаз в $(SHOT)'
	@echo '  make shell-probe действуют ли переходы оболочки: Esc в паузу и обратно, Tab в схему'
	@echo '  make walk-probe ходит ли человек: стоит, идёт, вертит головой, всходит на путь'
	@echo '  make terrain-probe  статистика поля высот спайка мира (без окна)'
	@echo '  make camera-probe   действует ли управление камерой (метры сдвига)'
	@echo '  make glb-probe      что внутри чужой .glb: узлы и габариты в метрах'
	@echo '  make perf-probe     почему тормозит: кадр и пересадка травы порознь'
	@echo '  make tile-dump      процедурные тайлы спайка мира в PNG (без окна)'
	@echo '  make vnc        напомнить, куда смотреть и где пароль'
	@echo
	@echo 'Переменные: MAP, SERVER_ADDR, SHOT, FOCUS="x,y", ZOOM=<пикс/м>, DISPLAY_ID'
	@echo
	@echo 'Примеры:'
	@echo '  make serve MAP=server/internal/mapfmt/testdata/fixture_station.json'
	@echo '  make shot FOCUS=400,0 ZOOM=30      # горловина крупно'

## game — ИГРА ЦЕЛИКОМ: меню, выбор роли, мир. Главная сцена проекта, поэтому
## запускается без указания сцены.
##
## Сервер не нужен: --offline берёт путь из эталона contract/. С сервером игра
## запускается целью dev (она поднимает и его), либо руками через --server.
##
## РОЛЬ можно взять сразу, минуя меню — это отладочный вход, а не игровой:
##   make game                    меню
##   make game ROLE=driver        сразу машинистом: человек в мире
##   make game ROLE=dsp           сразу ДСП: мир сверху, Tab — схема
##   make game ROLE=builder       сразу строителем
##
## УПРАВЛЕНИЕ ПЕЧАТАЕТСЯ В HUD, а не здесь: искать его в Makefile игроку негде.
ROLE ?=
STEP ?=
GAME_ARGS := --offline
ifneq ($(strip $(ROLE)),)
GAME_ARGS += --role $(ROLE)
endif
APP_ARGS := $(GAME_ARGS)
ifneq ($(strip $(STEP)),)
APP_ARGS += --step $(STEP)
endif

game:
	DISPLAY=$(DISPLAY_ID) $(GODOT) --path client -- $(GAME_ARGS)

## game-shot — снимок ОБОЛОЧКИ. Проверяет ровно то, чего не проверяет ни один
## снимок мира: экраны, переходы между ними и что роль действительно строит свой
## мир. Кнопка, которая не нарисовалась, ошибки в лог не пишет.
##   make game-shot                        главное меню
##   make game-shot STEP=map               экран выбора карты
##   make game-shot STEP=role              экран выбора роли
##   make game-shot ROLE=dsp STEP=chunks   отладочный слой чанков (F3)
##   make game-shot ROLE=driver            вид с высоты глаз машиниста
##   make game-shot ROLE=dsp STEP=schema   ДСП, переключённый в плоскую схему
##   make game-shot ROLE=builder STEP=pause  пауза поверх мира
game-shot:
	DISPLAY=$(DISPLAY_ID) $(GODOT) $(WINDOW) --path client \
		--script res://tools/app_shot.gd -- --shot $(SHOT) --frames $(FRAMES) $(APP_ARGS)
	@echo "снимок: $(SHOT) — посмотрите на него, логу верить нельзя"

## schema — ТОЛЬКО плоская схема, без оболочки и без ролей. Это оснастка, а не
## игра: ею проверяется контракт отрисовки. В самой игре та же схема живёт
## внутри роли ДСП (Tab), и код у них общий — scenes/main.tscn и app.tscn
## ссылаются на одни и те же world.gd, camera.gd, debug.gd.
schema:
	@echo 'Схема на $(DISPLAY_ID). Смотреть: $(VNC_URL)'
	DISPLAY=$(DISPLAY_ID) $(GODOT) --path client res://scenes/main.tscn -- --server=$(SERVER_URL)

## dev — сервер И игра одной командой. Обычный способ запустить всё.
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
## СНИМАЕТ ПЛОСКУЮ СХЕМУ, а не оболочку: проверяется провод от сервера до
## отрисовки, и лишние экраны здесь только мешают. Снимок игры — game-shot.
dev-shot: dev-bin
	@$(DEV_BIN) -map $(MAP) -addr $(SERVER_ADDR) & \
	srv=$$!; \
	trap 'kill $$srv 2>/dev/null || true' EXIT INT TERM; \
	for i in $$(seq 1 50); do \
		(exec 3<>/dev/tcp/127.0.0.1/$(SERVER_PORT)) 2>/dev/null && break; \
		kill -0 $$srv 2>/dev/null || { echo 'сервер не поднялся'; exit 1; }; \
		sleep 0.1; \
	done; \
	DISPLAY=$(DISPLAY_ID) $(GODOT) $(WINDOW) --path client --script tests/smoke_screenshot.gd -- \
		--server=$(SERVER_URL) $(SHOT_ARGS)
	@echo "снимок: $(SHOT) — посмотрите на него, логу верить нельзя"

# Всегда пересобираем: файловая цель кэшировалась бы, и dev молча запускал бы
# вчерашний сервер. Go-сборка инкрементальна, это дёшево.
dev-bin:
	@cd server && $(GO) build -o $(DEV_BIN) ./cmd/clearahead

## serve — только сервер. Держит порт, поэтому в переднем плане.
serve:
	$(GO) run ./server/cmd/clearahead -map $(MAP) -addr $(SERVER_ADDR)

## client — ИГРА против уже поднятого сервера. Открывается меню, путь приходит с
## сервера, а не из эталона (этим и отличается от цели game).
## Формат аргумента именно --server=URL: app.gd разбирает его через begins_with("--server=").
client:
	@echo 'Игра на $(DISPLAY_ID). Смотреть: $(VNC_URL)'
	DISPLAY=$(DISPLAY_ID) $(GODOT) --path client -- --server=$(SERVER_URL)

## shot — снимок ПЛОСКОЙ СХЕМЫ (scenes/main.tscn), а не игры: снимок оболочки —
## это game-shot. Сервер нужен; без него клиент штатно покажет отказ, и это
## будет видно на снимке, а не в логе.
## Аргументы снимка разделены ПРОБЕЛОМ: smoke_screenshot.gd берёт их через
## _arg_value(name) = args[i+1], а не через "=".
shot:
	DISPLAY=$(DISPLAY_ID) $(GODOT) $(WINDOW) --path client --script tests/smoke_screenshot.gd -- \
		--server=$(SERVER_URL) $(SHOT_ARGS)
	@echo "снимок: $(SHOT) — посмотрите на него, логу верить нельзя"

## shot-offline — снимок из файла эталона, без сервера. Полезно, когда
## проверяется отрисовка, а не сеть.
shot-offline:
	DISPLAY=$(DISPLAY_ID) $(GODOT) $(WINDOW) --path client --script tests/smoke_screenshot.gd -- \
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
	DISPLAY=$(DISPLAY_ID) $(GODOT) $(WINDOW) --path client --script res://scripts/sketch3d_shot.gd -- \
		--shot $(SHOT) $(SK_ARGS)
	@echo "снимок: $(SHOT)"

## world / world-shot — СПАЙК МИРА: тот же путь, но с рельефом, лесом, рекой и
## посёлком (client/scripts/spike_world.gd). Всё, кроме пути, выдумано клиентом.
##
## Нужен Forward+: спайк рассчитывает на тени, дымку и SSAO, которых в
## gl_compatibility нет. Метод передаётся флагом запуска, project.godot не
## трогается — на llvmpipe-машине Forward+ не поднимется, там RENDER=gl_compatibility.
##   make world                                живой просмотр
##   make world-shot                           общий план в $(SHOT)
##   make world-shot SIZE=150 ELEV=26 FOCUS=170,0   станция крупно
##
## УПРАВЛЕНИЕ В `make world` (оно же печатается строкой VIEW3D при запуске):
##   ЛКМ                      орбита вокруг точки взгляда
##   WASD / стрелки           панорама — УВОДИТ САМУ ТОЧКУ ВЗГЛЯДА; Shift быстрее
##   Shift+ЛКМ, СКМ, ПКМ      она же мышью
##   колесо, +/-              зум
##   F                        вернуть камеру на локомотив
##   L / P                    убрать локомотив / орто-перспектива
## Панорама только на средней и правой кнопке была нерабочей на трекпаде мака:
## оставалась одна орбита, и вид выглядел привязанным к одной точке.
##
## ЛОКОМОТИВ. У платформы стоит двухсекционный ВЛ80 из client/assets/vl80.glb
## (CC-BY-4.0, атрибуция там же). Файла нет — спайк рисует свой процедурный ЧМЭ3,
## сцена поднимается в обоих случаях. На общем плане машину не видно и не должно
## быть: 33 м с четырёхсот — это горстка пикселей. Кадры, где она читается
## (в перспективе SIZE — это ДИСТАНЦИЯ ДО ФОКУСА, а не ширина кадра):
##   make world-shot SIZE=45 ELEV=14 AZ=200 FOCUS=220,-0.2   обе секции у платформы
##   make world-shot SIZE=14 ELEV=7  AZ=55  FOCUS=232,-1.2   тележки на рельсах
##   make world-shot SIZE=26 ELEV=10 AZ=155 FOCUS=232,-0.5   крыша и токоприёмники
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
	DISPLAY=$(DISPLAY_ID) $(GODOT) --rendering-method $(RENDER) $(WINDOW) --path client \
		--script res://scripts/spike_shot.gd -- --scene $(WORLD_SCENE) \
		--shot $(SHOT) --persp 1 $(WORLD_ARGS)
	@echo "снимок: $(SHOT)"

## fpv / fpv-shot — СПАЙК МАШИНИСТА: тот же мир, но в нём стоит ЧЕЛОВЕК, и камера
## сидит у него в голове (client/scripts/spike_fpv.gd). Наследует спайк мира
## целиком и добавляет к нему твердь под ногами, фигуру и три вида.
##
## ЗАЧЕМ ОТДЕЛЬНАЯ ЦЕЛЬ, А НЕ ФЛАГ У world. Вид с высоты глаз требует того, чего
## общему плану не нужно вовсе: коллизий на каждом меше (0.9 с на старте), тени
## на 180 м вместо 900 и перспективы вместо ортографии. Мир этим платить не обязан.
##
## УПРАВЛЕНИЕ В `make fpv` (оно же печатается строкой FPV при запуске):
##   V                        вид: обзор -> от первого лица -> от третьего
##   мышь                     взгляд (курсор захватывается в видах от лица)
##   WASD / стрелки           идти, Shift — бежать, пробел — прыжок
##   F                        вернуться к локомотиву
##   Esc                      отпустить курсор и выйти в обзор
##
## СНИМОК ЗАДАЁТСЯ ПОЛОЖЕНИЕМ ЧЕЛОВЕКА, А НЕ КАМЕРЫ: камеру ставит его голова.
## Положение — в координатах пути (U вдоль E_MAIN, LAT от оси), поэтому оно
## переживает правку фикстуры. LAT отрицательная — сторона платформы; YAW в
## градусах от направления пути, плюс — влево; отметку под ногами ищет луч, так
## что LAT можно ставить и на путь, и на платформу.
##   make fpv-shot                                у платформы, лицом вдоль пути
##   make fpv-shot MODE=2                         то же от третьего лица: видно самого
##   make fpv-shot U=150 LAT=0 YAW=180            стоя на пути, вид вдоль перегона
##   make fpv-shot U=64 LAT=-3.2 YAW=-40          с платформы на ВЛ80
##   make fpv-shot U=72 LAT=-3.2 YAW=-80 PITCH=4  он же в упор, борт во весь кадр
U     ?=
LAT   ?=
YAW   ?=
PITCH ?=
MODE  ?=
FPV_SCENE := res://scenes/spike_fpv.tscn
FPV_ARGS :=
ifneq ($(strip $(U)),)
FPV_ARGS += --u $(U)
endif
ifneq ($(strip $(LAT)),)
FPV_ARGS += --lat $(LAT)
endif
ifneq ($(strip $(YAW)),)
FPV_ARGS += --yaw $(YAW)
endif
ifneq ($(strip $(PITCH)),)
FPV_ARGS += --pitch $(PITCH)
endif
ifneq ($(strip $(MODE)),)
FPV_ARGS += --mode $(MODE)
endif

fpv:
	DISPLAY=$(DISPLAY_ID) $(GODOT) --rendering-method $(RENDER) --path client $(FPV_SCENE)

fpv-shot:
	DISPLAY=$(DISPLAY_ID) $(GODOT) --rendering-method $(RENDER) $(WINDOW) --path client \
		--script res://scripts/fpv_shot.gd -- --shot $(SHOT) $(FPV_ARGS)
	@echo "снимок: $(SHOT)"

## shell-probe — ДЕЙСТВУЮТ ЛИ ПЕРЕХОДЫ ОБОЛОЧКИ. Подаёт Esc и Tab тем же путём,
## каким приходит настоящее нажатие, и печатает состояние после каждого.
##
## Снимок паузы доказывает, что меню НАРИСОВАНО, и молчит о том, можно ли из неё
## выйти: кадр в обоих случаях правильный, ошибок в логе нет. Так найден дефект
## 2026-08-10 — узел App без PROCESS_MODE_ALWAYS перестаёт слышать ввод вместе с
## миром, и Esc, которым в паузу вошли, из неё не выводит. Тот же класс, что зум
## только на колесе мыши и панорама только на средней кнопке (см. camera-probe).
##
## --headless НЕ ГОДИТСЯ: оболочка строит мир, а ему нужен вьюпорт.
shell-probe:
	DISPLAY=$(DISPLAY_ID) $(GODOT) $(WINDOW) --path client \
		--script res://tools/shell_probe.gd -- --offline --role dsp

## walk-probe — ХОДИТ ЛИ ЧЕЛОВЕК. Печатает числа там, где снимок с высоты глаз
## по своему устройству ничего не показывает: утоп он по колено или парит на
## треть метра, из его же глаз не видно НИЧЕГО — кадр в обоих случаях правильный.
## Меряет четыре вещи: стоит ли на тверди (зазор подошвы в метрах), идёт ли
## (м/с против заявленных), вертит ли головой (градусы на движение мыши) и
## всходит ли на путь через шпалу и рельс (пройдено и подъём в метрах).
##   make walk-probe            коротко
##   make walk-probe TRACE=1    со следом пути: где именно он встал
TRACE ?=
WALK_ARGS :=
ifneq ($(strip $(TRACE)),)
WALK_ARGS += --trace
endif
walk-probe:
	DISPLAY=$(DISPLAY_ID) $(GODOT) --rendering-method $(RENDER) $(WINDOW) --path client \
		--script res://tools/walk_probe.gd -- $(WALK_ARGS)

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

## camera-probe — ДЕЙСТВУЕТ ЛИ УПРАВЛЕНИЕ КАМЕРОЙ. Подаёт клавиши тем же путём,
## каким приходит настоящее нажатие, и печатает, НА СКОЛЬКО МЕТРОВ уехала точка
## взгляда. Не косметика: класс бага «жест есть в коде, но на этом вводе его не
## изобразить» случался дважды — зум жил только на колесе мыши (на трекпаде мака
## его не было), панорама только на средней и правой кнопке (камера оставалась
## привязана к одной точке). Ни снимок, ни лог этого не показывают: кадр
## правильный, ошибок нет, просто руками так сделать нельзя.
##   make camera-probe                             базовый спайк
##   make camera-probe SCENE=res://scenes/spike_world.tscn
## --headless НЕ ГОДИТСЯ: сцене нужен вьюпорт (см. шапку файла).
SCENE ?= res://scenes/spike_relief.tscn
camera-probe:
	DISPLAY=$(DISPLAY_ID) $(GODOT) --rendering-method $(RENDER) $(WINDOW) --path client \
		--script res://tools/camera_probe.gd -- --scene $(SCENE)

## glb-probe — ЧТО ВНУТРИ ЧУЖОЙ МОДЕЛИ: дерево узлов и габарит каждой детали в
## метрах. Про присланный .glb неизвестно ничего — ни где ноль, ни какая ось
## вдоль машины, ни в метрах ли он. Ставить его по снимку значит подгонять
## вслепую: ошибка масштаба там неотличима от ошибки высоты.
##
## Спрашивать надо ДВИЖОК, а не разбирать файл самому. У vl80.glb положение
## узлов задано матрицей matrix, а не translation/rotation/scale, и наивный
## разбор JSON показывает сцену, где все детали лежат в одной точке — вывод
## получается прямо противоположный правде.
##   make glb-probe                        res://assets/vl80.glb
##   make glb-probe RES=res://assets/x.glb
RES ?= res://assets/vl80.glb
glb-probe:
	$(GODOT) --headless --path client --script res://tools/glb_probe.gd -- --res $(RES)

## perf-probe — ПОЧЕМУ ТОРМОЗИТ. Мерит порознь две вещи, которые на глаз
## сливаются в одно: стоимость КАДРА (что рисуется) и стоимость ПЕРЕСАДКИ ТРАВЫ
## (что считается в главном потоке, пока кадр стоит колом). Ровный низкий фпс и
## рывок раз в несколько секунд лечатся разным, а выглядят похоже.
##
## Чем найдено 2026-08-10: зум упирался не в отрисовку и не в модель ВЛ80
## (она стоит +0.3 мс из 19.5), а в пересадку травы — 1.8 с вблизи и 3.1 с на
## отдалении, из них 98 % это обход сетки, а не загрузка в MultiMesh.
perf-probe:
	DISPLAY=$(DISPLAY_ID) $(GODOT) --rendering-method $(RENDER) $(WINDOW) --path client \
		--script res://tools/perf_probe.gd

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
