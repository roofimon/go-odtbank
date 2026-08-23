// "en-US" is pinned (not the default locale) so server (Node) and client
// (browser) produce identical strings — the default locale differs between
// the two and breaks React hydration (e.g. "1,234.56" vs "1.234,56").
const LOCALE = "en-US";

export function formatMoney(n: number): string {
  return n.toLocaleString(LOCALE, {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  });
}

export function formatDateTime(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return d.toLocaleString(LOCALE);
}
