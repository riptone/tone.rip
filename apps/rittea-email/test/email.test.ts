import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import worker from "../src/index";
import { htmlEmail, textEmail } from "./mime";

const SECRET = "segredo-de-teste";

const env: Env = {
  RITTEA_URL: "https://rittea.tone.rip",
  EMAIL_ENTRADA_SEGREDO: SECRET,
  FORWARD_TO: "humano@example.pt",
};

function fakeMessage(
  raw: string,
  to = "Suporte+abc123@rittea.tone.rip",
  rawSize = raw.length,
) {
  const forward = vi.fn(async () => ({ messageId: "x" }));
  const setReject = vi.fn();
  const message = {
    from: "ana.cliente@example.pt",
    to,
    headers: new Headers(),
    raw: new Blob([raw]).stream(),
    rawSize,
    canBeForwarded: true,
    forward,
    setReject,
    reply: vi.fn(),
  } as unknown as ForwardableEmailMessage;
  return { message, forward, setReject };
}

async function run(message: ForwardableEmailMessage, withEnv: Env = env) {
  await worker.email(message, withEnv);
}

function stubFetch(answer: () => Promise<Response>) {
  const fetchMock = vi.fn(answer);
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

function warnings(warn: ReturnType<typeof vi.spyOn>): string {
  return warn.mock.calls.flat().join("\n");
}

describe("email handler", () => {
  let warn: ReturnType<typeof vi.spyOn>;

  beforeEach(() => {
    warn = vi.spyOn(console, "warn").mockImplementation(() => undefined);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it("forwards plain suporte@ without calling rittea", async () => {
    const fetchMock = stubFetch(async () => new Response(null));
    const { message, forward } = fakeMessage(
      textEmail("Olá"),
      "Suporte@Rittea.Tone.Rip",
    );
    await run(message);
    expect(fetchMock).not.toHaveBeenCalled();
    expect(forward).toHaveBeenCalledWith("humano@example.pt");
  });

  it("posts a token address to rittea with the bearer header", async () => {
    const fetchMock = stubFetch(async () => new Response("{}"));
    const { message, forward } = fakeMessage(textEmail("Já resolvi."));
    await run(message);

    expect(fetchMock).toHaveBeenCalledOnce();
    const [url, init] = fetchMock.mock.calls[0] as unknown as [
      URL,
      RequestInit,
    ];
    expect(String(url)).toBe("https://rittea.tone.rip/api/email/entrada");
    expect(init.method).toBe("POST");
    expect(init.signal).toBeInstanceOf(AbortSignal);
    expect(init.headers).toMatchObject({
      Authorization: `Bearer ${SECRET}`,
      "Content-Type": "application/json",
    });
    expect(JSON.parse(String(init.body))).toEqual({
      para: "suporte+abc123@rittea.tone.rip",
      de: "ana.cliente@example.pt",
      nome: "Ana Cliente",
      assunto: "Re: Pedido de ajuda",
      texto: "Já resolvi.",
      messageId: "<resposta-1@example.pt>",
    });
    expect(forward).not.toHaveBeenCalled();
    expect(warn).not.toHaveBeenCalled();
  });

  it("sends a rendering of the html when there is no text part", async () => {
    const fetchMock = stubFetch(async () => new Response("{}"));
    const { message } = fakeMessage(
      htmlEmail("<p>Bom dia,</p><p>já está &amp; obrigado</p>"),
    );
    await run(message);
    const [, init] = fetchMock.mock.calls[0] as unknown as [URL, RequestInit];
    expect(JSON.parse(String(init.body)).texto).toBe(
      "Bom dia,\njá está & obrigado",
    );
  });

  it("forwards quietly when rittea ignores it (202)", async () => {
    stubFetch(async () => new Response(null, { status: 202 }));
    const { message, forward } = fakeMessage(textEmail("Olá"));
    await run(message);
    expect(forward).toHaveBeenCalledWith("humano@example.pt");
    expect(warn).not.toHaveBeenCalled();
  });

  it("forwards and logs the status when rittea fails (500)", async () => {
    stubFetch(async () => new Response("erro", { status: 500 }));
    const { message, forward } = fakeMessage(textEmail("Corpo privado"));
    await run(message);
    expect(forward).toHaveBeenCalledWith("humano@example.pt");
    expect(warn).toHaveBeenCalledOnce();
    const logged = warnings(warn);
    expect(logged).toContain("500");
    expect(logged).not.toContain(SECRET);
    expect(logged).not.toContain("Corpo privado");
    expect(logged).not.toContain("@");
  });

  it("forwards and logs the error when fetch throws", async () => {
    stubFetch(async () => {
      throw new TypeError("Network connection lost.");
    });
    const { message, forward } = fakeMessage(textEmail("Olá"));
    await run(message);
    expect(forward).toHaveBeenCalledWith("humano@example.pt");
    expect(warnings(warn)).toContain("Network connection lost.");
  });

  it("forwards an oversized email without calling rittea", async () => {
    const fetchMock = stubFetch(async () => new Response("{}"));
    const { message, forward } = fakeMessage(
      textEmail("Olá"),
      undefined,
      1024 * 1024 + 1,
    );
    await run(message);
    expect(fetchMock).not.toHaveBeenCalled();
    expect(forward).toHaveBeenCalledWith("humano@example.pt");
  });

  it("rejects when FORWARD_TO is not set", async () => {
    const { message, forward, setReject } = fakeMessage(
      textEmail("Olá"),
      "suporte@rittea.tone.rip",
    );
    await run(message, { ...env, FORWARD_TO: "" });
    expect(forward).not.toHaveBeenCalled();
    expect(setReject).toHaveBeenCalledOnce();
  });

  it("rejects when the forward itself fails", async () => {
    const { message, forward, setReject } = fakeMessage(
      textEmail("Olá"),
      "suporte@rittea.tone.rip",
    );
    forward.mockRejectedValueOnce(new Error("destination not verified"));
    await run(message);
    expect(setReject).toHaveBeenCalledOnce();
    expect(warnings(warn)).toContain("destination not verified");
  });
});
