export function formatLocalTime(value: string | Date): string {
  if (!value) return "";
  const date = value instanceof Date ? value : new Date(value);
  if (Number.isNaN(date.getTime())) return typeof value === "string" ? value : "";
  const pad = (part: number) => String(part).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`;
}

// Date silently normalizes DST gaps and invalid calendar dates. Export bounds
// must preserve the wall-clock time the user entered before converting to UTC.
export function localTimeBound(value: string): string {
  if (!value) return "";
  const parts = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})(?::(\d{2})(?:\.(\d{1,3}))?)?$/.exec(
    value,
  );
  if (!parts) throw new Error("Invalid local time");
  const date = new Date(value);
  const expected = [
    ...parts.slice(1, 6).map(Number),
    Number(parts[6] || "0"),
    Number((parts[7] || "").padEnd(3, "0")),
  ];
  const actual = [
    date.getFullYear(),
    date.getMonth() + 1,
    date.getDate(),
    date.getHours(),
    date.getMinutes(),
    date.getSeconds(),
    date.getMilliseconds(),
  ];
  if (actual.some((part, index) => part !== expected[index]))
    throw new Error("Invalid or nonexistent local time");
  return date.toISOString();
}

export function snowflakeTime(id: string): Date | null {
  try {
    if (!/^\d+$/.test(id)) return null;
    return new Date(Number((BigInt(id) >> 22n) + 1420070400000n));
  } catch {
    return null;
  }
}
