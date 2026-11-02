/** @type {import('tailwindcss').Config} */
module.exports = {
  content: ['./index.html', './src/**/*.{ts,tsx}'],
  // data-theme on <html> drives the palette swap (see theme.ts), so the
  // dark: variant keys off the same attribute — utilities like
  // dark:bg-ink-light follow the live theme without a class toggle.
  darkMode: ['selector', '[data-theme="dark"]'],
  theme: {
    extend: {
      fontFamily: {
        sans: ['HarmonyOS Sans', 'Noto Sans SC', 'Noto Sans', 'Segoe UI', 'sans-serif'],
        serif: ['EB Garamond', 'Noto Serif SC', 'Songti SC', 'serif'],
        mono: ['JetBrains Mono', 'ui-monospace', 'SFMono-Regular', 'monospace'],
      },
      // Palette = CSS variables (styles.css :root / [data-theme="dark"]).
      // Every color is an "R G B" channel triple so Tailwind's alpha
      // modifier (bg-ink/30, border-hairline/40 …) keeps working in both
      // themes — utilities never hardcode a hex, so dark mode is a
      // variable swap, not a component rewrite.
      colors: {
        paper: 'rgb(var(--paper) / <alpha-value>)',
        'paper-card': 'rgb(var(--paper-card) / <alpha-value>)',
        'paper-sunk': 'rgb(var(--paper-sunk) / <alpha-value>)',
        ink: {
          DEFAULT: 'rgb(var(--ink) / <alpha-value>)',
          light: 'rgb(var(--ink-light) / <alpha-value>)',
          muted: 'rgb(var(--ink-muted) / <alpha-value>)',
        },
        seal: {
          DEFAULT: 'rgb(var(--seal) / <alpha-value>)',
          hover: 'rgb(var(--seal-hover) / <alpha-value>)',
          light: 'rgb(var(--seal-light) / <alpha-value>)',
        },
        cinnabar: 'rgb(var(--cinnabar) / <alpha-value>)',
        moss: 'rgb(var(--moss) / <alpha-value>)',
        hairline: 'rgb(var(--hairline) / <alpha-value>)',
        // Foreground for accent fills (bg-seal / bg-moss buttons). White on
        // the light theme's mid-tone seal; dark ink on the dark theme's
        // lighter seal, which keeps contrast above the WCAG floor.
        'on-seal': 'rgb(var(--on-seal) / <alpha-value>)',
      },
      borderRadius: {
        xs: '2px',
        sm: '3px',
        md: '4px',
        lg: '6px',
        chat: '6px',
      },
      maxWidth: {
        chat: '720px',
      },
      // duration-fast was referenced by every component but never defined —
      // define it so `duration-fast` actually emits (snappy 120ms feedback).
      transitionDuration: {
        fast: '120ms',
      },
      // Motion tokens. "both" fill-mode keeps elements invisible until their
      // (staggered) animation starts, which is what makes stagger work.
      // Easing: ease-out-expo for entrances (fast start, long settle),
      // ease-in for exits (get out of the way quickly).
      keyframes: {
        'fade-in': {
          from: { opacity: '0' },
          to: { opacity: '1' },
        },
        'fade-out': {
          from: { opacity: '1' },
          to: { opacity: '0' },
        },
        'pop-in': {
          from: { opacity: '0', transform: 'translateY(8px) scale(0.985)' },
          to: { opacity: '1', transform: 'translateY(0) scale(1)' },
        },
        'pop-out': {
          from: { opacity: '1', transform: 'translateY(0) scale(1)' },
          to: { opacity: '0', transform: 'translateY(6px) scale(0.98)' },
        },
        'slide-in-right': {
          from: { opacity: '0', transform: 'translateX(20px)' },
          to: { opacity: '1', transform: 'translateX(0)' },
        },
        'slide-out-right': {
          from: { opacity: '1', transform: 'translateX(0)' },
          to: { opacity: '0', transform: 'translateX(16px)' },
        },
        'msg-in': {
          from: { opacity: '0', transform: 'translateY(10px)' },
          to: { opacity: '1', transform: 'translateY(0)' },
        },
        'rise-in': {
          from: { opacity: '0', transform: 'translateY(16px)' },
          to: { opacity: '1', transform: 'translateY(0)' },
        },
        'grow-x': {
          from: { transform: 'scaleX(0)' },
          to: { transform: 'scaleX(1)' },
        },
      },
      animation: {
        'fade-in': 'fade-in 180ms cubic-bezier(0.16, 1, 0.3, 1) both',
        'fade-out': 'fade-out 150ms ease-in both',
        'pop-in': 'pop-in 240ms cubic-bezier(0.16, 1, 0.3, 1) both',
        'pop-out': 'pop-out 140ms ease-in both',
        'slide-in-right': 'slide-in-right 260ms cubic-bezier(0.16, 1, 0.3, 1) both',
        'slide-out-right': 'slide-out-right 170ms ease-in both',
        'msg-in': 'msg-in 280ms cubic-bezier(0.16, 1, 0.3, 1) both',
        'rise-in': 'rise-in 480ms cubic-bezier(0.16, 1, 0.3, 1) both',
        'grow-x': 'grow-x 800ms cubic-bezier(0.16, 1, 0.3, 1) both',
      },
    },
  },
  plugins: [],
};
