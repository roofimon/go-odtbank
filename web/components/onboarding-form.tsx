"use client";

import { FormEvent, useState, type InputHTMLAttributes } from "react";
import { useRouter } from "next/navigation";
import { onboardCustomer } from "../lib/api";
import type { OnboardingReceipt, OnboardingRequest } from "../lib/types";

const initialForm: OnboardingRequest = {
  legal_first_name: "",
  legal_last_name: "",
  date_of_birth: "",
  nationality: "",
  email: "",
  phone: "",
  password: "",
  residential_address: {
    line1: "", line2: "", city: "", state_or_province: "", postal_code: "", country: "",
  },
  government_document: { type: "passport", number: "", issuing_country: "" },
  initial_deposit: 0,
};

const inputClass =
  "w-full rounded-lg border border-border-strong bg-surface px-3 py-2 text-sm text-text transition-colors placeholder:text-text-muted focus:border-brand focus:outline-none focus:ring-2 focus:ring-brand/20";

export function OnboardingForm() {
  const router = useRouter();
  const [step, setStep] = useState(1);
  const [form, setForm] = useState(initialForm);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const [receipt, setReceipt] = useState<OnboardingReceipt | null>(null);
  const [passportImage, setPassportImage] = useState<File | null>(null);
  const [passwordConfirmation, setPasswordConfirmation] = useState("");

  function update<K extends keyof OnboardingRequest>(key: K, value: OnboardingRequest[K]) {
    setForm((current) => ({ ...current, [key]: value }));
  }
  function updateAddress(key: keyof OnboardingRequest["residential_address"], value: string) {
    update("residential_address", { ...form.residential_address, [key]: value });
  }
  function updateDocument(key: keyof OnboardingRequest["government_document"], value: string) {
    update("government_document", { ...form.government_document, [key]: value } as OnboardingRequest["government_document"]);
  }

  function continueTo(nextStep: number) {
    const required = step === 1
      ? [form.legal_first_name, form.legal_last_name, form.date_of_birth, form.nationality,
          form.government_document.number, form.government_document.issuing_country]
      : [form.email, form.phone, form.residential_address.line1, form.residential_address.city,
          form.residential_address.postal_code, form.residential_address.country, form.password, passwordConfirmation];
    if (required.some((value) => !value.trim()) || (step === 1 && !passportImage)) {
      setError("Complete all required fields before continuing.");
      return;
    }
    if (step === 2 && form.password !== passwordConfirmation) {
      setError("Passwords do not match.");
      return;
    }
    setError(null);
    setStep(nextStep);
  }

  async function submit(event: FormEvent) {
    event.preventDefault();
    setLoading(true);
    setError(null);
    try {
      if (!passportImage) {
        setError("Attach a passport image before submitting.");
        return;
      }
      setReceipt(await onboardCustomer(form, passportImage));
    } catch (err) {
      setError(err instanceof Error ? err.message : "Onboarding failed");
    } finally {
      setLoading(false);
    }
  }

  if (receipt) {
    return (
      <div className="rounded-xl border border-positive/30 bg-surface p-6">
        <div className="mb-4 grid size-11 place-items-center rounded-full bg-positive-soft text-xl text-positive">✓</div>
        <p className="text-xs font-semibold uppercase tracking-wide text-positive">Application submitted</p>
        <h2 className="mt-1 text-xl font-semibold text-text">Waiting for approval</h2>
        <p className="mt-3 text-sm text-text-2">An administrator will review your information and passport. Log in to check your application status.</p>
        <button type="button" onClick={() => router.push(`/login?email=${encodeURIComponent(form.email)}`)} className="mt-5 rounded-lg bg-brand px-5 py-2 text-sm font-semibold text-white hover:bg-brand-hover">
          Continue to login
        </button>
      </div>
    );
  }

  return (
    <form onSubmit={submit} className="rounded-xl border border-border bg-surface p-4 sm:p-6">
      <ol className="mb-6 grid grid-cols-3 gap-2" aria-label="Onboarding progress">
        {["Identity", "Contact & address", "Review & funding"].map((label, index) => (
          <li key={label} className={`border-t-2 pt-2 text-xs font-medium ${step >= index + 1 ? "border-brand text-brand" : "border-border text-text-muted"}`}>
            {index + 1}. {label}
          </li>
        ))}
      </ol>

      {step === 1 && <IdentityStep form={form} update={update} updateDocument={updateDocument} passportImage={passportImage} setPassportImage={setPassportImage} setError={setError} />}
      {step === 2 && <ContactStep form={form} update={update} updateAddress={updateAddress} passwordConfirmation={passwordConfirmation} setPasswordConfirmation={setPasswordConfirmation} />}
      {step === 3 && <ReviewStep form={form} update={update} />}

      {error && <p className="mt-4 rounded-lg border border-negative/30 bg-negative-soft px-3 py-2 text-sm text-negative">{error}</p>}
      <div className="mt-6 flex gap-3">
        {step > 1 && <button type="button" onClick={() => { setError(null); setStep(step - 1); }} className="rounded-lg border border-border-strong px-5 py-2 text-sm font-semibold text-text hover:bg-surface-2">Back</button>}
        {step < 3 ? (
          <button type="button" onClick={() => continueTo(step + 1)} className="rounded-lg bg-brand px-5 py-2 text-sm font-semibold text-white hover:bg-brand-hover">Continue</button>
        ) : (
          <button type="submit" disabled={loading} className="rounded-lg bg-brand px-5 py-2 text-sm font-semibold text-white hover:bg-brand-hover disabled:opacity-40">{loading ? "Submitting…" : "Submit application"}</button>
        )}
      </div>
    </form>
  );
}

type StepProps = {
  form: OnboardingRequest;
  update: <K extends keyof OnboardingRequest>(key: K, value: OnboardingRequest[K]) => void;
};

function Field({ label, ...props }: InputHTMLAttributes<HTMLInputElement> & { label: string }) {
  return <label className="space-y-1.5"><span className="text-xs font-medium text-text-2">{label}</span><input {...props} className={inputClass} /></label>;
}

function IdentityStep({ form, update, updateDocument, passportImage, setPassportImage, setError }: StepProps & { updateDocument: (key: keyof OnboardingRequest["government_document"], value: string) => void; passportImage: File | null; setPassportImage: (file: File | null) => void; setError: (error: string | null) => void }) {
  return <fieldset><legend className="mb-4 text-sm font-semibold text-text">Identity and document</legend><div className="grid gap-4 sm:grid-cols-2">
    <Field label="Legal first name" value={form.legal_first_name} onChange={(e) => update("legal_first_name", e.target.value)} required />
    <Field label="Legal last name" value={form.legal_last_name} onChange={(e) => update("legal_last_name", e.target.value)} required />
    <Field label="Date of birth" type="date" value={form.date_of_birth} onChange={(e) => update("date_of_birth", e.target.value)} required />
    <Field label="Nationality (2-letter code)" maxLength={2} value={form.nationality} onChange={(e) => update("nationality", e.target.value)} placeholder="TH" required />
    <label className="space-y-1.5"><span className="text-xs font-medium text-text-2">Document type</span><select value={form.government_document.type} onChange={(e) => updateDocument("type", e.target.value)} className={inputClass}><option value="passport">Passport</option><option value="national_id">National ID</option><option value="driver_license">Driver license</option></select></label>
    <Field label="Document number" value={form.government_document.number} onChange={(e) => updateDocument("number", e.target.value)} required />
    <Field label="Issuing country" maxLength={2} value={form.government_document.issuing_country} onChange={(e) => updateDocument("issuing_country", e.target.value)} placeholder="TH" required />
    <label className="space-y-1.5 sm:col-span-2"><span className="text-xs font-medium text-text-2">Passport image</span><input type="file" accept="image/jpeg,image/png,image/webp" required onChange={(e) => { const file = e.target.files?.[0] ?? null; if (file && file.size > 5 * 1024 * 1024) { setError("Passport image must not exceed 5 MB."); setPassportImage(null); e.target.value = ""; return; } setError(null); setPassportImage(file); }} className={inputClass} /><span className="block text-xs text-text-muted">JPEG, PNG, or WebP · maximum 5 MB{passportImage ? ` · ${passportImage.name}` : ""}</span></label>
  </div></fieldset>;
}

function ContactStep({ form, update, updateAddress, passwordConfirmation, setPasswordConfirmation }: StepProps & { updateAddress: (key: keyof OnboardingRequest["residential_address"], value: string) => void; passwordConfirmation: string; setPasswordConfirmation: (value: string) => void }) {
  return <fieldset><legend className="mb-4 text-sm font-semibold text-text">Contact and residential address</legend><div className="grid gap-4 sm:grid-cols-2">
    <Field label="Email" type="email" value={form.email} onChange={(e) => update("email", e.target.value)} required />
    <Field label="Phone (E.164)" type="tel" value={form.phone} onChange={(e) => update("phone", e.target.value)} placeholder="+66812345678" required />
    <Field label="Password" type="password" minLength={10} maxLength={128} value={form.password} onChange={(e) => update("password", e.target.value)} required />
    <Field label="Confirm password" type="password" minLength={10} maxLength={128} value={passwordConfirmation} onChange={(e) => setPasswordConfirmation(e.target.value)} required />
    <Field label="Address line 1" value={form.residential_address.line1} onChange={(e) => updateAddress("line1", e.target.value)} required />
    <Field label="Address line 2 (optional)" value={form.residential_address.line2} onChange={(e) => updateAddress("line2", e.target.value)} />
    <Field label="City" value={form.residential_address.city} onChange={(e) => updateAddress("city", e.target.value)} required />
    <Field label="State / province (optional)" value={form.residential_address.state_or_province} onChange={(e) => updateAddress("state_or_province", e.target.value)} />
    <Field label="Postal code" value={form.residential_address.postal_code} onChange={(e) => updateAddress("postal_code", e.target.value)} required />
    <Field label="Country (2-letter code)" maxLength={2} value={form.residential_address.country} onChange={(e) => updateAddress("country", e.target.value)} placeholder="TH" required />
  </div></fieldset>;
}

function ReviewStep({ form, update }: StepProps) {
  return <fieldset><legend className="mb-4 text-sm font-semibold text-text">Review and initial funding</legend><div className="rounded-lg bg-surface-2 p-4 text-sm text-text-2"><p className="font-medium text-text">{form.legal_first_name} {form.legal_last_name}</p><p>{form.email} · {form.phone}</p><p className="mt-2">{form.government_document.type.replaceAll("_", " ")} · {form.government_document.issuing_country} · ending {form.government_document.number.slice(-4)}</p></div><div className="mt-4 max-w-sm"><Field label="Initial deposit (optional; 0 or at least 10.00)" type="number" min="0" step="0.01" value={form.initial_deposit} onChange={(e) => update("initial_deposit", Number(e.target.value))} /></div><p className="mt-4 text-xs text-text-muted">Demo only: submitted KYC data is stored locally and is not externally verified.</p></fieldset>;
}
