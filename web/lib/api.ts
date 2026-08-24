import type {
  Account,
  AccountsWithMeta,
  DepositReceipt,
  EventLogResponse,
  OnboardingReceipt,
  OnboardingRequest,
  TransferReceipt,
  WithdrawalReceipt,
} from "./types";

export const API_URL =
  process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";

export class ApiError extends Error {
  status: number;

  constructor(status: number, message: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
  }
}

async function request<T>(
  path: string,
  init?: RequestInit,
  payload?: string,
): Promise<T> {
  let res: Response;
  try {
    res = await fetch(`${API_URL}${path}`, {
      ...init,
      body: payload,
      headers: { "Content-Type": "application/json", ...init?.headers },
      cache: "no-store",
    });
  } catch {
    throw new ApiError(0, `Cannot reach backend at ${API_URL}`);
  }

  const json = (await res.json().catch(() => null)) as
    | (T & { error?: string })
    | null;

  if (!res.ok) {
    throw new ApiError(res.status, json?.error ?? res.statusText);
  }

  return json as T;
}

export function getAccounts(): Promise<Account[]> {
  return request<AccountsWithMeta>("/accounts").then((r) => r.accounts);
}

export function getEvents(aggregateId: string): Promise<EventLogResponse> {
  return request<EventLogResponse>(
    `/accounts/${encodeURIComponent(aggregateId)}/events`,
  );
}

export function transfer(
  amount: number,
  sourceAccountId: string,
  destinationAccountId: string,
): Promise<TransferReceipt> {
  return request<TransferReceipt>(
    "/transfer",
    { method: "POST" },
    JSON.stringify({
      amount,
      source_account_id: sourceAccountId,
      destination_account_id: destinationAccountId,
    }),
  );
}

export function deposit(
  amount: number,
  accountId: string,
): Promise<DepositReceipt> {
  return request<DepositReceipt>(
    "/deposit",
    { method: "POST" },
    JSON.stringify({ amount, account_id: accountId }),
  );
}

export function withdraw(
  amount: number,
  accountId: string,
): Promise<WithdrawalReceipt> {
  return request<WithdrawalReceipt>(
    "/withdraw",
    { method: "POST" },
    JSON.stringify({ amount, account_id: accountId }),
  );
}

export function onboardCustomer(
  payload: OnboardingRequest,
  passportImage: File,
): Promise<OnboardingReceipt> {
  const form = new FormData();
  form.append("payload", JSON.stringify(payload));
  form.append("passport_image", passportImage);
  return fetch(`${API_URL}/onboarding`, { method: "POST", body: form, cache: "no-store" })
    .catch(() => { throw new ApiError(0, `Cannot reach backend at ${API_URL}`); })
    .then(async (response) => {
      const json = (await response.json().catch(() => null)) as (OnboardingReceipt & { error?: string }) | null;
      if (!response.ok) throw new ApiError(response.status, json?.error ?? response.statusText);
      return json as OnboardingReceipt;
    });
}
