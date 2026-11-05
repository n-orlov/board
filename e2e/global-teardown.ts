import { rmSync } from 'node:fs';

// Removes the directory serve.sh and global-setup.ts used: binary, store, key.
export default function globalTeardown(): void {
  const work = process.env.E2E_WORK;
  if (work) rmSync(work, { recursive: true, force: true });
}
