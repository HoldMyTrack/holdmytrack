// The Settings page's System/Light/Dark control (templates/pages/settings.html). The choice
// lives in localStorage (hmt_theme; absent = System) and on <html data-theme>, which
// tokens.css reads; templates/theme.html applies it before first paint on every page.
(function () {
  var control = document.querySelector('[data-theme-control]');
  if (!control) return;

  function stored() {
    try {
      var theme = localStorage.getItem('hmt_theme');
      return theme === 'light' || theme === 'dark' ? theme : 'system';
    } catch (e) {
      return 'system';
    }
  }

  function show(choice) {
    control.querySelectorAll('[data-theme-choice]').forEach(function (button) {
      button.setAttribute('aria-pressed', String(button.dataset.themeChoice === choice));
    });
  }

  control.addEventListener('click', function (event) {
    var button = event.target.closest('[data-theme-choice]');
    if (!button) return;
    var choice = button.dataset.themeChoice;
    try {
      if (choice === 'system') localStorage.removeItem('hmt_theme');
      else localStorage.setItem('hmt_theme', choice);
    } catch (e) {}
    if (choice === 'system') delete document.documentElement.dataset.theme;
    else document.documentElement.dataset.theme = choice;
    show(choice);
  });

  show(stored());
  // Hidden until now: without this script the buttons would do nothing.
  control.hidden = false;
})();
