import PostalMime from "postal-mime";
import { describe, expect, it } from "vitest";
import {
  hasToken,
  htmlToText,
  MAX_TEXT_CHARS,
  toPayload,
} from "../src/payload";
import { emailWithAttachments, htmlEmail, textEmail } from "./mime";

const envelope = {
  to: "Suporte+ABC123@Rittea.Tone.Rip",
  from: "Bounce@Example.pt",
};

describe("hasToken", () => {
  it.each([
    "suporte+abc123@rittea.tone.rip",
    "suporte+a@rittea.tone.rip",
    "suporte+a+b@rittea.tone.rip",
  ])("is true for %s", (to) => {
    expect(hasToken(to)).toBe(true);
  });

  it.each(["suporte@rittea.tone.rip", "suporte+@rittea.tone.rip", "suporte"])(
    "is false for %s",
    (to) => {
      expect(hasToken(to)).toBe(false);
    },
  );

  it("only looks at the local part", () => {
    expect(hasToken("suporte@a+b.example.pt")).toBe(false);
  });
});

describe("htmlToText", () => {
  it("strips tags and keeps line breaks", () => {
    expect(htmlToText("<p>Olá</p><p>Segunda<br>linha</p>")).toBe(
      "Olá\nSegunda\nlinha",
    );
  });

  it("drops scripts and styles with their content", () => {
    expect(
      htmlToText("<style>p{color:red}</style><p>Texto</p><script>x()</script>"),
    ).toBe("Texto");
  });

  it("decodes the common entities once", () => {
    expect(
      htmlToText(
        "a &amp; b &lt;c&gt; &quot;d&quot; &#39;e&#39;&nbsp;f &amp;lt;",
      ),
    ).toBe("a & b <c> \"d\" 'e' f &lt;");
  });

  it("collapses runs of blank lines", () => {
    expect(htmlToText("<p>a</p>\n\n\n<p>b</p>")).toBe("a\n\nb");
  });
});

describe("toPayload", () => {
  it("builds the payload from a text email", async () => {
    const parsed = await PostalMime.parse(textEmail("Obrigado, já resolvi."));
    expect(toPayload(parsed, envelope)).toEqual({
      para: "suporte+abc123@rittea.tone.rip",
      de: "ana.cliente@example.pt",
      nome: "Ana Cliente",
      assunto: "Re: Pedido de ajuda",
      texto: "Obrigado, já resolvi.",
      messageId: "<resposta-1@example.pt>",
    });
  });

  it("renders the html when there is no text part", async () => {
    const parsed = await PostalMime.parse(
      htmlEmail("<div>Bom dia &amp; obrigado</div>"),
    );
    expect(toPayload(parsed, envelope).texto).toBe("Bom dia & obrigado");
  });

  it("falls back to the envelope sender and nulls", async () => {
    const parsed = await PostalMime.parse("\r\n\r\n");
    expect(toPayload(parsed, envelope)).toEqual({
      para: "suporte+abc123@rittea.tone.rip",
      de: "bounce@example.pt",
      nome: null,
      assunto: "",
      texto: "",
      messageId: null,
    });
  });

  it("uses null for a sender without a display name", async () => {
    const parsed = await PostalMime.parse(
      textEmail("Olá", { from: "ana@example.pt" }),
    );
    expect(toPayload(parsed, envelope).nome).toBeNull();
  });

  it("truncates long text", async () => {
    const parsed = await PostalMime.parse(textEmail("a".repeat(30_000)));
    expect(toPayload(parsed, envelope).texto).toHaveLength(MAX_TEXT_CHARS);
  });

  it("counts attachments without sending them", async () => {
    const parsed = await PostalMime.parse(emailWithAttachments("Segue.", 2));
    const payload = toPayload(parsed, envelope);
    expect(payload.anexos).toBe(2);
    expect(payload.texto).toBe("Segue.");
  });

  it("leaves anexos out when there are none", async () => {
    const parsed = await PostalMime.parse(textEmail("Olá"));
    expect(toPayload(parsed, envelope)).not.toHaveProperty("anexos");
  });
});
