package helix

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
)

func newTestClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := New("oauth:tok123")
	c.baseURL = srv.URL + "/helix"
	c.idURL = srv.URL + "/oauth2"
	c.clientID = "cid"
	return c
}

func TestValidateWhilePolling(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth2/validate" {
			writeJSON(w, 200, map[string]string{"client_id": "cid"})
			return
		}
		if got := r.Header.Get("Client-Id"); got != "cid" {
			t.Errorf("Client-Id = %q, want cid", got)
		}
		writeJSON(w, 200, map[string]any{"data": []any{}, "pagination": map[string]string{}})
	})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for range 100 {
			if _, err := c.Validate(context.Background()); err != nil {
				t.Error(err)
			}
		}
	}()
	go func() {
		defer wg.Done()
		for range 100 {
			if _, err := c.Chatters(context.Background(), "1", "2"); err != nil {
				t.Error(err)
			}
		}
	}()
	wg.Wait()
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func TestValidate(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oauth2/validate" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "OAuth tok123" {
			t.Errorf("Authorization = %q", got)
		}
		writeJSON(w, 200, map[string]any{
			"client_id": "abc", "login": "streamer", "user_id": "42",
			"scopes": []string{"chat:read", ScopeReadChatters}, "expires_in": 3600,
		})
	})
	c.clientID = ""
	info, err := c.Validate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.Login != "streamer" || info.UserID != "42" || !info.HasScope(ScopeReadChatters) || info.HasScope("chat:edit") {
		t.Errorf("unexpected info: %+v", info)
	}
	if c.clientID != "abc" {
		t.Errorf("clientID = %q, want abc", c.clientID)
	}
}

func TestValidateUnauthorized(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 401, map[string]any{"status": 401, "message": "invalid access token"})
	})
	_, err := c.Validate(context.Background())
	if !IsAuthError(err) {
		t.Fatalf("err = %v, want auth error", err)
	}
}

func TestUserID(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/helix/users" || r.URL.Query().Get("login") != "somechannel" {
			t.Errorf("unexpected request %s", r.URL)
		}
		writeJSON(w, 200, map[string]any{"data": []map[string]string{{"id": "99", "login": "somechannel"}}})
	})
	id, err := c.UserID(context.Background(), "SomeChannel")
	if err != nil || id != "99" {
		t.Fatalf("UserID = %q, %v", id, err)
	}
}

func TestChattersPaginates(t *testing.T) {
	calls := 0
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		q := r.URL.Query()
		if r.URL.Path != "/helix/chat/chatters" || q.Get("broadcaster_id") != "1" || q.Get("moderator_id") != "2" || q.Get("first") != "1000" {
			t.Errorf("unexpected request %s", r.URL)
		}
		if r.Header.Get("Authorization") != "Bearer tok123" || r.Header.Get("Client-Id") != "cid" {
			t.Errorf("bad headers: %v", r.Header)
		}
		switch q.Get("after") {
		case "":
			writeJSON(w, 200, map[string]any{
				"data":       []map[string]string{{"user_login": "alice"}, {"user_login": "Bob"}},
				"pagination": map[string]string{"cursor": "page2"},
			})
		case "page2":
			writeJSON(w, 200, map[string]any{
				"data":       []map[string]string{{"user_login": "bob"}, {"user_login": "carol"}},
				"pagination": map[string]string{},
			})
		default:
			t.Errorf("unexpected cursor %q", q.Get("after"))
		}
	})
	got, err := c.Chatters(context.Background(), "1", "2")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"alice", "bob", "carol"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Chatters = %v, want %v", got, want)
	}
	if calls != 2 {
		t.Errorf("calls = %d, want 2", calls)
	}
}

func TestChattersErrors(t *testing.T) {
	tests := []struct {
		status      int
		auth, limit bool
	}{
		{401, true, false},
		{403, true, false},
		{429, false, true},
		{500, false, false},
	}
	for _, tt := range tests {
		c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, tt.status, map[string]any{"message": "nope"})
		})
		_, err := c.Chatters(context.Background(), "1", "2")
		if err == nil {
			t.Fatalf("status %d: expected error", tt.status)
		}
		if IsAuthError(err) != tt.auth || IsRateLimited(err) != tt.limit {
			t.Errorf("status %d: auth=%v limit=%v", tt.status, IsAuthError(err), IsRateLimited(err))
		}
	}
}
