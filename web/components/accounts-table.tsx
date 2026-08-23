"use client";

import type { Account } from "../lib/types";
import { formatMoney } from "../lib/format";

type Props = {
  accounts: Account[];
  selectedId: string | null;
  onSelect: (id: string) => void;
};

export function AccountsTable({ accounts, selectedId, onSelect }: Props) {
  if (accounts.length === 0) {
    return (
      <div className="rounded-xl border border-dashed border-border-strong p-8 text-center text-sm text-text-2">
        No accounts yet.
      </div>
    );
  }

  return (
    <ul className="overflow-hidden rounded-xl border border-border bg-surface">
      {accounts.map((a, i) => {
        const isSelected = a.id === selectedId;
        return (
          <li key={a.id} className={i > 0 ? "border-t border-border" : ""}>
            <button
              type="button"
              onClick={() => onSelect(a.id)}
              className={`flex w-full items-center justify-between gap-3 px-4 py-3.5 text-left transition-colors ${
                isSelected
                  ? "bg-brand-soft"
                  : "hover:bg-surface-2"
              }`}
            >
              <div className="min-w-0 flex-1">
                <div
                  className={`truncate font-mono text-sm ${
                    isSelected ? "font-medium text-brand" : "text-text"
                  }`}
                >
                  {a.id}
                </div>
                <div className="text-xs text-text-muted">
                  {a.event_count} event{a.event_count === 1 ? "" : "s"}
                </div>
              </div>
              <div className="font-mono text-base font-medium tabular-nums text-text">
                {formatMoney(a.balance)}
              </div>
            </button>
          </li>
        );
      })}
    </ul>
  );
}
