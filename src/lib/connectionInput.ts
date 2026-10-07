/** Recognise request URLs without changing schemes, hosts, gateway prefixes or parameters. */
export function normalizeApiAddress(input: string): string {
  const value = input.trim();
  try {
    const url = new URL(value);
    if (
      !["http:", "https:"].includes(url.protocol) ||
      url.username ||
      url.password ||
      url.search ||
      url.hash
    )
      return value;
    const path = url.pathname.replace(/\/+$/, "");
    const base = path.replace(
      /\/(?:chat\/completions|responses|messages|models)$/i,
      "",
    );
    return base !== path ? url.origin + base : value;
  } catch {
    return value;
  }
}

export function normalizeApiKey(input: string): string {
  let value = input.trim();
  // A copied Authorization value is often wrapped by a shell or JSON example.
  if (
    (value.startsWith('"') && value.endsWith('"')) ||
    (value.startsWith("'") && value.endsWith("'"))
  )
    value = value.slice(1, -1).trim();
  value = value.replace(/^Bearer\s+/i, "").trim();
  return value;
}
