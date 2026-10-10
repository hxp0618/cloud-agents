const emailDomainPattern =
  /^(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/u;

function normalizeEmailDomain(value: string): string | null {
  if ([..."\\/:@?#[]%"].some((character) => value.includes(character))) return null;
  try {
    const url = new URL(`https://${value.toLowerCase()}`);
    const domain = url.hostname;
    if (
      url.username !== "" ||
      url.password !== "" ||
      url.port !== "" ||
      url.pathname !== "/" ||
      url.search !== "" ||
      url.hash !== "" ||
      domain.length > 253 ||
      !emailDomainPattern.test(domain)
    )
      return null;
    return domain;
  } catch {
    return null;
  }
}

export function parseEmailDomains(value: string): readonly string[] | null {
  const values = value.split(/[\s,]+/u).filter(Boolean);
  if (values.length > 64) return null;
  const domains: string[] = [];
  for (const value of values) {
    const domain = normalizeEmailDomain(value);
    if (domain === null) return null;
    domains.push(domain);
  }
  return Object.freeze([...new Set(domains)].sort());
}
