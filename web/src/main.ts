import './styles.css';
import { mount } from 'svelte';
import App from './App.svelte';
import { installScrollRestore } from './lib/historyEntry';

// Back and Forward return to where each history entry was scrolled (lib/historyEntry.ts).
installScrollRestore();
mount(App, { target: document.getElementById('app')! });
