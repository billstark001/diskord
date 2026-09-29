import { useEffect, useState } from "preact/hooks";
import type { Guild } from "../core/api";
import { text } from "../core/i18n";
import { guildName, initials } from "../core/navigation";
import { s } from "../styles";

type Props = {
  guilds: Guild[];
  page: "overview" | "messages" | "settings";
  guild: string;
  scope: string;
  onPage: (page: "overview" | "settings") => void;
  onSelect: (guild: string, scope: string) => void;
};

function GuildBadge({ item }: { item: Guild }) {
  const [failed, setFailed] = useState(false);
  useEffect(() => setFailed(false), [item.IconHash]);
  return item.IconHash && !failed ? (
    <img
      class={s.railIcon}
      src={`/assets/${item.IconHash}`}
      alt=""
      loading="lazy"
      onError={() => setFailed(true)}
    />
  ) : (
    <span class={s.railFallback} aria-hidden="true">
      {initials(guildName(item))}
    </span>
  );
}

export function ServerRail({ guilds, page, guild, scope, onPage, onSelect }: Props) {
  return (
    <aside class={s.rail} aria-label={text("servers")}>
      <button
        class={`${s.railButton} ${page === "overview" ? s.railSelected : ""}`}
        title={text("overview")}
        onClick={() => onPage("overview")}
      >
        ◈
      </button>
      <button
        class={`${s.railButton} ${page === "messages" && !guild && !scope ? s.railSelected : ""}`}
        title={text("all")}
        onClick={() => onSelect("", "")}
      >
        ◎
      </button>
      <button
        class={`${s.railButton} ${page === "messages" && scope === "dm" ? s.railSelected : ""}`}
        title={text("dm")}
        onClick={() => onSelect("", "dm")}
      >
        ✉
      </button>
      <div class={s.railDivider} />
      {guilds.map((item) => (
        <button
          key={item.ID}
          class={`${s.railButton} ${page === "messages" && guild === item.ID ? s.railSelected : ""}`}
          title={`${guildName(item)}${item.Deleted ? ` · ${text("deleted")}` : item.Unavailable ? ` · ${text("unavailable")}` : ""}`}
          onClick={() => onSelect(item.ID, "")}
        >
          <GuildBadge item={item} />
        </button>
      ))}
      <div class={s.spacer} />
      <button
        class={`${s.railButton} ${page === "settings" ? s.railSelected : ""}`}
        title={text("settings")}
        onClick={() => onPage("settings")}
      >
        ⚙
      </button>
    </aside>
  );
}
