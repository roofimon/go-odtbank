"use client";

import { useCallback, useState } from "react";
import { AccountsTable } from "../components/accounts-table";
import { EventLog } from "../components/event-log";
import { TransferForm } from "../components/transfer-form";
import { getAccounts, getEvents } from "../lib/api";
import type { Account, EventLogResponse } from "../lib/types";

type Props = {
  initialAccounts: Account[];
  initialLog: EventLogResponse | null;
  initialError: string | null;
};

export default function Dashboard({
  initialAccounts,
  initialLog,
  initialError,
}: Props) {
  const [accounts, setAccounts] = useState<Account[]>(initialAccounts);
  const [selectedId, setSelectedId] = useState<string | null>(
    initialAccounts[0]?.id ?? null,
  );
  const [log, setLog] = useState<EventLogResponse | null>(initialLog);
  const [error, setError] = useState<string | null>(initialError);
  const [refreshing, setRefreshing] = useState(false);

  const refresh = useCallback(async (keepSelection: string | null) => {
    setRefreshing(true);
    try {
      const accs = await getAccounts();
      setAccounts(accs);

      const id =
        keepSelection && accs.some((a) => a.id === keepSelection)
          ? keepSelection
          : accs[0]?.id ?? null;
      setSelectedId(id);
      setLog(id ? await getEvents(id) : null);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Refresh failed");
    } finally {
      setRefreshing(false);
    }
  }, []);

  const handleSelect = useCallback(
    async (id: string) => {
      setSelectedId(id);
      try {
        setLog(await getEvents(id));
      } catch (err) {
        setError(err instanceof Error ? err.message : "Failed to load events");
      }
    },
    [],
  );

  const isUnreachable = error?.startsWith("Cannot reach backend") ?? false;

  return (
    <main className="mx-auto w-full max-w-6xl flex-1 px-4 py-6 sm:py-8">
      <header className="mb-6 flex items-center justify-between sm:mb-8">
        <div>
          <h1 className="text-lg font-semibold tracking-tight text-text">
            ODT Bank
          </h1>
          <p className="text-xs text-text-2">
            Event-sourced transfer service — state is replayed from the
            append-only log.
          </p>
        </div>
        <button
          type="button"
          onClick={() => refresh(selectedId)}
          disabled={refreshing}
          className="rounded-lg border border-border-strong bg-surface px-3 py-1.5 text-sm font-medium text-text transition-colors hover:bg-surface-2 disabled:opacity-40"
        >
          {refreshing ? "Refreshing…" : "Refresh"}
        </button>
      </header>

      {error && (
        <div className="mb-6 rounded-lg border border-negative/30 bg-negative-soft px-4 py-3 text-sm text-negative">
          {error}
          {isUnreachable && (
            <div className="mt-1 text-xs opacity-80">
              Hint: make sure the Go backend is running (
              <code className="font-mono">go run ./cmd/server</code>) and
              reachable from this browser.
            </div>
          )}
        </div>
      )}

      <section aria-label="Transfer" className="mb-6">
        <TransferForm
          accounts={accounts}
          onCompleted={() => refresh(selectedId)}
        />
      </section>

      <section
        aria-label="Accounts and event log"
        className="grid grid-cols-1 gap-6 lg:grid-cols-5"
      >
        <div className="lg:col-span-2">
          <h2 className="mb-3 text-xs font-semibold uppercase tracking-wide text-text-2">
            Accounts
          </h2>
          <AccountsTable
            accounts={accounts}
            selectedId={selectedId}
            onSelect={handleSelect}
          />
        </div>

        <div className="lg:col-span-3">
          <h2 className="mb-3 text-xs font-semibold uppercase tracking-wide text-text-2">
            Event log {selectedId ? `— ${selectedId}` : ""}
          </h2>
          <div className="rounded-xl border border-border bg-surface p-4">
            {log ? (
              <EventLog log={log} />
            ) : (
              <div className="py-8 text-center text-sm text-text-2">
                Select an account to view its event stream.
              </div>
            )}
          </div>
        </div>
      </section>
    </main>
  );
}
