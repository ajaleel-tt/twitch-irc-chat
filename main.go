package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"
	"twitch-chat/helix"
	"twitch-chat/irc"
	"twitch-chat/ui"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	channel := flag.String("channel", "", "Twitch channel to join (required)")
	token := flag.String("token", "", "OAuth token (or set TWITCH_TOKEN)")
	nick := flag.String("nick", "", "Twitch username (or set TWITCH_NICK)")
	hideJoins := flag.Bool("hide-joins", false, "Hide join/leave messages (useful in large channels)")
	pollChatters := flag.Duration("poll-chatters", 0, "Poll the delayed chatter list at this interval (e.g. 30s) to supplement IRC join/leave tracking; needs moderator:read:chatters")
	flag.Parse()

	if *channel == "" {
		fmt.Fprintln(os.Stderr, "Usage: twitch-chat --channel <channel> [--token <token>] [--nick <nick>] [--hide-joins] [--poll-chatters <interval>]")
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

	opts := ui.Options{HideJoins: *hideJoins}
	if *pollChatters > 0 && !*hideJoins {
		setupChatterPolling(ctx, &opts, *token, *channel, *pollChatters)
	}

	model := ui.NewModel(client, ctx, *channel, *nick, opts)
	p := tea.NewProgram(model, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

const minPollInterval = 10 * time.Second

// setupChatterPolling validates the token and resolves the IDs needed for Get
// Chatters. Any problem becomes a startup note and polling stays off; chat
// itself keeps working.
func setupChatterPolling(ctx context.Context, opts *ui.Options, token, channel string, interval time.Duration) {
	note := func(msg string) {
		opts.StartupNotes = append(opts.StartupNotes, "[WARN] Chatter polling disabled: "+msg)
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	h := helix.New(token)
	info, err := h.Validate(ctx)
	if err != nil {
		note("token validation failed: " + err.Error())
		return
	}
	if !info.HasScope(helix.ScopeReadChatters) {
		note("token lacks the " + helix.ScopeReadChatters + " scope (create a Custom Scope Token at twitchtokengenerator.com with chat:read, chat:edit, " + helix.ScopeReadChatters + ")")
		return
	}

	channel = strings.TrimPrefix(channel, "#")
	broadcasterID := info.UserID
	if !strings.EqualFold(channel, info.Login) {
		broadcasterID, err = h.UserID(ctx, channel)
		if err != nil {
			note("could not look up channel " + channel + ": " + err.Error())
			return
		}
	}

	opts.Helix = h
	opts.PollInterval = max(interval, minPollInterval)
	opts.BroadcasterID = broadcasterID
	opts.ModeratorID = info.UserID
}
