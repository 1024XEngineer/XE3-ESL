import { getPortalEnv } from "./portal-db";

const rejectedAdminPasswords = new Set([
  "replace-with-a-random-password",
  "请替换为至少12位的随机密码",
]);

async function digest(value: string): Promise<ArrayBuffer> {
  return crypto.subtle.digest("SHA-256", new TextEncoder().encode(value));
}

function equalBytes(left: ArrayBuffer, right: ArrayBuffer): boolean {
  const a = new Uint8Array(left);
  const b = new Uint8Array(right);
  if (a.length !== b.length) return false;
  let difference = 0;
  for (let index = 0; index < a.length; index += 1) {
    difference |= a[index] ^ b[index];
  }
  return difference === 0;
}

export async function isAdminRequest(request: Request): Promise<boolean> {
  const configuredPassword = (await getPortalEnv()).PORTAL_ADMIN_PASSWORD ?? "";
  const providedPassword = request.headers.get("x-portal-admin-password") ?? "";
  if (
    configuredPassword.length < 12 ||
    rejectedAdminPasswords.has(configuredPassword) ||
    !providedPassword
  ) {
    return false;
  }
  return equalBytes(await digest(configuredPassword), await digest(providedPassword));
}
