package store

import (
	"context"
	"database/sql"
	"errors"

	"diskord/internal/model"
)

func emojiKey(id, name string) string {
	if model.ID(id) {
		return "c:" + id
	}
	if name != "" && len(name) <= 128 {
		return "u:" + name
	}
	return ""
}

func applyReactions(tx *sql.Tx, batch model.Batch) error {
	for _, message := range batch.Messages {
		if !model.ID(message.ID) || !message.HasReactions {
			continue
		}
		if _, err := tx.Exec("UPDATE reaction_totals SET snapshot_marker=0 WHERE message_id=?", message.ID); err != nil {
			return err
		}
		for _, reaction := range message.Reactions {
			key := emojiKey(reaction.EmojiID, reaction.EmojiName)
			if key == "" || reaction.Count < 0 {
				continue
			}
			_, err := tx.Exec(`INSERT INTO reaction_totals(message_id,emoji_key,emoji_id,emoji_name,animated,count,snapshot_marker)
                VALUES(?,?,?,?,?,?,?) ON CONFLICT(message_id,emoji_key) DO UPDATE SET
                emoji_name=excluded.emoji_name,animated=excluded.animated,
				count=MAX(excluded.count,(SELECT count(*) FROM reaction_users WHERE message_id=excluded.message_id AND emoji_key=excluded.emoji_key)),
				snapshot_marker=excluded.snapshot_marker`, message.ID, key, reaction.EmojiID, reaction.EmojiName, reaction.Animated, reaction.Count, 1)
			if err != nil {
				return err
			}
		}
		if _, err := tx.Exec("DELETE FROM reaction_totals WHERE message_id=? AND snapshot_marker=0", message.ID); err != nil {
			return err
		}
	}
	for _, change := range batch.ReactionChanges {
		if !model.ID(change.MessageID) {
			continue
		}
		if _, err := tx.Exec("INSERT OR IGNORE INTO messages(id,sort_key,source) VALUES(?,?,?)", change.MessageID, model.SortKey(change.MessageID), batch.Source); err != nil {
			return err
		}
		if change.Operation == "clear" {
			if _, err := tx.Exec("DELETE FROM reaction_totals WHERE message_id=?", change.MessageID); err != nil {
				return err
			}
			continue
		}
		key := emojiKey(change.EmojiID, change.EmojiName)
		if key == "" {
			continue
		}
		if change.Operation == "clear-emoji" {
			if _, err := tx.Exec("DELETE FROM reaction_totals WHERE message_id=? AND emoji_key=?", change.MessageID, key); err != nil {
				return err
			}
			continue
		}
		if !model.ID(change.UserID) {
			continue
		}
		if err := stub(tx, "users", change.UserID); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT OR IGNORE INTO reaction_totals(message_id,emoji_key,emoji_id,emoji_name,animated,count)
            VALUES(?,?,?,?,?,0)`, change.MessageID, key, change.EmojiID, change.EmojiName, change.Animated); err != nil {
			return err
		}
		switch change.Operation {
		case "add", "observe":
			result, err := tx.Exec("INSERT OR IGNORE INTO reaction_users(message_id,emoji_key,user_id) VALUES(?,?,?)", change.MessageID, key, change.UserID)
			if err != nil {
				return err
			}
			added, err := result.RowsAffected()
			if err != nil {
				return err
			}
			if change.Operation == "add" && added > 0 {
				if _, err := tx.Exec("UPDATE reaction_totals SET count=count+1 WHERE message_id=? AND emoji_key=?", change.MessageID, key); err != nil {
					return err
				}
			}
			if _, err := tx.Exec(`UPDATE reaction_totals SET count=MAX(count,(SELECT count(*) FROM reaction_users WHERE message_id=? AND emoji_key=?)) WHERE message_id=? AND emoji_key=?`, change.MessageID, key, change.MessageID, key); err != nil {
				return err
			}
		case "remove":
			if _, err := tx.Exec("DELETE FROM reaction_users WHERE message_id=? AND emoji_key=? AND user_id=?", change.MessageID, key, change.UserID); err != nil {
				return err
			}
			if _, err := tx.Exec("UPDATE reaction_totals SET count=MAX(0,count-1) WHERE message_id=? AND emoji_key=?", change.MessageID, key); err != nil {
				return err
			}
		default:
			return errors.New("invalid reaction operation")
		}
	}
	return nil
}

func (s *Store) messageReactions(ctx context.Context, messageID string) ([]model.ReactionRow, error) {
	rows, err := s.reader.QueryContext(ctx, `SELECT t.emoji_key,t.emoji_id,t.emoji_name,t.animated,t.count,
        COALESCE((SELECT r.hash FROM resource_urls r WHERE r.host='cdn.discordapp.com'
        AND substr(r.path,1,length('/emojis/'||t.emoji_id||'.'))='/emojis/'||t.emoji_id||'.' LIMIT 1),'')
        FROM reaction_totals t WHERE t.message_id=? AND t.count>0 ORDER BY t.emoji_key`, messageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []model.ReactionRow{}
	for rows.Next() {
		var key string
		var reaction model.ReactionRow
		if err := rows.Scan(&key, &reaction.EmojiID, &reaction.EmojiName, &reaction.Animated, &reaction.Count, &reaction.EmojiHash); err != nil {
			return nil, err
		}
		names, err := s.reader.QueryContext(ctx, `SELECT COALESCE(NULLIF(u.display_name,''),NULLIF(u.username,''),u.id) FROM reaction_users r JOIN users u ON u.id=r.user_id WHERE r.message_id=? AND r.emoji_key=? ORDER BY lower(COALESCE(u.display_name,u.username,u.id))`, messageID, key)
		if err != nil {
			return nil, err
		}
		for names.Next() {
			var name string
			if err := names.Scan(&name); err != nil {
				names.Close()
				return nil, err
			}
			reaction.Users = append(reaction.Users, name)
		}
		err = names.Err()
		names.Close()
		if err != nil {
			return nil, err
		}
		reaction.UnknownCount = max(0, reaction.Count-len(reaction.Users))
		result = append(result, reaction)
	}
	return result, rows.Err()
}
