import Link from "next/link";
import { OnboardingForm } from "../../components/onboarding-form";

export default function OnboardingPage() {
  return <main className="mx-auto w-full max-w-3xl px-4 py-8 sm:py-12"><header className="mb-8"><Link href="/login" className="text-sm font-medium text-brand">Already a customer? Log in</Link><h1 className="mt-4 text-3xl font-semibold text-text">Open your ODT Bank account</h1><p className="mt-2 text-sm text-text-2">Complete identity verification, upload your passport, and choose optional opening funding.</p></header><OnboardingForm /></main>;
}
