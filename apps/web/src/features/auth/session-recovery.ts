// Only relative, same-origin paths may be used after authentication.
export function safeInternalReturnTo(value: string | null): string {
  if (
    !value ||
    !value.startsWith("/") ||
    value.startsWith("//") ||
    /[\\\u0000-\u001f]/.test(value)
  )
    return "/";
  try {
    const decoded = decodeURIComponent(value);
    if (decoded.startsWith("//") || decoded.includes("\\")) return "/";
    const url = new URL(value, "https://workbench.invalid");
    if (url.origin !== "https://workbench.invalid" || url.pathname === "/login")
      return "/";
    return `${url.pathname}${url.search}${url.hash}`;
  } catch {
    return "/";
  }
}

export function recoverExpiredSession(
  response: Response,
  navigate: (path: string) => void = (path) => window.location.assign(path),
): boolean {
  if (response.status !== 401 || typeof window === "undefined") return false;
  const returnTo = safeInternalReturnTo(
    `${window.location.pathname}${window.location.search}${window.location.hash}`,
  );
  navigate(`/login?returnTo=${encodeURIComponent(returnTo)}`);
  return true;
}

export function clearConversationDrafts(): void {
  try {
    for (const key of Object.keys(sessionStorage))
      if (key.startsWith("ab.conversation.")) sessionStorage.removeItem(key);
  } catch {
    /* Storage may be unavailable in private browsing. */
  }
}

export function rememberConversationOwner(userId: string): void {
  try {
    const owner = sessionStorage.getItem("ab.conversation.owner");
    if (owner && owner !== userId) clearConversationDrafts();
    sessionStorage.setItem("ab.conversation.owner", userId);
  } catch {
    /* Authentication remains usable when storage is unavailable. */
  }
}
