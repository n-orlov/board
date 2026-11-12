import { expect, test, type Page } from '@playwright/test';
import { SECTIONS } from '../web/src/lib/attention';

// R13 Attention layout (SPEC 11, Attention view, wide): the five sections are cards in a grid, one per row below 1536 px,
// two per row from 1536 px and three per row from 2304 px, in the §5.3 order row by row. Everything is measured by
// bounding box, never by pixels. The sections draw whether or not they hold items, so the spec needs no seeded data.
const FIVE_SECONDS = { timeout: 5000 };
const KEYS = SECTIONS.map((s) => s.key);

interface Box {
  key: string;
  x: number;
  y: number;
  width: number;
  height: number;
}

async function openAttention(page: Page) {
  await page.goto('/#/attention');
  await expect(page.getByRole('heading', { level: 3 })).toHaveCount(KEYS.length, FIVE_SECONDS);
}

async function boxes(page: Page): Promise<Box[]> {
  const out: Box[] = [];
  for (const key of KEYS) {
    const box = (await page.getByTestId(`section-${key}`).boundingBox())!;
    out.push({ key, ...box });
  }
  return out;
}

// The sections grouped into visual rows: those whose tops are within 2 px share a row, and a row reads left to right.
function rowsOf(list: Box[]): Box[][] {
  const rows: Box[][] = [];
  for (const box of [...list].sort((a, b) => a.y - b.y || a.x - b.x)) {
    const row = rows.find((r) => Math.abs(r[0].y - box.y) <= 2);
    if (row === undefined) rows.push([box]);
    else row.push(box);
  }
  return rows.map((r) => r.sort((a, b) => a.x - b.x));
}

// The order of the sections read row by row, left to right, is the §5.3 order.
function expectOrderKept(list: Box[]) {
  expect(rowsOf(list).flat().map((b) => b.key)).toEqual(KEYS);
}

function expectNoOverlap(list: Box[]) {
  for (const row of rowsOf(list)) {
    for (let n = 1; n < row.length; n++) expect(row[n].x).toBeGreaterThanOrEqual(row[n - 1].x + row[n - 1].width);
  }
}

const cases = [
  { name: 'wide 2560x1080', size: { width: 2560, height: 1080 }, perRow: [3, 2] },
  { name: 'ultrawide 3440x1440', size: { width: 3440, height: 1440 }, perRow: [3, 2] },
  { name: 'just below three columns 2303x1080', size: { width: 2303, height: 1080 }, perRow: [2, 2, 1] },
  { name: 'two columns start 1536x864', size: { width: 1536, height: 864 }, perRow: [2, 2, 1] },
  { name: 'just below two columns 1535x864', size: { width: 1535, height: 864 }, perRow: [1, 1, 1, 1, 1] },
  { name: 'desktop 1440x900', size: { width: 1440, height: 900 }, perRow: [1, 1, 1, 1, 1] },
] as const;

for (const c of cases) {
  test.describe(c.name, () => {
    test.use({ viewport: c.size });

    test(`the sections sit ${c.perRow.join(', ')} per row in section order`, async ({ page }) => {
      await openAttention(page);
      const list = await boxes(page);
      expect(rowsOf(list).map((r) => r.length)).toEqual([...c.perRow]);
      expectOrderKept(list);
      expectNoOverlap(list);
      // Within a row the cards share one width; a one-per-row layout stacks them down the page.
      for (const row of rowsOf(list)) {
        for (const b of row) expect(Math.abs(b.width - row[0].width)).toBeLessThanOrEqual(2);
      }
      const rows = rowsOf(list);
      for (let n = 1; n < rows.length; n++) expect(rows[n][0].y).toBeGreaterThan(rows[n - 1][0].y);
    });
  });
}

test.describe('wide 2560x1080 cards', () => {
  test.use({ viewport: { width: 2560, height: 1080 } });

  test('a section is a bordered card from 1536 px', async ({ page }) => {
    await openAttention(page);
    const style = await page.getByTestId('section-open_asks').evaluate((el) => {
      const s = getComputedStyle(el);
      return { border: parseFloat(s.borderTopWidth), padding: parseFloat(s.paddingTop) };
    });
    expect(style.border).toBeGreaterThanOrEqual(1);
    expect(style.padding).toBeGreaterThan(0);
  });
});

test.describe('ultrawide 3440x1440 cap', () => {
  test.use({ viewport: { width: 3440, height: 1440 } });

  test('the grid is at most 2560 px wide and centred in the main column', async ({ page }) => {
    await openAttention(page);
    const grid = (await page.getByTestId('attention-sections').boundingBox())!;
    expect(grid.width).toBeLessThanOrEqual(2560);
    const column = await page.locator('#main').evaluate((el) => {
      const box = el.getBoundingClientRect();
      const style = getComputedStyle(el);
      return { left: box.left + parseFloat(style.paddingLeft), right: box.right - parseFloat(style.paddingRight) };
    });
    const left = grid.x - column.left;
    const right = column.right - (grid.x + grid.width);
    expect(left).toBeGreaterThan(0);
    expect(Math.abs(left - right)).toBeLessThanOrEqual(2);
  });
});
