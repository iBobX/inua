package gamesui

import (
	"math/rand/v2"

	"github.com/lyricat/inua/internal/games"
)

// Small Fortune's sounds, made by Space Pinball's synth: soft, a board
// game's. They follow the board as it shows the game, not the game: the
// dice rattle while they tumble and knock as they land, the pawn ticks on
// each square it hops onto, money chimes or thuds as the cash changes,
// and so on. M mutes them.

type fortuneSound uint8

const (
	sndDice     fortuneSound = iota // the dice tumbling
	sndDiceLand                     // and landing
	sndStep                         // a pawn hopping onto a square
	sndStart                        // passing Start
	sndJump                         // a pawn sent across the board
	sndCoin                         // cash gained
	sndPay                          // cash paid
	sndFountain                     // the fountain's pot taken
	sndBuy                          // a property's deed sealed
	sndBuild                        // a house going up
	sndHotel                        // a hotel going up
	sndRaze                         // a building sold or pulled down
	sndCardFlip                     // a card got, turning over
	sndCardPlay                     // a card played, rising
	sndCardHit                      // a card played, landing
	sndJail                         // to jail
	sndBankrupt                     // out of the game
	sndWin                          // the game won
	sndLose                         // the game lost
	sndYourTurn                     // the player's turn again
)

// fortune plays one of Small Fortune's sounds; step is how far the pawn
// has come, for its ticks to climb a little.
func (s *synth) fortune(snd fortuneSound, step int) {
	tone := func(wave int, delay, dur, f0, f1, vol float64) voice {
		return voice{delay: delay, dur: dur, f0: f0, f1: f1, vol: vol, wave: wave}
	}
	switch snd {
	case sndDice:
		var vs []voice
		for i := range 7 {
			at := float64(i)*0.06 + rand.Float64()*0.03
			vs = append(vs, tone(waveNoise, at, 0.018, 0, 0, 0.13), tone(waveTri, at, 0.02, 900+rand.Float64()*700, 600, 0.07))
		}
		s.add(vs...)
	case sndDiceLand:
		s.add(tone(waveNoise, 0, 0.035, 0, 0, 0.2), tone(waveTri, 0, 0.06, 240, 150, 0.22),
			tone(waveNoise, 0.07, 0.03, 0, 0, 0.16), tone(waveTri, 0.07, 0.05, 260, 170, 0.18))
	case sndStep:
		f := 480 + float64(min(step, 12))*18
		s.add(tone(waveTri, 0, 0.045, f, f*0.8, 0.13), tone(waveNoise, 0, 0.012, 0, 0, 0.05))
	case sndStart:
		s.add(notes(waveTri, 0.1, 0.16, 0.24, 784, 1047, 1319)...)
	case sndJump:
		s.add(tone(waveSine, 0, 0.22, 300, 900, 0.18), tone(waveSine, 0.22, 0.24, 900, 260, 0.18))
	case sndCoin:
		s.add(notes(waveTri, 0.07, 0.14, 0.2, 1568, 2093)...)
		s.add(tone(waveSine, 0.07, 0.25, 3136, 3136, 0.06))
	case sndPay:
		s.add(tone(waveSine, 0, 0.16, 230, 150, 0.3), tone(waveNoise, 0, 0.03, 0, 0, 0.08))
	case sndFountain:
		s.add(tone(waveNoise, 0, 0.45, 0, 0, 0.08))
		s.add(notes(waveTri, 0.07, 0.2, 0.22, 784, 988, 1175, 1568, 2093)...)
	case sndBuy:
		s.add(tone(waveNoise, 0, 0.05, 0, 0, 0.22), tone(waveSquare, 0, 0.09, 150, 90, 0.16), tone(waveTri, 0.06, 0.12, 660, 660, 0.12))
	case sndBuild:
		for _, at := range []float64{0, 0.13} {
			s.add(tone(waveNoise, at, 0.025, 0, 0, 0.18), tone(waveTri, at, 0.05, 720, 520, 0.18))
		}
	case sndHotel:
		for _, at := range []float64{0, 0.11, 0.22} {
			s.add(tone(waveNoise, at, 0.025, 0, 0, 0.18), tone(waveTri, at, 0.05, 620, 440, 0.18))
		}
		s.add(notes(waveTri, 0.1, 0.25, 0.22, 1047, 1319, 1568)...)
	case sndRaze:
		s.add(tone(waveNoise, 0, 0.3, 0, 0, 0.14), tone(waveSquare, 0, 0.3, 300, 80, 0.1))
	case sndCardFlip:
		s.add(tone(waveNoise, 0.1, 0.05, 0, 0, 0.12), tone(waveTri, 0.1, 0.09, 600, 1300, 0.14))
	case sndCardPlay:
		s.add(tone(waveSine, 0, 0.42, 380, 1500, 0.17))
		s.add(notes(waveSine, 0.08, 0.2, 0.12, 1568, 2093, 2637)...)
	case sndCardHit:
		s.add(tone(waveSine, 0, 0.14, 900, 280, 0.28), tone(waveNoise, 0, 0.03, 0, 0, 0.1))
	case sndJail:
		s.add(tone(waveNoise, 0, 0.06, 0, 0, 0.2), tone(waveSquare, 0, 0.12, 120, 90, 0.18))
		s.add(notes(waveSquare, 0.18, 0.24, 0.14, 392, 330, 262)...)
	case sndBankrupt:
		s.add(tone(waveSquare, 0, 0.9, 330, 80, 0.18))
	case sndWin:
		s.add(notes(waveTri, 0.13, 0.22, 0.28, 523, 659, 784, 1047)...)
		s.add(tone(waveTri, 0.52, 0.8, 1319, 1319, 0.28))
	case sndLose:
		s.add(notes(waveTri, 0.26, 0.36, 0.24, 392, 330, 262, 196)...)
	case sndYourTurn:
		s.add(notes(waveSine, 0.12, 0.32, 0.16, 880, 1175)...)
	}
}

// sound plays a sound of the board's.
func (p *fortune) sound(snd fortuneSound) { p.snd.fortune(snd, 0) }

// soundsFrom plays what changed between two views of the board: buildings
// up or down, deeds sealed, money, the fountain emptied, jail, a player
// out.
func (p *fortune) soundsFrom(old, now games.FortuneView) {
	if len(old.Cash) != len(now.Cash) {
		return
	}
	built, hotel, razed, bought := false, false, false, false
	for sq := range now.Houses {
		switch h, was := now.Houses[sq], old.Houses[sq]; {
		case h > was && h == games.Hotel:
			hotel = true
		case h > was:
			built = true
		case h < was && now.Owner[sq] >= 0:
			razed = true
		}
		if now.Owner[sq] >= 0 && now.Owner[sq] != old.Owner[sq] {
			bought = true
		}
	}
	gain, loss := false, false
	for i := range now.Cash {
		gain = gain || now.Cash[i] > old.Cash[i]
		loss = loss || now.Cash[i] < old.Cash[i]
		if now.Out[i] && !old.Out[i] {
			p.sound(sndBankrupt)
			return
		}
		if now.InJail[i] && !old.InJail[i] {
			p.sound(sndJail)
		}
	}
	switch {
	case hotel:
		p.sound(sndHotel)
	case built:
		p.sound(sndBuild)
	case razed:
		p.sound(sndRaze)
	case bought:
		p.sound(sndBuy)
	}
	switch {
	case old.Pot > 0 && now.Pot == 0 && gain:
		p.sound(sndFountain)
	case gain && !bought:
		p.sound(sndCoin)
	case loss && !bought && !built && !hotel:
		p.sound(sndPay)
	}
}

// cueSounds plays the sounds of the event the board is showing as it
// goes: the dice landing, each hop, passing Start, a played card landing.
// It is called each frame, before the event's time moves on by dt.
func (p *fortune) cueSounds(dt float64) {
	e := p.cur
	if e == nil {
		return
	}
	crossed := func(at float64) bool { return p.t < at && p.t+dt >= at }
	switch e.Kind {
	case games.EvDice:
		if crossed(diceTime - 0.1) {
			p.sound(sndDiceLand)
		}
	case games.EvWalk:
		for n := range e.Path {
			if crossed(hopTime * float64(n+1)) {
				p.snd.fortune(sndStep, n)
				if e.Path[n] == 0 {
					p.sound(sndStart)
				}
			}
		}
	case games.EvCard:
		if e.Played && crossed(1.15) {
			p.sound(sndCardHit)
		}
	}
}

// startSounds plays the sound an event makes as the board starts showing
// it.
func (p *fortune) startSounds(e *games.FortuneEvent) {
	switch e.Kind {
	case games.EvDice:
		p.sound(sndDice)
	case games.EvJump:
		p.sound(sndJump)
	case games.EvCard:
		if e.Played {
			p.sound(sndCardPlay)
		} else {
			p.sound(sndCardFlip)
		}
	}
}
