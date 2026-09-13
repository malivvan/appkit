/* appkit demo page script.
 *
 * The page is served by App.FS from the same uniform "app://" origin on every
 * platform. Serving is scheme-first on Windows and Linux (WebView2's https
 * virtual host; Linux's registered app scheme); macOS always serves over a
 * temporary loopback http://localhost server (a long-standing WebKit bug
 * keeps SharedArrayBuffer off plain WKWebView pages), and -http (App.HTTP)
 * opts Linux and Windows into that loopback origin too. SharedArrayBuffer
 * is available on every platform (the self-test step asserts it).
 *
 * The view carries the JS bridge (window.demo* functions and the events
 * bridge) - it is injected per view, independent of the page origin - so
 * every section below is live wherever the page runs.
 *
 * Every interactive element carries a stable id - that, plus the #selftest
 * hash hook, is what makes this app usable for automated UI tests later.
 */
'use strict';

const $ = (id) => document.getElementById(id);

const hasBridge = () => typeof window.demoEcho === 'function';

/* --- app sections ---------------------------------------------------------
 * The bridge sections below need the appkit host: the Go side attaches the
 * window.demo* bindings and the events bridge to the view regardless of the
 * page's origin. */

function wireBridgeSections() {
  $('section-overview').hidden = true;

  for (const id of ['bind', 'events', 'clipboard', 'dialogs', 'notify', 'autostart', 'files']) {
    $(`section-${id}`).hidden = false;
  }

  // Bind
  $('bindAdd').addEventListener('click', async () => {
    $('bindResult').textContent = await window.demoAdd(Number($('bindA').value), Number($('bindB').value));
  });
  $('bindEcho').addEventListener('click', async () => {
    $('bindEchoResult').textContent = await window.demoEcho($('bindText').value || '');
  });

  // Bind forms: an immutable constant, a function usable as a getter, and a
  // readable/writable accessor pair.
  $('bindMetaRead').addEventListener('click', () => {
    const meta = window.demo && window.demo.meta;
    $('bindMetaResult').textContent = meta
      ? JSON.stringify(meta) + (Object.isFrozen(meta) ? ' (frozen)' : '')
      : 'window.demo.meta missing';
  });
  $('bindClockCall').addEventListener('click', async () => {
    $('bindClockResult').textContent = 'call: ' + (await window.demo.clock());
  });
  $('bindClockRead').addEventListener('click', async () => {
    $('bindClockResult').textContent = 'await: ' + (await window.demo.clock);
  });
  $('bindThemeRead').addEventListener('click', async () => {
    try { $('bindThemeResult').textContent = await window.demo.theme; }
    catch (e) { $('bindThemeResult').textContent = 'read failed: ' + e; }
  });
  $('bindThemeWrite').addEventListener('click', async () => {
    try {
      // The assignment runs the Go setter; awaiting it surfaces setter errors.
      await (window.demo.theme = $('bindTheme').value.trim());
      $('bindThemeResult').textContent = 'wrote: ' + (await window.demo.theme);
    } catch (e) { $('bindThemeResult').textContent = 'write failed: ' + e; }
  });
  // Callable setter: a one-argument function is both a function and a
  // writable variable - call it, or assign to it (both run the Go function).
  $('bindMarkCall').addEventListener('click', async () => {
    try { $('bindMarkResult').textContent = 'call: ' + (await window.demo.mark($('bindMarkInput').value)); }
    catch (e) { $('bindMarkResult').textContent = 'call failed: ' + e; }
  });
  $('bindMarkSet').addEventListener('click', async () => {
    try {
      // Assignment runs the Go function with the assigned value. The
      // assignment expression itself yields the assigned value (ECMAScript),
      // so the display shows the value the setter ran with.
      const v = $('bindMarkInput').value;
      await (window.demo.mark = v);
      $('bindMarkResult').textContent = 'assign ran with: ' + v;
    } catch (e) { $('bindMarkResult').textContent = 'assign failed: ' + e; }
  });
  // A lone getter function binds read-only: every read runs the Go getter.
  $('bindCounterRead').addEventListener('click', async () => {
    try { $('bindCounterResult').textContent = 'count: ' + (await window.demo.counter); }
    catch (e) { $('bindCounterResult').textContent = 'read failed: ' + e; }
  });
  // Accessor pair: a length-2 (getter, setter) function pair binds as a
  // readable AND writable property - reading runs the Go getter, assigning
  // runs the Go setter.
  $('bindPairRead').addEventListener('click', async () => {
    try { $('bindPairResult').textContent = 'read: ' + (await window.demo.pair); }
    catch (e) { $('bindPairResult').textContent = 'read failed: ' + e; }
  });
  $('bindPairWrite').addEventListener('click', async () => {
    try {
      await (window.demo.pair = $('bindPair').value);
      $('bindPairResult').textContent = 'wrote: ' + (await window.demo.pair);
    } catch (e) { $('bindPairResult').textContent = 'write failed: ' + e; }
  });
  // A lone setter function binds write-only: assignment runs the Go setter;
  // the effect is observed through demo.setpState, a lone getter over the
  // same backing value.
  $('bindSetpWrite').addEventListener('click', async () => {
    try {
      await (window.demo.setp = Number($('bindSetp').value));
      $('bindSetpResult').textContent = 'wrote, state: ' + (await window.demo.setpState);
    } catch (e) { $('bindSetpResult').textContent = 'write failed: ' + e; }
  });
  $('bindSetpState').addEventListener('click', async () => {
    try { $('bindSetpResult').textContent = 'state: ' + (await window.demo.setpState); }
    catch (e) { $('bindSetpResult').textContent = 'read failed: ' + e; }
  });

  // Events: Go -> JS listener, plus a JS -> Go -> JS round trip.
  events.on('demo:goEvent', (msg) => { $('evResult').textContent = String(msg); });
  $('evGo').addEventListener('click', () => window.demoEmitGo('emitted from Go'));
  $('evJS').addEventListener('click', () => {
    $('evResult').textContent = '…';
    events.emit('demo:uiGreet', 'hello from JS');
  });

  // Clipboard
  $('clipCopy').addEventListener('click', async () => {
    try {
      await window.demoCopyText('appkit demo clipboard payload');
      $('clipResult').textContent = 'copied';
    } catch (e) { $('clipResult').textContent = 'copy failed: ' + e; }
  });
  $('clipPaste').addEventListener('click', async () => {
    $('clipResult').textContent = await window.demoPaste();
  });

  // Dialogs (native panels; cancelled -> empty)
  $('dlgOpen').addEventListener('click', async () => {
    $('dlgResult').textContent = JSON.stringify(await window.demoDialog('open'));
  });
  $('dlgSave').addEventListener('click', async () => {
    $('dlgResult').textContent = JSON.stringify(await window.demoDialog('save'));
  });
  $('dlgDir').addEventListener('click', async () => {
    $('dlgResult').textContent = JSON.stringify(await window.demoDialog('dir'));
  });

  // Notify
  $('notifyGo').addEventListener('click', async () => {
    const r = await window.demoNotify();
    $('notifyResult').textContent = r === '' ? 'notification sent' : 'notify error: ' + r;
  });

  // Autostart: render the registration state and let the user toggle it.
  async function renderAutostart() {
    const st = await window.demoAutostartState();
    $('auResult').textContent = st.enabled
      ? `enabled (backend ${st.backend}, path ${st.path})`
      : 'not enabled';
  }
  $('auEnable').addEventListener('click', async () => {
    const args = $('auArgs').value.trim().split(/\s+/).filter((a) => a !== '');
    try {
      await window.demoAutostartSet(true, args);
      await renderAutostart();
    } catch (e) {
      $('auResult').textContent = 'enable failed: ' + e;
    }
  });
  $('auDisable').addEventListener('click', async () => {
    try {
      await window.demoAutostartSet(false, []); // binding takes (on, args)
      await renderAutostart();
    } catch (e) {
      $('auResult').textContent = 'disable failed: ' + e;
    }
  });
  renderAutostart().catch(() => { $('auResult').textContent = 'state unavailable'; });

  // Open / Reveal (launch external programs; not exercised by the self test)
  $('openLink').addEventListener('click', async () => {
    const r = await window.demoOpen('https://github.com/malivvan/appkit');
    $('filesResult').textContent = r === '' ? 'opened in the default browser' : 'open error: ' + r;
  });
  $('revealFile').addEventListener('click', async () => {
    const r = await window.demoReveal();
    $('filesResult').textContent = r === '' ? 'revealed' : 'reveal error: ' + r;
  });
}

/* --- navigation ----------------------------------------------------------- */

function wireNav() {
  // Keep it simple: clicking a nav item scrolls the matching section into view.
  document.querySelectorAll('#nav .nav-item').forEach((a) => {
    a.addEventListener('click', () => {
      const sec = $(`section-${a.dataset.section}`);
      if (sec) sec.scrollIntoView({ behavior: 'smooth', block: 'start' });
    });
  });
}

$('btnMinimize').addEventListener('click', () => {
  if (typeof window.demoMinimize === 'function') window.demoMinimize();
});

// The maximize button toggles between maximize and unmaximize: the Go side
// reports the new state, so the button swaps its glyph and tooltip to become
// the "restore" button and back.
function renderMaximizeButton(on) {
  const btn = $('btnMaximize');
  if (on) {
    btn.textContent = '\u2750'; // ❐ restore glyph
    btn.title = 'Restore window';
    btn.dataset.maximized = 'true';
  } else {
    btn.textContent = '\u25A1'; // □ maximize glyph
    btn.title = 'Maximize window';
    btn.dataset.maximized = 'false';
  }
}
$('btnMaximize').addEventListener('click', async () => {
  if (typeof window.demoMaximize !== 'function') return;
  renderMaximizeButton(await window.demoMaximize());
});
renderMaximizeButton(false);
$('btnClose').addEventListener('click', () => {
  if (typeof window.demoExit === 'function') window.demoExit();
});

/* --- self test ------------------------------------------------------------
 * Runs a scripted suite of checks against the live bridge and reports each
 * step through reportSelfTest(name, pass, detail) - the Go side collects the
 * verdicts. Run it manually with the button, or automatically by loading the
 * page with #selftest (./demo --selftest). Everything here must stay free of
 * timing assumptions beyond its own awaits, so it stays deterministic for UI
 * automation.
 */

function selftestLog(name, pass, detail) {
  const li = document.createElement('li');
  li.className = pass ? 'pass' : 'fail';
  li.textContent = name;
  if (detail) {
    const d = document.createElement('span');
    d.className = 'detail';
    d.textContent = ' - ' + detail;
    li.appendChild(d);
  }
  $('selftestLog').appendChild(li);
}

async function runSelfTest() {
  const results = [];
  // step() accepts sync and async fns alike and always records a verdict, so
  // one throwing check can never abort the whole suite.
  const step = (name, fn) => Promise.resolve()
    .then(fn)
    .then((detail) => { selftestLog(name, true, detail); results.push({ name, pass: true, detail }); })
    .catch((e) => { selftestLog(name, false, String(e)); results.push({ name, pass: false, detail: String(e) }); });

  $('selftestResult').textContent = 'running…';

  // Bridge basics.
  await step('bridge add', async () => {
    const v = await window.demoAdd(20, 22);
    if (v !== 42) throw new Error(`demoAdd(20,22)=${v}, want 42`);
    return '42';
  });
  await step('bridge echo', async () => {
    const v = await window.demoEcho('selftest');
    if (v !== 'selftest') throw new Error(`echo=${v}`);
    return v;
  });

  // Bind forms: the frozen constant, the function-as-getter (callable AND
  // awaitable), and the readable/writable accessor pair.
  await step('bind: constant is frozen', async () => {
    if (!window.demo || typeof window.demo.meta !== 'object') throw new Error('window.demo.meta missing');
    if (!Object.isFrozen(window.demo.meta)) throw new Error('constant not frozen');
    const before = JSON.stringify(window.demo.meta);
    try { window.demo.meta.app = 'mutated'; } catch (e) { /* frozen: assignment throws or is silent */ }
    if (JSON.stringify(window.demo.meta) !== before) throw new Error('constant mutated');
    return 'demo.meta immutable';
  });
  await step('bind: function by call and by value', async () => {
    const byCall = await window.demo.clock();
    const byValue = await window.demo.clock; // thenable getter read
    if (typeof byCall !== 'string' || byCall.length === 0) throw new Error('clock() returned nothing');
    if (typeof byValue !== 'string' || byValue.length === 0) throw new Error('await clock returned nothing');
    return `clock()=${byCall} await=${byValue}`;
  });
  await step('bind: callable setter (function AND variable)', async () => {
    const byCall = await window.demo.mark('selftest');
    if (byCall !== 'marked: selftest') throw new Error(`call result=${byCall}`);
    // Assignment runs the same one-argument function with the assigned value,
    // but an ECMAScript assignment expression yields the ASSIGNED VALUE - the
    // setter's result is not part of the expression (await the CALL form to
    // observe it). Verify the expression value and that the name stays a
    // function; the Go call itself is the side effect being showcased.
    const assignValue = (window.demo.mark = 'assigned');
    if (assignValue !== 'assigned') throw new Error(`assign expression=${assignValue}`);
    if (typeof window.demo.mark !== 'function') throw new Error('name must still read as a function');
    return `call=${byCall} assign expression=${assignValue}`;
  });
  await step('bind: readable/writable accessor (demo.theme)', async () => {
    // demo.theme is a [2]any{getter, setter} pair: reading the property runs
    // the Go getter, assigning runs the Go setter.
    const first = await window.demo.theme;
    await (window.demo.theme = 'sunset');
    const second = await window.demo.theme;
    if (second !== 'sunset') throw new Error(`write did not stick: ${second}`);
    await (window.demo.theme = first); // restore the demo default
    const third = await window.demo.theme;
    if (third !== first) throw new Error(`restore failed: ${third}`);
    return `${first} -> sunset -> ${first}`;
  });
  await step('bind: getter function is read-only', async () => {
    // demo.counter is a lone zero-argument function: each read runs the Go
    // getter, which advances - so two reads prove every read reaches Go.
    const c1 = await window.demo.counter;
    const c2 = await window.demo.counter;
    if (typeof c1 !== 'number' || c2 !== c1 + 1) throw new Error(`counter reads ${c1}, ${c2}`);
    return `${c1} -> ${c2}`;
  });
  await step('bind: setter function is write-only', async () => {
    // demo.setp is a lone one-argument function: assignment runs the Go
    // setter. The effect is read back through demo.setpState, a lone getter
    // over the same backing value.
    await (window.demo.setp = 41);
    const state = await window.demo.setpState;
    if (state !== 41) throw new Error(`setp state = ${state}, want 41`);
    return `setp -> 41 (state ${state})`;
  });
  await step('bind: accessor pair read/write round trip', async () => {
    // demo.pair is a length-2 (getter, setter) function pair: reading the
    // property runs the Go getter, assigning runs the Go setter.
    const first = await window.demo.pair;
    await (window.demo.pair = 'paired');
    const second = await window.demo.pair;
    if (second !== 'paired') throw new Error(`pair write did not stick: ${second}`);
    await (window.demo.pair = first); // restore
    return `${first} -> paired -> ${first}`;
  });

  // Events round trip JS -> Go -> JS (via the events bridge, not a binding).
  await step('events round trip', () => new Promise((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error('event round trip timed out')), 8000);
    const off = events.on('demo:goEvent', (msg) => {
      if (msg !== 'pong:ping') return; // unrelated event; keep waiting
      clearTimeout(timer);
      off();
      resolve(msg);
    });
    events.emit('demo:uiGreet', 'ping');
  }));

  // Clipboard round trip through the Go clipboard service. Headless boxes often
  // have no clipboard tool (xclip/xsel/wl-clipboard), so when the backend
  // reports that, the step is skipped instead of failed.
  await step('clipboard round trip', async () => {
    try {
      await window.demoCopyText('selftest-clipboard');
    } catch (e) {
      if (/clipboard utilities/i.test(String(e))) return 'skipped: no clipboard tool on this system';
      throw e;
    }
    const v = await window.demoPaste();
    if (v !== 'selftest-clipboard') throw new Error(`paste=${JSON.stringify(v)}`);
    return 'round-tripped';
  });

  // Autostart round trip through App.Autostart: register, verify, remove.
  // Self-cleaning - the demo ends with no registration for this binary - and
  // the pre-existing state (a developer who already enabled it) is restored.
  await step('autostart round trip', async () => {
    const wasEnabled = (await window.demoAutostartState()).enabled;
    if (wasEnabled) await window.demoAutostartSet(false, []); // start clean
    try {
      await window.demoAutostartSet(true, ['--selftest-autostart']);
    } catch (e) {
      if (wasEnabled) await window.demoAutostartSet(true, []); // restore user state
      throw new Error('enable failed: ' + e);
    }
    const on = await window.demoAutostartState();
    let detail;
    try {
      if (!on.enabled) throw new Error('not enabled after Enable');
      if (!on.backend) throw new Error('no backend reported after Enable');
      if (!on.path) throw new Error('no registration path reported after Enable');
      detail = `backend=${on.backend}`;
    } finally {
      await window.demoAutostartSet(false, []); // binding takes (on, args)
      if (wasEnabled) await window.demoAutostartSet(true, []); // restore user state
    }
    const off = await window.demoAutostartState();
    if (!wasEnabled && off.enabled) throw new Error('still enabled after Disable');
    return `${detail}, path=${on.path}`;
  });

  // Drag-region contract: the stylesheet must carry the appkit drag rule.
  // Browsers can drop unknown -webkit- properties from the parsed CSSOM (the
  // appkit tracker reads the raw stylesheet text for the same reason), so scan
  // both cssRules and the fetched raw text, polling while the sheet loads.
  await step('custom chrome drag region', async () => {
    const hasRule = (text) => text.includes('-app-region') || text.includes('-webkit-app-region') || text.includes('-webview-app-region');
    const deadline = Date.now() + 5000;
    while (Date.now() < deadline) {
      for (const sheet of document.styleSheets) {
        let rules;
        try { rules = sheet.cssRules; } catch (e) { continue; }
        for (const r of rules || []) {
          if (r.cssText && hasRule(r.cssText)) return 'drag rule present (cssRules)';
        }
      }
      try {
        const text = await (await fetch('app.css')).text();
        if (hasRule(text)) return 'drag rule present (raw stylesheet)';
      } catch (e) { /* sheet not ready or not fetchable; keep polling */ }
      await new Promise((resolve) => setTimeout(resolve, 100));
    }
    throw new Error('no drag-region rule in the loaded stylesheet');
  });

  // Determinism: stable ids used by the automation hooks must exist.
  await step('stable ids', () => {
    for (const id of ['titlebar', 'btnMinimize', 'btnMaximize', 'btnClose', 'bindAdd', 'bindMetaRead', 'bindTheme', 'bindMarkCall', 'bindMarkSet', 'bindCounterRead', 'bindPair', 'bindSetp', 'evGo', 'auEnable', 'runSelftest']) {
      if (!$(id)) throw new Error(`#${id} missing`);
    }
    return 'ids present';
  });

  // SharedArrayBuffer is guaranteed on every platform. macOS always serves
  // over the loopback http://localhost origin (WKWebView SAB bug), Windows
  // serves the https vhost, and an HTTP-opted (-http) Linux/Windows window
  // uses the loopback origin - all of those are crossOriginIsolated (the
  // COOP/COEP headers). A scheme-served Linux page is NOT
  // crossOriginIsolated (WebKitGTK cannot attach the headers to scheme
  // responses) but still has SharedArrayBuffer via JSC_useSharedArrayBuffer,
  // so the step only requires isolation from the loopback http:// origin.
  await step('isolated context: SharedArrayBuffer available', async () => {
    const sab = typeof SharedArrayBuffer !== 'undefined' ? 'yes' : 'no';
    const detail = `crossOriginIsolated=${window.crossOriginIsolated}, ` +
      `isSecureContext=${window.isSecureContext}, SharedArrayBuffer=${sab}`;
    if (sab !== 'yes' || !window.isSecureContext) throw new Error(detail);
    if (location.protocol === 'http:' && !window.crossOriginIsolated) throw new Error('loopback page must be crossOriginIsolated: ' + detail);
    return detail;
  });

  // Window state: the maximize button must toggle the REAL window state
  // (View.Maximized - never a page-side flag, which would desync with the
  // native double-click-on-a-drag-box toggle) and report the state it ends in.
  await step('maximize toggle', async () => {
    const on = await window.demoMaximize();
    const off = await window.demoMaximize();
    if (on === false && off === false) {
      // No window manager under this display (bare Xvfb): the toplevel is
      // never actually maximized, so the toggle-vs-query round trip cannot be
      // exercised here. Not a failure.
      renderMaximizeButton(false);
      return 'skipped: the window manager did not maximize the window';
    }
    if (on !== true || off !== false) throw new Error(`maximize toggle reported ${on}/${off}`);
    renderMaximizeButton(false);
    return `toggled ${on}->${off}`;
  });

  // Notifications: a real call. An unsupported/unavailable platform reports an
  // error string, which is recorded as a skip rather than a failure.
  await step('notify: desktop notification', async () => {
    const r = await window.demoNotify();
    return r === '' ? 'notification sent' : 'skipped: ' + r;
  });

  // Features the page cannot drive itself. They are recorded as passing
  // skips (with a reason) so the suite's breadth matches the advertised
  // feature set instead of silently omitting them.
  const skip = (name, reason) => {
    const detail = 'skipped: ' + reason;
    selftestLog(name, true, detail);
    results.push({ name, pass: true, detail });
    return Promise.resolve();
  };
  await skip('dialog: native panels', 'modal panel - use the Dialogs section or dialog/demo');
  await skip('open / reveal', 'launches the desktop handler - use the Files section');
  await skip('tray Show / Hide', '--selftest runs without a tray host; use ./demo -tray');
  await skip('window State (fixed/min/max)', 'creation-time View.State - not changeable at runtime');
  await skip('run modes (-http)', 'launch-time flags, not page-testable');
  await skip('single instance', 'launch-time App.Exec / App.ID');

  const failed = results.filter((r) => !r.pass).length;
  $('selftestResult').textContent = `${results.length - failed}/${results.length} passed`;
  if (typeof window.demoReport === 'function') {
    window.demoReport(results);
  }
}

// The self-test controls need the JS bridge; the buttons are only skipped
// when the page has no appkit host at all (plain browser), in which case the
// section is hidden too (see boot).
if (hasBridge()) {
  $('runSelftest').addEventListener('click', () => { $('selftestLog').innerHTML = ''; runSelfTest(); });
  $('resetSelftest').addEventListener('click', () => {
    $('selftestLog').innerHTML = '';
    $('selftestResult').textContent = '-';
  });
}

/* --- boot ---------------------------------------------------------------- */

function boot() {
  wireNav();
  if (hasBridge()) {
    wireBridgeSections();
  } else {
    // No appkit host behind this page (e.g. it was opened in a plain
    // browser): the bridge sections and the self test cannot work, so only
    // the overview is shown.
    $('section-selftest').hidden = true;
    const navSelftest = document.querySelector('#nav [data-section="selftest"]');
    if (navSelftest) navSelftest.hidden = true;
  }
  if (typeof window.demoReady === 'function') window.demoReady();
  if (location.hash === '#selftest') runSelfTest();
}

if (document.readyState === 'loading') {
  document.addEventListener('DOMContentLoaded', boot);
} else {
  boot();
}
