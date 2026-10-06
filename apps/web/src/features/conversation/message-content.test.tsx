import { render, screen } from "@testing-library/react";
import { expect, it } from "vitest";
import { MessageContent } from "./message-content";

it("renders readable lists and code without executing embedded HTML or unsafe links", () => {
  render(
    <MessageContent
      text={
        '## 计划\n- **提交周报**\n- [资料](https://example.com)\n\n```js\nalert("test")\n```\n<script>alert(1)</script>\n[危险](javascript:alert(1))'
      }
    />,
  );
  expect(screen.getByRole("heading", { name: "计划" })).toBeInTheDocument();
  expect(screen.getByText("提交周报").tagName).toBe("STRONG");
  expect(screen.getByRole("link", { name: "资料" })).toHaveAttribute(
    "href",
    "https://example.com",
  );
  expect(document.querySelector("pre code")).toHaveTextContent('alert("test")');
  expect(document.querySelector("script")).toBeNull();
  expect(screen.queryByRole("link", { name: "危险" })).not.toBeInTheDocument();
});
