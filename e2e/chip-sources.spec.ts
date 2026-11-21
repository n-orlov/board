import { expect, test, type Page } from '@playwright/test';
import { actorApi, created } from './now';
import { openChips, pickTag, tagChip } from './chips';

// R7/R10 (SPEC 11, "Tag choices and saved filters on the task views"): Now and Timeline offer every tag of the tasks they show
// (own, initiative and derived actor kind, with their sources), and a restored filter keeps a tag a task carries even when no
// initiative lists it, the same rule the server filters by. Each test seeds an initiative of its own on the shared board.
const viewports = [
  { name: 'phone', size: { width: 390, height: 844 } },
  { name: 'desktop', size: { width: 1440, height: 900 } },
] as const;
const views = [
  { id: 'now', key: 'vh.view.v1.attention', endpoint: '/api/v1/attention/now' },
  { id: 'timeline', key: 'vh.view.v1.timeline', endpoint: '/api/v1/timeline' },
] as const;

async function seed(playwright: Parameters<typeof actorApi>[0], baseURL: string | undefined, slug: string, own: string[], lease = true) {
  const api = await actorApi(playwright, baseURL, 'ralph:review');
  await created(api, '/api/v1/initiatives', { slug, title: slug, tags: [`${slug}-ini`] });
  const { ref } = await created(api, '/api/v1/tasks', { initiative: slug, title: `task of ${slug}`, tags: own });
  if (lease) await created(api, '/api/v1/leases', { task: ref, intent: 'review' });
  await api.dispose();
  return ref;
}

// Two tasks of one initiative, each with tags of its own; the first is leased when `lease` (a derived actor-kind tag).
async function seedTwo(playwright: Parameters<typeof actorApi>[0], baseURL: string | undefined, slug: string, first: string[], second: string[], lease = false) {
  const api = await actorApi(playwright, baseURL, 'ralph:review');
  await created(api, '/api/v1/initiatives', { slug, title: slug });
  const a = await created(api, '/api/v1/tasks', { initiative: slug, title: 'first', tags: first });
  await created(api, '/api/v1/tasks', { initiative: slug, title: 'second', tags: second });
  if (lease) await created(api, '/api/v1/leases', { task: a.ref, intent: 'review' });
  await api.dispose();
}

// A bare navigation to the view after another one: the saved query is restored, and the read it makes is returned.
async function restoredRead(page: Page, view: (typeof views)[number], slug: string, query: string) {
  await page.addInitScript(({ key, query }) => localStorage.setItem(key, JSON.stringify({ v: 1, query })), { key: view.key, query });
  await page.goto('/#/actors');
  await expect(page.getByTestId('actors-view')).toBeVisible();
  const read = page.waitForResponse((r) => new URL(r.url()).pathname === view.endpoint && new URL(r.url()).searchParams.get('initiative') === slug);
  await page.evaluate((id) => (window.location.hash = `#/${id}`), view.id);
  return new URL((await read).url());
}

for (const viewport of viewports) {
  for (const view of views) {
    test.describe(`${view.id} at ${viewport.name}`, () => {
      test.beforeEach(async ({ page }) => page.setViewportSize(viewport.size));

      test('offers the task\'s own, its initiative\'s and its actor-kind tag, each with its source', async ({ page, playwright, baseURL }) => {
        const slug = `src-offer-${view.id}-${viewport.name}`;
        await seed(playwright, baseURL, slug, [`${slug}-own`]);
        await page.goto(`/#/${view.id}?initiative=${slug}`);
        await openChips(page);
        await expect(tagChip(page, `${slug}-ini`)).toBeVisible();
        await expect(tagChip(page, `${slug}-own`)).toHaveAttribute('aria-label', /task tag/);
        await expect(tagChip(page, `${slug}-ini`)).toHaveAttribute('aria-label', /initiative tag/);
        await expect(tagChip(page, 'ralph')).toHaveAttribute('aria-label', /actor kind/);
      });

      test('keeps the other chips on offer after one is chosen', async ({ page, playwright, baseURL }) => {
        const slug = `src-keep-${view.id}-${viewport.name}`;
        await seed(playwright, baseURL, slug, [`${slug}-own`]);
        await page.goto(`/#/${view.id}?initiative=${slug}`);
        await openChips(page);
        await pickTag(page, `${slug}-own`);
        await expect(tagChip(page, 'ralph')).toBeVisible();
        await expect(tagChip(page, `${slug}-ini`)).toBeVisible();
      });

      test('restores a task-only tag, with its initiative', async ({ page, playwright, baseURL }) => {
        const slug = `src-own-${view.id}-${viewport.name}`;
        await seed(playwright, baseURL, slug, [`${slug}-own`]);
        const url = await restoredRead(page, view, slug, `initiative=${slug}&tag=${slug}-own`);
        expect(url.searchParams.getAll('tag')).toEqual([`${slug}-own`]);
      });

      test('restores an actor-kind tag no initiative lists', async ({ page, playwright, baseURL }) => {
        const slug = `src-actor-${view.id}-${viewport.name}`;
        await seed(playwright, baseURL, slug, []);
        const url = await restoredRead(page, view, slug, `initiative=${slug}&tag=ralph`);
        expect(url.searchParams.getAll('tag')).toEqual(['ralph']);
      });

      // A chip names the sources its tag has on the tasks, not on the rows the combined filter keeps: with an intersection that keeps
      // none the view has no rows to read them from, so the sources come from the tasks that carry the tag.
      test('names the task source of two restored task-only tags whose intersection is empty', async ({ page, playwright, baseURL }) => {
        const slug = `src-empty-${view.id}-${viewport.name}`;
        await seedTwo(playwright, baseURL, slug, [`${slug}-left`], [`${slug}-right`]);
        const url = await restoredRead(page, view, slug, `initiative=${slug}&tag=${slug}-left&tag=${slug}-right`);
        expect(url.searchParams.getAll('tag')).toEqual([`${slug}-left`, `${slug}-right`]);
        await openChips(page);
        for (const side of ['left', 'right']) {
          const chip = tagChip(page, `${slug}-${side}`);
          await expect(chip).toHaveAttribute('aria-pressed', 'true');
          await expect(chip).toHaveAttribute('aria-label', `${slug}-${side}, task tag`);
          await expect(chip).toHaveAttribute('title', `${slug}-${side}: task tag`);
        }
      });

      test('names every source of a restored tag an initiative lists and another task owns, with an empty intersection', async ({ page, playwright, baseURL }) => {
        const slug = `src-both-${view.id}-${viewport.name}`;
        const api = await actorApi(playwright, baseURL, 'ralph:review');
        await created(api, '/api/v1/initiatives', { slug: `${slug}-x`, title: 'x', tags: [`${slug}-shared`] });
        await created(api, '/api/v1/initiatives', { slug, title: slug });
        await created(api, '/api/v1/tasks', { initiative: slug, title: 'shared', tags: [`${slug}-shared`] });
        await created(api, '/api/v1/tasks', { initiative: slug, title: 'other', tags: [`${slug}-other`] });
        await api.dispose();
        await restoredRead(page, view, slug, `initiative=${slug}&tag=${slug}-shared&tag=${slug}-other`);
        await openChips(page);
        await expect(tagChip(page, `${slug}-shared`)).toHaveAttribute('aria-label', `${slug}-shared, task tag, initiative tag`);
        await expect(tagChip(page, `${slug}-other`)).toHaveAttribute('aria-label', `${slug}-other, task tag`);
      });

      test('names the actor kind of a restored derived tag next to a task-only tag, with an empty intersection', async ({ page, playwright, baseURL }) => {
        const slug = `src-kind-${view.id}-${viewport.name}`;
        await seedTwo(playwright, baseURL, slug, [`${slug}-leased`], [`${slug}-bare`], true);
        const url = await restoredRead(page, view, slug, `initiative=${slug}&tag=ralph&tag=${slug}-bare`);
        expect(url.searchParams.getAll('tag')).toEqual(['ralph', `${slug}-bare`]);
        await openChips(page);
        await expect(tagChip(page, 'ralph')).toHaveAttribute('aria-label', /^ralph, actor kind/);
        await expect(tagChip(page, `${slug}-bare`)).toHaveAttribute('aria-label', `${slug}-bare, task tag`);
      });

      test('names the sources of a linked (not restored) pair of tags with an empty intersection', async ({ page, playwright, baseURL }) => {
        const slug = `src-link-${view.id}-${viewport.name}`;
        await seedTwo(playwright, baseURL, slug, [`${slug}-left`], [`${slug}-right`]);
        await page.goto(`/#/${view.id}?initiative=${slug}&tag=${slug}-left&tag=${slug}-right`);
        await openChips(page);
        await expect(tagChip(page, `${slug}-left`)).toHaveAttribute('aria-label', `${slug}-left, task tag`);
        await expect(tagChip(page, `${slug}-right`)).toHaveAttribute('aria-label', `${slug}-right, task tag`);
      });

      test('keeps the sources of the restored chips across navigation away and back', async ({ page, playwright, baseURL }) => {
        const slug = `src-nav-${view.id}-${viewport.name}`;
        await seedTwo(playwright, baseURL, slug, [`${slug}-left`], [`${slug}-right`]);
        await restoredRead(page, view, slug, `initiative=${slug}&tag=${slug}-left&tag=${slug}-right`);
        await page.evaluate(() => (window.location.hash = '#/actors'));
        await expect(page.getByTestId('actors-view')).toBeVisible();
        await page.evaluate((id) => (window.location.hash = `#/${id}`), view.id);
        await openChips(page);
        await expect(tagChip(page, `${slug}-left`)).toHaveAttribute('aria-label', `${slug}-left, task tag`);
        await expect(tagChip(page, `${slug}-right`)).toHaveAttribute('aria-pressed', 'true');
      });

      test('still drops a restored tag nothing carries beside a valid pair with an empty intersection', async ({ page, playwright, baseURL }) => {
        const slug = `src-ghost-${view.id}-${viewport.name}`;
        await seedTwo(playwright, baseURL, slug, [`${slug}-left`], [`${slug}-right`]);
        const url = await restoredRead(page, view, slug, `initiative=${slug}&tag=${slug}-left&tag=${slug}-ghost&tag=${slug}-right`);
        expect(url.searchParams.getAll('tag')).toEqual([`${slug}-left`, `${slug}-right`]);
      });

      test('restores a task-only and an initiative tag together and still drops a tag nothing carries', async ({ page, playwright, baseURL }) => {
        const slug = `src-mix-${view.id}-${viewport.name}`;
        await seed(playwright, baseURL, slug, [`${slug}-own`]);
        const url = await restoredRead(page, view, slug, `initiative=${slug}&tag=${slug}-ini&tag=${slug}-own&tag=${slug}-nothing`);
        expect(url.searchParams.getAll('tag')).toEqual([`${slug}-ini`, `${slug}-own`]);
      });
    });
  }
}
