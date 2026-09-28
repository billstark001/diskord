import { useState } from "preact/hooks";
import { useQuery, useQueryClient } from "@tanstack/preact-query";
import { api } from "../core/api";
import type { Settings } from "../core/api";
import { text } from "../core/i18n";
import {
  CertificateActions,
  CertificateCard,
  LoggingCard,
  ResourceCard,
} from "../components/SettingsCards";
import { s } from "../styles";

export function SettingsPage({ csrf }: { csrf: string }) {
  const client = useQueryClient();
  const settings = useQuery({
    queryKey: ["settings"],
    queryFn: () => api<Settings>("/api/settings"),
  });
  const [notice, setNotice] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  async function action(path: string, body: object) {
    setBusy(true);
    setError("");
    setNotice("");
    try {
      await api(path, { method: "POST", body: JSON.stringify(body) }, csrf);
      setNotice(text("saved"));
      await client.invalidateQueries({ queryKey: ["settings"] });
    } catch (reason) {
      setError(String(reason));
    } finally {
      setBusy(false);
    }
  }

  if (settings.isLoading) return <p>{text("loading")}</p>;
  if (settings.isError || !settings.data)
    return <div class={`${s.notice} ${s.error}`}>{String(settings.error)}</div>;
  const data = settings.data;
  return (
    <div class={s.narrowContent}>
      <div class={s.eyebrow}>{text("controlsHeading")}</div>
      <h2 class={s.heading}>{text("settings")}</h2>
      {notice && <div class={s.notice}>{notice}</div>}
      {error && <div class={`${s.notice} ${s.error}`}>{error}</div>}
      <div class={s.settingsGrid}>
        <ResourceCard
          enabled={data.resourcesEnabled}
          busy={busy}
          onChange={(enabled) =>
            void action("/api/settings", { revision: data.revision, resourcesEnabled: enabled })
          }
        />
        <CertificateCard settings={data} />
        <CertificateActions
          settings={data}
          busy={busy}
          onAction={(path, body) => void action(path, body)}
        />
        <LoggingCard settings={data} />
      </div>
    </div>
  );
}
