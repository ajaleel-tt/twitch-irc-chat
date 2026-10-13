package ui

import (
	"reflect"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 12, 14, 0, 0, 0, time.UTC)

func TestPresenceIRCDedupe(t *testing.T) {
	p := newPresence(time.Minute)
	if !p.join("Alice", t0) {
		t.Error("first join should be new")
	}
	if p.join("alice", t0) {
		t.Error("duplicate join should not be new")
	}
	if !p.part("alice", t0) {
		t.Error("part of present user should report true")
	}
	if p.part("alice", t0) {
		t.Error("duplicate part should report false")
	}
}

func TestPresenceSeedSuppressesJoin(t *testing.T) {
	p := newPresence(time.Minute)
	p.seed([]string{"alice", "bob"})
	if p.join("alice", t0) {
		t.Error("join of NAMES-seeded user should not be new")
	}
}

func TestPresenceReconcile(t *testing.T) {
	p := newPresence(time.Minute)
	if j, l := p.reconcile([]string{"alice", "bob"}, t0); j != nil || l != nil {
		t.Fatalf("first snapshot should only seed, got %v %v", j, l)
	}
	j, l := p.reconcile([]string{"bob", "dave", "carol"}, t0.Add(30*time.Second))
	if !reflect.DeepEqual(j, []string{"carol", "dave"}) || !reflect.DeepEqual(l, []string{"alice"}) {
		t.Errorf("got joined=%v left=%v", j, l)
	}
}

func TestPresenceGraceAfterIRCPart(t *testing.T) {
	p := newPresence(time.Minute)
	p.reconcile([]string{"alice"}, t0)
	p.part("alice", t0.Add(5*time.Second))

	// A lagging snapshot still lists alice; she must not "rejoin".
	if j, l := p.reconcile([]string{"alice"}, t0.Add(30*time.Second)); j != nil || l != nil {
		t.Errorf("within grace: joined=%v left=%v", j, l)
	}
	// After the grace window, the snapshot wins.
	j, _ := p.reconcile([]string{"alice"}, t0.Add(2*time.Minute))
	if !reflect.DeepEqual(j, []string{"alice"}) {
		t.Errorf("after grace: joined=%v", j)
	}
}

func TestPresenceGraceAfterIRCJoin(t *testing.T) {
	p := newPresence(time.Minute)
	p.reconcile(nil, t0)
	p.join("bob", t0.Add(5*time.Second))

	// Snapshot hasn't caught up with bob yet; he must not "leave".
	if _, l := p.reconcile(nil, t0.Add(30*time.Second)); l != nil {
		t.Errorf("within grace: left=%v", l)
	}
	if _, l := p.reconcile(nil, t0.Add(2*time.Minute)); !reflect.DeepEqual(l, []string{"bob"}) {
		t.Errorf("after grace: left=%v", l)
	}
}
