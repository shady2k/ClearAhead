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
//
//	1 — радиус в плане, предельный уклон, междупутье
//	2 — габарит приближения платформы (контракт отрисовки, редакция 6, §7.2):
//	    величины появились вместе с ВЫСОТОЙ платформы, а до неё отличить низкую
//	    от высокой было нечем, и проверять — нечего
const ProfileVersion = 2

type Profile struct {
	Version          int
	MinRadiusM       float64 // минимальный радиус кривой в плане
	MaxGrade         float64 // предельный уклон, доля (0.030 = 30 промилле)
	MinTrackSpacingM float64 // минимальное междупутье вне горловин
	// Габарит приближения платформы: ближняя кромка не ближе к оси пути, чем
	// нормировано для её высоты. Величины разные потому, что высокая платформа
	// подходит к габариту подвижного состава сбоку кузова, а низкая — ниже него.
	//
	// PlatformHighThresholdM — граница, выше которой платформа считается
	// высокой. Порог, а не перечень видов: вид платформы в формате не заводится
	// (потребителя нет), а высота есть, и по ней граница проводится однозначно.
	PlatformHighThresholdM float64
	PlatformOffsetLowMinM  float64
	PlatformOffsetHighMinM float64
}

func DefaultProfile() Profile {
	return Profile{
		Version:                ProfileVersion,
		MinRadiusM:             180.0,
		MaxGrade:               0.030,
		MinTrackSpacingM:       4.1,
		PlatformHighThresholdM: 0.5,
		PlatformOffsetLowMinM:  1.745,
		PlatformOffsetHighMinM: 1.920,
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

	// Габарит платформы — норма, а не отрисовка, поэтому проверка здесь, а не в
	// checkPlatformSizes. Различие не формальное: диапазон отвечает на вопрос
	// «правдоподобно ли число», норма — на вопрос «можно ли это построить», и
	// смена источника норм не должна трогать модуль отрисовки.
	//
	// Проверка стала возможна только с появлением высоты платформы: без неё
	// нельзя было отличить низкую от высокой, а у них разный габарит. Это второй
	// раз за редакцию, когда объявление датума z сняло отказ, стоявший по
	// причине «число формы не задаёт».
	for _, st := range m.Topology.Structures {
		if st.Kind != "platform" {
			continue
		}
		min := p.PlatformOffsetLowMinM
		what := "низкой"
		if st.Height > p.PlatformHighThresholdM {
			min = p.PlatformOffsetHighMinM
			what = "высокой"
		}
		if st.Offset < min {
			return fmt.Errorf(
				"нормы: платформа %s высотой %.3f м: ближняя кромка на %.3f м от оси, для %s платформы нужно не менее %.3f м (профиль %d)",
				st.ID, st.Height, st.Offset, what, min, p.Version)
		}
	}
	return nil
}
