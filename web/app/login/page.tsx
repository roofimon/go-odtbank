import { LoginForm } from "../../components/login-form";

export default async function LoginPage({ searchParams }: { searchParams: Promise<{ email?: string }> }) {
  const params = await searchParams;
  return <main className="mx-auto grid min-h-screen w-full max-w-md place-items-center px-4 py-8"><div className="w-full"><LoginForm initialEmail={params.email ?? ""} /></div></main>;
}
