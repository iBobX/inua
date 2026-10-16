package gamesui

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"codeberg.org/puregotk/puregotk/v4/gtk"

	"github.com/lyricat/inua/internal/assistant/answer"
	"github.com/lyricat/inua/internal/games/fortuneai"
	"github.com/lyricat/inua/internal/gtkx"
	"github.com/lyricat/inua/internal/i18n"
)

// The user's AI as a player (fortuneai), asked through inua-assistant
// --answer: a seat's choice in the setup. With more than one profile in
// Settings → AI, the seat picks the profile it asks (the one for quick
// choices first), and two seats may ask two. Each profile a seat asks
// first picks from a small choice, and Start waits for it. The program stays for the games after.

type aiState uint8

const (
	aiUnknown aiState = iota
	aiChecking
	aiReady
	aiFailed
)

// aiCheck is whether a profile answers, and why not.
type aiCheck struct {
	state aiState
	why   string
}

// checkAI starts inua-assistant --answer, then checks the profiles the
// seats ask.
func (p *fortune) checkAI() {
	switch p.aiState {
	case aiChecking:
		return
	case aiReady:
		p.checkProfiles()
		return
	}
	p.aiState = aiChecking
	go func() {
		c, err := startAI()
		gtkx.Post(func() {
			if err != nil {
				p.aiState, p.aiWhy = aiFailed, err.Error()
				p.fitSeats()
				return
			}
			p.aiState, p.aiClient = aiReady, c
			p.ai = fortuneai.New(c.Choose, gtkx.Post)
			var names []string
			for _, pr := range c.Profiles {
				names = append(names, pr.Name)
			}
			for _, d := range p.profileDrops {
				list := gtk.NewStringList(names)
				d.SetModel(list)
				list.Unref()
			}
			p.checkProfiles()
			p.fitSeats()
		})
	}()
}

// startAI starts the program; the error is why the AI cannot play, for the
// user.
func startAI() (*answer.Client, error) {
	prog, err := answer.Program()
	if err != nil {
		return nil, errors.New(i18n.T("inua-assistant is not installed"))
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	c, err := answer.Start(ctx, prog, "--answer")
	return c, aiError(err)
}

// checkProfiles gives each AI seat a profile, the default unless it has
// one, and asks each profile asked something small, once.
func (p *fortune) checkProfiles() {
	if p.checks == nil {
		p.checks = map[string]*aiCheck{}
	}
	c := p.aiClient
	for i, seat := range p.seats {
		if seat != seatAI {
			continue
		}
		if !slices.ContainsFunc(c.Profiles, func(pr answer.Profile) bool { return pr.Name == p.seatProfile[i] }) {
			p.seatProfile[i] = c.Profiles[0].Name
		}
		name := p.seatProfile[i]
		if p.checks[name] != nil {
			continue
		}
		ch := &aiCheck{state: aiChecking}
		p.checks[name] = ch
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			i, err := c.Choose(ctx, name, "A check that you can choose: pick the first option.", "Nothing to judge.", []string{"this one", "not this one"})
			if err == nil && i != 0 {
				err = errors.New(i18n.T("the AI picked the wrong option"))
			}
			err = aiError(err)
			gtkx.Post(func() {
				if err != nil {
					ch.state, ch.why = aiFailed, err.Error()
				} else {
					ch.state = aiReady
				}
				p.fitSeats()
			})
		}()
	}
}

// forgetFailures lets the setup check again what failed: the program, the
// profiles.
func (p *fortune) forgetFailures() {
	if p.aiState == aiFailed {
		p.aiState = aiUnknown
	}
	for name, ch := range p.checks {
		if ch.state == aiFailed {
			delete(p.checks, name)
		}
	}
}

func aiError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return errors.New(i18n.T("the AI did not answer in time"))
	}
	return err
}

// aiSeat is what AI seat i says under its name, its tooltip, and whether
// it can play.
func (p *fortune) aiSeat(i int) (sub, about string, ready bool) {
	switch p.aiState {
	case aiFailed:
		return p.aiWhy, p.aiWhy, false
	case aiReady:
	default:
		return i18n.T("checking the AI…"), "", false
	}
	name := p.seatProfile[i]
	ch := p.checks[name]
	switch {
	case ch == nil || ch.state == aiChecking:
		return i18n.T("checking the AI…"), "", false
	case ch.state == aiFailed:
		return i18n.T("this profile did not answer"), name + ": " + ch.why, false
	}
	model := name
	for _, pr := range p.aiClient.Profiles {
		if pr.Name == name {
			model = pr.Model
		}
	}
	return i18n.Tf("the AI: %s", model), name + " · " + model, true
}

// aiPlayers name the AI players, in the seats' order, and the profiles
// they ask: "AI", or "AI 1" …; with profiles to choose from, each is
// named for its profile.
func (p *fortune) aiPlayers() (names, profiles []string) {
	for i, c := range p.seats {
		if c == seatAI {
			profiles = append(profiles, p.seatProfile[i])
		}
	}
	switch {
	case p.aiClient != nil && len(p.aiClient.Profiles) > 1:
		for k, pr := range profiles {
			name, same := pr, 0
			for _, q := range profiles[:k] {
				if q == pr {
					same++
				}
			}
			if same > 0 {
				name = fmt.Sprintf("%s %d", pr, same+1)
			}
			names = append(names, name)
		}
	case len(profiles) == 1:
		names = []string{i18n.T("AI")}
	default:
		for k := range profiles {
			names = append(names, i18n.Tf("AI %d", k+1))
		}
	}
	return names, profiles
}

// profileDrop is a seat's choice of profile, shown for the AI with more
// than one.
func (p *fortune) profileDrop() *gtk.DropDown {
	d := gtk.NewDropDownFromStrings(nil)
	d.AddCssClass("fortune-seat")
	d.AddCssClass("fortune-profile")
	d.SetValign(gtk.AlignCenterValue)
	d.SetVisible(false)
	d.SetTooltipText(i18n.T("The profile of Settings → AI this player asks"))
	if p.onProfile == nil {
		p.onProfile = func(ptr, _, _ uintptr) {
			for i, d := range p.profileDrops {
				if d != nil && d.GoPointer() == ptr && !p.fillingSeats && p.aiClient != nil {
					if n := int(d.GetSelected()); n < len(p.aiClient.Profiles) {
						p.seatProfile[i] = p.aiClient.Profiles[n].Name
						p.checkProfiles()
						p.fitSeats()
					}
				}
			}
		}
	}
	gtkx.ConnectNotify(d, "selected", &p.onProfile)
	return d
}

// fitProfileDrop shows seat i's choice of profile when it is the AI's and
// there is a choice.
func (p *fortune) fitProfileDrop(i int) {
	d := p.profileDrops[i]
	on := p.seats[i] == seatAI && p.aiState == aiReady && len(p.aiClient.Profiles) > 1
	d.SetVisible(on)
	if on {
		n := slices.IndexFunc(p.aiClient.Profiles, func(pr answer.Profile) bool { return pr.Name == p.seatProfile[i] })
		d.SetSelected(uint32(max(0, n)))
	}
}

// aiNext has the AI move when its turn is next in a game at this screen
// (a game on the network, its host does), once the board is at rest.
func (p *fortune) aiNext() {
	if p.ai == nil || p.net != nil || p.g == nil {
		return
	}
	p.ai.Next(p.g, 400*time.Millisecond, func() {
		p.take()
		p.idle = false
		p.play()
	})
}
