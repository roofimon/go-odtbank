import { cookies } from "next/headers";
import { redirect } from "next/navigation";
import { getApplications, getMe } from "../../lib/api";
import { AdminDashboard } from "../../components/admin-dashboard";

export const dynamic = "force-dynamic";
export default async function AdminPage(){const cookie=(await cookies()).toString();let principal;try{principal=await getMe(cookie)}catch{redirect("/login")};if(principal.role!=="admin")redirect("/");const applications=await getApplications("waiting_for_approval",cookie);return <AdminDashboard initialItems={applications} />}
