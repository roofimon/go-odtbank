"use client";

/* eslint-disable @next/next/no-img-element */
import { useState } from "react";
import { useRouter } from "next/navigation";
import { approveApplication, getApplication, getApplications, getPassport, logout, rejectApplication } from "../lib/api";
import type { ApplicationDetail, ApplicationSummary, KYCStatus } from "../lib/types";
import { AdminTransactionSearch } from "./admin-transaction-search";
import { AdminNavigationMenu, type AdminFeature, type AdjustmentFeature } from "./admin-navigation-menu";
import { AdminAdjustments } from "./admin-adjustments";

export function AdminDashboard({ initialItems, adminId }: { initialItems: ApplicationSummary[]; adminId: string }) {
  const router = useRouter();
  const [status, setStatus] = useState<KYCStatus>("waiting_for_approval");
  const [items, setItems] = useState(initialItems);
  const [selected, setSelected] = useState<ApplicationDetail | null>(null);
  const [image, setImage] = useState<string | null>(null);
  const [reason, setReason] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [activeFeature, setActiveFeature] = useState<AdminFeature>("approval");
  const [activeAdjustment, setActiveAdjustment] = useState<AdjustmentFeature>("create");

  async function load(nextStatus = status) {
    setError(null);
    try { setItems(await getApplications(nextStatus)); setSelected(null); if (image) URL.revokeObjectURL(image); setImage(null); }
    catch (err) { setError(err instanceof Error ? err.message : "Could not load applications"); }
  }
  async function open(id: string) {
    setError(null);
    try { const [detail, blob] = await Promise.all([getApplication(id), getPassport(id)]); setSelected(detail); if (image) URL.revokeObjectURL(image); setImage(URL.createObjectURL(blob)); }
    catch (err) { setError(err instanceof Error ? err.message : "Could not load application"); }
  }
  async function review(action: "approve" | "reject") {
    if (!selected) return;
    setBusy(true); setError(null);
    try { if (action === "approve") await approveApplication(selected.customer_id); else await rejectApplication(selected.customer_id, reason); setReason(""); await load(); }
    catch (err) { setError(err instanceof Error ? err.message : "Review failed"); }
    finally { setBusy(false); }
  }

  return <div className="min-h-screen lg:grid lg:grid-cols-[16rem_minmax(0,1fr)]">
    <AdminNavigationMenu activeFeature={activeFeature} activeStatus={status} activeAdjustment={activeAdjustment} onSelect={setActiveFeature} onStatusSelect={(nextStatus) => { setStatus(nextStatus); void load(nextStatus); }} onAdjustmentSelect={setActiveAdjustment} onLogout={async () => { await logout(); router.push("/login"); router.refresh(); }} />
    <main className="min-w-0 px-4 py-6 sm:px-6 sm:py-8 lg:px-8"><div className="mx-auto w-full max-w-6xl">
    <header><p className="text-xs font-semibold uppercase tracking-wide text-brand">Administration</p><h1 className="mt-1 text-3xl font-semibold text-text">{activeFeature === "approval" ? "Application approval" : activeFeature === "query-transaction" ? "Query transaction history" : "Adjustments"}</h1><p className="mt-1 text-sm text-text-2">{activeFeature === "approval" ? "Review submitted customer identity applications." : activeFeature === "query-transaction" ? "Inspect any account's balance and event timeline." : "Create and review dual-control balance corrections."}</p></header>
    {activeFeature === "approval" && <>
    {error && <p className="mt-4 text-sm text-negative">{error}</p>}
    <div className="mt-6 grid gap-6 lg:grid-cols-[1fr_1.4fr]">
      <section className="rounded-xl border border-border bg-surface p-4"><h2 className="font-semibold">Applications</h2><div className="mt-3 space-y-2">{items.length === 0 && <p className="text-sm text-text-muted">No applications.</p>}{items.map((item) => <button key={item.customer_id} onClick={() => void open(item.customer_id)} className="w-full rounded-lg border border-border p-3 text-left hover:bg-surface-2"><p className="font-medium">{item.legal_first_name} {item.legal_last_name}</p><p className="text-xs text-text-muted">{item.email} · {item.requested_initial_deposit.toFixed(2)}</p></button>)}</div></section>
      <section className="rounded-xl border border-border bg-surface p-5">{!selected ? <p className="text-sm text-text-muted">Select an application to review.</p> : <div><h2 className="text-xl font-semibold">{selected.legal_first_name} {selected.legal_last_name}</h2><dl className="mt-4 grid gap-3 text-sm sm:grid-cols-2"><div><dt className="text-text-muted">Email</dt><dd>{selected.email}</dd></div><div><dt className="text-text-muted">Phone</dt><dd>{selected.phone}</dd></div><div><dt className="text-text-muted">Birth date</dt><dd>{selected.date_of_birth.slice(0, 10)}</dd></div><div><dt className="text-text-muted">Nationality</dt><dd>{selected.nationality}</dd></div><div><dt className="text-text-muted">Document</dt><dd>{selected.government_document.type} · {selected.government_document.number}</dd></div><div><dt className="text-text-muted">Requested deposit</dt><dd>{selected.requested_initial_deposit.toFixed(2)}</dd></div></dl>{image && <img src={image} alt="Submitted passport" className="mt-5 max-h-80 rounded-lg border border-border object-contain" />}{selected.kyc_status === "waiting_for_approval" && <div className="mt-5"><textarea value={reason} onChange={(event) => setReason(event.target.value)} maxLength={500} placeholder="Rejection reason" className="w-full rounded-lg border border-border-strong bg-surface p-3 text-sm" /><div className="mt-3 flex gap-3"><button disabled={busy} onClick={() => void review("approve")} className="rounded-lg bg-positive px-5 py-2 text-sm font-semibold text-white">Approve</button><button disabled={busy || !reason.trim()} onClick={() => void review("reject")} className="rounded-lg bg-negative px-5 py-2 text-sm font-semibold text-white">Reject</button></div></div>}{selected.rejection_reason && <p className="mt-4 rounded-lg bg-negative-soft p-3 text-sm text-negative">{selected.rejection_reason}</p>}</div>}</section>
    </div>
    </>}
    {activeFeature === "query-transaction" && <AdminTransactionSearch />}
    {activeFeature === "adjustments" && <AdminAdjustments key={activeAdjustment} mode={activeAdjustment} adminId={adminId} />}
    </div></main>
  </div>;
}
