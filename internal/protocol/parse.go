// Package protocol projects allowlisted response/event fields into four entities.
// Unknown fields are never marshalled or persisted, including tokens in READY.
package protocol

import (
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"

	"diskord/internal/model"
	"diskord/internal/scope"
)

type object map[string]json.RawMessage

func obj(b json.RawMessage) object { var o object; _ = json.Unmarshal(b, &o); return o }
func arr(b json.RawMessage) []json.RawMessage {
	var a []json.RawMessage
	_ = json.Unmarshal(b, &a)
	return a
}
func str(b json.RawMessage) string {
	var s string
	if json.Unmarshal(b, &s) == nil {
		return s
	}
	// ETF snowflakes are often integers. Marshal preserves their decimal
	// digits; never route them through float64 or lose precision.
	if model.ID(string(b)) {
		return string(b)
	}
	return ""
}
func sp(o object, k string) *string {
	b, ok := o[k]
	if !ok || string(b) == "null" {
		return nil
	}
	var s string
	if json.Unmarshal(b, &s) != nil {
		return nil
	}
	return &s
}
func ip(o object, k string) *int {
	b, ok := o[k]
	if !ok {
		return nil
	}
	var i int
	if json.Unmarshal(b, &i) != nil {
		return nil
	}
	return &i
}
func bp(o object, k string) *bool {
	b, ok := o[k]
	if !ok {
		return nil
	}
	var v bool
	if json.Unmarshal(b, &v) != nil {
		return nil
	}
	return &v
}
func idp(o object, k string) *string {
	s := str(o[k])
	if !model.ID(s) {
		return nil
	}
	return &s
}
func inherited(o object, k, defaultID string) *string {
	if p := idp(o, k); p != nil {
		return p
	}
	if model.ID(defaultID) {
		return &defaultID
	}
	return nil
}

type parser struct {
	b   model.Batch
	now int64
}

func (p *parser) user(o object) string {
	id := str(o["id"])
	if !model.ID(id) {
		return ""
	}
	p.b.Users = append(p.b.Users, model.User{ID: id, Username: sp(o, "username"), DisplayName: sp(o, "global_name"), Avatar: sp(o, "avatar")})
	return id
}
func (p *parser) member(o object, gid string) {
	id := p.user(obj(o["user"]))
	if model.ID(gid) && id != "" {
		p.b.Members = append(p.b.Members, model.Member{GuildID: gid, UserID: id})
	}
}
func (p *parser) guild(o object, deleted bool) {
	id := str(o["id"])
	if !model.ID(id) {
		return
	}
	name := sp(o, "name")
	if name == nil {
		name = sp(obj(o["properties"]), "name")
	}
	g := model.Guild{ID: id, Name: name, Unavailable: bp(o, "unavailable")}
	if deleted {
		// A temporary outage is not a guild deletion.
		if g.Unavailable == nil || !*g.Unavailable {
			g.Deleted = model.Ptr(true)
		}
	} else {
		g.Deleted = model.Ptr(false)
	}
	p.b.Guilds = append(p.b.Guilds, g)
	for _, k := range []string{"channels", "threads"} {
		for _, c := range arr(o[k]) {
			p.channel(obj(c), id, false)
		}
	}
	for _, m := range arr(o["members"]) {
		p.member(obj(m), id)
	}
}
func (p *parser) channel(o object, gid string, deleted bool) {
	id := str(o["id"])
	if !model.ID(id) {
		return
	}
	c := model.Channel{ID: id, GuildID: inherited(o, "guild_id", gid), ParentID: idp(o, "parent_id"), Name: sp(o, "name"), Type: ip(o, "type"), Deleted: model.Ptr(deleted)}
	if _, ok := o["recipients"]; ok {
		c.Recipients = make([]string, 0)
	}
	for _, r := range arr(o["recipients"]) {
		if id := p.user(obj(r)); id != "" {
			c.Recipients = append(c.Recipients, id)
		}
	}
	if _, ok := o["recipient_ids"]; ok && c.Recipients == nil {
		c.Recipients = make([]string, 0)
	}
	for _, r := range arr(o["recipient_ids"]) {
		id := str(r)
		if model.ID(id) {
			c.Recipients = append(c.Recipients, id)
		}
	}
	p.b.Channels = append(p.b.Channels, c)
}
func (p *parser) message(o object, channel string, deleted bool) {
	id := str(o["id"])
	if !model.ID(id) {
		return
	}
	m := model.Message{ID: id, ChannelID: inherited(o, "channel_id", channel), Content: sp(o, "content"), Timestamp: sp(o, "timestamp"), EditedTimestamp: sp(o, "edited_timestamp"), Type: ip(o, "type")}
	if deleted {
		m.Deleted = model.Ptr(true)
	}
	if author := p.user(obj(o["author"])); author != "" {
		m.AuthorID = &author
	}
	for _, u := range arr(o["mentions"]) {
		p.user(obj(u))
	}
	gid := str(o["guild_id"])
	if model.ID(gid) && m.ChannelID != nil {
		// A sparse channel record does not clear its name, parent or recipients.
		p.b.Channels = append(p.b.Channels, model.Channel{ID: *m.ChannelID, GuildID: &gid})
		if m.AuthorID != nil {
			p.b.Members = append(p.b.Members, model.Member{GuildID: gid, UserID: *m.AuthorID})
		}
	}
	_, m.HasAttachments = o["attachments"]
	for _, raw := range arr(o["attachments"]) {
		a := obj(raw)
		aid := str(a["id"])
		if !model.ID(aid) {
			continue
		}
		h, path, ok := scope.AssetURL(str(a["url"]))
		if !ok {
			continue
		}
		var size int64
		_ = json.Unmarshal(a["size"], &size)
		m.Attachments = append(m.Attachments, model.Attachment{ID: aid, MessageID: id, Name: str(a["filename"]), Host: h, Path: path, Size: size})
	}
	_, m.HasReactions = o["reactions"]
	for _, raw := range arr(o["reactions"]) {
		reaction := obj(raw)
		emoji := obj(reaction["emoji"])
		id, name := str(emoji["id"]), str(emoji["name"])
		if id != "" && !model.ID(id) || id == "" && (name == "" || len(name) > 128) {
			continue
		}
		var count int
		if json.Unmarshal(reaction["count"], &count) != nil || count < 0 || count > 1000000 {
			continue
		}
		animated := bp(emoji, "animated")
		m.Reactions = append(m.Reactions, model.Reaction{EmojiID: id, EmojiName: name, Animated: animated != nil && *animated, Count: count})
	}
	// Deletions and metadata-only updates must not advance the content revision:
	// a later observed message snapshot may be the first source of its body.
	if !deleted && (m.Content != nil || m.Timestamp != nil || m.EditedTimestamp != nil) {
		m.Revision = model.Revision(m.EditedTimestamp, m.Timestamp, p.now)
	}
	p.b.Messages = append(p.b.Messages, m)
}

func Gateway(data []byte) (model.Batch, error) {
	var envelope struct {
		Op *int            `json:"op"`
		T  string          `json:"t"`
		D  json.RawMessage `json:"d"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return model.Batch{}, errors.New("invalid Gateway JSON")
	}
	p := parser{b: model.Batch{Source: "ws"}, now: time.Now().UnixMilli()}
	if envelope.Op == nil || *envelope.Op != 0 {
		return p.b, nil
	} // Never process IDENTIFY/RESUME even if misrouted.
	o := obj(envelope.D)
	switch envelope.T {
	case "READY":
		p.user(obj(o["user"]))
		for _, u := range arr(o["users"]) {
			p.user(obj(u))
		}
		for _, g := range arr(o["guilds"]) {
			p.guild(obj(g), false)
		}
		for _, c := range arr(o["private_channels"]) {
			p.channel(obj(c), "", false)
		}
	case "READY_SUPPLEMENTAL":
		for _, g := range arr(o["guilds"]) {
			p.guild(obj(g), false)
		}
	case "GUILD_CREATE", "GUILD_UPDATE":
		p.guild(o, false)
	case "GUILD_DELETE":
		p.guild(o, true)
	case "CHANNEL_CREATE", "CHANNEL_UPDATE", "THREAD_CREATE", "THREAD_UPDATE":
		p.channel(o, "", false)
	case "CHANNEL_DELETE", "THREAD_DELETE":
		p.channel(o, "", true)
	case "THREAD_LIST_SYNC":
		for _, c := range arr(o["threads"]) {
			p.channel(obj(c), str(o["guild_id"]), false)
		}
	case "MESSAGE_CREATE", "MESSAGE_UPDATE":
		p.message(o, "", false)
	case "MESSAGE_DELETE":
		p.message(o, "", true)
	case "MESSAGE_DELETE_BULK":
		for _, id := range arr(o["ids"]) {
			p.message(object{"id": id, "channel_id": o["channel_id"], "guild_id": o["guild_id"]}, "", true)
		}
	case "MESSAGE_REACTION_ADD", "MESSAGE_REACTION_REMOVE", "MESSAGE_REACTION_REMOVE_ALL", "MESSAGE_REACTION_REMOVE_EMOJI":
		messageID := str(o["message_id"])
		if !model.ID(messageID) {
			break
		}
		change := model.ReactionChange{MessageID: messageID}
		switch envelope.T {
		case "MESSAGE_REACTION_ADD":
			change.Operation = "add"
		case "MESSAGE_REACTION_REMOVE":
			change.Operation = "remove"
		case "MESSAGE_REACTION_REMOVE_ALL":
			change.Operation = "clear"
		case "MESSAGE_REACTION_REMOVE_EMOJI":
			change.Operation = "clear-emoji"
		}
		if change.Operation != "clear" {
			emoji := obj(o["emoji"])
			change.EmojiID, change.EmojiName = str(emoji["id"]), str(emoji["name"])
			if change.EmojiID != "" && !model.ID(change.EmojiID) || change.EmojiID == "" && (change.EmojiName == "" || len(change.EmojiName) > 128) {
				break
			}
			animated := bp(emoji, "animated")
			change.Animated = animated != nil && *animated
		}
		if change.Operation == "add" || change.Operation == "remove" {
			change.UserID = str(o["user_id"])
			if !model.ID(change.UserID) {
				break
			}
			p.user(obj(obj(o["member"])["user"]))
			p.user(obj(o["user"]))
		}
		p.b.ReactionChanges = append(p.b.ReactionChanges, change)
	case "GUILD_MEMBER_ADD", "GUILD_MEMBER_UPDATE":
		p.member(o, str(o["guild_id"]))
	case "GUILD_MEMBERS_CHUNK":
		for _, m := range arr(o["members"]) {
			p.member(obj(m), str(o["guild_id"]))
		}
	case "USER_UPDATE":
		p.user(o)
	}
	return p.b, nil
}

func HTTP(path string, data []byte) (model.Batch, error) {
	p := parser{b: model.Batch{Source: "http"}, now: time.Now().UnixMilli()}
	endpoint := scope.Endpoint(path)
	if endpoint == "" {
		return p.b, nil
	}
	if !json.Valid(data) {
		return p.b, errors.New("invalid HTTP JSON")
	}
	parts := strings.Split(endpoint, "/")
	each := func(fn func(object)) {
		if len(data) > 0 && strings.TrimSpace(string(data))[0] == '[' {
			for _, v := range arr(data) {
				fn(obj(v))
			}
		} else {
			fn(obj(data))
		}
	}
	switch {
	case parts[0] == "channels" && len(parts) == 6 && parts[2] == "messages" && parts[4] == "reactions":
		name, err := url.PathUnescape(parts[5])
		if err != nil || len(name) > 128 || !model.ID(parts[3]) {
			break
		}
		id := ""
		if colon := strings.LastIndexByte(name, ':'); colon >= 0 && model.ID(name[colon+1:]) {
			id, name = name[colon+1:], name[:colon]
		}
		if name == "" && id == "" {
			break
		}
		for _, raw := range arr(data) {
			userID := p.user(obj(raw))
			if userID != "" {
				p.b.ReactionChanges = append(p.b.ReactionChanges, model.ReactionChange{MessageID: parts[3], EmojiID: id, EmojiName: name, UserID: userID, Operation: "observe"})
			}
		}
	case strings.HasSuffix(endpoint, "/messages/search"):
		// Search returns an array of arrays, not a flat message list.
		for _, group := range arr(obj(data)["messages"]) {
			for _, m := range arr(group) {
				p.message(obj(m), "", false)
			}
		}
	case parts[0] == "channels" && len(parts) >= 3 && parts[2] == "messages":
		each(func(o object) { p.message(o, parts[1], false) })
	case parts[0] == "channels":
		each(func(o object) { p.channel(o, "", false) })
	case parts[0] == "guilds" && len(parts) == 3 && parts[2] == "channels":
		each(func(o object) { p.channel(o, parts[1], false) })
	case parts[0] == "guilds" || strings.HasSuffix(endpoint, "/guilds"):
		each(func(o object) { p.guild(o, false) })
	case strings.HasSuffix(endpoint, "/channels"):
		each(func(o object) { p.channel(o, "", false) })
	case parts[0] == "users":
		each(func(o object) { p.user(o) })
	}
	return p.b, nil
}
