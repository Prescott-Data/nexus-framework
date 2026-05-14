/* Nexus Docs — Header enhancements
   - Injects version chip after site title
   - Injects GitHub repo + stars widget
   - Moves theme palette toggle to far-right
   - Opens external tabs in new window
*/
document.addEventListener('DOMContentLoaded', function () {

  /* ── 1. Version chip ─────────────────────────────────── */
  var title = document.querySelector('.md-header__title');
  var fallbackVersion = 'v0.4.0';
  var chip;
  if (title) {
    chip = document.createElement('span');
    chip.className = 'nx-version-chip';
    chip.textContent = fallbackVersion;
    title.insertAdjacentElement('afterend', chip);
  }

  /* Fetch live version from GitHub tags */
  fetch('https://api.github.com/repos/Prescott-Data/nexus-framework/tags')
    .then(function (r) { return r.json(); })
    .then(function (tags) {
      if (Array.isArray(tags) && tags.length > 0) {
        if (chip) chip.textContent = tags[0].name;
      }
    })
    .catch(function () {});

  /* ── 2. GitHub stars widget ─────────────────────────── */
  var inner = document.querySelector('.md-header__inner');
  var palette = document.querySelector('form[data-md-component="palette"]');
  if (inner && palette) {
    var ghBtn = document.createElement('a');
    ghBtn.href = 'https://github.com/Prescott-Data/nexus-framework';
    ghBtn.target = '_blank';
    ghBtn.rel = 'noopener noreferrer';
    ghBtn.className = 'nx-gh-btn';
    ghBtn.setAttribute('aria-label', 'Star Nexus on GitHub');
    ghBtn.innerHTML =
      '<span class="nx-gh-repo">Prescott-Data/nexus</span>' +
      '<span class="nx-gh-sep">★</span>' +
      '<span class="nx-stars-count">—</span>';

    inner.insertBefore(ghBtn, palette);
    inner.appendChild(palette);
  }

  /* ── 3. Async star count ────────────────────────────── */
  fetch('https://api.github.com/repos/Prescott-Data/nexus-framework')
    .then(function (r) { return r.json(); })
    .then(function (data) {
      var el = document.querySelector('.nx-stars-count');
      if (el && typeof data.stargazers_count === 'number') {
        var n = data.stargazers_count;
        el.textContent = n >= 1000 ? (n / 1000).toFixed(1) + 'k' : String(n);
      }
    })
    .catch(function () {});

  /* ── 4. External tabs → new window ────────────────────── */
  var EXT = ['https://developers.prescottdata.io', 'https://discord.gg'];
  document.querySelectorAll('.md-tabs__link').forEach(function (link) {
    var href = link.getAttribute('href') || '';
    if (EXT.some(function (p) { return href.startsWith(p); })) {
      link.setAttribute('target', '_blank');
      link.setAttribute('rel', 'noopener noreferrer');
    }
  });

});
