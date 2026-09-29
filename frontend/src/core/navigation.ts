import { batch, signal } from "@preact/signals";
import type { Channel, Guild } from "./api";
import { params } from "./api";
import { text } from "./i18n";

type Page = "overview" | "messages" | "settings";
const initial = new URL(location.href);
export const page = signal<Page>(
  initial.pathname === "/settings"
    ? "settings"
    : initial.pathname === "/messages"
      ? "messages"
      : "overview",
);
export const guild = signal(initial.searchParams.get("guild") || "");
export const channel = signal(initial.searchParams.get("channel") || "");
export const scope = signal(initial.searchParams.get("scope") === "dm" ? "dm" : "");
export const query = signal(initial.searchParams.get("q") || "");
export const around = signal(initial.searchParams.get("around") || "");

export function syncFromURL() {
  const url = new URL(location.href);
  batch(() => {
    page.value =
      url.pathname === "/settings"
        ? "settings"
        : url.pathname === "/messages"
          ? "messages"
          : "overview";
    guild.value = url.searchParams.get("guild") || "";
    channel.value = url.searchParams.get("channel") || "";
    scope.value = url.searchParams.get("scope") === "dm" ? "dm" : "";
    query.value = url.searchParams.get("q") || "";
    around.value = url.searchParams.get("around") || "";
  });
}
export function navigate(next: {
  page?: Page;
  guild?: string;
  channel?: string;
  scope?: string;
  q?: string;
  around?: string;
}) {
  const targetPage = next.page ?? page.value;
  const targetGuild = next.guild ?? guild.value;
  const targetChannel = next.channel ?? channel.value;
  const targetScope = next.scope ?? scope.value;
  const targetQuery = next.q ?? query.value;
  const targetAround = next.around ?? around.value;
  const search =
    targetPage === "messages"
      ? params({
          guild: targetGuild,
          channel: targetChannel,
          scope: targetGuild ? "" : targetScope,
          q: targetQuery,
          around: targetAround,
        })
      : "";
  const path = targetPage === "overview" ? "/" : `/${targetPage}`;
  history.pushState(null, "", path + (search ? `?${search}` : ""));
  batch(() => {
    page.value = targetPage;
    guild.value = targetGuild;
    channel.value = targetChannel;
    scope.value = targetScope;
    query.value = targetQuery;
    around.value = targetAround;
  });
}
export function guildName(item: Guild) {
  return item.Name || item.ID;
}
export function channelName(item: Channel) {
  return item.Name || `${text("unknownChannel")} ${item.ID}`;
}
export function initials(value: string) {
  const words = value.trim().split(/\s+/u).filter(Boolean);
  if (words.length > 1)
    return words
      .slice(0, 2)
      .map((word) => [...word][0])
      .join("")
      .toLocaleUpperCase();
  return [...(words[0] || "?")].slice(0, 2).join("").toLocaleUpperCase();
}
