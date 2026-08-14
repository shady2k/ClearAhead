# W7-C — vegetation: отказ вместо молчания

Отчёт воркера W7-C (бида sqym.19, P2). Ветка `b1-server-half`, файлы:
`server/internal/vegetation/vegetation.go`, `server/internal/vegetation/vegetation_test.go`.
`server/internal/chunk/chunk.go` не тронут вовсе.

## Что сделано

1. **Уровень выше нулевого глотает источники** (vegetation.go:174–185). `Project`
   на уровне `!= chunk.ForestLevel` теперь сначала проверяет источники: пустые —
   законная пустота (`nil, nil`), непустые — отказ
   `vegetation: уровень 1, где леса нет (лес — только уровень 0): исходников 1 шт.`,
   с уровнем и суммарным числом по всем четырём формам.
2. **Вырожденный прямоугольник.** Проверка области стала `Min >= Max` (было
   `>`): `Min == Max` — площадь ноль — отказ «вырождена» тем же текстом, что и
   прежний `Min > Max`.
3. **NaN в границе.** Перед сравнением — явный `math.IsNaN` по всем четырём
   координатам, отказ «с NaN-границей», текст печатает прямоугольник (NaN виден
   как `NaN`).
4. **CutMask.Add вне границ.** `Add` сам проверяет адрес и отказывает
   (`vegetation: маска вырубки чанка (0,0): ячейка (64,0) вне сетки 64×64`).
   Подпись `chunk.SetForestOccupied`/`ClearForestOccupied` не менялась.

## Сигнатуры

- `vegetation.Project` — **не менял** (сосед подключает её к боевому пути).
- `chunk.*` — **не менял**.
- **`CutMask.Add` изменил: `Add(i, j int)` → `Add(i, j int) error`.** Это
  изменение API пакета vegetation, и оно осознанное: бриф ограничивает только
  подписи `Project` и `chunk`, а грепом по всему `server/internal/` подтверждено,
  что `CutMask`/`NewCutMask`/`Add`/`Contains` не имеют наружных потребителей —
  только сам пакет и его тесты. Отказ ошибкой, а не panic: в этом пакете каждый
  отказ — ошибка (`checkCell`, `Project`), panic-прецедентов в валидации нет.
  Если сосед начнёт строить маски `Add`-ом — увидит новую сигнатуру как
  ошибку компиляции, а не молчаливую потерю.

## Тесты: было 15, стало 19

Убрано: `TestProjectAboveLevelZeroIsEmpty` (утверждал старый дефект — непустые
исходники на уровне 1 принимаются).
Добавлено 5:

- `TestProjectSourcesAboveForestLevelRefused` — непустые на уровне 1 → отказ,
  текст называет уровень и число;
- `TestProjectAboveForestLevelEmptySourcesIsEmpty` — пустые на уровнях 1, 2,
  MaxLevelLimit → законная пустота;
- `TestDegenerateClearingMinEqualsMaxRefused` — `Min == Max` по каждой оси и
  точка → отказ «вырождена»;
- `TestNaNInClearingRefused` — NaN в каждой из четырёх границ → отказ, текст
  называет NaN;
- `TestCutMaskAddOutOfBoundsRefused` — адреса `-1`, `CoverCells` и т.п. → отказ;
  после отказов маска пуста.
Обновлены `TestMaskCutMatchesGraves` и `TestCutMaskAddAndContains` под новую
сигнатуру `Add`.

## Реверт-проверка (каждый новый тест падает на старом коде)

Пакет `vegetation/` не отслеживается git'ом (untracked, коммитит координатор),
поэтому «старый код» реконструирован из прочитанного до правок исходника и
проверен в два шага:

1. Новый тест-файл против старого исходника: **сборка падает** —
   `mask.Add(i, j) (no value) used as value` в четырёх местах (два обновлённых
   call-site'а и `TestCutMaskAddOutOfBoundsRefused`). Сигнатурный тест падает
   ошибкой компиляции — это и есть его «падение на сегодняшнем коде».
2. Тест-файл временно адаптирован под старую сигнатуру `Add` (рантайм-тесты
   уровня/области/NaN остались): падают по одному —
   - `TestProjectSourcesAboveForestLevelRefused`: «рубка на уровне 1 принята»;
   - `TestDegenerateClearingMinEqualsMaxRefused`: «область [10, 10] × [0, 10] принята»;
   - `TestNaNInClearingRefused`: «область с NaN принята».
   `TestProjectAboveForestLevelEmptySourcesIsEmpty` на старом коде **проходит
   осознанно**: старый код и так возвращал `nil, nil` для пустых источников —
   это пин против переусердствовавшего фикса (отказа там, где нечего терять),
   а не регрессионный тест.

После реверта всё восстановлено из контрольных копий (`/tmp/vegetation_fixed.go`,
`/tmp/vegetation_test_new.go`) — `diff` байт в байт.

## Гейт

```
go build ./internal/vegetation/ ./internal/chunk/   # ok
go vet ./internal/vegetation/ ./internal/chunk/     # ok
go test -count=1 ./internal/vegetation/ ./internal/chunk/  # ok: 19+ тестов, все PASS
gofmt -l internal/vegetation/                       # пусто
```

## Что заметил, но намеренно не тронул

- `CutMask.Contains` остался тихим bool-запросом: это чтение (в отличие от
  мутирующего `Add`), адрес вне сетки даёт `false` — то же, что
  `ForestOccupied`. Бриф требовал отказ только в `Add`.
- Длину `Bits` в `Add` не проверяю: маску создаёт `NewCutMask` (размер верен),
  а битую маску, собранную литералом, ловит `Project` («маска вырубки N байт»).
  Дублировать проверку в `Add` — два места с одними порогами.
- `±Inf` в границах области не отвергаю: бриф называл NaN, а Inf даёт не пустую
  вырубку, а всепокрывающую — другой класс. В репозитории есть готовый паттерн
  `math.IsNaN || math.IsInf` (content, geom, units, mapfmt) — если решите, что
  Inf тоже обязан отказывать, это три строки в той же проверке.
- `chunk.go` в рабочем дереве изменён (сосед) — не мой файл, не трогал.
- `terrain` использует `Project` с прежней сигнатурой — не ломается; наружных
  потребителей `CutMask.Add`/`Contains` нет (греп по `server/internal/`).
