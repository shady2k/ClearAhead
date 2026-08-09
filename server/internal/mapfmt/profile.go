package mapfmt

import (
	"fmt"
	"math"
)

// Профиль инженерных норм. Спека §10.4: держится в коде и версионируется целым
// номером; манифест ревизии записывает, каким профилем карта проверена.
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

// validateProfile — модуль «нормы». Отвечает на вопрос «можно ли это
// построить», а не «связно ли это». Зовётся из Validate последним: сперва
// структура и топология, потом нормы — иначе на сломанной карте отказ придёт
// не по той причине.
//
// Междупутье (MinTrackSpacingM) здесь сознательно не проверяется: у горловин
// пути законно сходятся, и правило без исключения для горловин даст ложные
// срабатывания (отдельная задача).
func (m *Map) validateProfile(p Profile) error {
	// Обходим AllAlignments, а не Geometry.Edges: проход стрелки — адресуемый
	// линейный элемент наравне с ребром (спека §8), и самый тесный радиус
	// станции живёт именно в отклонённом проходе.
	for id, a := range m.AllAlignments() {
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
		// Уклон в файле карты — промилле (спека §6), профиль хранит предел
		// долей (0.030 = 30‰). vertical_curve доходит до EndSlopePermille —
		// проверяем и её: предел относится к профилю, а не к примитиву.
		for i, prim := range a.Vertical {
			var permille float64
			switch prim.Kind {
			case "grade":
				permille = prim.SlopePermille
			case "vertical_curve":
				permille = prim.EndSlopePermille
			default:
				continue // kind уже отвергнут validateAlignments
			}
			if math.Abs(permille) > p.MaxGrade*1000 {
				return fmt.Errorf(
					"нормы: %s: вертикаль[%d]: уклон %.0f‰ превышает предел %.0f‰ (профиль %d)",
					id, i, permille, p.MaxGrade*1000, p.Version)
			}
		}
	}
	return nil
}
