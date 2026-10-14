package ui

import (
	"sort"
	"strings"
	"time"
)

// presence estimates who is in the channel using IRC JOIN/PART and delayed
// Get Chatters snapshots. Neither source guarantees every visit is observed.
type presence struct {
	present map[string]bool
	ircSeen map[string]time.Time // time of the last IRC JOIN/PART per login
	seeded  bool                 // whether the first snapshot has been applied
	grace   time.Duration        // how long an IRC event outranks snapshots
	pruneAt time.Time            // next IRC history cleanup, even without polls
}

func newPresence(grace time.Duration) presence {
	return presence{
		present: make(map[string]bool),
		ircSeen: make(map[string]time.Time),
		grace:   grace,
	}
}

// join records an IRC JOIN and reports whether login was newly present.
func (p *presence) join(login string, now time.Time) bool {
	p.pruneIRC(now)
	login = strings.ToLower(login)
	p.ircSeen[login] = now
	if p.present[login] {
		return false
	}
	p.present[login] = true
	return true
}

// part records an IRC PART and reports whether login was present.
func (p *presence) part(login string, now time.Time) bool {
	p.pruneIRC(now)
	login = strings.ToLower(login)
	p.ircSeen[login] = now
	if !p.present[login] {
		return false
	}
	delete(p.present, login)
	return true
}

// pruneIRC also runs on membership events, so disabled or stopped polling
// cannot leave an ever-growing history of departed users. Scan at most once
// per grace window rather than once per JOIN/PART in a busy channel.
func (p *presence) pruneIRC(now time.Time) {
	if now.Before(p.pruneAt) {
		return
	}
	for login, seen := range p.ircSeen {
		if now.Sub(seen) >= p.grace {
			delete(p.ircSeen, login)
		}
	}
	p.pruneAt = now.Add(p.grace)
}

// seed silently marks logins as present (e.g. from the NAMES list).
func (p *presence) seed(logins []string) {
	for _, l := range logins {
		p.present[strings.ToLower(l)] = true
	}
}

// reconcile applies a Get Chatters snapshot and returns who joined and left
// since the last known state. The first snapshot only seeds. Users with a
// recent IRC event are skipped, because the snapshot lags behind IRC and would
// otherwise undo (and re-announce) what IRC already reported.
func (p *presence) reconcile(snapshot []string, now time.Time) (joined, left []string) {
	p.pruneIRC(now)
	in := make(map[string]bool, len(snapshot))
	for _, l := range snapshot {
		in[strings.ToLower(l)] = true
	}
	first := !p.seeded
	p.seeded = true
	recent := func(login string) bool {
		t, ok := p.ircSeen[login]
		return ok && now.Sub(t) < p.grace
	}
	for l := range in {
		if !p.present[l] && !recent(l) {
			p.present[l] = true
			joined = append(joined, l)
		}
	}
	for l := range p.present {
		if !in[l] && !recent(l) {
			delete(p.present, l)
			left = append(left, l)
		}
	}
	if first {
		// Apply the same grace rules when seeding, but do not announce the
		// existing occupants as new joins or departures.
		return nil, nil
	}
	sort.Strings(joined)
	sort.Strings(left)
	return joined, left
}
