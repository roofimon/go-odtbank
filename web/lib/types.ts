export type Account = {
  id: string;
  balance: number;
  event_count: number;
};

export type StoredEvent = {
  seq: number;
  type: "AccountOpened" | "MoneyDebited" | "MoneyCredited";
  amount?: number;
  occurred_at: string;
};

export type AccountRef = {
  ID: string;
  Balance: number;
};

export type TransferReceipt = {
  InitialSourceAccount: AccountRef;
  InitialDestinationAccount: AccountRef;
  FinalSourceAccount: AccountRef;
  FinalDestinationAccount: AccountRef;
  TransferAmount: number;
  FeeAmount: number;
};

export type DepositReceipt = {
  InitialAccount: AccountRef;
  FinalAccount: AccountRef;
  DepositAmount: number;
};

export type EventLogResponse = {
  aggregate_id: string;
  events: StoredEvent[];
};

export type AccountsWithMeta = {
  accounts: Account[];
};
