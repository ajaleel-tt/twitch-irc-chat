package irc

import (
	"context"
	"fmt"
	"strings"

	"github.com/coder/websocket"
)

const twitchWSURL = "wss://irc-ws.chat.twitch.tv:443"

// Client manages a WebSocket connection to Twitch IRC.
type Client struct {
	conn     *websocket.Conn
	nick     string
	channel  string
	incoming chan Message
	outgoing chan string
	done     chan struct{}
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
		outgoing: make(chan string, 64),
		done:     make(chan struct{}),
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
	go c.writeLoop(ctx)

	return nil
}

func (c *Client) send(ctx context.Context, line string) error {
	return c.conn.Write(ctx, websocket.MessageText, []byte(line+"\r\n"))
}

func (c *Client) readLoop(ctx context.Context) {
	defer close(c.incoming)
	for {
		_, data, err := c.conn.Read(ctx)
		if err != nil {
			return
		}
		lines := strings.Split(string(data), "\r\n")
		for _, line := range lines {
			if line == "" {
				continue
			}
			msg := ParseMessage(line)

			// Auto-respond to PING
			if msg.Command == "PING" {
				_ = c.send(ctx, "PONG :"+msg.Params)
				continue
			}

			select {
			case c.incoming <- msg:
			case <-ctx.Done():
				return
			}
		}
	}
}

func (c *Client) writeLoop(ctx context.Context) {
	for {
		select {
		case text, ok := <-c.outgoing:
			if !ok {
				return
			}
			_ = c.send(ctx, text)
		case <-ctx.Done():
			return
		}
	}
}

// Incoming returns the channel of parsed incoming messages.
func (c *Client) Incoming() <-chan Message {
	return c.incoming
}

// SendMessage sends a PRIVMSG to the joined channel.
// It blocks until the write completes and returns any error.
func (c *Client) SendMessage(ctx context.Context, text string) error {
	return c.send(ctx, fmt.Sprintf("PRIVMSG %s :%s", c.channel, text))
}

// Close gracefully shuts down the connection.
func (c *Client) Close() {
	close(c.outgoing)
	if c.conn != nil {
		c.conn.Close(websocket.StatusNormalClosure, "bye")
	}
}
