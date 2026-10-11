import { style } from "@vanilla-extract/css";
import { vars } from "./theme.css";

export const overlays = {
  dialog: style({
    width: "min(520px, calc(100vw - 32px))",
    maxHeight: "calc(100dvh - 32px)",
    overflowY: "auto",
    padding: 24,
    color: vars.color.ink,
    background: vars.color.surface,
    border: `1px solid ${vars.color.line}`,
    borderRadius: 14,
    boxShadow: "0 24px 70px #0008",
    selectors: { "&::backdrop": { background: "#080a10b8" } },
  }),
  dialogTitle: style({ margin: "0 0 12px", fontSize: 22 }),
  dialogActions: style({ display: "flex", gap: 10, justifyContent: "flex-end", marginTop: 24 }),
  exportDates: style({
    display: "grid",
    gridTemplateColumns: "repeat(auto-fit,minmax(200px,1fr))",
    gap: 12,
  }),
  contextMenu: style({
    position: "fixed",
    zIndex: 100,
    minWidth: 180,
    maxWidth: "calc(100vw - 16px)",
    padding: 6,
    border: `1px solid ${vars.color.line}`,
    borderRadius: 10,
    background: vars.color.surface,
    boxShadow: "0 8px 30px #0007",
  }),
  menuItem: style({
    display: "block",
    width: "100%",
    textAlign: "left",
    padding: "10px 12px",
    border: 0,
    borderRadius: 6,
    color: vars.color.ink,
    background: "transparent",
    ":hover": { background: vars.color.raised },
    ":focus-visible": { background: vars.color.raised },
  }),
};
