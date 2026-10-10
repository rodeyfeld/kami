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

