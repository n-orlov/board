import { describe, expect, it } from 'vitest';
import { CADENCE_REQUIRED, cadenceInput, dayOf, toTaskEditForm, toTaskPatch, type TaskEditForm } from './taskEdit';

const form = (extra: Partial<TaskEditForm> = {}): TaskEditForm => ({ title: 'T', description: 'D', plannedStart: '', plannedEnd: '', cadence: '', ...extra });

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
    ).toEqual({ title: 'T', description: 'D', plannedStart: '2026-03-01', plannedEnd: '2026-04-01', cadence: '15m' });
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
