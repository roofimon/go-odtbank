import type { EventLogResponse } from "../lib/types";
import { formatDateTime, formatMoney } from "../lib/format";

type Props = { log: EventLogResponse };

const EVENT_BADGE: Record<string, string> = {
  AccountOpened: "bg-positive-soft text-positive",
  MoneyDebited: "bg-negative-soft text-negative",
  MoneyCredited: "bg-brand-soft text-brand",
};

export function EventLog({ log }: Props) {
  if (log.events.length === 0) {
    return (
      <div className="rounded-lg border border-dashed border-border-strong p-8 text-center text-sm text-text-2">
        No events for this aggregate yet.
      </div>
    );
  }

  return (
    <div className="overflow-x-auto">
      <table className="w-full border-collapse text-sm">
        <thead>
          <tr className="text-left text-xs text-text-muted">
            <th className="py-2 pr-3 font-medium">Seq</th>
            <th className="py-2 pr-3 font-medium">Type</th>
            <th className="py-2 pr-3 text-right font-medium">Amount</th>
            <th className="py-2 font-medium">Occurred</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-border">
          {[...log.events].reverse().map((e) => (
            <tr key={e.seq} className="align-top">
              <td className="py-2.5 pr-3 font-mono text-xs text-text-muted">
                {e.seq}
              </td>
              <td className="py-2.5 pr-3">
                <span
                  className={
                    "inline-flex rounded-md px-2 py-0.5 text-xs font-medium " +
                    (EVENT_BADGE[e.type] ?? "bg-surface-2 text-text-2")
                  }
                >
                  {e.type}
                </span>
              </td>
              <td className="py-2.5 pr-3 text-right font-mono tabular-nums text-text">
                {e.amount !== undefined ? formatMoney(e.amount) : "—"}
              </td>
              <td className="py-2.5 text-xs text-text-2">
                {formatDateTime(e.occurred_at)}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
