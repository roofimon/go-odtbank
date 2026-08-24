"use client";

import { FormEvent, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { login } from "../lib/api";

export function LoginForm({ initialEmail }: { initialEmail: string }) {
  const router = useRouter();
  const [email, setEmail] = useState(initialEmail);
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  async function submit(event: FormEvent) { event.preventDefault(); setLoading(true); setError(null); try { const principal = await login(email, password); router.push(principal.role === "admin" ? "/admin" : "/"); router.refresh(); } catch (err) { setError(err instanceof Error ? err.message : "Login failed"); } finally { setLoading(false); } }
  return <form onSubmit={submit} className="rounded-xl border border-border bg-surface p-6"><h1 className="text-2xl font-semibold text-text">Welcome back</h1><p className="mt-1 text-sm text-text-2">Log in to manage your account.</p><label className="mt-6 block space-y-1.5"><span className="text-xs font-medium text-text-2">Email</span><input type="email" required value={email} onChange={(e) => setEmail(e.target.value)} className="w-full rounded-lg border border-border-strong bg-surface px-3 py-2 text-sm" /></label><label className="mt-4 block space-y-1.5"><span className="text-xs font-medium text-text-2">Password</span><input type="password" required value={password} onChange={(e) => setPassword(e.target.value)} className="w-full rounded-lg border border-border-strong bg-surface px-3 py-2 text-sm" /></label>{error && <p className="mt-4 text-sm text-negative">{error}</p>}<button disabled={loading} className="mt-6 w-full rounded-lg bg-brand px-5 py-2 text-sm font-semibold text-white disabled:opacity-40">{loading ? "Logging in…" : "Log in"}</button><p className="mt-5 text-center text-sm text-text-2">New customer? <Link href="/onboarding" className="font-medium text-brand">Open an account</Link></p></form>;
}
