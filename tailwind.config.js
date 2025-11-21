/** @type {import('tailwindcss').Config} */
import daisyui from "daisyui"
export default {
  content: ['./internal/**/*.{go,templ,js,html,css}'],
  theme: {
    extend: {},
  },
  plugins: [daisyui,],
  daisyui: {
    themes: ["forest"],
  }
}

