import type { Message } from "../core/api";
import { text } from "../core/i18n";
import { s } from "../styles";

export function MessageItem({ message }: { message: Message }) {
  return (
    <article class={s.message}>
      {message.AvatarHash ? (
        <img class={s.avatar} src={`/assets/${message.AvatarHash}`} alt="" loading="lazy" />
      ) : (
        <span class={s.avatarFallback} aria-hidden="true">
          {[...(message.AuthorName || "?")][0]?.toLocaleUpperCase()}
        </span>
      )}
      <div class={s.messageContent}>
        <div class={s.messageHead}>
          <span class={s.messageAuthor}>{message.AuthorName || text("unknownUser")}</span>
          <span class={s.messageMeta}>
            {message.GuildName || text("unknownGuild")} /{" "}
            {message.ChannelName || text("unknownChannel")}
          </span>
          <span class={s.messageMeta}>{message.Timestamp}</span>
        </div>
        <div class={s.messageBody}>{message.Content}</div>
        {message.Deleted && <span class={s.badge}>{text("deleted")}</span>}
        {message.EditedTimestamp && (
          <span class={s.messageMeta}>
            {" "}
            &nbsp;· {text("edited")} {message.EditedTimestamp}
          </span>
        )}
        {message.Attachments?.map((attachment) => (
          <div key={attachment.Name} class={s.muted}>
            {attachment.Hash && (
              <img
                class={s.asset}
                src={`/assets/${attachment.Hash}`}
                alt={attachment.Name}
                loading="lazy"
              />
            )}
            <span>{attachment.Name}</span>
            {!attachment.Hash && <span> · {text("assetMissing")}</span>}
          </div>
        ))}
        {!!message.Reactions?.length && (
          <div class={s.reactions}>
            {message.Reactions.map((reaction) => {
              const names = [...(reaction.Users || [])];
              if (reaction.UnknownCount > 0)
                names.push(`${reaction.UnknownCount} ${text("unknownReactors")}`);
              return (
                <span
                  key={reaction.EmojiID || reaction.EmojiName}
                  class={s.reaction}
                  title={names.join(", ")}
                  aria-label={names.join(", ")}
                >
                  {reaction.EmojiHash ? (
                    <img
                      class={s.reactionEmoji}
                      src={`/assets/${reaction.EmojiHash}`}
                      alt={reaction.EmojiName}
                      loading="lazy"
                    />
                  ) : reaction.EmojiID ? (
                    `:${reaction.EmojiName}:`
                  ) : (
                    reaction.EmojiName
                  )}{" "}
                  {reaction.Count}
                </span>
              );
            })}
          </div>
        )}
      </div>
    </article>
  );
}
