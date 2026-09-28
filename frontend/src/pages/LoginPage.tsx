import { useState } from "preact/hooks";
import { api } from "../core/api";
import { text } from "../core/i18n";
import { LanguageSelect } from "../components/LanguageSelect";
import { s } from "../styles";

export function LoginPage({ csrf, onSuccess }: { csrf: string; onSuccess: () => void }) {
  const [token, setToken] = useState("");
  const [problem, setProblem] = useState("");
  const [busy, setBusy] = useState(false);
  async function submit(event: Event) {
    event.preventDefault();
    setBusy(true);
    setProblem("");
    try {
      await api("/api/login", { method: "POST", body: JSON.stringify({ token }) }, csrf);
      setToken("");
      onSuccess();
    } catch (error) {
      setProblem(String(error));
    } finally {
      setBusy(false);
    }
  }
  return (
    <main class={s.loginPage}>
      <div class={s.loginCard}>
        <div class={s.logo}>
          diskord<span class={s.logoDot}>.</span>
        </div>
        <div class={s.eyebrow}>LOCAL / PRIVATE / EXPLICIT</div>
        <h1 class={s.heading}>{text("loginTitle")}</h1>
        <p class={s.muted}>{text("loginHint")}</p>
        {problem && <div class={`${s.notice} ${s.error}`}>{problem}</div>}
        <form onSubmit={submit}>
          <label class={s.label} for="token">
            {text("token")}
          </label>
          <input
            class={`${s.input} ${s.fullInput}`}
            id="token"
            type="password"
            autoComplete="off"
            required
            value={token}
            onInput={(e) => setToken(e.currentTarget.value)}
          />
          <button class={`${s.button} ${s.loginSubmit}`} disabled={busy}>
            {busy ? text("loading") : text("enter")}
          </button>
        </form>
        <LanguageSelect csrf={csrf} />
      </div>
    </main>
  );
}
