package ui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	"twitch-chat/helix"
	"twitch-chat/irc"
)

func newTestModel(opts Options) Model {
	m := NewModel(nil, context.Background(), "chan", "me", opts)
	m.now = func() time.Time { return t0 }
	return m
}

func pollingOpts() Options {
	return Options{Helix: helix.New("tok"), PollInterval: 30 * time.Second}
}

func TestHandleIRCJoinPart(t *testing.T) {
	tests := []struct {
		name      string
		hideJoins bool
		wantLines int
	}{
		{"shown by default", false, 2},
		{"hidden with HideJoins", true, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestModel(Options{HideJoins: tt.hideJoins})
			m.handleIRC(irc.Message{Command: "JOIN", Nick: "alice"})
			m.handleIRC(irc.Message{Command: "PART", Nick: "alice"})
			if len(m.lines) != tt.wantLines {
				t.Fatalf("got %d lines, want %d: %q", len(m.lines), tt.wantLines, m.lines)
			}
			if tt.wantLines > 0 {
				if !strings.Contains(m.lines[0], "14:00:00 alice joined") || !strings.Contains(m.lines[1], "14:00:00 alice left") {
					t.Errorf("unexpected lines: %q", m.lines)
				}
			}
		})
	}
}

func TestHandleIRCDuplicateJoin(t *testing.T) {
	m := newTestModel(Options{})
	m.handleIRC(irc.Message{Command: "JOIN", Nick: "alice"})
	m.handleIRC(irc.Message{Command: "JOIN", Nick: "alice"})
	if len(m.lines) != 1 {
		t.Fatalf("got %d lines, want 1: %q", len(m.lines), m.lines)
	}
}

func TestHandleIRCNamesSeeds(t *testing.T) {
	m := newTestModel(Options{})
	m.handleIRC(irc.ParseMessage(":me.tmi.twitch.tv 353 me = #chan :alice bob"))
	m.handleIRC(irc.Message{Command: "JOIN", Nick: "bob"})
	if len(m.lines) != 0 {
		t.Fatalf("join of NAMES user should be silent, got %q", m.lines)
	}
}

func TestHandleIRCHideJoinsKeepsChat(t *testing.T) {
	m := newTestModel(Options{HideJoins: true})
	m.handleIRC(irc.Message{Command: "PRIVMSG", Nick: "alice", Params: "hi", Tags: map[string]string{"display-name": "alice"}})
	if len(m.lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(m.lines))
	}
}

func TestHandleChattersDiffs(t *testing.T) {
	m := newTestModel(pollingOpts())
	if cmd := m.handleChatters(chattersMsg{logins: []string{"alice", "bob"}}); cmd == nil {
		t.Fatal("expected next poll to be scheduled")
	}
	if len(m.lines) != 1 || !strings.Contains(m.lines[0], "2 chatters in channel") {
		t.Fatalf("first poll lines: %q", m.lines)
	}
	m.handleChatters(chattersMsg{logins: []string{"bob", "carol"}})
	got := strings.Join(m.lines[1:], "\n")
	if !strings.Contains(got, "14:00:00 carol joined") || !strings.Contains(got, "14:00:00 alice left") {
		t.Errorf("unexpected lines: %q", m.lines)
	}
}

func TestHandleChattersHideJoins(t *testing.T) {
	opts := pollingOpts()
	opts.HideJoins = true
	m := newTestModel(opts)
	m.handleChatters(chattersMsg{logins: []string{"alice"}})
	m.handleChatters(chattersMsg{logins: []string{"bob"}})
	if len(m.lines) != 0 {
		t.Errorf("expected no lines, got %q", m.lines)
	}
}

func TestHandleChattersAuthErrorStops(t *testing.T) {
	m := newTestModel(pollingOpts())
	if cmd := m.handleChatters(chattersMsg{err: &helix.APIError{Status: 403, Message: "not a moderator"}}); cmd != nil {
		t.Error("expected polling to stop")
	}
	if m.polling || len(m.lines) != 1 || !strings.Contains(m.lines[0], "not a moderator") {
		t.Errorf("polling=%v lines=%q", m.polling, m.lines)
	}
	if cmd := m.handleChatters(chattersMsg{logins: []string{"x"}}); cmd != nil {
		t.Error("stopped poller should not reschedule")
	}
}

func TestHandleChattersTransientErrorDeduped(t *testing.T) {
	m := newTestModel(pollingOpts())
	err := errors.New("timeout")
	for range 3 {
		if cmd := m.handleChatters(chattersMsg{err: err}); cmd == nil {
			t.Fatal("transient error should keep polling")
		}
	}
	if len(m.lines) != 1 {
		t.Errorf("repeated error should print once, got %q", m.lines)
	}
}

func TestHandleValidate(t *testing.T) {
	m := newTestModel(pollingOpts())
	if cmd := m.handleValidate(validateMsg{}); cmd == nil {
		t.Error("valid token should schedule next validation")
	}
	if cmd := m.handleValidate(validateMsg{err: &helix.APIError{Status: 401}}); cmd != nil || m.polling {
		t.Error("invalid token should stop polling")
	}
}

func TestStartupNotes(t *testing.T) {
	m := newTestModel(Options{StartupNotes: []string{"hello"}})
	if len(m.lines) != 1 || !strings.Contains(m.lines[0], "hello") {
		t.Errorf("lines = %q", m.lines)
	}
}
