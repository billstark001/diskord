import { useEffect, useLayoutEffect, useRef, useState } from "preact/hooks";
import { useInfiniteQuery } from "@tanstack/preact-query";
import { api, params } from "../core/api";
import type { Message, Messages } from "../core/api";
import { text } from "../core/i18n";
import { MessageItem } from "../components/MessageItem";
import { s } from "../styles";

type Anchor = { id: string; top: number };

export function ArchivePage({
  guild: guildID,
  channel: channelID,
  scope: selectedScope,
  query: submitted,
  around,
  onSearch,
  onJump,
  onRefresh,
}: {
  guild: string;
  channel: string;
  scope: string;
  query: string;
  around: string;
  onSearch: (q: string) => void;
  onJump: (message: Message) => void;
  onRefresh: () => void;
}) {
  const [draft, setDraft] = useState(submitted);
  const viewport = useRef<HTMLDivElement>(null);
  const initialized = useRef("");
  const pendingAnchor = useRef<Anchor | null>(null);
  const followBottom = useRef(true);
  useEffect(() => setDraft(submitted), [submitted]);

  const streamKey = [guildID, channelID, selectedScope, submitted, around].join(":");
  const pages = useInfiniteQuery({
    queryKey: ["messages", guildID, channelID, selectedScope, submitted, around],
    initialPageParam: around ? `around:${around}` : "",
    refetchInterval: 10000,
    queryFn: ({ pageParam }) => {
      const cursor = String(pageParam || "");
      const [kind, id] = cursor ? cursor.split(":", 2) : ["", ""];
      return api<Messages>(
        `/api/messages?${params({
          guild: guildID,
          channel: channelID,
          scope: selectedScope,
          q: submitted,
          before: kind === "before" ? id : "",
          after: kind === "after" ? id : "",
          around: kind === "around" ? id : "",
        })}`,
      );
    },
    getPreviousPageParam: (first) => (first.before ? `before:${first.before}` : undefined),
    getNextPageParam: (last) => (last.after ? `after:${last.after}` : undefined),
  });

  const rowsByID = new Map<string, Message>();
  for (const row of pages.data?.pages.flatMap((part) => part.rows) || []) rowsByID.set(row.ID, row);
  const rows = [...rowsByID.values()];
  const firstID = rows[0]?.ID || "";
  const lastID = rows.at(-1)?.ID || "";

  function visibleAnchor(): Anchor | null {
    const element = viewport.current;
    if (!element) return null;
    const top = element.getBoundingClientRect().top;
    for (const row of element.querySelectorAll<HTMLElement>("[data-message-id]")) {
      if (row.getBoundingClientRect().bottom >= top) {
        return { id: row.dataset.messageId || "", top: row.getBoundingClientRect().top - top };
      }
    }
    return null;
  }

  function fetchOlder() {
    if (!pages.hasPreviousPage || pages.isFetchingPreviousPage) return;
    pendingAnchor.current = visibleAnchor();
    void pages.fetchPreviousPage();
  }
  function fetchNewer() {
    if (!pages.hasNextPage || pages.isFetchingNextPage) return;
    void pages.fetchNextPage();
  }

  useLayoutEffect(() => {
    const element = viewport.current;
    if (!element || pages.isPending) return;
    if (initialized.current !== streamKey) {
      initialized.current = streamKey;
      pendingAnchor.current = null;
      const target = around && element.querySelector<HTMLElement>(`[data-message-id="${around}"]`);
      if (target) {
        target.scrollIntoView({ block: "center" });
        followBottom.current = false;
      } else {
        element.scrollTop = element.scrollHeight;
        followBottom.current = true;
      }
      return;
    }
    if (pendingAnchor.current) {
      const anchor = pendingAnchor.current;
      const row = element.querySelector<HTMLElement>(`[data-message-id="${anchor.id}"]`);
      if (row)
        element.scrollTop +=
          row.getBoundingClientRect().top - element.getBoundingClientRect().top - anchor.top;
      pendingAnchor.current = null;
      return;
    }
    if (followBottom.current) element.scrollTop = element.scrollHeight;
  }, [streamKey, firstID, lastID, rows.length, pages.isPending]);

  useEffect(() => {
    const element = viewport.current;
    if (!element || pages.isPending || !rows.length) return;
    if (element.scrollHeight <= element.clientHeight + 2) {
      if (pages.hasNextPage) fetchNewer();
      else if (pages.hasPreviousPage) fetchOlder();
    }
  }, [
    streamKey,
    firstID,
    lastID,
    rows.length,
    pages.isPending,
    pages.hasNextPage,
    pages.hasPreviousPage,
  ]);

  return (
    <div class={s.archivePage}>
      <div
        class={s.messageViewport}
        ref={viewport}
        onScroll={(event) => {
          if (initialized.current !== streamKey) return;
          const element = event.currentTarget;
          const remaining = element.scrollHeight - element.scrollTop - element.clientHeight;
          followBottom.current = remaining < 96;
          if (element.scrollTop < 120) fetchOlder();
          if (remaining < 120) fetchNewer();
        }}
      >
        <div class={s.messageStack}>
          {pages.hasPreviousPage && (
            <button
              class={`${s.button} ${s.quietButton}`}
              disabled={pages.isFetchingPreviousPage}
              onClick={fetchOlder}
            >
              {pages.isFetchingPreviousPage ? text("loading") : text("older")}
            </button>
          )}
          {pages.isPending && <p class={s.muted}>{text("loading")}</p>}
          {pages.isError && <div class={`${s.notice} ${s.error}`}>{String(pages.error)}</div>}
          {!pages.isPending && !pages.isError && rows.length === 0 && (
            <div class={s.card}>{text("noMessages")}</div>
          )}
          {rows.map((message) => (
            <MessageItem
              key={message.ID}
              message={message}
              focused={message.ID === around}
              onJump={submitted ? () => onJump(message) : undefined}
            />
          ))}
          {pages.isFetchingNextPage && <p class={s.muted}>{text("loading")}</p>}
          {pages.hasNextPage && (
            <button class={`${s.button} ${s.quietButton}`} onClick={fetchNewer}>
              {text("newer")}
            </button>
          )}
        </div>
      </div>
      <form
        class={s.archiveSearch}
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
    </div>
  );
}
