"use client";
import { useCallback, useEffect, useRef, useState } from "react";
import {
  createIntent,
  decode,
  type InvestmentClient,
  type Intent,
  type Outcome,
} from "./fetch-investment";
export function useResource<T>(
  client: InvestmentClient,
  path: string,
  schema: string,
  poll = false,
) {
  const [revision, setRevision] = useState(0);
  const [state, setState] = useState<{
    key: string;
    result: Outcome<T>;
  } | null>(null);
  const key = path + "|" + schema;
  const retry = useCallback(() => setRevision((x) => x + 1), []);
  useEffect(() => {
    let live = true;
    let generation = 0;
    let timer: ReturnType<typeof setTimeout> | undefined;
    let controller = new AbortController();
    const load = async () => {
      const requestGeneration = ++generation;
      const result = await client.request<T>(
        path,
        { signal: controller.signal },
        decode<T>(schema),
      );
      if (live && requestGeneration === generation) {
        setState({ key, result });
        if (poll && document.visibilityState === "visible")
          timer = setTimeout(() => void load(), 5000);
      }
    };
    const visible = () => {
      generation++;
      if (timer) clearTimeout(timer);
      if (document.visibilityState === "hidden") {
        if (timer) clearTimeout(timer);
        controller.abort();
      } else {
        controller = new AbortController();
        void load();
      }
    };
    if (!poll || document.visibilityState === "visible") void load();
    if (poll) document.addEventListener("visibilitychange", visible);
    return () => {
      live = false;
      generation++;
      controller.abort();
      if (timer) clearTimeout(timer);
      if (poll) document.removeEventListener("visibilitychange", visible);
    };
  }, [client, key, path, schema, revision, poll]);
  return { result: state?.key === key ? state.result : null, retry };
}
export function useMutation<T>(client: InvestmentClient, schema: string) {
  const intent = useRef<Intent | null>(null);
  const active = useRef(false);
  const live = useRef(true);
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState<Outcome<T> | null>(null);
  useEffect(() => {
    live.current = true;
    return () => {
      live.current = false;
    };
  }, []);
  const submit = async (
    method: string,
    path: string,
    body: unknown,
  ): Promise<Outcome<T> | null> => {
    if (active.current) return null;
    const signature = method + path + JSON.stringify(body);
    if (intent.current?.signature !== signature)
      intent.current = createIntent(method, path, body);
    const request = intent.current;
    active.current = true;
    setBusy(true);
    const next = await client.request<T>(
      request.path,
      request.options,
      decode<T>(schema),
    );
    active.current = false;
    if (!live.current) return null;
    setBusy(false);
    setResult(next);
    if (next.ok) intent.current = null;
    return next;
  };
  return { submit, busy, result };
}
