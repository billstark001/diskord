import type { ComponentChildren } from "preact";
import type { Message } from "../core/api";
import { emojiForShortcode } from "../core/emoji";
import { s } from "../styles";

const markup =
  /<@!?(\d+)>|<@&(\d+)>|<#(\d+)>|<(a?):([A-Za-z0-9_]+):(\d+)>|(@everyone|@here)|:([A-Za-z0-9_+\-]{2,64}):/g;

export function RichContent({ message }: { message: Message }) {
  const parts: ComponentChildren[] = [];
  let start = 0;
  for (const match of message.Content.matchAll(markup)) {
    const index = match.index;
    if (index > start) parts.push(message.Content.slice(start, index));
    const [, user, role, channel, , emojiName, emojiID, broadcast, shortcode] = match;
    const key = `${index}-${match[0]}`;
    if (user) {
      parts.push(
        <span key={key} class={s.mention}>
          @{message.MentionNames?.[`user:${user}`] || user}
        </span>,
      );
    } else if (role) {
      parts.push(
        <span key={key} class={s.mention}>
          @{message.MentionNames?.[`role:${role}`] || `role ${role}`}
        </span>,
      );
    } else if (channel) {
      parts.push(
        <span key={key} class={s.mention}>
          #{message.MentionNames?.[`channel:${channel}`] || channel}
        </span>,
      );
    } else if (emojiID) {
      const hash = message.EmojiHashes?.[emojiID];
      parts.push(
        hash ? (
          <img
            key={key}
            class={s.inlineEmoji}
            src={`/assets/${hash}`}
            alt={`:${emojiName}:`}
            title={`:${emojiName}:`}
            loading="lazy"
          />
        ) : (
          <span key={key} class={s.emojiFallback}>
            :{emojiName}:
          </span>
        ),
      );
    } else if (broadcast) {
      parts.push(
        <span key={key} class={s.mention}>
          {broadcast}
        </span>,
      );
    } else if (shortcode) {
      const emoji = emojiForShortcode(shortcode);
      parts.push(
        emoji ? (
          <span key={key} title={`:${shortcode}:`}>
            {emoji}
          </span>
        ) : (
          match[0]
        ),
      );
    }
    start = index + match[0].length;
  }
  if (start < message.Content.length) parts.push(message.Content.slice(start));
  return <>{parts}</>;
}
