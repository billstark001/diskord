import { useEffect, useState } from "preact/hooks";
import { useInfiniteQuery } from "@tanstack/preact-query";
import { api, params } from "../core/api";
import type { Message, Messages } from "../core/api";
import { text } from "../core/i18n";
import { MessageItem } from "../components/MessageItem";
import { s } from "../styles";

export function ArchivePage({
  guild: guildID,
  channel: channelID,
  scope: selectedScope,
  query: submitted,
  onSearch,
  onRefresh,
}: {
  guild: string;
  channel: string;
  scope: string;
  query: string;
  onSearch: (q: string) => void;
  onRefresh: () => void;
}) {
  const [draft, setDraft] = useState(submitted);
  useEffect(() => setDraft(submitted), [submitted]);
  const pages = useInfiniteQuery({
    queryKey: ["messages", guildID, channelID, selectedScope, submitted],
    refetchInterval: 10000,
    initialPageParam: "",
    queryFn: ({ pageParam }) =>
      api<Messages>(
        `/api/messages?${params({ guild: guildID, channel: channelID, scope: selectedScope, q: submitted, before: pageParam })}`,
      ),
    getNextPageParam: (last) => last.next || undefined,
  });
  const rowsByID = new Map<string, Message>();
  for (const row of pages.data?.pages.flatMap((part) => part.rows) || []) {
    if (!rowsByID.has(row.ID)) rowsByID.set(row.ID, row);
  }
  const rows = [...rowsByID.values()];
  return (
    <div class={s.narrowContent}>
      <form
        class={s.toolbar}
        onSubmit={(event) => {
          event.preventDefault();
          onSearch(draft.trim());
        }}
      >
        <input
          class={`${s.input} ${s.searchInput}`}
          aria-label={text("search")}
          placeholder={text("search")}
          value={draft}
          onInput={(event) => setDraft(event.currentTarget.value)}
          maxLength={256}
        />
        <button class={s.button}>{text("filter")}</button>
        <button
          type="button"
          class={`${s.button} ${s.quietButton}`}
          onClick={() => {
            setDraft("");
            onSearch("");
          }}
        >
          {text("clear")}
        </button>
        <button type="button" class={`${s.button} ${s.quietButton}`} onClick={onRefresh}>
          {text("refresh")}
        </button>
      </form>
      {pages.isLoading && <p class={s.muted}>{text("loading")}</p>}
      {pages.isError && <div class={`${s.notice} ${s.error}`}>{String(pages.error)}</div>}
      {!pages.isLoading && !pages.isError && rows.length === 0 && (
        <div class={s.card}>{text("noMessages")}</div>
      )}
      {rows.map((message) => (
        <MessageItem key={message.ID} message={message} />
      ))}
      {pages.hasNextPage && (
        <button
          class={`${s.button} ${s.quietButton}`}
          disabled={pages.isFetchingNextPage}
          onClick={() => void pages.fetchNextPage()}
        >
          {pages.isFetchingNextPage ? text("loading") : text("older")}
        </button>
      )}
    </div>
  );
}
