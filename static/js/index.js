/**
 * Kami - Main JavaScript Bundle
 * 
 * This bundles HTMX for dynamic status updates
 * 
 * Bun bundles everything from node_modules into static/dist/index.js
 * 
 * Build commands:
 *   - Development: bun run dev:js (watch mode)
 *   - Production: bun run build:js (minified)
 */

import htmx from 'htmx.org';

// Make htmx globally available
window.htmx = htmx;

// Log HTMX events for debugging
if (import.meta.env.DEV) {
    document.body.addEventListener('htmx:beforeRequest', (evt) => {
        console.log('HTMX request:', evt.detail.requestConfig.path);
    });
    
    document.body.addEventListener('htmx:afterRequest', (evt) => {
        console.log('HTMX response:', evt.detail.successful ? 'success' : 'error');
    });
}


// Drawers the viewer opened or closed stay that way when a poll re-renders them
const drawers = new Map();

document.addEventListener('click', (evt) => {
    const summary = evt.target.closest('details[data-drawer] > summary');
    if (summary) {
        const details = summary.parentElement;
        drawers.set(details.dataset.drawer, !details.open);
    }
});

const restoreDrawers = () => {
    document.querySelectorAll('details[data-drawer]').forEach((details) => {
        const open = drawers.get(details.dataset.drawer);
        if (open !== undefined) {
            details.open = open;
        }
    });
};

document.body.addEventListener('htmx:oobAfterSwap', restoreDrawers);
document.body.addEventListener('htmx:afterSwap', restoreDrawers);
