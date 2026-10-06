const PREFIX = "ab.conversation.";

export function readConversationState(key: string): string {
  try {
    return typeof window === "undefined"
      ? ""
      : (window.sessionStorage.getItem(PREFIX + key) ?? "");
  } catch {
    return "";
  }
}

export function writeConversationState(key: string, value: string): void {
  try {
    if (value) window.sessionStorage.setItem(PREFIX + key, value);
    else window.sessionStorage.removeItem(PREFIX + key);
  } catch {
    /* Private browsers can deny storage; the live conversation remains usable. */
  }
}
