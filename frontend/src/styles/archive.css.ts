import { globalStyle, style } from "@vanilla-extract/css";
import { vars } from "./theme.css";

export const archive = {
  message: style({
    position: "relative",
    borderBottom: `1px solid ${vars.color.line}`,
    padding: "15px 84px 15px 8px",
    display: "flex",
    flexShrink: 0,
    alignItems: "flex-start",
    gap: 12,
    overflowWrap: "anywhere",
    ":hover": { background: "#20242c" },
  }),
  messageFocused: style({ background: "#293450", borderLeft: `3px solid ${vars.color.accent}` }),
  messageJumpable: style({ cursor: "pointer" }),
  messageActions: style({
    position: "absolute",
    top: 5,
    right: 7,
    display: "flex",
    alignItems: "center",
    gap: 3,
    opacity: 0,
    pointerEvents: "none",
    background: vars.color.surface,
    border: `1px solid ${vars.color.line}`,
    borderRadius: 7,
    padding: 2,
    zIndex: 1,
  }),
  messageAction: style({
    display: "grid",
    placeItems: "center",
    width: 27,
    height: 26,
    border: 0,
    borderRadius: 5,
    background: "transparent",
    color: vars.color.ink,
    ":hover": { background: vars.color.raised },
    ":disabled": { opacity: 0.45, cursor: "not-allowed" },
  }),
  copyStatus: style({
    position: "absolute",
    right: 0,
    bottom: "calc(100% + 4px)",
    whiteSpace: "nowrap",
    color: vars.color.ink,
    background: vars.color.surface,
    borderRadius: 4,
    fontSize: 11,
    padding: "2px 5px",
    ":empty": { display: "none" },
  }),
  messageContent: style({ flex: 1, minWidth: 0 }),
  avatar: style({ width: 38, height: 38, flexShrink: 0, borderRadius: "50%", objectFit: "cover" }),
  avatarFallback: style({
    width: 38,
    height: 38,
    flexShrink: 0,
    borderRadius: "50%",
    background: vars.color.raised,
    display: "grid",
    placeItems: "center",
    fontWeight: 700,
  }),
  reactions: style({ display: "flex", flexWrap: "wrap", gap: 6, marginTop: 10 }),
  reaction: style({
    border: `1px solid ${vars.color.line}`,
    background: vars.color.raised,
    borderRadius: 8,
    padding: "4px 8px",
    fontSize: 12,
  }),
  reactionEmoji: style({ width: 18, height: 18, verticalAlign: "middle", objectFit: "contain" }),
  messageHead: style({
    display: "flex",
    alignItems: "baseline",
    gap: 10,
    flexWrap: "wrap",
    marginBottom: 5,
  }),
  messageAuthor: style({ fontWeight: 760, color: vars.color.ink }),
  messageMeta: style({ color: vars.color.muted, fontSize: 11 }),
  messageBody: style({ lineHeight: 1.58, color: "#e4e6ec", minWidth: 0, overflowWrap: "anywhere" }),
  richContent: style({ minWidth: 0 }),
  mention: style({ color: "#b9c9ff", background: "#343f63", borderRadius: 4, padding: "1px 3px" }),
  inlineEmoji: style({ width: 24, height: 24, objectFit: "contain", verticalAlign: "middle" }),
  emojiFallback: style({ color: vars.color.muted }),
  archivePage: style({
    display: "flex",
    flexDirection: "column",
    flex: 1,
    minHeight: 0,
    width: "100%",
  }),
  messageViewport: style({
    flex: 1,
    minHeight: 0,
    overflowY: "auto",
    overflowX: "hidden",
    scrollbarGutter: "stable",
  }),
  messageStack: style({
    minHeight: "100%",
    display: "flex",
    flexDirection: "column",
    justifyContent: "flex-end",
    width: "min(100%, 940px)",
    margin: "0 auto",
    padding: "12px 16px 20px",
  }),
  archiveSearch: style({
    position: "sticky",
    bottom: 0,
    zIndex: 2,
    flexShrink: 0,
    display: "flex",
    gap: 8,
    alignItems: "center",
    padding: "12px clamp(16px, 4vw, 48px)",
    margin: 0,
    borderTop: `1px solid ${vars.color.line}`,
    background: vars.color.bg,
    "@media": { "(max-width: 760px)": { flexWrap: "wrap", padding: "10px 12px" } },
  }),
  asset: style({
    display: "block",
    maxWidth: "min(100%, 420px)",
    maxHeight: 300,
    objectFit: "contain",
    borderRadius: 8,
    marginTop: 10,
  }),
  badge: style({
    display: "inline-block",
    borderRadius: 5,
    padding: "2px 7px",
    background: "#614449",
    color: vars.color.danger,
    fontSize: 11,
    marginTop: 7,
  }),
  stats: style({
    display: "grid",
    gridTemplateColumns: "repeat(auto-fit,minmax(155px,1fr))",
    gap: 12,
    margin: "24px 0",
  }),
  statValue: style({ display: "block", fontSize: 30, fontWeight: 780, marginTop: 5 }),
};

globalStyle(
  `${archive.message}:hover ${archive.messageActions}, ${archive.message}:focus-within ${archive.messageActions}`,
  {
    opacity: 1,
    pointerEvents: "auto",
  },
);
globalStyle(`${archive.messageActions}:focus-within`, { opacity: 1, pointerEvents: "auto" });
globalStyle(`${archive.messageActions}`, {
  "@media": { "(hover: none)": { opacity: 1, pointerEvents: "auto" } },
});

globalStyle(`${archive.richContent} p`, { margin: "0 0 6px" });
globalStyle(`${archive.richContent} p:last-child`, { marginBottom: 0 });
globalStyle(`${archive.richContent} pre`, {
  overflowX: "auto",
  padding: 12,
  borderRadius: 8,
  background: vars.color.rail,
});
globalStyle(`${archive.richContent} code`, {
  fontFamily: "ui-monospace, SFMono-Regular, monospace",
  background: vars.color.rail,
  borderRadius: 4,
  padding: "1px 3px",
});
globalStyle(`${archive.richContent} pre code`, { background: "transparent", padding: 0 });
globalStyle(`${archive.richContent} blockquote`, {
  margin: "7px 0",
  paddingLeft: 12,
  borderLeft: `3px solid ${vars.color.line}`,
});
globalStyle(`${archive.richContent} ul, ${archive.richContent} ol`, {
  margin: "6px 0",
  paddingLeft: 22,
});
globalStyle(`${archive.richContent} a`, { color: vars.color.accent, textDecoration: "underline" });
