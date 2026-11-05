import { expect, test as base } from '@playwright/test';

// Signs in through the mock provider, so no API key rides along (see login.spec.ts).
const test = base;

test('an initiative created on the page is listed with its tags, and survives a reload', async ({ page }) => {
  await page.goto('/');
  await expect(page.getByRole('heading', { level: 2, name: 'Initiatives' })).toBeVisible();
  const row = page.locator('[data-testid=initiative-row][data-slug=e2e-created]');
  await expect(row).toHaveCount(0);

  await page.getByLabel('Slug').fill('e2e-created');
  await page.getByLabel('Title').fill('Created from the page');
  await page.getByLabel('Tags').fill('web, e2e');
  await page.getByRole('button', { name: 'Create initiative' }).click();

  await expect(row).toHaveCount(1);
  await expect(row).toContainText('Created from the page');
  await expect(row.getByTestId('initiative-status')).toHaveText('open');
  await expect(row.getByTestId('initiative-tags')).toHaveText('e2e, web');
  await expect(page.getByRole('alert')).toHaveCount(0);
  // The form is cleared for the next one.
  await expect(page.getByLabel('Slug')).toHaveValue('');

  await page.reload();
  await expect(row).toHaveCount(1);
});

test('a slug the server refuses is shown on the form and nothing is listed', async ({ page }) => {
  await page.goto('/');
  await expect(page.getByRole('heading', { level: 2, name: 'Initiatives' })).toBeVisible();
  await page.getByLabel('Slug').fill('Bad Slug!');
  await page.getByLabel('Title').fill('Refused');
  await page.getByRole('button', { name: 'Create initiative' }).click();

  await expect(page.getByRole('alert')).toContainText('slug');
  await expect(page.locator('[data-testid=initiative-row][data-slug="Bad Slug!"]')).toHaveCount(0);
  // What was typed stays, so it can be corrected.
  await expect(page.getByLabel('Slug')).toHaveValue('Bad Slug!');

  await page.getByLabel('Slug').fill('e2e-fixed');
  await page.getByRole('button', { name: 'Create initiative' }).click();
  await expect(page.locator('[data-testid=initiative-row][data-slug=e2e-fixed]')).toHaveCount(1);
  await expect(page.getByRole('alert')).toHaveCount(0);
});
