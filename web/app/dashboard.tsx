"use client";

import { useCallback, useState } from "react";
import { AccountsTable } from "../components/accounts-table";
import { EventLog } from "../components/event-log";
import { DepositForm } from "../components/deposit-form";
import {
  NavigationMenu,
  type Feature,
} from "../components/navigation-menu";
import { TransferForm } from "../components/transfer-form";
import { WithdrawForm } from "../components/withdraw-form";
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
  const [activeFeature, setActiveFeature] = useState<Feature>("transfer");

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
  const featureHeading = {
    transfer: {
      title: "Transfer funds",
      description: "Move money securely between existing accounts.",
    },
    deposit: {
      title: "Deposit funds",
      description: "Add at least 10.00 to a specific account.",
    },
    withdraw: {
      title: "Withdraw funds",
      description: "Remove at least 10.00 without exceeding the account balance.",
    },
    "transaction-history": {
      title: "Transaction history",
      description: "Select an account and inspect its complete event history.",
    },
  }[activeFeature];

  return (
    <div className="min-h-screen lg:grid lg:grid-cols-[16rem_minmax(0,1fr)]">
      <NavigationMenu
        activeFeature={activeFeature}
        onSelect={setActiveFeature}
      />

      <main className="min-w-0 px-4 py-6 sm:px-6 sm:py-8 lg:px-8">
        <div className="mx-auto w-full max-w-6xl">
          <header className="mb-6 flex items-center justify-between sm:mb-8">
            <div>
              <p className="mb-1 text-xs font-semibold uppercase tracking-wide text-brand">
                {activeFeature === "transaction-history"
                  ? "Activity"
                  : "Account operation"}
              </p>
              <h1 className="text-2xl font-semibold tracking-tight text-text">
                {featureHeading.title}
              </h1>
              <p className="mt-1 text-sm text-text-2">
                {featureHeading.description}
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

          {activeFeature === "transfer" && (
            <section aria-label="Transfer funds">
              <TransferForm
                accounts={accounts}
                onCompleted={() => refresh(selectedId)}
              />
            </section>
          )}

          {activeFeature === "deposit" && (
            <section aria-label="Deposit funds">
              <DepositForm
                accounts={accounts}
                onCompleted={(id) => refresh(id)}
              />
            </section>
          )}

          {activeFeature === "withdraw" && (
            <section aria-label="Withdraw funds">
              <WithdrawForm
                accounts={accounts}
                onCompleted={(id) => refresh(id)}
              />
            </section>
          )}

          {activeFeature === "transaction-history" && (
            <section
              aria-label="Transaction history"
              className="grid grid-cols-1 gap-6 xl:grid-cols-5"
            >
              <div className="xl:col-span-2">
                <h2 className="mb-3 text-xs font-semibold uppercase tracking-wide text-text-2">
                  Accounts
                </h2>
                <AccountsTable
                  accounts={accounts}
                  selectedId={selectedId}
                  onSelect={handleSelect}
                />
              </div>

              <div className="xl:col-span-3">
                <h2 className="mb-3 text-xs font-semibold uppercase tracking-wide text-text-2">
                  Transaction history {selectedId ? `— ${selectedId}` : ""}
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
          )}
        </div>
      </main>
    </div>
  );
}
