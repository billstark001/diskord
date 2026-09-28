import { createGlobalTheme, globalStyle } from "@vanilla-extract/css";

export const vars = createGlobalTheme(":root", {
  color: {
    bg: "#181a20",
    rail: "#111318",
    sidebar: "#20232b",
    surface: "#292d37",
    raised: "#323744",
    line: "#414653",
    ink: "#f1f2f5",
    muted: "#a6acb9",
    accent: "#8ea6ff",
    accentStrong: "#617eea",
    danger: "#f19a9a",
    success: "#7bd5a7",
  },
});
globalStyle("*", { boxSizing: "border-box" });
globalStyle("html, body, #app", { margin: 0, width: "100%", height: "100%" });
globalStyle("body", {
  background: vars.color.bg,
  color: vars.color.ink,
  fontFamily:
    'Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif',
  fontSize: 14,
});
globalStyle("button, input, select", { font: "inherit" });
globalStyle("button", { cursor: "pointer" });
globalStyle("a", { color: "inherit" });
globalStyle(":focus-visible", { outline: `2px solid ${vars.color.accent}`, outlineOffset: 2 });
