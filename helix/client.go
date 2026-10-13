// Package helix is a minimal client for the Twitch Helix API endpoints used
// to track who is connected to a channel's chat.
package helix

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultBaseURL = "https://api.twitch.tv/helix"
	defaultIDURL   = "https://id.twitch.tv/oauth2"

	// ScopeReadChatters is required to call Get Chatters.
	ScopeReadChatters = "moderator:read:chatters"
)

// Client calls Helix with a user access token. Call Validate before any other
// method so the Client-Id matching the token is known.
type Client struct {
	http     *http.Client
	baseURL  string
	idURL    string
	token    string
	clientID string
}

// New creates a client for the given OAuth token (with or without "oauth:").
func New(token string) *Client {
	return &Client{
		http:    &http.Client{Timeout: 15 * time.Second},
		baseURL: defaultBaseURL,
		idURL:   defaultIDURL,
		token:   strings.TrimPrefix(token, "oauth:"),
	}
}

// APIError is a non-2xx response from Twitch.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("twitch api: HTTP %d", e.Status)
	}
	return fmt.Sprintf("twitch api: HTTP %d: %s", e.Status, e.Message)
}

// IsAuthError reports whether err means the token cannot be used for the
// request (invalid token, missing scope, or not a moderator).
func IsAuthError(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && (apiErr.Status == http.StatusUnauthorized || apiErr.Status == http.StatusForbidden)
}

// IsRateLimited reports whether err is a 429 response.
func IsRateLimited(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Status == http.StatusTooManyRequests
}

// TokenInfo is the result of validating a token.
type TokenInfo struct {
	ClientID  string   `json:"client_id"`
	Login     string   `json:"login"`
	UserID    string   `json:"user_id"`
	Scopes    []string `json:"scopes"`
	ExpiresIn int      `json:"expires_in"`
}

// HasScope reports whether the token was granted scope.
func (t TokenInfo) HasScope(scope string) bool {
	for _, s := range t.Scopes {
		if s == scope {
			return true
		}
	}
	return false
}

// Validate checks the token and records its Client-Id for later requests.
// Twitch requires apps to validate at startup and hourly thereafter.
func (c *Client) Validate(ctx context.Context) (TokenInfo, error) {
	var info TokenInfo
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.idURL+"/validate", nil)
	if err != nil {
		return info, err
	}
	req.Header.Set("Authorization", "OAuth "+c.token)
	if err := c.do(req, &info); err != nil {
		return info, err
	}
	c.clientID = info.ClientID
	return info, nil
}

// UserID looks up the user ID for a login name.
func (c *Client) UserID(ctx context.Context, login string) (string, error) {
	var resp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	q := url.Values{"login": {strings.ToLower(login)}}
	if err := c.get(ctx, "/users", q, &resp); err != nil {
		return "", err
	}
	if len(resp.Data) == 0 {
		return "", fmt.Errorf("twitch user %q not found", login)
	}
	return resp.Data[0].ID, nil
}

// Chatters returns the lowercase logins of every user connected to the
// broadcaster's chat. moderatorID must be the token's user and must be the
// broadcaster or one of their moderators.
func (c *Client) Chatters(ctx context.Context, broadcasterID, moderatorID string) ([]string, error) {
	seen := make(map[string]bool)
	var logins []string
	cursor := ""
	for {
		q := url.Values{
			"broadcaster_id": {broadcasterID},
			"moderator_id":   {moderatorID},
			"first":          {"1000"},
		}
		if cursor != "" {
			q.Set("after", cursor)
		}
		var resp struct {
			Data []struct {
				UserLogin string `json:"user_login"`
			} `json:"data"`
			Pagination struct {
				Cursor string `json:"cursor"`
			} `json:"pagination"`
		}
		if err := c.get(ctx, "/chat/chatters", q, &resp); err != nil {
			return nil, err
		}
		// The list can change while paging, so the same user may repeat.
		for _, d := range resp.Data {
			login := strings.ToLower(d.UserLogin)
			if login != "" && !seen[login] {
				seen[login] = true
				logins = append(logins, login)
			}
		}
		if resp.Pagination.Cursor == "" || len(resp.Data) == 0 {
			return logins, nil
		}
		cursor = resp.Pagination.Cursor
	}
}

func (c *Client) get(ctx context.Context, path string, q url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path+"?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Client-Id", c.clientID)
	return c.do(req, out)
}

func (c *Client) do(req *http.Request, out any) error {
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		var body struct {
			Message string `json:"message"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&body)
		return &APIError{Status: resp.StatusCode, Message: body.Message}
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
