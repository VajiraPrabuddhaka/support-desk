'use strict';

// GeoGrid Support Desk console. Plain JavaScript, no build step.

const $ = (sel, root = document) => root.querySelector(sel);
const $$ = (sel, root = document) => [...root.querySelectorAll(sel)];

const state = {
  tickets: [],
  filter: 'all',
  selected: null,   // ticket number
  detail: null,     // ticket detail
  job: null,        // latest triage job of the selected ticket
  jobTimer: null,
  clockTimer: null,
  account: null,
  accountFor: null,
  lastInvoices: null,
};

// ------------------------------------------------------------------ helpers

async function api(path, options = {}) {
  const res = await fetch(path, {
    ...options,
    headers: { 'Content-Type': 'application/json', ...(options.headers || {}) },
  });
  const text = await res.text();
  let body = null;
  if (text) {
    try { body = JSON.parse(text); } catch { body = { error: text }; }
  }
  if (!res.ok) throw new Error((body && body.error) || `HTTP ${res.status}`);
  return body;
}

function esc(value) {
  return String(value ?? '').replace(/[&<>"']/g, c => (
    { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
}

// inline formats escaped text: code, bold, italics and http(s) links.
function inline(s) {
  return s
    .replace(/`([^`]+)`/g, '<code>$1</code>')
    .replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>')
    .replace(/(^|[^*\w])\*([^*\s][^*]*?)\*(?!\w)/g, '$1<em>$2</em>')
    .replace(/\[([^\]]+)\]\((https?:\/\/[^\s)]+)\)/g, '<a href="$2" target="_blank" rel="noopener noreferrer">$1</a>');
}

// markdown renders the small Markdown subset the agents write. Everything is
// escaped first, so ticket text written by customers cannot inject markup.
function markdown(src) {
  const lines = esc(src).replace(/\r\n/g, '\n').split('\n');
  let html = '';
  let para = [];
  let list = null;
  const flushPara = () => {
    if (para.length) { html += `<p>${inline(para.join('<br>'))}</p>`; para = []; }
  };
  const flushList = () => {
    if (list) {
      html += `<${list.type}>${list.items.map(item => `<li>${inline(item)}</li>`).join('')}</${list.type}>`;
      list = null;
    }
  };
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    if (/^\s*```/.test(line)) {
      flushPara(); flushList();
      const code = [];
      for (i++; i < lines.length && !/^\s*```/.test(lines[i]); i++) code.push(lines[i]);
      html += `<pre><code>${code.join('\n')}</code></pre>`;
      continue;
    }
    const heading = line.match(/^(#{1,4})\s+(.*)$/);
    if (heading) {
      flushPara(); flushList();
      const level = Math.min(heading[1].length + 2, 6);
      html += `<h${level}>${inline(heading[2])}</h${level}>`;
      continue;
    }
    const ul = line.match(/^\s*[-*]\s+(.*)$/);
    const ol = line.match(/^\s*\d+[.)]\s+(.*)$/);
    if (ul || ol) {
      flushPara();
      const type = ul ? 'ul' : 'ol';
      if (!list || list.type !== type) { flushList(); list = { type, items: [] }; }
      list.items.push((ul || ol)[1]);
      continue;
    }
    if (/^\s*(---|\*\*\*)\s*$/.test(line)) { flushPara(); flushList(); html += '<hr>'; continue; }
    if (line.trim() === '') { flushPara(); flushList(); continue; }
    if (list && /^\s{2,}\S/.test(line)) { list.items[list.items.length - 1] += ' ' + line.trim(); continue; }
    flushList();
    para.push(line);
  }
  flushPara(); flushList();
  return html;
}

function ago(iso) {
  const seconds = Math.max(0, (Date.now() - new Date(iso).getTime()) / 1000);
  if (seconds < 60) return 'just now';
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  return `${Math.floor(hours / 24)}d ago`;
}

function clock(ms) {
  const s = Math.max(0, Math.round(ms / 1000));
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`;
}

function initials(name, email) {
  const src = (name || email || '?').replace(/@.*/, '');
  const parts = src.split(/[\s._-]+/).filter(Boolean);
  return ((parts[0] || '?')[0] + (parts.length > 1 ? parts[parts.length - 1][0] : '')).toUpperCase();
}

function money(cents, currency) {
  return `${(cents / 100).toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 })} ${currency || ''}`.trim();
}

const statusText = { 'new': 'New', 'triaged': 'Triaged', 'needs-info': 'Needs info' };

function labelChip(label, cls = 'tag') {
  const text = label.name.replace(/^(type|priority|status)\//, '');
  return `<span class="${cls}"><span class="swatch" style="background:${esc(label.color)}"></span>${esc(text)}</span>`;
}

let toastTimer = null;
function toast(message, isError = false) {
  const el = $('#toast');
  el.textContent = message;
  el.classList.toggle('error', isError);
  el.classList.add('show');
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => el.classList.remove('show'), isError ? 6000 : 3200);
}

const icons = {
  check: '<svg viewBox="0 0 12 12"><path d="M2.5 6.3l2.2 2.2 4.8-5" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"/></svg>',
  cross: '<svg viewBox="0 0 12 12"><path d="M3 3l6 6M9 3l-6 6" stroke="currentColor" stroke-width="1.7" stroke-linecap="round"/></svg>',
  arrow: '<svg viewBox="0 0 12 12"><path d="M2 6h7M6.5 3l3 3-3 3" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"/></svg>',
  reply: '<svg viewBox="0 0 12 12"><path d="M10 6H3M5.5 3l-3 3 3 3" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"/></svg>',
  spark: '<svg viewBox="0 0 12 12"><path d="M6 1.5l1.1 3.4L10.5 6 7.1 7.1 6 10.5 4.9 7.1 1.5 6l3.4-1.1z" fill="currentColor"/></svg>',
  dot: '<svg viewBox="0 0 12 12"><circle cx="6" cy="6" r="2" fill="currentColor"/></svg>',
  sparkles: '<svg viewBox="0 0 16 16" aria-hidden="true"><path d="M7 1.5l1.3 3.9 3.9 1.3-3.9 1.3L7 11.9 5.7 8 1.8 6.7 5.7 5.4z" fill="currentColor"/><path d="M12.5 10l.6 1.6 1.6.6-1.6.6-.6 1.6-.6-1.6-1.6-.6 1.6-.6z" fill="currentColor"/></svg>',
  external: '<svg viewBox="0 0 12 12" width="11" height="11" aria-hidden="true"><path d="M5 2.5H2.5v7h7V7M7 2.5h2.5V5M9.3 2.7L5.5 6.5" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" stroke-linejoin="round"/></svg>',
};

// ------------------------------------------------------------------- agent

async function loadOverview() {
  try {
    const o = await api('/api/overview');
    const pill = $('#agentPill');
    const online = o.agent && o.agent.online;
    pill.classList.toggle('online', !!online);
    pill.classList.toggle('offline', !online);
    $('#agentText').textContent = online ? 'support-triage · online' : 'support-triage · unreachable';
    pill.title = (o.agent && o.agent.description) || '';
  } catch {
    $('#agentPill').classList.add('offline');
    $('#agentText').textContent = 'console offline';
  }
}

// ------------------------------------------------------------------- queue

async function loadTickets({ quiet = false } = {}) {
  const btn = $('#refreshBtn');
  if (!quiet) btn.classList.add('spinning');
  try {
    state.tickets = await api('/api/tickets');
    renderQueue();
  } catch (err) {
    if (!quiet) toast(`Could not load the tickets: ${err.message}`, true);
  } finally {
    btn.classList.remove('spinning');
  }
}

function renderQueue() {
  const counts = { all: state.tickets.length, new: 0, triaged: 0, 'needs-info': 0 };
  state.tickets.forEach(t => { counts[t.status] = (counts[t.status] || 0) + 1; });
  $$('#filters button').forEach(b => {
    b.classList.toggle('active', b.dataset.filter === state.filter);
    b.querySelector('span').textContent = counts[b.dataset.filter] || 0;
  });

  const list = $('#ticketList');
  const shown = state.tickets.filter(t => state.filter === 'all' || t.status === state.filter);
  if (shown.length === 0) {
    list.innerHTML = `<li class="list-empty">${state.tickets.length ? 'No tickets here.' : 'No tickets yet.'}</li>`;
    return;
  }
  list.innerHTML = shown.map(t => {
    const tags = [];
    if (t.triaging) tags.push('<span class="tag busy">Triaging</span>');
    if (t.type) tags.push(`<span class="tag">${esc(t.type)}</span>`);
    if (t.priority) tags.push(`<span class="tag">${esc(t.priority)}</span>`);
    const who = [t.company, t.accountId].filter(Boolean).join(' · ') || t.fromEmail || '—';
    return `
      <li class="ticket ${t.number === state.selected ? 'selected' : ''}" data-number="${t.number}">
        <div class="t-top">
          <span class="status-dot status-${esc(t.status)}" title="${esc(statusText[t.status] || t.status)}"></span>
          <span class="t-num">#${t.number}</span>
          <span class="t-time">${esc(ago(t.createdAt))}</span>
        </div>
        <div class="t-title">${esc(t.title)}</div>
        <div class="t-meta">${esc(who)}</div>
        ${tags.length ? `<div class="t-tags">${tags.join('')}</div>` : ''}
      </li>`;
  }).join('');
}

// ------------------------------------------------------------------ detail

async function selectTicket(number, { keepActivity = false } = {}) {
  if (state.selected !== number) {
    stopJobPolling();
    state.job = null;
    state.account = null;
    state.accountFor = null;
    state.lastInvoices = null;
  }
  state.selected = number;
  if (location.hash !== `#ticket-${number}`) history.replaceState(null, '', `#ticket-${number}`);
  renderQueue();

  try {
    state.detail = await api(`/api/tickets/${number}`);
  } catch (err) {
    state.detail = null;
    $('#detail').innerHTML = `<div class="empty"><p>${esc(err.message)}</p></div>`;
    return;
  }
  renderDetail();
  loadAccount();
  if (!keepActivity) {
    try {
      state.job = await api(`/api/tickets/${number}/triage`);
    } catch { state.job = null; }
    renderActivity();
    if (state.job && state.job.state === 'running') pollJob(state.job.id);
  }
}

function renderDetail() {
  const d = state.detail;
  if (!d) return;
  const running = state.job && state.job.state === 'running' && state.job.ticket === d.number;
  const triaged = d.status !== 'new';
  const badges = [`<span class="badge st-${esc(d.status)}">${esc(statusText[d.status] || d.status)}</span>`]
    .concat(d.labels.filter(l => !l.name.startsWith('status/')).map(l => labelChip(l, 'badge')));
  const triageNotes = d.notes.filter(n => n.byTriage);
  const otherNotes = d.notes.filter(n => !n.byTriage);
  const noteHTML = n => `
    <article class="note ${n.byTriage ? 'by-triage' : ''}">
      <div class="note-head">
        ${n.byTriage ? icons.spark : ''}<strong>${esc(n.name || n.author)}</strong>
        <span>${n.byTriage ? 'triage agent' : esc(n.author)}</span>
        <span class="when">${esc(ago(n.createdAt))}</span>
      </div>
      <div class="note-body md">${markdown(n.body)}</div>
    </article>`;

  $('#detail').innerHTML = `
    <header class="d-head">
      <div class="d-head-main">
        <div class="d-kicker"><span class="mono">#${d.number}</span><span>·</span><span>opened ${esc(ago(d.createdAt))}</span>${d.channel ? `<span>·</span><span>via ${esc(d.channel)}</span>` : ''}</div>
        <h1 class="d-title">${esc(d.title)}</h1>
        <div class="d-badges">${badges.join('')}</div>
      </div>
      <div class="d-actions">
        <button class="btn btn-primary" id="triageBtn" type="button" ${running ? 'disabled' : ''}>
          ${icons.sparkles}${running ? 'Triaging…' : triaged ? 'Redo triage' : 'Triage with AI'}
        </button>
        <a class="link" href="${esc(d.url)}" target="_blank" rel="noopener noreferrer">Open in Gitea ${icons.external}</a>
      </div>
    </header>
    <div class="d-body">
      <div class="customer">
        <div class="avatar">${esc(initials(d.fromName, d.fromEmail))}</div>
        <div class="c-main">
          <div class="c-name">${esc(d.fromName || d.fromEmail || 'Unknown sender')}</div>
          ${d.fromName && d.fromEmail ? `<div class="c-sub">${esc(d.fromEmail)}</div>` : ''}
        </div>
        <div class="c-company"><strong>${esc(d.company || '—')}</strong><span class="mono">${esc(d.accountId || 'no account ID')}</span></div>
      </div>
      <section>
        <div class="section-label">Customer message</div>
        <div class="message md">${markdown(d.message)}</div>
      </section>
      <section>
        <div class="section-label">Triage note</div>
        ${triageNotes.length ? triageNotes.map(noteHTML).join('') : `<div class="no-notes">${d.status === 'new'
          ? 'Not triaged yet. The triage agent writes its note here.'
          : 'The triage agent has not written a note on this ticket. Redo the triage to get one.'}</div>`}
      </section>
      ${otherNotes.length ? `<section><div class="section-label">Other comments</div>${otherNotes.map(noteHTML).join('')}</section>` : ''}
    </div>`;
  $('#triageBtn').addEventListener('click', () => startTriage(triaged));
}

// ---------------------------------------------------------------- triage

async function startTriage(redo) {
  const number = state.selected;
  if (!number) return;
  try {
    state.job = await api(`/api/tickets/${number}/triage`, { method: 'POST', body: JSON.stringify({ redo }) });
  } catch (err) {
    toast(`Could not start the triage: ${err.message}`, true);
    return;
  }
  renderDetail();
  renderActivity();
  const t = state.tickets.find(x => x.number === number);
  if (t) { t.triaging = true; renderQueue(); }
  pollJob(state.job.id);
}

function pollJob(id) {
  stopJobPolling();
  const tick = async () => {
    let job;
    try {
      job = await api(`/api/jobs/${id}`);
    } catch {
      state.jobTimer = setTimeout(tick, 1500);
      return;
    }
    if (!state.job || state.job.id !== id) return;
    state.job = job;
    renderActivity();
    if (job.state === 'running') {
      state.jobTimer = setTimeout(tick, 900);
      return;
    }
    stopJobPolling();
    if (job.state === 'done') toast(`Ticket #${job.ticket} triaged`);
    else toast(`Triage of #${job.ticket} failed`, true);
    const refresh = [loadTickets({ quiet: true })];
    if (state.selected === job.ticket) refresh.push(selectTicket(job.ticket, { keepActivity: true }));
    await Promise.all(refresh);
  };
  state.jobTimer = setTimeout(tick, 600);
  state.clockTimer = setInterval(renderRunState, 1000);
}

function stopJobPolling() {
  clearTimeout(state.jobTimer);
  clearInterval(state.clockTimer);
  state.jobTimer = null;
  state.clockTimer = null;
}

function runStateHTML(job) {
  if (!job) return '<span class="run-state">Idle</span>';
  const started = new Date(job.startedAt).getTime();
  const ended = job.endedAt ? new Date(job.endedAt).getTime() : Date.now();
  if (job.state === 'running') return `<span class="run-state running">Running · ${clock(ended - started)}</span>`;
  if (job.state === 'done') return `<span class="run-state done">Done in ${clock(ended - started)}</span>`;
  return `<span class="run-state failed">Failed after ${clock(ended - started)}</span>`;
}

function renderRunState() {
  const el = $('#runState');
  if (el && state.job) el.innerHTML = runStateHTML(state.job);
}

function stepHTML(s, startedAt) {
  const offset = clock(new Date(s.at).getTime() - new Date(startedAt).getTime());
  const node = s.state === 'failed' ? icons.cross
    : s.kind === 'handoff' ? icons.arrow
    : s.kind === 'reply' ? icons.reply
    : s.kind === 'say' ? icons.spark
    : s.state === 'done' ? icons.check : icons.dot;
  const who = s.kind === 'reply' ? 'billing-agent' : s.kind === 'say' ? '' : 'support-triage';
  let detail = '';
  if (s.detail) {
    if (s.format === 'labels') {
      detail = `<div class="s-labels">${s.detail.split(',').map(name => `<span class="tag">${esc(name.replace(/^(type|priority|status)\//, ''))}</span>`).join('')}</div>`;
    } else if (s.format === 'markdown') {
      const body = `<div class="s-detail md${s.kind === 'reply' ? ' reply' : ''}">${markdown(s.detail)}</div>`;
      // The summary and billing-agent's answer show as they are; the note and
      // the request to billing-agent open on a click.
      detail = s.kind === 'say' || s.kind === 'reply' ? body
        : `<details class="s-more"><summary>${s.kind === 'handoff' ? 'Show the request' : 'Show the note'}</summary>${body}</details>`;
    } else {
      const cls = s.kind === 'reply' ? 's-detail text reply' : 's-detail text';
      detail = s.kind === 'handoff'
        ? `<details class="s-more"><summary>Show the request</summary><div class="${cls}">${esc(s.detail)}</div></details>`
        : `<div class="${cls}">${esc(s.detail)}</div>`;
    }
  }
  const title = s.kind === 'say' ? 'Summary' : s.title;
  return `
    <li class="step ${esc(s.state)} kind-${esc(s.kind)}">
      <span class="node">${node}</span>
      <div class="s-main">
        <div class="s-line">
          <span class="s-title">${esc(title)}</span>
          ${who ? `<span class="s-who">${who}</span>` : ''}
          <span class="s-time">+${offset}</span>
        </div>
        ${detail}
        ${s.state === 'failed' && s.result ? `<div class="s-error">${esc(s.result)}</div>` : ''}
      </div>
    </li>`;
}

function renderActivity() {
  const job = state.job && state.detail && state.job.ticket === state.detail.number ? state.job : null;
  const head = `
    <div class="panel-head">
      <h3>Agent activity</h3>
      <span id="runState">${runStateHTML(job)}</span>
    </div>`;
  if (!job) {
    $('#activity').innerHTML = head + `
      <div class="activity-body">
        <div class="idle">
          Triage this ticket to watch the agents work. Each step shows up here as it happens.
          <div class="flow">
            <div><i>1</i><span><b>support-triage</b> reads the ticket in Gitea</span></div>
            <div><i>2</i><span>asks <b>billing-agent</b> over A2A, for billing tickets</span></div>
            <div><i>3</i><span><b>billing-agent</b> checks the account in Lago</span></div>
            <div><i>4</i><span><b>support-triage</b> labels the ticket and writes its note</span></div>
          </div>
        </div>
      </div>`;
    return;
  }
  const steps = job.steps.map(s => stepHTML(s, job.startedAt)).join('');
  const waiting = job.state === 'running'
    ? `<li class="step running"><span class="node">${icons.dot}</span><div class="s-main"><div class="s-line"><span class="s-title">${job.steps.length ? 'Working…' : 'Starting…'}</span></div></div></li>`
    : '';
  $('#activity').innerHTML = head + `
    <div class="activity-body">
      <ol class="timeline">${steps}${waiting}</ol>
      ${job.state === 'failed' ? `<div class="run-error">${esc(job.error)}</div>` : ''}
    </div>`;
  const body = $('#activity');
  if (job.state === 'running') body.scrollTop = body.scrollHeight;
}

// ----------------------------------------------------------------- account

async function loadAccount() {
  const d = state.detail;
  const el = $('#account');
  const head = (link = '') => `<div class="panel-head"><h3>Account</h3>${link}</div>`;
  if (!d || !d.accountId) {
    el.innerHTML = head() + '<div class="account-body"><div class="muted-note">This ticket names no account.</div></div>';
    return;
  }
  if (state.accountFor !== d.accountId) {
    el.innerHTML = head() + '<div class="account-body"><div class="muted-note">Loading the account…</div></div>';
  }
  let a;
  try {
    a = await api(`/api/accounts/${encodeURIComponent(d.accountId)}`);
  } catch (err) {
    el.innerHTML = head() + `<div class="account-body"><div class="muted-note">${esc(err.message === 'account not found' ? `No account ${d.accountId} in Lago.` : err.message)}</div></div>`;
    return;
  }
  if (!state.detail || state.detail.accountId !== d.accountId) return;

  const before = state.accountFor === a.id && state.lastInvoices ? state.lastInvoices : null;
  state.account = a;
  state.accountFor = a.id;
  state.lastInvoices = Object.fromEntries(a.invoices.map(i => [i.number, `${i.status}/${i.paymentStatus}`]));

  const rows = a.invoices.map(i => {
    const voided = i.status === 'voided';
    const pill = voided ? '<span class="pill voided">Voided</span>'
      : i.paymentStatus === 'succeeded' ? '<span class="pill paid">Paid</span>'
      : i.paymentStatus === 'failed' ? '<span class="pill failed">Failed</span>'
      : '<span class="pill pending">Unpaid</span>';
    const changed = before && before[i.number] && before[i.number] !== `${i.status}/${i.paymentStatus}`;
    const type = i.type === 'one_off' ? 'one-off' : i.type;
    return `<tr class="${voided ? 'voided' : ''} ${changed ? 'changed' : ''}">
      <td><span class="mono">${esc(i.number)}</span><div class="acct-sub">${esc(type)} · ${esc(i.issuingDate)}</div></td>
      <td class="num">${esc(money(i.totalCents, i.currency))}</td>
      <td>${pill}</td></tr>`;
  }).join('');
  const link = a.lagoUrl ? `<a href="${esc(a.lagoUrl)}" target="_blank" rel="noopener noreferrer" class="link" style="font-size:12.5px">Open in Lago ${icons.external}</a>` : '';
  el.innerHTML = head(link) + `
    <div class="account-body">
      <div class="acct-top">
        <div>
          <div class="acct-name">${esc(a.name)}</div>
          <div class="acct-sub"><span class="mono">${esc(a.id)}</span> · ${esc(a.email)}</div>
        </div>
      </div>
      ${a.coupons.length ? `<div class="coupons">${a.coupons.map(c => `<span class="coupon">${esc(c)} applied</span>`).join('')}</div>` : ''}
      ${a.invoices.length ? `<table class="invoices"><thead><tr><th>Invoice</th><th class="num">Total</th><th>Status</th></tr></thead><tbody>${rows}</tbody></table>`
        : '<div class="muted-note">No invoices yet.</div>'}
    </div>`;
}

// ------------------------------------------------------------- new request

const examples = [
  {
    company: 'Kettle & Co', accountId: 'ACC-1077', name: 'Priya Raman', email: 'accounts@kettle.example.com',
    subject: 'Charged for premium support we never ordered',
    message: 'Hello,\n\nOur latest invoice includes 300 USD for premium support, but nobody on our side ordered it. Can you take it off our bill?\n\nThanks,\nPriya',
  },
  {
    company: 'Tidewater Labs', accountId: 'ACC-1019', name: 'Sam Okafor', email: 'dev-ops@tidewater.example.com',
    subject: 'Reverse geocoding in the batch API',
    message: 'We run the batch API every night. Could you add reverse geocoding (coordinates to addresses) to the batch endpoint? Today we call the single endpoint about 40,000 times instead.',
  },
  {
    company: 'Orbit Health', accountId: 'ACC-1103', name: 'Marco Bianchi', email: 'finance@orbithealth.example.com',
    subject: 'How do we rotate our API key?',
    message: 'Hi, one of our developers has left the company. How do we rotate our API key without downtime?',
  },
];
let exampleIndex = 0;

function openNewRequest() {
  $('#newRequestForm').reset();
  $('#newRequestDialog').showModal();
  $('#newRequestForm [name=company]').focus();
}

function fillExample() {
  const ex = examples[exampleIndex++ % examples.length];
  const form = $('#newRequestForm');
  Object.entries(ex).forEach(([key, value]) => { form.elements[key].value = value; });
}

async function submitNewRequest(event) {
  event.preventDefault();
  const form = event.target;
  const data = Object.fromEntries(new FormData(form).entries());
  const btn = $('#submitRequestBtn');
  btn.disabled = true;
  try {
    const t = await api('/api/tickets', { method: 'POST', body: JSON.stringify(data) });
    $('#newRequestDialog').close();
    toast(`Ticket #${t.number} filed`);
    state.filter = 'all';
    await loadTickets({ quiet: true });
    await selectTicket(t.number);
  } catch (err) {
    toast(err.message, true);
  } finally {
    btn.disabled = false;
  }
}

// -------------------------------------------------------------------- wire

function init() {
  $('#ticketList').addEventListener('click', e => {
    const li = e.target.closest('.ticket');
    if (li) selectTicket(Number(li.dataset.number));
  });
  $('#filters').addEventListener('click', e => {
    const b = e.target.closest('button');
    if (b) { state.filter = b.dataset.filter; renderQueue(); }
  });
  $('#refreshBtn').addEventListener('click', async () => {
    await loadTickets();
    if (state.selected) selectTicket(state.selected, { keepActivity: true });
  });
  $('#newRequestBtn').addEventListener('click', openNewRequest);
  $('#exampleBtn').addEventListener('click', fillExample);
  $('#newRequestForm').addEventListener('submit', submitNewRequest);
  $$('[data-close]').forEach(b => b.addEventListener('click', () => $('#newRequestDialog').close()));

  renderActivity();
  loadAccount();
  loadOverview();
  setInterval(loadOverview, 20000);

  loadTickets().then(() => {
    const m = location.hash.match(/^#ticket-(\d+)$/);
    const first = state.tickets[0];
    if (m) selectTicket(Number(m[1]));
    else if (first) selectTicket(first.number);
  });
  setInterval(() => { if (!document.hidden) loadTickets({ quiet: true }); }, 15000);
}

init();
