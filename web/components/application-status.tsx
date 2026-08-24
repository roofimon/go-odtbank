"use client";

import { useRouter } from "next/navigation";
import { logout } from "../lib/api";
import type { Principal } from "../lib/types";

export function ApplicationStatus({ principal }: { principal: Principal }) {
  const router = useRouter();
  const rejected = principal.kyc_status === "rejected";
  return <main className="mx-auto grid min-h-screen max-w-xl place-items-center px-4"><section className="w-full rounded-xl border border-border bg-surface p-6"><p className={`text-xs font-semibold uppercase tracking-wide ${rejected ? "text-negative" : "text-brand"}`}>{rejected ? "Application rejected" : "Application submitted"}</p><h1 className="mt-2 text-2xl font-semibold text-text">{rejected ? "We could not approve your account" : "Waiting for approval"}</h1><p className="mt-3 text-sm text-text-2">{rejected ? principal.rejection_reason : "An administrator is reviewing your identity information and passport. Refresh this page later to check the status."}</p><div className="mt-6 flex gap-3">{!rejected && <button onClick={() => router.refresh()} className="rounded-lg bg-brand px-5 py-2 text-sm font-semibold text-white">Refresh status</button>}<button onClick={async()=>{await logout();router.push("/login");router.refresh();}} className="rounded-lg border border-border-strong px-5 py-2 text-sm font-semibold text-text">Log out</button></div></section></main>;
}
