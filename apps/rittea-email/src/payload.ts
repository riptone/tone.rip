import type { Email } from "postal-mime";

/**
 * The body rittea's `POST /api/email/entrada` expects.
 *
 * The field names are Portuguese because they are rittea's contract, not
 * this repo's: rittea is written in European Portuguese end to end, and a
 * translation layer here would be one more place for the two to drift.
 */
export interface EntradaPayload {
  /** Envelope recipient, lowercased - carries the ticket token. */
  para: string;
  /** Sender address, lowercased. */
  de: string;
  nome: string | null;
  assunto: string;
  /** Plain text, quoted replies included: rittea strips them, not this. */
  texto: string;
  messageId: string | null;
  /** Present only when the email had attachments; they are not sent. */
  anexos?: number;
}

/** rittea stores text, not documents; anything past this is cut. */
export const MAX_TEXT_CHARS = 20_000;

/**
 * Whether the local part of `to` carries a `+token` subaddress.
 *
 * `suporte@...` is mail for a human; `suporte+<token>@...` is a reply to a
 * ticket. An empty token (`suporte+@...`) is the former, since there is
 * nothing for rittea to look up.
 */
export function hasToken(to: string): boolean {
  const at = to.lastIndexOf("@");
  const local = at === -1 ? to : to.slice(0, at);
  const plus = local.indexOf("+");
  return plus !== -1 && plus < local.length - 1;
}

const SCRIPT_OR_STYLE = /<(script|style)\b[^>]*>[\s\S]*?<\/\1\s*>/gi;
const LINE_BREAK_TAG = /<br\s*\/?>|<\/(p|div|li|tr|h[1-6]|blockquote)\s*>/gi;
const ANY_TAG = /<[^>]*>/g;
const ENTITY = /&(amp|lt|gt|quot|#39|nbsp);/g;
const TRAILING_SPACES = /[ \t]+\n/g;
const BLANK_RUN = /\n{3,}/g;

const ENTITIES: Record<string, string> = {
  amp: "&",
  lt: "<",
  gt: ">",
  quot: '"',
  "#39": "'",
  nbsp: " ",
};

/**
 * A readable plain-text rendering of an HTML body, for the emails that
 * arrive without a text part. Not a sanitiser: rittea treats the result as
 * text. Entities are decoded in one pass, so `&amp;lt;` becomes `&lt;` and
 * not `<`.
 */
export function htmlToText(html: string): string {
  return html
    .replace(SCRIPT_OR_STYLE, "")
    .replace(LINE_BREAK_TAG, "\n")
    .replace(ANY_TAG, "")
    .replace(ENTITY, (_, name: string) => ENTITIES[name] ?? "")
    .replace(TRAILING_SPACES, "\n")
    .replace(BLANK_RUN, "\n\n")
    .trim();
}

/** The parts of the envelope the payload reads. */
type Envelope = Pick<ForwardableEmailMessage, "to" | "from">;

export function toPayload(parsed: Email, envelope: Envelope): EntradaPayload {
  const from = parsed.from;
  const text = parsed.text ?? htmlToText(parsed.html ?? "");
  const payload: EntradaPayload = {
    para: envelope.to.toLowerCase(),
    de: (from?.address || envelope.from).toLowerCase(),
    nome: from?.name || null,
    assunto: parsed.subject ?? "",
    texto: text.trim().slice(0, MAX_TEXT_CHARS),
    messageId: parsed.messageId ?? null,
  };
  if (parsed.attachments.length > 0) {
    payload.anexos = parsed.attachments.length;
  }
  return payload;
}
