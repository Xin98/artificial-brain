"use client";
import { useState } from "react";
import { useMutation, useResource } from "./hooks";
import { failureText, type InvestmentClient } from "./fetch-investment";
import { reasonText, runStateText } from "./status-badge";
import type { RunView, EvaluationReadView } from "./types";
export function EvaluationControl({
  client,
  accountId,
}: {
  client: InvestmentClient;
  accountId: string;
}) {
  const [id, setId] = useState("");
  const mutation = useMutation<RunView>(client, "RunView");
  return (
    <div className="investment-section">
      <h2>账户研究评估</h2>
      <p>
        使用账户绑定的股票池和策略版本进行研究评估；自动调仓由每日任务单独处理。
      </p>
      <button
        disabled={mutation.busy}
        onClick={async () => {
          const result = await mutation.submit(
            "POST",
            "/accounts/" + accountId + "/evaluations",
            { purpose: "research" },
          );
          if (result?.ok) setId(result.value.runId);
        }}
      >
        运行研究评估
      </button>
      {mutation.result && !mutation.result.ok ? (
        <p role="alert">{failureText(mutation.result.code)}</p>
      ) : null}
      {id ? (
        <EvaluationProgress client={client} accountId={accountId} id={id} />
      ) : null}
    </div>
  );
}
function EvaluationProgress({
  client,
  accountId,
  id,
}: {
  client: InvestmentClient;
  accountId: string;
  id: string;
}) {
  const { result } = useResource<EvaluationReadView>(
    client,
    "/accounts/" + accountId + "/evaluations/" + id,
    "EvaluationReadView",
    true,
  );
  return (
    <p role="status">
      {result?.ok
        ? [
            runStateText(result.value.state),
            result.value.reason ? reasonText(result.value.reason) : "",
            "订单 " + result.value.orderIds.length + " 笔",
          ]
            .filter(Boolean)
            .join(" · ")
        : result && !result.ok
          ? failureText(result.code)
          : "评估已排队"}
    </p>
  );
}
