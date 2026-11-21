import { describe, expect, it } from 'vitest';
import { CatalogReads, type CatalogEntry } from './catalogReads';

// A read the test answers by hand, so the order the reads land in is the test's.
function controlled() {
  const pending: { resolve: (list: CatalogEntry[]) => void; reject: (e: unknown) => void }[] = [];
  const reads = new CatalogReads(
    () =>
      new Promise<CatalogEntry[]>((resolve, reject) => {
        pending.push({ resolve, reject });
      }),
  );
  return { reads, pending };
}

const OLD: CatalogEntry[] = [{ slug: 'a', tags: ['stale'] }];
const NEW: CatalogEntry[] = [
  { slug: 'a', tags: ['current'] },
  { slug: 'b', tags: [] },
];

describe('CatalogReads', () => {
  it('answers a single read with its entries', async () => {
    const reads = new CatalogReads(async () => NEW);
    expect(await reads.next()).toEqual(NEW);
  });

  it('answers with a copy, so a later change to the answer does not reach the view', async () => {
    const source: CatalogEntry[] = [{ slug: 'a', tags: ['x'] }];
    const reads = new CatalogReads(async () => source);
    const got = await reads.next();
    source[0].tags.push('y');
    expect(got).toEqual([{ slug: 'a', tags: ['x'] }]);
  });

  it('drops an older read that lands after a newer one was applied', async () => {
    const { reads, pending } = controlled();
    const older = reads.next();
    const newer = reads.next();
    pending[1].resolve(NEW);
    expect(await newer).toEqual(NEW);
    pending[0].resolve(OLD);
    expect(await older).toBeUndefined();
  });

  it('applies both reads when they land in the order they began', async () => {
    const { reads, pending } = controlled();
    const older = reads.next();
    const newer = reads.next();
    pending[0].resolve(OLD);
    expect(await older).toEqual(OLD);
    pending[1].resolve(NEW);
    expect(await newer).toEqual(NEW);
  });

  it('answers undefined for a failed read and keeps an older read in flight free to land', async () => {
    const { reads, pending } = controlled();
    const older = reads.next();
    const newer = reads.next();
    pending[1].reject(new Error('down'));
    expect(await newer).toBeUndefined();
    pending[0].resolve(OLD);
    expect(await older).toEqual(OLD);
  });

  it('still drops a read older than the newest applied when a read between them failed', async () => {
    const { reads, pending } = controlled();
    const first = reads.next();
    const second = reads.next();
    const third = reads.next();
    pending[2].resolve(NEW);
    expect(await third).toEqual(NEW);
    pending[1].reject(new Error('down'));
    expect(await second).toBeUndefined();
    pending[0].resolve(OLD);
    expect(await first).toBeUndefined();
  });

  it('applies a read begun after the newest applied one', async () => {
    const { reads, pending } = controlled();
    const first = reads.next();
    pending[0].resolve(OLD);
    expect(await first).toEqual(OLD);
    const second = reads.next();
    pending[1].resolve(NEW);
    expect(await second).toEqual(NEW);
  });
});
