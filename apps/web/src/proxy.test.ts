import { NextRequest } from "next/server";
import { expect, it } from "vitest";
import { proxy } from "./proxy";

it("preserves the requested workbench path and overwrites a forged return header", () => {
  const response = proxy(
    new NextRequest("https://workbench.test/todos?view=overdue", {
      headers: { "x-ab-return-to": "https://evil.test" },
    }),
  );
  expect(response.headers.get("x-middleware-request-x-ab-return-to")).toBe(
    "/todos?view=overdue",
  );
});
