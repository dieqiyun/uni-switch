import { createPrivateKey, sign, createPublicKey } from "node:crypto";
import { readFile, writeFile } from "node:fs/promises";
import path from "node:path";

const [input, privateKeyFile, output] = process.argv.slice(2);
if (!input || !privateKeyFile || !output || process.argv.length !== 5) {
  throw new Error(
    "Usage: node scripts/sign-model-registry.mjs <registry.json> <external-ed25519-private.pem> <registry.json.sig>",
  );
}
const targets = [input, privateKeyFile, output].map((value) =>
  path.resolve(value),
);
if (
  new Set(
    targets.map((value) =>
      process.platform === "win32" ? value.toLowerCase() : value,
    ),
  ).size !== 3
) {
  throw new Error("Input, private key and output must be different files");
}
const payload = await readFile(targets[0]);
const registry = JSON.parse(payload.toString("utf8"));
if (
  payload.length > 2 * 1024 * 1024 ||
  registry.schemaVersion !== 1 ||
  !Number.isSafeInteger(registry.version) ||
  registry.version < 1 ||
  !Array.isArray(registry.entries) ||
  !registry.entries.length
) {
  throw new Error(
    "Invalid registry schema or size; run the Rust registry tests before signing",
  );
}
const key = createPrivateKey(await readFile(targets[1]));
if (key.asymmetricKeyType !== "ed25519")
  throw new Error("The registry requires an Ed25519 key");
const publicDer = createPublicKey(key).export({ type: "spki", format: "der" });
const signature = sign(null, payload, key).toString("base64");
await writeFile(targets[2], `${signature}\n`, { encoding: "utf8", flag: "wx" });
console.log(
  JSON.stringify(
    {
      signature: targets[2],
      version: registry.version,
      publicKey: publicDer.subarray(-32).toString("base64"),
    },
    null,
    2,
  ),
);
