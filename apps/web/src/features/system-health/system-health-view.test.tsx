import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, test } from "vitest";

import type { HealthStatus, SystemHealthReport } from "./types";
import { SystemHealthView } from "./system-health-view";

function reportWithStatus(status: HealthStatus): SystemHealthReport {
  const checkedAt = "2026-08-13T04:00:00Z";

  return {
    status,
    checkedAt,
    correlationId: "req-1",
    components: {
      api: {
        status: status === "unavailable" ? "unavailable" : "healthy",
        checkedAt,
      },
      database: { status: "healthy", checkedAt },
      worker: {
        status: status === "degraded" ? "unavailable" : "healthy",
        checkedAt,
      },
    },
  };
}

describe("SystemHealthView", () => {
  afterEach(cleanup);

  test.each([
    ["healthy", "所有系统运行正常"],
    ["degraded", "部分系统需要关注"],
    ["unavailable", "健康状态暂不可用"],
  ] as const)("renders %s status", (status, heading) => {
    render(<SystemHealthView report={reportWithStatus(status)} />);

    expect(screen.getByRole("heading", { name: heading })).toBeInTheDocument();
    for (const name of ["Web", "API", "PostgreSQL", "Worker"]) {
      expect(screen.getByText(name)).toBeInTheDocument();
    }
  });

  test("does not render internal connection details", () => {
    render(
      <SystemHealthView
        report={{
          ...reportWithStatus("degraded"),
          components: {
            ...reportWithStatus("degraded").components,
            api: {
              status: "unavailable",
              checkedAt: "2026-08-13T04:00:00Z",
              detail: "http://api.internal:8080 stack trace ECONNREFUSED",
            },
          },
        }}
      />,
    );

    expect(
      screen.queryByText(/api\.internal|stack trace|ECONNREFUSED/i),
    ).not.toBeInTheDocument();
  });
});
