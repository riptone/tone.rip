import { fetchWithTimeout } from "@repo/net";
import PostalMime from "postal-mime";
import { outcome } from "./outcome";
import { hasToken, toPayload } from "./payload";

/** Past this, the email skips rittea and goes straight to a human. */
const MAX_RAW_BYTES = 1024 * 1024;
const RITTEA_TIMEOUT_MS = 10_000;
const ENDPOINT = "/api/email/entrada";
const REJECT_REASON = "This message could not be delivered.";

/**
 * A one-line reason for the log. Only the error's own name and message:
 * nothing here may carry the secret, the body or an address.
 */
function describe(error: unknown): string {
  return error instanceof Error ? `${error.name}: ${error.message}` : "unknown";
}

/**
 * Hands the email to a human. With FORWARD_TO unset, or a forward that
 * fails (an unverified destination, say), the sender gets a bounce rather
 * than the email vanishing - the fallback Cloudflare's own docs use.
 */
async function forward(message: ForwardableEmailMessage, env: Env) {
  if (!env.FORWARD_TO) {
    console.warn("rittea-email: FORWARD_TO is not set, rejecting");
    message.setReject(REJECT_REASON);
    return;
  }
  try {
    await message.forward(env.FORWARD_TO);
  } catch (error) {
    console.warn(
      `rittea-email: forward failed (${describe(error)}), rejecting`,
    );
    message.setReject(REJECT_REASON);
  }
}

/**
 * Posts the email to rittea. Returns null when rittea took it, or the
 * reason to forward it instead - empty for a 202, which is rittea working
 * as intended and not worth a log line.
 */
async function sendToRittea(
  message: ForwardableEmailMessage,
  env: Env,
): Promise<string | null> {
  try {
    const parsed = await PostalMime.parse(message.raw);
    const response = await fetchWithTimeout(
      new URL(ENDPOINT, env.RITTEA_URL),
      RITTEA_TIMEOUT_MS,
      {
        method: "POST",
        headers: {
          Authorization: `Bearer ${env.EMAIL_ENTRADA_SEGREDO}`,
          "Content-Type": "application/json",
        },
        body: JSON.stringify(toPayload(parsed, message)),
      },
    );
    await response.body?.cancel();
    const result = outcome(response.status);
    if (result === "delivered") return null;
    if (result === "ignored") return "";
    return `rittea answered ${response.status}`;
  } catch (error) {
    return describe(error);
  }
}

export default {
  async email(message, env) {
    if (!hasToken(message.to.toLowerCase())) {
      await forward(message, env);
      return;
    }
    if (message.rawSize > MAX_RAW_BYTES) {
      console.warn(`rittea-email: ${message.rawSize} bytes, over the limit`);
      await forward(message, env);
      return;
    }
    const reason = await sendToRittea(message, env);
    if (reason === null) return;
    if (reason) console.warn(`rittea-email: ${reason}, forwarding`);
    await forward(message, env);
  },
} satisfies ExportedHandler<Env>;
