package irc

import (
	"testing"
)

func TestParseMessage(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want Message
	}{
		{
			name: "PRIVMSG with tags",
			raw:  "@badge-info=;badges=moderator/1;color=#FF4500;display-name=TestUser;mod=1 :testuser!testuser@testuser.tmi.twitch.tv PRIVMSG #mychannel :Hello world!",
			want: Message{
				Tags: map[string]string{
					"badge-info":   "",
					"badges":       "moderator/1",
					"color":        "#FF4500",
					"display-name": "TestUser",
					"mod":          "1",
				},
				Source:  "testuser!testuser@testuser.tmi.twitch.tv",
				Nick:    "testuser",
				Command: "PRIVMSG",
				Channel: "#mychannel",
				Params:  "Hello world!",
			},
		},
		{
			name: "PING",
			raw:  "PING :tmi.twitch.tv",
			want: Message{
				Tags:    map[string]string{},
				Command: "PING",
				Params:  "tmi.twitch.tv",
			},
		},
		{
			name: "JOIN",
			raw:  ":testuser!testuser@testuser.tmi.twitch.tv JOIN #mychannel",
			want: Message{
				Tags:    map[string]string{},
				Source:  "testuser!testuser@testuser.tmi.twitch.tv",
				Nick:    "testuser",
				Command: "JOIN",
				Channel: "#mychannel",
			},
		},
		{
			name: "NOTICE",
			raw:  "@msg-id=slow_off :tmi.twitch.tv NOTICE #mychannel :This room is no longer in slow mode.",
			want: Message{
				Tags:    map[string]string{"msg-id": "slow_off"},
				Source:  "tmi.twitch.tv",
				Command: "NOTICE",
				Channel: "#mychannel",
				Params:  "This room is no longer in slow mode.",
			},
		},
		{
			name: "RECONNECT",
			raw:  "RECONNECT",
			want: Message{
				Tags:    map[string]string{},
				Command: "RECONNECT",
			},
		},
		{
			name: "Numeric 001 welcome",
			raw:  ":tmi.twitch.tv 001 testuser :Welcome, GLHF!",
			want: Message{
				Tags:    map[string]string{},
				Source:  "tmi.twitch.tv",
				Command: "001",
				Params:  "testuser :Welcome, GLHF!",
			},
		},
		{
			name: "PRIVMSG with escaped tag value",
			raw:  "@display-name=Test\\sUser :testuser!testuser@testuser.tmi.twitch.tv PRIVMSG #ch :hi",
			want: Message{
				Tags: map[string]string{
					"display-name": "Test User",
				},
				Source:  "testuser!testuser@testuser.tmi.twitch.tv",
				Nick:    "testuser",
				Command: "PRIVMSG",
				Channel: "#ch",
				Params:  "hi",
			},
		},
		{
			name: "USERNOTICE (sub/resub)",
			raw:  "@badge-info=subscriber/12;badges=subscriber/12;color=#00FF7F;display-name=SubUser;login=subuser;msg-id=resub;system-msg=SubUser\\ssubscribed\\sat\\sTier\\s1. :tmi.twitch.tv USERNOTICE #mychannel :Great stream!",
			want: Message{
				Tags: map[string]string{
					"badge-info":   "subscriber/12",
					"badges":       "subscriber/12",
					"color":        "#00FF7F",
					"display-name": "SubUser",
					"login":        "subuser",
					"msg-id":       "resub",
					"system-msg":   "SubUser subscribed at Tier 1.",
				},
				Source:  "tmi.twitch.tv",
				Command: "USERNOTICE",
				Channel: "#mychannel",
				Params:  "Great stream!",
			},
		},
		{
			name: "CLEARCHAT (timeout)",
			raw:  "@ban-duration=600;target-user-id=12345 :tmi.twitch.tv CLEARCHAT #mychannel :baduser",
			want: Message{
				Tags: map[string]string{
					"ban-duration":   "600",
					"target-user-id": "12345",
				},
				Source:  "tmi.twitch.tv",
				Command: "CLEARCHAT",
				Channel: "#mychannel",
				Params:  "baduser",
			},
		},
		{
			name: "PART",
			raw:  ":testuser!testuser@testuser.tmi.twitch.tv PART #mychannel",
			want: Message{
				Tags:    map[string]string{},
				Source:  "testuser!testuser@testuser.tmi.twitch.tv",
				Nick:    "testuser",
				Command: "PART",
				Channel: "#mychannel",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseMessage(tt.raw)

			if got.Command != tt.want.Command {
				t.Errorf("Command = %q, want %q", got.Command, tt.want.Command)
			}
			if got.Nick != tt.want.Nick {
				t.Errorf("Nick = %q, want %q", got.Nick, tt.want.Nick)
			}
			if got.Source != tt.want.Source {
				t.Errorf("Source = %q, want %q", got.Source, tt.want.Source)
			}
			if got.Channel != tt.want.Channel {
				t.Errorf("Channel = %q, want %q", got.Channel, tt.want.Channel)
			}
			if got.Params != tt.want.Params {
				t.Errorf("Params = %q, want %q", got.Params, tt.want.Params)
			}
			for k, v := range tt.want.Tags {
				if got.Tags[k] != v {
					t.Errorf("Tag[%q] = %q, want %q", k, got.Tags[k], v)
				}
			}
		})
	}
}

func TestToChatMessage(t *testing.T) {
	raw := "@badge-info=;badges=moderator/1;color=#FF4500;display-name=CoolMod;mod=1 :coolmod!coolmod@coolmod.tmi.twitch.tv PRIVMSG #test :Hey everyone!"
	m := ParseMessage(raw)
	cm := m.ToChatMessage()

	if cm.DisplayName != "CoolMod" {
		t.Errorf("DisplayName = %q, want %q", cm.DisplayName, "CoolMod")
	}
	if cm.Color != "#FF4500" {
		t.Errorf("Color = %q, want %q", cm.Color, "#FF4500")
	}
	if cm.Message != "Hey everyone!" {
		t.Errorf("Message = %q, want %q", cm.Message, "Hey everyone!")
	}
	if !cm.IsMod {
		t.Error("IsMod = false, want true")
	}
	if cm.Badges != "moderator/1" {
		t.Errorf("Badges = %q, want %q", cm.Badges, "moderator/1")
	}
}
