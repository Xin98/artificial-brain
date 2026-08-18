import { redirect } from "next/navigation";

import { apiInternalURL } from "../../shared/server/runtime-config";
import { fetchSession, readSessionCookie } from "../../shared/server/session";
import { WorkbenchShell } from "./workbench-shell";

// WorkbenchLayout is the server-side session gate: without a valid session
// every workbench route redirects to /login.
export default async function WorkbenchLayout({
  children,
}: Readonly<{ children: React.ReactNode }>): Promise<React.JSX.Element> {
  const cookie = await readSessionCookie();
  const session = cookie
    ? await fetchSession(apiInternalURL(), fetch, cookie)
    : null;

  if (!session) {
    redirect("/login");
  }

  return <WorkbenchShell>{children}</WorkbenchShell>;
}
