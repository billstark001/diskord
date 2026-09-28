import { useLayoutEffect, useRef } from "preact/hooks";
import { useQuery, useQueryClient } from "@tanstack/preact-query";
import { api, params } from "../core/api";
import type { Navigation } from "../core/api";
import { text } from "../core/i18n";
import {
  page,
  guild,
  channel,
  scope,
  query,
  navigate,
  guildName,
  channelName,
} from "../core/navigation";
import { LanguageSelect } from "./LanguageSelect";
import { ServerRail } from "./ServerRail";
import { ChannelSidebar } from "./ChannelSidebar";
import { ArchivePage } from "../pages/ArchivePage";
import { OverviewPage } from "../pages/OverviewPage";
import { SettingsPage } from "../pages/SettingsPage";
import { s } from "../styles";

export function ConsoleShell({ csrf, onLogout }: { csrf: string; onLogout: () => void }) {
  const client = useQueryClient();
  const scroll = useRef<HTMLElement>(null);
  const currentPage = page.value,
    currentGuild = guild.value,
    currentChannel = channel.value,
    currentScope = scope.value,
    currentQuery = query.value;
  const nav = useQuery({
    queryKey: ["navigation", currentGuild, currentScope],
    queryFn: () =>
      api<Navigation>(`/api/navigation?${params({ guild: currentGuild, scope: currentScope })}`),
    staleTime: 10000,
  });
  const selectedGuild = nav.data?.guilds.find((item) => item.ID === currentGuild);
  const selectedChannel = nav.data?.channels.find((item) => item.ID === currentChannel);
  const title =
    currentPage === "overview"
      ? text("overview")
      : currentPage === "settings"
        ? text("settings")
        : selectedChannel
          ? `# ${channelName(selectedChannel)}`
          : currentGuild
            ? guildName(
                selectedGuild || { ID: currentGuild, Name: "", Unavailable: false, Deleted: false },
              )
            : currentScope === "dm"
              ? text("dm")
              : text("all");
  function select(next: { guild: string; channel: string; scope: string }) {
    navigate({ page: "messages", ...next, q: "" });
  }
  async function logout() {
    await api("/api/logout", { method: "POST", body: "{}" }, csrf);
    onLogout();
  }
  const scrollKey = `${currentPage}:${currentGuild}:${currentChannel}:${currentScope}:${currentQuery}`;
  const scrollPositions = useRef(new Map<string, number>());
  useLayoutEffect(() => {
    scroll.current?.scrollTo({ top: scrollPositions.current.get(scrollKey) || 0 });
  }, [scrollKey]);
  return (
    <div class={s.shell}>
      <ServerRail
        guilds={nav.data?.guilds || []}
        page={currentPage}
        guild={currentGuild}
        scope={currentScope}
        onPage={(page) => navigate({ page })}
        onSelect={(guild, scope) => select({ guild, channel: "", scope })}
      />
      <ChannelSidebar
        navigation={nav.data}
        loading={nav.isLoading}
        page={currentPage}
        guild={currentGuild}
        channel={currentChannel}
        scope={currentScope}
        onSelect={(channel) => select({ guild: currentGuild, channel, scope: currentScope })}
      />
      <main class={s.main}>
        <header class={s.top}>
          <h1 class={s.title}>{title}</h1>
          <span class={s.spacer} />
          <LanguageSelect csrf={csrf} compact />
          <button class={`${s.button} ${s.quietButton}`} onClick={() => void logout()}>
            {text("logout")}
          </button>
        </header>
        <section
          class={s.content}
          ref={scroll}
          onScroll={(event) =>
            scrollPositions.current.set(scrollKey, event.currentTarget.scrollTop)
          }
        >
          {currentPage === "overview" ? (
            <OverviewPage />
          ) : currentPage === "settings" ? (
            <SettingsPage csrf={csrf} />
          ) : (
            <ArchivePage
              guild={currentGuild}
              channel={currentChannel}
              scope={currentScope}
              query={currentQuery}
              onSearch={(q) => {
                navigate({ q });
              }}
              onRefresh={() => void client.invalidateQueries({ queryKey: ["messages"] })}
            />
          )}
        </section>
        <footer class={s.footer}>diskord · {text("archiveHint")}</footer>
      </main>
    </div>
  );
}
