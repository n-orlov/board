// The initiative catalog a view offers as filter choices is read again on every initiative event (PRD R1), so reads can
// overlap and land out of order: a read that lands after a newer read has already been applied answers for a catalog older
// than the one shown and is dropped, so the choices never go back to an older catalog. A failed read is no answer at all:
// it changes nothing and leaves an older read still in flight free to land.

/** One initiative as a view's filter choices need it: its slug and its tags. */
export interface CatalogEntry {
  slug: string;
  tags: string[];
}

export class CatalogReads {
  private readonly read: () => Promise<readonly CatalogEntry[]>;
  private issued = 0;
  private applied = 0;

  constructor(read: () => Promise<readonly CatalogEntry[]>) {
    this.read = read;
  }

  /**
   * Reads the catalog: its entries when this read is the newest to land so far, else undefined (it failed, or a read begun
   * after it has already been applied).
   */
  async next(): Promise<CatalogEntry[] | undefined> {
    const mine = ++this.issued;
    let list: readonly CatalogEntry[];
    try {
      list = await this.read();
    } catch {
      return undefined;
    }
    if (mine < this.applied) return undefined;
    this.applied = mine;
    return list.map((i) => ({ slug: i.slug, tags: [...i.tags] }));
  }
}
