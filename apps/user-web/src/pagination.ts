export function recordPageToken(
  seenTokens: Set<string>,
  pageToken: string | undefined,
  resource: string,
): void {
  if (pageToken === undefined) return;
  if (seenTokens.has(pageToken)) throw new Error(`Control Plane repeated a ${resource} page token`);
  seenTokens.add(pageToken);
}
