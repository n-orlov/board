import { describe, expect, it, vi } from 'vitest';
import { copyText } from './clipboard';

describe('copyText', () => {
  it('writes exactly the text it was given and says it worked', async () => {
    const writeText = vi.fn(async () => {});
    expect(await copyText({ writeText }, 'brd_abc')).toBe(true);
    expect(writeText).toHaveBeenCalledTimes(1);
    expect(writeText).toHaveBeenCalledWith('brd_abc');
  });

  it('says it did not work when the write is refused', async () => {
    const writeText = vi.fn(async () => {
      throw new DOMException('denied', 'NotAllowedError');
    });
    expect(await copyText({ writeText }, 'brd_abc')).toBe(false);
    expect(writeText).toHaveBeenCalledTimes(1);
  });

  it('says it did not work when there is no Clipboard API', async () => {
    expect(await copyText(undefined, 'brd_abc')).toBe(false);
  });
});
