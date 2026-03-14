package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"twitch-chat/irc"
	"twitch-chat/ui"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	channel := flag.String("channel", "", "Twitch channel to join (required)")
	token := flag.String("token", "", "OAuth token (or set TWITCH_TOKEN)")
	nick := flag.String("nick", "", "Twitch username (or set TWITCH_NICK)")
	flag.Parse()

	if *channel == "" {
		fmt.Fprintln(os.Stderr, "Usage: twitch-chat --channel <channel> [--token <token>] [--nick <nick>]")
		os.Exit(1)
	}

	if *token == "" {
		*token = os.Getenv("TWITCH_TOKEN")
	}
	if *token == "" {
		fmt.Fprintln(os.Stderr, "Error: OAuth token required. Use --token or set TWITCH_TOKEN env var.")
		fmt.Fprintln(os.Stderr, "Generate one at: https://twitchtokengenerator.com")
		os.Exit(1)
	}

	if *nick == "" {
		*nick = os.Getenv("TWITCH_NICK")
	}
	if *nick == "" {
		*nick = "justinfan12345" // anonymous read-only
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := irc.NewClient(*nick, *channel)
	if err := client.Connect(ctx, *token); err != nil {
		fmt.Fprintf(os.Stderr, "Error connecting: %v\n", err)
		os.Exit(1)
	}
	defer client.Close()

	model := ui.NewModel(client, ctx, *channel, *nick)
	p := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
