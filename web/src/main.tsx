import { render } from 'preact';
import App from './App';
import { initTheme } from './theme';
import './styles.css';

// The index.html boot snippet already set data-theme pre-paint; initTheme
// syncs the module's copy of that decision and arms the OS-preference
// watcher for users who never pick explicitly.
initTheme();

render(<App />, document.getElementById('app')!);
