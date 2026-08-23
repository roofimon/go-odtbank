import type { Metadata } from "next";
import { getAccounts, getEvents } from "../lib/api";
import Dashboard from "./dashboard";
import type { Account, EventLogResponse } from "../lib/types";

export const metadata: Metadata = {
  title: "ODT Bank",
  description: "Event-sourced money transfer service dashboard",
};

export const dynamic = "force-dynamic";

export default async function HomePage() {
  let accounts: Account[] = [];
  let backendError: string | null = null;

  try {
    accounts = await getAccounts();
  } catch (err) {
    backendError = err instanceof Error ? err.message : "Backend unavailable";
  }

  const firstId = accounts[0]?.id ?? null;
  let initialLog: EventLogResponse | null = null;
  if (firstId) {
    try {
      initialLog = await getEvents(firstId);
    } catch {
      initialLog = { aggregate_id: firstId, events: [] };
    }
  }

  return (
    <Dashboard
      initialAccounts={accounts}
      initialLog={initialLog}
      initialError={backendError}
    />
  );
}
