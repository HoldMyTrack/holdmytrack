// The server-rendered pages' searchable picker: turns a <select data-search-picker> into the
// map app's SearchPicker (apps/web/src/ui/SearchPicker.tsx) — a trigger button over a popover
// with a search field and a list — drawn by the same search-picker.css. The same ranking, keys
// and markup, less what these fields don't need: no "Add" row (a closed list), no leading slot.
// Settings' Country and Timezone use it.
//
// The <select> stays in the form, hidden, and holds the value: picking sets it and fires its
// `change`, so the form posts exactly as without script, and other scripts listening to the
// select (Timezone's local time) keep working. Without script the native select is the field.
//
// Read from the markup:
//   select: data-search-label (the search field's placeholder and accessible name) and
//     data-no-matches (the empty list's text, with {query}). The field's visible label, which
//     names the trigger and the list, is the enclosing <label>'s .settings__label.
//   option: data-detail (the muted right-hand text), data-keywords (extra search terms,
//     space-separated), data-exact (terms that, typed exactly, rank the option first — a
//     country's code). A disabled option with value "" is the empty prompt, never selectable.
//   optgroup: its label heads its rows while browsing; search results show them ungrouped.
(function () {
  'use strict';

  var SVG = 'http://www.w3.org/2000/svg';
  var uid = 0;

  // Case- and accent-insensitive, so "aland" finds "Åland Islands" and "sao" finds "São Paulo".
  function fold(s) {
    return s.normalize('NFD').replace(/\p{Diacritic}/gu, '').toLowerCase();
  }

  // An exact match first (the whole label, or an exact term), then labels with a word
  // starting with the query, then anything containing it — so "york" lists every row with
  // York in it. The list's own order is kept within each rank.
  function search(options, query) {
    var q = fold(query.trim());
    var ranked = [[], [], []];
    options.forEach(function (o) {
      if (o.label === q || o.exact.indexOf(q) >= 0) ranked[0].push(o);
      else if (o.label.indexOf(q) === 0 || o.label.indexOf(' ' + q) >= 0) ranked[1].push(o);
      else if (o.label.indexOf(q) >= 0 || o.foldedDetail.indexOf(q) >= 0 || o.keywords.some(function (k) { return k.indexOf(q) >= 0; })) ranked[2].push(o);
    });
    return ranked[0].concat(ranked[1], ranked[2]);
  }

  function icon(paths) {
    var svg = document.createElementNS(SVG, 'svg');
    svg.setAttribute('viewBox', '0 0 24 24');
    svg.setAttribute('fill', 'none');
    svg.setAttribute('stroke', 'currentColor');
    svg.setAttribute('stroke-width', '2');
    svg.setAttribute('stroke-linecap', 'round');
    svg.setAttribute('stroke-linejoin', 'round');
    svg.setAttribute('aria-hidden', 'true');
    svg.innerHTML = paths;
    return svg;
  }

  function el(tag, className, text) {
    var e = document.createElement(tag);
    if (className) e.className = className;
    if (text) e.textContent = text;
    return e;
  }

  function enhance(select) {
    var id = 'search-picker-' + ++uid;
    var fieldLabel = select.closest('label') && select.closest('label').querySelector('.settings__label');
    if (fieldLabel && !fieldLabel.id) fieldLabel.id = id + '-label';
    var labelledBy = fieldLabel ? fieldLabel.id : '';
    var emptyLabel = '';
    var options = [];
    Array.prototype.forEach.call(select.options, function (opt) {
      if (opt.value === '' && opt.disabled) {
        emptyLabel = opt.textContent;
        return;
      }
      var group = opt.parentElement.tagName === 'OPTGROUP' ? opt.parentElement.label : '';
      var detail = opt.dataset.detail || '';
      options.push({
        value: opt.value,
        text: opt.textContent,
        detail: detail,
        group: group,
        label: fold(opt.textContent),
        foldedDetail: fold(detail),
        keywords: (opt.dataset.keywords || '').split(' ').filter(Boolean).map(fold).concat(group ? [fold(group)] : []),
        exact: (opt.dataset.exact || '').split(' ').filter(Boolean).map(fold),
      });
    });

    // A required select that's hidden can't show the browser's own "Please select" bubble, so
    // the picker checks it on submit instead.
    var required = select.required;
    select.required = false;
    select.hidden = true;

    var root = el('div', 'search-picker');
    var trigger = el('button', 'search-picker__trigger');
    trigger.type = 'button';
    trigger.setAttribute('aria-haspopup', 'listbox');
    trigger.setAttribute('aria-expanded', 'false');
    if (labelledBy) trigger.setAttribute('aria-labelledby', labelledBy);
    trigger.setAttribute('aria-describedby', id + '-value');
    var valueBox = el('span', 'search-picker__value');
    valueBox.id = id + '-value';
    var chevron = icon('<path d="m6 9 6 6 6-6"/>');
    chevron.setAttribute('class', 'search-picker__chevron');
    trigger.append(valueBox, chevron);
    root.append(trigger);
    select.after(root);

    var panel = null;
    var input = null;
    var list = null;
    var shown = [];
    var active = 0;

    function renderValue() {
      valueBox.textContent = '';
      var o = options.find(function (o) { return o.value === select.value; });
      if (o) {
        valueBox.append(el('span', 'search-picker__label', o.text), el('span', 'search-picker__detail', o.detail));
      } else {
        valueBox.append(el('span', 'search-picker__label search-picker__label--unset', emptyLabel));
      }
    }

    function renderList(recentre) {
      var query = input.value.trim();
      shown = query ? search(options, query) : options;
      list.textContent = '';
      var lastGroup = null;
      shown.forEach(function (o, i) {
        if (!query && o.group && o.group !== lastGroup) {
          list.append(el('li', 'search-picker__group', o.group));
          list.lastChild.setAttribute('role', 'presentation');
        }
        lastGroup = o.group;
        var li = el('li', 'search-picker__option');
        li.id = id + '-' + i;
        li.setAttribute('role', 'option');
        li.setAttribute('aria-selected', String(o.value === select.value));
        if (o.value === select.value) li.classList.add('search-picker__option--selected');
        // Ungrouped search results carry their group (Timezone's region) as the detail.
        li.append(el('span', 'search-picker__label', o.text), el('span', 'search-picker__detail', query && o.group ? o.group : o.detail));
        li.addEventListener('pointerdown', function (e) { e.preventDefault(); });
        li.addEventListener('pointermove', function () { if (i !== active) setActive(i, false); });
        li.addEventListener('click', function () { pick(o.value); });
        o.row = li;
        list.append(li);
      });
      if (!shown.length) {
        var empty = el('li', 'search-picker__empty', (select.dataset.noMatches || '').replace('{query}', query));
        empty.setAttribute('role', 'presentation');
        list.append(empty);
      }
      setActive(active, recentre);
    }

    // Marks row i active and keeps it in view — centred on open, otherwise scrolled just
    // enough as ↑/↓ walk past the list's edge. Sets the list's own scrollTop rather than
    // calling scrollIntoView, which would scroll the page along with it.
    function setActive(i, recentre) {
      if (shown[active] && shown[active].row) shown[active].row.classList.remove('search-picker__option--active');
      active = Math.max(0, Math.min(shown.length - 1, i));
      var o = shown[active];
      if (!o) {
        input.removeAttribute('aria-activedescendant');
        return;
      }
      o.row.classList.add('search-picker__option--active');
      input.setAttribute('aria-activedescendant', o.row.id);
      var row = o.row;
      if (recentre) list.scrollTop = row.offsetTop - (list.clientHeight - row.offsetHeight) / 2;
      else if (row.offsetTop < list.scrollTop) list.scrollTop = row.offsetTop;
      else if (row.offsetTop + row.offsetHeight > list.scrollTop + list.clientHeight) list.scrollTop = row.offsetTop + row.offsetHeight - list.clientHeight;
    }

    function open() {
      panel = el('div', 'search-picker__panel');
      var box = el('div', 'search-picker__search');
      box.append(icon('<circle cx="11" cy="11" r="8"/><path d="m21 21-4.3-4.3"/>'));
      input = el('input');
      input.type = 'text';
      input.setAttribute('role', 'combobox');
      input.setAttribute('aria-label', select.dataset.searchLabel || '');
      input.setAttribute('aria-expanded', 'true');
      input.setAttribute('aria-controls', id + '-list');
      input.setAttribute('aria-autocomplete', 'list');
      input.placeholder = select.dataset.searchLabel || '';
      input.autocomplete = 'off';
      input.spellcheck = false;
      box.append(input);
      list = el('ul', 'search-picker__list');
      list.id = id + '-list';
      list.setAttribute('role', 'listbox');
      if (labelledBy) list.setAttribute('aria-labelledby', labelledBy);
      panel.append(box, list);
      root.append(panel);
      trigger.classList.add('search-picker__trigger--open');
      trigger.setAttribute('aria-expanded', 'true');
      input.addEventListener('input', function () {
        active = 0;
        list.scrollTop = 0;
        renderList(false);
      });
      input.addEventListener('keydown', onKeyDown);
      document.addEventListener('pointerdown', onOutside);
      active = Math.max(0, options.findIndex(function (o) { return o.value === select.value; }));
      renderList(true);
      input.focus();
    }

    function close(refocus) {
      if (!panel) return;
      panel.remove();
      panel = input = list = null;
      trigger.classList.remove('search-picker__trigger--open');
      trigger.setAttribute('aria-expanded', 'false');
      document.removeEventListener('pointerdown', onOutside);
      if (refocus) trigger.focus();
    }

    function pick(value) {
      if (value !== select.value) {
        select.value = value;
        renderValue();
        trigger.removeAttribute('aria-invalid');
        select.dispatchEvent(new Event('change', { bubbles: true }));
      }
      close(true);
    }

    function onOutside(e) {
      if (!root.contains(e.target)) close(false);
    }

    function onKeyDown(e) {
      switch (e.key) {
        case 'ArrowDown': setActive(active + 1, false); break;
        case 'ArrowUp': setActive(active - 1, false); break;
        case 'PageDown': setActive(active + 8, false); break;
        case 'PageUp': setActive(active - 8, false); break;
        case 'Enter': if (shown[active]) pick(shown[active].value); break;
        case 'Escape': close(true); break;
        case 'Tab': close(false); return;
        default: return;
      }
      e.preventDefault();
    }

    trigger.addEventListener('click', function () { panel ? close(false) : open(); });
    trigger.addEventListener('keydown', function (e) {
      if (!panel && (e.key === 'ArrowDown' || e.key === 'ArrowUp')) {
        e.preventDefault();
        open();
      }
    });
    // A disabled fieldset (the demo account's) disables the select; the trigger follows it.
    trigger.disabled = select.matches(':disabled');
    if (required && select.form) {
      select.form.addEventListener('submit', function (e) {
        if (select.value) return;
        e.preventDefault();
        trigger.setAttribute('aria-invalid', 'true');
        trigger.focus();
      });
    }
    renderValue();
  }

  Array.prototype.forEach.call(document.querySelectorAll('select[data-search-picker]'), enhance);
})();
