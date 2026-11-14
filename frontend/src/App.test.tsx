import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { act, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { forwardRef, useImperativeHandle } from 'react'
import App from './App'
import { SessionController } from './session/SessionController'
import { FakeWebSocket } from './test/FakeWebSocket'
import { PREFS_KEY } from './prefs/prefs'

// xterm needs a real layout engine; replace it with a recording stub here.
const written: string[] = []
vi.mock('./terminal/TerminalView', () => ({
  TerminalView: forwardRef(function Stub(_: unknown, ref) {
    useImperativeHandle(ref, () => ({
      write: (d: Uint8Array | string) => written.push(typeof d === 'string' ? d : new TextDecoder().decode(d)),
      clear: () => written.splice(0),
      scrollPages: () => undefined,
      getSelection: () => '',
      size: () => ({ cols: 100, rows: 30 }),
    }))
    return <div data-testid="terminal" />
  }),
}))

vi.stubGlobal('WebSocket', FakeWebSocket)

function mount() {
  const controller = new SessionController({ url: 'wss://x/ws', socketFactory: (u, p) => new FakeWebSocket(u, p) as unknown as WebSocket })
  const user = userEvent.setup()
  render(<App controller={controller} />)
  return { controller, user, ws: () => FakeWebSocket.last }
}
const goOnline = () => act(() => {
  FakeWebSocket.last.open()
  FakeWebSocket.last.json({ v: 1, type: 'connection.status', state: 'connected' })
})
const inputs = () => FakeWebSocket.last.messages.filter((m) => m.type === 'terminal.input').map((m) => m.data)
const box = () => screen.getByTestId('command-input') as HTMLInputElement

beforeEach(() => {
  FakeWebSocket.reset()
  written.length = 0
  localStorage.clear()
})
afterEach(() => vi.clearAllMocks())

describe('connection states', () => {
  it('connects on load using the fitted terminal size, then shows connected', () => {
    mount()
    expect(screen.getByTestId('status')).toHaveAttribute('data-state', 'connecting')
    goOnline()
    expect(FakeWebSocket.last.messages[0]).toMatchObject({ type: 'session.connect', cols: 100, rows: 30 })
    expect(screen.getByTestId('status')).toHaveAttribute('data-state', 'connected')
    expect(screen.getByRole('button', { name: 'Disconnect' })).toBeInTheDocument()
    expect(box()).toBeEnabled()
  })

  it('disables input while disconnected', () => {
    mount()
    expect(box()).toBeDisabled()
  })

  it('shows a clear error with a reconnect action, and reconnecting opens a fresh socket', async () => {
    const { user } = mount()
    act(() => FakeWebSocket.last.open())
    act(() => {
      FakeWebSocket.last.json({ v: 1, type: 'connection.error', code: 'upstream_tls', message: 'The game server’s TLS certificate could not be verified.' })
      FakeWebSocket.last.json({ v: 1, type: 'connection.status', state: 'disconnected', detail: 'x' })
    })
    const banner = screen.getByTestId('error-banner')
    expect(banner).toHaveTextContent('TLS certificate could not be verified')
    expect(banner).toHaveTextContent('upstream_tls')
    expect(screen.getByTestId('status')).toHaveAttribute('data-state', 'disconnected')
    expect(box()).toBeDisabled()
    await user.click(within(banner).getByRole('button', { name: 'Reconnect' }))
    expect(FakeWebSocket.instances).toHaveLength(2)
    expect(screen.queryByTestId('error-banner')).toBeNull()
  })

  it('the header Reconnect button reconnects after a drop', async () => {
    const { user } = mount()
    goOnline()
    act(() => FakeWebSocket.last.serverClose(1006))
    expect(screen.getByTestId('error-banner')).toHaveTextContent('Could not reach the gateway')
    await user.click(screen.getAllByRole('button', { name: 'Reconnect' })[0]!) // header button (banner has another)
    expect(FakeWebSocket.instances).toHaveLength(2)
    expect(screen.getByTestId('status')).toHaveAttribute('data-state', 'connecting')
  })

  it('Disconnect sends session.disconnect', async () => {
    const { user } = mount()
    goOnline()
    await user.click(screen.getByRole('button', { name: 'Disconnect' }))
    expect(FakeWebSocket.last.messages.at(-1)).toEqual({ v: 1, type: 'session.disconnect' })
    expect(screen.getByTestId('status')).toHaveAttribute('data-state', 'disconnected')
  })
})

describe('terminal input and history', () => {
  it('Enter sends the line, echoes it and clears the field', async () => {
    const { user } = mount()
    goOnline()
    await user.type(box(), 'look{Enter}')
    expect(inputs()).toEqual(['look\n'])
    expect(box()).toHaveValue('')
    expect(written.join('')).toContain('look')
  })

  it('arbitrary text, including MUD-style punctuation, is passed through unmodified', async () => {
    const { user } = mount()
    goOnline()
    await user.type(box(), `'hello wor"ld! @#$%{{}{Enter}`)
    expect(inputs()).toEqual([`'hello wor"ld! @#$%{}\n`])
  })

  it('ArrowUp/ArrowDown browse history and restore the draft', async () => {
    const { user } = mount()
    goOnline()
    for (const c of ['look', 'north', 'say hi']) await user.type(box(), c + '{Enter}')
    await user.type(box(), 'dra')
    await user.keyboard('{ArrowUp}')
    expect(box()).toHaveValue('say hi')
    await user.keyboard('{ArrowUp}{ArrowUp}')
    expect(box()).toHaveValue('look')
    await user.keyboard('{ArrowDown}')
    expect(box()).toHaveValue('north')
    await user.keyboard('{ArrowDown}{ArrowDown}')
    expect(box()).toHaveValue('dra')
  })

  it('Escape clears the line', async () => {
    const { user } = mount()
    goOnline()
    await user.type(box(), 'abc{Escape}')
    expect(box()).toHaveValue('')
  })

  it('an empty line still sends a newline (some MUD prompts need it)', async () => {
    const { user } = mount()
    goOnline()
    await user.type(box(), '{Enter}')
    expect(inputs()).toEqual(['\n'])
  })

  it('multi-line paste sends each line as a command', async () => {
    const { user } = mount()
    goOnline()
    box().focus()
    await user.paste('north\nlook\nsouth')
    // the final unterminated line is also a complete command here
    expect(inputs()).toEqual(['north\n', 'look\n', 'south\n'])
  })
})

describe('password masking', () => {
  it('switches to a password field on server ECHO, hides history, then restores', async () => {
    const { user } = mount()
    goOnline()
    await user.type(box(), 'look{Enter}')
    act(() => {
      FakeWebSocket.last.binary('Password: ')
      FakeWebSocket.last.json({ v: 1, type: 'session.echo', enabled: false })
    })
    expect(box()).toHaveAttribute('type', 'password')
    expect(box()).toHaveAttribute('data-masked', 'true')
    await user.keyboard('{ArrowUp}')
    expect(box()).toHaveValue('') // history cannot be recalled into a password field
    await user.type(box(), 's3cret{Enter}')
    expect(inputs()).toEqual(['look\n', 's3cret\n'])
    expect(written.join('')).not.toContain('s3cret')
    act(() => FakeWebSocket.last.json({ v: 1, type: 'session.echo', enabled: true }))
    expect(box()).toHaveAttribute('type', 'text')
    await user.keyboard('{ArrowUp}')
    expect(box()).toHaveValue('look')
    await user.keyboard('{ArrowUp}')
    expect(box()).not.toHaveValue('s3cret')
  })
})

describe('GMCP inspector', () => {
  it('shows merged state and a log with malformed messages flagged', async () => {
    const { user } = mount()
    goOnline()
    await user.click(screen.getByRole('button', { name: 'GMCP inspector' }))
    const insp = screen.getByTestId('gmcp-inspector')
    expect(insp).toHaveTextContent('No GMCP data received yet')
    act(() => {
      FakeWebSocket.last.json({ v: 1, type: 'gmcp.message', package: 'Char.Vitals', data: { hp: 10, maxhp: 20 } })
      FakeWebSocket.last.json({ v: 1, type: 'gmcp.message', package: 'Char.Vitals', data: { hp: 7 } })
      FakeWebSocket.last.json({ v: 1, type: 'gmcp.message', package: 'Bad.Pkg', malformed: true, error: 'invalid JSON payload', raw: '{"x":' })
    })
    expect(insp).toHaveTextContent('Char.Vitals')
    expect(insp).toHaveTextContent('"hp": 7')
    expect(insp).toHaveTextContent('"maxhp": 20') // delta merged
    await user.click(within(insp).getByRole('button', { name: /Log/ }))
    expect(insp).toHaveTextContent('malformed: invalid JSON payload')
    await user.type(within(insp).getByLabelText('Filter log'), 'vitals')
    expect(insp).not.toHaveTextContent('Bad.Pkg')
  })
})

describe('preferences', () => {
  it('persists changes to localStorage and restores them', async () => {
    const { user } = mount()
    await user.click(screen.getByRole('button', { name: 'Settings' }))
    const slider = screen.getByLabelText(/Font size/)
    act(() => {
      // jsdom range inputs: set via fireEvent-like change
      Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value')!.set!.call(slider, '20')
      slider.dispatchEvent(new Event('input', { bubbles: true }))
    })
    expect(screen.getByTestId('font-size-value')).toHaveTextContent('20')
    expect(JSON.parse(localStorage.getItem(PREFS_KEY)!)).toMatchObject({ fontSize: 20 })
  })

  it('loads saved preferences and tolerates corrupt storage', () => {
    localStorage.setItem(PREFS_KEY, JSON.stringify({ inspectorOpen: true }))
    mount()
    expect(screen.getByTestId('gmcp-inspector')).toBeInTheDocument()
  })
  it('corrupt storage falls back to defaults', () => {
    localStorage.setItem(PREFS_KEY, '{{{')
    expect(() => mount()).not.toThrow()
  })
})

describe('keyboard shortcuts', () => {
  it('Ctrl+L clears the terminal; Alt+R reconnects', async () => {
    const { user } = mount()
    goOnline()
    written.push('x')
    await user.keyboard('{Control>}l{/Control}')
    expect(written).toEqual([])
    act(() => FakeWebSocket.last.serverClose(1006))
    await user.keyboard('{Alt>}r{/Alt}')
    expect(FakeWebSocket.instances).toHaveLength(2)
  })
})

describe('branding', () => {
  it('renders the default brand, and a custom one with a two-tone name', () => {
    const controller = new SessionController({ url: 'wss://x/ws', socketFactory: (u, p) => new FakeWebSocket(u, p) as unknown as WebSocket })
    const { unmount } = render(<App controller={controller} />)
    expect(screen.getByTestId('brand-name').textContent).toBe('Tellwire')
    unmount()
    render(<App controller={controller} brand={{ name: 'DuneMUD', accentStart: 4, tagline: 'hi there', title: 't', description: 'd' }} />)
    expect(screen.getByTestId('brand-name').textContent).toBe('DuneMUD')
    expect(screen.getByTestId('brand-name').querySelector('span')!.textContent).toBe('MUD')
    expect(screen.getByText('hi there')).toBeInTheDocument()
  })
})
