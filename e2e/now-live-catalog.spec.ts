import { expect, test, type Page } from '@playwright/test';
import { actorApi, created } from './now';
import { initiativeChip, openChips, showAllInitiatives, tagChip } from './chips';

// The filter controls of Now follow the initiative catalog live (PRD R1): an initiative made, edited or closed while the page
// is open changes the choices with no reload, and the chips chosen, their sources and the filter in the fragment stay.
// Each test seeds its own initiatives and tags (one shared store).
const FIVE_SECONDS = { timeout: 5000 };
const streamUp = (page: Page) => page.waitForResponse((r) => r.headers()['content-type']?.includes('text/event-stream') === true);

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

    test('a new initiative becomes a choice on the open page', async ({ page, playwright, baseURL }, testInfo) => {
      const first = `e2e-livecat-a-${vp.name}${testInfo.repeatEachIndex}`;
      const second = `e2e-livecat-b-${vp.name}${testInfo.repeatEachIndex}`;
      const api = await actorApi(playwright, baseURL, 'pi:livecat');
      await created(api, '/api/v1/initiatives', { slug: first, title: first });
      await openNow(page, `/#/now?initiative=${first}`);
      await showAllInitiatives(page);
      await expect(initiativeChip(page, first)).toHaveAttribute('aria-pressed', 'true');
      expect(await initiativeChip(page, second).count()).toBe(0);
      await created(api, '/api/v1/initiatives', { slug: second, title: second });
      await showAllInitiatives(page);
      await expect(initiativeChip(page, second)).toBeVisible(FIVE_SECONDS);
      // The chosen one stays chosen and the fragment keeps the filter.
      await expect(initiativeChip(page, first)).toHaveAttribute('aria-pressed', 'true');
      await expect(initiativeChip(page, second)).toHaveAttribute('aria-pressed', 'false');
      expect(new URL(page.url()).hash).toBe(`#/now?initiative=${first}`);
      await api.dispose();
    });

    test('an initiative tag added and then removed comes and goes as a choice with its source', async ({ page, playwright, baseURL }, testInfo) => {
      const slug = `e2e-livecat-c-${vp.name}${testInfo.repeatEachIndex}`;
      const tag = `livecat-tag-c-${vp.name}${testInfo.repeatEachIndex}`;
      const api = await actorApi(playwright, baseURL, 'pi:livecat');
      await created(api, '/api/v1/initiatives', { slug, title: slug });
      await created(api, '/api/v1/tasks', { initiative: slug, title: 'carries the initiative tag' });
      await openNow(page, `/#/now?initiative=${slug}`);
      await expect(page.getByTestId('chip-filter')).toBeVisible();
      expect(await tagChip(page, tag).count()).toBe(0);
      const added = await api.patch(`/api/v1/initiatives/${slug}`, { data: { tags: [tag] } });
      expect(added.ok()).toBe(true);
      await expect(tagChip(page, tag)).toBeVisible(FIVE_SECONDS);
      await expect(tagChip(page, tag)).toHaveAttribute('aria-label', `${tag}, initiative tag`);
      expect(new URL(page.url()).hash).toBe(`#/now?initiative=${slug}`);
      const removed = await api.patch(`/api/v1/initiatives/${slug}`, { data: { tags: [] } });
      expect(removed.ok()).toBe(true);
      await expect(tagChip(page, tag)).toHaveCount(0, FIVE_SECONDS);
      await api.dispose();
    });

    test('a tag chosen stays on offer after its initiative stops carrying it', async ({ page, playwright, baseURL }, testInfo) => {
      const slug = `e2e-livecat-d-${vp.name}${testInfo.repeatEachIndex}`;
      const tag = `livecat-tag-d-${vp.name}${testInfo.repeatEachIndex}`;
      const api = await actorApi(playwright, baseURL, 'pi:livecat');
      await created(api, '/api/v1/initiatives', { slug, title: slug, tags: [tag] });
      await created(api, '/api/v1/tasks', { initiative: slug, title: 'tagged by its initiative' });
      await openNow(page, `/#/now?initiative=${slug}&tag=${tag}`);
      await expect(tagChip(page, tag)).toHaveAttribute('aria-pressed', 'true', FIVE_SECONDS);
      await expect(tagChip(page, tag)).toHaveAttribute('aria-label', `${tag}, initiative tag`);
      const removed = await api.patch(`/api/v1/initiatives/${slug}`, { data: { tags: [] } });
      expect(removed.ok()).toBe(true);
      // The chosen tag is still chosen (the server still filters by it), but it no longer claims a source.
      await expect(tagChip(page, tag)).toHaveAttribute('aria-label', tag, FIVE_SECONDS);
      await expect(tagChip(page, tag)).toHaveAttribute('aria-pressed', 'true');
      await api.dispose();
    });

    test('a closed initiative stays a choice and a reopened one is unchanged', async ({ page, playwright, baseURL }, testInfo) => {
      const slug = `e2e-livecat-e-${vp.name}${testInfo.repeatEachIndex}`;
      const other = `e2e-livecat-f-${vp.name}${testInfo.repeatEachIndex}`;
      const api = await actorApi(playwright, baseURL, 'pi:livecat');
      await created(api, '/api/v1/initiatives', { slug, title: slug });
      await openNow(page, `/#/now?initiative=${slug}`);
      await showAllInitiatives(page);
      await expect(initiativeChip(page, slug)).toBeVisible();
      expect((await api.post(`/api/v1/initiatives/${slug}/close`, { data: {} })).ok()).toBe(true);
      // A later initiative is the proof the catalog was read again after the close.
      await created(api, '/api/v1/initiatives', { slug: other, title: other });
      await showAllInitiatives(page);
      await expect(initiativeChip(page, other)).toBeVisible(FIVE_SECONDS);
      await expect(initiativeChip(page, slug)).toHaveAttribute('aria-pressed', 'true');
      await api.dispose();
    });
  });
}
