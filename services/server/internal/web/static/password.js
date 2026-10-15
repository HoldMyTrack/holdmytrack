// The show-password button on the sign-in, sign-up and reset pages (templates/auth.html's
// "password-reveal"). It flips its field between hidden and plain text, and hides it again
// on submit so a password manager offering to save it sees an ordinary password field.
(function () {
  document.querySelectorAll('[data-password-reveal]').forEach(function (button) {
    var input = document.getElementById(button.getAttribute('aria-controls'));
    if (!input) return;

    function show(visible) {
      input.type = visible ? 'text' : 'password';
      button.setAttribute('aria-pressed', String(visible));
    }

    button.addEventListener('click', function () {
      show(input.type === 'password');
    });
    if (input.form) {
      input.form.addEventListener('submit', function () {
        show(false);
      });
    }
    button.hidden = false;
  });
})();
