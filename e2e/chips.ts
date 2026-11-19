import { expect, type Locator, type Page } from '@playwright/test';

// Helpers for the shared chip filter on Tasks (web/src/ChipFilter.svelte): its testids and how a test reaches a chip.
// On a phone the filter is folded behind one line; a long list of initiatives folds behind "more".

export const initiativeChip = (page: Page, slug: string): Locator => page.locator(`[data-testid="chip-initiative"][data-initiative="${slug}"]`);
export const tagChip = (page: Page, tag: string): Locator => page.locator(`[data-testid="chip-tag"][data-tag="${tag}"]`);

/** Opens the phone's folded filter; a no-op where the filter is always open. */
export async function openChips(page: Page): Promise<void> {
  const toggle = page.getByTestId('chip-toggle');
  if ((await toggle.isVisible()) && (await toggle.getAttribute('aria-expanded')) === 'false') await toggle.click();
}

/** Folds the phone's filter again; a no-op where the filter is always open. */
export async function foldChips(page: Page): Promise<void> {
  const toggle = page.getByTestId('chip-toggle');
  if ((await toggle.isVisible()) && (await toggle.getAttribute('aria-expanded')) === 'true') await toggle.click();
}

/** Shows every initiative chip (opens "more" when the list is folded). */
export async function showAllInitiatives(page: Page): Promise<void> {
  await openChips(page);
  await expect(page.getByTestId('chip-initiatives')).toBeVisible();
  const more = page.getByTestId('chip-more');
  if ((await more.count()) > 0 && (await more.getAttribute('aria-expanded')) === 'false') await more.click();
}

/** The chip of an initiative, scrolled into reach: the filter opened and the list unfolded when it is not on show. */
export async function revealInitiative(page: Page, slug: string): Promise<Locator> {
  await openChips(page);
  const chip = initiativeChip(page, slug);
  await expect(async () => {
    if ((await chip.count()) === 0) await showAllInitiatives(page);
    await expect(chip).toBeVisible({ timeout: 500 });
  }).toPass({ timeout: 10_000 });
  return chip;
}

export async function pickInitiative(page: Page, slug: string): Promise<void> {
  await (await revealInitiative(page, slug)).click();
}

/** The initiatives the chips show as chosen, sorted. */
export async function chosenInitiatives(page: Page): Promise<string[]> {
  const chips = page.locator('[data-testid="chip-initiative"][aria-pressed="true"]');
  const slugs = await chips.evaluateAll((els) => els.map((el) => el.getAttribute('data-initiative') ?? ''));
  return slugs.sort();
}

/** Asserts exactly these initiatives are chosen (an empty list is "any initiative"). */
export async function expectInitiatives(page: Page, slugs: string[]): Promise<void> {
  await expect.poll(() => chosenInitiatives(page)).toEqual([...slugs].sort());
}

export async function pickTag(page: Page, tag: string): Promise<void> {
  await openChips(page);
  const chip = tagChip(page, tag);
  await expect(chip).toBeVisible();
  await chip.click();
}

/** The tags the chips show as chosen, sorted. */
export async function chosenTags(page: Page): Promise<string[]> {
  const chips = page.locator('[data-testid="chip-tag"][aria-pressed="true"]');
  const tags = await chips.evaluateAll((els) => els.map((el) => el.getAttribute('data-tag') ?? ''));
  return tags.sort();
}

/** Asserts exactly these tags are chosen (an empty list is "any tag"). */
export async function expectTags(page: Page, tags: string[]): Promise<void> {
  await expect.poll(() => chosenTags(page)).toEqual([...tags].sort());
}
