/* One command that gets you a real session: `bun run dev`.
 *
 * The server needs two things before it is worth looking at, and neither is
 * interesting enough to make somebody do by hand every time:
 *
 *   - a host key, or it generates one relative to the working directory and
 *     your client warns about a changed identity the next time you run it
 *     from somewhere else;
 *   - an allowlist, or no session is a recognised one and the footer never
 *     shows a key label.
 *
 * So this makes both under .dev/ (gitignored, throwaway) if they are not
 * already there, starts the server, and prints the exact line to paste into
 * another terminal. Nothing here is used in production; see README.md for
 * what a real deployment passes instead.
 */

import { spawn } from "node:child_process";
import { existsSync } from "node:fs";
import { mkdir } from "node:fs/promises";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const appRoot = join(dirname(fileURLToPath(import.meta.url)), "..");
const devDir = join(appRoot, ".dev");
const hostKey = join(devDir, "host_ed25519");
const clientKey = join(devDir, "id_ed25519");
const authorizedKeys = join(devDir, "authorized_keys");
const ADDR = "localhost:2222";

async function run(command: string, args: string[]): Promise<void> {
  await new Promise<void>((resolve, reject) => {
    const child = spawn(command, args, { stdio: "inherit" });
    child.on("error", reject);
    child.on("exit", (code) =>
      code === 0
        ? resolve()
        : reject(new Error(`${command} exited with ${code}`)),
    );
  });
}

/**
 * A client key whose comment is the allowlist entry.
 *
 * `authz.ParseAuthorizedKeys` reads the label and any scopes from the
 * trailing fields of the comment, so `-C "laptop"` is what makes this key a
 * recognised one - the same shape a real key takes, which is the point of
 * using it here rather than hand-writing a fingerprint line.
 */
async function ensureKeys(): Promise<void> {
  if (!existsSync(hostKey)) {
    await run("ssh-keygen", [
      "-q",
      "-t",
      "ed25519",
      "-N",
      "",
      "-C",
      "ssh-cv dev host",
      "-f",
      hostKey,
    ]);
  }
  if (!existsSync(clientKey)) {
    await run("ssh-keygen", [
      "-q",
      "-t",
      "ed25519",
      "-N",
      "",
      "-C",
      "laptop",
      "-f",
      clientKey,
    ]);
    await Bun.write(authorizedKeys, await Bun.file(`${clientKey}.pub`).text());
  }
}

await mkdir(devDir, { recursive: true });
await ensureKeys();

console.log("");
console.log("  ssh-cv is starting. In another terminal:");
console.log("");
console.log(`    ssh -p 2222 -i ${clientKey} localhost`);
console.log("");
console.log("  That key is allowlisted, so the footer names it. Everyone");
console.log("  else reads the same CV without a label:");
console.log("");
console.log("    ssh -p 2222 -o IdentitiesOnly=yes localhost");
console.log("");
console.log("  And in Portuguese:");
console.log("");
console.log("    ssh -p 2222 -o IdentitiesOnly=yes pt@localhost");
console.log("");

await run("go", [
  "run",
  ".",
  "--addr",
  ADDR,
  "--host-key",
  hostKey,
  "--authorized-keys",
  authorizedKeys,
]);
