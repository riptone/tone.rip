/* Cut a release of apps/ssh-cv: `bun run release patch`.
 *
 * The version lives in exactly one place - the git tag - and this is what
 * writes it. There is deliberately no VERSION file and no field in
 * package.json to bump: two places to say the same number is one place to
 * forget, and the tag is the thing the release workflow and the box's updater
 * both actually read.
 *
 * Tags are namespaced `ssh-cv/vX.Y.Z`. The prefix is not decoration - it is
 * what makes "the newest ssh-cv release" answerable in a monorepo where the
 * next app to need releases will want `v1.0.0` too, and it is what
 * scripts/install.sh filters the GitHub API on.
 *
 * Pushing the tag is what triggers the release, so it does not happen by
 * accident: this writes the tag locally and prints the push command, unless
 * you pass --push.
 */

import { $ } from "bun";

type Bump = "patch" | "minor" | "major";

const TAG_PREFIX = "ssh-cv/";

function usage(): never {
  console.error(`
Cut a release of apps/ssh-cv.

  bun run release patch          v1.4.0 -> v1.4.1
  bun run release minor          v1.4.0 -> v1.5.0
  bun run release major          v1.4.0 -> v2.0.0
  bun run release v2.1.0         exactly this version
  bun run release patch --push   also push, which starts the release

Pushing the tag is what builds and publishes it; without --push this only
writes the tag locally and tells you the command.
`);
  process.exit(1);
}

async function git(...args: string[]): Promise<string> {
  const result = await $`git ${args}`.quiet().nothrow();
  if (result.exitCode !== 0) {
    throw new Error(
      `git ${args.join(" ")} failed:\n${result.stderr.toString().trim()}`,
    );
  }
  return result.stdout.toString().trim();
}

/**
 * The newest existing release, or null for the very first one.
 *
 * `--sort=-v:refname` is version sorting rather than lexicographic, which is
 * the difference between v1.10.0 and v1.9.0 ordering correctly and not.
 */
async function latestTag(): Promise<string | null> {
  const tags = await git(
    "tag",
    "--list",
    `${TAG_PREFIX}v*`,
    "--sort=-v:refname",
  );
  const [newest] = tags.split("\n").filter(Boolean);
  return newest ?? null;
}

function parse(version: string): [number, number, number] {
  const match = /^v(\d+)\.(\d+)\.(\d+)$/.exec(version);
  if (!match) throw new Error(`not a version: ${version}`);
  return [Number(match[1]), Number(match[2]), Number(match[3])];
}

function bump(from: string, kind: Bump): string {
  const [major, minor, patch] = parse(from);
  if (kind === "major") return `v${major + 1}.0.0`;
  if (kind === "minor") return `v${major}.${minor + 1}.0`;
  return `v${major}.${minor}.${patch + 1}`;
}

const args = process.argv.slice(2);
const push = args.includes("--push");
const [what] = args.filter((arg) => !arg.startsWith("--"));
if (!what) usage();

// A release is built from the tagged commit, so a dirty tree means shipping
// something that is not in the tag. Check before doing any of the work.
if ((await git("status", "--porcelain")) !== "") {
  console.error("The working tree has uncommitted changes. Commit them first.");
  process.exit(1);
}

const branch = await git("rev-parse", "--abbrev-ref", "HEAD");
if (branch !== "main") {
  console.error(`On branch ${branch}. Releases are cut from main.`);
  process.exit(1);
}

const previous = await latestTag();
let next: string;
if (what === "patch" || what === "minor" || what === "major") {
  if (!previous) {
    console.error(
      `No ${TAG_PREFIX}v* tag exists yet, so there is nothing to bump.\n` +
        "Name the first version explicitly: bun run release v0.1.0",
    );
    process.exit(1);
  }
  next = bump(previous.slice(TAG_PREFIX.length), what);
} else {
  next = what.startsWith("v") ? what : `v${what}`;
  parse(next); // throws with a useful message if it is not a version
}

const tag = `${TAG_PREFIX}${next}`;

const existing = await git("tag", "--list", tag);
if (existing !== "") {
  console.error(
    `${tag} already exists. A published version is not re-cut - bump instead.`,
  );
  process.exit(1);
}

const head = await git("rev-parse", "--short", "HEAD");
await git("tag", "-a", tag, "-m", `ssh-cv ${next}`);

console.log(`\n  ${previous ?? "(first release)"} -> ${tag}  at ${head}\n`);

if (!push) {
  console.log(
    "  Tag written locally. Pushing it is what builds and publishes:\n",
  );
  console.log(`    git push origin ${tag}\n`);
  console.log(`  Or undo:  git tag -d ${tag}\n`);
  process.exit(0);
}

await git("push", "origin", tag);
const remote = (await git("remote", "get-url", "origin"))
  .replace(/^git@github\.com:/, "https://github.com/")
  .replace(/\.git$/, "");
console.log(`  Pushed. The release build is starting:\n`);
console.log(`    ${remote}/actions\n`);
