import type {
  Account,
  ApplicationDetail,
  ApplicationSummary,
  AdminAccountHistory,
  AdjustmentRequest,
  AccountsWithMeta,
  DepositReceipt,
  EventLogResponse,
  OnboardingReceipt,
  OnboardingRequest,
  Principal,
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
      credentials: "include",
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

export function getAccounts(cookie?: string): Promise<Account[]> {
  return request<AccountsWithMeta>("/accounts", cookie ? { headers: { Cookie: cookie } } : undefined).then((r) => r.accounts);
}

export function getEvents(aggregateId: string, cookie?: string): Promise<EventLogResponse> {
  return request<EventLogResponse>(
    `/accounts/${encodeURIComponent(aggregateId)}/events`,
    cookie ? { headers: { Cookie: cookie } } : undefined,
  );
}

export function transfer(
  amount: number,
  sourceAccountId: string,
  destinationAccountId: string,
  idempotencyKey: string,
): Promise<TransferReceipt> {
  return request<TransferReceipt>(
    "/transfer",
    { method: "POST", headers: { "Idempotency-Key": idempotencyKey } },
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
  return fetch(`${API_URL}/onboarding`, { method: "POST", body: form, cache: "no-store", credentials: "include" })
    .catch(() => { throw new ApiError(0, `Cannot reach backend at ${API_URL}`); })
    .then(async (response) => {
      const json = (await response.json().catch(() => null)) as (OnboardingReceipt & { error?: string }) | null;
      if (!response.ok) throw new ApiError(response.status, json?.error ?? response.statusText);
      return json as OnboardingReceipt;
    });
}

export function login(email: string, password: string): Promise<Principal> {
  return request<Principal>("/login", { method: "POST" }, JSON.stringify({ email, password }));
}

export function logout(): Promise<void> {
  return request<void>("/logout", { method: "POST" });
}

export function getMe(cookie?: string): Promise<Principal> { return request<Principal>("/me", cookie ? { headers: { Cookie: cookie } } : undefined); }
export function getApplications(status: string, cookie?: string): Promise<ApplicationSummary[]> { return request<{ applications: ApplicationSummary[] }>(`/admin/applications?status=${encodeURIComponent(status)}`, cookie ? { headers: { Cookie: cookie } } : undefined).then((r) => r.applications); }
export function getApplication(id: string): Promise<ApplicationDetail> { return request<ApplicationDetail>(`/admin/applications/${encodeURIComponent(id)}`); }
export function approveApplication(id: string): Promise<void> { return request<void>(`/admin/applications/${encodeURIComponent(id)}/approve`, { method: "POST" }); }
export function rejectApplication(id: string, reason: string): Promise<void> { return request<void>(`/admin/applications/${encodeURIComponent(id)}/reject`, { method: "POST" }, JSON.stringify({ reason })); }
export async function getPassport(id: string): Promise<Blob> { const response = await fetch(`${API_URL}/admin/applications/${encodeURIComponent(id)}/passport`, { credentials: "include", cache: "no-store" }); if (!response.ok) throw new ApiError(response.status, "Could not load passport image"); return response.blob(); }
export function getAdminAccountHistory(accountId: string): Promise<AdminAccountHistory> { return request<AdminAccountHistory>(`/admin/accounts/${encodeURIComponent(accountId)}/events`); }
export function createAdjustment(payload: Partial<AdjustmentRequest>): Promise<AdjustmentRequest> { return request<AdjustmentRequest>("/admin/adjustments", { method: "POST" }, JSON.stringify(payload)); }
export function getAdjustments(status: string): Promise<AdjustmentRequest[]> { return request<{ adjustments: AdjustmentRequest[] }>(`/admin/adjustments?status=${encodeURIComponent(status)}`).then((result) => result.adjustments); }
export function approveAdjustment(id: string): Promise<void> { return request<void>(`/admin/adjustments/${encodeURIComponent(id)}/approve`, { method: "POST" }); }
export function rejectAdjustment(id: string, reason: string): Promise<void> { return request<void>(`/admin/adjustments/${encodeURIComponent(id)}/reject`, { method: "POST" }, JSON.stringify({ reason })); }
