"use client";

import { FormEvent, useRef, useState } from "react";
import type { Account, TransferReceipt } from "../lib/types";
import { transfer } from "../lib/api";
import { formatMoney } from "../lib/format";

type Status =
  | { kind: "idle" }
  | { kind: "loading" }
  | { kind: "error"; message: string }
  | { kind: "success"; receipt: TransferReceipt };

type Props = {
  accounts: Account[];
  onCompleted: () => void;
};

const inputClass =
  "w-full rounded-lg border border-border-strong bg-surface px-3 py-2 text-sm text-text transition-colors placeholder:text-text-muted focus:border-brand focus:outline-none focus:ring-2 focus:ring-brand/20";

export function TransferForm({ accounts, onCompleted }: Props) {
  const sourceId = accounts[0]?.id ?? "";
  const [destId, setDestId] = useState("");
  const [amount, setAmount] = useState("");
  const [status, setStatus] = useState<Status>({ kind: "idle" });
  const idempotencyKey = useRef("");

  function detailsChanged() { idempotencyKey.current = ""; setStatus({ kind: "idle" }); }

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    const amt = Number(amount);
    if (!Number.isFinite(amt)) {
      setStatus({ kind: "error", message: "Amount must be a number." });
      return;
    }
    setStatus({ kind: "loading" });
    try {
      if (!idempotencyKey.current) idempotencyKey.current = crypto.randomUUID();
      const receipt = await transfer(amt, sourceId, destId, idempotencyKey.current);
      setStatus({ kind: "success", receipt });
      onCompleted();
    } catch (err) {
      setStatus({
        kind: "error",
        message: err instanceof Error ? err.message : "Transfer failed",
      });
    }
  }

  const r = status.kind === "success" ? status.receipt : null;

  return (
    <form
      onSubmit={handleSubmit}
      className="rounded-xl border border-border bg-surface p-4 sm:p-5"
    >
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-3">
        <label className="space-y-1.5">
          <span className="text-xs font-medium text-text-2">From</span>
          <input
            value={sourceId}
            readOnly
            className={inputClass}
          />
        </label>

        <label className="space-y-1.5">
          <span className="text-xs font-medium text-text-2">To</span>
          <input
            value={destId}
            onChange={(e) => { setDestId(e.target.value); detailsChanged(); }}
            placeholder="Destination account ID"
            required
            className={inputClass}
          />
        </label>

        <label className="space-y-1.5">
          <span className="text-xs font-medium text-text-2">Amount</span>
          <input
            type="number"
            step="0.01"
            min="1"
            required
            value={amount}
            onChange={(e) => { setAmount(e.target.value); detailsChanged(); }}
            placeholder="10.00"
            className={inputClass}
          />
        </label>
      </div>

      <div className="mt-4 flex items-center gap-3">
        <button
          type="submit"
          disabled={
            status.kind === "loading" ||
            accounts.length < 1 ||
            sourceId === destId
          }
          className="rounded-lg bg-brand px-5 py-2 text-sm font-semibold text-white transition-colors hover:bg-brand-hover disabled:cursor-not-allowed disabled:opacity-40"
        >
          {status.kind === "loading" ? "Transferring…" : "Transfer"}
        </button>
        {sourceId === destId && (
          <span className="text-xs font-medium text-warning">
            Source and destination must differ.
          </span>
        )}
      </div>

      {status.kind === "error" && (
        <p className="mt-4 rounded-lg border border-negative/30 bg-negative-soft px-3 py-2 text-sm text-negative">
          {status.message}
        </p>
      )}

      {r && (
        <div className="mt-4 grid grid-cols-2 gap-4 rounded-lg bg-surface-2 p-4 text-sm sm:grid-cols-4">
          <Result
            label="Source"
            from={r.InitialSourceAccount}
            to={r.FinalSourceAccount}
          />
          <div><div className="text-xs text-text-muted">Destination</div><div className="break-all font-mono text-text">{r.DestinationAccountID}</div></div>
          <Result
            label="Transferred"
            from={{ ID: "", Balance: 0 }}
            to={{ ID: "", Balance: r.TransferAmount }}
          />
          <Result
            label="Fee"
            from={{ ID: "", Balance: 0 }}
            to={{ ID: "", Balance: r.FeeAmount }}
          />
        </div>
      )}
    </form>
  );
}

function Result({
  label,
  from,
  to,
}: {
  label: string;
  from?: { ID: string; Balance: number };
  to: { ID: string; Balance: number };
}) {
  return (
    <div>
      <div className="text-xs text-text-muted">{label}</div>
      {from && from.Balance !== to.Balance ? (
        <div className="font-mono tabular-nums text-text">
          <span className="text-text-muted line-through">
            {formatMoney(from.Balance)}
          </span>{" "}
          {formatMoney(to.Balance)}
        </div>
      ) : (
        <div className="font-mono tabular-nums text-text">
          {formatMoney(to.Balance)}
        </div>
      )}
    </div>
  );
}
