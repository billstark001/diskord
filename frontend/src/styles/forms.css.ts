import { style } from "@vanilla-extract/css";
import { vars } from "./theme.css";

export const forms = {
  languageRow: style({ marginTop: 20 }),
  languageCompact: style({ alignItems: "center" }),
  languageLabel: style({ margin: 0 }),
  loginSubmit: style({ width: "100%", marginTop: 16 }),
  logoDot: style({ color: "#fff" }),
  loginPage: style({
    height: "100%",
    display: "grid",
    placeItems: "center",
    padding: 20,
    background: "radial-gradient(circle at 20% 10%, #2d3758, #181a20 54%)",
  }),
  loginCard: style({
    width: "min(100%, 430px)",
    padding: "32px",
    borderRadius: 18,
    background: vars.color.surface,
    border: `1px solid ${vars.color.line}`,
    boxShadow: "0 24px 70px #0007",
  }),
  logo: style({ color: vars.color.accent, fontSize: 32, fontWeight: 850, letterSpacing: -1 }),
  settingsGrid: style({
    display: "grid",
    gridTemplateColumns: "repeat(auto-fit,minmax(280px,1fr))",
    gap: 14,
  }),
  switch: style({ display: "flex", alignItems: "center", gap: 9 }),
};
