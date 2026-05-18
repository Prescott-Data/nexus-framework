/* Nexus Docs — Header enhancements
   - Injects version chip after site title (live from GitHub tags)
   - Injects GitHub repo + stars widget (Prescott-Data/nexus ★ N)
   - Moves theme palette toggle to far-right
   - Opens external tabs in new window
*/
function initHeader() {

  /* ── 1. Version chip — live from GitHub API ─────────── */
  var title = document.querySelector('.md-header__title');
  var chip = document.querySelector('.nx-version-chip');
  
  // Skip if already injected (instant navigation guard)
  if (!chip && title) {
    chip = document.createElement('span');
    chip.className = 'nx-version-chip';
    chip.textContent = '…';
    title.insertAdjacentElement('afterend', chip);
  }

  /* Fetch the LATEST tag from GitHub — single source of truth */
  if (chip) {
    fetch('https://api.github.com/repos/Prescott-Data/nexus-framework/tags')
      .then(function (r) { return r.json(); })
      .then(function (tags) {
        if (Array.isArray(tags) && tags.length > 0) {
          var latestVersion = tags[0].name;
          if (chip) chip.textContent = latestVersion;

          /* Also update hero badge if present */
          var heroBadge = document.getElementById('nx-hero-version-badge');
          if (heroBadge) {
            heroBadge.innerHTML = latestVersion + ' &middot; Apache 2.0 &middot; Production Ready';
          }
        }
      })
      .catch(function () {
        /* On network failure, hide chip entirely rather than showing stale data */
        if (chip) chip.style.display = 'none';
      });
  }

  /* ── 2. GitHub stars widget ─────────────────────────── */
  var inner = document.querySelector('.md-header__inner');
  var palette = document.querySelector('form[data-md-component="palette"]');
  var existingGhBtn = document.querySelector('.nx-gh-btn');
  
  // Skip if already injected (instant navigation guard)
  if (inner && palette && !existingGhBtn) {
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

}

// Run on first load
document.addEventListener('DOMContentLoaded', initHeader);

// Re-run on every instant navigation (Material for MkDocs SPA mode)
if (typeof document$ !== 'undefined') {
  document$.subscribe(initHeader);
}
