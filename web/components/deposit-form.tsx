"use client";

import { FormEvent, useState } from "react";
import { deposit } from "../lib/api";
import { formatMoney } from "../lib/format";
import type { Account, DepositReceipt } from "../lib/types";

type Status =
  | { kind: "idle" }
  | { kind: "loading" }
  | { kind: "error"; message: string }
  | { kind: "success"; receipt: DepositReceipt };

type Props = {
  accounts: Account[];
  onCompleted: (accountId: string) => void;
};

const inputClass =
  "w-full rounded-lg border border-border-strong bg-surface px-3 py-2 text-sm text-text transition-colors placeholder:text-text-muted focus:border-brand focus:outline-none focus:ring-2 focus:ring-brand/20";

export function DepositForm({ accounts, onCompleted }: Props) {
  const [accountId, setAccountId] = useState(accounts[0]?.id ?? "");
  const [amount, setAmount] = useState("");
  const [status, setStatus] = useState<Status>({ kind: "idle" });

  const selectedAccountId = accounts.some(
    (account) => account.id === accountId,
  )
    ? accountId
    : (accounts[0]?.id ?? "");

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    const parsedAmount = Number(amount);
    if (!Number.isFinite(parsedAmount) || parsedAmount < 10) {
      setStatus({
        kind: "error",
        message: "Deposit amount must be at least 10.00.",
      });
      return;
    }

    setStatus({ kind: "loading" });
    try {
      const receipt = await deposit(parsedAmount, selectedAccountId);
      setStatus({ kind: "success", receipt });
      setAmount("");
      onCompleted(selectedAccountId);
    } catch (err) {
      setStatus({
        kind: "error",
        message: err instanceof Error ? err.message : "Deposit failed",
      });
    }
  }

  const receipt = status.kind === "success" ? status.receipt : null;

  return (
    <form
      onSubmit={handleSubmit}
      className="h-full rounded-xl border border-border bg-surface p-4 sm:p-5"
    >
      <h2 className="mb-4 text-sm font-semibold text-text">Deposit</h2>
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
        <label className="space-y-1.5">
          <span className="text-xs font-medium text-text-2">Account</span>
          <select
            value={selectedAccountId}
            onChange={(e) => setAccountId(e.target.value)}
            className={inputClass}
          >
            {accounts.map((account) => (
              <option key={account.id} value={account.id}>
                {account.id} ({formatMoney(account.balance)})
              </option>
            ))}
          </select>
        </label>
        <label className="space-y-1.5">
          <span className="text-xs font-medium text-text-2">Amount</span>
          <input
            type="number"
            step="0.01"
            min="10"
            required
            value={amount}
            onChange={(e) => setAmount(e.target.value)}
            placeholder="10.00"
            className={inputClass}
          />
        </label>
      </div>

      <button
        type="submit"
        disabled={status.kind === "loading" || !selectedAccountId}
        className="mt-4 rounded-lg bg-brand px-5 py-2 text-sm font-semibold text-white transition-colors hover:bg-brand-hover disabled:cursor-not-allowed disabled:opacity-40"
      >
        {status.kind === "loading" ? "Depositing…" : "Deposit"}
      </button>

      {status.kind === "error" && (
        <p className="mt-4 rounded-lg border border-negative/30 bg-negative-soft px-3 py-2 text-sm text-negative">
          {status.message}
        </p>
      )}
      {receipt && (
        <div className="mt-4 rounded-lg bg-surface-2 p-4 text-sm">
          <div className="text-xs text-text-muted">Balance</div>
          <div className="font-mono tabular-nums text-text">
            <span className="text-text-muted line-through">
              {formatMoney(receipt.InitialAccount.Balance)}
            </span>{" "}
            {formatMoney(receipt.FinalAccount.Balance)}
          </div>
        </div>
      )}
    </form>
  );
}
