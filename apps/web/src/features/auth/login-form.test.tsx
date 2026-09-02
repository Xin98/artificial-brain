import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";

import { LoginForm } from "./login-form";

afterEach(() => {
  window.localStorage.clear();
});

function challengeResponse(): Response {
  return new Response("{}", { status: 202 });
}

function verifyResponse(): Response {
  return new Response(
    JSON.stringify({
      userId: "user-1",
      workspaceId: "ws-1",
      expiresAt: "2026-08-25T12:00:00Z",
    }),
    { status: 200, headers: { "content-type": "application/json" } },
  );
}

it("runs the two-step login with exact request bodies", async () => {
  const fetcher = vi
    .fn()
    .mockResolvedValueOnce(challengeResponse())
    .mockResolvedValueOnce(verifyResponse());
  const onNavigate = vi.fn();
  render(
    <LoginForm
      fetcher={fetcher as unknown as typeof fetch}
      onNavigate={onNavigate}
    />,
  );

  fireEvent.change(screen.getByLabelText("手机号或邮箱"), {
    target: { value: "+8613800138000" },
  });
  fireEvent.click(screen.getByRole("button", { name: "获取验证码" }));

  await waitFor(() =>
    expect(screen.getByLabelText("验证码")).toBeInTheDocument(),
  );
  const [challengeUrl, challengeInit] = fetcher.mock.calls[0];
  expect(challengeUrl).toBe("/api/v1/auth/login/request");
  expect(JSON.parse(String(challengeInit?.body))).toEqual({
    phone: "+8613800138000",
  });

  fireEvent.change(screen.getByLabelText("验证码"), {
    target: { value: "123456" },
  });
  fireEvent.click(screen.getByRole("button", { name: "登录" }));

  await waitFor(() => expect(onNavigate).toHaveBeenCalledWith("/"));
  const [verifyUrl, verifyInit] = fetcher.mock.calls[1];
  expect(verifyUrl).toBe("/api/v1/auth/login/verify");
  expect(JSON.parse(String(verifyInit?.body))).toEqual({
    phone: "+8613800138000",
    code: "123456",
  });
});

it("sends an email identifier when the input contains @", async () => {
  const fetcher = vi
    .fn()
    .mockResolvedValueOnce(challengeResponse())
    .mockResolvedValueOnce(verifyResponse());
  const onNavigate = vi.fn();
  render(
    <LoginForm
      fetcher={fetcher as unknown as typeof fetch}
      onNavigate={onNavigate}
    />,
  );

  fireEvent.change(screen.getByLabelText("手机号或邮箱"), {
    target: { value: "admin@example.com" },
  });
  fireEvent.click(screen.getByRole("button", { name: "获取验证码" }));

  await waitFor(() =>
    expect(screen.getByLabelText("验证码")).toBeInTheDocument(),
  );
  const [challengeUrl, challengeInit] = fetcher.mock.calls[0];
  expect(challengeUrl).toBe("/api/v1/auth/login/request");
  expect(JSON.parse(String(challengeInit?.body))).toEqual({
    email: "admin@example.com",
  });

  fireEvent.change(screen.getByLabelText("验证码"), {
    target: { value: "123456" },
  });
  fireEvent.click(screen.getByRole("button", { name: "登录" }));

  await waitFor(() => expect(onNavigate).toHaveBeenCalledWith("/"));
  const [verifyUrl, verifyInit] = fetcher.mock.calls[1];
  expect(verifyUrl).toBe("/api/v1/auth/login/verify");
  expect(JSON.parse(String(verifyInit?.body))).toEqual({
    email: "admin@example.com",
    code: "123456",
  });
});

it("shows the rate-limit message when the API throttles", async () => {
  const fetcher = vi
    .fn()
    .mockResolvedValue(
      new Response(JSON.stringify({ code: "rate_limited" }), { status: 429 }),
    );
  render(<LoginForm fetcher={fetcher as unknown as typeof fetch} />);

  fireEvent.change(screen.getByLabelText("手机号或邮箱"), {
    target: { value: "+8613800138000" },
  });
  fireEvent.click(screen.getByRole("button", { name: "获取验证码" }));

  await waitFor(() =>
    expect(screen.getByRole("alert")).toHaveTextContent("请求过于频繁"),
  );
});

it("shows the invalid-code message on rejected verification", async () => {
  const fetcher = vi
    .fn()
    .mockResolvedValueOnce(challengeResponse())
    .mockResolvedValueOnce(
      new Response(JSON.stringify({ code: "unauthenticated" }), {
        status: 401,
      }),
    );
  render(<LoginForm fetcher={fetcher as unknown as typeof fetch} />);

  fireEvent.change(screen.getByLabelText("手机号或邮箱"), {
    target: { value: "+8613800138000" },
  });
  fireEvent.click(screen.getByRole("button", { name: "获取验证码" }));
  await waitFor(() =>
    expect(screen.getByLabelText("验证码")).toBeInTheDocument(),
  );

  fireEvent.change(screen.getByLabelText("验证码"), {
    target: { value: "000000" },
  });
  fireEvent.click(screen.getByRole("button", { name: "登录" }));

  await waitFor(() =>
    expect(screen.getByRole("alert")).toHaveTextContent("验证码不正确"),
  );
});

it("shows the sms-unavailable message when phone login is rejected", async () => {
  const fetcher = vi.fn().mockResolvedValue(
    new Response(JSON.stringify({ code: "sms_unavailable" }), {
      status: 503,
    }),
  );
  render(<LoginForm fetcher={fetcher as unknown as typeof fetch} />);

  fireEvent.change(screen.getByLabelText("手机号或邮箱"), {
    target: { value: "+8613800138000" },
  });
  fireEvent.click(screen.getByRole("button", { name: "获取验证码" }));

  await waitFor(() =>
    expect(screen.getByRole("alert")).toHaveTextContent("暂不支持手机号登录"),
  );
});
