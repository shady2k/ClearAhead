# Инженерный профиль валидации — план реализации

> **Для агентов-исполнителей:** ОБЯЗАТЕЛЬНЫЙ САБ-СКИЛЛ: используйте
> beads-superpowers:subagent-driven-development или
> beads-superpowers:executing-plans. Каждая Task становится бидой
> (`bd create -t task --parent <epic-id>`).

**Goal:** Сделать невозможной карту с физически неисполнимой геометрией —
минимальный радиус, предельный уклон, междупутье, — с версионированным профилем
норм и корпусом красных карт, доказывающим каждое правило.

**Архитектура:** Профиль — набор норм с целым номером версии, живущий в коде
(`map-format-design.md` §10.4). Валидатор состоит из модулей; прогон один,
вердикт один, но сообщение называет модуль и объект. Каждое правило доказывается
красной картой, падающей по своей причине.

**Стек:** Go, стандартная библиотека. Тесты — `go test`.

## Глобальные ограничения

- Валидатор **отказывает, а не чинит** (§10).
- Карта — недоверенный вход; ограничения редактора это удобство, не гарантия:
  «редактор предупреждает, **решает сервер**» (§10.4).
- Числа первой версии профиля **заведомо предварительные** и так помечены;
  источник норм назовёт владелец позже.
- Все файлы этого плана — только `server/internal/mapfmt/**`. Дерево
  `server/internal/track/**`, `httpapi/**`, `contract/**`, `client/**`
  принадлежит плану контракта отрисовки; пересекаться нельзя.
- Не коммитить, не пушить, не трогать `bd` — это делает координатор.

---

### Task 1: Профиль как структура норм с номером версии

**Файлы:**
- Создать: `server/internal/mapfmt/profile.go`
- Тест: `server/internal/mapfmt/profile_test.go`

**Интерфейсы:**
- Produces: `type Profile struct`, `func DefaultProfile() Profile`,
  `const ProfileVersion = 1`.

**Критерии приёмки:**
- Профиль имеет целый номер версии, доступный как константа.
- Каждое поле снабжено комментарием «предварительное значение, источник норм не
  назначен».
- `DefaultProfile()` возвращает профиль версии `ProfileVersion`.

- [ ] **Шаг 1: написать падающий тест**

```go
func TestDefaultProfileHasVersion(t *testing.T) {
	p := DefaultProfile()
	if p.Version != ProfileVersion {
		t.Fatalf("версия профиля %d, ожидали %d", p.Version, ProfileVersion)
	}
	if p.MinRadiusM <= 0 || p.MaxGrade <= 0 || p.MinTrackSpacingM <= 0 {
		t.Fatalf("нормы профиля должны быть положительными: %+v", p)
	}
}
```

- [ ] **Шаг 2: прогнать, убедиться что красный**

`cd server && go test ./internal/mapfmt/ -run TestDefaultProfileHasVersion -v`
Ожидается: FAIL, `undefined: DefaultProfile`.

- [ ] **Шаг 3: минимальная реализация**

```go
// Package-level: профиль инженерных норм. Спека §10.4: держится в коде и
// версионируется целым номером; манифест ревизии записывает, каким профилем
// карта проверена.
//
// ВНИМАНИЕ: числа ниже ПРЕДВАРИТЕЛЬНЫЕ. Источник норм не назначен, и до его
// появления профиль отвергает только заведомо невозможное. Менять числа —
// значит поднимать ProfileVersion, иначе одна карта будет приниматься одной
// сборкой и отвергаться другой, а причину не увидеть.
const ProfileVersion = 1

type Profile struct {
	Version          int
	MinRadiusM       float64 // минимальный радиус кривой в плане
	MaxGrade         float64 // предельный уклон, доля (0.030 = 30 промилле)
	MinTrackSpacingM float64 // минимальное междупутье вне горловин
}

func DefaultProfile() Profile {
	return Profile{
		Version:          ProfileVersion,
		MinRadiusM:       180.0,
		MaxGrade:         0.030,
		MinTrackSpacingM: 4.1,
	}
}
```

- [ ] **Шаг 4: прогнать, убедиться что зелёный**

`cd server && go test ./internal/mapfmt/ -run TestDefaultProfileHasVersion -v`
Ожидается: PASS.

---

### Task 2: Минимальный радиус — правило и красная карта

**Файлы:**
- Изменить: `server/internal/mapfmt/profile.go`
- Изменить: `server/internal/mapfmt/validate.go` (вызов из `Validate`)
- Создать: `server/internal/mapfmt/testdata/red/radius_below_min.json`
- Тест: `server/internal/mapfmt/profile_test.go`

**Интерфейсы:**
- Consumes: `Profile` из Task 1.
- Produces: `func (m *Map) validateProfile(p Profile) error`.

**Критерии приёмки:**
- Карта с дугой радиуса ниже `MinRadiusM` отвергается.
- Текст отказа называет модуль «нормы», идентификатор элемента, фактический и
  минимальный радиус.
- ST_A (`server/maps/st_a.json`) проходит: её дуги радиусом 300 м законны.

- [ ] **Шаг 1: написать падающий тест**

```go
func TestProfileRejectsTightRadius(t *testing.T) {
	m := loadTestMap(t, "testdata/red/radius_below_min.json")
	err := Validate(m)
	if err == nil {
		t.Fatal("карта с радиусом ниже минимума обязана быть отвергнута")
	}
	if !strings.Contains(err.Error(), "нормы:") {
		t.Fatalf("отказ обязан называть модуль «нормы», получено: %v", err)
	}
	if !strings.Contains(err.Error(), "радиус") {
		t.Fatalf("отказ обязан называть причину, получено: %v", err)
	}
}
```

- [ ] **Шаг 2: прогнать, убедиться что красный по нужной причине**

`cd server && go test ./internal/mapfmt/ -run TestProfileRejectsTightRadius -v`
Ожидается: FAIL — карта проходит валидацию, потому что правила ещё нет.

- [ ] **Шаг 3: реализация**

```go
// validateProfile — модуль «нормы». Отвечает на вопрос «можно ли это
// построить», а не «связно ли это».
func (m *Map) validateProfile(p Profile) error {
	for id, a := range m.Geometry.Edges {
		for i, prim := range a.Horizontal {
			if prim.Kind != "arc" {
				continue
			}
			if prim.Radius < p.MinRadiusM {
				return fmt.Errorf(
					"нормы: %s: примитив %d: радиус %.1f м меньше минимального %.1f м (профиль %d)",
					id, i, prim.Radius, p.MinRadiusM, p.Version)
			}
		}
	}
	return nil
}
```

Вызвать из `Validate` последним, после топологических проверок: сперва
структура, потом нормы — иначе на сломанной топологии отказ придёт не по той
причине.

- [ ] **Шаг 4: прогнать оба теста и ST_A**

```
cd server && go test ./internal/mapfmt/ -run 'TestProfile' -v
cd server && go test -count=1 ./internal/mapfmt/ ./internal/track/
```
Ожидается: PASS, ST_A по-прежнему валидна.

---

### Task 3: Предельный уклон — правило и красная карта

**Файлы:**
- Изменить: `server/internal/mapfmt/profile.go`
- Создать: `server/internal/mapfmt/testdata/red/grade_above_max.json`
- Тест: `server/internal/mapfmt/profile_test.go`

**Интерфейсы:**
- Consumes: `Profile`, `validateProfile` из Task 2.

**Критерии приёмки:**
- Карта с уклоном выше `MaxGrade` отвергается сообщением модуля «нормы».
- В отказе названы элемент, фактический уклон в промилле и предел.
- ST_A проходит: она плоская.

- [ ] **Шаг 1: написать падающий тест**

```go
func TestProfileRejectsSteepGrade(t *testing.T) {
	m := loadTestMap(t, "testdata/red/grade_above_max.json")
	err := Validate(m)
	if err == nil {
		t.Fatal("карта с уклоном выше предела обязана быть отвергнута")
	}
	if !strings.Contains(err.Error(), "нормы:") || !strings.Contains(err.Error(), "уклон") {
		t.Fatalf("отказ обязан называть модуль и причину, получено: %v", err)
	}
}
```

- [ ] **Шаг 2: прогнать, убедиться что красный**

`cd server && go test ./internal/mapfmt/ -run TestProfileRejectsSteepGrade -v`

- [ ] **Шаг 3: дописать правило в `validateProfile`**

Пройти вертикальные выравнивания каждого ребра, сравнить `grade` по модулю с
`p.MaxGrade`; в тексте отказа перевести в промилле умножением на 1000.

- [ ] **Шаг 4: прогнать, убедиться что зелёный**

`cd server && go test -count=1 ./internal/mapfmt/`

---

### Task 4: Каждое правило доказано своей красной картой

**Файлы:**
- Создать: `server/internal/mapfmt/testdata/red/` — по файлу на правило
- Тест: `server/internal/mapfmt/red_corpus_test.go`

**Интерфейсы:**
- Consumes: `Validate`.
- Produces: табличный тест, обходящий каталог `testdata/red/`.

**Критерии приёмки:**
- Каждый файл в `testdata/red/` **обязан** быть отвергнут.
- Тест сверяет **причину**, а не только факт отказа: рядом с картой лежит файл
  `<имя>.want` с подстрокой, которая обязана встретиться в тексте ошибки.
- Удаление любого правила из валидатора делает красным ровно свой тест.
- Каталог покрывает как минимум: радиус ниже нормы, уклон выше нормы,
  пересечение осей без устройства, разрыв связности, висящее ребро без упора,
  дублирующийся ключ JSON, не конечное число, ссылка на несуществующий элемент.

- [ ] **Шаг 1: написать тест-обходчик**

```go
func TestRedCorpus(t *testing.T) {
	entries, err := os.ReadDir("testdata/red")
	if err != nil {
		t.Fatalf("каталог красных карт не читается: %v", err)
	}
	seen := 0
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		seen++
		t.Run(e.Name(), func(t *testing.T) {
			want, err := os.ReadFile(filepath.Join("testdata/red", strings.TrimSuffix(e.Name(), ".json")+".want"))
			if err != nil {
				t.Fatalf("нет файла .want с ожидаемой причиной отказа: %v", err)
			}
			m, derr := decodeFile(t, filepath.Join("testdata/red", e.Name()))
			var got string
			if derr != nil {
				got = derr.Error()
			} else if verr := Validate(m); verr != nil {
				got = verr.Error()
			} else {
				t.Fatal("красная карта прошла валидацию")
			}
			if !strings.Contains(got, strings.TrimSpace(string(want))) {
				t.Fatalf("отказ по не той причине:\n получено: %s\n ожидали подстроку: %s", got, want)
			}
		})
	}
	if seen == 0 {
		t.Fatal("корпус красных карт пуст — правило считается недоказанным")
	}
}
```

- [ ] **Шаг 2: прогнать, убедиться что красный**

`cd server && go test ./internal/mapfmt/ -run TestRedCorpus -v`
Ожидается: FAIL — каталога ещё нет.

- [ ] **Шаг 3: завести карты и файлы причин**

По одной минимальной карте на правило. Каждая ломает **ровно одно** правило;
остальное в ней законно, иначе отказ придёт от соседа.

- [ ] **Шаг 4: доказать чувствительность**

Временно закомментировать проверку минимального радиуса, прогнать корпус,
убедиться, что покраснел **ровно** `radius_below_min`, вернуть проверку.
Записать результат в докладе числом: сколько карт, сколько правил, какое
покраснело при снятии какого правила.

---

### Task 5: Марка крестовины перестаёт быть обязательной

**Файлы:**
- Изменить: `server/internal/mapfmt/validate.go:218`
- Тест: `server/internal/mapfmt/validate_test.go`

**Критерии приёмки:**
- Карта со стрелкой без `frog` проходит валидацию.
- ST_A проходит.
- В коде остаётся комментарий, почему марка необязательна.

- [ ] **Шаг 1: написать падающий тест**

```go
func TestTurnoutWithoutFrogIsValid(t *testing.T) {
	m := loadTestMap(t, "testdata/turnout_no_frog.json")
	if err := Validate(m); err != nil {
		t.Fatalf("марка крестовины необязательна (§8), получен отказ: %v", err)
	}
}
```

- [ ] **Шаг 2: прогнать, убедиться что красный**

Ожидается: FAIL с текстом про пустую марку крестовины.

- [ ] **Шаг 3: удалить проверку**

Убрать блок `if t.Frog == ""` и поставить на его место комментарий:

```go
// Марка крестовины НЕ проверяется намеренно: §8 объявляет её происхождением,
// а не ограничением, ради импорта реальных станций с произвольными радиусами.
// Клиент строит крестовину из геометрии, а не из марки.
```

- [ ] **Шаг 4: прогнать весь пакет**

`cd server && go test -count=1 ./internal/mapfmt/ ./internal/track/ ./internal/httpapi/`

---

## Что этот план сознательно НЕ делает

- **Междупутье** (`MinTrackSpacingM`) заведено в структуре, но правило не
  написано: у горловин пути законно сходятся, и проверка без исключения для
  горловин даст ложные срабатывания. Отдельная задача, требует определения
  «вне горловины».
- **Запись номера профиля в манифест ревизии** — файл `track/hash.go`
  принадлежит плану контракта отрисовки. Делается после слияния обеих волн.
- **Модульные сообщения** заведены соглашением о префиксе («нормы: …»), а не
  типом. Полноценный реестр модулей — когда модулей станет больше двух.
