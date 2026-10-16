package games

import "github.com/lyricat/inua/internal/i18n"

// FortuneCity is a city to play Small Fortune in: the board is the same
// (its kinds of squares, prices and rents), the names are the city's.
type FortuneCity struct {
	ID    string // "tokyo", "new-york", "shanghai", "roma"
	Name  string // in English
	Local string // as the city writes it
	// Names are the properties' names in the board's order: each side of
	// the ring from Start, its streets cheap to dear round a station or
	// pier and a utility on two sides; then the two branch roads.
	Names []string
}

// FortuneCities are the boards to choose from; the first is the default.
var FortuneCities = []FortuneCity{
	{"tokyo", i18n.M("Tokyo"), "東京", []string{
		// the ring's first side, from Start
		i18n.M("Sugamo"), i18n.M("Kitasenju"), i18n.M("Otsuka"), i18n.M("Komagome"), i18n.M("Tabata"), i18n.M("Oji"), i18n.M("Ueno Station"), i18n.M("Asakusa"), i18n.M("Yanaka"), i18n.M("Nippori"), i18n.M("Ryogoku"), i18n.M("Kuramae"), i18n.M("Kiyosumi"),
		// its second, from Jail
		i18n.M("Kanda"), i18n.M("Jimbocho"), i18n.M("Akihabara"), i18n.M("Tokyo Power"), i18n.M("Ningyocho"), i18n.M("Monzen-Nakacho"), i18n.M("Tsukishima"), i18n.M("Tokyo Station"), i18n.M("Harajuku"), i18n.M("Omotesando"), i18n.M("Shibuya"), i18n.M("Sangenjaya"), i18n.M("Shimokitazawa"), i18n.M("Jiyugaoka"),
		// its third, from the Fountain
		i18n.M("Nakameguro"), i18n.M("Daikanyama"), i18n.M("Ebisu"), i18n.M("Meguro"), i18n.M("Gotanda"), i18n.M("Osaki"), i18n.M("Shinagawa Station"), i18n.M("Kagurazaka"), i18n.M("Yotsuya"), i18n.M("Shinjuku"), i18n.M("Tokyo Waterworks"), i18n.M("Ikebukuro"), i18n.M("Takadanobaba"), i18n.M("Waseda"),
		// its fourth, from Go to Jail
		i18n.M("Toranomon"), i18n.M("Akasaka"), i18n.M("Roppongi"), i18n.M("Kudanshita"), i18n.M("Nagatacho"), i18n.M("Kasumigaseki"), i18n.M("Shimbashi Station"), i18n.M("Hibiya"), i18n.M("Yurakucho"), i18n.M("Nihonbashi"), i18n.M("Otemachi"), i18n.M("Marunouchi"), i18n.M("Ginza"),
		// the first branch road
		i18n.M("Okachimachi"), i18n.M("Yushima"), i18n.M("Nezu"), i18n.M("Hongo"), i18n.M("Sendagi"), i18n.M("Ochanomizu"), i18n.M("Korakuen"), i18n.M("Suidobashi"), i18n.M("Hakusan"), i18n.M("Myogadani"), i18n.M("Edogawabashi"), i18n.M("Iidabashi"),
		// the second
		i18n.M("Azabu-Juban"), i18n.M("Hiroo"), i18n.M("Nishi-Azabu"), i18n.M("Shirokane"), i18n.M("Takanawa"), i18n.M("Sengakuji"), i18n.M("Mita"), i18n.M("Tamachi"), i18n.M("Shibaura"), i18n.M("Hamamatsucho"), i18n.M("Shiodome"), i18n.M("Shiba Park"),
	}},
	{"new-york", i18n.M("New York"), i18n.M("New York"), []string{
		// the ring's first side, from Start
		i18n.M("Harlem"), i18n.M("Washington Heights"), i18n.M("Inwood"), i18n.M("Hamilton Heights"), i18n.M("Morningside Heights"), i18n.M("East Harlem"), i18n.M("Penn Station"), i18n.M("Chinatown"), i18n.M("Little Italy"), i18n.M("Lower East Side"), i18n.M("East Village"), i18n.M("Alphabet City"), i18n.M("Stuyvesant Town"),
		// its second, from Jail
		i18n.M("Williamsburg"), i18n.M("DUMBO"), i18n.M("Brooklyn Heights"), i18n.M("Con Edison"), i18n.M("Park Slope"), i18n.M("Cobble Hill"), i18n.M("Red Hook"), i18n.M("Grand Central"), i18n.M("Greenwich Village"), i18n.M("SoHo"), i18n.M("Tribeca"), i18n.M("West Village"), i18n.M("Meatpacking District"), i18n.M("Hudson Square"),
		// its third, from the Fountain
		i18n.M("Chelsea"), i18n.M("Gramercy"), i18n.M("Flatiron"), i18n.M("Union Square"), i18n.M("Madison Square"), i18n.M("Herald Square"), i18n.M("Staten Island Ferry"), i18n.M("Broadway"), i18n.M("Times Square"), i18n.M("Rockefeller Center"), i18n.M("City Water Tunnel"), i18n.M("Bryant Park"), i18n.M("Turtle Bay"), i18n.M("Sutton Place"),
		// its fourth, from Go to Jail
		i18n.M("Upper West Side"), i18n.M("Upper East Side"), i18n.M("Central Park South"), i18n.M("Lincoln Center"), i18n.M("Carnegie Hill"), i18n.M("Lenox Hill"), i18n.M("Hudson Pier"), i18n.M("Park Avenue"), i18n.M("Madison Avenue"), i18n.M("Billionaires' Row"), i18n.M("Hudson Yards"), i18n.M("Wall Street"), i18n.M("Fifth Avenue"),
		// the first branch road
		i18n.M("Hell's Kitchen"), i18n.M("Garment District"), i18n.M("Penn South"), i18n.M("Koreatown"), i18n.M("NoMad"), i18n.M("Rose Hill"), i18n.M("Murray Hill"), i18n.M("Kips Bay"), i18n.M("Tudor City"), i18n.M("Midtown East"), i18n.M("Beekman"), i18n.M("Roosevelt Island"),
		// the second
		i18n.M("Battery Park City"), i18n.M("Financial District"), i18n.M("Bowling Green"), i18n.M("Seaport"), i18n.M("Two Bridges"), i18n.M("Civic Center"), i18n.M("Bowery"), i18n.M("NoLIta"), i18n.M("NoHo"), i18n.M("Astor Place"), i18n.M("Cooper Square"), i18n.M("Washington Square"),
	}},
	{"shanghai", i18n.M("Shanghai"), "上海", []string{
		// the ring's first side, from Start
		i18n.M("Zhabei"), i18n.M("Hongkou"), i18n.M("Baoshan Road"), i18n.M("Tilanqiao"), i18n.M("Dalian Road"), i18n.M("Quyang"), i18n.M("Railway Station"), i18n.M("Old City"), i18n.M("Yuyuan Garden"), i18n.M("City God Temple"), i18n.M("Penglai Road"), i18n.M("Dapuqiao"), i18n.M("Ruijin Road"),
		// its second, from Jail
		i18n.M("Changle Road"), i18n.M("Wukang Road"), i18n.M("Jing'an Temple"), i18n.M("Nanshi Power"), i18n.M("Yuyuan Road"), i18n.M("Yan'an Road"), i18n.M("Julu Road"), i18n.M("Hongqiao Station"), i18n.M("Tianzifang"), i18n.M("Huaihai Road"), i18n.M("Xintiandi"), i18n.M("Taikang Road"), i18n.M("Shaanxi South Road"), i18n.M("Maoming Road"),
		// its third, from the Fountain
		i18n.M("Fuxing Park"), i18n.M("Sinan Road"), i18n.M("Hengshan Road"), i18n.M("Xujiahui"), i18n.M("Tianping Road"), i18n.M("Anfu Road"), i18n.M("Shiliupu Ferry"), i18n.M("Jiangning Road"), i18n.M("People's Square"), i18n.M("Nanjing Road"), i18n.M("Yangshupu Waterworks"), i18n.M("Wujiang Road"), i18n.M("Fuzhou Road"), i18n.M("Weihai Road"),
		// its fourth, from Go to Jail
		i18n.M("Jinqiao"), i18n.M("Century Avenue"), i18n.M("Lujiazui"), i18n.M("Zhangjiang"), i18n.M("Huamu"), i18n.M("Binjiang Avenue"), i18n.M("Bund Ferry"), i18n.M("Sichuan Road"), i18n.M("Yuanmingyuan Road"), i18n.M("Rockbund"), i18n.M("Customs House"), i18n.M("Peace Hotel"), i18n.M("The Bund"),
		// the first branch road
		i18n.M("Suzhou Creek"), i18n.M("Moganshan Road"), i18n.M("Changhua Road"), i18n.M("Changshou Road"), i18n.M("Caoyang"), i18n.M("Jiaozhou Road"), i18n.M("Zhongshan Park"), i18n.M("Changning Road"), i18n.M("Jiangsu Road"), i18n.M("Hongqiao Road"), i18n.M("Hongmei Road"), i18n.M("Gubei"),
		// the second
		i18n.M("Dongjiadu"), i18n.M("Laoximen"), i18n.M("Xiaonanmen"), i18n.M("Dajing Road"), i18n.M("Fangbang Road"), i18n.M("Zhonghua Road"), i18n.M("Henan Road"), i18n.M("Cool Docks"), i18n.M("Fuyou Road"), i18n.M("Yu Garden"), i18n.M("Renmin Road"), i18n.M("Jinling Road"),
	}},
	{"roma", i18n.M("Rome"), i18n.M("Roma"), []string{
		// the ring's first side, from Start
		i18n.M("Ostiense"), i18n.M("Testaccio"), i18n.M("Marconi"), i18n.M("Portuense"), i18n.M("Monteverde"), i18n.M("San Paolo"), i18n.M("Roma Termini"), i18n.M("San Lorenzo"), i18n.M("Monti"), i18n.M("Trastevere"), i18n.M("Celio"), i18n.M("San Giovanni"), i18n.M("Laterano"),
		// its second, from Jail
		i18n.M("Borgo Pio"), i18n.M("Prati"), i18n.M("Via Cola di Rienzo"), i18n.M("Centrale Elettrica"), i18n.M("Flaminio"), i18n.M("Delle Vittorie"), i18n.M("Mazzini"), i18n.M("Stazione Tiburtina"), i18n.M("Campo de' Fiori"), i18n.M("Pantheon"), i18n.M("Piazza Navona"), i18n.M("Via Giulia"), i18n.M("Piazza Farnese"), i18n.M("Sant'Eustachio"),
		// its third, from the Fountain
		i18n.M("Via del Corso"), i18n.M("Fontana di Trevi"), i18n.M("Piazza di Spagna"), i18n.M("Piazza del Popolo"), i18n.M("Via del Babuino"), i18n.M("Via Margutta"), i18n.M("Porto di Ostia"), i18n.M("Aventino"), i18n.M("Palatino"), i18n.M("Circo Massimo"), i18n.M("Acquedotto"), i18n.M("Campidoglio"), i18n.M("Fori Imperiali"), i18n.M("Piazza Venezia"),
		// its fourth, from Go to Jail
		i18n.M("Parioli"), i18n.M("Via Veneto"), i18n.M("Villa Borghese"), i18n.M("Pinciano"), i18n.M("Ludovisi"), i18n.M("Quirinale"), i18n.M("Stazione Trastevere"), i18n.M("Via Sistina"), i18n.M("Trinità dei Monti"), i18n.M("Piazza Barberini"), i18n.M("Via Frattina"), i18n.M("Via Condotti"), i18n.M("Colosseo"),
		// the first branch road
		i18n.M("Esquilino"), i18n.M("Piazza Vittorio"), i18n.M("Santa Croce"), i18n.M("Pigneto"), i18n.M("Porta Maggiore"), i18n.M("Torpignattara"), i18n.M("Nomentano"), i18n.M("Salario"), i18n.M("Trieste"), i18n.M("Coppedè"), i18n.M("Villa Ada"), i18n.M("Villa Torlonia"),
		// the second
		i18n.M("Garbatella"), i18n.M("Piramide"), i18n.M("San Saba"), i18n.M("Roseto"), i18n.M("Bocca della Verità"), i18n.M("Foro Boario"), i18n.M("Ghetto"), i18n.M("Isola Tiberina"), i18n.M("Portico d'Ottavia"), i18n.M("Largo Argentina"), i18n.M("Teatro di Marcello"), i18n.M("Lungotevere"),
	}},
}

// propertyAt is each square's place among the properties, -1 for a square
// that is not one.
var propertyAt = func() (at [len(Board)]int) {
	n := 0
	for sq := range Board {
		at[sq] = -1
		if IsProperty(sq) {
			at[sq], n = n, n+1
		}
	}
	return at
}()

// FortuneSettings are a new game's: how many computer players (1–3), how
// many rounds, how much each player starts with, which city; and its
// rules: one die or two (0 is two), and whether a street may go up a level
// the turn it is bought.
type FortuneSettings struct {
	// Humans are the people's names, one to four, playing in this order
	// before the computer players; none is one person, "You". With
	// people named, Rivals 0 is no computer player, not the default.
	Humans []string
	// RivalNames name the computer players, in order: Mika, Ren and Sol
	// when none, or for those not named.
	RivalNames []string
	// AIs name the players the user's AI plays, after the people and
	// before the computer players.
	AIs                        []string
	Rivals, Rounds, Cash, City int
	Dice                       int
	BuildOnBuy                 bool
}

// The choices a game offers: rounds, starting cash.
var (
	FortuneRounds = []int{30, 60, 90, 120}
	FortuneCash   = []int{1000, 2000, 3000, 5000}
)

// DefaultFortune is a game against two, 60 rounds, $2,000 each, in Tokyo.
var DefaultFortune = FortuneSettings{Rivals: 2, Rounds: 60, Cash: 2000}

// SquareName is a square's name in the game's city.
func (g *Fortune) SquareName(sq int) string { return i18n.T(FortuneCities[g.City].SquareName(sq)) }

// SquareName is a square's name in the city, untranslated.
func (c FortuneCity) SquareName(sq int) string {
	if n := propertyAt[sq]; n >= 0 {
		return c.Names[n]
	}
	return Board[sq].Name
}
