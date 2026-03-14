package ui

import (
	"hash/fnv"

	"github.com/charmbracelet/lipgloss"
)

var (
	systemStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Italic(true)
	sepStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	inputStyle  = lipgloss.NewStyle().BorderTop(true).BorderStyle(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color("240"))
)

// Default colors for users who have no color set.
var defaultColors = []string{
	"#FF0000", "#0000FF", "#00FF00", "#B22222", "#FF7F50",
	"#9ACD32", "#FF4500", "#2E8B57", "#DAA520", "#D2691E",
	"#5F9EA0", "#1E90FF", "#FF69B4", "#8A2BE2", "#00FF7F",
}

func usernameStyle(color string, name string) lipgloss.Style {
	if color == "" {
		color = fallbackColor(name)
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Bold(true)
}

func fallbackColor(name string) string {
	h := fnv.New32a()
	h.Write([]byte(name))
	return defaultColors[h.Sum32()%uint32(len(defaultColors))]
}

func badgePrefix(badges string) string {
	if badges == "" {
		return ""
	}
	out := ""
	for _, b := range splitBadges(badges) {
		switch {
		case b == "broadcaster/1":
			out += lipgloss.NewStyle().Foreground(lipgloss.Color("#E91916")).Render("*") + " "
		case b == "moderator/1":
			out += lipgloss.NewStyle().Foreground(lipgloss.Color("#00AD03")).Render("@") + " "
		case b == "vip/1":
			out += lipgloss.NewStyle().Foreground(lipgloss.Color("#E005B9")).Render("+") + " "
		case len(b) > 11 && b[:11] == "subscriber/":
			out += lipgloss.NewStyle().Foreground(lipgloss.Color("#8205B4")).Render("~") + " "
		}
	}
	return out
}

func splitBadges(s string) []string {
	var badges []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == ',' {
			badges = append(badges, s[start:i])
			start = i + 1
		}
	}
	badges = append(badges, s[start:])
	return badges
}
