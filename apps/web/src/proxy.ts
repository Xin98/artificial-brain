import { NextRequest, NextResponse } from "next/server";

export function proxy(request: NextRequest): NextResponse {
  const headers = new Headers(request.headers);
  headers.set(
    "x-ab-return-to",
    `${request.nextUrl.pathname}${request.nextUrl.search}`,
  );
  return NextResponse.next({ request: { headers } });
}

export const config = {
  matcher: ["/", "/todos", "/conversation", "/settings", "/data"],
};
