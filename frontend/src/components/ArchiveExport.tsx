import { useEffect, useRef, useState } from "preact/hooks";
import { text } from "../core/i18n";
import { params } from "../core/api";
import { localTimeBound } from "../core/time";
import { Dialog } from "../primitives/Dialog";
import { Menu } from "../primitives/Menu";
import { s } from "../styles";

export type ExportTarget = {
  includeThreads?: boolean;
  thread?: string;
  guild?: string;
  scope?: string;
  name: string;
};
export function openExport(target: ExportTarget) {
  document.dispatchEvent(new CustomEvent("diskord-export", { detail: target }));
}

export function ArchiveExport() {
  const [menu, setMenu] = useState<{ x: number; y: number; target: ExportTarget } | null>(null);
  const [target, setTarget] = useState<ExportTarget | null>(null);
  const [scope, setScope] = useState("current");
  const [since, setSince] = useState("");
  const [until, setUntil] = useState("");
  const [format, setFormat] = useState("md");
  const [includeThreads, setIncludeThreads] = useState(false);
  const [attachments, setAttachments] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const controller = useRef<AbortController | null>(null);
  const trigger = useRef<HTMLElement | null>(null);
  function show(next: ExportTarget) {
    controller.current?.abort();
    controller.current = null;
    setBusy(false);
    setMenu(null);
    setScope("current");
    setSince("");
    setUntil("");
    setError("");
    setIncludeThreads(!!next.includeThreads);
    setTarget(next);
  }
  function close() {
    controller.current?.abort();
    controller.current = null;
    setBusy(false);
    setTarget(null);
    trigger.current?.focus();
  }
  useEffect(() => {
    const contextMenu = (event: MouseEvent | KeyboardEvent) => {
      if (!(event.target instanceof Element)) return;
      if (event.target.closest("a,input,textarea,select")) return;
      const element = event.target.closest<HTMLElement>("[data-export]");
      if (!element?.closest("[data-export-root]")) return;
      event.preventDefault();
      trigger.current = element;
      element.focus();
      const bounds = element.getBoundingClientRect();
      setMenu({
        x: event instanceof MouseEvent ? event.clientX : bounds.left + 12,
        y: event instanceof MouseEvent ? event.clientY : bounds.top + 12,
        target: {
          thread: element.dataset.exportThread,
          includeThreads: element.dataset.exportIncludeThreads === "true",
          guild: element.dataset.exportGuild,
          scope: element.dataset.exportScope,
          name: element.dataset.exportName || text("all"),
        },
      });
    };
    const keyboard = (event: KeyboardEvent) => {
      if (event.key === "ContextMenu" || (event.shiftKey && event.key === "F10"))
        contextMenu(event);
    };
    const requested = (event: Event) => {
      trigger.current = document.activeElement as HTMLElement;
      show((event as CustomEvent<ExportTarget>).detail);
    };
    document.addEventListener("contextmenu", contextMenu);
    document.addEventListener("keydown", keyboard);
    document.addEventListener("diskord-export", requested);
    return () => {
      document.removeEventListener("contextmenu", contextMenu);
      document.removeEventListener("keydown", keyboard);
      document.removeEventListener("diskord-export", requested);
      controller.current?.abort();
    };
  }, []);
  async function download(event: SubmitEvent) {
    event.preventDefault();
    if (!target || busy) return;
    const fields = new FormData(event.currentTarget as HTMLFormElement);
    const selected: ExportTarget =
      fields.get("scope") === "current" ? target : { name: text("all") };
    const from = String(fields.get("since") || "");
    const to = String(fields.get("until") || "");
    const selectedFormat = String(fields.get("format") || "md");
    const selectedAttachments = fields.has("attachments");
    const bounds: Record<string, string> = {};
    try {
      if (from) bounds.since = localTimeBound(from);
      if (to) bounds.until = localTimeBound(to);
      if (from && to && bounds.since! >= bounds.until!) throw new Error();
    } catch {
      setError(text("exportInvalidTime"));
      return;
    }
    const abort = new AbortController();
    controller.current = abort;
    setBusy(true);
    setError("");
    let url = "";
    try {
      const response = await fetch(
        `/api/export?${params({
          thread: selected.thread || "",
          include_threads: String(!!selected.thread && fields.has("include_threads")),
          guild: selected.guild || "",
          scope: selected.scope || "",
          ...bounds,
          format: selectedFormat,
          attachments: String(selectedAttachments),
        })}`,
        { credentials: "same-origin", cache: "no-store", signal: abort.signal },
      );
      if (!response.ok) {
        const body = (await response.json()) as { error?: string };
        throw new Error(body.error || `HTTP ${response.status}`);
      }
      const blob = await response.blob();
      if (abort.signal.aborted) return;
      url = URL.createObjectURL(blob);
      const anchor = document.createElement("a");
      anchor.href = url;
      anchor.download = selectedAttachments ? "archive.zip" : `messages.${selectedFormat}`;
      document.body.append(anchor);
      anchor.click();
      anchor.remove();
      // Keep the URL alive while the browser takes ownership of the download.
      const downloadURL = url;
      setTimeout(() => URL.revokeObjectURL(downloadURL), 60000);
      url = "";
      close();
    } catch (reason) {
      if (!abort.signal.aborted)
        setError(reason instanceof Error ? reason.message : String(reason));
    } finally {
      if (url) URL.revokeObjectURL(url);
      if (controller.current === abort) {
        controller.current = null;
        setBusy(false);
      }
    }
  }
  return (
    <>
      {menu && (
        <Menu
          x={menu.x}
          y={menu.y}
          label={text("exportMessages")}
          onClose={() => setMenu(null)}
          items={[{ label: text("exportMessages"), onSelect: () => show(menu.target) }]}
        />
      )}
      <Dialog
        open={!!target}
        title={text("exportTitle")}
        description={text("exportHint")}
        onClose={close}
      >
        <form onSubmit={(event) => void download(event)}>
          <div class={s.field}>
            <label class={s.label} for="export-scope">
              {text("exportScope")}
            </label>
            <select
              id="export-scope"
              name="scope"
              class={`${s.input} ${s.fullInput}`}
              value={scope}
              disabled={busy}
              onChange={(event) => setScope(event.currentTarget.value)}
            >
              <option value="current">
                {text("exportCurrent")} · {target?.name}
              </option>
              <option value="all">{text("exportAll")}</option>
            </select>
          </div>
          <div class={s.exportDates}>
            <div class={s.field}>
              <label class={s.label} for="export-since">
                {text("exportSince")}
              </label>
              <input
                id="export-since"
                name="since"
                class={`${s.input} ${s.fullInput}`}
                type="datetime-local"
                step="1"
                value={since}
                disabled={busy}
                onInput={(event) => setSince(event.currentTarget.value)}
              />
            </div>
            <div class={s.field}>
              <label class={s.label} for="export-until">
                {text("exportUntil")}
              </label>
              <input
                id="export-until"
                name="until"
                class={`${s.input} ${s.fullInput}`}
                type="datetime-local"
                step="1"
                value={until}
                disabled={busy}
                onInput={(event) => setUntil(event.currentTarget.value)}
              />
            </div>
          </div>
          <p class={s.muted}>{text("exportTimeHint")}</p>
          <div class={s.field}>
            <label class={s.label} for="export-format">
              {text("exportFormat")}
            </label>
            <select
              id="export-format"
              name="format"
              class={`${s.input} ${s.fullInput}`}
              value={format}
              disabled={busy}
              onChange={(event) => setFormat(event.currentTarget.value)}
            >
              <option value="md">Markdown</option>
              <option value="jsonl">JSONL</option>
            </select>
          </div>
          {scope === "current" && target?.thread && (
            <label class={s.switch}>
              <input
                type="checkbox"
                name="include_threads"
                checked={includeThreads}
                disabled={busy}
                onChange={(event) => setIncludeThreads(event.currentTarget.checked)}
              />
              {text("exportChildren")}
            </label>
          )}
          <label class={s.switch}>
            <input
              type="checkbox"
              name="attachments"
              checked={attachments}
              disabled={busy}
              onChange={(event) => setAttachments(event.currentTarget.checked)}
            />
            {text("exportAttachments")}
          </label>
          {error && (
            <p class={`${s.notice} ${s.error}`} role="alert">
              {error}
            </p>
          )}
          <div class={s.dialogActions}>
            <button type="button" class={`${s.button} ${s.quietButton}`} onClick={close}>
              {text("cancel")}
            </button>
            <button class={s.button} disabled={busy}>
              {busy ? text("exportPreparing") : text("exportDownload")}
            </button>
          </div>
        </form>
      </Dialog>
    </>
  );
}
