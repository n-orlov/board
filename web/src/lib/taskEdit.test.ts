import { describe, expect, it } from 'vitest';
import { CADENCE_REQUIRED, LINK_INCOMPLETE, MAX_LINKS, NEXT_CHECK_HINT, TOO_MANY_LINKS, addEditLink, cadenceInput, dayOf, minuteOf, removeEditLink, toTaskEditForm, toTaskPatch, type TaskEditForm } from './taskEdit';

const form = (extra: Partial<TaskEditForm> = {}): TaskEditForm => ({ title: 'T', description: 'D', plannedStart: '', plannedEnd: '', cadence: '', nextCheck: '', links: [], linkLabel: '', linkUrl: '', ...extra });

describe('dayOf', () => {
  it('keeps the date of an instant and gives empty for none', () => {
    expect(dayOf('2026-03-01T10:20:00Z')).toBe('2026-03-01');
    expect(dayOf(undefined)).toBe('');
  });
});

describe('cadenceInput', () => {
  it.each([
    [undefined, ''],
    [86400, '1d'],
    [172800, '2d'],
    [7200, '2h'],
    [5400, '90m'],
    [60, '1m'],
    [90, '90s'],
    [59, '59s'],
    [3600, '1h'],
  ])('%s is %j', (seconds, text) => {
    expect(cadenceInput(seconds)).toBe(text);
  });
});

describe('toTaskEditForm', () => {
  it('starts from the task as it is', () => {
    expect(
      toTaskEditForm({ title: 'T', description: 'D', planned_start: '2026-03-01T00:00:00Z', planned_end: '2026-04-01T09:00:00Z', cadence_seconds: 900 }),
    ).toEqual(form({ plannedStart: '2026-03-01', plannedEnd: '2026-04-01', cadence: '15m' }));
    expect(toTaskEditForm({ title: 'T', description: '' })).toEqual(form({ description: '' }));
  });
});

describe('toTaskPatch', () => {
  it('sends nothing when nothing changed', () => {
    expect(toTaskPatch(form(), form(), 'work')).toEqual({ ok: true, body: {} });
  });

  it('sends the trimmed text that changed and leaves the rest out', () => {
    expect(toTaskPatch(form({ title: '  New  ' }), form(), 'work')).toEqual({ ok: true, body: { title: 'New' } });
    expect(toTaskPatch(form({ description: 'E' }), form(), 'work')).toEqual({ ok: true, body: { description: 'E' } });
    expect(toTaskPatch(form({ description: '' }), form(), 'work')).toEqual({ ok: true, body: { description: '' } });
  });

  it('ignores surrounding blanks that are not a change', () => {
    expect(toTaskPatch(form({ title: ' T ', description: ' D ' }), form(), 'work')).toEqual({ ok: true, body: {} });
  });

  it('turns a planned date into the start of that day and an emptied one into null', () => {
    const start = form({ plannedStart: '2026-03-01', plannedEnd: '2026-04-01' });
    expect(toTaskPatch(form({ plannedStart: '2026-03-02', plannedEnd: '2026-04-01' }), start, 'work')).toEqual({
      ok: true,
      body: { planned_start: '2026-03-02T00:00:00Z' },
    });
    expect(toTaskPatch(form({ plannedStart: '2026-03-01', plannedEnd: '' }), start, 'work')).toEqual({ ok: true, body: { planned_end: null } });
    expect(toTaskPatch(form({ plannedStart: '', plannedEnd: '2026-05-01' }), start, 'work')).toEqual({
      ok: true,
      body: { planned_start: null, planned_end: '2026-05-01T00:00:00Z' },
    });
  });

  it('sends a watch task its cadence in seconds, and a work task never', () => {
    const watch = form({ cadence: '15m' });
    expect(toTaskPatch(form({ cadence: '2h' }), watch, 'watch')).toEqual({ ok: true, body: { cadence_seconds: 7200 } });
    expect(toTaskPatch(form({ cadence: '60s' }), watch, 'watch')).toEqual({ ok: true, body: { cadence_seconds: 60 } });
    expect(toTaskPatch(form({ cadence: '365d' }), watch, 'watch')).toEqual({ ok: true, body: { cadence_seconds: 31536000 } });
    expect(toTaskPatch(form({ cadence: '15m ' }), watch, 'watch')).toEqual({ ok: true, body: {} });
    expect(toTaskPatch(form({ cadence: '2h' }), watch, 'work')).toEqual({ ok: true, body: {} });
  });

  it('refuses an emptied watch cadence, never sending null, while an emptied planned date still clears', () => {
    const watch = form({ cadence: '15m', plannedStart: '2026-03-01' });
    for (const empty of ['', ' ', '\t']) {
      expect(toTaskPatch(form({ cadence: empty, plannedStart: '2026-03-01' }), watch, 'watch')).toEqual({ ok: false, error: CADENCE_REQUIRED });
      expect(toTaskPatch(form({ cadence: empty, plannedStart: '' }), watch, 'watch')).toEqual({ ok: false, error: CADENCE_REQUIRED });
    }
    expect(toTaskPatch(form({ cadence: '15m', plannedStart: '' }), watch, 'watch')).toEqual({ ok: true, body: { planned_start: null } });
  });

  it('refuses a cadence that is not a number and a unit, and says how to type it', () => {
    const result = toTaskPatch(form({ cadence: 'soon' }), form({ cadence: '15m' }), 'watch');
    expect(result).toEqual({ ok: false, error: expect.stringContaining('15m') });
  });

  it('puts the text and the cadence in one patch', () => {
    expect(toTaskPatch(form({ title: 'N', cadence: '1d' }), form({ cadence: '15m' }), 'watch')).toEqual({ ok: true, body: { title: 'N', cadence_seconds: 86400 } });
  });
});

describe('minuteOf', () => {
  it('keeps the date and the minute of an instant and gives empty for none', () => {
    expect(minuteOf('2026-03-01T10:20:30Z')).toBe('2026-03-01T10:20');
    expect(minuteOf(undefined)).toBe('');
  });
});

describe('toTaskEditForm links and next check', () => {
  it('copies the links and takes the next check to the minute', () => {
    const links = [{ label: 'PR', url: 'https://example.com/pr/1' }];
    const got = toTaskEditForm({ title: 'T', description: 'D', next_check_at: '2026-05-06T07:08:09Z', external_refs: links });
    expect(got.nextCheck).toBe('2026-05-06T07:08');
    expect(got.links).toEqual(links);
    expect(got.links[0]).not.toBe(links[0]);
  });
});

describe('next check in toTaskPatch', () => {
  const was = form({ nextCheck: '2026-05-06T07:08' });
  it('sends a changed next check as a UTC instant, only for a watch task', () => {
    expect(toTaskPatch(form({ nextCheck: '2026-06-01T09:30' }), was, 'watch')).toEqual({ ok: true, body: { next_check_at: '2026-06-01T09:30:00Z' } });
    expect(toTaskPatch(form({ nextCheck: '2026-06-01T09:30' }), was, 'work')).toEqual({ ok: true, body: {} });
  });
  it('sends nothing for an unchanged one and null for an emptied one', () => {
    expect(toTaskPatch(was, was, 'watch')).toEqual({ ok: true, body: {} });
    expect(toTaskPatch(form({ nextCheck: ' ' }), was, 'watch')).toEqual({ ok: true, body: { next_check_at: null } });
  });
  it('refuses text that is not a date and a time', () => {
    for (const bad of ['soon', '2026-06-01', '2026-06-01 09:30', '2026-6-1T09:30']) {
      expect(toTaskPatch(form({ nextCheck: bad }), was, 'watch')).toEqual({ ok: false, error: NEXT_CHECK_HINT });
    }
  });
  it('sends the next check together with the cadence and fields that changed', () => {
    const initial = form({ cadence: '15m', nextCheck: '2026-05-06T07:08' });
    expect(toTaskPatch(form({ title: 'U', cadence: '2h', nextCheck: '2026-05-07T07:08' }), initial, 'watch')).toEqual({
      ok: true,
      body: { title: 'U', cadence_seconds: 7200, next_check_at: '2026-05-07T07:08:00Z' },
    });
  });
});

describe('links in toTaskPatch', () => {
  const pr = { label: 'PR', url: 'https://example.com/pr/1' };
  const ci = { label: 'CI', url: 'https://example.com/ci/9' };
  const was = form({ links: [pr, ci] });

  it('sends nothing for links that did not change, and ignores blanks around them', () => {
    expect(toTaskPatch(form({ links: [{ ...pr }, { ...ci }] }), was, 'work')).toEqual({ ok: true, body: {} });
    expect(toTaskPatch(form({ links: [{ label: ' PR ', url: ' https://example.com/pr/1 ' }, ci] }), was, 'work')).toEqual({ ok: true, body: {} });
  });
  it('adds a link, typed or drafted, replaces one and removes one, always sending the whole list', () => {
    const added = { label: 'Docs', url: 'https://example.com/docs' };
    expect(toTaskPatch(form({ links: [pr, ci, added] }), was, 'work')).toEqual({ ok: true, body: { external_refs: [pr, ci, added] } });
    expect(toTaskPatch(form({ links: [pr, ci], linkLabel: ' Docs ', linkUrl: ' https://example.com/docs ' }), was, 'work')).toEqual({
      ok: true,
      body: { external_refs: [pr, ci, added] },
    });
    expect(toTaskPatch(form({ links: [{ ...pr, url: 'https://example.com/pr/2' }, ci] }), was, 'work')).toEqual({
      ok: true,
      body: { external_refs: [{ label: 'PR', url: 'https://example.com/pr/2' }, ci] },
    });
    expect(toTaskPatch(form({ links: [ci] }), was, 'work')).toEqual({ ok: true, body: { external_refs: [ci] } });
    expect(toTaskPatch(form({ links: [] }), was, 'work')).toEqual({ ok: true, body: { external_refs: [] } });
    expect(toTaskPatch(form({ links: [ci, pr] }), was, 'work')).toEqual({ ok: true, body: { external_refs: [ci, pr] } });
  });
  it('refuses a half-typed link', () => {
    expect(toTaskPatch(form({ links: [pr, ci], linkLabel: 'x' }), was, 'work')).toEqual({ ok: false, error: LINK_INCOMPLETE });
    expect(toTaskPatch(form({ links: [pr, ci], linkUrl: 'https://x.example' }), was, 'work')).toEqual({ ok: false, error: LINK_INCOMPLETE });
  });
  it('refuses more links than a task holds', () => {
    const many = Array.from({ length: MAX_LINKS + 1 }, (_, n) => ({ label: `L${n}`, url: `https://example.com/${n}` }));
    expect(toTaskPatch(form({ links: many }), was, 'work')).toEqual({ ok: false, error: TOO_MANY_LINKS });
    expect(toTaskPatch(form({ links: many.slice(0, MAX_LINKS) }), was, 'work').ok).toBe(true);
  });
});

describe('addEditLink and removeEditLink', () => {
  const pr = { label: 'PR', url: 'https://example.com/pr/1' };
  it('moves a complete draft into the list and leaves an incomplete one alone', () => {
    expect(addEditLink(form({ links: [pr], linkLabel: ' CI ', linkUrl: ' https://example.com/ci ' }))).toEqual(form({ links: [pr, { label: 'CI', url: 'https://example.com/ci' }] }));
    const half = form({ linkLabel: 'CI' });
    expect(addEditLink(half)).toBe(half);
    const none = form();
    expect(addEditLink(none)).toBe(none);
  });
  it('drops the link at an index', () => {
    const two = form({ links: [pr, { label: 'CI', url: 'https://example.com/ci' }] });
    expect(removeEditLink(two, 0).links).toEqual([{ label: 'CI', url: 'https://example.com/ci' }]);
    expect(removeEditLink(two, 1).links).toEqual([pr]);
  });
});
