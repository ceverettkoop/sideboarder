// Demo shim: runs the Go server (compiled to WebAssembly) inside the page and
// routes the app's /api/ requests to it instead of the network. Documents are
// kept in this browser's localStorage.
'use strict';
(() => {
  const KEY = 'sideboarder.demo.v2.docs'; // bump when the sample changes
  let saved = {};
  try { saved = JSON.parse(localStorage.getItem(KEY) || '{}') || {}; } catch { saved = {}; }
  window.sideboarderSaved = saved;
  window.sideboarderSave = (name, data) => {
    saved[name] = data;
    try { localStorage.setItem(KEY, JSON.stringify(saved)); } catch { /* storage unavailable: memory only */ }
  };

  let markReady;
  const ready = new Promise((resolve) => { markReady = resolve; });
  window.sideboarderReady = () => markReady();

  const go = new Go();
  const load = WebAssembly.instantiateStreaming
    ? WebAssembly.instantiateStreaming(fetch('sideboarder.wasm'), go.importObject)
        .catch(() => fetch('sideboarder.wasm').then((r) => r.arrayBuffer()).then((b) => WebAssembly.instantiate(b, go.importObject)))
    : fetch('sideboarder.wasm').then((r) => r.arrayBuffer()).then((b) => WebAssembly.instantiate(b, go.importObject));
  load.then((res) => go.run(res.instance)).catch((err) => {
    document.getElementById('main').innerHTML = `<p class="loading">Couldn't start the demo: ${String(err)}</p>`;
  });

  async function serve(method, url, body, contentType) {
    await ready;
    return window.sideboarderServe(method, url, body ?? null, contentType || '');
  }

  const realFetch = window.fetch.bind(window);
  window.fetch = async (input, opts = {}) => {
    const url = typeof input === 'string' ? input : input.url;
    if (!url.startsWith('/api/')) return realFetch(input, opts);
    const headers = opts.headers || {};
    const r = await serve(opts.method || 'GET', url, opts.body, headers['Content-Type']);
    return new Response(r.body, { status: r.status, headers: { 'Content-Type': r.contentType || 'text/plain' } });
  };

  // CSV / document downloads are plain links. Pages hosted as previews often
  // can't start downloads, so show the file's contents with a Copy button.
  document.addEventListener('click', async (e) => {
    const a = e.target.closest('a[href^="/api/"]');
    if (!a) return;
    e.preventDefault();
    e.stopPropagation();
    const r = await serve('GET', a.getAttribute('href'));
    const name = /filename="?([^";]+)"?/.exec(r.disposition || '')?.[1] || 'file';
    openSheet({
      title: name,
      wide: true,
      body: `<p class="muted">The demo can't save files. Copy the contents instead.</p>
        <div class="btn-row"><button type="button" class="btn" id="demo-copy">Copy</button></div>
        <pre class="decklist" id="demo-file">${esc(r.body)}</pre>`,
    });
    document.getElementById('demo-copy').addEventListener('click', () => {
      navigator.clipboard.writeText(r.body).then(() => toast('Copied.'), () => {
        const range = document.createRange();
        range.selectNodeContents(document.getElementById('demo-file'));
        getSelection().removeAllRanges();
        getSelection().addRange(range);
        toast('Selected. Copy it with your browser.');
      });
    });
  }, true);
})();
