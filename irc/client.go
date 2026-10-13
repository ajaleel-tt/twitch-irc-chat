package irc

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

const twitchWSURL = "wss://irc-ws.chat.twitch.tv:443"

// writeTimeout bounds every write. If it expires mid-write, websocket closes
// the connection, which ends readLoop instead of leaving a dead socket open.
const writeTimeout = 10 * time.Second

// wsConn is the subset of *websocket.Conn the client uses.
type wsConn interface {
	Read(ctx context.Context) (websocket.MessageType, []byte, error)
	Write(ctx context.Context, typ websocket.MessageType, p []byte) error
	Close(code websocket.StatusCode, reason string) error
}

// Client manages a WebSocket connection to Twitch IRC.
type Client struct {
	conn     wsConn
	nick     string
	channel  string
	incoming chan Message

	mu         sync.Mutex
	err        error  // why readLoop stopped; set before incoming is closed
	lastNotice string // most recent server-wide NOTICE, e.g. a login failure
}

// NewClient creates a new IRC client for the given channel.
func NewClient(nick, channel string) *Client {
	if !strings.HasPrefix(channel, "#") {
		channel = "#" + channel
	}
	return &Client{
		nick:     strings.ToLower(nick),
		channel:  strings.ToLower(channel),
		incoming: make(chan Message, 256),
	}
}

// Connect establishes the WebSocket connection and authenticates.
func (c *Client) Connect(ctx context.Context, token string) error {
	conn, _, err := websocket.Dial(ctx, twitchWSURL, nil)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	conn.SetReadLimit(1 << 20) // 1MB
	c.conn = conn

	// Request Twitch-specific capabilities
	if err := c.send(ctx, "CAP REQ :twitch.tv/membership twitch.tv/tags twitch.tv/commands"); err != nil {
		return fmt.Errorf("cap req: %w", err)
	}

	// Authenticate
	if !strings.HasPrefix(token, "oauth:") {
		token = "oauth:" + token
	}
	if err := c.send(ctx, "PASS "+token); err != nil {
		return fmt.Errorf("pass: %w", err)
	}
	if err := c.send(ctx, "NICK "+c.nick); err != nil {
		return fmt.Errorf("nick: %w", err)
	}

	// Join channel
	if err := c.send(ctx, "JOIN "+c.channel); err != nil {
		return fmt.Errorf("join: %w", err)
	}

	go c.readLoop(ctx)

	return nil
}

func (c *Client) send(ctx context.Context, line string) error {
	ctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	return c.conn.Write(ctx, websocket.MessageText, []byte(line+"\r\n"))
}

func (c *Client) readLoop(ctx context.Context) {
	defer close(c.incoming)
	for {
		_, data, err := c.conn.Read(ctx)
		if err != nil {
			c.setErr(err)
			return
		}
		lines := strings.Split(string(data), "\r\n")
		for _, line := range lines {
			if line == "" {
				continue
			}
			msg := ParseMessage(line)

			// Auto-respond to PING. Write in the background so a stalled
			// write can't stop us reading; send's timeout bounds the goroutine.
			if msg.Command == "PING" {
				go func(params string) {
					_ = c.send(ctx, "PONG :"+params)
				}(msg.Params)
				continue
			}

			// Twitch sends server-wide notices to "*" rather than a channel,
			// e.g. "NOTICE * :Login authentication failed" before it hangs up.
			if msg.Command == "NOTICE" && msg.Channel == "" {
				c.mu.Lock()
				c.lastNotice = strings.TrimPrefix(msg.Params, "* :")
				c.mu.Unlock()
			}

			select {
			case c.incoming <- msg:
			case <-ctx.Done():
				c.setErr(ctx.Err())
				return
			}
		}
	}
}

func (c *Client) setErr(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.lastNotice != "" {
		err = fmt.Errorf("%s: %w", c.lastNotice, err)
	}
	c.err = err
}

// Err returns why the connection ended, or nil while it is still open.
// Once Incoming is closed, Err is non-nil.
func (c *Client) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

// Incoming returns the channel of parsed incoming messages.
func (c *Client) Incoming() <-chan Message {
	return c.incoming
}

// SendMessage sends a PRIVMSG to the joined channel.
// It blocks until the write completes or times out and returns any error.
func (c *Client) SendMessage(ctx context.Context, text string) error {
	return c.send(ctx, fmt.Sprintf("PRIVMSG %s :%s", c.channel, text))
}

// Close gracefully shuts down the connection.
func (c *Client) Close() {
	if c.conn != nil {
		c.conn.Close(websocket.StatusNormalClosure, "bye")
	}
}
