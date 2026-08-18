package command

import (
	"fmt"

	"github.com/shady2k/ClearAhead/server/internal/match"
	"github.com/shady2k/ClearAhead/server/internal/track"
)

// Couple — СЦЕПИТЬ состав названной единицы с тем, что стоит вплотную.
//
// Третья доменная правка проекта. Первая была у кабины, вторая у пути, а эта —
// у СВЯЗИ МЕЖДУ МАШИНАМИ: она не меняет ни органов, ни остряков, а меняет то,
// что в этом мире считается одним телом.
type Couple struct {
	// Unit — чьей сцепкой цепляем. Сцеп находит сама правка: имя сцепа —
	// внутренняя запись партии, и требовать его от клиента значило бы заставить
	// кабину знать бухгалтерию мира.
	Unit string
	// Consist — имя нового сцепа. Приходит с командой ради идемпотентности
	// (разбор — у protocol.CoupleRequest).
	Consist string
	// Net — сеть региона: по ней ищется смычка (обход портов и положение
	// остряков живут там же, где движение).
	Net *track.CompiledNetwork
}

// Name — имя правки для журнала и логов.
func (Couple) Name() string { return "consist.couple" }

// Apply сцепляет, проверив правила партией.
//
// Как и у прочих правок, проверки живут в match, а не здесь: «валидатор
// отказывает, а не чинит» обязано стоять там, где состояние, иначе вторая
// дорога к тому же полю пройдёт мимо.
func (c Couple) Apply(m *match.Match) error {
	own, ok := m.ConsistOf(c.Unit)
	if !ok {
		return fmt.Errorf("command: единица %s не в сцепе — сцеплять нечего", c.Unit)
	}
	other, ok := m.NeighbourConsist(c.Net, own)
	if !ok {
		return fmt.Errorf("command: у сцепа единицы %s нет соседа вплотную: цеплять не с кем", c.Unit)
	}
	_, err := m.Couple(c.Net, own.ID, other.ID, c.Consist)
	return err
}

// Uncouple — РАСЦЕПИТЬ состав за названной единицей.
type Uncouple struct {
	// Unit — за какой единицей режем цепочку.
	Unit string
	// Consist — имя, которое получит отцепленная часть.
	Consist string
}

// Name — имя правки для журнала и логов.
func (Uncouple) Name() string { return "consist.uncouple" }

// Apply расцепляет, проверив правила партией.
func (c Uncouple) Apply(m *match.Match) error {
	own, ok := m.ConsistOf(c.Unit)
	if !ok {
		return fmt.Errorf("command: единица %s не в сцепе — расцеплять нечего", c.Unit)
	}
	_, _, err := m.Uncouple(own.ID, c.Unit, c.Consist)
	return err
}
