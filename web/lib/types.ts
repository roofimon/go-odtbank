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
  FinalSourceAccount: AccountRef;
  DestinationAccountID: string;
  TransferAmount: number;
  FeeAmount: number;
};

export type DepositReceipt = {
  InitialAccount: AccountRef;
  FinalAccount: AccountRef;
  DepositAmount: number;
};

export type WithdrawalReceipt = {
  InitialAccount: AccountRef;
  FinalAccount: AccountRef;
  WithdrawalAmount: number;
};

export type EventLogResponse = {
  aggregate_id: string;
  events: StoredEvent[];
};

export type AccountsWithMeta = {
  accounts: Account[];
};

export type OnboardingRequest = {
  legal_first_name: string;
  legal_last_name: string;
  date_of_birth: string;
  nationality: string;
  email: string;
  phone: string;
  password: string;
  residential_address: {
    line1: string;
    line2: string;
    city: string;
    state_or_province: string;
    postal_code: string;
    country: string;
  };
  government_document: {
    type: "passport" | "national_id" | "driver_license";
    number: string;
    issuing_country: string;
  };
  initial_deposit: number;
};

export type Principal = { customer_id: string; account_id: string; email: string };

export type OnboardingReceipt = {
  customer_id: string;
  account_id: string;
  kyc_status: "verified";
  balance: number;
};
