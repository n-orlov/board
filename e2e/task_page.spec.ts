import { expect, test as base, type APIRequestContext, type Page, type PlaywrightTestArgs } from '@playwright/test';

// The task page `#/t/<ref>` at the phone and the desktop size: what it shows (header, description, planned window,
// links, dependencies, live leases, the event timeline) and the Edit, Done, Reopen and Close actions, each leaving its
// event in the timeline. Every test works on an initiative of its own.
const test = base;

const viewports = [
  { name: 'phone', size: { width: 390, height: 844 } },
  { name: 'desktop', size: { width: 1440, height: 900 } },
] as const;

const RESOLUTIONS = ['completed', 'dropped', 'superseded', 'duplicate'] as const;

async function post(api: APIRequestContext, path: string, data: object, status: number) {
  const response = await api.post(path, { data });
  expect(response.status(), `${path} ${await response.text()}`).toBe(status);
  return (await response.json()) as { ref: string };
}

async function apiContext(playwright: PlaywrightTestArgs['playwright'], baseURL: string | undefined) {
  return playwright.request.newContext({
    baseURL,
    extraHTTPHeaders: { Authorization: `Bearer ${process.env.E2E_API_KEY}`, 'Board-Actor': 'playwright' },
  });
}

// An open initiative with one task (and any extra fields); returns its ref.
async function seedTask(api: APIRequestContext, slug: string, extra: object = {}) {
  await post(api, '/api/v1/initiatives', { slug, title: `Page ${slug}` }, 201);
  return (await post(api, '/api/v1/tasks', { initiative: slug, title: 'Write the plan', ...extra }, 201)).ref;
}

const events = (page: Page) => page.getByTestId('event');
const eventTypes = (page: Page) => events(page).evaluateAll((els) => els.map((el) => el.getAttribute('data-type')));
const dialog = (page: Page, name: string) => page.getByRole('dialog', { name });
const actions = (page: Page) => page.getByTestId('task-actions');

for (const vp of viewports) {
  test.describe(`${vp.name} ${vp.size.width}x${vp.size.height}`, () => {
    test.use({ viewport: vp.size });

    test('the page shows the header, description, planned window, links, dependencies, live lease and timeline', async ({ page, playwright, baseURL }) => {
      const api = await apiContext(playwright, baseURL);
      const slug = `e2e-tp-show-${vp.name}`;
      const blocker = await seedTask(api, slug, {
        description: 'Plan with **bold** words and <b>raw html</b>.',
        planned_start: '2026-03-01T00:00:00Z',
        planned_end: '2026-04-15T00:00:00Z',
        external_refs: [{ label: 'Plan PR', url: 'https://example.com/pr/1' }],
      });
      const blocked = (await post(api, '/api/v1/tasks', { initiative: slug, title: 'Needs the plan', blocked_by: [blocker] }, 201)).ref;
      await post(api, '/api/v1/leases', { task: blocker, intent: 'drafting the plan', resources: ['docs/plan.md', 'docs/notes.md'] }, 201);

      // From the Tasks view the ref is a link to the page.
      await page.goto(`/#/tasks?initiative=${slug}`);
      await page.locator(`[data-testid=task-row][data-ref=${blocker}]`).getByRole('link', { name: blocker }).click();
      await expect(page).toHaveURL(new RegExp(`#/t/${blocker}$`));

      await expect(page.getByTestId('task-ref')).toHaveText(blocker);
      await expect(page.getByRole('heading', { level: 2, name: 'Write the plan' })).toBeVisible();
      await expect(page.getByTestId('task-status')).toHaveText('open');
      await expect(page.getByTestId('task-kind')).toHaveText('work');
      await expect(page.getByTestId('flag-active')).toHaveText('active');
      await expect(page.getByTestId('flag-blocked')).toHaveCount(0);
      await expect(page.getByTestId('task-initiative')).toHaveText(slug);
      const description = page.getByTestId('task-description');
      await expect(description.locator('strong')).toHaveText('bold');
      await expect(description.locator('b')).toHaveCount(0);
      await expect(description).toContainText('<b>raw html</b>');
      await expect(page.getByTestId('task-planned')).toHaveText('2026-03-01 to 2026-04-15');
      const link = page.getByTestId('task-links').getByRole('link', { name: 'Plan PR' });
      await expect(link).toHaveAttribute('href', 'https://example.com/pr/1');
      await expect(link).toHaveAttribute('rel', 'noopener noreferrer');

      // Blocking is linked and opens that task's page, where the dependency is shown the other way.
      await expect(page.getByTestId('no-deps')).toBeVisible();
      const blocking = page.getByTestId('task-blocking').locator(`[data-ref=${blocked}]`);
      await expect(blocking).toContainText('Needs the plan');
      await expect(blocking.getByRole('link')).toHaveAttribute('href', `#/t/${blocked}`);

      const lease = page.getByTestId('lease');
      await expect(lease).toHaveCount(1);
      await expect(lease.getByTestId('lease-holder')).toHaveText('playwright');
      await expect(lease.getByTestId('lease-intent')).toHaveText('drafting the plan');
      await expect(lease.getByTestId('lease-resources')).toHaveText('docs/notes.md, docs/plan.md');
      await expect(lease.getByTestId('lease-expires')).toHaveText(/^expires in (1h 59m|2h)$/);

      // The timeline: newest first, with relative time, summary and actor.
      await expect(eventTypes(page)).resolves.toEqual(['lease_started', 'task_created']);
      await expect(events(page).first().getByTestId('event-when')).toHaveText('just now');
      await expect(events(page).first().getByTestId('event-actor')).toHaveText('playwright');
      await expect(events(page).last().getByTestId('event-summary')).toHaveText(`task ${blocker} created`);

      await blocking.getByRole('link').click();
      await expect(page).toHaveURL(new RegExp(`#/t/${blocked}$`));
      await expect(page.getByTestId('task-ref')).toHaveText(blocked);
      await expect(page.getByTestId('flag-blocked')).toHaveText('blocked');
      const dep = page.getByTestId('task-deps').locator(`[data-ref=${blocker}]`);
      await expect(dep).toContainText('Write the plan');
      await expect(page.getByTestId('no-blocking')).toBeVisible();
      await expect(page.getByTestId('no-leases')).toBeVisible();

      const { scrollWidth, clientWidth } = await page.evaluate(() => ({
        scrollWidth: document.documentElement.scrollWidth,
        clientWidth: document.documentElement.clientWidth,
      }));
      expect(scrollWidth).toBeLessThanOrEqual(clientWidth);
      await api.dispose();
    });

    test('a task that does not exist says so, and the page survives a reload', async ({ page, playwright, baseURL }) => {
      const api = await apiContext(playwright, baseURL);
      const ref = await seedTask(api, `e2e-tp-miss-${vp.name}`);
      await page.goto('/#/t/zz-99');
      await expect(page.getByRole('alert')).toBeVisible();
      await expect(page.getByTestId('task-title')).toHaveCount(0);
      await page.goto(`/#/t/${ref}`);
      await page.reload();
      await expect(page.getByTestId('task-title')).toHaveText('Write the plan');
      await api.dispose();
    });

    test('the main actions sit in a bar above the tab bar on the phone and in the header on desktop', async ({ page, playwright, baseURL }) => {
      const api = await apiContext(playwright, baseURL);
      const ref = await seedTask(api, `e2e-tp-bar-${vp.name}`);
      await page.goto(`/#/t/${ref}`);
      await expect(actions(page).getByRole('button')).toHaveText(['Edit', 'Done', 'Close']);
      const bar = (await actions(page).boundingBox())!;
      const viewport = page.viewportSize()!;
      if (vp.name === 'phone') {
        const tabbar = (await page.getByTestId('tabbar').boundingBox())!;
        expect(bar.y + bar.height).toBeLessThanOrEqual(tabbar.y + 1);
        expect(viewport.height - bar.y - bar.height).toBeLessThan(viewport.height / 4);
        for (const button of await actions(page).getByRole('button').all()) expect((await button.boundingBox())!.height).toBeGreaterThanOrEqual(44);
      } else {
        const header = (await page.getByTestId('task-title').boundingBox())!;
        expect(bar.y).toBeGreaterThan(header.y);
        expect(bar.y).toBeLessThan(viewport.height / 2);
      }
      await api.dispose();
    });

    test('Edit changes the title, description and planned window, and a refused edit keeps what was typed', async ({ page, playwright, baseURL }) => {
      const api = await apiContext(playwright, baseURL);
      const ref = await seedTask(api, `e2e-tp-edit-${vp.name}`, { description: 'Old words.', planned_start: '2026-03-01T00:00:00Z' });
      await page.goto(`/#/t/${ref}`);
      await expect(page.getByTestId('task-planned')).toHaveText('from 2026-03-01');

      await actions(page).getByRole('button', { name: 'Edit' }).click();
      const edit = dialog(page, 'Edit task');
      await expect(edit).toBeVisible();
      await expect(edit.getByLabel('Title')).toHaveValue('Write the plan');
      await expect(edit.getByLabel('Description')).toHaveValue('Old words.');
      await expect(edit.getByLabel('Planned start')).toHaveValue('2026-03-01');
      await expect(edit.getByLabel('Cadence')).toHaveCount(0); // a work task has no cadence

      // A refusal (an empty title) shows inline, keeps the dialog open and the other input.
      await edit.getByLabel('Title').fill('');
      await edit.getByLabel('Description').fill('Kept words.');
      await edit.getByRole('button', { name: 'Save' }).click();
      await expect(edit.getByTestId('dialog-error')).toBeVisible();
      await expect(edit.getByLabel('Description')).toHaveValue('Kept words.');

      await edit.getByLabel('Title').fill('Write the final plan');
      await edit.getByLabel('Planned end').fill('2026-03-09');
      await edit.getByRole('button', { name: 'Save' }).click();
      await expect(edit).toHaveCount(0);
      await expect(page.getByTestId('toast')).toContainText(`Task ${ref} saved`);
      await expect(page.getByTestId('task-title')).toHaveText('Write the final plan');
      await expect(page.getByTestId('task-description')).toHaveText('Kept words.');
      await expect(page.getByTestId('task-planned')).toHaveText('2026-03-01 to 2026-03-09');
      await expect(eventTypes(page)).resolves.toEqual(['task_updated', 'task_created']);
      await expect(events(page).first().getByTestId('event-summary')).toHaveText(`task ${ref} updated: title, description, planned_end`);
      await expect(page.getByRole('button', { name: 'Edit' })).toBeFocused();
      await api.dispose();
    });

    test('Edit on a watch task changes its cadence, and a cadence that is not a number and a unit is refused', async ({ page, playwright, baseURL }) => {
      const api = await apiContext(playwright, baseURL);
      const ref = await seedTask(api, `e2e-tp-watch-${vp.name}`, { kind: 'watch', cadence_seconds: 900 });
      await page.goto(`/#/t/${ref}`);
      await expect(page.getByTestId('task-kind')).toHaveText('watch, every 15 minutes');

      await actions(page).getByRole('button', { name: 'Edit' }).click();
      const edit = dialog(page, 'Edit task');
      await expect(edit.getByLabel('Cadence')).toHaveValue('15m');
      await edit.getByLabel('Cadence').fill('soon');
      await edit.getByRole('button', { name: 'Save' }).click();
      await expect(edit.getByTestId('dialog-error')).toContainText('15m');
      await edit.getByLabel('Cadence').fill('2h');
      await edit.getByRole('button', { name: 'Save' }).click();
      await expect(edit).toHaveCount(0);
      await expect(page.getByTestId('task-kind')).toHaveText('watch, every 2 hours');
      await expect(events(page).first().getByTestId('event-summary')).toHaveText(`task ${ref} updated: cadence_seconds`);
      await api.dispose();
    });

    test('Done and Reopen move the task and leave their events, and a refused Done shows inline', async ({ page, playwright, baseURL }) => {
      const api = await apiContext(playwright, baseURL);
      const ref = await seedTask(api, `e2e-tp-done-${vp.name}`);
      await page.goto(`/#/t/${ref}`);

      await actions(page).getByRole('button', { name: 'Done' }).click();
      await expect(page.getByTestId('toast')).toContainText(`Task ${ref} marked done`);
      await expect(page.getByTestId('task-status')).toHaveText('done');
      await expect(actions(page).getByRole('button')).toHaveText(['Edit', 'Reopen', 'Close']);
      await expect(eventTypes(page)).resolves.toEqual(['task_done', 'task_created']);
      await expect(events(page).first().getByTestId('event-summary')).toHaveText(`task ${ref} marked done`);

      await actions(page).getByRole('button', { name: 'Reopen' }).click();
      await expect(page.getByTestId('toast')).toContainText(`Task ${ref} reopened`);
      await expect(page.getByTestId('task-status')).toHaveText('open');
      await expect(eventTypes(page)).resolves.toEqual(['task_reopened', 'task_done', 'task_created']);
      await expect(events(page).first().getByTestId('event-summary')).toHaveText(`task ${ref} reopened`);

      // Someone else finishes the task while this page is open: the stale Done is refused, inline, and the page is unchanged.
      await post(api, `/api/v1/tasks/${ref}/done`, {}, 200);
      await actions(page).getByRole('button', { name: 'Done' }).click();
      await expect(page.getByTestId('action-error')).toBeVisible();
      await expect(page.getByTestId('action-error')).toContainText('done');
      await expect(page.getByTestId('task-status')).toHaveText('open');
      await api.dispose();
    });

    for (const resolution of RESOLUTIONS) {
      test(`Close with the resolution ${resolution} closes the task and leaves its event, and Reopen undoes it`, async ({ page, playwright, baseURL }) => {
        const api = await apiContext(playwright, baseURL);
        const ref = await seedTask(api, `e2e-tp-close-${resolution.slice(0, 4)}-${vp.name}`);
        await page.goto(`/#/t/${ref}`);

        await actions(page).getByRole('button', { name: 'Close' }).click();
        const close = dialog(page, 'Close task');
        await expect(close).toBeVisible();
        const select = close.getByLabel('Resolution');
        expect(await select.locator('option').evaluateAll((os) => os.map((o) => (o as HTMLOptionElement).value))).toEqual([...RESOLUTIONS]);
        await expect(select).toHaveValue('completed');
        await select.selectOption(resolution);
        await close.getByRole('button', { name: 'Close task' }).click();

        await expect(close).toHaveCount(0);
        await expect(page.getByTestId('toast')).toContainText(`Task ${ref} closed as ${resolution}`);
        await expect(page.getByTestId('task-status')).toHaveText('closed');
        await expect(page.getByTestId('task-resolution')).toHaveText(resolution);
        await expect(actions(page).getByRole('button')).toHaveText(['Reopen']);
        await expect(eventTypes(page)).resolves.toEqual(['task_closed', 'task_created']);
        await expect(events(page).first().getByTestId('event-summary')).toHaveText(`task ${ref} closed as ${resolution}`);

        await actions(page).getByRole('button', { name: 'Reopen' }).click();
        await expect(page.getByTestId('task-status')).toHaveText('open');
        await expect(page.getByTestId('task-resolution')).toHaveCount(0);
        await expect(eventTypes(page)).resolves.toEqual(['task_reopened', 'task_closed', 'task_created']);
        await api.dispose();
      });
    }

    test('a refused Close shows inline in the confirmation and keeps it open', async ({ page, playwright, baseURL }) => {
      const api = await apiContext(playwright, baseURL);
      const ref = await seedTask(api, `e2e-tp-refuse-${vp.name}`);
      await page.goto(`/#/t/${ref}`);
      await actions(page).getByRole('button', { name: 'Close' }).click();
      const close = dialog(page, 'Close task');
      await close.getByLabel('Resolution').selectOption('duplicate');

      const closed = await api.post(`/api/v1/tasks/${ref}/close`, { data: { resolution: 'dropped' } });
      expect(closed.status()).toBe(200);
      await close.getByRole('button', { name: 'Close task' }).click();
      await expect(close.getByTestId('dialog-error')).toContainText('closed');
      await expect(close).toBeVisible();
      await expect(close.getByLabel('Resolution')).toHaveValue('duplicate');
      await close.getByRole('button', { name: 'Cancel' }).click();
      await expect(close).toHaveCount(0);
      await api.dispose();
    });
  });
}
