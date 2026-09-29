import shortcodes from "emojibase-data/en/shortcodes/joypixels.json";

const aliases = new Map<string, string>();
for (const [hexcode, names] of Object.entries(shortcodes as Record<string, string | string[]>)) {
  const emoji = String.fromCodePoint(...hexcode.split("-").map((part) => parseInt(part, 16)));
  for (const name of Array.isArray(names) ? names : [names]) aliases.set(name.toLowerCase(), emoji);
}

export function emojiForShortcode(name: string): string | undefined {
  return aliases.get(name.toLowerCase());
}
