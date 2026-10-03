// The header's Upload menu (templates/header.html, docs/SPEC.md FR-3.4), on every page: files
// chosen here — or dropped on the map, which sends them here as an `hmt:upload-files` event —
// upload one at a time with their progress, then show as "Processing…" until the server has
// finished them, a .zip or Takeout export as one row counting its files. A finished import
// leaves the menu, whatever became of it: what it came to is the header's Sync item's, the
// /sync page, whose red dot this script also keeps — a failure the account hasn't seen there.
//
// The server's GET /v1/uploads/active is the list of what's still processing — a phone sync's
// too — and the count of unseen failures, read every POLL_MS while anything is in flight and
// every IDLE_MS otherwise. Whenever an import finishes, or a file's upload lands, the menu
// fires `hmt:imports-changed` on window, which the map listens to (MapView.tsx) to redraw its
// list and tracks.
//
// The menu's words are the server's catalog, rendered into <script class="upload-menu__strings">.
(function () {
  'use strict';

  var POLL_MS = 2000;
  var IDLE_MS = 20000;
  // An individually chosen batch's cap; a .zip bypasses it (IMPLEMENTATION.md §4.0.1).
  var MAX_PLAIN_FILES = 20;
  var ACCEPT = /\.(gpx|fit|tcx|zip)$/i;

  var menu = document.querySelector('[data-upload-menu]');
  if (!menu) return;
  var strings = JSON.parse(menu.querySelector('.upload-menu__strings').textContent);
  var input = menu.querySelector('.upload-menu__input');
  var list = menu.querySelector('.upload-menu__list');
  var idle = menu.querySelector('.upload-menu__idle');
  var notes = menu.querySelector('.upload-menu__notes');
  // The header's Sync item, where finished imports are: its dot is a failure not yet seen there.
  var syncLink = document.querySelector('[data-sync-link]');
  var syncAlert = syncLink && syncLink.querySelector('.sync-link__alert');
  var badge = menu.querySelector('.upload-menu__badge');
  var alertDot = menu.querySelector('.upload-menu__alert');

  var transfers = []; // { id, name, progress, sending } — files still to send, or being sent
  var active = []; // GET /v1/uploads/active's imports
  var unseenFailures = 0;
  var noteList = []; // { id, text, error } — this page's own messages, dismissed one by one
  var uid = 0;
  var timer = null;
  var polling = false;

  function fill(template, values) {
    return template.replace(/\{(\w+)\}/g, function (m, key) {
      return Object.prototype.hasOwnProperty.call(values, key) ? String(values[key]) : m;
    });
  }

  function el(tag, className, text) {
    var e = document.createElement(tag);
    if (className) e.className = className;
    if (text !== undefined) e.textContent = text;
    return e;
  }

  function dismissButton(onClick) {
    var b = el('button', 'upload-menu__dismiss', '×');
    b.type = 'button';
    b.setAttribute('aria-label', strings.dismiss);
    b.title = strings.dismiss;
    b.addEventListener('click', onClick);
    return b;
  }

  function row(title, status) {
    var li = el('li', 'upload-menu__row');
    li.appendChild(el('span', 'upload-menu__title', title));
    li.appendChild(el('span', 'upload-menu__status', status));
    return li;
  }

  function render() {
    list.replaceChildren();
    transfers.forEach(function (t) {
      list.appendChild(row(t.name, t.sending ? fill(strings.uploading, { percent: Math.round(t.progress * 100) }) : strings.queued));
    });
    active.forEach(function (a) {
      var status = a.total > 1 ? fill(strings.processing_count, { done: a.done, total: a.total }) : strings.processing;
      list.appendChild(row(a.title, status));
    });
    var busy = transfers.length + active.length;
    idle.hidden = busy > 0;
    badge.hidden = busy === 0;
    badge.textContent = String(busy);

    notes.replaceChildren();
    noteList.forEach(function (n) {
      var li = el('li', 'upload-menu__note' + (n.error ? ' upload-menu__note--error' : ''));
      li.appendChild(el('span', '', n.text));
      li.appendChild(dismissButton(function () {
        noteList = noteList.filter(function (x) { return x.id !== n.id; });
        render();
      }));
      notes.appendChild(li);
    });

    alertDot.hidden = !noteList.some(function (n) { return n.error; });
    if (syncAlert) {
      syncAlert.hidden = unseenFailures === 0;
      if (unseenFailures > 0) syncLink.title = fill(strings.failed_unseen, { n: unseenFailures });
      else syncLink.removeAttribute('title');
    }
  }

  function note(text, error) {
    uid += 1;
    noteList.push({ id: uid, text: text, error: !!error });
    render();
  }

  function changed() {
    window.dispatchEvent(new CustomEvent('hmt:imports-changed'));
  }

  // ---- What's still processing ----------------------------------------------------------

  function keyOf(a) {
    return a.title + '|' + a.submitted_at;
  }

  function refresh() {
    return fetch('/v1/uploads/active', { credentials: 'same-origin', headers: { Accept: 'application/json' } })
      .then(function (res) { return res.ok ? res.json() : null; })
      .then(function (body) {
        if (!body) return;
        var before = {};
        active.forEach(function (a) { before[keyOf(a)] = a.done; });
        var now = {};
        body.imports.forEach(function (a) { now[keyOf(a)] = a.done; });
        var finishedSome = Object.keys(before).some(function (k) {
          return !(k in now) || now[k] > before[k];
        });
        active = body.imports;
        unseenFailures = body.unseen_failures;
        render();
        if (finishedSome) changed();
      })
      .catch(function () {
        // The next tick tries again.
      });
  }

  function schedule() {
    clearTimeout(timer);
    var busy = transfers.length > 0 || active.length > 0;
    timer = setTimeout(tick, busy ? POLL_MS : IDLE_MS);
  }

  function tick() {
    if (document.hidden) return schedule();
    if (polling) return;
    polling = true;
    refresh().then(function () {
      polling = false;
      schedule();
    });
  }

  // ---- Uploading --------------------------------------------------------------------------

  function errorMessage(xhr) {
    var text = (xhr.responseText || '').trim();
    try {
      var body = JSON.parse(text);
      if (body && body.error && body.error.message) return body.error.message;
    } catch (e) {
      // Plain text.
    }
    return text || fill(strings.request_failed, { status: xhr.status });
  }

  function send(file, transfer) {
    return new Promise(function (resolve) {
      var xhr = new XMLHttpRequest();
      xhr.open('POST', '/v1/activities/upload');
      xhr.withCredentials = true;
      xhr.upload.onprogress = function (event) {
        if (!event.lengthComputable) return;
        transfer.progress = event.loaded / event.total;
        render();
      };
      xhr.onload = function () {
        if (xhr.status < 200 || xhr.status >= 300) {
          note(fill(strings.failed_transfer, { filename: file.name, error: errorMessage(xhr) }), true);
          return resolve();
        }
        var body = {};
        try {
          body = JSON.parse(xhr.responseText);
        } catch (e) {
          // An upload the server took; nothing more to say about it.
        }
        if (body.status === 'already_processed') note(fill(strings.already, { filename: file.name }));
        if (body.status === 'zip_processed') {
          var files = body.files || [];
          var count = function (status) {
            return files.filter(function (f) { return f.status === status; }).length;
          };
          // Neither kind becomes a job, so neither shows on the Sync page: this note is the only
          // place they're told — and for an archive with nothing new, the only sign it arrived.
          var already = count('already_processed');
          var skipped = count('skipped');
          if (already > 0) note(fill(strings.already_in, { filename: file.name, n: already }));
          if (skipped > 0) note(fill(strings.skipped, { filename: file.name, n: skipped }));
          if (body.truncated) note(fill(strings.truncated, { filename: file.name }));
        }
        resolve();
      };
      xhr.onerror = function () {
        note(fill(strings.failed_transfer, { filename: file.name, error: strings.network_error }), true);
        resolve();
      };
      var form = new FormData();
      form.append('file', file);
      xhr.send(form);
    });
  }

  function enqueue(fileList) {
    var files = Array.prototype.filter.call(fileList, function (f) { return ACCEPT.test(f.name); });
    var plain = files.filter(function (f) { return !/\.zip$/i.test(f.name); });
    if (plain.length > MAX_PLAIN_FILES) {
      note(fill(strings.too_many, { n: plain.length }), true);
      files = files.filter(function (f) { return /\.zip$/i.test(f.name); });
    }
    if (files.length === 0) return;
    menu.open = true;
    var queued = files.map(function (f) {
      uid += 1;
      return { id: uid, name: f.name, progress: 0, file: f };
    });
    transfers = transfers.concat(queued);
    render();
    schedule();
    // One at a time: per-file progress stays one XHR to follow, and a batch of a few files
    // against a personal server gains nothing from a pool.
    queued.reduce(function (chain, t) {
      return chain.then(function () {
        t.sending = true;
        render();
        return send(t.file, t).then(function () {
          transfers = transfers.filter(function (x) { return x.id !== t.id; });
          return refresh().then(changed);
        });
      });
    }, Promise.resolve()).then(schedule);
  }

  input.addEventListener('change', function () {
    enqueue(input.files);
    input.value = '';
  });

  window.addEventListener('hmt:upload-files', function (event) {
    enqueue(event.detail);
  });

  // The Google Maps Timeline import is a window on the map (TimelineImportWindow.tsx): on the
  // map, the link opens it there — the map cancels this event to say it has — and from any
  // other page it goes to /?import=timeline. The window sends its activities itself, and says
  // when a batch has gone so the list shows it now rather than at the next idle tick.
  var timelineLink = menu.querySelector('[data-timeline-import]');
  if (timelineLink) {
    timelineLink.addEventListener('click', function (event) {
      var opened = !window.dispatchEvent(new CustomEvent('hmt:open-timeline-import', { cancelable: true }));
      if (opened) {
        event.preventDefault();
        menu.open = false;
      }
    });
  }
  window.addEventListener('hmt:imports-sent', function () {
    refresh().then(schedule);
  });

  // Opening the menu reads the latest at once rather than waiting for the next tick.
  menu.addEventListener('toggle', function () {
    if (menu.open) refresh();
  });

  // A <details> stays open until its summary is clicked again; this one also closes on a click
  // elsewhere or Escape, like a menu.
  document.addEventListener('click', function (event) {
    if (menu.open && !menu.contains(event.target)) menu.open = false;
  });
  document.addEventListener('keydown', function (event) {
    if (event.key === 'Escape' && menu.open) {
      menu.open = false;
      menu.querySelector('summary').focus();
    }
  });
  document.addEventListener('visibilitychange', function () {
    if (!document.hidden) tick();
  });

  render();
  tick();
})();
