import { auth } from "@/lib/auth";

export async function getSessionUser(headers: Headers) {
  const session = await auth.api.getSession({ headers });
  return session?.user ?? null;
}

export async function requireUser(headers: Headers) {
  const user = await getSessionUser(headers);
  if (!user) {
    throw new Response("Unauthorized", { status: 401 });
  }
  return user;
}
