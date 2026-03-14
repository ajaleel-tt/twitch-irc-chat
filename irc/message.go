package irc

import (
	"strings"
)

// Message represents a parsed IRC message with optional Twitch tags.
type Message struct {
	Tags    map[string]string
	Source  string
	Nick    string
	Command string
	Channel string
	Params  string
}

// ChatMessage is a display-friendly representation of a PRIVMSG.
type ChatMessage struct {
	Username    string
	DisplayName string
	Color       string
	Message     string
	IsMod       bool
	Badges      string
}

// ParseMessage parses a raw IRC line into a Message struct.
// Format: [@tags] [:source] COMMAND [#channel] [:params]
func ParseMessage(raw string) Message {
	raw = strings.TrimRight(raw, "\r\n")
	m := Message{Tags: make(map[string]string)}
	pos := 0

	// Parse tags
	if len(raw) > pos && raw[pos] == '@' {
		end := strings.Index(raw[pos:], " ")
		if end == -1 {
			return m
		}
		tagStr := raw[pos+1 : pos+end]
		parseTags(tagStr, m.Tags)
		pos += end + 1
	}

	// Parse source
	if len(raw) > pos && raw[pos] == ':' {
		end := strings.Index(raw[pos:], " ")
		if end == -1 {
			m.Source = raw[pos+1:]
			return m
		}
		m.Source = raw[pos+1 : pos+end]
		pos += end + 1

		if idx := strings.Index(m.Source, "!"); idx != -1 {
			m.Nick = m.Source[:idx]
		}
	}

	// Parse command
	rest := raw[pos:]
	parts := strings.SplitN(rest, " ", 2)
	m.Command = parts[0]
	if len(parts) == 1 {
		return m
	}
	rest = parts[1]

	// Parse channel and trailing params
	if strings.HasPrefix(rest, "#") {
		chParts := strings.SplitN(rest, " ", 2)
		m.Channel = chParts[0]
		if len(chParts) == 2 {
			rest = chParts[1]
		} else {
			return m
		}
	}

	if strings.HasPrefix(rest, ":") {
		m.Params = rest[1:]
	} else {
		m.Params = rest
	}

	return m
}

func parseTags(raw string, tags map[string]string) {
	for _, pair := range strings.Split(raw, ";") {
		kv := strings.SplitN(pair, "=", 2)
		if len(kv) == 2 {
			tags[kv[0]] = unescapeTagValue(kv[1])
		} else {
			tags[kv[0]] = ""
		}
	}
}

func unescapeTagValue(s string) string {
	r := strings.NewReplacer(
		"\\:", ";",
		"\\s", " ",
		"\\\\", "\\",
		"\\r", "\r",
		"\\n", "\n",
	)
	return r.Replace(s)
}

// ToChatMessage converts a parsed IRC Message to a display-friendly ChatMessage.
func (m Message) ToChatMessage() ChatMessage {
	cm := ChatMessage{
		Username:    m.Nick,
		DisplayName: m.Tags["display-name"],
		Color:       m.Tags["color"],
		Message:     m.Params,
		Badges:      m.Tags["badges"],
	}
	if cm.DisplayName == "" {
		cm.DisplayName = m.Nick
	}
	if m.Tags["mod"] == "1" {
		cm.IsMod = true
	}
	return cm
}
