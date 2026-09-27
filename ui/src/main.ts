import '@fontsource-variable/inter';
import '@fontsource-variable/jetbrains-mono';
import './styles/tokens.css';
import './styles/base.css';
import { mount } from 'svelte';
import App from './routes/App.svelte';
import { prefs } from './lib/state/prefs.svelte';

prefs.applyTheme();

const app = mount(App, { target: document.getElementById('app')! });

export default app;
