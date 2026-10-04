import type { Channel } from "../core/api";
import { text } from "../core/i18n";
import { channelName } from "../core/navigation";
import { s } from "../styles";

export function ChannelButton({
  item,
  active,
  depth = 0,
  onClick,
}: {
  item: Channel;
  active: boolean;
  depth?: number;
  onClick: () => void;
}) {
  const thread = item.Kind === 10 || item.Kind === 11 || item.Kind === 12;
  const forum = item.Kind === 15 || item.Kind === 16;
  return (
    <button
      class={`${s.sidebarItem} ${active ? s.sidebarSelected : ""}`}
      title={`${channelName(item)}${item.Deleted ? ` · ${text("deleted")}` : ""}`}
      style={{ paddingLeft: `${12 + depth * 16}px` }}
      onClick={onClick}
    >
      {thread ? "↳" : forum ? "▣" : "#"} &nbsp;{channelName(item)}
      {item.Deleted ? ` · ${text("deleted")}` : ""}
    </button>
  );
}
