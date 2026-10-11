import { useEffect, useState } from "preact/hooks";
import { useQuery } from "@tanstack/preact-query";
import { api } from "../core/api";
import type { Forum, ForumPost } from "../core/api";
import { text } from "../core/i18n";
import { formatLocalTime, snowflakeTime } from "../core/time";
import { s } from "../styles";

function dateLabel(timestamp: string, id: string) {
  const parsed = timestamp ? new Date(timestamp) : null;
  const date = parsed && !Number.isNaN(parsed.getTime()) ? parsed : snowflakeTime(id);
  return date ? formatLocalTime(date) : "";
}
function compareIDs(a: string, b: string) {
  return a.length - b.length || (a < b ? -1 : a === b ? 0 : 1);
}

export function ForumPage({ channel, onOpen }: { channel: string; onOpen: (id: string) => void }) {
  const forum = useQuery({
    queryKey: ["forum", channel],
    queryFn: () => api<Forum>(`/api/forum?channel=${encodeURIComponent(channel)}`),
    refetchInterval: 10000,
  });
  const [search, setSearch] = useState("");
  const [sort, setSort] = useState<"activity" | "creation">("activity");
  const [layout, setLayout] = useState<"list" | "gallery">("list");
  useEffect(() => setSearch(""), [channel]);
  useEffect(() => {
    setSort(forum.data?.DefaultSortOrder === 1 ? "creation" : "activity");
    setLayout(forum.data?.DefaultForumLayout === 2 ? "gallery" : "list");
  }, [channel, forum.data?.DefaultSortOrder, forum.data?.DefaultForumLayout]);
  const needle = search.trim().toLocaleLowerCase();
  const posts = (forum.data?.Posts || [])
    .filter(
      (post) =>
        !needle ||
        [post.Name, post.Content, post.AuthorName, ...post.Tags].some((part) =>
          part.toLocaleLowerCase().includes(needle),
        ),
    )
    .sort(
      (a, b) =>
        Number(b.Pinned) - Number(a.Pinned) ||
        (sort === "creation"
          ? compareIDs(b.ID, a.ID)
          : compareIDs(b.LastActivityID, a.LastActivityID)),
    );
  return (
    <div class={s.forumPage}>
      {forum.data?.Topic && <p class={s.forumTopic}>{forum.data.Topic}</p>}
      <div class={s.forumToolbar}>
        <input
          class={`${s.input} ${s.forumSearch}`}
          aria-label={text("searchPosts")}
          placeholder={text("searchPosts")}
          value={search}
          onInput={(event) => setSearch(event.currentTarget.value)}
        />
        <label class={s.forumControl}>
          {text("sortPosts")}
          <select
            class={s.forumSelect}
            value={sort}
            onChange={(event) => setSort(event.currentTarget.value as "activity" | "creation")}
          >
            <option value="activity">{text("latestActivity")}</option>
            <option value="creation">{text("newestPosts")}</option>
          </select>
        </label>
        <label class={s.forumControl}>
          {text("viewPosts")}
          <select
            class={s.forumSelect}
            value={layout}
            onChange={(event) => setLayout(event.currentTarget.value as "list" | "gallery")}
          >
            <option value="list">{text("listView")}</option>
            <option value="gallery">{text("galleryView")}</option>
          </select>
        </label>
      </div>
      {forum.isPending && <p class={s.muted}>{text("loading")}</p>}
      {forum.isError && <p class={`${s.notice} ${s.error}`}>{String(forum.error)}</p>}
      {!forum.isPending && !forum.isError && posts.length === 0 && (
        <p class={s.muted}>{text("noPosts")}</p>
      )}
      <div class={`${s.forumPosts} ${layout === "gallery" ? s.forumGallery : ""}`}>
        {posts.map((post: ForumPost) => (
          <button
            data-export
            data-export-thread={post.ID}
            data-export-name={post.Name}
            key={post.ID}
            class={s.forumPost}
            onClick={() => onOpen(post.ID)}
          >
            <div class={s.forumPostText}>
              <div class={s.forumPostTitle}>
                {post.Pinned && <span aria-label={text("pinned")}>📌 </span>}
                {post.Name}
                {post.Deleted && <span class={s.muted}> · {text("deleted")}</span>}
              </div>
              <div class={s.forumPostMeta}>
                {post.AuthorName || text("unknownUser")}
                {post.Archived && <> · {text("archived")}</>}
              </div>
              {post.Content && <p class={s.forumExcerpt}>{post.Content}</p>}
              {!!post.Tags?.length && (
                <div class={s.forumTags}>
                  {post.Tags.map((tag) => (
                    <span key={tag} class={s.forumTag}>
                      {tag}
                    </span>
                  ))}
                </div>
              )}
              <div class={s.forumPostStats}>
                {post.Reactions?.slice(0, 3).map((reaction) => (
                  <span key={reaction.EmojiID || reaction.EmojiName}>
                    {reaction.EmojiHash ? (
                      <img
                        class={s.reactionEmoji}
                        src={`/assets/${reaction.EmojiHash}`}
                        alt={reaction.EmojiName}
                      />
                    ) : reaction.EmojiID ? (
                      `:${reaction.EmojiName}:`
                    ) : (
                      reaction.EmojiName
                    )}{" "}
                    {reaction.Count}
                  </span>
                ))}
                <span>◌ {post.ReplyCount}</span>
                <span>{dateLabel(post.LastActivity, post.LastActivityID || post.ID)}</span>
              </div>
            </div>
            {post.Attachment?.Hash && (
              <img
                class={s.forumThumbnail}
                src={`/assets/${post.Attachment.Hash}`}
                alt={post.Attachment.Name}
                loading="lazy"
              />
            )}
          </button>
        ))}
      </div>
      <p class={s.forumFootnote}>{text("observedPostsOnly")}</p>
    </div>
  );
}
