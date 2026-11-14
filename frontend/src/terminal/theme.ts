import type { ITheme } from '@xterm/xterm'

/**
 * Dark ANSI palette tuned for a charcoal background. Blue is lightened vs. the
 * VGA default (unreadable on dark). ANSI "black" is deliberately dim: it is
 * mostly used for shadows/backgrounds by MUDs, so it stays distinguishable
 * from the background without being loud (it is NOT meant to reach AA contrast).
 */
export const terminalTheme: ITheme = {
  background: '#0b0a08',
  foreground: '#e8e0cf',
  cursor: '#d9b26f',
  cursorAccent: '#0b0a08',
  selectionBackground: '#5a4a2866',
  black: '#3a352a',
  red: '#e0604f',
  green: '#8fbf6a',
  yellow: '#e0b44f',
  blue: '#6f9bd8',
  magenta: '#c47fc0',
  cyan: '#5fbfc0',
  white: '#d8d0bd',
  brightBlack: '#7a705c',
  brightRed: '#ff7f6b',
  brightGreen: '#a9db82',
  brightYellow: '#f5d070',
  brightBlue: '#8fb7f0',
  brightMagenta: '#e69de0',
  brightCyan: '#7fe0e0',
  brightWhite: '#fbf6ea',
}

export const terminalFont = "ui-monospace, 'JetBrains Mono', 'Cascadia Code', 'SF Mono', Menlo, Consolas, 'Liberation Mono', monospace"
