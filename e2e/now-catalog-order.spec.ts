import { expect, test, type Page } from '@playwright/test';
import { actorApi, created, openFold } from './now';
import { initiativeChip, openChips, showAllInitiatives, tagChip } from './chips';

// Now reads the initiative catalog again on every initiative event (PRD R1), so two reads can overlap and land out of order.
// The filter choices follow the newest catalog: a read that lands after a newer one was shown is dropped, whether it is a live
// read or the page's first. Each test holds one read's answer by hand, lets a newer one land, then releases the held one.
// Each test seeds its own initiatives and tags (one shared store).
const streamUp = (page: Page) => page.waitForResponse((r) => r.headers()['content-type']?.includes('text/event-stream') === true);

// The catalog is read page by page (100 a page) and a new initiative sorts last, so the page that ends a read is the one
// that carries it: that is the page held. The browser counts the catalog reads it has finished parsing (the ending pages).
const COUNT_READS = () => {
  const w = window as unknown as { __catalogReads: number };
  w.__catalogReads = 0;
  const json = Response.prototype.json;
  Response.prototype.json = async function (this: Response) {
    const body = await json.call(this);
    if (new URL(this.url).pathname === '/api/v1/initiatives' && body?.next_after == null) w.__catalogReads += 1;
    return body;
  };
};
const readsDone = (page: Page) => page.evaluate(() => (window as unknown as { __catalogReads: number }).__catalogReads);

interface Held {
  /** The body of the held page, once the server answered it. */
  captured: Promise<{ items: { slug: string; tags: string[] }[] }>;
  /** Hands the held page to the page and waits until the read it ends has been parsed and its outcome applied. */
  release: () => Promise<void>;
}

async function holdNextRead(page: Page): Promise<Held> {
  const captured = Promise.withResolvers<{ items: { slug: string; tags: string[] }[] }>();
  const gate = Promise.withResolvers<void>();
  let armed = true;
  await page.route(
    (url) => url.pathname === '/api/v1/initiatives',
    async (route) => {
      if (!armed) return route.continue();
      const response = await route.fetch();
      const body = await response.json();
      if (body.next_after != null) return route.fulfill({ response });
      armed = false;
      captured.resolve(body);
      await gate.promise;
      await route.fulfill({ response });
    },
  );
  return {
    captured: captured.promise,
    release: async () => {
      const before = await readsDone(page);
      gate.resolve();
      await page.waitForFunction((n) => (window as unknown as { __catalogReads: number }).__catalogReads > n, before);
      // A task boundary: the microtasks that apply the parsed answer to the view have all run.
      await page.evaluate(() => new Promise((done) => setTimeout(done, 0)));
    },
  };
}

async function openNow(page: Page, hash: string): Promise<void> {
  const ready = streamUp(page);
  await page.goto(hash);
  await ready;
  await openChips(page);
}

const viewports = [
  { name: 'phone', size: { width: 390, height: 844 } },
  { name: 'desktop', size: { width: 1440, height: 900 } },
] as const;

for (const vp of viewports) {
  test.describe(`${vp.name} ${vp.size.width}x${vp.size.height}`, () => {
    test.use({ viewport: vp.size });
    test.beforeEach(async ({ page }) => {
      await page.addInitScript(COUNT_READS);
    });

    test('an older live catalog answer cannot bring back a dropped tag nor take away the current one', async ({ page, playwright, baseURL }, testInfo) => {
      const n = `${vp.name}${testInfo.repeatEachIndex}`;
      const slug = `e2e-catorder-a-${n}`;
      const selected = `catorder-sel-${n}`;
      const stale = `catorder-stale-${n}`;
      const current = `catorder-cur-${n}`;
      const api = await actorApi(playwright, baseURL, 'pi:catorder');
      await created(api, '/api/v1/initiatives', { slug, title: slug, tags: [selected] });
      const task = await created(api, '/api/v1/tasks', { initiative: slug, title: 'changes filter membership' });
      const hash = `#/now?initiative=${slug}&tag=${selected}`;
      await openNow(page, `/${hash}`);
      await openFold(page, 'recent');
      const row = page.locator(`[data-testid="recent-row"][data-task="${task.ref}"]`);
      await expect(row).toBeVisible();
      await expect(tagChip(page, selected)).toHaveAttribute('aria-label', `${selected}, initiative tag`);
      const timeOrigin = await page.evaluate(() => performance.timeOrigin);

      const held = await holdNextRead(page);
      expect((await api.patch(`/api/v1/initiatives/${slug}`, { data: { tags: [stale] } })).ok()).toBe(true);
      expect((await held.captured).items).toContainEqual(expect.objectContaining({ slug, tags: [stale] }));
      await expect(row).toHaveCount(0);
      expect((await api.patch(`/api/v1/initiatives/${slug}`, { data: { tags: [selected, current] } })).ok()).toBe(true);
      await expect(row).toBeVisible();
      await expect(tagChip(page, current)).toHaveAttribute('aria-label', `${current}, initiative tag`);
      await expect(tagChip(page, stale)).toHaveCount(0);

      await held.release();
      expect(await tagChip(page, stale).count()).toBe(0);
      await expect(tagChip(page, current)).toHaveAttribute('aria-label', `${current}, initiative tag`);
      await expect(tagChip(page, selected)).toHaveAttribute('aria-pressed', 'true');
      await expect(tagChip(page, selected)).toHaveAttribute('aria-label', `${selected}, initiative tag`);
      await expect(row).toBeVisible();
      expect(new URL(page.url()).hash).toBe(hash);
      expect(await page.evaluate(() => performance.timeOrigin)).toBe(timeOrigin);
      await api.dispose();
    });

    test('an older live catalog answer cannot take away an initiative made after it', async ({ page, playwright, baseURL }, testInfo) => {
      const n = `${vp.name}${testInfo.repeatEachIndex}`;
      const chosen = `e2e-catorder-b-${n}`;
      const first = `e2e-catorder-c-${n}`;
      const second = `e2e-catorder-d-${n}`;
      const api = await actorApi(playwright, baseURL, 'pi:catorder');
      await created(api, '/api/v1/initiatives', { slug: chosen, title: chosen });
      const hash = `#/now?initiative=${chosen}`;
      await openNow(page, `/${hash}`);
      await showAllInitiatives(page);
      await expect(initiativeChip(page, chosen)).toHaveAttribute('aria-pressed', 'true');

      const held = await holdNextRead(page);
      await created(api, '/api/v1/initiatives', { slug: first, title: first });
      const old = (await held.captured).items.map((i) => i.slug);
      expect(old).toContain(first);
      expect(old).not.toContain(second);
      await created(api, '/api/v1/initiatives', { slug: second, title: second });
      await showAllInitiatives(page);
      await expect(initiativeChip(page, second)).toBeVisible();
      await expect(initiativeChip(page, first)).toBeVisible();

      await held.release();
      await showAllInitiatives(page);
      expect(await initiativeChip(page, second).count()).toBe(1);
      await expect(initiativeChip(page, second)).toHaveAttribute('aria-pressed', 'false');
      await expect(initiativeChip(page, first)).toBeVisible();
      await expect(initiativeChip(page, chosen)).toHaveAttribute('aria-pressed', 'true');
      expect(new URL(page.url()).hash).toBe(hash);
      await api.dispose();
    });

    test("the page's first catalog read landing after a live one cannot undo it", async ({ page, playwright, baseURL }, testInfo) => {
      const n = `${vp.name}${testInfo.repeatEachIndex}`;
      const slug = `e2e-catorder-e-${n}`;
      const before = `catorder-before-${n}`;
      const after = `catorder-after-${n}`;
      const api = await actorApi(playwright, baseURL, 'pi:catorder');
      await created(api, '/api/v1/initiatives', { slug, title: slug, tags: [before] });
      // Armed before the first navigation: the read held is the page's own first one.
      const held = await holdNextRead(page);
      const hash = `#/now?initiative=${slug}`;
      await openNow(page, `/${hash}`);
      expect((await held.captured).items).toContainEqual(expect.objectContaining({ slug, tags: [before] }));
      await expect(initiativeChip(page, slug)).toHaveAttribute('aria-pressed', 'true');

      expect((await api.patch(`/api/v1/initiatives/${slug}`, { data: { tags: [after] } })).ok()).toBe(true);
      await expect(tagChip(page, after)).toHaveAttribute('aria-label', `${after}, initiative tag`);
      await expect(tagChip(page, before)).toHaveCount(0);

      await held.release();
      expect(await tagChip(page, before).count()).toBe(0);
      await expect(tagChip(page, after)).toHaveAttribute('aria-label', `${after}, initiative tag`);
      await expect(initiativeChip(page, slug)).toHaveAttribute('aria-pressed', 'true');
      expect(new URL(page.url()).hash).toBe(hash);
      await api.dispose();
    });
  });
}
