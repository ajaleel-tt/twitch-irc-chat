package main

import "testing"

func TestResolveAuth(t *testing.T) {
	tests := []struct {
		name          string
		token, nick   string
		wantToken     string
		wantNick      string
		wantAnonymous bool
		wantErr       bool
	}{
		{name: "neither is anonymous", wantNick: anonymousNick, wantAnonymous: true},
		{name: "both logs in", token: "abc", nick: "me", wantToken: "abc", wantNick: "me"},
		{name: "token without nick", token: "abc", wantErr: true},
		{name: "nick without token", nick: "me", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token, nick, anonymous, err := resolveAuth(tt.token, tt.nick)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if token != tt.wantToken || nick != tt.wantNick || anonymous != tt.wantAnonymous {
				t.Errorf("got (%q, %q, %v), want (%q, %q, %v)",
					token, nick, anonymous, tt.wantToken, tt.wantNick, tt.wantAnonymous)
			}
		})
	}
}
