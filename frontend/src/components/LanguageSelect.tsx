import { useState } from "preact/hooks";
import { api } from "../core/api";
import type { Locale } from "../core/api";
import { locale, text } from "../core/i18n";
import { s } from "../styles";

export function LanguageSelect({ csrf, compact = false }: { csrf: string; compact?: boolean }) {
  const [error, setError] = useState("");
  async function change(next: Locale) {
    try {
      await api("/api/language", { method: "POST", body: JSON.stringify({ lang: next }) }, csrf);
      locale.value = next;
      document.documentElement.lang = next;
      setError("");
    } catch (reason) {
      setError(String(reason));
    }
  }
  return (
    <div class={`${s.formRow} ${compact ? s.languageCompact : s.languageRow}`}>
      <label class={`${s.label} ${s.languageLabel}`} for="language">
        {text("language")}
      </label>
      <select
        class={s.input}
        id="language"
        value={locale.value}
        onChange={(e) => void change(e.currentTarget.value as Locale)}
      >
        <option value="zh-CN">中文</option>
        <option value="en">English</option>
      </select>
      {error && <span class={s.badge}>{error}</span>}
    </div>
  );
}
