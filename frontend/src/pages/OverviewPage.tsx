import { useQuery } from "@tanstack/preact-query";
import { api } from "../core/api";
import type { Overview } from "../core/api";
import { locale, text } from "../core/i18n";
import type { Key } from "../core/i18n";
import { s } from "../styles";

export function OverviewPage() {
  const result = useQuery({
    queryKey: ["overview"],
    queryFn: () => api<Overview>("/api/overview"),
    refetchInterval: 5000,
  });
  if (result.isLoading) return <p>{text("loading")}</p>;
  if (result.isError || !result.data)
    return (
      <div class={`${s.notice} ${s.error}`}>
        {text("statusUnavailable")}: {String(result.error)}
      </div>
    );
  const { counts, stats } = result.data;
  const tiles: [Key, number][] = [
    ["users", counts.Users],
    ["servers", counts.Guilds],
    ["channels", counts.Channels],
    ["messages", counts.Messages],
  ];
  return (
    <div class={s.narrowContent}>
      <div class={s.eyebrow}>{text("overviewHeading")}</div>
      <h2 class={s.heading}>{text("observed")}</h2>
      <p class={s.muted}>{text("archiveHint")}</p>
      <div class={s.stats}>
        {tiles.map(([label, value]) => (
          <div class={s.card} key={label}>
            <div class={s.muted}>{text(label)}</div>
            <strong class={s.statValue}>{value.toLocaleString(locale.value)}</strong>
          </div>
        ))}
      </div>
      <div class={s.card}>
        <h3>{text("health")}</h3>
        <div class={s.stats}>
          {(
            [
              ["ws", stats.ActiveGateway],
              ["http", stats.HTTPBodies],
              ["gateway", stats.GatewayEvents],
              ["batches", stats.BatchesCommitted],
            ] as [Key, number][]
          ).map(([label, value]) => (
            <div key={label}>
              {text(label)}: <strong>{value}</strong>
            </div>
          ))}
        </div>
        <p class={s.muted}>
          {text("errors")}:{" "}
          {stats.DroppedBatches +
            stats.HTTPDropped +
            stats.GatewayLost +
            stats.UnsupportedGateway +
            stats.DecodeErrors +
            stats.StoreErrors +
            stats.AssetSkipped}
        </p>
      </div>
      <div class={s.card}>
        <div>
          {text("proxy")}: <code class={s.mono}>{result.data.proxy}</code>
        </div>
        <div>
          {text("console")}: <code class={s.mono}>{result.data.web}</code>
        </div>
        <div>
          {text("runtime")}: <code class={s.mono}>{result.data.runtime}</code>
        </div>
      </div>
    </div>
  );
}
