package irc

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// stallConn is a wsConn whose reads come from frames and whose writes block
// until their context is done, like a write stuck on the network.
type stallConn struct {
	frames chan string
	writes chan string
}

func newStallConn() *stallConn {
	return &stallConn{frames: make(chan string, 8), writes: make(chan string, 8)}
}

func (s *stallConn) Read(ctx context.Context) (websocket.MessageType, []byte, error) {
	select {
	case f := <-s.frames:
		return websocket.MessageText, []byte(f), nil
	case <-ctx.Done():
		return 0, nil, ctx.Err()
	}
}

func (s *stallConn) Write(ctx context.Context, _ websocket.MessageType, p []byte) error {
	s.writes <- string(p)
	<-ctx.Done()
	return ctx.Err()
}

func (s *stallConn) Close(websocket.StatusCode, string) error { return nil }

func TestReadLoopNotBlockedByStalledPong(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	conn := newStallConn()
	c := NewClient("me", "mychannel")
	c.conn = conn
	go c.readLoop(ctx)

	conn.frames <- "PING :tmi.twitch.tv\r\n"
	conn.frames <- ":someone!someone@someone.tmi.twitch.tv PRIVMSG #mychannel :still here\r\n"

	select {
	case w := <-conn.writes:
		if w != "PONG :tmi.twitch.tv\r\n" {
			t.Errorf("write = %q, want PONG", w)
		}
	case <-time.After(time.Second):
		t.Fatal("PONG was never written")
	}

	select {
	case msg := <-c.Incoming():
		if msg.Command != "PRIVMSG" || msg.Params != "still here" {
			t.Errorf("got %s %q, want PRIVMSG \"still here\"", msg.Command, msg.Params)
		}
	case <-time.After(time.Second):
		t.Fatal("readLoop stopped delivering messages while the PONG write was stalled")
	}
}

func TestSendHasWriteTimeout(t *testing.T) {
	var deadline time.Time
	var hasDeadline bool
	c := NewClient("me", "mychannel")
	c.conn = writeFunc(func(ctx context.Context) error {
		deadline, hasDeadline = ctx.Deadline()
		return nil
	})

	before := time.Now()
	if err := c.SendMessage(context.Background(), "hi"); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	after := time.Now()
	if !hasDeadline {
		t.Fatal("write context has no deadline")
	}
	if deadline.Before(before.Add(writeTimeout)) || deadline.After(after.Add(writeTimeout)) {
		t.Errorf("write deadline %v after call start, want %v", deadline.Sub(before), writeTimeout)
	}
}

func TestSendReturnsErrorWhenWriteTimesOut(t *testing.T) {
	c := NewClient("me", "mychannel")
	c.conn = newStallConn()

	// A short parent deadline stands in for writeTimeout expiring.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	err := c.SendMessage(ctx, "hi")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("SendMessage error = %v, want context.DeadlineExceeded", err)
	}
}

// writeFunc is a wsConn whose Write calls the function; Read blocks.
type writeFunc func(ctx context.Context) error

func (f writeFunc) Read(ctx context.Context) (websocket.MessageType, []byte, error) {
	<-ctx.Done()
	return 0, nil, ctx.Err()
}

func (f writeFunc) Write(ctx context.Context, _ websocket.MessageType, _ []byte) error {
	return f(ctx)
}

func (f writeFunc) Close(websocket.StatusCode, string) error { return nil }
