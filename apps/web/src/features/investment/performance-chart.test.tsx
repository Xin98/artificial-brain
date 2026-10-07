import { render, screen } from "@testing-library/react";
import { expect, it } from "vitest";
import { PerformanceChart } from "./performance-chart";
it("keeps the synthetic data label beside the plotted result", () => {
  render(
    <PerformanceChart
      title="净资产"
      sourceLabel="演示数据 · synthetic"
      series={[{ sessionDate: "2026-10-01T00:00:00Z", nav: "100000.00" }]}
    />,
  );
  expect(screen.getByText(/演示数据 · synthetic/)).toBeVisible();
});
it.each([
  { series: [] },
  { series: [{ sessionDate: "2026-10-01T00:00:00Z", nav: "100000.00" }] },
  {
    series: [
      { sessionDate: "2026-10-01T00:00:00Z", nav: "100000.00" },
      { sessionDate: "2026-10-02T00:00:00Z", nav: "100000.00" },
    ],
  },
])("empty single point and constant curves render safely", ({ series }) => {
  const { container } = render(
    <PerformanceChart title="净资产" series={series} />,
  );
  expect(container.innerHTML).not.toContain("NaN");
  expect(container.innerHTML).not.toContain("Infinity");
  if (series.length === 0) expect(screen.getByText(/暂无已确认/)).toBeVisible();
  else expect(screen.getByRole("img", { name: "净资产" })).toBeVisible();
});
it("rejects incomplete series instead of drawing a fake curve", () => {
  render(
    <PerformanceChart
      title="净资产"
      series={[{ sessionDate: "bad", nav: "not-money" }]}
    />,
  );
  expect(screen.queryByRole("img")).toBeNull();
  expect(screen.getByText(/不完整/)).toBeVisible();
});
