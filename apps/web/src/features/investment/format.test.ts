import { expect, it } from "vitest";
import { groupMoney, localTime } from "./format";
it("localTime hides the year inside the current year and shows it otherwise", () => {
  const year = new Date().getFullYear();
  expect(localTime(year + "-03-05T08:30:00Z")).not.toContain(String(year));
  expect(localTime(year - 1 + "-03-05T08:30:00Z")).toContain(String(year - 1));
  expect(localTime("not-a-date")).toBe("not-a-date");
});
it("groupMoney adds thousands separators to two-decimal money only", () => {
  expect(groupMoney("1000000.00")).toBe("1,000,000.00");
  expect(groupMoney("90000.00")).toBe("90,000.00");
  expect(groupMoney("-2100.00")).toBe("-2,100.00");
  expect(groupMoney("100.000000")).toBe("100.000000");
  expect(groupMoney("等待完整估值")).toBe("等待完整估值");
});
