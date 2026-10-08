export async function collectPages<T>(
  load: (pageToken: string | undefined) => Promise<{
    items: readonly T[];
    nextPageToken: string | undefined;
  }>,
  resource: string,
): Promise<T[]> {
  const items: T[] = [];
  const seenTokens = new Set<string>();
  let pageToken: string | undefined;
  do {
    const page = await load(pageToken);
    items.push(...page.items);
    pageToken = page.nextPageToken;
    if (pageToken === undefined) break;
    if (seenTokens.has(pageToken))
      throw new Error(`Control Plane repeated a ${resource} page token`);
    seenTokens.add(pageToken);
  } while (pageToken !== undefined);
  return items;
}
