import type { Channel, Navigation } from "../core/api";
import { text } from "../core/i18n";
import { channelName, guildName } from "../core/navigation";
import { s } from "../styles";
import { ChannelButton } from "./ChannelButton";

type Props = {
  navigation?: Navigation;
  loading: boolean;
  page: "overview" | "messages" | "settings";
  guild: string;
  channel: string;
  scope: string;
  onSelect: (channel: string) => void;
};

function groupedChannels(channels: Channel[]) {
  const categories = channels.filter((item) => item.Kind === 4);
  const ids = new Set(channels.map((item) => item.ID));
  const roots = channels.filter(
    (item) => item.Kind !== 4 && (!item.ParentID || !ids.has(item.ParentID)),
  );
  const children = new Map<string, Channel[]>();
  for (const item of channels) {
    if (item.Kind === 4 || !item.ParentID || !ids.has(item.ParentID)) continue;
    const entries = children.get(item.ParentID) || [];
    entries.push(item);
    children.set(item.ParentID, entries);
  }
  return { categories, roots, children };
}

export function ChannelSidebar({
  navigation,
  loading,
  page,
  guild,
  channel,
  scope,
  onSelect,
}: Props) {
  const selectedGuild = navigation?.guilds.find((item) => item.ID === guild);
  const { categories, roots, children } = groupedChannels(navigation?.channels || []);
  const header = guild
    ? guildName(
        selectedGuild || { ID: guild, Name: "", IconHash: "", Unavailable: false, Deleted: false },
      )
    : scope === "dm"
      ? text("dm")
      : text("servers");
  const channelBranch = (item: Channel, depth = 0, ancestors = new Set<string>()) => {
    if (ancestors.has(item.ID)) return null;
    const next = new Set(ancestors);
    next.add(item.ID);
    return (
      <div key={item.ID}>
        <ChannelButton
          item={item}
          depth={depth}
          active={channel === item.ID}
          onClick={() => onSelect(item.ID)}
        />
        {children.get(item.ID)?.map((child) => channelBranch(child, depth + 1, next))}
      </div>
    );
  };
  if (page !== "messages") {
    return (
      <aside class={s.sidebar} aria-label={text("channels")}>
        <div class={s.sidebarHeader}>{text(page)}</div>
      </aside>
    );
  }
  return (
    <aside class={s.sidebar} aria-label={text("channels")}>
      <div class={s.sidebarHeader}>{header}</div>
      <div class={s.sidebarScroll}>
        <button
          class={`${s.sidebarItem} ${!channel && page === "messages" ? s.sidebarSelected : ""}`}
          onClick={() => onSelect("")}
        >
          {text("allChannels")}
        </button>
        {guild || scope === "dm" ? (
          <>
            {categories.map((category) => (
              <div key={category.ID}>
                <div class={s.category}>
                  {channelName(category)}
                  {category.Deleted ? ` · ${text("deleted")}` : ""}
                </div>
                {children.get(category.ID)?.map((item) => channelBranch(item))}
              </div>
            ))}
            {roots.map((item) => channelBranch(item))}
            {!loading && !navigation?.channels.length && (
              <p class={s.muted}>{text("noChannels")}</p>
            )}
          </>
        ) : (
          <p class={s.muted}>
            {loading
              ? text("loading")
              : navigation?.guilds.length
                ? text("archiveHint")
                : text("noServers")}
          </p>
        )}
      </div>
    </aside>
  );
}
