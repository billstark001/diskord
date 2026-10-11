// The server supplies the selected scope and dates. Respect UTF-8 filenames
// from RFC 5987 rather than overriding them with a generic browser filename.
export function exportFilename(disposition: string | null, fallback: string): string {
  const safe = (name: string) =>
    name && !/[\x00-\x1f\x7f\\/\u0085\u2028\u2029]/.test(name) && name !== "." && name !== ".."
      ? name
      : fallback;
  const encoded = disposition?.match(/(?:^|;)\s*filename\*\s*=\s*UTF-8'[^']*'([^;\s]+)/i);
  if (encoded) {
    try {
      return safe(decodeURIComponent(encoded[1]));
    } catch {
      // Try the optional ASCII fallback if the extended value is malformed.
    }
  }
  const plain = disposition?.match(/(?:^|;)\s*filename\s*=\s*(?:"((?:\\.|[^"\\])*)"|([^;\s]+))/i);
  return plain
    ? safe(plain[1] !== undefined ? plain[1].replace(/\\(.)/g, "$1") : plain[2])
    : fallback;
}
