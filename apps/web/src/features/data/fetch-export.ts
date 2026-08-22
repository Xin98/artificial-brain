import { safeTimeout } from "../validation";

// exportBundle POSTs to the portability export route and returns the
// workspace's export bundle as a zip blob. Any non-2xx answer or network
// failure fails closed to null — a half-downloaded bundle is never surfaced.
export async function exportBundle(
  baseURL: string,
  fetcher: typeof fetch,
  timeoutMs = 30000,
): Promise<Blob | null> {
  try {
    const response = await fetcher(`${baseURL}/api/v1/portability/export`, {
      method: "POST",
      signal: AbortSignal.timeout(safeTimeout(timeoutMs)),
      cache: "no-store",
      headers: { accept: "application/zip" },
    });
    if (!response.ok) {
      return null;
    }
    return await response.blob();
  } catch {
    return null;
  }
}
