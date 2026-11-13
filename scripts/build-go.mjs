// Build our own Go core with locked modules; no uni-switch runtime dependency.
import { spawnSync } from "node:child_process";
import {
  existsSync,
  mkdirSync,
  readdirSync,
  readFileSync,
  writeFileSync,
} from "node:fs";
import { createHash } from "node:crypto";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const args = process.argv.slice(2);
const qaBuild = args.includes("--qa");
const buildTags = qaBuild ? "nogui,uniswitchqa" : "nogui";
function option(name) {
  const i = args.indexOf(name);
  return i < 0 ? undefined : args[i + 1];
}
function run(command, arguments_, options = {}) {
  const result = spawnSync(command, arguments_, {
    cwd: root,
    encoding: "utf8",
    windowsHide: true,
    ...options,
  });
  if (result.error || result.status !== 0) {
    throw new Error(
      `${command} failed: ${result.error?.message ?? [result.stdout, result.stderr].filter(Boolean).join("\n")}`,
    );
  }
  return result.stdout.trim();
}
const localGo = join(
  root,
  ".tools",
  "go",
  "bin",
  process.platform === "win32" ? "go.exe" : "go",
);
const go = process.env.UNI_SWITCH_GO || (existsSync(localGo) ? localGo : "go");
const hosts = {
  win32: `${process.arch === "arm64" ? "aarch64" : "x86_64"}-pc-windows-msvc`,
  linux: `${process.arch === "arm64" ? "aarch64" : "x86_64"}-unknown-linux-gnu`,
  darwin: `${process.arch === "arm64" ? "aarch64" : "x86_64"}-apple-darwin`,
};
const target =
  option("--target") ||
  process.env.TAURI_ENV_TARGET_TRIPLE ||
  hosts[process.platform];
const targets = {
  "x86_64-pc-windows-msvc": ["windows", "amd64"],
  "aarch64-pc-windows-msvc": ["windows", "arm64"],
  "x86_64-unknown-linux-gnu": ["linux", "amd64"],
  "aarch64-unknown-linux-gnu": ["linux", "arm64"],
  "x86_64-apple-darwin": ["darwin", "amd64"],
  "aarch64-apple-darwin": ["darwin", "arm64"],
};
const output = join(root, "src-tauri", "binaries");
mkdirSync(output, { recursive: true });
function binary(triple) {
  return join(
    output,
    `uni-switch-core-${triple}${triple.includes("windows") ? ".exe" : ""}`,
  );
}
function sources(directory) {
  return readdirSync(directory, { withFileTypes: true }).flatMap((e) =>
    e.isDirectory()
      ? sources(join(directory, e.name))
      : [join(directory, e.name)],
  );
}
const sourceHash = createHash("sha256");
for (const path of [
  ...sources(join(root, "backend")),
  fileURLToPath(import.meta.url),
].sort()) {
  sourceHash
    .update(path.slice(root.length))
    .update("\0")
    .update(readFileSync(path))
    .update("\0");
}
const sourceFingerprint = sourceHash.digest("hex");
// Verify tool availability even for a cached artifact, so missing build prerequisites
// fail here instead of producing a package with an accidental stale executable.
const version = run(go, ["version"], {
  env: { ...process.env, GOTOOLCHAIN: "local" },
});
if (!/^go version go1\.(2[6-9]|[3-9]\d)(?:\.|\s)/.test(version)) {
  throw new Error(`Go >= 1.26 required, got ${version}`);
}
if (args.includes("--test")) {
  console.log(run(process.execPath, [join(root,"scripts/test-go.mjs"), "-timeout=20m"]));
  process.exit(0);
}
function build(triple) {
  const platform = targets[triple];
  if (!platform) throw new Error(`Unsupported Go core target: ${triple}`);
  const destination = binary(triple);
  const applicationVersion=JSON.parse(readFileSync(join(root,"package.json"),"utf8")).version;
  if(!/^\d+\.\d+\.\d+(?:-[\w.-]+)?$/.test(applicationVersion))throw new Error("Invalid application version");
  const fingerprint = createHash("sha256")
    .update(
      `${sourceFingerprint}\0${version}\0${applicationVersion}\0${triple}\0tags=${buildTags},cgo=0,trimpath,buildvcs=false,s,w`,
    )
    .digest("hex");
  let cached = false;
  try {
    const record = JSON.parse(
      readFileSync(`${destination}.build.json`, "utf8"),
    );
    cached =
      record.fingerprint === fingerprint &&
      record.sha256 ===
        createHash("sha256").update(readFileSync(destination)).digest("hex");
  } catch {
    /* A missing or invalid cache always rebuilds. */
  }
  if (!cached) {
    run(
      go,
      [
        "build",
        `-tags=${buildTags}`,
        "-trimpath",
        "-buildvcs=false",
        `-ldflags=-s -w -X main.version=${applicationVersion}`,
        "-o",
        destination,
        "./cmd/uni-switch-core",
      ],
      {
        cwd: join(root, "backend"),
        env: {
          ...process.env,
          GOOS: platform[0],
          GOARCH: platform[1],
          CGO_ENABLED: "0",
          GOTOOLCHAIN: "local",
        },
      },
    );
    writeFileSync(
      `${destination}.build.json`,
      JSON.stringify({
        fingerprint,
        qa: qaBuild,
        sha256: createHash("sha256")
          .update(readFileSync(destination))
          .digest("hex"),
      }) + "\n",
      "utf8",
    );
  }
  return destination;
}
let destination;
if (target === "universal-apple-darwin") {
  if (process.platform !== "darwin")
    throw new Error("A universal Go core requires macOS lipo");
  const intel = build("x86_64-apple-darwin");
  const arm = build("aarch64-apple-darwin");
  destination = binary(target);
  run("lipo", ["-create", intel, arm, "-output", destination]);
  run("codesign", ["--force", "--sign", "-", destination]);
} else {
  destination = build(target);
}
console.log(`Built Go core: ${target}${qaBuild ? " (isolated QA)" : ""}`);
