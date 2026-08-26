import type { KYCStatus } from "../lib/types";

export type AdminFeature = "approval" | "query-transaction";

const links: { id: AdminFeature; label: string }[] = [
  { id: "approval", label: "Approval" },
  { id: "query-transaction", label: "Query transaction" },
];

const statuses: { id: KYCStatus; label: string }[] = [
  { id: "waiting_for_approval", label: "Waiting for approval" },
  { id: "approved", label: "Approved" },
  { id: "rejected", label: "Rejected" },
];

export function AdminNavigationMenu({ activeFeature, activeStatus, onSelect, onStatusSelect, onLogout }: { activeFeature: AdminFeature; activeStatus: KYCStatus; onSelect: (feature: AdminFeature) => void; onStatusSelect: (status: KYCStatus) => void; onLogout: () => void }) {
  return <aside className="border-b border-border bg-surface lg:sticky lg:top-0 lg:h-screen lg:border-b-0 lg:border-r"><div className="mx-auto flex max-w-6xl items-center gap-6 px-4 py-4 lg:h-full lg:w-64 lg:flex-col lg:items-stretch lg:px-5 lg:py-6"><button type="button" onClick={() => onSelect("approval")} className="flex shrink-0 items-center gap-3 text-left"><span className="grid size-9 place-items-center rounded-xl bg-brand text-sm font-bold text-white">O</span><span><span className="block text-sm font-semibold tracking-tight text-text">ODT Bank Admin</span><span className="hidden text-xs text-text-muted sm:block">Operations console</span></span></button><nav aria-label="Admin navigation" className="min-w-0 flex-1 lg:mt-5"><ul className="flex gap-1 overflow-x-auto lg:flex-col">{links.map((link) => <li key={link.id}><button type="button" onClick={() => onSelect(link.id)} aria-current={activeFeature === link.id ? "page" : undefined} className={`block w-full whitespace-nowrap rounded-lg px-3 py-2 text-left text-sm font-medium transition-colors ${activeFeature === link.id ? "bg-brand-soft text-brand" : "text-text-2 hover:bg-surface-2 hover:text-text"}`}>{link.label}</button>{link.id === "approval" && activeFeature === "approval" && <ul className="ml-2 flex gap-1 border-l border-border pl-2 lg:mt-1 lg:flex-col">{statuses.map((item) => <li key={item.id}><button type="button" onClick={() => onStatusSelect(item.id)} aria-current={activeStatus === item.id ? "page" : undefined} className={`block w-full whitespace-nowrap rounded-md px-3 py-1.5 text-left text-xs transition-colors ${activeStatus === item.id ? "bg-surface-2 font-semibold text-brand" : "text-text-muted hover:text-text"}`}>{item.label}</button></li>)}</ul>}</li>)}</ul></nav><div className="hidden rounded-lg border border-border bg-surface-2 p-3 text-xs text-text-2 lg:block">Review customer applications and inspect account activity.</div><button type="button" onClick={onLogout} className="shrink-0 rounded-lg border border-border-strong px-3 py-2 text-left text-sm font-medium text-text-2 hover:bg-surface-2">Log out</button></div></aside>;
}
