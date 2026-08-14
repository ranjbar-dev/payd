import { invalidateSession, verifyCsrf, verifySession } from "@/lib/session";

function cookie(request: Request, name: string): string | undefined {
  return request.headers.get("cookie")?.split(";").map((item) => item.trim()).find((item) => item.startsWith(`${name}=`))?.slice(name.length + 1);
}

export async function POST(request: Request): Promise<Response> {
  const session = verifySession(cookie(request, "payd_session"));
  if (!session || !verifyCsrf(session, cookie(request, "payd_csrf"), request.headers.get("x-csrf-token"))) {
    return Response.json({ error: { code: "unauthorized", message: "Unauthorized" } }, { status: 401 });
  }
  invalidateSession(session.id);
  console.info(JSON.stringify({ timestamp: new Date().toISOString(), action: "logout", target: "session", outcome: "success" }));
  const response = Response.json({ ok: true });
  response.headers.append("Set-Cookie", "payd_session=; Path=/; Max-Age=0; HttpOnly; Secure; SameSite=Strict");
  response.headers.append("Set-Cookie", "payd_csrf=; Path=/; Max-Age=0; Secure; SameSite=Strict");
  return response;
}
