package ui

import (
	"sort"
	"strings"
	"time"
)

// presence tracks who is in the channel by merging two sources: IRC JOIN/PART
// (fast but lossy, and absent above 1,000 chatters) and periodic Get Chatters
// snapshots (complete but delayed).
type presence struct {
	present map[string]bool
	ircSeen map[string]time.Time // time of the last IRC JOIN/PART per login
	seeded  bool                 // whether the first snapshot has been applied
	grace   time.Duration        // how long an IRC event outranks snapshots
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
	login = strings.ToLower(login)
	p.ircSeen[login] = now
	if !p.present[login] {
		return false
	}
	delete(p.present, login)
	return true
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
	in := make(map[string]bool, len(snapshot))
	for _, l := range snapshot {
		in[strings.ToLower(l)] = true
	}
	if !p.seeded {
		p.seeded = true
		p.present = in
		return nil, nil
	}
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
	for l, t := range p.ircSeen {
		if now.Sub(t) >= p.grace {
			delete(p.ircSeen, l)
		}
	}
	sort.Strings(joined)
	sort.Strings(left)
	return joined, left
}
