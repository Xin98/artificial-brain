import { recoverExpiredSession } from "../auth/session-recovery";
import { matches } from "./schema";
import { investmentSchemas } from "./schemas";
export type Outcome<T> =
  { ok: true; value: T } | { ok: false; code: string; correlationId?: string };
export type Decoder<T> = (value: unknown) => T | null;
export interface InvestmentClient {
  request<T>(
    path: string,
    options: RequestInit,
    decode: Decoder<T>,
  ): Promise<Outcome<T>>;
}
export function decode<T>(schema: string): Decoder<T> {
  return (value) =>
    matches(
      value,
      investmentSchemas[schema] ?? { type: "never" },
      investmentSchemas,
    )
      ? (value as T)
      : null;
}
export function createInvestmentClient(
  baseURL = "",
  fetcher: typeof fetch = fetch,
  navigate?: (path: string) => void,
): InvestmentClient {
  if (baseURL !== "" && baseURL !== "/")
    throw new Error("investment requires same-origin transport");
  return {
    async request<T>(
      path: string,
      options: RequestInit,
      decoder: Decoder<T>,
    ): Promise<Outcome<T>> {
      if (!/^\/[a-zA-Z0-9/?&=%,._-]*$/.test(path) || path.includes(".."))
        return { ok: false, code: "invalid_request" };
      const controller = new AbortController();
      const relay = () => controller.abort();
      options.signal?.addEventListener("abort", relay, { once: true });
      if (options.signal?.aborted) controller.abort();
      const timer = setTimeout(() => controller.abort(), 15_000);
      try {
        const response = await fetcher("/api/v1/investment" + path, {
          ...options,
          headers: { "Content-Type": "application/json", ...options.headers },
          credentials: "same-origin",
          cache: "no-store",
          signal: controller.signal,
        });
        if (!response.ok) {
          recoverExpiredSession(response, navigate);
          let code =
            response.status === 503
              ? "service_unavailable"
              : response.status === 401
                ? "unauthenticated"
                : response.status === 409
                  ? "conflict"
                  : "rejected";
          let correlationId: string | undefined;
          try {
            const body: unknown = await response.json();
            if (
              body &&
              typeof body === "object" &&
              "code" in body &&
              typeof body.code === "string"
            ) {
              code = body.code;
              if (
                "correlationId" in body &&
                typeof body.correlationId === "string"
              )
                correlationId = body.correlationId;
            }
          } catch {}
          return { ok: false, code, correlationId };
        }
        const v = decoder(await response.json());
        return v === null
          ? { ok: false, code: "invalid_response" }
          : { ok: true, value: v };
      } catch {
        return {
          ok: false,
          code: controller.signal.aborted ? "timeout" : "network",
        };
      } finally {
        clearTimeout(timer);
        options.signal?.removeEventListener("abort", relay);
      }
    },
  };
}
export const investmentClient = createInvestmentClient();
export interface Intent {
  path: string;
  options: RequestInit;
  signature: string;
}
export function createIntent(
  method: string,
  path: string,
  body: unknown,
): Intent {
  const text = JSON.stringify(body);
  return {
    path,
    signature: method + path + text,
    options: {
      method,
      body: text,
      headers: { "Idempotency-Key": crypto.randomUUID() },
    },
  };
}
export function failureText(code: string): string {
  const messages: Record<string, string> = {
    network: "网络连接失败，输入已保留，请重试。",
    timeout: "请求结果尚未确认，请用原请求重试。",
    service_unavailable: "服务暂不可用，输入已保留，请稍后重试。",
    conflict: "版本已变化或请求冲突，请刷新并检查配置后重新提交。",
    version_conflict: "版本已变化，请刷新并检查配置后重新提交。",
    invalid_response: "响应格式不完整，请稍后重试。",
    unauthenticated: "登录已失效，正在返回登录页。",
    data_stale: "行情或财报数据尚未齐备，请稍后重试。",
    data_not_configured: "数据源未配置，请先完成数据配置。",
    insufficient_cash: "可用已结算资金不足。",
    risk_limit_exceeded:
      "触发风控上限（单股10%/同行业30%/总仓位80%/当日换手20%/回撤15%暂停/止损10%）；股票详情页可查看当前最多可买数量。",
    insufficient_universe: "满足条件的候选证券不足。",
    factor_unavailable: "因子数据不可用，无法评分。",
    corporate_action_incomplete: "公司行动数据不完整，已阻断交易。",
    automation_paused: "自动交易已暂停。",
    order_not_cancellable: "订单已进入生效时段，不能撤销。",
    idempotency_conflict: "相同请求键携带了不同内容，请检查后重试。",
    not_found: "资源不存在或不属于当前账户。",
    invalid_input: "输入不合法，请检查后重试。",
  };
  return messages[code] ?? "请求未完成：" + code;
}
