import { expect, it } from "vitest";
import {
  safeInternalReturnTo,
  clearConversationDrafts,
  rememberConversationOwner,
  recoverExpiredSession,
} from "./session-recovery";

it("allows internal return paths and rejects redirect bypasses", () => {
  expect(safeInternalReturnTo("/todos?view=today")).toBe("/todos?view=today");
  for (const path of [
    "https://evil.test",
    "//evil.test",
    "/\\evil.test",
    "/%2f%2fevil.test",
    "/login",
    "javascript:alert(1)",
  ])
    expect(safeInternalReturnTo(path)).toBe("/");
});
it("recovers an expired session with its full internal path and preserves drafts", () => {
  window.history.replaceState({}, "", "/todos?view=today#edit");
  sessionStorage.setItem("ab.conversation.draft.new", "draft");
  const paths: string[] = [];
  expect(
    recoverExpiredSession(new Response("", { status: 401 }), (path) =>
      paths.push(path),
    ),
  ).toBe(true);
  expect(paths).toEqual(["/login?returnTo=%2Ftodos%3Fview%3Dtoday%23edit"]);
  expect(sessionStorage.getItem("ab.conversation.draft.new")).toBe("draft");
  expect(
    recoverExpiredSession(new Response("", { status: 503 }), (path) =>
      paths.push(path),
    ),
  ).toBe(false);
  window.history.replaceState({}, "", "/");
});
it("preserves same-owner drafts and clears them when account changes", () => {
  sessionStorage.clear();
  rememberConversationOwner("user-1");
  sessionStorage.setItem("ab.conversation.draft.new", "草稿");
  sessionStorage.setItem("unrelated", "keep");
  rememberConversationOwner("user-1");
  expect(sessionStorage.getItem("ab.conversation.draft.new")).toBe("草稿");
  rememberConversationOwner("user-2");
  expect(sessionStorage.getItem("ab.conversation.draft.new")).toBeNull();
  clearConversationDrafts();
  expect(sessionStorage.getItem("unrelated")).toBe("keep");
});
