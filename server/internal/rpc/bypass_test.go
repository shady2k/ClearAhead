package rpc_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// probeDir — временный пакет внутри модуля. Внутри, потому что пакеты
// internal/* недоступны другим модулям: обход барьера достижим только из этого
// же модуля, а значит и проверять его надо изнутри.
const probeDir = "bypassprobe"

// TestEmbeddingBypassDoesNotCompile — сторож на барьер валидации.
//
// Владелец проекта потребовал, чтобы вызвать метод без валидации было
// АРХИТЕКТУРНО невозможно. Первая редакция плана это требование не выполняла:
// sealed() наследуется промоушеном при встраивании, а Parse экспортирован и
// потому переопределяем, — вдвоём они складывались в обход, и невалидный вход
// доходил до обработчика. Дыру закрывает native() T: сигнатура упоминает сам
// тип, поэтому унаследовать её нельзя, а объявить свою нельзя из-за
// неэкспортированного имени.
//
// Обычным тестом это не проверить: код обхода не должен собираться вообще.
// Поэтому пробник лежит в testdata (куда go tool не заглядывает), на время
// теста разворачивается в пакет и собирается с ТРЕБОВАНИЕМ ошибки.
//
// Если тест упал — это не «поправить тест», это регрессия защиты.
func TestEmbeddingBypassDoesNotCompile(t *testing.T) {
	probe, err := os.ReadFile(filepath.Join("testdata", "bypass_probe.go.txt"))
	if err != nil {
		t.Fatalf("пробник не читается: %v", err)
	}
	if err := os.MkdirAll(probeDir, 0o755); err != nil {
		t.Fatalf("создание пакета пробника: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(probeDir) })
	if err := os.WriteFile(filepath.Join(probeDir, "probe.go"), probe, 0o644); err != nil {
		t.Fatalf("запись пробника: %v", err)
	}

	cmd := exec.Command("go", "build", "./internal/rpc/"+probeDir+"/")
	cmd.Dir = filepath.Join("..", "..")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("РЕГРЕССИЯ БАРЬЕРА: обход встраиванием собрался. Чужой пакет может " +
			"встроить protocol-запрос, переопределить Parse заглушкой и получить " +
			"невалидный вход в обработчике.")
	}
	if !strings.Contains(string(out), "native") {
		t.Fatalf("сборка упала, но не по причине барьера — проверьте пробник.\n%s", out)
	}
}
