package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNoCredentialsCrossBoundary(t *testing.T) {
	for _, s := range []string{`{"op":2,"d":{"token":"DO-NOT-SAVE"}}`, `{"op":6,"d":{"token":"DO-NOT-SAVE","session_id":"DO-NOT-SAVE"}}`} {
		b, e := Gateway([]byte(s))
		if e != nil || !b.Empty() {
			t.Fatal(b, e)
		}
	}
	b, e := Gateway([]byte(`{"op":0,"t":"READY","d":{"token":"DO-NOT-SAVE","session_id":"DO-NOT-SAVE","user":{"id":"123","username":"alice","email":"DO-NOT-SAVE"},"guilds":[{"id":"456","properties":{"name":"test"}}]}}`))
	if e != nil {
		t.Fatal(e)
	}
	encoded, _ := json.Marshal(b)
	if strings.Contains(string(encoded), "DO-NOT-SAVE") {
		t.Fatal(string(encoded))
	}
	if len(b.Users) != 1 || len(b.Guilds) != 1 {
		t.Fatal(b)
	}
}
func TestSparseEditAndDelete(t *testing.T) {
	b, e := Gateway([]byte(`{"op":0,"t":"MESSAGE_UPDATE","d":{"id":"9007199254740993","channel_id":"123","content":"","edited_timestamp":"2025-01-01T00:00:00Z"}}`))
	if e != nil {
		t.Fatal(e)
	}
	m := b.Messages[0]
	if m.ID != "9007199254740993" || m.Content == nil || *m.Content != "" || m.AuthorID != nil || m.HasAttachments {
		t.Fatal(m)
	}
	b, e = Gateway([]byte(`{"op":0,"t":"MESSAGE_DELETE_BULK","d":{"ids":["1","2"],"channel_id":"3"}}`))
	if e != nil || len(b.Messages) != 2 || !*b.Messages[0].Deleted || b.Messages[0].Revision != 0 {
		t.Fatal(b, e)
	}
	b, e = Gateway([]byte(`{"op":0,"t":"MESSAGE_UPDATE","d":{"id":"1","channel_id":"3","flags":4}}`))
	if e != nil || len(b.Messages) != 1 || b.Messages[0].Revision != 0 {
		t.Fatal("metadata-only update advanced content revision", b, e)
	}
}
func TestHTTPAndSignedAttachment(t *testing.T) {
	b, e := HTTP("/api/v9/channels/2/messages", []byte(`[{"id":"1","channel_id":"2","author":{"id":"3","username":"test"},"content":"hi","attachments":[{"id":"4","filename":"a.png","url":"https://cdn.discordapp.com/attachments/2/4/a.png?token=secret"}]}]`))
	if e != nil || len(b.Messages) != 1 || len(b.Users) != 1 {
		t.Fatal(b, e)
	}
	encoded, _ := json.Marshal(b)
	if strings.Contains(string(encoded), "secret") {
		t.Fatal(string(encoded))
	}
	b, e = HTTP("/api/v9/auth/login", []byte(`{"token":"secret"}`))
	if e != nil || !b.Empty() {
		t.Fatal(b, e)
	}
}
func TestGuildSearchResults(t *testing.T) {
	b, e := HTTP("/api/v9/guilds/123/messages/search", []byte(`{"messages":[[{"id":"789","channel_id":"456","guild_id":"123","content":"found","author":{"id":"321","username":"member"}}]],"total_results":1}`))
	if e != nil || len(b.Messages) != 1 || len(b.Channels) != 1 || len(b.Guilds) != 0 {
		t.Fatalf("search results were not projected: %+v, %v", b, e)
	}
	if b.Messages[0].ID != "789" || b.Channels[0].GuildID == nil || *b.Channels[0].GuildID != "123" {
		t.Fatalf("search message lost guild relationship: %+v", b)
	}
}
func TestNumericSnowflakesPreserveDigits(t *testing.T) {
	b, e := Gateway([]byte(`{"op":0,"t":"READY","d":{"guilds":[{"id":9007199254740993,"properties":{"name":"Example"},"channels":[{"id":9007199254740995,"parent_id":9007199254740994,"name":"child"}]}]}}`))
	if e != nil || len(b.Guilds) != 1 || len(b.Channels) != 1 {
		t.Fatalf("numeric IDs were dropped: %+v, %v", b, e)
	}
	if b.Guilds[0].ID != "9007199254740993" || b.Channels[0].ID != "9007199254740995" || b.Channels[0].ParentID == nil || *b.Channels[0].ParentID != "9007199254740994" {
		t.Fatalf("numeric IDs lost precision: %+v", b)
	}
}
func TestOutageNotDeletion(t *testing.T) {
	b, e := Gateway([]byte(`{"op":0,"t":"GUILD_DELETE","d":{"id":"1","unavailable":true}}`))
	if e != nil || b.Guilds[0].Deleted != nil {
		t.Fatal(b, e)
	}
}
func TestGuildIconSnapshotsAndRemoval(t *testing.T) {
	b, err := Gateway([]byte(`{"op":0,"t":"READY","d":{"guilds":[{"id":"1","properties":{"name":"Guild","icon":"icon_hash"}}]}}`))
	if err != nil || len(b.Guilds) != 1 || b.Guilds[0].Icon == nil || *b.Guilds[0].Icon != "icon_hash" {
		t.Fatalf("guild icon snapshot: %+v, %v", b, err)
	}
	b, err = Gateway([]byte(`{"op":0,"t":"GUILD_UPDATE","d":{"id":"1","icon":null}}`))
	if err != nil || len(b.Guilds) != 1 || b.Guilds[0].Icon == nil || *b.Guilds[0].Icon != "" {
		t.Fatalf("guild icon removal: %+v, %v", b, err)
	}
}
func TestReactionsFromSnapshotsAndEvents(t *testing.T) {
	b, err := HTTP("/api/v9/channels/2/messages", []byte(`[{"id":"10","channel_id":"2","reactions":[{"count":2,"emoji":{"id":null,"name":"👍"}}]}]`))
	if err != nil || len(b.Messages) != 1 || !b.Messages[0].HasReactions || len(b.Messages[0].Reactions) != 1 || b.Messages[0].Reactions[0].Count != 2 {
		t.Fatalf("reaction snapshot: %+v, %v", b, err)
	}
	b, err = Gateway([]byte(`{"op":0,"t":"MESSAGE_REACTION_ADD","d":{"message_id":"10","user_id":"3","emoji":{"id":null,"name":"👍"},"member":{"user":{"id":"3","username":"Alice"}}}}`))
	if err != nil || len(b.ReactionChanges) != 1 || b.ReactionChanges[0].Operation != "add" || len(b.Users) != 1 {
		t.Fatalf("reaction add: %+v, %v", b, err)
	}
	b, err = HTTP("/api/v9/channels/2/messages/10/reactions/%F0%9F%91%8D", []byte(`[{"id":"4","username":"Bob"}]`))
	if err != nil || len(b.ReactionChanges) != 1 || b.ReactionChanges[0].Operation != "observe" || b.ReactionChanges[0].EmojiName != "👍" {
		t.Fatalf("reaction users: %+v, %v", b, err)
	}
}
