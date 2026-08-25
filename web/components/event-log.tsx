import type { EventLogResponse, StoredEvent } from "../lib/types";
import { formatDateTime, formatMoney } from "../lib/format";

const EVENT_BADGE: Record<string, string> = { AccountOpened: "bg-positive-soft text-positive", MoneyDebited: "bg-negative-soft text-negative", MoneyCredited: "bg-brand-soft text-brand" };

export function EventLog({ log }: { log: EventLogResponse }) {
  if (!log.events.length) return <div className="rounded-lg border border-dashed border-border-strong p-8 text-center text-sm text-text-2">No events for this aggregate yet.</div>;
  const groups: StoredEvent[][] = [];
  for (const event of [...log.events].reverse()) {
    const previous = groups.at(-1);
    if (event.transfer_id && previous?.[0].transfer_id === event.transfer_id) previous.push(event); else groups.push([event]);
  }
  return <div className="overflow-x-auto"><table className="w-full border-collapse text-sm"><thead><tr className="text-left text-xs text-text-muted"><th className="py-2 pr-3 font-medium">Transaction</th><th className="py-2 pr-3 font-medium">Type</th><th className="py-2 pr-3 text-right font-medium">Amount</th><th className="py-2 font-medium">Occurred</th></tr></thead>{groups.map((group) => <tbody key={group[0].transfer_id ?? `event-${group[0].seq}`} className="border-t border-border">{group.map((event, index) => <tr key={event.seq} className="align-top">{index === 0 && <td rowSpan={group.length} className="py-2.5 pr-3"><p className="font-mono text-xs text-text">{event.transfer_id ?? `Event #${event.seq}`}</p>{event.counterparty_account_id && <p className="mt-1 text-xs text-text-muted">Counterparty: {event.counterparty_account_id}</p>}</td>}<td className="py-2.5 pr-3"><span className={`inline-flex rounded-md px-2 py-0.5 text-xs font-medium ${EVENT_BADGE[event.type] ?? "bg-surface-2 text-text-2"}`}>{event.purpose === "fee" ? "Transfer fee" : event.type}</span></td><td className="py-2.5 pr-3 text-right font-mono tabular-nums text-text">{event.amount !== undefined ? formatMoney(event.amount) : "—"}</td><td className="py-2.5 text-xs text-text-2">{formatDateTime(event.occurred_at)}</td></tr>)}</tbody>)}</table></div>;
}
