export type Locale = "zh-CN" | "en";
export type Session = { authenticated: boolean; csrf: string; locale: Locale };
export type Guild = { ID: string; Name: string; Unavailable: boolean; Deleted: boolean };
export type Channel = {
  ID: string;
  GuildID: string;
  ParentID: string;
  Name: string;
  Kind: number;
  Deleted: boolean;
};
export type Message = {
  ID: string;
  ChannelID: string;
  ChannelName: string;
  GuildName: string;
  AuthorName: string;
  AvatarHash: string;
  Content: string;
  Timestamp: string;
  EditedTimestamp: string;
  Source: string;
  Deleted: boolean;
  Attachments: { Name: string; Hash: string }[];
  Reactions: {
    EmojiID: string;
    EmojiName: string;
    EmojiHash: string;
    Animated: boolean;
    Count: number;
    UnknownCount: number;
    Users: string[];
  }[];
};
export type Navigation = { guilds: Guild[]; channels: Channel[] };
export type Messages = { rows: Message[]; next: string };
export type Counts = { Users: number; Guilds: number; Channels: number; Messages: number };
export type Stats = {
  ActiveGateway: number;
  HTTPBodies: number;
  GatewayEvents: number;
  BatchesCommitted: number;
  Assets: number;
  DroppedBatches: number;
  HTTPDropped: number;
  GatewayLost: number;
  UnsupportedGateway: number;
  DecodeErrors: number;
  StoreErrors: number;
  AssetSkipped: number;
};
export type Overview = {
  counts: Counts;
  stats: Stats;
  proxy: string;
  web: string;
  runtime: string;
  resourcesEnabled: boolean;
};
export type Settings = {
  revision: string;
  resourcesEnabled: boolean;
  cert: string;
  key: string;
  fingerprint: string;
  expires: string;
  trustNotice: string;
  caFiles: string[];
  fileLoggerEnabled: boolean;
  discordLoggerEnabled: boolean;
};

export async function api<T>(path: string, init?: RequestInit, csrf?: string): Promise<T> {
  const response = await fetch(path, {
    credentials: "same-origin",
    cache: "no-store",
    ...init,
    headers: {
      ...(init?.body ? { "Content-Type": "application/json" } : {}),
      ...(csrf ? { "X-CSRF-Token": csrf } : {}),
      ...init?.headers,
    },
  });
  if (!response.ok) {
    let message = `HTTP ${response.status}`;
    try {
      const data = (await response.json()) as { error?: string };
      if (data.error) message = data.error;
    } catch {
      /* keep status */
    }
    throw new Error(message);
  }
  return response.json() as Promise<T>;
}

export function params(values: Record<string, string>) {
  const search = new URLSearchParams();
  for (const [key, value] of Object.entries(values)) if (value) search.set(key, value);
  return search.toString();
}
