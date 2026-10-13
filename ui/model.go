package ui

import (
	"context"
	"fmt"
	"strings"
	"time"
	"twitch-chat/helix"
	"twitch-chat/irc"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const maxLines = 1000

type ircMsg irc.Message

// ChatClient is the subset of the IRC client the UI depends on.
type ChatClient interface {
	Incoming() <-chan irc.Message
	SendMessage(ctx context.Context, text string) error
}

type sendResult struct {
	text string
	err  error
}

// chattersMsg carries the result of one Get Chatters poll.
type chattersMsg struct {
	logins []string
	err    error
}

// validateMsg carries the result of the hourly token validation.
type validateMsg struct {
	err error
}

const (
	pollTimeout      = 10 * time.Second
	validateInterval = time.Hour
	minPollGrace     = time.Minute
)

// Options configures optional display behavior of the chat UI.
type Options struct {
	HideJoins bool // suppress "joined/left the channel" lines

	// Chatter polling. Enabled when Helix is non-nil and PollInterval > 0.
	Helix         *helix.Client
	PollInterval  time.Duration
	BroadcasterID string
	ModeratorID   string

	StartupNotes []string // system lines shown when the UI starts
}

// Model is the Bubbletea model for the chat UI.
type Model struct {
	viewport viewport.Model
	input    textinput.Model
	client   ChatClient
	ctx      context.Context
	lines    []string
	sent     map[string]int // text -> count of pending local echoes to dedupe
	channel  string
	nick     string
	opts     Options
	ready    bool

	presence    presence
	polling     bool   // whether chatter polls are still being scheduled
	lastPollErr string // last poll error shown, to avoid repeating it
	now         func() time.Time
}

// NewModel creates a new chat UI model.
func NewModel(client ChatClient, ctx context.Context, channel, nick string, opts Options) Model {
	ti := textinput.New()
	ti.Placeholder = "Type a message..."
	ti.Focus()
	ti.CharLimit = 500

	m := Model{
		client:   client,
		ctx:      ctx,
		input:    ti,
		channel:  channel,
		nick:     nick,
		opts:     opts,
		lines:    []string{},
		sent:     make(map[string]int),
		presence: newPresence(max(minPollGrace, 2*opts.PollInterval)),
		polling:  opts.Helix != nil && opts.PollInterval > 0,
		now:      time.Now,
	}
	for _, note := range opts.StartupNotes {
		m.appendLine(systemStyle.Render(note))
	}
	return m
}

func waitForIRC(incoming <-chan irc.Message) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-incoming
		if !ok {
			return tea.Quit()
		}
		return ircMsg(msg)
	}
}

// pollChatters fetches the chatter list after delay. The next poll is only
// scheduled once this one's result is handled, so polls never overlap.
func (m Model) pollChatters(delay time.Duration) tea.Cmd {
	h, ctx := m.opts.Helix, m.ctx
	bID, modID := m.opts.BroadcasterID, m.opts.ModeratorID
	fetch := func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, pollTimeout)
		defer cancel()
		logins, err := h.Chatters(ctx, bID, modID)
		return chattersMsg{logins: logins, err: err}
	}
	if delay <= 0 {
		return fetch
	}
	return tea.Tick(delay, func(time.Time) tea.Msg { return fetch() })
}

func (m Model) validateLater() tea.Cmd {
	h, ctx := m.opts.Helix, m.ctx
	return tea.Tick(validateInterval, func(time.Time) tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, pollTimeout)
		defer cancel()
		_, err := h.Validate(ctx)
		return validateMsg{err: err}
	})
}

//goland:noinspection GoMixedReceiverTypes
func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{
		textinput.Blink,
		waitForIRC(m.client.Incoming()),
	}
	if m.polling {
		cmds = append(cmds, m.pollChatters(0), m.validateLater())
	}
	return tea.Batch(cmds...)
}

//goland:noinspection GoMixedReceiverTypes
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "enter":
			text := strings.TrimSpace(m.input.Value())
			if text == "" {
				return m, nil
			}
			if text == "/quit" {
				return m, tea.Quit
			}
			m.input.Reset()
			client := m.client
			ctx := m.ctx
			return m, func() tea.Msg {
				err := client.SendMessage(ctx, text)
				return sendResult{text: text, err: err}
			}
		}

	case tea.WindowSizeMsg:
		inputHeight := 3 // border + input line + padding
		if !m.ready {
			m.viewport = viewport.New(msg.Width, msg.Height-inputHeight)
			m.viewport.SetContent(m.wrappedContent())
			m.ready = true
		} else {
			m.viewport.Width = msg.Width
			m.viewport.Height = msg.Height - inputHeight
			m.viewport.SetContent(m.wrappedContent())
		}

	case sendResult:
		if msg.err == nil {
			name := usernameStyle("", m.nick).Render(m.nick)
			sep := sepStyle.Render(": ")
			m.appendLine(name + sep + msg.text)
			m.sent[msg.text]++
		} else {
			m.appendLine(systemStyle.Render("[ERROR] Failed to send: " + msg.err.Error()))
		}
		if m.ready {
			m.viewport.SetContent(m.wrappedContent())
			m.viewport.GotoBottom()
		}

	case ircMsg:
		m.handleIRC(irc.Message(msg))
		m.refresh()
		cmds = append(cmds, waitForIRC(m.client.Incoming()))

	case chattersMsg:
		cmds = append(cmds, m.handleChatters(msg))
		m.refresh()

	case validateMsg:
		cmds = append(cmds, m.handleValidate(msg))
		m.refresh()
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	cmds = append(cmds, cmd)

	m.viewport, cmd = m.viewport.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

//goland:noinspection GoMixedReceiverTypes
func (m *Model) handleIRC(msg irc.Message) {
	switch msg.Command {
	case "PRIVMSG":
		// Dedupe server echo of messages we sent from this terminal
		if strings.EqualFold(msg.Nick, m.nick) {
			if count, ok := m.sent[msg.Params]; ok && count > 0 {
				m.sent[msg.Params]--
				if m.sent[msg.Params] == 0 {
					delete(m.sent, msg.Params)
				}
				return
			}
		}
		cm := msg.ToChatMessage()
		badges := badgePrefix(cm.Badges)
		name := usernameStyle(cm.Color, cm.DisplayName).Render(cm.DisplayName)
		sep := sepStyle.Render(": ")
		line := badges + name + sep + cm.Message
		m.appendLine(line)

	case "JOIN":
		if msg.Nick != "" && m.presence.join(msg.Nick, m.now()) {
			m.joinLine(msg.Nick, "joined")
		}

	case "PART":
		if msg.Nick != "" && m.presence.part(msg.Nick, m.now()) {
			m.joinLine(msg.Nick, "left")
		}

	case "353": // NAMES: "<nick> = #channel :name1 name2 ..."
		if i := strings.LastIndex(msg.Params, " :"); i != -1 {
			m.presence.seed(strings.Fields(msg.Params[i+2:]))
		}

	case "NOTICE":
		m.appendLine(systemStyle.Render("[NOTICE] " + msg.Params))

	case "USERNOTICE":
		sysMsg := msg.Tags["system-msg"]
		if sysMsg != "" {
			m.appendLine(systemStyle.Render("[*] " + sysMsg))
		}
		if msg.Params != "" {
			cm := msg.ToChatMessage()
			name := usernameStyle(cm.Color, cm.DisplayName).Render(cm.DisplayName)
			sep := sepStyle.Render(": ")
			m.appendLine("  " + name + sep + cm.Message)
		}

	case "CLEARCHAT":
		target := msg.Params
		dur := msg.Tags["ban-duration"]
		if target != "" && dur != "" {
			m.appendLine(systemStyle.Render(fmt.Sprintf("[MOD] %s timed out for %ss", target, dur)))
		} else if target != "" {
			m.appendLine(systemStyle.Render(fmt.Sprintf("[MOD] %s banned", target)))
		} else {
			m.appendLine(systemStyle.Render("[MOD] Chat cleared"))
		}

	case "CLEARMSG":
		m.appendLine(systemStyle.Render("[MOD] Message deleted"))
	}
}

// handleChatters applies a poll result and returns the command for the next
// poll, or nil if polling has stopped.
func (m *Model) handleChatters(msg chattersMsg) tea.Cmd {
	if !m.polling {
		return nil
	}
	if msg.err != nil {
		if helix.IsAuthError(msg.err) {
			m.polling = false
			m.appendLine(systemStyle.Render("[WARN] Chatter polling stopped: " + msg.err.Error()))
			return nil
		}
		if text := msg.err.Error(); text != m.lastPollErr {
			m.lastPollErr = text
			m.appendLine(systemStyle.Render("[WARN] Chatter poll failed: " + text))
		}
		return m.pollChatters(m.opts.PollInterval)
	}
	m.lastPollErr = ""
	first := !m.presence.seeded
	joined, left := m.presence.reconcile(msg.logins, m.now())
	if first && !m.opts.HideJoins {
		m.appendLine(systemStyle.Render(fmt.Sprintf("* %d chatters currently tracked", len(m.presence.present))))
	}
	for _, l := range joined {
		m.joinLine(l, "joined")
	}
	for _, l := range left {
		m.joinLine(l, "left")
	}
	return m.pollChatters(m.opts.PollInterval)
}

// handleValidate stops polling if the token is no longer valid, otherwise
// schedules the next hourly validation.
func (m *Model) handleValidate(msg validateMsg) tea.Cmd {
	if !m.polling {
		return nil
	}
	if helix.IsAuthError(msg.err) {
		m.polling = false
		m.appendLine(systemStyle.Render("[WARN] Token is no longer valid; chatter polling stopped"))
		return nil
	}
	return m.validateLater()
}

// joinLine prints a timestamped join/leave line unless they are hidden.
func (m *Model) joinLine(login, verb string) {
	if m.opts.HideJoins {
		return
	}
	ts := m.now().Format("15:04:05")
	m.appendLine(systemStyle.Render(fmt.Sprintf("* %s %s the channel (detected at %s)", login, verb, ts)))
}

// refresh re-renders the viewport, staying pinned to the bottom if it was.
func (m *Model) refresh() {
	if !m.ready {
		return
	}
	atBottom := m.viewport.AtBottom()
	m.viewport.SetContent(m.wrappedContent())
	if atBottom {
		m.viewport.GotoBottom()
	}
}

//goland:noinspection GoMixedReceiverTypes
func (m *Model) appendLine(line string) {
	m.lines = append(m.lines, line)
	if len(m.lines) > maxLines {
		m.lines = m.lines[len(m.lines)-maxLines:]
	}
}

//goland:noinspection GoMixedReceiverTypes
func (m *Model) wrappedContent() string {
	width := m.viewport.Width
	if width <= 0 {
		return strings.Join(m.lines, "\n")
	}
	var b strings.Builder
	for i, line := range m.lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(ansi.Wordwrap(line, width, ""))
	}
	return b.String()
}

//goland:noinspection GoMixedReceiverTypes
func (m Model) View() string {
	if !m.ready {
		return "Connecting to " + m.channel + "..."
	}
	inputBox := inputStyle.Width(m.viewport.Width).Render(m.input.View())
	return lipgloss.JoinVertical(lipgloss.Left, m.viewport.View(), inputBox)
}
