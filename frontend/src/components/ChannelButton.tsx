import type { Channel } from "../core/api";
import { text } from "../core/i18n";
import { channelName } from "../core/navigation";
import { s } from "../styles";

export function ChannelButton({
  item,
  active,
  onClick,
}: {
  item: Channel;
  active: boolean;
  onClick: () => void;
}) {
  return (
    <button
      class={`${s.sidebarItem} ${active ? s.sidebarSelected : ""}`}
      title={`${channelName(item)}${item.Deleted ? ` · ${text("deleted")}` : ""}`}
      onClick={onClick}
    >
      # &nbsp;{channelName(item)}
      {item.Deleted ? ` · ${text("deleted")}` : ""}
    </button>
  );
}
