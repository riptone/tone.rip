# rittea-email

A Cloudflare Email Worker for `suporte@rittea.tone.rip`. It receives the
replies customers send to rittea's support tickets and hands them to rittea
(https://rittea.tone.rip, a separate product with its own repo). It has no
fetch handler, no URL and no route: Email Routing calls it by name.

```bash
bun run test
bun run build    # dry-run bundle into dist/
bun run deploy
```

## What it does

For every email Email Routing hands it (`src/index.ts`):

1. **`suporte@rittea.tone.rip`** (no `+token`): forwarded to `FORWARD_TO`.
   rittea is not called.
2. **Over 1 MiB** (`message.rawSize`): forwarded, rittea is not called.
3. **`suporte+<token>@rittea.tone.rip`**: parsed with `postal-mime` and
   posted as JSON to `${RITTEA_URL}/api/email/entrada`, with
   `Authorization: Bearer ${EMAIL_ENTRADA_SEGREDO}` and a 10 s deadline:

   ```json
   { "para": "suporte+<token>@rittea.tone.rip", "de": "ana@example.pt",
     "nome": "Ana", "assunto": "Re: ...", "texto": "...",
     "messageId": "<...>", "anexos": 1 }
   ```

   `texto` is the text part, or the HTML part with its tags stripped when
   there is no text part, cut at 20,000 characters. Quoted replies are left in
   (rittea removes them). Attachments are not sent; `anexos` counts them and
   is left out when there are none. The field names are rittea's contract,
   which is in Portuguese.
4. **rittea's answer** (`src/outcome.ts`):
   - `200`: done. rittea stored it and posted it in the ticket's Discord thread.
   - `202`: rittea ignored it (an unknown or forged token, or a sender who is
     not the ticket's requester). Forwarded so a human sees it.
   - anything else, a network error or the timeout: forwarded, so nothing is
     lost, with a one-line `console.warn` (the status or the error message,
     never the secret, the body or an address).

If `FORWARD_TO` is unset or the forward itself fails, the email is rejected
with `setReject()`, so the sender gets a bounce instead of the email
disappearing.

## Environment

| Name | Kind | What |
| --- | --- | --- |
| `RITTEA_URL` | var (`wrangler.jsonc`) | `https://rittea.tone.rip` |
| `EMAIL_ENTRADA_SEGREDO` | secret | The bearer token. Same value as rittea's Coolify variable of the same name. |
| `FORWARD_TO` | secret | Where mail for a human goes. Must be a verified Email Routing destination address. A secret rather than a var because it is a personal address in a public repo, and because `wrangler deploy` overwrites vars set in the dashboard. |

Both secrets are listed in `secrets.required` in `wrangler.jsonc`, so
`wrangler types` generates the same `Env` on every machine, and wrangler
refuses to deploy without them. For local runs, copy `.dev.vars.example` to
`.dev.vars` (gitignored).

## One-time setup

In the Cloudflare dashboard, on the `tone.rip` zone. The menu paths are the
ones Cloudflare's docs give today (**Compute > Email Service > Email
Routing**); older dashboards show it as **Email > Email Routing**.

1. **Enable Email Routing for `rittea.tone.rip`.** Email Routing → Settings →
   *Subdomains*, add `rittea`. Cloudflare adds the MX records
   (`route1/2/3.mx.cloudflare.net`) and an SPF TXT
   (`v=spf1 include:_spf.mx.cloudflare.net ~all`) on `rittea.tone.rip`.
   If `rittea.tone.rip` already has an SPF record for Brevo, merge them into
   **one** record, never two:
   `v=spf1 include:spf.brevo.com include:_spf.mx.cloudflare.net ~all`.
   (Unverified: whether the subdomain records land exactly on
   `rittea.tone.rip` with those values - the docs list them for an apex
   domain; check the DNS tab after enabling.)
2. **Enable subaddressing.** Email Routing → Settings → *Subaddressing*. With
   it, `suporte+<token>@` matches the `suporte@` rule, and the `+token` is
   kept in `message.to` (per Cloudflare's docs).
3. **Add `FORWARD_TO` as a destination address.** Email Routing →
   Destination addresses → add it, and click the link in the verification
   email Cloudflare sends.
4. **Deploy, with the secrets.** wrangler will not create a new Worker with
   `secrets.required` unset, and `wrangler secret put` cannot run before the
   Worker exists, so the first deploy takes them from a file:

   ```bash
   cd apps/rittea-email
   cp .dev.vars.example .dev.vars   # fill in both values
   bun run deploy --secrets-file .dev.vars
   ```

   Afterwards, to change one: `bunx wrangler secret put EMAIL_ENTRADA_SEGREDO`
   (or `FORWARD_TO`), and a plain `bun run deploy` keeps them.
5. **Route the address to the Worker.** Email Routing → Routing rules →
   Custom address `suporte@rittea.tone.rip` → action *Send to a Worker* →
   `rittea-email`.

Then send a test to `suporte@rittea.tone.rip` (it should reach `FORWARD_TO`)
and check the Worker's logs in the observability panel.

This Worker is not in CI's `deploy` job: a deploy before step 4 would fail
there. Add it once the Worker exists, if `main` should deploy it too.

## Types

`worker-configuration.d.ts` is generated, not committed: `bun run types`.
