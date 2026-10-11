/** @type {import('tailwindcss').Config} */
import daisyui from "daisyui"
export default {
  content: ['./internal/**/*.{go,templ,js,html,css}'],
  theme: {
    extend: {
      fontFamily: {
        sans: ['"Inter"', 'system-ui', 'sans-serif'],
        serif: ['"Shippori Mincho"', 'serif'],
        mono: ['"JetBrains Mono"', 'ui-monospace', 'monospace'],
      },
    },
  },
  plugins: [daisyui,],
  daisyui: {
    // Pale, desaturated tones on near-black: status colors read as tints, not signals shouting
    themes: [{
      kami: {
        "color-scheme": "dark",
        "primary": "#b8c7e0",
        "secondary": "#c9b8e0",
        "accent": "#a8d0d0",
        "neutral": "#1a1c22",
        "base-100": "#0c0d11",
        "base-200": "#0c0d11",
        "base-300": "#1c1e25",
        "base-content": "#d4d7de",
        "info": "#9db8e3",
        "success": "#93c9ad",
        "warning": "#ddc28e",
        "error": "#e3a0a0",
      },
    }],
  }
}
