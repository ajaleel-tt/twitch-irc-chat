package ui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"twitch-chat/irc"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

type fakeClient struct {
	in   chan irc.Message
	sent []string
	err  error
}

func (f *fakeClient) Incoming() <-chan irc.Message { return f.in }

func (f *fakeClient) SendMessage(_ context.Context, text string) error {
	f.sent = append(f.sent, text)
	return f.err
}

func newTestModel(t *testing.T, client *fakeClient) Model {
	t.Helper()
	m := NewModel(client, context.Background(), "mychannel", "me")
	return update(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
}

func update(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()
	next, _ := m.Update(msg)
	return next.(Model)
}

// send types text, presses Enter, runs the resulting send command and feeds
// its result back into the model.
func send(t *testing.T, m Model, text string) Model {
	t.Helper()
	m.input.SetValue(text)
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatalf("Enter with %q produced no command", text)
	}
	return update(t, next.(Model), cmd())
}

func privmsg(nick, text string) ircMsg {
	return ircMsg(irc.Message{
		Tags:    map[string]string{},
		Nick:    nick,
		Command: "PRIVMSG",
		Channel: "#mychannel",
		Params:  text,
	})
}

func plainLines(m Model) []string {
	out := make([]string, len(m.lines))
	for i, l := range m.lines {
		out[i] = ansi.Strip(l)
	}
	return out
}

func assertLines(t *testing.T, m Model, want ...string) {
	t.Helper()
	got := plainLines(m)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("lines = %q, want %q", got, want)
	}
}

func TestSendSuccessShowsMessage(t *testing.T) {
	client := &fakeClient{}
	m := send(t, newTestModel(t, client), "hello")

	if len(client.sent) != 1 || client.sent[0] != "hello" {
		t.Errorf("client.sent = %q, want [\"hello\"]", client.sent)
	}
	assertLines(t, m, "me: hello")
	if m.input.Value() != "" {
		t.Errorf("input = %q, want it cleared", m.input.Value())
	}
}

func TestSendFailureShowsError(t *testing.T) {
	client := &fakeClient{err: errors.New("connection closed")}
	m := send(t, newTestModel(t, client), "hello")

	assertLines(t, m, "[ERROR] Failed to send: connection closed")
	if len(m.sent) != 0 {
		t.Errorf("m.sent = %v, want empty after failed send", m.sent)
	}
}

func TestEnterIgnoresBlankAndQuits(t *testing.T) {
	client := &fakeClient{}
	m := newTestModel(t, client)

	m.input.SetValue("   ")
	if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter}); cmd != nil {
		t.Errorf("blank input produced a command")
	}

	m.input.SetValue("/quit")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("/quit produced no command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("/quit command did not return tea.QuitMsg")
	}
	if len(client.sent) != 0 {
		t.Errorf("client.sent = %q, want nothing sent", client.sent)
	}
}

func TestServerEchoOfOwnMessageIsDeduped(t *testing.T) {
	m := send(t, newTestModel(t, &fakeClient{}), "hello")
	m = update(t, m, privmsg("me", "hello"))

	assertLines(t, m, "me: hello")
	if len(m.sent) != 0 {
		t.Errorf("m.sent = %v, want entry removed after echo", m.sent)
	}
}

func TestOwnMessageFromAnotherClientIsShown(t *testing.T) {
	m := newTestModel(t, &fakeClient{})
	m = update(t, m, privmsg("me", "from browser"))

	assertLines(t, m, "me: from browser")
}

func TestDedupeCountsRepeatedSends(t *testing.T) {
	m := newTestModel(t, &fakeClient{})
	m = send(t, m, "gg")
	m = send(t, m, "gg")
	m = update(t, m, privmsg("me", "gg"))
	m = update(t, m, privmsg("me", "gg"))
	// A third echo has no pending send left, so it is displayed.
	m = update(t, m, privmsg("me", "gg"))

	assertLines(t, m, "me: gg", "me: gg", "me: gg")
}

func TestOtherUserWithSameTextIsShown(t *testing.T) {
	m := send(t, newTestModel(t, &fakeClient{}), "hello")
	m = update(t, m, privmsg("someone", "hello"))

	assertLines(t, m, "me: hello", "someone: hello")
	if m.sent["hello"] != 1 {
		t.Errorf("m.sent[hello] = %d, want 1 still pending", m.sent["hello"])
	}
}

func TestClearChat(t *testing.T) {
	tests := []struct {
		name   string
		params string
		tags   map[string]string
		want   string
	}{
		{
			name:   "timeout",
			params: "baduser",
			tags:   map[string]string{"ban-duration": "600"},
			want:   "[MOD] baduser timed out for 600s",
		},
		{
			name:   "ban",
			params: "baduser",
			tags:   map[string]string{},
			want:   "[MOD] baduser banned",
		},
		{
			name: "clear",
			tags: map[string]string{},
			want: "[MOD] Chat cleared",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestModel(t, &fakeClient{})
			m = update(t, m, ircMsg(irc.Message{
				Tags:    tt.tags,
				Command: "CLEARCHAT",
				Channel: "#mychannel",
				Params:  tt.params,
			}))
			assertLines(t, m, tt.want)
		})
	}
}
