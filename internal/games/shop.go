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

// Day is what a day's work comes to.
type Day struct {
	Bought, Sold, Cash, Condition int
}

// Ready reports whether every job has a die.
func (g *Shop) Ready() bool {
	for _, d := range g.Jobs {
		if d < 0 {
			return false
		}
	}
	return g.Result == Playing
}

// Forecast is what the day would bring with the dice as assigned: stock
// is bought first (as much as the die and the cash allow), then sold
// (the sales die plus a customer for every three points of condition),
// then rent is paid; repairs change the condition for tomorrow.
func (g *Shop) Forecast() Day {
	var d Day
	die := func(job int) int {
		if g.Jobs[job] < 0 {
			return 0
		}
		return g.Dice[g.Jobs[job]]
	}
	d.Bought = min(die(0), g.Cash/StockCost)
	stock := g.Stock + d.Bought
	if g.Jobs[1] >= 0 {
		d.Sold = min(stock, die(1)+g.Condition/3)
	}
	d.Cash = g.Cash - d.Bought*StockCost + d.Sold*SalePrice - Rent
	d.Condition = g.Condition
	if g.Jobs[2] >= 0 {
		d.Condition = min(9, max(0, g.Condition+die(2)-3))
	}
	return d
}

const (
	StockCost  = 2
	SalePrice  = 5
	Rent       = 6
	Days       = 7
	ShopTarget = 75
)

func (g *Shop) EndDay() bool {
	if !g.Ready() {
		return false
	}
	d := g.Forecast()
	g.Stock += d.Bought - d.Sold
	g.Cash, g.Condition = d.Cash, d.Condition
	g.Message = fmt.Sprintf("Day %d: bought %d, sold %d, paid $%d rent.", g.Day, d.Bought, d.Sold, Rent)
	if g.Cash < 0 {
		g.Result = Lost
	} else if g.Day == Days {
		if g.Cash >= ShopTarget {
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
