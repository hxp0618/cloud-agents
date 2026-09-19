export type AdminPage<T> = Readonly<{
  items: readonly T[];
  nextPageToken: string | undefined;
}>;

/** Collects token pages and fails closed when a server repeats a cursor. */
export async function collectAdminPages<T>(
  load: (pageToken: string | undefined) => Promise<AdminPage<T>>,
  repeatedToken: () => Error,
): Promise<T[]> {
  const items: T[] = [];
  const seenTokens = new Set<string>();
  let pageToken: string | undefined;
  do {
    const page = await load(pageToken);
    items.push(...page.items);
    pageToken = page.nextPageToken;
    if (pageToken === undefined) continue;
    if (seenTokens.has(pageToken)) throw repeatedToken();
    seenTokens.add(pageToken);
  } while (pageToken !== undefined);
  return items;
}
