import { redirect } from "next/navigation";
import { headers } from "next/headers";
import { safeInternalReturnTo } from "../../features/auth/session-recovery";

import { apiInternalURL } from "../../shared/server/runtime-config";
import { lookupSession, readSessionCookie } from "../../shared/server/session";
import { WorkbenchShell } from "./workbench-shell";

// WorkbenchLayout is the server-side session gate: without a valid session
// every workbench route redirects to /login.
export default async function WorkbenchLayout({
  children,
}: Readonly<{ children: React.ReactNode }>): Promise<React.JSX.Element> {
  const cookie = await readSessionCookie();
  const returnTo = safeInternalReturnTo(
    (await headers()).get("x-ab-return-to"),
  );
  const loginPath = `/login?returnTo=${encodeURIComponent(returnTo)}`;
  if (!cookie) {
    redirect(loginPath);
  }
  const result = await lookupSession(apiInternalURL(), fetch, cookie);
  if (result.kind === "unauthenticated") redirect(loginPath);
  if (result.kind === "unavailable")
    return (
      <main className="service-unavailable">
        <h1>工作台暂时无法连接</h1>
        <p>服务暂时不可用，你的登录状态尚未验证。请稍后重新加载。</p>
        <a className="btn-primary" href="">
          重新加载
        </a>
        <a className="btn-ghost" href="/status">
          查看服务状态
        </a>
      </main>
    );
  return <WorkbenchShell session={result.session}>{children}</WorkbenchShell>;
}
