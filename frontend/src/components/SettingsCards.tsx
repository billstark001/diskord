import { useState } from "preact/hooks";
import type { Settings } from "../core/api";
import { text } from "../core/i18n";
import { s } from "../styles";

export function ResourceCard({
  enabled,
  busy,
  onChange,
}: {
  enabled: boolean;
  busy: boolean;
  onChange: (enabled: boolean) => void;
}) {
  return (
    <section class={s.card}>
      <h3>{text("resources")}</h3>
      <label class={s.switch}>
        <input
          type="checkbox"
          checked={enabled}
          disabled={busy}
          onChange={(event) => onChange(event.currentTarget.checked)}
        />
        {text("enabled")}
      </label>
    </section>
  );
}

export function CertificateCard({ settings }: { settings: Settings }) {
  return (
    <section class={s.card}>
      <h3>{text("certificate")}</h3>
      {(
        [
          ["certPath", settings.cert],
          ["keyPath", settings.key],
          ["fingerprint", settings.fingerprint],
          ["expires", settings.expires],
        ] as const
      ).map(([label, value]) => (
        <div class={s.field} key={label}>
          <span class={s.label}>{text(label)}</span>
          <code class={s.mono}>{value}</code>
        </div>
      ))}
      {settings.trustNotice && <div class={`${s.notice} ${s.error}`}>{settings.trustNotice}</div>}
    </section>
  );
}

export function CertificateActions({
  settings,
  busy,
  onAction,
}: {
  settings: Settings;
  busy: boolean;
  onAction: (path: string, body: object) => void;
}) {
  const [cert, setCert] = useState("");
  const [key, setKey] = useState("");
  const fields = () => (
    <>
      <label class={s.label}>
        {text("certPath")}
        <input
          class={`${s.input} ${s.fullInput}`}
          list="ca-files"
          value={cert}
          required
          placeholder="ca/root.pem"
          onInput={(event) => setCert(event.currentTarget.value)}
        />
      </label>
      <label class={s.label}>
        {text("keyPath")}
        <input
          class={`${s.input} ${s.fullInput}`}
          value={key}
          required
          placeholder="ca/root.key"
          onInput={(event) => setKey(event.currentTarget.value)}
        />
      </label>
    </>
  );
  return (
    <>
      <section class={s.card}>
        <h3>{text("selectCA")}</h3>
        <form
          onSubmit={(event) => {
            event.preventDefault();
            onAction("/api/ca/select", { revision: settings.revision, cert, key });
          }}
        >
          {fields()}
          <datalist id="ca-files">
            {settings.caFiles.map((file) => (
              <option value={file} key={file} />
            ))}
          </datalist>
          <button class={s.button} disabled={busy}>
            {text("selectCA")}
          </button>
        </form>
      </section>
      <section class={s.card}>
        <h3>{text("issueCA")}</h3>
        <form
          onSubmit={(event) => {
            event.preventDefault();
            onAction("/api/ca/issue", { cert, key });
          }}
        >
          {fields()}
          <button class={s.button} disabled={busy}>
            {text("issue")}
          </button>
        </form>
      </section>
    </>
  );
}

export function LoggingCard({ settings }: { settings: Settings }) {
  return (
    <section class={s.card}>
      <h3>{text("fileLogging")}</h3>
      <div>
        {text("fileLogging")}: {settings.fileLoggerEnabled ? text("on") : text("off")}
      </div>
      <div>
        {text("discordLogging")}: {settings.discordLoggerEnabled ? text("on") : text("off")}
      </div>
      <p class={s.muted}>{text("loggerHint")}</p>
    </section>
  );
}
