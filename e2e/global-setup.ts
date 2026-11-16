import { execFileSync } from 'node:child_process';
import { join } from 'node:path';

// Mints the API key every spec authenticates with, at test time, with the same
// `vh admin keys create` an operator runs: against the store of the server
// serve.sh started, which is up by now. The key lives in this run's
// environment only; nothing commits it.
export default function globalSetup(): void {
  const work = process.env.E2E_WORK;
  if (!work) throw new Error('E2E_WORK is not set (playwright.config.ts sets it)');
  const env: NodeJS.ProcessEnv = {};
  for (const [name, value] of Object.entries(process.env)) {
    if (!name.startsWith('BOARD_')) env[name] = value;
  }
  env.BOARD_DB_URL = `sqlite://${join(work, 'board.db')}`;
  const out = execFileSync(join(work, 'vh'), ['admin', 'keys', 'create', 'e2e'], { env, encoding: 'utf8' });
  const key = out.trim().split(/\s+/).pop() ?? '';
  if (!/^brd_[0-9a-z]{52}$/.test(key)) throw new Error('vh admin keys create did not print a key');
  process.env.E2E_API_KEY = key;
}
