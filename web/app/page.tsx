import type { Metadata } from "next";
import { cookies } from "next/headers";
import { redirect } from "next/navigation";
import { getAccounts, getEvents, getMe } from "../lib/api";
import Dashboard from "./dashboard";
import { ApplicationStatus } from "../components/application-status";
import type { Account, EventLogResponse } from "../lib/types";

export const metadata: Metadata = {
  title: "ODT Bank",
  description: "Event-sourced money transfer service dashboard",
};

export const dynamic = "force-dynamic";

export default async function HomePage() {
  let accounts: Account[] = [];
  let backendError: string | null = null;
  const cookieHeader = (await cookies()).toString();

  let principal;
  try { principal = await getMe(cookieHeader); } catch { redirect("/login"); }
  if (principal.role === "admin") redirect("/admin");
  if (principal.kyc_status !== "approved") return <ApplicationStatus principal={principal} />;

  try {
    accounts = await getAccounts(cookieHeader);
  } catch (err) {
    if (err instanceof Error && "status" in err && err.status === 401) redirect("/login");
    backendError = err instanceof Error ? err.message : "Backend unavailable";
  }

  const firstId = accounts[0]?.id ?? null;
  let initialLog: EventLogResponse | null = null;
  if (firstId) {
    try {
      initialLog = await getEvents(firstId, cookieHeader);
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
