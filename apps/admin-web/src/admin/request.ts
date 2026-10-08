export function newRequestId(): string {
  return `admin-${crypto.randomUUID()}`;
}

export function newIdempotencyKey(): string {
  return `admin-${crypto.randomUUID()}`;
}

export function adminMutationKey(operation: string, body: object): string {
  return `${operation}:${JSON.stringify(body)}`;
}

export function pendingIdempotencyKey(pending: Map<string, string>, operationKey: string): string {
  const existing = pending.get(operationKey);
  if (existing !== undefined) return existing;
  const created = newIdempotencyKey();
  pending.set(operationKey, created);
  return created;
}
