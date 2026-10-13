package games

import "github.com/lyricat/inua/internal/i18n"

// FortuneCity is a city to play Small Fortune in: the board is the same
// (its kinds of squares, prices and rents), the names are the city's.
type FortuneCity struct {
	ID    string // "tokyo", "new-york", "shanghai", "roma"
	Name  string // in English
	Local string // as the city writes it
	// Names are the properties' names by square: the ring's streets cheap
	// to dear, the four stops (piers in the rules), the two utilities, and
	// the two branch roads' streets.
	Names map[int]string
}

// FortuneCities are the boards to choose from; the first is the default.
var FortuneCities = []FortuneCity{
	{"tokyo", i18n.M("Tokyo"), "東京", map[int]string{
		1: i18n.M("Sugamo"), 3: i18n.M("Kitasenju"),
		5: i18n.M("Ueno Station"),
		6: i18n.M("Asakusa"), 8: i18n.M("Yanaka"), 9: i18n.M("Nippori"),
		11: i18n.M("Kanda"), 12: i18n.M("Tokyo Power"), 13: i18n.M("Jimbocho"), 14: i18n.M("Akihabara"),
		15: i18n.M("Tokyo Station"),
		16: i18n.M("Harajuku"), 18: i18n.M("Omotesando"), 19: i18n.M("Shibuya"),
		21: i18n.M("Nakameguro"), 23: i18n.M("Daikanyama"), 24: i18n.M("Ebisu"),
		25: i18n.M("Shinagawa Station"),
		26: i18n.M("Kagurazaka"), 27: i18n.M("Yotsuya"), 28: i18n.M("Tokyo Waterworks"), 29: i18n.M("Shinjuku"),
		31: i18n.M("Toranomon"), 32: i18n.M("Akasaka"), 34: i18n.M("Roppongi"),
		35: i18n.M("Shimbashi Station"),
		37: i18n.M("Marunouchi"), 39: i18n.M("Ginza"),
		40: i18n.M("Okachimachi"), 41: i18n.M("Yushima"), 43: i18n.M("Hongo"), 44: i18n.M("Ochanomizu"), 46: i18n.M("Suidobashi"), 47: i18n.M("Iidabashi"),
		48: i18n.M("Azabu-Juban"), 49: i18n.M("Hiroo"), 51: i18n.M("Shirokane"), 52: i18n.M("Takanawa"), 54: i18n.M("Mita"), 55: i18n.M("Hamamatsucho"),
	}},
	{"new-york", i18n.M("New York"), i18n.M("New York"), map[int]string{
		1: i18n.M("Harlem"), 3: i18n.M("Washington Heights"),
		5: i18n.M("Penn Station"),
		6: i18n.M("Chinatown"), 8: i18n.M("Little Italy"), 9: i18n.M("Lower East Side"),
		11: i18n.M("Williamsburg"), 12: i18n.M("Con Edison"), 13: i18n.M("DUMBO"), 14: i18n.M("Brooklyn Heights"),
		15: i18n.M("Grand Central"),
		16: i18n.M("Greenwich Village"), 18: i18n.M("SoHo"), 19: i18n.M("Tribeca"),
		21: i18n.M("Chelsea"), 23: i18n.M("Gramercy"), 24: i18n.M("Flatiron"),
		25: i18n.M("Staten Island Ferry"),
		26: i18n.M("Broadway"), 27: i18n.M("Times Square"), 28: i18n.M("City Water Tunnel"), 29: i18n.M("Rockefeller Center"),
		31: i18n.M("Upper West Side"), 32: i18n.M("Upper East Side"), 34: i18n.M("Central Park South"),
		35: i18n.M("Hudson Pier"),
		37: i18n.M("Wall Street"), 39: i18n.M("Fifth Avenue"),
		40: i18n.M("Hell's Kitchen"), 41: i18n.M("Garment District"), 43: i18n.M("Koreatown"), 44: i18n.M("NoMad"), 46: i18n.M("Murray Hill"), 47: i18n.M("Kips Bay"),
		48: i18n.M("Battery Park City"), 49: i18n.M("Financial District"), 51: i18n.M("Seaport"), 52: i18n.M("Two Bridges"), 54: i18n.M("Bowery"), 55: i18n.M("NoLIta"),
	}},
	{"shanghai", i18n.M("Shanghai"), "上海", map[int]string{
		1: i18n.M("Zhabei"), 3: i18n.M("Hongkou"),
		5: i18n.M("Railway Station"),
		6: i18n.M("Old City"), 8: i18n.M("Yuyuan Garden"), 9: i18n.M("City God Temple"),
		11: i18n.M("Changle Road"), 12: i18n.M("Nanshi Power"), 13: i18n.M("Wukang Road"), 14: i18n.M("Jing'an Temple"),
		15: i18n.M("Hongqiao Station"),
		16: i18n.M("Tianzifang"), 18: i18n.M("Huaihai Road"), 19: i18n.M("Xintiandi"),
		21: i18n.M("Fuxing Park"), 23: i18n.M("Sinan Road"), 24: i18n.M("Hengshan Road"),
		25: i18n.M("Shiliupu Ferry"),
		26: i18n.M("Jiangning Road"), 27: i18n.M("People's Square"), 28: i18n.M("Yangshupu Waterworks"), 29: i18n.M("Nanjing Road"),
		31: i18n.M("Jinqiao"), 32: i18n.M("Century Avenue"), 34: i18n.M("Lujiazui"),
		35: i18n.M("Bund Ferry"),
		37: i18n.M("Peace Hotel"), 39: i18n.M("The Bund"),
		40: i18n.M("Suzhou Creek"), 41: i18n.M("Moganshan Road"), 43: i18n.M("Changshou Road"), 44: i18n.M("Caoyang"), 46: i18n.M("Zhongshan Park"), 47: i18n.M("Gubei"),
		48: i18n.M("Dongjiadu"), 49: i18n.M("Laoximen"), 51: i18n.M("Dajing Road"), 52: i18n.M("Fangbang Road"), 54: i18n.M("Henan Road"), 55: i18n.M("Yu Garden"),
	}},
	{"roma", i18n.M("Rome"), i18n.M("Roma"), map[int]string{
		1: i18n.M("Ostiense"), 3: i18n.M("Testaccio"),
		5: i18n.M("Roma Termini"),
		6: i18n.M("San Lorenzo"), 8: i18n.M("Monti"), 9: i18n.M("Trastevere"),
		11: i18n.M("Borgo Pio"), 12: i18n.M("Centrale Elettrica"), 13: i18n.M("Prati"), 14: i18n.M("Via Cola di Rienzo"),
		15: i18n.M("Stazione Tiburtina"),
		16: i18n.M("Campo de' Fiori"), 18: i18n.M("Pantheon"), 19: i18n.M("Piazza Navona"),
		21: i18n.M("Via del Corso"), 23: i18n.M("Fontana di Trevi"), 24: i18n.M("Piazza di Spagna"),
		25: i18n.M("Porto di Ostia"),
		26: i18n.M("Aventino"), 27: i18n.M("Palatino"), 28: i18n.M("Acquedotto"), 29: i18n.M("Circo Massimo"),
		31: i18n.M("Parioli"), 32: i18n.M("Via Veneto"), 34: i18n.M("Villa Borghese"),
		35: i18n.M("Stazione Trastevere"),
		37: i18n.M("Via Condotti"), 39: i18n.M("Colosseo"),
		40: i18n.M("Esquilino"), 41: i18n.M("Piazza Vittorio"), 43: i18n.M("Pigneto"), 44: i18n.M("Porta Maggiore"), 46: i18n.M("Nomentano"), 47: i18n.M("Salario"),
		48: i18n.M("Garbatella"), 49: i18n.M("Piramide"), 51: i18n.M("Roseto"), 52: i18n.M("Bocca della Verità"), 54: i18n.M("Ghetto"), 55: i18n.M("Isola Tiberina"),
	}},
}

// FortuneSettings are a new game's: how many computer players (1–3), how
// many rounds, how much each player starts with, which city; and its
// rules: one die or two (0 is two), and whether a street may go up a level
// the turn it is bought.
type FortuneSettings struct {
	Rivals, Rounds, Cash, City int
	Dice                       int
	BuildOnBuy                 bool
}

// The choices a game offers: rounds, starting cash.
var (
	FortuneRounds = []int{20, 30, 45, 60}
	FortuneCash   = []int{1000, 1500, 2000, 3000}
)

// DefaultFortune is a game against two, 30 rounds, $1,500 each, in Tokyo.
var DefaultFortune = FortuneSettings{Rivals: 2, Rounds: 30, Cash: 1500}

// SquareName is a square's name in the game's city.
func (g *Fortune) SquareName(sq int) string {
	if n, ok := FortuneCities[g.City].Names[sq]; ok {
		return i18n.T(n)
	}
	return i18n.T(Board[sq].Name)
}
