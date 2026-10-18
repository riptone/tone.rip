/* Synthetic MIME emails for the tests. example.pt addresses only. */

const CRLF = "\r\n";

interface MimeOptions {
  from?: string;
  subject?: string;
  messageId?: string;
}

function headers({
  from = '"Ana Cliente" <Ana.Cliente@Example.pt>',
  subject = "Re: Pedido de ajuda",
  messageId = "<resposta-1@example.pt>",
}: MimeOptions): string[] {
  return [
    `From: ${from}`,
    "To: suporte+abc123@rittea.tone.rip",
    `Subject: ${subject}`,
    `Message-ID: ${messageId}`,
    "Date: Sun, 18 Oct 2026 10:00:00 +0100",
    "MIME-Version: 1.0",
  ];
}

export function textEmail(text: string, options: MimeOptions = {}): string {
  return [
    ...headers(options),
    "Content-Type: text/plain; charset=utf-8",
    "",
    text,
  ].join(CRLF);
}

export function htmlEmail(html: string, options: MimeOptions = {}): string {
  return [
    ...headers(options),
    "Content-Type: text/html; charset=utf-8",
    "",
    html,
  ].join(CRLF);
}

export function emailWithAttachments(text: string, count: number): string {
  const boundary = "fronteira";
  const parts = [`--${boundary}`, "Content-Type: text/plain", "", text];
  for (let i = 0; i < count; i++) {
    parts.push(
      `--${boundary}`,
      "Content-Type: application/pdf",
      `Content-Disposition: attachment; filename="anexo-${i}.pdf"`,
      "Content-Transfer-Encoding: base64",
      "",
      "JVBERi0xLjQK",
    );
  }
  parts.push(`--${boundary}--`, "");
  return [
    ...headers({}),
    `Content-Type: multipart/mixed; boundary="${boundary}"`,
    "",
    ...parts,
  ].join(CRLF);
}
