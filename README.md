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
├── irc/
│   ├── client.go     # WebSocket connection, auth, read/write
│   ├── message.go    # IRC message parser
│   └── message_test.go
└── ui/
    ├── model.go      # Bubbletea TUI (viewport + text input)
    └── styles.go     # Username colors, badges, layout styles
```

## How It Works

The app connects to `wss://irc-ws.chat.twitch.tv` over WebSocket and speaks Twitch's IRC protocol. It requests three Twitch-specific capabilities (`tags`, `commands`, `membership`) to get metadata like username colors, badges, and sub notifications.

The TUI is built with [Bubbletea](https://github.com/charmbracelet/bubbletea) using the Elm Architecture pattern -- a loop of Model/Update/View that redraws the terminal on every state change. Incoming IRC messages are bridged into Bubbletea's event system via Go channels.

## Running Tests

```bash
go test ./irc/...
```
