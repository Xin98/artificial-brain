import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { expect, it, vi } from "vitest";

import { ChannelManager } from "./channel-manager";

const channel = {
  id: "channel-1",
  kind: "email",
  address: "user@example.com",
  verified: false,
  enabled: true,
  createdAt: "2026-08-18T00:00:00Z",
};

function json(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json" },
  });
}

it("adds a channel with the exact body", async () => {
  const fetcher = vi
    .fn()
    .mockResolvedValueOnce(json(200, { channels: [channel] }))
    .mockResolvedValueOnce(json(201, channel))
    .mockResolvedValueOnce(json(200, { channels: [channel] }));
  render(<ChannelManager fetcher={fetcher as unknown as typeof fetch} />);

  await waitFor(() =>
    expect(screen.getByText(/user@example\.com/)).toBeInTheDocument(),
  );

  fireEvent.change(screen.getByLabelText("类型"), {
    target: { value: "email" },
  });
  fireEvent.change(screen.getByLabelText("地址"), {
    target: { value: "new@example.com" },
  });
  fireEvent.click(screen.getByRole("button", { name: "添加" }));

  await waitFor(() => expect(fetcher).toHaveBeenCalledTimes(3));
  const [url, init] = fetcher.mock.calls[1];
  expect(url).toBe("/api/v1/settings/contact-channels");
  expect(JSON.parse(String(init?.body))).toEqual({
    kind: "email",
    address: "new@example.com",
  });
});

it("shows the conflict message for duplicates", async () => {
  const fetcher = vi
    .fn()
    .mockResolvedValueOnce(json(200, { channels: [] }))
    .mockResolvedValueOnce(json(409, { code: "conflict" }));
  render(<ChannelManager fetcher={fetcher as unknown as typeof fetch} />);

  fireEvent.change(screen.getByLabelText("地址"), {
    target: { value: "dup@example.com" },
  });
  fireEvent.click(screen.getByRole("button", { name: "添加" }));

  await waitFor(() =>
    expect(screen.getByRole("alert")).toHaveTextContent("已存在"),
  );
});

it("verifies a channel with the entered code and toggles it", async () => {
  const verifiedChannel = { ...channel, verified: true, enabled: true };
  const fetcher = vi
    .fn()
    .mockResolvedValueOnce(json(200, { channels: [channel] }))
    .mockResolvedValueOnce(json(200, { verified: true }))
    .mockResolvedValueOnce(json(200, { channels: [verifiedChannel] }))
    .mockResolvedValueOnce(json(200, { ...verifiedChannel, enabled: false }))
    .mockResolvedValueOnce(
      json(200, { channels: [{ ...verifiedChannel, enabled: false }] }),
    );
  render(<ChannelManager fetcher={fetcher as unknown as typeof fetch} />);

  await waitFor(() =>
    expect(screen.getByText(/user@example\.com/)).toBeInTheDocument(),
  );

  fireEvent.change(screen.getByLabelText("验证码"), {
    target: { value: "222333" },
  });
  fireEvent.click(screen.getByRole("button", { name: "验证" }));

  await waitFor(() => {
    const [url, init] = fetcher.mock.calls[1];
    expect(url).toBe("/api/v1/settings/contact-channels/channel-1/verify");
    expect(JSON.parse(String(init?.body))).toEqual({ code: "222333" });
  });

  await waitFor(() =>
    expect(screen.getByRole("button", { name: "停用" })).toBeInTheDocument(),
  );
  fireEvent.click(screen.getByRole("button", { name: "停用" }));

  await waitFor(() => {
    const [toggleUrl, toggleInit] = fetcher.mock.calls[3];
    expect(toggleUrl).toBe("/api/v1/settings/contact-channels/channel-1");
    expect(JSON.parse(String(toggleInit?.body))).toEqual({ enabled: false });
  });
});
