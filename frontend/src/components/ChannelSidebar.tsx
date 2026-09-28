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
  const categoryIDs = new Set(categories.map((item) => item.ID));
  const roots = channels.filter((item) => item.Kind !== 4 && !categoryIDs.has(item.ParentID));
  const children = new Map<string, Channel[]>();
  for (const item of channels) {
    if (item.Kind === 4 || !categoryIDs.has(item.ParentID)) continue;
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
    ? guildName(selectedGuild || { ID: guild, Name: "", Unavailable: false, Deleted: false })
    : scope === "dm"
      ? text("dm")
      : text("servers");
  const channelButton = (item: Channel) => (
    <ChannelButton
      key={item.ID}
      item={item}
      active={page === "messages" && channel === item.ID}
      onClick={() => onSelect(item.ID)}
    />
  );
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
                {children.get(category.ID)?.map(channelButton)}
              </div>
            ))}
            {roots.map(channelButton)}
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
