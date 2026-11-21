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

	if err := run(*channel, tok, name, anonymous); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

// run connects and drives the UI. It returns errors instead of exiting so
// its deferred cleanup (closing the socket, cancelling the context) runs
// before main calls os.Exit, which skips defers.
func run(channel, token, nick string, anonymous bool) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := irc.NewClient(nick, channel)
	// Deferred before Connect so a connection that dials but then fails a
	// handshake write is still closed. Close is a no-op if nothing was dialed.
	defer client.Close()
	if err := client.Connect(ctx, token); err != nil {
		return fmt.Errorf("connecting: %w", err)
	}

	model := ui.NewModel(client, ctx, channel, nick, anonymous)
	if _, err := tea.NewProgram(model, tea.WithAltScreen()).Run(); err != nil {
		return err
	}
	return nil
}
