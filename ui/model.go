package ui

import (
	"context"
	"fmt"
	"strings"
	"twitch-chat/irc"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const maxLines = 1000

type ircMsg irc.Message

type sendResult struct {
	text string
	err  error
}

// Model is the Bubbletea model for the chat UI.
type Model struct {
	viewport viewport.Model
	input    textinput.Model
	client   *irc.Client
	ctx      context.Context
	lines    []string
	sent     map[string]int // text -> count of pending local echoes to dedupe
	channel  string
	nick     string
	ready    bool
}

// NewModel creates a new chat UI model.
func NewModel(client *irc.Client, ctx context.Context, channel, nick string) Model {
	ti := textinput.New()
	ti.Placeholder = "Type a message..."
	ti.Focus()
	ti.CharLimit = 500

	return Model{
		client:  client,
		ctx:     ctx,
		input:   ti,
		channel: channel,
		nick:    nick,
		lines:   []string{},
		sent:    make(map[string]int),
	}
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

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		textinput.Blink,
		waitForIRC(m.client.Incoming()),
	)
}

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
			m.viewport.SetContent(strings.Join(m.lines, "\n"))
			m.ready = true
		} else {
			m.viewport.Width = msg.Width
			m.viewport.Height = msg.Height - inputHeight
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
			m.viewport.SetContent(strings.Join(m.lines, "\n"))
			m.viewport.GotoBottom()
		}

	case ircMsg:
		m.handleIRC(irc.Message(msg))
		if m.ready {
			atBottom := m.viewport.AtBottom()
			m.viewport.SetContent(strings.Join(m.lines, "\n"))
			if atBottom {
				m.viewport.GotoBottom()
			}
		}
		cmds = append(cmds, waitForIRC(m.client.Incoming()))
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	cmds = append(cmds, cmd)

	m.viewport, cmd = m.viewport.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

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
		if msg.Nick != "" {
			m.appendLine(systemStyle.Render(fmt.Sprintf("* %s joined the channel", msg.Nick)))
		}

	case "PART":
		if msg.Nick != "" {
			m.appendLine(systemStyle.Render(fmt.Sprintf("* %s left the channel", msg.Nick)))
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

func (m *Model) appendLine(line string) {
	m.lines = append(m.lines, line)
	if len(m.lines) > maxLines {
		m.lines = m.lines[len(m.lines)-maxLines:]
	}
}

func (m Model) View() string {
	if !m.ready {
		return "Connecting to " + m.channel + "..."
	}
	inputBox := inputStyle.Width(m.viewport.Width).Render(m.input.View())
	return lipgloss.JoinVertical(lipgloss.Left, m.viewport.View(), inputBox)
}
