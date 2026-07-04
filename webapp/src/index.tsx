/* @refresh reload */
import { render } from 'solid-js/web'
// Self-hosted IBM Plex (docs/DESIGN.md "type"): bundled by Vite, never an
// external CDN/font URL — required under this app's strict production CSP.
// Only the weights the design actually uses.
import '@fontsource/ibm-plex-sans/400.css'
import '@fontsource/ibm-plex-sans/500.css'
import '@fontsource/ibm-plex-sans/600.css'
import '@fontsource/ibm-plex-sans/700.css'
import '@fontsource/ibm-plex-mono/400.css'
import '@fontsource/ibm-plex-mono/500.css'
import './index.css'
import App from './App.tsx'

const root = document.getElementById('root')

render(() => <App />, root!)
