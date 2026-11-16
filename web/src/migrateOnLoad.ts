// Runs the board.* -> vh.* browser-storage migration (lib/storageMigration.ts) as a side effect of being imported. ES
// modules evaluate in import order, and themeController.ts reads the theme while it is being imported, so main.ts and
// themeController.ts both import this first: the migration is done before any theme, view state or entry state is read.
import { migrateBrowserStorage } from './lib/storageMigration';

migrateBrowserStorage();
