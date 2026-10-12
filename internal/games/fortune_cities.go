package games

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
	{"tokyo", "Tokyo", "東京", map[int]string{
		1: "Sugamo", 3: "Kitasenju",
		5: "Ueno Station",
		6: "Asakusa", 8: "Yanaka", 9: "Nippori",
		11: "Kanda", 12: "Tokyo Power", 13: "Jimbocho", 14: "Akihabara",
		15: "Tokyo Station",
		16: "Harajuku", 18: "Omotesando", 19: "Shibuya",
		21: "Nakameguro", 23: "Daikanyama", 24: "Ebisu",
		25: "Shinagawa Station",
		26: "Kagurazaka", 27: "Yotsuya", 28: "Tokyo Waterworks", 29: "Shinjuku",
		31: "Toranomon", 32: "Akasaka", 34: "Roppongi",
		35: "Shimbashi Station",
		37: "Marunouchi", 39: "Ginza",
		40: "Okachimachi", 41: "Yushima", 43: "Hongo", 44: "Ochanomizu", 46: "Suidobashi", 47: "Iidabashi",
		48: "Azabu-Juban", 49: "Hiroo", 51: "Shirokane", 52: "Takanawa", 54: "Mita", 55: "Hamamatsucho",
	}},
	{"new-york", "New York", "New York", map[int]string{
		1: "Harlem", 3: "Washington Heights",
		5: "Penn Station",
		6: "Chinatown", 8: "Little Italy", 9: "Lower East Side",
		11: "Williamsburg", 12: "Con Edison", 13: "DUMBO", 14: "Brooklyn Heights",
		15: "Grand Central",
		16: "Greenwich Village", 18: "SoHo", 19: "Tribeca",
		21: "Chelsea", 23: "Gramercy", 24: "Flatiron",
		25: "Staten Island Ferry",
		26: "Broadway", 27: "Times Square", 28: "City Water Tunnel", 29: "Rockefeller Center",
		31: "Upper West Side", 32: "Upper East Side", 34: "Central Park South",
		35: "Hudson Pier",
		37: "Wall Street", 39: "Fifth Avenue",
		40: "Hell's Kitchen", 41: "Garment District", 43: "Koreatown", 44: "NoMad", 46: "Murray Hill", 47: "Kips Bay",
		48: "Battery Park City", 49: "Financial District", 51: "Seaport", 52: "Two Bridges", 54: "Bowery", 55: "NoLIta",
	}},
	{"shanghai", "Shanghai", "上海", map[int]string{
		1: "Zhabei", 3: "Hongkou",
		5: "Railway Station",
		6: "Old City", 8: "Yuyuan Garden", 9: "City God Temple",
		11: "Changle Road", 12: "Nanshi Power", 13: "Wukang Road", 14: "Jing'an Temple",
		15: "Hongqiao Station",
		16: "Tianzifang", 18: "Huaihai Road", 19: "Xintiandi",
		21: "Fuxing Park", 23: "Sinan Road", 24: "Hengshan Road",
		25: "Shiliupu Ferry",
		26: "Jiangning Road", 27: "People's Square", 28: "Yangshupu Waterworks", 29: "Nanjing Road",
		31: "Jinqiao", 32: "Century Avenue", 34: "Lujiazui",
		35: "Bund Ferry",
		37: "Peace Hotel", 39: "The Bund",
		40: "Suzhou Creek", 41: "Moganshan Road", 43: "Changshou Road", 44: "Caoyang", 46: "Zhongshan Park", 47: "Gubei",
		48: "Dongjiadu", 49: "Laoximen", 51: "Dajing Road", 52: "Fangbang Road", 54: "Henan Road", 55: "Yu Garden",
	}},
	{"roma", "Rome", "Roma", map[int]string{
		1: "Ostiense", 3: "Testaccio",
		5: "Roma Termini",
		6: "San Lorenzo", 8: "Monti", 9: "Trastevere",
		11: "Borgo Pio", 12: "Centrale Elettrica", 13: "Prati", 14: "Via Cola di Rienzo",
		15: "Stazione Tiburtina",
		16: "Campo de' Fiori", 18: "Pantheon", 19: "Piazza Navona",
		21: "Via del Corso", 23: "Fontana di Trevi", 24: "Piazza di Spagna",
		25: "Porto di Ostia",
		26: "Aventino", 27: "Palatino", 28: "Acquedotto", 29: "Circo Massimo",
		31: "Parioli", 32: "Via Veneto", 34: "Villa Borghese",
		35: "Stazione Trastevere",
		37: "Via Condotti", 39: "Colosseo",
		40: "Esquilino", 41: "Piazza Vittorio", 43: "Pigneto", 44: "Porta Maggiore", 46: "Nomentano", 47: "Salario",
		48: "Garbatella", 49: "Piramide", 51: "Roseto", 52: "Bocca della Verità", 54: "Ghetto", 55: "Isola Tiberina",
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
		return n
	}
	return Board[sq].Name
}
