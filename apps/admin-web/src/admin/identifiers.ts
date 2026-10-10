const identifierMaxLength = 128;

function trimIdentifier(value: string): string {
  return value.slice(0, identifierMaxLength).replace(/^[^a-z0-9]+|[^a-z0-9]+$/gu, "");
}

export function identifierFromName(name: string, fallback: string): string {
  const slug = trimIdentifier(
    name
      .normalize("NFKD")
      .replace(/\p{M}+/gu, "")
      .toLowerCase()
      .replace(/[^a-z0-9._~-]+/gu, "-")
      .replace(/-{2,}/gu, "-"),
  );
  return slug === "" ? fallback : slug;
}

export function uniqueIdentifier(base: string, taken: ReadonlySet<string>): string {
  if (!taken.has(base)) return base;
  for (let index = 2; ; index += 1) {
    const suffix = `-${index}`;
    const candidate = `${trimIdentifier(base.slice(0, identifierMaxLength - suffix.length))}${suffix}`;
    if (!taken.has(candidate)) return candidate;
  }
}

export function newIdentifierSuffix(): string {
  return crypto.randomUUID().replaceAll("-", "").slice(0, 8);
}

export function identifierWithSuffix(base: string, suffix: string): string {
  const prefix = trimIdentifier(base.slice(0, identifierMaxLength - suffix.length - 1));
  return prefix === "" ? suffix : `${prefix}-${suffix}`;
}

export function nextProfileVersion(
  profiles: readonly Readonly<{ spec: Readonly<{ profileId: string; version: number }> }>[],
  profileId: string,
): number {
  return (
    profiles.reduce(
      (latest, { spec }) =>
        spec.profileId === profileId ? Math.max(latest, spec.version) : latest,
      0,
    ) + 1
  );
}
