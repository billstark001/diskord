import { style } from "@vanilla-extract/css";
import { vars } from "./theme.css";

export const archive = {
  message: style({
    borderBottom: `1px solid ${vars.color.line}`,
    padding: "15px 8px",
    overflowWrap: "anywhere",
    ":hover": { background: "#20242c" },
  }),
  messageHead: style({
    display: "flex",
    alignItems: "baseline",
    gap: 10,
    flexWrap: "wrap",
    marginBottom: 5,
  }),
  messageAuthor: style({ fontWeight: 760, color: vars.color.ink }),
  messageMeta: style({ color: vars.color.muted, fontSize: 11 }),
  messageBody: style({ whiteSpace: "pre-wrap", lineHeight: 1.58, color: "#e4e6ec" }),
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
