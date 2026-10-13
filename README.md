# twitch-irc-chat

A terminal-based Twitch chat client. Connect to any channel, read live chat, and send messages -- all from your terminal.

![Go](https://img.shields.io/badge/Go-1.24-00ADD8?logo=go&logoColor=white)

## Features

- Live chat via WebSocket connection to Twitch IRC
- Colored usernames matching Twitch chat colors
- Badge indicators for broadcasters, mods, VIPs, and subscribers
- Scrollable chat history (1000-line buffer)
- Sub/resub notifications, timeouts, bans, and chat clears
- Auto PING/PONG keepalive
- Send confirmation -- your message only appears after a successful write

## Setup

### Prerequisites

- [Go](https://go.dev/dl/) 1.24+
- A Twitch account
- An OAuth token (see below)

### Get an OAuth Token

1. Go to [twitchtokengenerator.com](https://twitchtokengenerator.com)
2. Select **Chat Bot Token**
3. Authorize with your Twitch account
4. Copy the **Access Token**

### Build

```bash
git clone <repo-url>
cd twitch-irc-chat
go build -o twitch-irc-chat .
```

## Usage

```bash
# Set credentials as environment variables
export TWITCH_TOKEN=your_oauth_token
export TWITCH_NICK=your_twitch_username

# Join a channel
./twitch-irc-chat --channel xqc
```

Or pass everything as flags:

```bash
./twitch-irc-chat --channel xqc --token your_oauth_token --nick your_twitch_username
```

### Options

| Flag | Description |
|---|---|
| `--channel` | Twitch channel to join (required) |
| `--token` | OAuth token (or set `TWITCH_TOKEN`) |
| `--nick` | Twitch username (or set `TWITCH_NICK`) |
| `--hide-joins` | Hide "joined/left the channel" lines -- handy in large channels where they're mostly noise |
| `--poll-chatters` | Poll the chatter list at this interval (e.g. `30s`, minimum `10s`) for accurate join/leave tracking. Off by default. See below |

### Join/leave accuracy

Twitch has no real-time join/leave feed, so the client combines two sources:

- **IRC `JOIN`/`PART`** (always on): low latency but batched, can be dropped or duplicated, and Twitch stops sending them entirely once a chat has more than 1,000 users.
- **Get Chatters polling** (`--poll-chatters=30s`): the full list of logged-in users connected to chat, compared on each poll to catch anything IRC missed. Twitch's list itself lags by an undocumented amount, so polling faster than ~30s gains little.

Both feed one presence list, so each join or leave is printed once, with the time it was detected. Viewers who are logged out or don't have chat open can't be seen by either source.

Polling requires a token with the `moderator:read:chatters` scope, belonging to the broadcaster or one of their moderators. On [twitchtokengenerator.com](https://twitchtokengenerator.com), choose **Custom Scope Token** and select `chat:read`, `chat:edit`, and `moderator:read:chatters`. If the token can't be used for polling, the client shows why and carries on with IRC only.

```bash
./twitch-irc-chat --channel your_channel --poll-chatters=30s
```

### Controls

| Key | Action |
|---|---|
| `Enter` | Send message |
| `Up/Down` | Scroll chat |
| `Page Up/Down` | Scroll chat (fast) |
| `/quit` | Exit |
| `Ctrl+C` | Exit |

## Project Structure

```
twitch-irc-chat/
├── main.go           # Entry point, flag parsing
├── helix/
│   ├── client.go     # Helix API: token validation, Get Users, Get Chatters
│   └── client_test.go
├── irc/
│   ├── client.go     # WebSocket connection, auth, read/write
│   ├── message.go    # IRC message parser
│   └── message_test.go
└── ui/
    ├── model.go      # Bubbletea TUI (viewport + text input)
    ├── presence.go   # Merges IRC JOIN/PART and chatter polls into one presence list
    └── styles.go     # Username colors, badges, layout styles
```

## How It Works

The app connects to `wss://irc-ws.chat.twitch.tv` over WebSocket and speaks Twitch's IRC protocol. It requests three Twitch-specific capabilities (`tags`, `commands`, `membership`) to get metadata like username colors, badges, and sub notifications.

The TUI is built with [Bubbletea](https://github.com/charmbracelet/bubbletea) using the Elm Architecture pattern -- a loop of Model/Update/View that redraws the terminal on every state change. Incoming IRC messages are bridged into Bubbletea's event system via Go channels.

## Running Tests

```bash
go test ./...
```
