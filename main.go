package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"twitch-chat/irc"
	"twitch-chat/ui"

	tea "github.com/charmbracelet/bubbletea"
)

// anonymousNick is Twitch's read-only login. Twitch accepts any justinfan
// nick without a token, but drops everything it sends.
const anonymousNick = "justinfan12345"

// resolveAuth picks the login to use. With no token and no nick it returns
// the anonymous read-only login. Supplying only one of the two is an error:
// a token without a nick would log in anonymously, so sent messages would
// look delivered but never appear in chat.
func resolveAuth(token, nick string) (string, string, bool, error) {
	switch {
	case token == "" && nick == "":
		return "", anonymousNick, true, nil
	case token == "":
		return "", "", false, errors.New("--nick needs an OAuth token too. Use --token or set TWITCH_TOKEN, or drop --nick to read chat anonymously")
	case nick == "":
		return "", "", false, errors.New("--token needs a username too. Use --nick or set TWITCH_NICK")
	}
	return token, nick, false, nil
}

func main() {
	channel := flag.String("channel", "", "Twitch channel to join (required)")
	token := flag.String("token", "", "OAuth token (or set TWITCH_TOKEN); omit with --nick to read anonymously")
	nick := flag.String("nick", "", "Twitch username (or set TWITCH_NICK)")
	flag.Parse()

	if *channel == "" {
		fmt.Fprintln(os.Stderr, "Usage: twitch-chat --channel <channel> [--token <token> --nick <nick>]")
		os.Exit(1)
	}

	if *token == "" {
		*token = os.Getenv("TWITCH_TOKEN")
	}
	if *nick == "" {
		*nick = os.Getenv("TWITCH_NICK")
	}

	tok, name, anonymous, err := resolveAuth(*token, *nick)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		fmt.Fprintln(os.Stderr, "Generate a token at: https://twitchtokengenerator.com")
		os.Exit(1)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := irc.NewClient(name, *channel)
	if err := client.Connect(ctx, tok); err != nil {
		fmt.Fprintf(os.Stderr, "Error connecting: %v\n", err)
		os.Exit(1)
	}
	defer client.Close()

	model := ui.NewModel(client, ctx, *channel, name, anonymous)
	p := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
