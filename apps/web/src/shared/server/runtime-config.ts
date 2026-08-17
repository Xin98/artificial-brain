import "server-only";

const defaultAPIInternalURL = "http://localhost:8080";

export function apiInternalURL(): string {
  const value = process.env.API_INTERNAL_URL ?? defaultAPIInternalURL;
  const url = new URL(value);

  if (url.protocol !== "http:" && url.protocol !== "https:") {
    throw new Error("API_INTERNAL_URL must be an absolute HTTP(S) URL");
  }

  return url.toString().replace(/\/$/, "");
}
