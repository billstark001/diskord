import { useState } from "preact/hooks";
import type { Message } from "../core/api";
import { text } from "../core/i18n";
import { formatLocalTime } from "../core/time";
import { s } from "../styles";
import { RichContent } from "./RichContent";

export function MessageItem({
  message,
  onJump,
  focused,
}: {
  message: Message;
  onJump?: () => void;
  focused?: boolean;
}) {
  const [copyStatus, setCopyStatus] = useState("");
  const isDM = message.ChannelKind === 1 || message.ChannelKind === 3;
  const messageURL =
    message.ChannelID && message.ID && (message.GuildID || isDM)
      ? `https://discord.com/channels/${message.GuildID || "@me"}/${message.ChannelID}/${message.ID}`
      : "";
  async function copy(value: string) {
    try {
      await navigator.clipboard.writeText(value);
      setCopyStatus(text("copied"));
    } catch {
      setCopyStatus(text("copyFailed"));
    }
  }
  return (
    <article
      class={`${s.message} ${focused ? s.messageFocused : ""} ${onJump ? s.messageJumpable : ""}`}
      data-message-id={message.ID}
      data-export={message.ChannelID ? "" : undefined}
      data-export-thread={message.ChannelID}
      data-export-name={message.ChannelName || message.ChannelID}
      onDblClick={onJump}
      onKeyDown={(event) => {
        if (onJump && event.target === event.currentTarget && event.key === "Enter") onJump();
      }}
      tabIndex={onJump || message.ChannelID ? 0 : undefined}
      role={onJump ? "button" : undefined}
      title={onJump ? text("jumpToMessage") : undefined}
    >
      <div class={s.messageActions} onDblClick={(event) => event.stopPropagation()}>
        <button
          type="button"
          class={s.messageAction}
          aria-label={text("copyMessageLink")}
          title={messageURL ? text("copyMessageLink") : text("linkUnavailable")}
          disabled={!messageURL}
          onClick={(event) => {
            event.stopPropagation();
            if (messageURL) void copy(messageURL);
          }}
        >
          <svg
            viewBox="0 0 24 24"
            width="17"
            height="17"
            fill="none"
            stroke="currentColor"
            stroke-width="1.8"
            stroke-linecap="round"
            stroke-linejoin="round"
            aria-hidden="true"
          >
            <path d="M10 13a5 5 0 0 0 7.5.5l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71" />
            <path d="M14 11a5 5 0 0 0-7.5-.5l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71" />
          </svg>
        </button>
        <button
          type="button"
          class={s.messageAction}
          aria-label={text("copyRawMessage")}
          title={text("copyRawMessage")}
          onClick={(event) => {
            event.stopPropagation();
            void copy(message.Content);
          }}
        >
          <svg
            viewBox="0 0 24 24"
            width="17"
            height="17"
            fill="none"
            stroke="currentColor"
            stroke-width="1.8"
            stroke-linecap="round"
            stroke-linejoin="round"
            aria-hidden="true"
          >
            <rect x="8" y="8" width="12" height="13" rx="2" />
            <path d="M16 8V5a2 2 0 0 0-2-2H5a2 2 0 0 0-2 2v10a2 2 0 0 0 2 2h3" />
          </svg>
        </button>
        <span class={s.copyStatus} role="status">
          {copyStatus}
        </span>
      </div>
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
            {message.GuildName ||
              (message.ChannelKind === 1 || message.ChannelKind === 3
                ? text("dm")
                : text("unknownGuild"))}{" "}
            / {message.ChannelName || text("unknownChannel")}
          </span>
          <span class={s.messageMeta} title={message.Timestamp}>
            {formatLocalTime(message.Timestamp)}
          </span>
        </div>
        <div class={s.messageBody}>
          <RichContent message={message} />
        </div>
        {message.Deleted && <span class={s.badge}>{text("deleted")}</span>}
        {message.EditedTimestamp && (
          <span class={s.messageMeta}>
            {" "}
            &nbsp;· {text("edited")} {formatLocalTime(message.EditedTimestamp)}
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
