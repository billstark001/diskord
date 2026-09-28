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
	if e != nil || len(b.Messages) != 2 || !*b.Messages[0].Deleted {
		t.Fatal(b, e)
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
func TestOutageNotDeletion(t *testing.T) {
	b, e := Gateway([]byte(`{"op":0,"t":"GUILD_DELETE","d":{"id":"1","unavailable":true}}`))
	if e != nil || b.Guilds[0].Deleted != nil {
		t.Fatal(b, e)
	}
}
