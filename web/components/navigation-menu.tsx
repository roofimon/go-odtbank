export type Feature =
  | "transfer"
  | "deposit"
  | "withdraw"
  | "transaction-history";

const links: { id: Feature; label: string }[] = [
  { id: "transfer", label: "Transfer" },
  { id: "deposit", label: "Deposit" },
  { id: "withdraw", label: "Withdraw" },
  { id: "transaction-history", label: "Transaction history" },
];

type Props = {
  activeFeature: Feature;
  onSelect: (feature: Feature) => void;
};

export function NavigationMenu({ activeFeature, onSelect }: Props) {
  return (
    <aside className="border-b border-border bg-surface lg:sticky lg:top-0 lg:h-screen lg:border-b-0 lg:border-r">
      <div className="mx-auto flex max-w-6xl items-center gap-6 px-4 py-4 lg:h-full lg:w-64 lg:flex-col lg:items-stretch lg:px-5 lg:py-6">
        <button
          type="button"
          onClick={() => onSelect("transfer")}
          className="flex shrink-0 items-center gap-3 text-left"
        >
          <span className="grid size-9 place-items-center rounded-xl bg-brand text-sm font-bold text-white">
            O
          </span>
          <span>
            <span className="block text-sm font-semibold tracking-tight text-text">
              ODT Bank
            </span>
            <span className="hidden text-xs text-text-muted sm:block">
              Event-sourced banking
            </span>
          </span>
        </button>

        <nav
          aria-label="Main navigation"
          className="min-w-0 flex-1 lg:mt-5"
        >
          <ul className="flex gap-1 overflow-x-auto lg:flex-col">
            {links.map((link) => (
              <li key={link.id}>
                <button
                  type="button"
                  onClick={() => onSelect(link.id)}
                  aria-current={activeFeature === link.id ? "page" : undefined}
                  className={`block w-full whitespace-nowrap rounded-lg px-3 py-2 text-left text-sm font-medium transition-colors ${
                    activeFeature === link.id
                      ? "bg-brand-soft text-brand"
                      : "text-text-2 hover:bg-surface-2 hover:text-text"
                  }`}
                >
                  {link.label}
                </button>
              </li>
            ))}
          </ul>
        </nav>

        <div className="hidden rounded-lg border border-border bg-surface-2 p-3 text-xs text-text-2 lg:block">
          Account state is rebuilt from the append-only event log.
        </div>
      </div>
    </aside>
  );
}
