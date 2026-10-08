import Link from "next/link";
const tabs = [
  { id: "research", href: "/investment", label: "股票研究" },
  { id: "accounts", href: "/investment/accounts", label: "模拟账户" },
  { id: "backtests", href: "/investment/research", label: "回测实验" },
] as const;
export function InvestmentTabs({
  current,
}: {
  current: (typeof tabs)[number]["id"];
}) {
  return (
    <nav className="investment-tabs" aria-label="投资模块">
      {tabs.map((t) => (
        <Link
          key={t.id}
          href={t.href}
          aria-current={current === t.id ? "page" : undefined}
        >
          {t.label}
        </Link>
      ))}
    </nav>
  );
}
