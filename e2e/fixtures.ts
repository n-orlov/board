import { test as base } from '@playwright/test';

// Every spec imports test from here: each request the page or the API client
// sends carries the key global-setup.ts minted and names its actor. Only
// GET /healthz is open, so nothing works without them.
export const test = base.extend({
  extraHTTPHeaders: async ({}, use) => {
    const key = process.env.E2E_API_KEY;
    if (!key) throw new Error('E2E_API_KEY is not set (global-setup.ts mints it)');
    await use({ Authorization: `Bearer ${key}`, 'Board-Actor': 'playwright' });
  },
});

export { expect } from '@playwright/test';
