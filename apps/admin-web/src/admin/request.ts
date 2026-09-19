export function newRequestId(): string {
  return `admin-${crypto.randomUUID()}`;
}

export function newIdempotencyKey(): string {
  return `admin-${crypto.randomUUID()}`;
}
