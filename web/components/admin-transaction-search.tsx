"use client";

import { FormEvent, useState } from "react";
import { getAdminAccountHistory } from "../lib/api";
import type { AdminAccountHistory } from "../lib/types";
import { EventLog } from "./event-log";
import { formatMoney } from "../lib/format";

export function AdminTransactionSearch() {
  const [accountId, setAccountId] = useState("");
  const [history, setHistory] = useState<AdminAccountHistory | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  async function submit(event: FormEvent) { event.preventDefault(); setLoading(true); setError(null); setHistory(null); try { setHistory(await getAdminAccountHistory(accountId.trim())); } catch (err) { setError(err instanceof Error ? err.message : "Could not load transaction history"); } finally { setLoading(false); } }
  return <section className="mt-8 rounded-xl border border-border bg-surface p-5"><div><p className="text-xs font-semibold uppercase tracking-wide text-brand">Account operations</p><h2 className="mt-1 text-xl font-semibold text-text">Query transaction history</h2><p className="mt-1 text-sm text-text-2">Enter any account ID to inspect its balance and event timeline.</p></div><form onSubmit={submit} className="mt-4 flex flex-col gap-3 sm:flex-row"><input value={accountId} onChange={(event) => setAccountId(event.target.value)} required placeholder="acc_... or acc1" className="min-w-0 flex-1 rounded-lg border border-border-strong bg-surface px-3 py-2 text-sm"/><button disabled={loading || !accountId.trim()} className="rounded-lg bg-brand px-5 py-2 text-sm font-semibold text-white disabled:opacity-40">{loading ? "Searching…" : "Search"}</button></form>{error && <p className="mt-4 rounded-lg bg-negative-soft p-3 text-sm text-negative">{error}</p>}{history && <div className="mt-5"><div className="mb-4 flex flex-wrap gap-5 rounded-lg bg-surface-2 p-4 text-sm"><div><p className="text-text-muted">Account</p><p className="font-mono text-text">{history.aggregate_id}</p></div><div><p className="text-text-muted">Balance</p><p className="font-mono text-text">{formatMoney(history.balance)}</p></div><div><p className="text-text-muted">Events</p><p className="font-mono text-text">{history.event_count}</p></div></div><EventLog log={history}/></div>}</section>;
}
