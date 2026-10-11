package games

import (
	"fmt"
	"math/rand/v2"
)

type Shop struct {
	Day, Cash, Stock, Condition int
	Dice                        [3]int
	Jobs                        [3]int // die index for stock, sales, repairs; -1 is unassigned
	Result                      Outcome
	Message                     string
}

func NewShop() *Shop {
	g := &Shop{Day: 1, Cash: 20, Stock: 3, Condition: 5}
	g.roll()
	return g
}

func (g *Shop) roll() {
	for i := range g.Dice {
		g.Dice[i] = rand.IntN(6) + 1
		g.Jobs[i] = -1
	}
}

func (g *Shop) Assign(job, die int) {
	if g.Result != Playing || job < 0 || job > 2 || die < 0 || die > 2 {
		return
	}
	for i, d := range g.Jobs {
		if d == die {
			g.Jobs[i] = -1
		}
	}
	g.Jobs[job] = die
}

func (g *Shop) EndDay() bool {
	if g.Result != Playing {
		return false
	}
	for _, d := range g.Jobs {
		if d < 0 {
			return false
		}
	}
	buy := min(g.Dice[g.Jobs[0]], g.Cash/2)
	g.Cash -= buy * 2
	g.Stock += buy
	sold := min(g.Stock, g.Dice[g.Jobs[1]]+g.Condition/3)
	g.Stock -= sold
	g.Cash += sold*5 - 6
	g.Condition = min(9, max(0, g.Condition+g.Dice[g.Jobs[2]]-3))
	g.Message = fmt.Sprintf("Day %d: bought %d, sold %d, paid $6 rent.", g.Day, buy, sold)
	if g.Cash < 0 {
		g.Result = Lost
	} else if g.Day == 7 {
		if g.Cash >= 65 {
			g.Result = Won
		} else {
			g.Result = Lost
		}
	} else {
		g.Day++
		g.roll()
	}
	return true
}
