package games

import (
	"fmt"
	"math/rand/v2"

	"github.com/lyricat/inua/internal/i18n"
)

// Space Pinball's rules: what the table's shots are worth, the missions
// and the ranks they earn, fuel, the hyperspace ramp's levels,
// multiball, the end-of-ball bonus and tilt (docs/feat/pinball.md).

// Shot is something a ball did.
type Shot uint8

const (
	ShotBumper    Shot = iota
	ShotSling          // a slingshot
	ShotTarget         // a mission target
	ShotBooster        // a booster drop target
	ShotLane           // a re-entry lane
	ShotInlane         //
	ShotOutlane        //
	ShotHyper          // the hyperspace ramp
	ShotLaunch         // the launch ramp
	ShotBlackHole      //
	ShotWormhole       //
	ShotBank           // the booster bank, all three down
	ShotAnyTarget      // for missions: a mission or booster target, or a slingshot
)

// Sound is a sound the table asks for; the window plays them.
type Sound uint8

const (
	SndFlipper Sound = iota
	SndBumper
	SndSling
	SndTarget
	SndDrop
	SndLane
	SndRamp
	SndHole
	SndKick
	SndWormhole
	SndLaunch
	SndDrain
	SndMission   // a mission lit or accepted
	SndComplete  // a mission completed
	SndPromotion //
	SndFail      // a mission failed
	SndExtraBall //
	SndMultiball //
	SndJackpot   //
	SndSkill     // the skill shot
	SndNudge     //
	SndTilt      //
	SndSave      // ball saved
)

// Ranks are the career a player climbs by flying missions.
var Ranks = []string{i18n.M("Cadet"), i18n.M("Ensign"), i18n.M("Lieutenant"), i18n.M("Captain"), i18n.M("Lt. Commander"),
	i18n.M("Commander"), i18n.M("Commodore"), i18n.M("Admiral"), i18n.M("Fleet Admiral")}

// promoteAt are the missions done that earn each rank after Cadet.
var promoteAt = []int{1, 2, 4, 6, 8, 11, 14, 18}

// MissionStep is a part of a mission: a shot, so many times.
type MissionStep struct {
	Shot  Shot
	Count int
}

// Mission is a task the launch ramp accepts once the mission targets
// light it.
type Mission struct {
	Name, Brief string
	Rank        int // the rank it opens at
	Steps       []MissionStep
	Time        float64 // seconds to fly it
	Reward      int
}

var Missions = []Mission{
	{i18n.M("Target Practice"), i18n.M("Hit the attack bumpers 8 times."), 0, []MissionStep{{ShotBumper, 8}}, 60, 15000},
	{i18n.M("Launch Training"), i18n.M("Shoot the launch ramp 3 times."), 0, []MissionStep{{ShotLaunch, 3}}, 75, 20000},
	{i18n.M("Re-entry Training"), i18n.M("Roll through the re-entry lanes 3 times."), 0, []MissionStep{{ShotLane, 3}}, 75, 15000},
	{i18n.M("Science Run"), i18n.M("Knock down all the booster targets."), 1, []MissionStep{{ShotBank, 1}}, 60, 25000},
	{i18n.M("Stray Comet"), i18n.M("Shoot the hyperspace ramp twice."), 1, []MissionStep{{ShotHyper, 2}}, 75, 30000},
	{i18n.M("Into the Black Hole"), i18n.M("Sink the black hole twice."), 2, []MissionStep{{ShotBlackHole, 2}}, 75, 30000},
	{i18n.M("Wormhole Survey"), i18n.M("Enter the wormhole, then sink the black hole."), 2,
		[]MissionStep{{ShotWormhole, 1}, {ShotBlackHole, 1}}, 90, 40000},
	{i18n.M("Bug Hunt"), i18n.M("Hit 15 targets or slingshots."), 3, []MissionStep{{ShotAnyTarget, 15}}, 45, 40000},
	{i18n.M("Satellite Retrieval"), i18n.M("Two launch ramps, then the black hole."), 3,
		[]MissionStep{{ShotLaunch, 2}, {ShotBlackHole, 1}}, 90, 50000},
	{i18n.M("Rescue"), i18n.M("Hyperspace, then the launch ramp, then the wormhole."), 4,
		[]MissionStep{{ShotHyper, 1}, {ShotLaunch, 1}, {ShotWormhole, 1}}, 90, 60000},
	{i18n.M("Radiation Storm"), i18n.M("Hit the bumpers 20 times, then shoot hyperspace."), 5,
		[]MissionStep{{ShotBumper, 20}, {ShotHyper, 1}}, 90, 75000},
	{i18n.M("Doomsday Machine"), i18n.M("The booster bank, both ramps, the wormhole, then the black hole."), 6,
		[]MissionStep{{ShotBank, 1}, {ShotHyper, 1}, {ShotLaunch, 1}, {ShotWormhole, 1}, {ShotBlackHole, 1}}, 120, 150000},
}

// ShotName names a shot for a mission's progress.
func ShotName(s Shot) string {
	return map[Shot]string{ShotBumper: i18n.T("Bumpers"), ShotLaunch: i18n.T("Launch ramp"), ShotLane: i18n.T("Re-entry lanes"),
		ShotBank: i18n.T("Booster bank"), ShotHyper: i18n.T("Hyperspace"), ShotBlackHole: i18n.T("Black hole"),
		ShotWormhole: i18n.T("Wormhole"), ShotAnyTarget: i18n.T("Targets")}[s]
}

// HyperAwards are what the hyperspace ramp's five levels pay.
var HyperAwards = [5]int{10000, 25000, 50000, 100000, 250000}

const (
	ballSaveTime  = 8.0
	redeployTime  = 12.0
	missionFuel   = 20.0 // the least fuel to accept a mission
	fuelBurn      = 1.5  // a second, while a mission is flown
	jackpotStart  = 50000
	skillWindow   = 5.0
	tiltDanger    = 2.0
	tiltLimit     = 3.0
	tiltRecovery  = 0.7 // of the tilt meter, a second
	messageTime   = 2.2
	bankResetTime = 1.2
)

// Pinball is one game: the balls on the table and everything the rules
// keep.
type Pinball struct {
	Balls  []Ball
	BallNo int // the ball in play, from 1
	Balls0 int // balls a game
	Extra  int // extra balls to come
	Score  int
	Result Outcome

	Flipper [2]float64 // angle below the horizontal, in radians
	turn    [2]float64
	Charge  float64 // the plunger, pulled from 0 to 1
	pulling bool

	// Lights and what they count.
	Lanes       [3]bool // lit re-entry lanes
	FieldMult   int     // 1–5
	Down        [3]bool // dropped booster targets
	bankReset   float64
	Targets     [3]bool // lit mission targets
	MissionLit  bool    // the launch ramp accepts Selected
	Selected    int     // the mission the targets picked
	Mission     int     // the mission flown, -1 none
	MStep       int     // its step
	Progress    int     // shots towards the step
	MissionLeft float64 // seconds left to fly it
	MissionsWon int
	Rank        int
	Fuel        float64 // 0–100
	BumperHits  int
	HyperLevel  int  // hyperspace levels lit, 0–4
	hyperExtra  bool // hyperspace has lit its extra ball
	ExtraLit    bool
	LockLit     bool
	Locked      int // balls locked towards multiball
	Multiball   bool
	Jackpot     int
	Save        float64 // seconds of ball save left
	saveSpent   bool    // the ball's launch save is spent
	Skill       int     // the lane the skill shot is lit on, -1 none
	skillLeft   float64
	Bonus       int // the end-of-ball bonus, before the field multiplier
	Tilted      bool
	TiltMeter   float64
	launches    int // launches of this ball

	// Flashes are seconds of light after a hit, for the drawing.
	BumperFlash [3]float64
	SlingFlash  [2]float64
	TargetFlash [3]float64
	LaneFlash   float64
	RampFlash   [2]float64
	HoleFlash   float64
	Shake       float64 // seconds the table shakes after a nudge
	Clock       float64 // seconds of play, for the drawing's animations

	Message     string // what just happened, large on the table
	MessageTime float64
	Log         []string // the latest messages, newest first
	Sounds      []Sound  // sounds to play, taken by the window
}

func NewPinball() *Pinball {
	g := &Pinball{Balls0: 3, BallNo: 1, FieldMult: 1, Mission: -1, Fuel: 100, Jackpot: jackpotStart, Skill: -1}
	g.Flipper = [2]float64{flipperRest, flipperRest}
	g.serve(false)
	g.armSkill()
	return g
}

// Ball reports how many balls the table has live (not on the plunger).
func (g *Pinball) live() int {
	n := 0
	for _, b := range g.Balls {
		if b.Mode != OnPlunger {
			n++
		}
	}
	return n
}

func (g *Pinball) sound(s Sound) {
	if len(g.Sounds) < 32 {
		g.Sounds = append(g.Sounds, s)
	}
}

// TakeSounds returns the sounds asked for since the last call.
func (g *Pinball) TakeSounds() []Sound {
	s := g.Sounds
	g.Sounds = nil
	return s
}

func (g *Pinball) say(s string) {
	g.Message, g.MessageTime = s, messageTime
	g.Log = append([]string{s}, g.Log...)
	if len(g.Log) > 5 {
		g.Log = g.Log[:5]
	}
}

// score adds points, times the field multiplier, and a tenth to the bonus.
func (g *Pinball) score(n int) {
	if g.Tilted {
		return
	}
	g.Score += n * g.FieldMult
	g.Bonus += n / 10
}

// RankName is the player's rank.
func (g *Pinball) RankName() string { return i18n.T(Ranks[g.Rank]) }

// BumperLevel is the attack bumpers' value level, 1–5.
func (g *Pinball) BumperLevel() int { return min(5, 1+g.BumperHits/12) }

// Available are the missions the player's rank opens.
func (g *Pinball) available() []int {
	var out []int
	for i, m := range Missions {
		if m.Rank <= g.Rank {
			out = append(out, i)
		}
	}
	return out
}

func (g *Pinball) armSkill() {
	g.Skill, g.skillLeft = rand.IntN(3), 0
}

// launched is a ball leaving the plunger.
func (g *Pinball) launched() {
	g.launches++
	if !g.saveSpent && !g.Multiball {
		g.Save, g.saveSpent = ballSaveTime, true
	}
	if g.Skill >= 0 {
		g.skillLeft = skillWindow
	}
}

// tick runs the rules' clocks.
func (g *Pinball) tick(h float64) {
	g.Clock += h
	for i := range g.BumperFlash {
		g.BumperFlash[i] = max(0, g.BumperFlash[i]-h)
	}
	for i := range g.SlingFlash {
		g.SlingFlash[i] = max(0, g.SlingFlash[i]-h)
	}
	for i := range g.TargetFlash {
		g.TargetFlash[i] = max(0, g.TargetFlash[i]-h)
	}
	for i := range g.RampFlash {
		g.RampFlash[i] = max(0, g.RampFlash[i]-h)
	}
	g.LaneFlash = max(0, g.LaneFlash-h)
	g.HoleFlash = max(0, g.HoleFlash-h)
	g.Shake = max(0, g.Shake-h)
	g.MessageTime = max(0, g.MessageTime-h)
	g.TiltMeter = max(0, g.TiltMeter-tiltRecovery*h)
	if g.bankReset > 0 {
		if g.bankReset -= h; g.bankReset <= 0 {
			g.Down = [3]bool{}
		}
	}
	if g.live() == 0 {
		return // clocks stand while no ball is in play
	}
	if g.Save > 0 {
		g.Save = max(0, g.Save-h)
	}
	if g.skillLeft > 0 {
		if g.skillLeft -= h; g.skillLeft <= 0 {
			g.Skill = -1
		}
	}
	if g.Mission >= 0 {
		g.MissionLeft -= h
		g.Fuel = max(0, g.Fuel-fuelBurn*h)
		switch {
		case g.Fuel == 0:
			g.failMission(i18n.T("OUT OF FUEL"))
		case g.MissionLeft <= 0:
			g.failMission(i18n.T("MISSION FAILED"))
		}
	}
}

// hit is a ball making a shot on the table.
func (g *Pinball) hit(s Shot, i int) {
	if g.Tilted {
		return
	}
	switch s {
	case ShotBumper:
		g.BumperHits++
		g.score(500 * g.BumperLevel())
		if g.BumperHits%12 == 0 && g.BumperLevel() <= 5 && g.BumperHits <= 48 {
			g.say(i18n.Tf("BUMPERS AT LEVEL %d", g.BumperLevel()))
		}
		if g.Multiball {
			g.Jackpot += 5000
		}
		g.sound(SndBumper)
		g.progress(ShotBumper)
	case ShotSling:
		g.score(50)
		g.sound(SndSling)
		g.progress(ShotAnyTarget)
	case ShotTarget:
		g.score(750)
		g.sound(SndTarget)
		g.Targets[i] = true
		if av := g.available(); len(av) > 0 && g.Mission < 0 {
			k := 0
			for j, m := range av {
				if m == g.Selected {
					k = j + 1
				}
			}
			g.Selected = av[k%len(av)]
		}
		if g.Targets == [3]bool{true, true, true} && g.Mission < 0 && !g.MissionLit {
			g.MissionLit = true
			g.say(i18n.Tf("MISSION LIT: %s", i18n.T(Missions[g.Selected].Name)))
			g.sound(SndMission)
		}
		g.progress(ShotAnyTarget)
	case ShotBooster:
		g.score(1000)
		g.sound(SndDrop)
		g.progress(ShotAnyTarget)
		if g.Down == [3]bool{true, true, true} {
			g.score(5000)
			g.bankReset = bankResetTime
			if !g.Multiball && g.Locked < 2 {
				g.LockLit = true
				g.say(i18n.T("BOOSTER BANK: LOCK IS LIT"))
			} else {
				g.say(i18n.T("BOOSTER BANK"))
			}
			g.progress(ShotBank)
		}
	case ShotLane:
		g.score(1000)
		g.Fuel = min(100, g.Fuel+8)
		g.sound(SndLane)
		if g.skillLeft > 0 {
			if i == g.Skill {
				g.score(25000 * (g.Rank + 1))
				g.say(i18n.T("SKILL SHOT"))
				g.sound(SndSkill)
			}
			g.Skill, g.skillLeft = -1, 0
		}
		g.Lanes[i] = true
		if g.Lanes == [3]bool{true, true, true} {
			g.Lanes = [3]bool{}
			g.LaneFlash = 0.8
			g.Fuel = 100
			g.score(2500)
			if g.FieldMult < 5 {
				g.FieldMult++
				g.say(i18n.Tf("FIELD MULTIPLIER ×%d", g.FieldMult))
			} else {
				g.Save = redeployTime
				g.say(i18n.T("RE-DEPLOY: BALL SAVE LIT"))
			}
		}
		g.progress(ShotLane)
	case ShotInlane:
		g.score(500)
		g.Fuel = min(100, g.Fuel+15)
	case ShotOutlane:
		g.score(2000)
	case ShotHyper:
		g.RampFlash[HyperRamp] = 0.8
		g.sound(SndRamp)
		g.score(5000)
		award := HyperAwards[g.HyperLevel]
		g.score(award * (g.Rank + 1))
		switch g.HyperLevel {
		case 2:
			if g.hyperExtra { // once a game
				g.say(i18n.T("HYPERSPACE 3"))
				break
			}
			g.ExtraLit, g.hyperExtra = true, true
			g.say(i18n.T("HYPERSPACE 3: EXTRA BALL LIT"))
		case 4:
			g.say(i18n.Tf("HYPERSPACE JACKPOT %s", thousands(award*(g.Rank+1)*g.FieldMult)))
			g.sound(SndJackpot)
		default:
			g.say(i18n.Tf("HYPERSPACE %d", g.HyperLevel+1))
		}
		g.HyperLevel = (g.HyperLevel + 1) % 5
		g.progress(ShotHyper)
	case ShotLaunch:
		g.RampFlash[LaunchRamp] = 0.8
		g.sound(SndRamp)
		g.score(5000)
		switch {
		case g.Multiball:
			g.score(g.Jackpot)
			g.say(i18n.Tf("JACKPOT %s", thousands(g.Jackpot*g.FieldMult)))
			g.sound(SndJackpot)
			g.Jackpot = jackpotStart
		case g.MissionLit && g.Mission < 0:
			if g.Fuel < missionFuel {
				g.say(i18n.T("REFUEL FIRST: ROLL THE LANES"))
				break
			}
			m := Missions[g.Selected]
			g.Mission, g.MStep, g.Progress, g.MissionLeft = g.Selected, 0, 0, m.Time
			g.MissionLit, g.Targets = false, [3]bool{}
			g.say(i18n.Tf("MISSION: %s", i18n.T(m.Name)))
			g.sound(SndMission)
		default:
			g.progress(ShotLaunch)
		}
	}
}

// captured is a ball falling into a saucer.
func (g *Pinball) captured(b *Ball, i int) {
	if g.Tilted {
		b.hold = 0.3
		return
	}
	g.HoleFlash = 1
	if i == Wormhole {
		g.score(5000)
		g.say(i18n.T("WORMHOLE"))
		g.sound(SndWormhole)
		g.progress(ShotWormhole)
		return
	}
	g.score(7500)
	g.sound(SndHole)
	switch {
	case g.ExtraLit:
		g.ExtraLit = false
		g.Extra++
		g.say(i18n.T("EXTRA BALL"))
		g.sound(SndExtraBall)
	case g.LockLit:
		g.LockLit = false
		g.Locked++
		if g.Locked < 2 {
			g.say(i18n.Tf("BALL %d LOCKED", g.Locked))
			break
		}
		g.Locked, g.Multiball, g.Jackpot = 0, true, jackpotStart
		g.say(i18n.T("MULTIBALL"))
		g.sound(SndMultiball)
		g.serve(true)
		g.serve(true)
		g.Save = ballSaveTime
	default:
		g.say(i18n.T("BLACK HOLE"))
	}
	g.progress(ShotBlackHole)
}

// progress counts a shot towards the mission flown.
func (g *Pinball) progress(s Shot) {
	if g.Mission < 0 {
		return
	}
	m := Missions[g.Mission]
	st := m.Steps[g.MStep]
	if st.Shot != s {
		return
	}
	if g.Progress++; g.Progress < st.Count {
		return
	}
	g.MStep, g.Progress = g.MStep+1, 0
	if g.MStep < len(m.Steps) {
		g.say(i18n.Tf("%s next", ShotName(m.Steps[g.MStep].Shot)))
		return
	}
	// Done.
	g.Mission = -1
	g.MissionsWon++
	g.score(m.Reward * (g.Rank + 1))
	g.Bonus += 10000
	g.say(i18n.Tf("MISSION COMPLETE: %s", i18n.T(m.Name)))
	g.sound(SndComplete)
	if g.Rank < len(promoteAt) && g.MissionsWon >= promoteAt[g.Rank] {
		g.Rank++
		g.score(50000 * g.Rank)
		g.say(i18n.Tf("PROMOTED TO %s", i18n.T(Ranks[g.Rank])))
		g.sound(SndPromotion)
		if g.Rank%3 == 0 {
			g.ExtraLit = true
		}
	}
}

func (g *Pinball) failMission(why string) {
	g.Mission = -1
	g.say(why)
	g.sound(SndFail)
}

// MissionStatus describes the mission flown, or the one lit, for the
// display: its name, what to shoot and how far along it is.
func (g *Pinball) MissionStatus() (name, todo string, done, of int) {
	if g.Mission < 0 {
		m := Missions[g.Selected]
		if g.MissionLit {
			return i18n.T(m.Name), i18n.T("Shoot the launch ramp to accept"), 0, 0
		}
		return i18n.T(m.Name), i18n.T("Light the three mission targets"), 0, 0
	}
	m := Missions[g.Mission]
	st := m.Steps[g.MStep]
	return i18n.T(m.Name), ShotName(st.Shot), g.Progress, st.Count
}

// nudged is the table being bumped.
func (g *Pinball) nudged() {
	g.TiltMeter++
	switch {
	case g.TiltMeter >= tiltLimit:
		g.Tilted = true
		g.turn = [2]float64{-1, -1}
		g.say(i18n.T("TILT"))
		g.sound(SndTilt)
	case g.TiltMeter >= tiltDanger:
		g.say(i18n.T("DANGER"))
	}
}

// drained is n balls leaving the table.
func (g *Pinball) drained(n int) {
	if g.live() > 0 || g.onPlunger() != nil {
		if g.Multiball && g.live() <= 1 {
			g.Multiball = false
			g.say(i18n.T("MULTIBALL OVER"))
		}
		return
	}
	g.Multiball = false
	if g.Save > 0 && !g.Tilted {
		g.Save = 0
		g.serve(true)
		g.say(i18n.T("BALL SAVED"))
		g.sound(SndSave)
		return
	}
	g.sound(SndDrain)
	// The end-of-ball bonus.
	if !g.Tilted {
		bonus := (g.Bonus + 5000*g.Rank) * g.FieldMult
		g.Score += bonus
		g.say(i18n.Tf("BONUS %s", thousands(bonus)))
	}
	g.FieldMult, g.Lanes, g.Bonus = 1, [3]bool{}, 0
	g.Tilted, g.TiltMeter, g.turn = false, 0, [2]float64{-1, -1}
	g.saveSpent, g.launches = false, 0
	if g.Extra > 0 {
		g.Extra--
		g.serve(false)
		g.armSkill()
		g.say(i18n.T("SHOOT AGAIN"))
		return
	}
	if g.BallNo == g.Balls0 {
		g.Result = Lost
		g.failMission(i18n.T("GAME OVER"))
		g.Mission = -1
		return
	}
	g.BallNo++
	g.serve(false)
	g.armSkill()
}

// thousands writes n with commas: 12,340.
func thousands(n int) string {
	s := fmt.Sprint(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}
