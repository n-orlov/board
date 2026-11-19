import { describe, expect, it } from 'vitest';
import { optionLabel, optionValue, pickOption, sortFragment, sortFromFragment, sortOptions, withSort, type SortColumn } from './sortControl';

const columns: SortColumn[] = [
  { column: 'ref', label: 'Ref' },
  { column: 'title', label: 'Title' },
];

describe('the options', () => {
  it('offer the default order, then each column ascending and descending', () => {
    expect(sortOptions(columns)).toEqual([
      { value: '', label: 'Default order' },
      { value: 'ref:asc', label: 'Ref (ascending)' },
      { value: 'ref:desc', label: 'Ref (descending)' },
      { value: 'title:asc', label: 'Title (ascending)' },
      { value: 'title:desc', label: 'Title (descending)' },
    ]);
  });
});

describe('choosing an option', () => {
  it('reads each option value back as the sort it shows', () => {
    for (const option of sortOptions(columns)) {
      expect(optionValue(pickOption(option.value, columns))).toBe(option.value);
    }
    expect(pickOption('title:desc', columns)).toEqual({ column: 'title', dir: 'desc' });
    expect(pickOption('', columns)).toBeNull();
  });

  it('takes a column the page does not have, or a broken value, as the default order', () => {
    expect(pickOption('flags:asc', columns)).toBeNull();
    expect(pickOption('title:sideways', columns)).toBeNull();
    expect(pickOption('title', columns)).toBeNull();
  });

  it('labels the chosen option', () => {
    expect(optionLabel({ column: 'ref', dir: 'desc' }, columns)).toBe('Ref (descending)');
    expect(optionLabel(null, columns)).toBe('Default order');
    expect(optionLabel({ column: 'gone', dir: 'asc' }, columns)).toBe('Default order');
  });
});

describe('the fragment round trip', () => {
  it('writes sort and dir, and reads the same sort back, for every option', () => {
    for (const option of sortOptions(columns)) {
      const sort = pickOption(option.value, columns);
      expect(sortFromFragment(sortFragment(sort), columns)).toEqual(sort);
    }
    expect(sortFragment({ column: 'ref', dir: 'desc' })).toBe('sort=ref&dir=desc');
    expect(sortFragment(null)).toBe('');
  });

  it('reads an unknown column as the default order and a missing dir as ascending', () => {
    expect(sortFromFragment('sort=nope&dir=desc', columns)).toBeNull();
    expect(sortFromFragment('sort=ref', columns)).toEqual({ column: 'ref', dir: 'asc' });
    expect(sortFromFragment('', columns)).toBeNull();
  });

  it('replaces the sort of a saved query and keeps the rest', () => {
    expect(withSort('status=all&sort=ref&dir=asc&tag=x', { column: 'title', dir: 'desc' })).toBe('status=all&tag=x&sort=title&dir=desc');
    expect(withSort('sort=ref&dir=asc', null)).toBe('');
    expect(withSort('', { column: 'ref', dir: 'asc' })).toBe('sort=ref&dir=asc');
  });
});
