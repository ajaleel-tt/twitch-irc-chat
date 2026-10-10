package ui

import (
	"strings"
	"testing"
	"twitch-chat/irc"
)

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
			m := Model{opts: Options{HideJoins: tt.hideJoins}, sent: map[string]int{}}
			m.handleIRC(irc.Message{Command: "JOIN", Nick: "alice"})
			m.handleIRC(irc.Message{Command: "PART", Nick: "alice"})
			if len(m.lines) != tt.wantLines {
				t.Fatalf("got %d lines, want %d: %q", len(m.lines), tt.wantLines, m.lines)
			}
			if tt.wantLines > 0 {
				if !strings.Contains(m.lines[0], "alice joined") || !strings.Contains(m.lines[1], "alice left") {
					t.Errorf("unexpected lines: %q", m.lines)
				}
			}
		})
	}
}

func TestHandleIRCHideJoinsKeepsChat(t *testing.T) {
	m := Model{opts: Options{HideJoins: true}, sent: map[string]int{}}
	m.handleIRC(irc.Message{Command: "PRIVMSG", Nick: "alice", Params: "hi", Tags: map[string]string{"display-name": "alice"}})
	if len(m.lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(m.lines))
	}
}
