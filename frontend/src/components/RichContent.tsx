import MarkdownIt from "markdown-it";
import type { Message } from "../core/api";
import { emojiForShortcode } from "../core/emoji";
import { s } from "../styles";

const markup =
  /<@!?(\d+)>|<@&(\d+)>|<#(\d+)>|<(a?):([A-Za-z0-9_]+):(\d+)>|(@everyone|@here)|:([A-Za-z0-9_+\-]{2,64}):/g;

const markdown = new MarkdownIt({ html: false, linkify: true, breaks: true });
// Archived messages never fetch remote images merely because a message used
// Markdown image syntax. Cached Discord attachments remain separate below.
markdown.renderer.rules.image = (tokens, index) => markdown.utils.escapeHtml(tokens[index].content);
markdown.renderer.rules.text = (tokens, index, _options, context) => {
  const env = context as Message;
  const value = tokens[index].content;
  const parts: string[] = [];
  let start = 0;
  for (const match of value.matchAll(markup)) {
    const offset = match.index;
    if (offset > start) parts.push(markdown.utils.escapeHtml(value.slice(start, offset)));
    const [, user, role, channel, , emojiName, emojiID, broadcast, shortcode] = match;
    if (user || role || channel || broadcast) {
      const label = user
        ? `@${env.MentionNames?.[`user:${user}`] || user}`
        : role
          ? `@${env.MentionNames?.[`role:${role}`] || `role ${role}`}`
          : channel
            ? `#${env.MentionNames?.[`channel:${channel}`] || channel}`
            : broadcast;
      parts.push(`<span class="${s.mention}">${markdown.utils.escapeHtml(label)}</span>`);
    } else if (emojiID) {
      const hash = env.EmojiHashes?.[emojiID];
      const label = `:${emojiName}:`;
      parts.push(
        hash && /^[a-f0-9]{64}$/i.test(hash)
          ? `<img class="${s.inlineEmoji}" src="/assets/${hash}" alt="${markdown.utils.escapeHtml(label)}" title="${markdown.utils.escapeHtml(label)}" loading="lazy">`
          : `<span class="${s.emojiFallback}">${markdown.utils.escapeHtml(label)}</span>`,
      );
    } else if (shortcode) {
      const emoji = emojiForShortcode(shortcode);
      parts.push(markdown.utils.escapeHtml(emoji || match[0]));
    }
    start = offset + match[0].length;
  }
  if (start < value.length) parts.push(markdown.utils.escapeHtml(value.slice(start)));
  return parts.join("");
};

export function RichContent({ message }: { message: Message }) {
  const html = markdown.render(message.Content, message);
  return <div class={s.richContent} dangerouslySetInnerHTML={{ __html: html }} />;
}
