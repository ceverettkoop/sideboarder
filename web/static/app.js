// Sideboarder web client.
//
// All rules (effective plans, validation, deck revisions, records) live in the
// Go server; this file renders the server's view of a document and sends small
// edit operations back. Every edit is saved immediately.
'use strict';

// ----- tiny helpers ------------------------------------------------------------

const $ = (sel, root = document) => root.querySelector(sel);
const $$ = (sel, root = document) => Array.from(root.querySelectorAll(sel));
const ESC = { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' };
const esc = (s) => String(s ?? '').replace(/[&<>"']/g, (c) => ESC[c]);
const fold = (s) => String(s ?? '').toLowerCase();
const total = (entries) => entries.reduce((n, e) => n + e.qty, 0);
const enc = encodeURIComponent;

const store = {
  get(key, fallback) {
    try {
      const v = localStorage.getItem('sideboarder.' + key);
      return v === null ? fallback : JSON.parse(v);
    } catch { return fallback; }
  },
  set(key, value) {
    try { localStorage.setItem('sideboarder.' + key, JSON.stringify(value)); } catch { /* private mode */ }
  },
};

function todayISO() {
  const d = new Date();
  const pad = (n) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}

// ----- state ---------------------------------------------------------------------

const S = {
  file: null,            // open document file name
  view: null,            // server view: {doc, matchups, records, overall, ...}
  tab: store.get('tab', 'plans'),
  archId: store.get('arch', null),
  layer: 'base',         // plan layer being edited: base | play | draw
  showEffective: store.get('effective', false),
  reportMode: 'base',
  openFits: new Set(),   // build view: matchups whose plan is expanded
  report: null,
  pending: 0,            // in-flight saves
};

const LAYERS = [['base', 'Base'], ['play', 'On the play +'], ['draw', 'On the draw +']];
const OUTCOME = { W: 'win', L: 'loss', D: 'draw' };

// ----- server calls ----------------------------------------------------------------

async function api(method, url, body) {
  const opts = { method, headers: {} };
  if (body !== undefined) {
    opts.headers['Content-Type'] = 'application/json';
    opts.body = JSON.stringify(body);
  }
  let res;
  try {
    res = await fetch(url, opts);
  } catch {
    throw new Error('Can’t reach the Sideboarder server. Is Tailscale connected?');
  }
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error(data.error || `${res.status} ${res.statusText}`);
  return data;
}

const fileURL = (suffix = '') => `/api/files/${enc(S.file)}${suffix}`;

function setPending(delta) {
  S.pending += delta;
  const el = $('#save-state');
  if (S.pending > 0) {
    el.textContent = 'Saving…';
    el.className = 'save-state busy';
  } else {
    el.textContent = 'Saved';
    el.className = 'save-state ok';
  }
}

// Edits are queued so they reach the server in order.
let opChain = Promise.resolve();

function op(payload) {
  const file = S.file;
  if (!file) return Promise.resolve(null);
  const run = async () => {
    setPending(1);
    try {
      const res = await api('POST', `/api/files/${enc(file)}/ops`, payload);
      if (S.file === file) applyFile(res);
      return res;
    } catch (err) {
      toast(err.message, 'error');
      return null;
    } finally {
      setPending(-1);
    }
  };
  const p = opChain.then(run);
  opChain = p;
  return p;
}

function applyFile(res) {
  S.file = res.file;
  S.view = res.view;
  store.set('file', S.file);
  const archs = S.view.doc.archetypes;
  if (!archs.some((a) => a.id === S.archId)) S.archId = archs.length ? archs[0].id : null;
  render();
  if (S.tab === 'report') loadReport();
}

async function openFile(name) {
  try {
    applyFile(await api('GET', `/api/files/${enc(name)}`));
    S.report = null;
    if (S.tab === 'report') loadReport();
    return true;
  } catch (err) {
    toast(err.message, 'error');
    return false;
  }
}

async function reloadFile() {
  if (!S.file || S.pending > 0 || $('#sheet').open) return;
  try {
    const res = await api('GET', fileURL());
    if (S.pending === 0) applyFile(res);
  } catch { /* offline: keep what we have */ }
}

async function loadReport() {
  if (!S.file) return;
  try {
    S.report = await api('GET', fileURL(`/report?mode=${S.reportMode}`));
    if (S.tab === 'report') renderMain();
  } catch (err) {
    toast(err.message, 'error');
  }
}

// ----- toasts ------------------------------------------------------------------------

function toast(message, kind = 'info') {
  const el = document.createElement('div');
  el.className = `toast ${kind}`;
  el.setAttribute('role', kind === 'error' ? 'alert' : 'status');
  el.textContent = message;
  $('#toasts').append(el);
  setTimeout(() => el.classList.add('out'), kind === 'error' ? 5000 : 2600);
  setTimeout(() => el.remove(), kind === 'error' ? 5400 : 3000);
}

// ----- rendering ---------------------------------------------------------------------

function render() {
  renderTop();
  renderTabs();
  renderMain();
}

function renderTop() {
  const doc = S.view?.doc;
  $('#doc-name').textContent = doc ? (doc.deck.name || 'Untitled') : 'Sideboarder';
  $('#doc-file').textContent = S.file || 'No document open';
  document.title = doc ? `${doc.deck.name || 'Untitled'} · Sideboarder` : 'Sideboarder';
}

function renderTabs() {
  for (const t of $$('.tab')) {
    const on = t.dataset.tab === S.tab;
    t.classList.toggle('active', on);
    if (on) t.setAttribute('aria-current', 'page'); else t.removeAttribute('aria-current');
  }
}

function renderMain() {
  const main = $('#main');
  if (!S.view) {
    main.innerHTML = welcome();
    return;
  }
  const views = { plans: viewPlans, deck: viewDeck, build: viewBuild, results: viewResults, report: viewReport };
  // Keep focus (e.g. on the next metagame share) across re-renders.
  const focused = document.activeElement;
  let refocus = '';
  if (focused && focused !== main && main.contains(focused)) {
    if (focused.id) refocus = `#${CSS.escape(focused.id)}`;
    else if (focused.dataset.act && focused.dataset.id) {
      refocus = `[data-act="${CSS.escape(focused.dataset.act)}"][data-id="${CSS.escape(focused.dataset.id)}"]`;
    }
  }
  main.innerHTML = (views[S.tab] || viewPlans)();
  if (refocus) main.querySelector(refocus)?.focus();
}

function welcome() {
  return `<section class="empty-state">
    <h1>Sideboarder</h1>
    <p>Plan sideboard guides and track tournament results. Documents are the same
    <code>.sbd.json</code> files the terminal app uses.</p>
    <div class="btn-row center">
      <button class="btn primary" data-act="files">Open or create a document</button>
    </div>
  </section>`;
}

function currentArch() {
  return S.view?.doc.archetypes.find((a) => a.id === S.archId) || null;
}

function layerPlan(arch, layer) {
  if (!arch) return null;
  if (layer === 'base') return arch.base;
  return (layer === 'play' ? arch.play_override : arch.draw_override) || null;
}

// OUT/IN quantities already used by the layer on screen, by folded name.
function allocations(arch) {
  const out = {}, inn = {};
  const plan = layerPlan(arch, S.layer);
  if (plan) {
    for (const e of plan.out) out[fold(e.name)] = (out[fold(e.name)] || 0) + e.qty;
    for (const e of plan.in) inn[fold(e.name)] = (inn[fold(e.name)] || 0) + e.qty;
  }
  return { out, in: inn };
}

function qtyButtons(act, attrs, label) {
  return `<span class="row-actions">
    <button class="mini-btn" data-act="${act}" ${attrs} data-delta="-1" aria-label="One fewer ${esc(label)}">−</button>
    <button class="mini-btn" data-act="${act}" ${attrs} data-delta="1" aria-label="One more ${esc(label)}">+</button>
  </span>`;
}

// ----- plans view ------------------------------------------------------------------------

function viewPlans() {
  const archs = S.view.doc.archetypes;
  const arch = currentArch();
  const chips = archs.map((a) => {
    const m = S.view.matchups[a.id];
    const warn = m && (!m.play.validation.ok || !m.draw.validation.ok);
    return `<button class="arch-chip ${a.id === S.archId ? 'active' : ''}" data-act="arch-select" data-id="${esc(a.id)}"
      ${a.id === S.archId ? 'aria-current="true"' : ''}>
      <span>${esc(a.name)}</span>${warn ? '<span class="warn-dot" title="Plan needs attention">!</span>' : ''}
    </button>`;
  }).join('');
  return `<div class="plans-grid">
    <aside class="pane arch-pane">
      <div class="pane-head">
        <h2>Archetypes <span class="count">${archs.length}</span></h2>
        <button class="btn small" data-act="arch-add">+ Add</button>
      </div>
      <div class="arch-list">${chips || '<p class="muted pad">No opponent archetypes yet.</p>'}</div>
    </aside>
    <section class="pane plan-pane">${arch ? planEditor(arch) : planEmpty()}</section>
    <aside class="pane deck-side">${deckBoards('plans', arch)}</aside>
  </div>`;
}

function planEmpty() {
  const hasDeck = S.view.doc.deck.mainboard.length || S.view.doc.deck.sideboard.length;
  return `<div class="empty-state small">
    <h2>No matchup selected</h2>
    <p>${hasDeck ? 'Add the opponent archetypes you expect, then plan what comes out and in for each.'
      : 'Start by importing your decklist, then add the opponent archetypes you expect.'}</p>
    <div class="btn-row center">
      ${hasDeck ? '' : '<button class="btn" data-act="import">Import decklist</button>'}
      <button class="btn primary" data-act="arch-add">Add archetype</button>
    </div>
  </div>`;
}

function planEditor(arch) {
  const plan = layerPlan(arch, S.layer) || { out: [], in: [] };
  const m = S.view.matchups[arch.id];
  const seg = LAYERS.map(([key, label]) => `<button class="seg-btn ${S.layer === key ? 'active' : ''}"
      data-act="layer" data-layer="${key}" aria-pressed="${S.layer === key}">${label}</button>`).join('');
  const layerHint = S.layer === 'base' ? ''
    : `<p class="hint">Changes on top of the base plan when you are ${S.layer === 'play' ? 'on the play' : 'on the draw'}.</p>`;
  return `<div class="pane-head">
      <h2 class="matchup-title"><span class="muted">vs</span> ${esc(arch.name)}</h2>
      <button class="btn small" data-act="arch-menu" aria-label="Matchup options">Edit</button>
    </div>
    ${arch.notes ? `<p class="notes">${esc(arch.notes)}</p>` : ''}
    <div class="seg" role="group" aria-label="Plan layer">${seg}</div>
    ${layerHint}
    ${planList(plan.out, 'out')}
    ${planList(plan.in, 'in')}
    <div class="summary">
      ${summaryLine('On the play', m.play.validation)}
      ${summaryLine('On the draw', m.draw.validation)}
    </div>
    <details class="effective" data-act-toggle="effective" ${S.showEffective ? 'open' : ''}>
      <summary>Effective plans</summary>
      <div class="effective-grid">
        ${effectiveBlock('On the play', m.play.plan)}
        ${effectiveBlock('On the draw', m.draw.plan)}
      </div>
    </details>`;
}

function planList(entries, list) {
  const title = list === 'out' ? 'OUT' : 'IN';
  const from = list === 'out' ? 'mainboard' : 'sideboard';
  const rows = entries.map((e) => {
    const attrs = `data-list="${list}" data-card="${esc(e.name)}"`;
    return `<li class="card-row">
      <span class="qty">${e.qty}</span>
      <span class="name">${esc(e.name)}</span>
      ${qtyButtons('plan-qty', attrs, e.name)}
      <button class="mini-btn danger" data-act="plan-remove" ${attrs} aria-label="Remove ${esc(e.name)}">×</button>
    </li>`;
  }).join('');
  return `<div class="list-block ${list}">
    <div class="list-head">
      <h3><span class="tag ${list}">${title}</span> <span class="muted">${from}</span> <span class="count">${total(entries)}</span></h3>
      <button class="btn small" data-act="plan-add" data-list="${list}">+ Add ${title}</button>
    </div>
    <ul class="card-list">${rows || `<li class="muted empty-row">Nothing ${list === 'out' ? 'taken out' : 'brought in'}.</li>`}</ul>
  </div>`;
}

function summaryLine(label, v) {
  const bad = [];
  if (!v.balanced) bad.push('unbalanced');
  if (v.illegal_in.length) bad.push(`not in SB: ${v.illegal_in.map(esc).join(', ')}`);
  return `<p class="${bad.length ? 'warn' : 'ok'}"><strong>${label}:</strong> OUT ${v.out_total} / IN ${v.in_total}
    ${bad.map((b) => `<span class="warn-text">⚠ ${b}</span>`).join(' ')}</p>`;
}

function effectiveBlock(label, plan) {
  const lines = (entries) => entries.map((e) => `<li><span class="qty">${e.qty}</span> ${esc(e.name)}</li>`).join('')
    || '<li class="muted">—</li>';
  return `<div class="eff">
    <h4>${label}</h4>
    <div class="eff-cols">
      <div><span class="tag out">OUT</span><ul>${lines(plan.out)}</ul></div>
      <div><span class="tag in">IN</span><ul>${lines(plan.in)}</ul></div>
    </div>
  </div>`;
}

// ----- deck boards (deck view, and the plans view's side pane) -------------------------

function deckBoards(ctx, arch) {
  const deck = S.view.doc.deck;
  const alloc = ctx === 'plans' && arch ? allocations(arch) : null;
  const board = (which, entries) => {
    const label = which === 'main' ? 'Mainboard' : 'Sideboard';
    const used = alloc ? (which === 'main' ? alloc.out : alloc.in) : null;
    const rows = entries.map((e) => {
      const attrs = `data-board="${which}" data-card="${esc(e.name)}"`;
      if (used) {
        // Plans context: show what's still available for this matchup layer.
        const remaining = Math.max(0, e.qty - (used[fold(e.name)] || 0));
        return `<li class="card-row tappable ${remaining ? '' : 'spent'}" data-act="deck-to-plan" ${attrs}>
          <span class="qty">${remaining}${remaining !== e.qty ? `<small>/${e.qty}</small>` : ''}</span>
          <span class="name">${esc(e.name)}</span>
          <span class="chev" aria-hidden="true">${which === 'main' ? 'OUT' : 'IN'}</span>
        </li>`;
      }
      return `<li class="card-row">
        <span class="qty">${e.qty}</span>
        <button class="name link" data-act="deck-edit" ${attrs}>${esc(e.name)}</button>
        ${qtyButtons('deck-qty', attrs, e.name)}
      </li>`;
    }).join('');
    return `<div class="list-block">
      <div class="list-head">
        <h3>${label} <span class="count">${total(entries)}</span></h3>
        ${ctx === 'deck' ? `<button class="btn small" data-act="deck-add" data-board="${which}">+ Add</button>` : ''}
      </div>
      <ul class="card-list">${rows || '<li class="muted empty-row">Empty.</li>'}</ul>
    </div>`;
  };
  const head = ctx === 'plans'
    ? `<div class="pane-head"><h2>Deck</h2>${arch ? '<span class="muted small-text">available · tap to add</span>' : ''}</div>`
    : '';
  return head + board('main', deck.mainboard) + board('side', deck.sideboard);
}

function viewDeck() {
  const doc = S.view.doc;
  const deck = doc.deck;
  const pending = doc.deck_modified
    ? `<p class="hint">Edits pending: this list becomes revision ${doc.deck_revision + 1} once it is used
       (a plan is edited or a result is recorded).</p>` : '';
  return `<section class="pane deck-head">
      <div>
        <h2>${esc(deck.name || 'Untitled')}</h2>
        <p class="muted">${deck.format ? esc(deck.format) + ' · ' : ''}${total(deck.mainboard)} main · ${total(deck.sideboard)} side · revision ${doc.deck_revision}</p>
      </div>
      <div class="btn-row">
        <button class="btn" data-act="deck-info">Rename</button>
        <button class="btn primary" data-act="import">Import</button>
      </div>
      ${pending}
    </section>
    <div class="deck-grid">
      <section class="pane">${deckBoards('deck')}</section>
    </div>`;
}

// ----- results view ------------------------------------------------------------------------

function viewResults() {
  const v = S.view;
  const results = v.doc.results;
  const rows = results.map((r) => {
    const outcome = OUTCOME[v.outcomes[r.id]];
    const rev = r.deck_revision == null ? '—' : r.deck_revision;
    return `<tr class="result ${outcome}" data-act="result-edit" data-id="${esc(r.id)}" tabindex="0">
      <td data-label="Date">${esc(r.date)}</td>
      <td data-label="Event">${esc(r.event)}</td>
      <td data-label="Opponent" class="opp">${esc(r.archetype) || '<span class="muted">?</span>'}</td>
      <td data-label="P/D">${r.play_draw ? (r.play_draw === 'play' ? 'Play' : 'Draw') : '—'}</td>
      <td data-label="Games" class="games"><span class="badge ${outcome}">${r.games_won}-${r.games_lost}</span></td>
      <td data-label="Rev">${r.deck_revision == null ? rev
        : `<button class="link" data-act="view-rev" data-id="${esc(r.id)}">${rev}</button>`}</td>
      <td data-label="Notes" class="notes-cell">${esc(r.notes)}</td>
    </tr>`;
  }).join('');
  const stats = v.records.map((rec) => `<tr>
      <td>${esc(rec.name)}</td><td>${rec.wins}</td><td>${rec.losses}</td><td>${rec.draws}</td>
      <td>${rec.games_text}</td><td>${rec.winrate_text}</td></tr>`).join('');
  const o = v.overall;
  return `<div class="results-grid">
    <section class="pane results-pane">
      <div class="pane-head">
        <h2>Match results <span class="count">${results.length}</span></h2>
        <button class="btn primary small" data-act="result-new">+ New result</button>
      </div>
      <p class="legend">Rows: <span class="badge win">win</span> <span class="badge loss">loss</span>
        <span class="badge draw">draw</span> · tap a row to edit, the revision to see that decklist</p>
      ${rows ? `<table class="results-table">
        <thead><tr><th>Date</th><th>Event</th><th>Opponent</th><th>P/D</th><th>Games</th><th>Rev</th><th>Notes</th></tr></thead>
        <tbody>${rows}</tbody></table>`
      : '<p class="muted empty-row">No matches logged yet.</p>'}
    </section>
    <section class="pane stats-pane">
      <div class="pane-head"><h2>By archetype</h2>
        <a class="btn small" href="${fileURL('/results.csv')}" download>Export CSV</a></div>
      <table class="stats-table">
        <thead><tr><th>Archetype</th><th>W</th><th>L</th><th>D</th><th>Games</th><th>Win%</th></tr></thead>
        <tbody>${stats || '<tr><td colspan="6" class="muted">(no matches yet)</td></tr>'}</tbody>
      </table>
      <p class="overall"><strong>Overall:</strong> ${o.record_text} · games ${o.games_text}
        · winrate ${o.winrate_text} (${o.matches} matches)</p>
      <p class="hint">Draws are excluded from winrate.</p>
    </section>
  </div>`;
}

// ----- build view (metagame builder) --------------------------------------------------------

const pct = (x) => `${Math.round(x * 10) / 10}%`;

function viewBuild() {
  const v = S.view;
  const archs = v.doc.archetypes;
  if (!archs.length) {
    return `<section class="empty-state">
      <h2>Build a 75 from your matchups</h2>
      <p>Add the opponent archetypes you expect first. Then give each one its share of the field and the
      60 cards you want after sideboarding, and this suggests a main deck and sideboard.</p>
      <div class="btn-row center"><button class="btn primary" data-act="arch-add">Add archetype</button></div>
    </section>`;
  }
  const shareTotal = archs.reduce((n, a) => n + (a.meta_share || 0), 0);
  const rows = archs.map((a) => {
    const size = total(a.target_deck || []);
    const status = !size ? '<span class="status none">No post-board deck</span>'
      : size === 60 ? '<span class="status ok">60 cards</span>'
        : `<span class="status warn">${size} cards</span>`;
    return `<li class="meta-row">
      <span class="meta-name">${esc(a.name)}</span>
      <label class="share">
        <input type="number" id="share-${esc(a.id)}" inputmode="decimal" min="0" max="100" step="0.5"
          value="${a.meta_share ?? ''}" placeholder="—" data-share="${esc(a.id)}" aria-label="${esc(a.name)} share of the field">
        <span>%</span>
      </label>
      ${status}
      <button class="btn small" data-act="target-edit" data-id="${esc(a.id)}">${size ? 'Edit deck' : 'Set deck'}</button>
    </li>`;
  }).join('');
  let shareNote = `Shares add up to ${pct(shareTotal)}.`;
  if (shareTotal > 100.05) shareNote += ' <span class="warn-text">That’s over 100%.</span>';
  else if (shareTotal > 0 && shareTotal < 99.95) shareNote += ` The other ${pct(100 - shareTotal)} of the field isn’t planned for.`;
  return `<section class="pane">
      <div class="pane-head"><h2>Metagame</h2></div>
      <p class="hint">For each matchup, enter its share of the field and the 60 cards you want to play after
        sideboarding. Shares only need to be right relative to each other.</p>
      <ul class="meta-list">${rows}</ul>
      <p class="hint">${shareNote}</p>
    </section>
    ${v.suggestion.ready ? suggestionView(v.suggestion) : `<section class="pane empty-state small">
      <h2>No suggestion yet</h2>
      <p>Set a post-board deck for at least one matchup to get a suggested main deck and sideboard.</p>
    </section>`}`;
}

function suggestionView(sug) {
  // "new" marks a card that isn't anywhere in the current 75; moves between
  // main deck and sideboard show as +/− counts.
  const delta = (now, before, inDeck) => {
    if (now === before) return '';
    if (!inDeck) return '<span class="delta add">new</span>';
    return `<span class="delta ${now > before ? 'add' : 'cut'}">${now > before ? '+' : '−'}${Math.abs(now - before)}</span>`;
  };
  const cardRows = (key, curKey) => sug.cards.filter((c) => c[key] > 0).map((c) => `<li class="card-row">
      <span class="qty">${c[key]}</span>
      <span class="name">${esc(c.name)} ${delta(c[key], c[curKey], c.current_main + c.current_side > 0)}</span>
      <span class="avg" title="Copies wanted by share of the field: ${c.wanted.map(pct).join(', ')}">avg ${c.average}</span>
    </li>`).join('');
  const kept = new Set(sug.cards.map((c) => fold(c.name)));
  const deck = S.view.doc.deck;
  const cuts = [...deck.mainboard, ...deck.sideboard].filter((e) => !kept.has(fold(e.name)));
  const cutNames = [...new Set(cuts.map((e) => e.name))];
  const mainCount = total(sug.main), sideCount = total(sug.side);
  const fits = sug.matchups.map((f) => {
    const open = S.openFits.has(f.archetype_id);
    const list = (entries) => entries.map((e) => `<li><span class="qty">${e.qty}</span> ${esc(e.name)}</li>`).join('') || '<li class="muted">—</li>';
    return `<tr class="fit ${open ? 'open' : ''}" data-act="fit-toggle" data-id="${esc(f.archetype_id)}" tabindex="0" aria-expanded="${open}">
        <td>${esc(f.name)}</td><td>${pct(f.weight)}</td>
        <td>${f.game1}/${f.target_size}</td><td>${f.postboard}/${f.target_size}</td><td>${f.swaps}</td>
      </tr>
      ${open ? `<tr class="fit-detail"><td colspan="5">
        <div class="eff-cols">
          <div><span class="tag out">OUT</span><ul>${list(f.plan.out)}</ul></div>
          <div><span class="tag in">IN</span><ul>${list(f.plan.in)}</ul></div>
        </div>
        ${f.missing.length ? `<p class="warn-text small-text">Not in the 75: ${f.missing.map((e) => `${e.qty} ${esc(e.name)}`).join(', ')}</p>` : ''}
      </td></tr>` : ''}`;
  }).join('');
  return `<section class="pane">
      <div class="pane-head">
        <h2>Suggested 75</h2>
        <button class="btn primary small" data-act="apply-suggestion">Use this deck and plans</button>
      </div>
      <div class="stats">
        <div class="stat"><span class="stat-num">${pct(sug.game1_rate)}</span><span class="stat-label">of target cards in game 1</span></div>
        <div class="stat"><span class="stat-num">${pct(sug.postboard_rate)}</span><span class="stat-label">after sideboarding</span></div>
        <div class="stat"><span class="stat-num">${sug.avg_swaps}</span><span class="stat-label">cards swapped per match</span></div>
      </div>
      <p class="hint">Every copy of a card is ranked by how much of the field wants it after sideboarding. The 60
        most-wanted copies make the main deck and the next 15 the sideboard. Figures are weighted by share.</p>
      ${sug.warnings.length ? `<ul class="warnings">${sug.warnings.map((w) => `<li>${esc(w)}</li>`).join('')}</ul>` : ''}
    </section>
    <div class="build-grid">
      <section class="pane">
        <div class="list-head"><h3>Main deck <span class="count">${mainCount}</span></h3></div>
        <ul class="card-list">${cardRows('main', 'current_main')}</ul>
      </section>
      <section class="pane">
        <div class="list-head"><h3>Sideboard <span class="count">${sideCount}</span></h3></div>
        <ul class="card-list">${cardRows('side', 'current_side') || '<li class="muted empty-row">Empty.</li>'}</ul>
        ${cutNames.length ? `<p class="hint">Cut from your current deck: ${cutNames.map(esc).join(', ')}</p>` : ''}
      </section>
    </div>
    <section class="pane">
      <div class="pane-head"><h2>By matchup</h2></div>
      <table class="fit-table">
        <thead><tr><th>Matchup</th><th>Share</th><th>Game 1</th><th>Boarded</th><th>Swaps</th></tr></thead>
        <tbody>${fits}</tbody>
      </table>
      <p class="hint">Game 1 and Boarded count the target cards you’d be playing. Tap a matchup for its plan.</p>
    </section>`;
}

function sheetTarget(id) {
  const arch = S.view.doc.archetypes.find((a) => a.id === id);
  if (!arch) return;
  const current = (arch.target_deck || []).map((e) => `${e.qty} ${e.name}`).join('\n');
  const fromPlan = S.view.matchups[id]?.base_postboard || '';
  openSheet({
    title: `Post-board deck vs ${arch.name}`,
    wide: true,
    body: `<p class="muted">The 60 cards you want to play after sideboarding in this matchup.</p>
      ${fromPlan ? '<div class="btn-row"><button type="button" class="btn small" id="target-from-plan">Start from current deck and plan</button></div>' : ''}
      ${field('Decklist', `<textarea name="text" id="target-text" rows="14" spellcheck="false" placeholder="4 Lightning Bolt\n4 Goblin Guide\n…">${esc(current)}</textarea>`,
        'One “qty name” per line. Blank lines between groups are fine.')}
      <p class="preview" id="target-count"></p>`,
    submitLabel: 'Save',
    extraButtons: current ? `<button type="button" class="btn danger" data-act="target-clear" data-id="${esc(id)}">Clear</button>` : '',
    onSubmit: async (form) => {
      const res = await op({ op: 'set_target_deck', archetype_id: id, text: form.text.value });
      if (!res) return false;
      toast(res.result?.message || 'Saved.');
      if (res.result?.unparsed?.length) toast(`${res.result.unparsed.length} line(s) not understood: ${res.result.unparsed.slice(0, 3).join('; ')}`, 'warn');
      return true;
    },
  });
  const form = $('#sheet form');
  let timer = 0, seq = 0;
  const count = () => {
    clearTimeout(timer);
    timer = setTimeout(async () => {
      const mine = ++seq;
      try {
        const p = await api('POST', '/api/parse-decklist', { text: form.text.value, name: '' });
        if (mine !== seq) return;
        // Same rule as the server: a full 60 drops a trailing sideboard.
        const n = p.main_count >= 60 ? p.main_count : p.main_count + p.side_count;
        let msg = n === 60 ? '60 cards' : n < 60 ? `${n} cards · ${60 - n} short of 60` : `${n} cards · ${n - 60} over 60`;
        if (p.main_count >= 60 && p.side_count) msg += ` · the ${p.side_count}-card sideboard is ignored`;
        if (p.unparsed.length) msg += `\n⚠ ${p.unparsed.length} unparsed line(s): ${p.unparsed.slice(0, 3).join('; ')}`;
        $('#target-count').textContent = msg;
      } catch { /* preview only */ }
    }, 200);
  };
  form.text.addEventListener('input', count);
  $('#target-from-plan')?.addEventListener('click', () => {
    form.text.value = fromPlan;
    count();
  });
  count();
}

// ----- report view ---------------------------------------------------------------------------

function viewReport() {
  const modes = [['base', 'Base plans'], ['play', 'On the play'], ['draw', 'On the draw']];
  const seg = modes.map(([key, label]) => `<button class="seg-btn ${S.reportMode === key ? 'active' : ''}"
    data-act="report-mode" data-mode="${key}" aria-pressed="${S.reportMode === key}">${label}</button>`).join('');
  let body = '<p class="muted">Loading…</p>';
  if (S.report && S.report.mode === S.reportMode) {
    const rows = S.report.rows.map((r) => `<tr><td>${esc(r.name)}</td>
      <td>${r.out_count || ''}</td><td>${r.in_count || ''}</td><td>${r.out_qty || ''}</td><td>${r.in_qty || ''}</td></tr>`).join('');
    body = `<table class="freq-table">
      <thead><tr><th>Card</th><th title="Matchups it comes out in">OUT<small>matchups</small></th>
      <th title="Matchups it comes in for">IN<small>matchups</small></th><th>OUT<small>qty</small></th><th>IN<small>qty</small></th></tr></thead>
      <tbody>${rows || '<tr><td colspan="5" class="muted">(no plans yet)</td></tr>'}</tbody></table>`;
  }
  return `<section class="pane">
    <div class="pane-head"><h2>Frequency report</h2>
      <a class="btn small" href="${fileURL(`/report.csv?mode=${S.reportMode}`)}" download>Export CSV</a></div>
    <p class="hint">How often each card is boarded out / in across your ${S.view.doc.archetypes.length} matchups.
      Effective counts combine the base plan with the play/draw changes.</p>
    <div class="seg" role="group" aria-label="Counting mode">${seg}</div>
    ${body}
  </section>`;
}

// ----- sheets (modal dialogs) ------------------------------------------------------------------

let sheetSubmit = null;

function openSheet({ title, body, submitLabel, onSubmit, extraButtons = '', wide = false }) {
  const dlg = $('#sheet');
  sheetSubmit = onSubmit || null;
  dlg.classList.toggle('wide', wide);
  dlg.innerHTML = `<form class="sheet-inner" novalidate>
    <header class="sheet-head">
      <h2 id="sheet-title">${esc(title)}</h2>
      <button type="button" class="icon-btn" data-act="sheet-close" aria-label="Close">✕</button>
    </header>
    <div class="sheet-body">${body}</div>
    ${onSubmit || extraButtons ? `<footer class="sheet-foot">
      ${extraButtons}
      <span class="spacer"></span>
      ${onSubmit ? `<button type="button" class="btn" data-act="sheet-close">Cancel</button>
      <button type="submit" class="btn primary">${esc(submitLabel || 'OK')}</button>` : ''}
    </footer>` : ''}
  </form>`;
  if (!dlg.open) dlg.showModal();
  const first = $('[autofocus]', dlg);
  // Avoid popping the keyboard over the sheet on touch screens.
  if (first && !matchMedia('(pointer: coarse)').matches) first.focus();
  return dlg;
}

function closeSheet() {
  const dlg = $('#sheet');
  if (dlg.open) dlg.close();
  sheetSubmit = null;
}

document.addEventListener('submit', async (e) => {
  if (!e.target.closest('#sheet')) return;
  e.preventDefault();
  if (!sheetSubmit) return;
  const form = e.target;
  const btn = $('button[type=submit]', form);
  if (btn) btn.disabled = true;
  try {
    const ok = await sheetSubmit(form);
    if (ok !== false) closeSheet();
  } finally {
    if (btn) btn.disabled = false;
  }
});

$('#sheet').addEventListener('click', (e) => {
  if (e.target === e.currentTarget) closeSheet(); // backdrop tap
});
$('#sheet').addEventListener('close', () => { sheetSubmit = null; });

function field(label, input, hint = '') {
  return `<label class="field"><span class="label">${label}</span>${input}${hint ? `<span class="hint">${hint}</span>` : ''}</label>`;
}

function stepper(name, value) {
  return `<div class="stepper">
    <button type="button" class="mini-btn" data-step="-1" aria-label="Decrease">−</button>
    <input type="number" name="${name}" value="${value}" min="1" inputmode="numeric" pattern="[0-9]*">
    <button type="button" class="mini-btn" data-step="1" aria-label="Increase">+</button>
  </div>`;
}

document.addEventListener('click', (e) => {
  const b = e.target.closest('.stepper [data-step]');
  if (!b) return;
  const input = $('input', b.parentElement);
  input.value = Math.max(1, (parseInt(input.value, 10) || 1) + Number(b.dataset.step));
});

// Suggestion dropdown under an input. source(text) -> [{value, label?, hint?}] (or a promise).
function attachSuggest(input, source, onPick) {
  const box = document.createElement('div');
  box.className = 'suggest';
  box.hidden = true;
  input.parentElement.classList.add('has-suggest');
  input.after(box);
  input.setAttribute('autocomplete', 'off');
  let seq = 0, timer = 0, active = -1;

  const show = (items) => {
    active = -1;
    box.innerHTML = items.map((it) => `<button type="button" tabindex="-1" data-value="${esc(it.value)}">
      <span>${esc(it.label ?? it.value)}</span>${it.hint ? `<small>${esc(it.hint)}</small>` : ''}</button>`).join('');
    box.hidden = !items.length || document.activeElement !== input;
  };
  const update = () => {
    clearTimeout(timer);
    timer = setTimeout(async () => {
      const mine = ++seq;
      const items = await Promise.resolve(source(input.value)).catch(() => []);
      if (mine === seq) show(items.slice(0, 12));
    }, 120);
  };
  const pick = (value) => {
    input.value = value;
    box.hidden = true;
    onPick?.(value);
  };
  input.addEventListener('input', update);
  input.addEventListener('focus', update);
  input.addEventListener('blur', () => setTimeout(() => { box.hidden = true; }, 150));
  input.addEventListener('keydown', (e) => {
    const buttons = $$('button', box);
    if (box.hidden || !buttons.length) return;
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault();
      active = (active + (e.key === 'ArrowDown' ? 1 : -1) + buttons.length) % buttons.length;
      buttons.forEach((b, i) => b.classList.toggle('active', i === active));
    } else if (e.key === 'Enter' && active >= 0) {
      e.preventDefault();
      pick(buttons[active].dataset.value);
    } else if (e.key === 'Escape') {
      e.preventDefault();
      box.hidden = true;
    }
  });
  box.addEventListener('mousedown', (e) => e.preventDefault()); // keep input focus
  box.addEventListener('click', (e) => {
    const b = e.target.closest('button');
    if (b) pick(b.dataset.value);
  });
}

const cardSource = (text) => (text.trim().length < 2 ? []
  : api('GET', `/api/cards?q=${enc(text)}&limit=12`).then((names) => names.map((n) => ({ value: n }))));

function listSource(names) {
  return (text) => {
    const q = fold(text.trim());
    const pre = [], sub = [];
    for (const n of names) {
      const f = fold(n);
      if (!q || f.startsWith(q)) pre.push(n);
      else if (f.includes(q)) sub.push(n);
    }
    return [...pre, ...sub].filter((n) => fold(n) !== q).map((n) => ({ value: n }));
  };
}

// ----- sheets: documents & settings -------------------------------------------------------------

async function sheetFiles() {
  openSheet({ title: 'Documents', body: '<p class="muted">Loading…</p>' });
  let data;
  try {
    data = await api('GET', '/api/files');
  } catch (err) {
    toast(err.message, 'error');
    return;
  }
  const files = data.files.map((f) => `<li>
      <button type="button" class="file-row ${f.file === S.file ? 'current' : ''}" data-act="file-open" data-file="${esc(f.file)}" ${f.error ? 'disabled' : ''}>
        <span class="name">${esc(f.deck_name || f.file)}</span>
        <small>${esc(f.file)} · ${f.error ? `<span class="warn-text">${esc(f.error)}</span>` : new Date(f.modified).toLocaleString()}</small>
      </button>
    </li>`).join('');
  const current = S.file ? `<div class="btn-row">
      <a class="btn small" href="${fileURL('/download')}" download>Download ${esc(S.file)}</a>
      <button type="button" class="btn small" data-act="file-copy">Save a copy as…</button>
    </div>` : '';
  openSheet({
    title: 'Documents',
    body: `${current}
      <h3 class="sub">Open</h3>
      <ul class="file-list">${files || '<li class="muted">No documents yet.</li>'}</ul>
      <p class="hint">Folder on the server: <code>${esc(data.dir)}</code></p>
      <h3 class="sub">New document</h3>
      <div class="inline-form">
        <input name="deck_name" placeholder="Deck name, e.g. Mono-Red Burn" aria-label="New deck name">
        <button type="submit" class="btn primary">Create</button>
      </div>`,
    onSubmit: async (form) => {
      const deckName = form.deck_name.value.trim();
      if (!deckName) { form.deck_name.focus(); return false; }
      try {
        applyFile(await api('POST', '/api/files', { deck_name: deckName }));
        toast(`Created ${S.file}`);
        return true;
      } catch (err) { toast(err.message, 'error'); return false; }
    },
  });
  // The footer's Cancel/Create pair is redundant here.
  $('#sheet .sheet-foot')?.remove();
}

function sheetCopy() {
  const deck = S.view.doc.deck;
  const suggested = (deck.name || 'deck').replace(/[^\p{L}\p{N} _-]/gu, '').trim().replace(/ /g, '_') || 'deck';
  openSheet({
    title: 'Save a copy as',
    body: field('File name', `<input name="file" value="${esc(suggested)}-copy.sbd.json" autofocus>`,
      'Saved next to the other documents; an existing file is never overwritten.'),
    submitLabel: 'Save copy',
    onSubmit: async (form) => {
      try {
        applyFile(await api('POST', fileURL('/copy'), { file: form.file.value.trim() }));
        toast(`Now editing ${S.file}`);
        return true;
      } catch (err) { toast(err.message, 'error'); return false; }
    },
  });
}

let pollTimer = 0;

async function sheetSettings() {
  let st;
  try {
    st = await api('GET', '/api/settings');
  } catch (err) {
    toast(err.message, 'error');
    return;
  }
  const radios = Object.entries(st.sources).map(([key, label]) => `<label class="radio">
    <input type="radio" name="source" value="${key}" ${st.card_source === key ? 'checked' : ''}> ${esc(label)}</label>`).join('');
  openSheet({
    title: 'Settings',
    wide: true,
    body: `<h3 class="sub">Card database</h3>
      <p class="muted">Card-name autocomplete uses a local copy of every card name, downloaded once
      and shared with the terminal app. Update it whenever new sets come out.</p>
      <div class="radio-row" role="radiogroup" aria-label="Card database source">${radios}</div>
      <p id="db-status" class="status-line"></p>
      <button type="button" class="btn" data-act="carddb-update" id="carddb-update">Update card database now</button>
      <h3 class="sub">Documents folder</h3>
      <p><code>${esc(st.dir)}</code></p>
      <h3 class="sub">How it works</h3>
      <ul class="help">
        <li><strong>Plans:</strong> pick an opponent archetype, then the <em>Base</em> layer or an
          <em>On the play / On the draw</em> layer. The effective plan is the base plus that layer's changes
          (quantities summed per card).</li>
        <li><strong>Deck:</strong> import a Moxfield “MTGO” export (blank line before the sideboard),
          or add, rename and adjust cards one at a time.</li>
        <li><strong>Build:</strong> give each matchup its share of the field and the 60 cards you want
          after sideboarding; it suggests the main deck, sideboard and plans that get closest across the field.</li>
        <li><strong>Results:</strong> log matches as games won-lost (2-1, 1-2, 1-1…). Each result is pinned
          to the deck revision it was played with. Deck edits after logging results freeze the old list;
          the revision number advances only when the revised deck is used (a plan edit or a new result).</li>
        <li><strong>Report:</strong> how often each card comes out or in across matchups.</li>
        <li>Every change is saved to the server immediately, in the same file format as the terminal app.</li>
      </ul>`,
  });
  renderCardStatus(st);
  for (const r of $$('#sheet input[name=source]')) {
    r.addEventListener('change', async () => {
      try { renderCardStatus(await api('POST', '/api/settings', { card_source: r.value })); } catch (err) { toast(err.message, 'error'); }
    });
  }
}

function renderCardStatus(st) {
  const el = $('#db-status');
  const btn = $('#carddb-update');
  if (!el || !btn) return;
  const db = st.db;
  let text = db.available
    ? `Loaded ${db.count.toLocaleString()} card names (source: ${st.sources[db.source] || db.source || '?'}, updated: ${db.updated || '?'}).`
    : 'No card database yet — update to enable name autocomplete.';
  if (st.job.running || st.job.message || st.job.error) text = st.job.error || st.job.message;
  el.textContent = text;
  el.classList.toggle('warn-text', !!st.job.error);
  btn.disabled = st.job.running;
  clearTimeout(pollTimer);
  if (st.job.running) {
    pollTimer = setTimeout(async () => {
      if (!$('#db-status')) return;
      try { renderCardStatus(await api('GET', '/api/carddb/status')); } catch { /* retry on next open */ }
    }, 1000);
  }
}

// ----- sheets: deck --------------------------------------------------------------------------

function sheetImport() {
  const deck = S.view.doc.deck;
  const hasCards = deck.mainboard.length || deck.sideboard.length;
  openSheet({
    title: 'Import decklist',
    wide: true,
    body: `${field('Deck name', `<input name="name" value="${esc(deck.name)}">`)}
      ${field('Decklist', '<textarea name="text" rows="12" autofocus spellcheck="false" placeholder="4 Lightning Bolt\n4 Goblin Guide\n…\n\n3 Smash to Smithereens"></textarea>',
        'Paste a Moxfield “MTGO” export: qty + name per line, a blank line before the sideboard. “Sideboard” headers, SB: prefixes and set codes are fine.')}
      <p class="preview" id="import-preview">Mainboard: 0 cards · Sideboard: 0 cards</p>
      ${hasCards ? '<p class="hint">Replaces the current decklist. Results already logged keep the list they were played with.</p>' : ''}`,
    submitLabel: 'Import',
    onSubmit: async (form) => {
      const res = await op({ op: 'import_deck', text: form.text.value, name: form.name.value });
      if (!res) return false;
      const r = res.result || {};
      toast(r.message || 'Imported.');
      if (r.unparsed?.length) toast(`${r.unparsed.length} line(s) not understood: ${r.unparsed.slice(0, 3).join('; ')}`, 'warn');
      return true;
    },
  });
  const form = $('#sheet form');
  let timer = 0, seq = 0;
  const preview = () => {
    clearTimeout(timer);
    timer = setTimeout(async () => {
      const mine = ++seq;
      try {
        const p = await api('POST', '/api/parse-decklist', { text: form.text.value, name: form.name.value });
        if (mine !== seq) return;
        let msg = `Mainboard: ${p.main_count} cards (${p.main_unique} unique) · Sideboard: ${p.side_count} cards (${p.side_unique} unique)`;
        if (p.unparsed.length) msg += `\n⚠ ${p.unparsed.length} unparsed line(s): ${p.unparsed.slice(0, 3).join('; ')}`;
        $('#import-preview').textContent = msg;
      } catch { /* preview only */ }
    }, 200);
  };
  form.text.addEventListener('input', preview);
}

function sheetDeckInfo() {
  const deck = S.view.doc.deck;
  openSheet({
    title: 'Deck details',
    body: `${field('Deck name', `<input name="name" value="${esc(deck.name)}" autofocus>`)}
      ${field('Format', `<input name="format" value="${esc(deck.format)}" placeholder="e.g. Modern">`)}`,
    submitLabel: 'Save',
    onSubmit: async (form) => !!(await op({ op: 'set_deck_info', name: form.name.value, format: form.format.value })),
  });
}

function sheetDeckCard(board, card) {
  const entries = board === 'main' ? S.view.doc.deck.mainboard : S.view.doc.deck.sideboard;
  const current = card ? entries.find((e) => e.name === card) : null;
  const where = board === 'main' ? 'mainboard' : 'sideboard';
  openSheet({
    title: current ? `Edit / replace card` : `Add to ${where}`,
    body: `${field('Card name', `<input name="name" value="${esc(current?.name || '')}" placeholder="Start typing…" autofocus autocapitalize="words">`,
        current ? 'Change the name to replace this card (e.g. swap one card for another).' : '')}
      ${field('Quantity', stepper('qty', current?.qty || 1))}`,
    submitLabel: current ? 'Save' : 'Add',
    extraButtons: current ? '<button type="button" class="btn danger" data-act="deck-delete">Remove</button>' : '',
    onSubmit: async (form) => {
      const name = form.name.value.trim();
      if (!name) { form.name.focus(); return false; }
      const qty = Math.max(1, parseInt(form.qty.value, 10) || 1);
      const payload = current
        ? { op: 'deck_edit', board, card: current.name, name, qty }
        : { op: 'deck_add', board, name, qty };
      return !!(await op(payload));
    },
  });
  const dlg = $('#sheet');
  dlg.dataset.board = board;
  dlg.dataset.card = current?.name || '';
  attachSuggest($('input[name=name]', dlg), cardSource);
}

// ----- sheets: plans ------------------------------------------------------------------------

function sheetPlanAdd(list, prefill = '') {
  const arch = currentArch();
  if (!arch) return;
  const deck = S.view.doc.deck;
  const entries = list === 'out' ? deck.mainboard : deck.sideboard;
  const used = list === 'out' ? allocations(arch).out : allocations(arch).in;
  const remaining = (name) => {
    const e = entries.find((x) => fold(x.name) === fold(name));
    return e ? Math.max(0, e.qty - (used[fold(e.name)] || 0)) : 0;
  };
  const layerName = { base: 'base plan', play: 'on-the-play changes', draw: 'on-the-draw changes' }[S.layer];
  openSheet({
    title: list === 'out' ? 'Take OUT (from mainboard)' : 'Bring IN (from sideboard)',
    body: `<p class="muted">vs ${esc(arch.name)} · ${layerName}</p>
      ${field('Card', `<input name="name" value="${esc(prefill)}" placeholder="Filter or type a card name" autocapitalize="words">`)}
      ${field('Quantity', stepper('qty', prefill ? (remaining(prefill) || 1) : 1))}
      <ul class="pick-list" id="pick-list"></ul>`,
    submitLabel: list === 'out' ? 'Take out' : 'Bring in',
    onSubmit: async (form) => {
      const name = form.name.value.trim();
      if (!name) { form.name.focus(); return false; }
      const qty = Math.max(1, parseInt(form.qty.value, 10) || 1);
      return !!(await op({ op: 'plan_add', archetype_id: arch.id, layer: S.layer, list, name, qty }));
    },
  });
  const form = $('#sheet form');
  const renderPicks = () => {
    const q = fold(form.name.value.trim());
    const rows = entries.filter((e) => !q || fold(e.name).includes(q)).map((e) => {
      const left = remaining(e.name);
      return `<li><button type="button" class="pick ${fold(e.name) === q ? 'active' : ''} ${left ? '' : 'spent'}" data-pick="${esc(e.name)}">
        <span class="qty">${left}<small>/${e.qty}</small></span><span class="name">${esc(e.name)}</span></button></li>`;
    }).join('');
    $('#pick-list').innerHTML = rows || `<li class="muted">${entries.length ? 'No match — the typed name will be used as is.'
      : `Your ${list === 'out' ? 'mainboard' : 'sideboard'} is empty; type a card name.`}</li>`;
  };
  form.name.addEventListener('input', renderPicks);
  $('#pick-list').addEventListener('click', (e) => {
    const b = e.target.closest('[data-pick]');
    if (!b) return;
    form.name.value = b.dataset.pick;
    form.qty.value = remaining(b.dataset.pick) || 1;
    renderPicks();
  });
  renderPicks();
}

function sheetArchAdd() {
  openSheet({
    title: 'New archetype',
    body: field('Opponent archetype', '<input name="name" placeholder="e.g. Azorius Control" autofocus autocapitalize="words">'),
    submitLabel: 'Add',
    onSubmit: async (form) => {
      const name = form.name.value.trim();
      if (!name) { form.name.focus(); return false; }
      const res = await op({ op: 'add_archetype', name });
      if (!res) return false;
      S.archId = res.result.message; // the new archetype's id
      store.set('arch', S.archId);
      render();
      return true;
    },
  });
}

function sheetArchMenu() {
  const arch = currentArch();
  if (!arch) return;
  openSheet({
    title: 'Edit matchup',
    body: `${field('Opponent archetype', `<input name="name" value="${esc(arch.name)}" autocapitalize="words">`)}
      ${field('Notes', `<textarea name="notes" rows="4" placeholder="Key cards, how the matchup plays…">${esc(arch.notes)}</textarea>`)}`,
    submitLabel: 'Save',
    extraButtons: '<button type="button" class="btn danger" data-act="arch-remove">Remove</button>',
    onSubmit: async (form) => !!(await op({ op: 'update_archetype', archetype_id: arch.id, name: form.name.value, notes: form.notes.value })),
  });
}

function sheetConfirm(question, yesLabel, onYes) {
  openSheet({
    title: question,
    body: '',
    submitLabel: yesLabel,
    onSubmit: onYes,
  });
  $('#sheet button[type=submit]').classList.replace('primary', 'danger');
}

// ----- sheets: results ------------------------------------------------------------------------

const GAME_SCORES = ['2-0', '2-1', '1-2', '0-2', '1-1', '1-0', '0-1'];

function sheetResult(id) {
  const v = S.view;
  const existing = id ? v.doc.results.find((r) => r.id === id) : null;
  const last = v.doc.results[v.doc.results.length - 1];
  const r = existing || {
    date: todayISO(),
    // Most matches in a session are at the same event: carry it over.
    event: last && last.date === todayISO() ? last.event : '',
    archetype: '', play_draw: '', games_won: null, games_lost: null, notes: '',
  };
  const games = r.games_won == null ? '' : `${r.games_won}-${r.games_lost}`;
  const pd = [['play', 'On the play'], ['draw', 'On the draw'], ['', 'Unknown']].map(([val, label]) => `<label class="seg-radio">
    <input type="radio" name="play_draw" value="${val}" ${r.play_draw === val ? 'checked' : ''}><span>${label}</span></label>`).join('');
  const chips = GAME_SCORES.map((g) => `<button type="button" class="chip ${g === games ? 'active' : ''}" data-games="${g}">${g}</button>`).join('');
  const revBtn = existing && existing.deck_revision != null
    ? `<button type="button" class="btn" data-act="view-rev" data-id="${esc(existing.id)}">Deck rev ${existing.deck_revision}</button>` : '';
  openSheet({
    title: existing ? 'Edit match result' : 'New match result',
    body: `${field('Opponent archetype', `<input name="archetype" value="${esc(r.archetype)}" placeholder="e.g. Azorius Control" autocapitalize="words" ${existing ? '' : 'autofocus'}>`)}
      <div class="field"><span class="label">Games won-lost</span>
        <div class="chips" id="games-chips">${chips}</div>
        <input name="games" value="${esc(games)}" placeholder="e.g. 2-1" inputmode="numeric" aria-label="Games won-lost">
      </div>
      <div class="field"><span class="label">Play / draw</span><div class="seg seg-radios">${pd}</div></div>
      <div class="two-col">
        ${field('Date', `<input type="date" name="date" value="${esc(r.date)}">`)}
        ${field('Event', `<input name="event" value="${esc(r.event)}" placeholder="e.g. FNM, RCQ…">`)}
      </div>
      ${field('Notes', `<input name="notes" value="${esc(r.notes)}">`)}`,
    submitLabel: existing ? 'Save' : 'Add result',
    extraButtons: existing ? `<button type="button" class="btn danger" data-act="result-delete" data-id="${esc(existing.id)}">Delete</button>${revBtn}` : '',
    onSubmit: async (form) => {
      const result = {
        date: form.date.value, event: form.event.value, archetype: form.archetype.value,
        play_draw: form.play_draw.value, games: form.games.value, notes: form.notes.value,
      };
      if (!result.games.trim()) {
        toast('Enter the games won-lost, e.g. 2-1', 'error');
        form.games.focus();
        return false;
      }
      const res = await op(existing ? { op: 'result_update', result_id: existing.id, result } : { op: 'result_add', result });
      return !!res;
    },
  });
  const form = $('#sheet form');
  attachSuggest(form.archetype, listSource(v.archetype_candidates));
  const events = [...new Set(v.doc.results.map((x) => x.event.trim()).filter(Boolean))].reverse();
  attachSuggest(form.event, listSource(events));
  $('#games-chips').addEventListener('click', (e) => {
    const b = e.target.closest('[data-games]');
    if (!b) return;
    form.games.value = b.dataset.games;
    $$('#games-chips .chip').forEach((c) => c.classList.toggle('active', c === b));
  });
  form.games.addEventListener('input', () => {
    $$('#games-chips .chip').forEach((c) => c.classList.toggle('active', c.dataset.games === form.games.value.trim()));
  });
}

function sheetRevision(resultId) {
  const v = S.view;
  const r = v.doc.results.find((x) => x.id === resultId);
  const rev = r && r.deck_revision != null ? v.revision_decks[r.deck_revision] : null;
  if (!rev) {
    toast('No deck revision recorded for this result.', 'warn');
    return;
  }
  const d = rev.deck;
  openSheet({
    title: `Deck revision ${r.deck_revision}`,
    wide: true,
    body: `<p class="muted">${esc(r.date)} vs ${esc(r.archetype || '?')} · ${esc(d.name || 'Untitled')} —
      ${total(d.mainboard)} main / ${total(d.sideboard)} side</p>
      <pre class="decklist">${esc(rev.text)}</pre>`,
  });
}

// ----- actions ------------------------------------------------------------------------------

const actions = {
  tab(d) {
    S.tab = d.tab;
    store.set('tab', S.tab);
    render();
    window.scrollTo(0, 0);
    if (S.tab === 'report') loadReport();
  },
  files: sheetFiles,
  settings: sheetSettings,
  'sheet-close': closeSheet,
  async 'file-open'(d) {
    if (await openFile(d.file)) closeSheet();
  },
  'file-copy': sheetCopy,
  'carddb-update': async () => {
    const source = $('#sheet input[name=source]:checked')?.value || 'mtgjson';
    try { renderCardStatus(await api('POST', '/api/carddb/update', { source })); } catch (err) { toast(err.message, 'error'); }
  },

  'arch-select'(d) {
    S.archId = d.id;
    store.set('arch', S.archId);
    render();
  },
  'arch-add': sheetArchAdd,
  'arch-menu': sheetArchMenu,
  'arch-remove'() {
    const arch = currentArch();
    if (!arch) return;
    sheetConfirm(`Remove archetype “${arch.name}”?`, 'Remove',
      async () => !!(await op({ op: 'remove_archetype', archetype_id: arch.id })));
  },
  layer(d) {
    S.layer = d.layer;
    render();
  },
  'plan-add'(d) { sheetPlanAdd(d.list); },
  'plan-remove'(d) {
    op({ op: 'plan_remove', archetype_id: S.archId, layer: S.layer, list: d.list, card: d.card });
  },
  'plan-qty'(d) {
    op({ op: 'plan_qty', archetype_id: S.archId, layer: S.layer, list: d.list, card: d.card, delta: Number(d.delta) });
  },
  'deck-to-plan'(d) {
    if (!currentArch()) return;
    sheetPlanAdd(d.board === 'main' ? 'out' : 'in', d.card);
  },

  import: sheetImport,
  'deck-info': sheetDeckInfo,
  'deck-add'(d) { sheetDeckCard(d.board); },
  'deck-edit'(d) { sheetDeckCard(d.board, d.card); },
  'deck-qty'(d) {
    op({ op: 'deck_qty', board: d.board, card: d.card, delta: Number(d.delta) });
  },
  async 'deck-delete'() {
    const dlg = $('#sheet');
    if (await op({ op: 'deck_delete', board: dlg.dataset.board, card: dlg.dataset.card })) closeSheet();
  },

  'result-new'() { sheetResult(null); },
  'result-edit'(d) { sheetResult(d.id); },
  'result-delete'(d) {
    const r = S.view.doc.results.find((x) => x.id === d.id);
    if (!r) return;
    sheetConfirm(`Delete result “${r.date} vs ${r.archetype || '?'} (${r.games_won}-${r.games_lost})”?`, 'Delete',
      async () => !!(await op({ op: 'result_delete', result_id: r.id })));
  },
  'view-rev'(d) { sheetRevision(d.id); },

  'target-edit'(d) { sheetTarget(d.id); },
  async 'target-clear'(d) {
    if (await op({ op: 'set_target_deck', archetype_id: d.id, text: '' })) closeSheet();
  },
  'fit-toggle'(d) {
    if (S.openFits.has(d.id)) S.openFits.delete(d.id); else S.openFits.add(d.id);
    renderMain();
  },
  'apply-suggestion'() {
    const sug = S.view.suggestion;
    openSheet({
      title: 'Use the suggested 75?',
      body: `<p>This replaces your deck with the suggested ${total(sug.main)}-card main deck and
        ${total(sug.side)}-card sideboard, and the base sideboard plan for ${sug.matchups.length}
        ${sug.matchups.length === 1 ? 'matchup' : 'matchups'}. On-the-play and on-the-draw changes are kept.</p>
        <p class="muted">Results you’ve logged keep the decklist they were played with.</p>`,
      submitLabel: 'Use it',
      onSubmit: async () => {
        const res = await op({ op: 'apply_suggestion' });
        if (res) toast(res.result?.message || 'Deck updated.');
        return !!res;
      },
    });
  },

  'report-mode'(d) {
    S.reportMode = d.mode;
    renderMain();
    loadReport();
  },
};

document.addEventListener('click', (e) => {
  const el = e.target.closest('[data-act]');
  if (!el || el.disabled) return;
  const fn = actions[el.dataset.act];
  if (!fn) return;
  e.preventDefault();
  e.stopPropagation(); // a button inside a tappable row shouldn't also trigger the row
  fn(el.dataset, el);
});

// Keyboard access for tappable table rows.
document.addEventListener('keydown', (e) => {
  if ((e.key === 'Enter' || e.key === ' ') && e.target.matches('tr[data-act]')) {
    e.preventDefault();
    e.target.click();
  }
});

// Metagame shares save when the field is committed (blur / enter).
document.addEventListener('change', (e) => {
  const input = e.target.closest?.('input[data-share]');
  if (!input) return;
  const text = input.value.trim();
  const share = text === '' ? null : Number(text);
  if (share !== null && !(share >= 0 && share <= 100)) {
    toast('Enter a share between 0 and 100%.', 'error');
    return;
  }
  op({ op: 'set_meta_share', archetype_id: input.dataset.share, share });
});

document.addEventListener('toggle', (e) => {
  if (e.target.matches?.('details.effective')) {
    S.showEffective = e.target.open;
    store.set('effective', S.showEffective);
  }
}, true);

// Pick up edits made from another device (or the TUI) when coming back to the tab.
document.addEventListener('visibilitychange', () => {
  if (document.visibilityState === 'visible') reloadFile();
});

// ----- start-up -----------------------------------------------------------------------------

async function start() {
  render();
  let files = [];
  try {
    files = (await api('GET', '/api/files')).files;
  } catch (err) {
    toast(err.message, 'error');
  }
  const last = store.get('file', null);
  const target = files.find((f) => f.file === last && !f.error) || files.find((f) => !f.error);
  if (target) await openFile(target.file);
  else render();
}

start();
