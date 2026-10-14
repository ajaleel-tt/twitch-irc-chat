package irc

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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

// blockedWriteConn lets the HTTP/WebSocket handshake complete, then simulates
// a socket whose writes cannot progress. Closing the socket unblocks the write,
// just as it would for a real network connection.
type blockedWriteConn struct {
	net.Conn
	stallWrites  chan struct{}
	writeStarted chan struct{}
	closed       chan struct{}
	writeOnce    sync.Once
	closeOnce    sync.Once
}

func (c *blockedWriteConn) Write(p []byte) (int, error) {
	select {
	case <-c.stallWrites:
		c.writeOnce.Do(func() { close(c.writeStarted) })
		<-c.closed
		return 0, net.ErrClosed
	default:
		return c.Conn.Write(p)
	}
}

func (c *blockedWriteConn) Close() error {
	c.closeOnce.Do(func() { close(c.closed) })
	return c.Conn.Close()
}

func TestWriteTimeoutClosesConnectionAndIncoming(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("accept websocket: %v", err)
			return
		}
		defer conn.CloseNow()
		// Read until the client closes its socket; no Twitch connection is needed.
		_, _, _ = conn.Read(r.Context())
	}))
	t.Cleanup(server.Close)

	var socket *blockedWriteConn
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			conn, err := (&net.Dialer{}).DialContext(ctx, network, addr)
			if err != nil {
				return nil, err
			}
			socket = &blockedWriteConn{
				Conn:         conn,
				stallWrites:  make(chan struct{}),
				writeStarted: make(chan struct{}),
				closed:       make(chan struct{}),
			}
			return socket, nil
		},
	}
	t.Cleanup(transport.CloseIdleConnections)
	dialCtx, cancelDial := context.WithTimeout(context.Background(), time.Second)
	defer cancelDial()
	conn, _, err := websocket.Dial(dialCtx, "ws"+strings.TrimPrefix(server.URL, "http"), &websocket.DialOptions{
		HTTPClient: &http.Client{Transport: transport},
	})
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	t.Cleanup(func() { _ = conn.CloseNow() })
	close(socket.stallWrites)

	c := NewClient("me", "mychannel")
	c.conn = conn
	// Keep the read context alive so only the write timeout can end this loop.
	readCtx, cancelRead := context.WithCancel(context.Background())
	t.Cleanup(cancelRead)
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		c.readLoop(readCtx)
	}()

	// A shorter parent deadline exercises the real library's timeout behavior
	// without waiting for the production 10-second timeout.
	writeCtx, cancelWrite := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancelWrite()
	writeResult := make(chan error, 1)
	go func() { writeResult <- c.SendMessage(writeCtx, "hi") }()
	select {
	case err := <-writeResult:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("SendMessage error = %v, want context.DeadlineExceeded", err)
		}
	case <-time.After(time.Second):
		t.Fatal("SendMessage stayed blocked after its write deadline")
	}
	select {
	case <-socket.writeStarted:
	default:
		t.Fatal("write timed out before reaching the socket")
	}
	select {
	case <-socket.closed:
	default:
		t.Fatal("write timeout did not close the socket")
	}
	select {
	case _, ok := <-c.Incoming():
		if ok {
			t.Fatal("incoming delivered a message, want a closed channel")
		}
	case <-time.After(time.Second):
		t.Fatal("incoming channel stayed open after the write timeout")
	}
	select {
	case <-readDone:
	case <-time.After(time.Second):
		t.Fatal("readLoop did not exit after the write timeout")
	}
	if err := readCtx.Err(); err != nil {
		t.Fatalf("read context ended independently of the write timeout: %v", err)
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

// scriptConn is a wsConn that returns frames in order, then fails every
// Read with err, like a server sending some lines and hanging up.
type scriptConn struct {
	frames []string
	err    error
}

func (s *scriptConn) Read(context.Context) (websocket.MessageType, []byte, error) {
	if len(s.frames) == 0 {
		return 0, nil, s.err
	}
	f := s.frames[0]
	s.frames = s.frames[1:]
	return websocket.MessageText, []byte(f), nil
}

func (s *scriptConn) Write(context.Context, websocket.MessageType, []byte) error { return nil }

func (s *scriptConn) Close(websocket.StatusCode, string) error { return nil }

// runUntilClosed runs readLoop over conn, drains Incoming until it closes,
// and returns the delivered messages.
func runUntilClosed(t *testing.T, conn wsConn) (*Client, []Message) {
	t.Helper()
	c := NewClient("me", "mychannel")
	c.conn = conn
	go c.readLoop(context.Background())

	var got []Message
	timeout := time.After(time.Second)
	for {
		select {
		case msg, ok := <-c.Incoming():
			if !ok {
				return c, got
			}
			got = append(got, msg)
		case <-timeout:
			t.Fatal("Incoming was never closed")
		}
	}
}

func TestErrNilWhileConnected(t *testing.T) {
	if err := NewClient("me", "mychannel").Err(); err != nil {
		t.Errorf("Err() = %v, want nil before the connection ends", err)
	}
}

func TestErrReportsReadFailure(t *testing.T) {
	readErr := errors.New("connection reset")
	c, got := runUntilClosed(t, &scriptConn{
		frames: []string{":someone!someone@someone.tmi.twitch.tv PRIVMSG #mychannel :bye\r\n"},
		err:    readErr,
	})

	if len(got) != 1 || got[0].Params != "bye" {
		t.Errorf("delivered %v, want the PRIVMSG before the error", got)
	}
	if err := c.Err(); err == nil || !errors.Is(err, readErr) || err.Error() != readErr.Error() {
		t.Errorf("Err() = %v, want exactly %v", err, readErr)
	}
}

func TestErrIncludesServerNotice(t *testing.T) {
	c, got := runUntilClosed(t, &scriptConn{
		frames: []string{":tmi.twitch.tv NOTICE * :Login authentication failed\r\n"},
		err:    io.EOF,
	})

	if len(got) != 1 || got[0].Command != "NOTICE" {
		t.Errorf("delivered %v, want the NOTICE still passed to the UI", got)
	}
	err := c.Err()
	if !errors.Is(err, io.EOF) {
		t.Errorf("Err() = %v, want it to wrap io.EOF", err)
	}
	if err == nil || !strings.HasPrefix(err.Error(), "Login authentication failed: ") {
		t.Errorf("Err() = %v, want it to start with the notice text", err)
	}
}

func TestErrIgnoresChannelNotice(t *testing.T) {
	c, _ := runUntilClosed(t, &scriptConn{
		frames: []string{"@msg-id=slow_on :tmi.twitch.tv NOTICE #mychannel :This room is now in slow mode.\r\n"},
		err:    io.EOF,
	})

	if err := c.Err(); err != io.EOF {
		t.Errorf("Err() = %v, want bare io.EOF; channel notices aren't disconnect reasons", err)
	}
}
