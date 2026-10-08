"use client";
import Link from "next/link";
import { useState } from "react";
import {
  failureText,
  investmentClient,
  type InvestmentClient,
} from "./fetch-investment";
import { useCursorPager, useResource } from "./hooks";
import { groupMoney, localTime } from "./format";
import {
  automationEventText,
  ledgerKindText,
  reasonText,
  runStateText,
  SourceNotice,
} from "./status-badge";
import { AccountForm } from "./account-form";
import { AutomationForm } from "./automation-form";
import { OrderForm, OrderRow } from "./order-form";
import { EvaluationControl } from "./evaluation-control";
import { PerformancePanel, PositionDistribution } from "./performance-panel";
import { InvestmentTabs } from "./tabs";
import type {
  AccountView,
  AccountsPage,
  OrderView,
  LedgerEntry,
  Position,
  EvaluationReadView,
  AutomationEventView,
} from "./types";
export function AccountDirectory({
  client = investmentClient,
  prefillInstrumentId = "",
}: {
  client?: InvestmentClient;
  prefillInstrumentId?: string;
}) {
  const [created, setCreated] = useState<AccountView | null>(null);
  const pager = useCursorPager();
  const { result, retry } = useResource<AccountsPage>(
    client,
    "/accounts?cursor=" + encodeURIComponent(pager.cursor),
    "AccountsPage",
  );
  return (
    <div className="investment-layout">
      <InvestmentTabs current="accounts" />
      <AccountForm
        client={client}
        onCreated={(a) => {
          setCreated(a);
          retry();
        }}
      />
      {created ? (
        <p role="status">
          账户已创建：
          <Link href={"/investment/accounts/" + created.id}>
            打开{created.name || "模拟账户"}
          </Link>
        </p>
      ) : null}
      <section>
        <h2>我的模拟账户</h2>
        {!result ? (
          <p role="status">正在加载账户…</p>
        ) : !result.ok ? (
          <p role="alert">
            {failureText(result.code)}{" "}
            <button onClick={retry}>重试账户列表</button>
          </p>
        ) : (
          <>
            <div className="investment-table-wrap">
              <table className="investment-table">
                <thead>
                  <tr>
                    <th>账户</th>
                    <th>数据模式</th>
                    <th>可用现金 USD</th>
                    <th>自动交易</th>
                  </tr>
                </thead>
                <tbody>
                  {result.value.items.map((a) => (
                    <tr key={a.id}>
                      <td>
                        <Link href={"/investment/accounts/" + a.id}>
                          {a.name || "模拟账户"}
                        </Link>
                        {prefillInstrumentId ? (
                          <small>
                            <Link
                              href={
                                "/investment/accounts/" +
                                a.id +
                                "?instrument=" +
                                encodeURIComponent(prefillInstrumentId)
                              }
                            >
                              下单
                            </Link>
                          </small>
                        ) : null}
                      </td>
                      <td>
                        {a.mode === "fixture" ? "演示数据" : "真实只读数据"}
                      </td>
                      <td>{groupMoney(a.cash.available)}</td>
                      <td>{a.automationEnabled ? "已启用" : "已暂停"}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            {result.value.items.length === 0 ? (
              <p>还没有模拟账户，可用上方表单创建。</p>
            ) : null}
            {result.value.nextCursor ? (
              <button onClick={() => pager.next(result.value.nextCursor)}>
                下一页账户
              </button>
            ) : null}
            {pager.canPrev ? (
              <button onClick={pager.prev}>上一页账户</button>
            ) : null}
            {pager.canPrev ? (
              <button onClick={pager.reset}>返回账户首屏</button>
            ) : null}
          </>
        )}
      </section>
    </div>
  );
}
export function AccountPanel({
  client = investmentClient,
  accountId,
  prefillInstrumentId = "",
  tab = "",
}: {
  client?: InvestmentClient;
  accountId: string;
  prefillInstrumentId?: string;
  tab?: string;
}) {
  return (
    <AccountDetail
      key={accountId}
      client={client}
      accountId={accountId}
      prefillInstrumentId={prefillInstrumentId}
      tab={tab}
    />
  );
}
const ACCOUNT_TABS = [
  { id: "trade", label: "交易" },
  { id: "automation", label: "自动化" },
  { id: "positions", label: "持仓与绩效" },
  { id: "records", label: "记录" },
] as const;
type AccountTabId = (typeof ACCOUNT_TABS)[number]["id"];
function AccountDetail({
  client,
  accountId,
  prefillInstrumentId,
  tab,
}: {
  client: InvestmentClient;
  accountId: string;
  prefillInstrumentId: string;
  tab: string;
}) {
  const { result, lastGood, retry } = useResource<AccountView>(
    client,
    "/accounts/" + accountId,
    "AccountView",
    true,
  );
  if (!result) return <p role="status">正在读取账户…</p>;
  if (!result.ok && !lastGood)
    return (
      <p role="alert">
        {failureText(result.code)} <button onClick={retry}>重试账户</button>
      </p>
    );
  const a = result.ok ? result.value : lastGood!;
  const active: AccountTabId = ACCOUNT_TABS.some((t) => t.id === tab)
    ? (tab as AccountTabId)
    : "trade";
  const tabHref = (id: AccountTabId) =>
    "?tab=" +
    id +
    (prefillInstrumentId
      ? "&instrument=" + encodeURIComponent(prefillInstrumentId)
      : "");
  return (
    <div className="investment-layout">
      <InvestmentTabs current="accounts" />
      <Link href="/investment/accounts">返回我的模拟账户</Link>
      {!result.ok ? (
        <p role="alert">
          {failureText(result.code)}{" "}
          当前显示上次成功读取的账户，金额可能已变化。
          <button onClick={retry}>重试账户</button>
        </p>
      ) : null}
      <SourceNotice data={a} />
      <header>
        <h1>{a.name || "模拟账户"}</h1>
        <p>
          净资产 USD：
          <strong>{a.nav ? groupMoney(a.nav) : "等待完整估值"}</strong> ·
          数据截止 {a.asOf}
        </p>
        <p>
          <Link className="btn-primary" href={tabHref("trade")}>
            下单
          </Link>
        </p>
      </header>
      <dl className="investment-cash">
        {Object.entries({
          available: "可用现金",
          reserved: "订单预留",
          unsettled: "未结算卖出款",
          dividends: "应收股息",
        }).map(([key, label]) => (
          <div key={key}>
            <dt>{label}</dt>
            <dd>{groupMoney(a.cash[key as keyof AccountView["cash"]])}</dd>
          </div>
        ))}
      </dl>
      {a.blockReasons.length ? (
        <p role="status">{a.blockReasons.map(reasonText).join(" · ")}</p>
      ) : null}
      <p>
        暂停后仍可能补记已生效订单；最新净资产等待相关日线确认，不会把缺失持仓价格当作零。
      </p>
      <nav className="investment-tabs" aria-label="账户区块">
        {ACCOUNT_TABS.map((t) => (
          <Link
            key={t.id}
            href={tabHref(t.id)}
            aria-current={active === t.id ? "page" : undefined}
          >
            {t.label}
          </Link>
        ))}
      </nav>
      <div hidden={active !== "trade"}>
        <section className="investment-section">
          <OrderForm
            key={prefillInstrumentId || "manual"}
            client={client}
            accountId={accountId}
            account={a}
            prefillInstrumentId={prefillInstrumentId || undefined}
            onSubmitted={retry}
          />
        </section>
        <PagedTable<OrderView>
          key={"orders/" + a.version}
          client={client}
          path={"/accounts/" + accountId + "/orders"}
          schema="OrdersPage"
          title="订单"
          headers={["证券", "状态", "目标开盘", "成交详情 USD", "操作"]}
          row={(o) => (
            <OrderRow
              key={o.id}
              client={client}
              accountId={accountId}
              order={o}
              onChanged={retry}
            />
          )}
        />
      </div>
      <div hidden={active !== "automation"}>
        <section className="investment-section">
          <AutomationForm client={client} account={a} onChanged={retry} />
        </section>
        <EvaluationControl client={client} accountId={accountId} />
        <PagedTable<EvaluationReadView>
          client={client}
          path={"/accounts/" + accountId + "/evaluations"}
          schema="EvaluationsPage"
          title="评估记录"
          headers={["会话日期", "用途", "状态", "订单批次", "原因"]}
          row={(v) => (
            <tr key={v.id}>
              <td>{v.sessionDate}</td>
              <td>{v.purpose === "automatic" ? "自动调仓" : "研究评估"}</td>
              <td>{runStateText(v.state)}</td>
              <td>{v.orderIds.length}</td>
              <td>{reasonText(v.reason)}</td>
            </tr>
          )}
        />
        <PagedTable<AutomationEventView>
          client={client}
          path={"/accounts/" + accountId + "/automation-events"}
          schema="EventsPage"
          title="自动交易审计"
          headers={["事件", "原因", "经济时间", "记录时间"]}
          row={(v) => (
            <tr key={v.id}>
              <td>{automationEventText(v.kind)}</td>
              <td>{reasonText(v.reason)}</td>
              <td>
                <time dateTime={v.effectiveAt} title={v.effectiveAt}>
                  {localTime(v.effectiveAt)}
                </time>
              </td>
              <td>
                <time dateTime={v.recordedAt} title={v.recordedAt}>
                  {localTime(v.recordedAt)}
                </time>
              </td>
            </tr>
          )}
        />
      </div>
      <div hidden={active !== "positions"}>
        <PagedTable<Position>
          client={client}
          path={"/accounts/" + accountId + "/positions"}
          schema="PositionsPage"
          title="持仓"
          headers={["证券", "整数股数", "预留股数", "剩余成本 USD"]}
          row={(p) => (
            <tr key={p.instrumentId}>
              <td>
                <Link
                  href={
                    "/investment/stocks/" +
                    encodeURIComponent(p.instrumentId) +
                    "?accountId=" +
                    accountId
                  }
                >
                  {p.instrumentId}
                </Link>
                <small>{p.industry}</small>
              </td>
              <td>{p.quantity}</td>
              <td>{p.reservedQuantity}</td>
              <td>{groupMoney(p.costBasis)}</td>
            </tr>
          )}
        />
        <PositionDistribution client={client} accountId={accountId} />
        <PerformancePanel client={client} resource="account" id={accountId} />
      </div>
      <div hidden={active !== "records"}>
        <PagedTable<LedgerEntry>
          client={client}
          path={"/accounts/" + accountId + "/ledger"}
          schema="LedgerPage"
          title="资金流水"
          headers={[
            "事件",
            "可用变化",
            "预留变化",
            "未结算变化",
            "股息变化",
            "经济/记录时间",
          ]}
          row={(l) => (
            <tr key={l.id}>
              <td>
                {ledgerKindText(l.kind)}
                <small>{l.instrumentId}</small>
              </td>
              <td>{groupMoney(l.delta.available)}</td>
              <td>{groupMoney(l.delta.reserved)}</td>
              <td>{groupMoney(l.delta.unsettled)}</td>
              <td>{groupMoney(l.delta.dividends)}</td>
              <td>
                <time dateTime={l.effectiveAt} title={l.effectiveAt}>
                  {localTime(l.effectiveAt)}
                </time>
                <small>
                  <time dateTime={l.recordedAt} title={l.recordedAt}>
                    {localTime(l.recordedAt)}
                  </time>
                </small>
              </td>
            </tr>
          )}
        />
      </div>
    </div>
  );
}
export function PagedTable<T>({
  client,
  path,
  schema,
  title,
  headers,
  row,
}: {
  client: InvestmentClient;
  path: string;
  schema: string;
  title: string;
  headers: string[];
  row: (v: T) => React.ReactNode;
}) {
  const pager = useCursorPager();
  const { result, retry } = useResource<{ items: T[]; nextCursor: string }>(
    client,
    path + "?cursor=" + encodeURIComponent(pager.cursor),
    schema,
    !pager.canPrev,
  );
  return (
    <section className="investment-section">
      <h2>{title}</h2>
      {!result ? (
        <p role="status">正在读取{title}…</p>
      ) : !result.ok ? (
        <p role="alert">
          {failureText(result.code)}{" "}
          <button onClick={retry}>重试{title}</button>
        </p>
      ) : (
        <>
          <div className="investment-table-wrap">
            <table className="investment-table">
              <caption>{title}，按最新记录分页，每页 25 条。</caption>
              <thead>
                <tr>
                  {headers.map((h) => (
                    <th key={h}>{h}</th>
                  ))}
                </tr>
              </thead>
              <tbody>{result.value.items.map(row)}</tbody>
            </table>
          </div>
          {result.value.items.length === 0 ? <p>暂无{title}记录。</p> : null}
          <div className="investment-toolbar">
            {pager.canPrev ? (
              <button onClick={pager.prev}>上一页{title}</button>
            ) : null}
            {result.value.nextCursor ? (
              <button onClick={() => pager.next(result.value.nextCursor)}>
                下一页{title}
              </button>
            ) : null}
            {pager.canPrev ? (
              <button onClick={pager.reset}>返回{title}首屏</button>
            ) : null}
          </div>
        </>
      )}
    </section>
  );
}
