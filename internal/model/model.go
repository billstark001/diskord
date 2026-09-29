// Package model is the only data allowed across the capture/storage boundary.
// It deliberately has no HTTP header, Gateway token, cookie or raw-JSON fields.
package model

import (
	"strings"
	"time"
)

type User struct {
	ID                            string
	Username, DisplayName, Avatar *string
}
type Guild struct {
	ID                   string
	Name, Icon           *string
	Unavailable, Deleted *bool
}
type Channel struct {
	ID                      string
	GuildID, ParentID, Name *string
	Type                    *int
	Deleted                 *bool
	Recipients              []string
}
type Member struct{ GuildID, UserID string }
type Attachment struct {
	ID, MessageID, Name, Host, Path string
	Size                            int64
}
type Message struct {
	ID                                                       string
	ChannelID, AuthorID, Content, Timestamp, EditedTimestamp *string
	Type                                                     *int
	Deleted                                                  *bool
	Revision                                                 int64
	Attachments                                              []Attachment
	// HasAttachments distinguishes omission (partial update) from an empty list.
	HasAttachments bool
	Reactions      []Reaction
	HasReactions   bool
}
type Reaction struct {
	EmojiID, EmojiName string
	Animated           bool
	Count              int
}
type ReactionChange struct {
	MessageID, EmojiID, EmojiName, UserID string
	Animated                              bool
	Operation                             string // add, remove, observe, clear, clear-emoji
}
type Batch struct {
	Source          string
	Users           []User
	Guilds          []Guild
	Channels        []Channel
	Members         []Member
	Messages        []Message
	ReactionChanges []ReactionChange
}

func (b Batch) Empty() bool {
	return len(b.Users)+len(b.Guilds)+len(b.Channels)+len(b.Members)+len(b.Messages)+len(b.ReactionChanges) == 0
}
func ID(s string) bool {
	if len(s) == 0 || len(s) > 20 {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return s == "0" || s[0] != '0'
}
func SortKey(s string) string {
	if !ID(s) {
		return ""
	}
	return strings.Repeat("0", 20-len(s)) + s
}
func Ptr[T any](v T) *T { return &v }
func Revision(edited, created *string, fallback int64) int64 {
	for _, p := range []*string{edited, created} {
		if p != nil {
			if t, e := time.Parse(time.RFC3339Nano, *p); e == nil {
				return t.UnixMilli()
			}
		}
	}
	return fallback
}

type MessageRow struct {
	ID, ChannelID, ChannelName, GuildID, GuildName, AuthorName, Content string
	Timestamp, EditedTimestamp, Source                                  string
	AvatarHash                                                          string
	Deleted                                                             bool
	Attachments                                                         []AssetRow
	Reactions                                                           []ReactionRow
	MentionNames                                                        map[string]string
	EmojiHashes                                                         map[string]string
}
type ReactionRow struct {
	EmojiID, EmojiName  string
	EmojiHash           string
	Animated            bool
	Count, UnknownCount int
	Users               []string
}
type GuildRow struct {
	ID, Name, IconHash   string
	Unavailable, Deleted bool
}
type ChannelRow struct {
	ID, GuildID, ParentID, Name string
	Kind                        int
	Deleted                     bool
}
type AssetRow struct{ Name, Hash string }
type Filter struct {
	Query, GuildID, ChannelID, Before, After, Around, Scope string
	Limit                                                   int
}
type MessagePage struct {
	Rows   []MessageRow `json:"rows"`
	Before string       `json:"before"`
	After  string       `json:"after"`
}
type Counts struct{ Users, Guilds, Channels, Messages int64 }
