// Runs micko --demo (micko.wasm.gz, built by make web) in an xterm.js
// terminal. Nothing heavy loads until the start button: the terminal, the Go
// runtime glue and the program all come after the click.
(() => {
  const gate = document.getElementById('gate');
  const start = document.getElementById('start');
  const status = document.getElementById('status');
  const host = document.getElementById('term');

  // micko's monokai skin, which the site draws its screens in.
  const theme = {
    background: '#272822', foreground: '#f8f8f2', cursor: '#f8f8f2', cursorAccent: '#272822',
    selectionBackground: '#49483e',
    black: '#272822', red: '#f92672', green: '#a6e22e', yellow: '#e6db74',
    blue: '#66d9ef', magenta: '#ae81ff', cyan: '#a1efe4', white: '#f8f8f2',
    brightBlack: '#75715e', brightRed: '#f92672', brightGreen: '#a6e22e', brightYellow: '#e6db74',
    brightBlue: '#66d9ef', brightMagenta: '#ae81ff', brightCyan: '#a1efe4', brightWhite: '#f9f8f5',
  };

  const script = (src) => new Promise((ok, fail) => {
    const s = document.createElement('script');
    s.src = src; s.onload = ok; s.onerror = () => fail(new Error('could not load ' + src));
    document.head.appendChild(s);
  });

  const mb = (n) => (n / 1048576).toFixed(1);

  // The program ships gzipped and is unpacked here, so the transfer stays
  // small whatever the server does with .wasm files. A server that sends it
  // with Content-Encoding: gzip has the browser unpack it first, so the
  // bytes are unpacked only if they still start with the gzip magic.
  async function fetchModule() {
    const res = await fetch('micko.wasm.gz');
    if (!res.ok) throw new Error('micko.wasm.gz: HTTP ' + res.status);
    const total = Number(res.headers.get('content-length')) || 0;
    const reader = res.body.getReader();
    const chunks = [];
    let seen = 0;
    for (let r; !(r = await reader.read()).done;) {
      chunks.push(r.value);
      seen += r.value.length;
      status.textContent = `downloading ${mb(seen)}${total ? ' / ' + mb(total) : ''} MB…`;
    }
    let blob = new Blob(chunks);
    const head = new Uint8Array(await blob.slice(0, 2).arrayBuffer());
    if (head[0] === 0x1f && head[1] === 0x8b) {
      blob = await new Response(blob.stream().pipeThrough(new DecompressionStream('gzip'))).blob();
    }
    status.textContent = 'starting…';
    return WebAssembly.compile(await blob.arrayBuffer());
  }

  let term, fit, module;

  // micko needs 60 columns; below that the font shrinks to fit 64, down to 8px.
  // JetBrains Mono's cell is 0.6em wide.
  const fontSize = () => Math.max(8, Math.min(14, Math.floor(host.clientWidth / (64 * 0.6))));
  const refit = () => {
    if (term.options.fontSize !== fontSize()) term.options.fontSize = fontSize();
    fit.fit();
  };

  async function openTerminal() {
    await Promise.all([
      script('xterm/xterm.js'), script('xterm/addon-fit.js'), script('xterm/addon-clipboard.js'), script('wasm_exec.js'),
      document.fonts.load('400 14px JBM'), document.fonts.load('700 14px JBM'),
    ]);
    // bubbletea draws as it would on a terminal that turns \n into \r\n, as
    // it assumes when its input is not a tty; convertEol does that here.
    term = new Terminal({
      fontFamily: 'JBM, ui-monospace, monospace', fontSize: fontSize(), lineHeight: 1.1,
      theme, convertEol: true, cursorBlink: false, allowProposedApi: true, scrollback: 0, macOptionIsMeta: true,
    });
    fit = new FitAddon.FitAddon();
    term.loadAddon(fit);
    term.loadAddon(new ClipboardAddon.ClipboardAddon());
    term.open(host);
    refit();
    new ResizeObserver(refit).observe(host);
    term.onData((s) => globalThis.mickoTerminal?.input?.(s));
    term.onBinary((s) => globalThis.mickoTerminal?.input?.(s));
    term.onResize(({ cols, rows }) => globalThis.mickoTerminal?.resize?.(cols, rows));
  }

  async function run() {
    term.reset();
    globalThis.mickoTerminal = { cols: term.cols, rows: term.rows, skin: 'monokai', write: (b) => term.write(b) };
    const go = new Go();
    const instance = await WebAssembly.instantiate(module, go.importObject);
    gate.hidden = true;
    term.focus();
    await go.run(instance);
    // Timers the Go runtime scheduled before it exited would fire into a
    // finished program and throw.
    go._scheduledTimeouts.forEach(clearTimeout);
    go._scheduledTimeouts.clear();
    globalThis.mickoTerminal = null;
    status.textContent = 'micko quit. Start it again whenever you like.';
    start.textContent = '▶ run it again';
    start.disabled = false;
    gate.hidden = false;
  }

  start.addEventListener('click', async () => {
    start.disabled = true;
    try {
      if (!term) {
        status.textContent = 'loading the terminal…';
        await openTerminal();
      }
      if (!module) module = await fetchModule();
      await run();
    } catch (err) {
      status.textContent = 'The demo could not start: ' + err.message;
      start.disabled = false;
    }
  });
})();
