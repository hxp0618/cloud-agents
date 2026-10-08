const emailDomainPattern = /^(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/u;

export function parseEmailDomains(value: string): readonly string[] | null {
  const domains = [...new Set(value.split(/[\s,]+/u).filter(Boolean).map((domain) => domain.toLowerCase()))];
  return domains.every((domain) => emailDomainPattern.test(domain)) ? Object.freeze(domains) : null;
}
