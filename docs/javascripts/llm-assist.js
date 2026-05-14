function initLLMWidget() {
  var contentInner = document.querySelector('.md-content__inner');
  if (!contentInner) return;

  // Skip if already injected (instant navigation guard)
  if (contentInner.querySelector('.nx-llm-widget')) return;

  // Hide ALL existing MkDocs action buttons (edit + view source)
  var allButtons = contentInner.querySelectorAll('.md-content__button');
  var rawUrl = null;  // resolved below — never default to the HTML page URL
  allButtons.forEach(function(btn) {
    if (btn.tagName === 'A' && btn.href) {
      // Prefer the edit button URL for raw markdown (GitHub-hosted docs)
      var u = btn.href
        .replace('github.com', 'raw.githubusercontent.com')
        .replace('/edit/', '/')
        .replace('/blob/', '/');
      if (u !== btn.href) rawUrl = u;
    }
    btn.style.display = 'none';
  });

  // Local dev fallback: derive the .md source path from the current URL.
  // MkDocs serves /foo/bar/ from docs/foo/bar.md (or docs/foo/bar/index.md).
  if (!rawUrl && window.location.hostname === 'localhost') {
    var pathname = window.location.pathname.replace(/\/$/, '') || '/index';
    rawUrl = '/docs' + pathname + '.md';
    window._nxMdPathBase = pathname;
  }

  var icons = {
    copy: '<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="9" y="9" width="13" height="13" rx="2" ry="2"></rect><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"></path></svg>',
    check: '<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="20 6 9 17 4 12"></polyline></svg>',
    markdown: '<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M2 3h20v18H2z"></path><path d="M6 16V8l4 4 4-4v8"></path><path d="M16 11v5h2v-5z"></path></svg>',
    chevron: '<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="6 9 12 15 18 9"></polyline></svg>',
    external: '<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="margin-left:auto;opacity:0.4"><path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"></path><polyline points="15 3 21 3 21 9"></polyline><line x1="10" y1="14" x2="21" y2="3"></line></svg>'
  };

  var menuItems = [
    { id: 'copy',     icon: icons.copy,     title: 'Copy page',       sub: 'Copy page as Markdown for LLMs' },
    { id: 'markdown', icon: icons.markdown, title: 'View as Markdown', sub: 'View this page as plain text' }
  ];

  var container = document.createElement('div');
  container.className = 'nx-llm-widget';

  var trigger = document.createElement('div');
  trigger.className = 'nx-llm-trigger';

  var triggerMain = document.createElement('div');
  triggerMain.className = 'nx-llm-btn-main';
  triggerMain.innerHTML = icons.copy;
  triggerMain.title = 'Copy page';

  var triggerChev = document.createElement('div');
  triggerChev.className = 'nx-llm-btn-chev';
  triggerChev.innerHTML = icons.chevron;
  triggerChev.title = 'More options';

  trigger.appendChild(triggerMain);
  trigger.appendChild(triggerChev);

  var dropdown = document.createElement('div');
  dropdown.className = 'nx-llm-dropdown';

  menuItems.forEach(function(item) {
    var el = document.createElement('div');
    el.className = 'nx-llm-item';
    var textHtml = '<div class="nx-llm-text"><strong>' + item.title + '</strong><span>' + item.sub + '</span></div>';
    el.innerHTML = '<div class="nx-llm-icon-box">' + item.icon + '</div>' + textHtml + (item.id === 'markdown' ? icons.external : '');

    el.addEventListener('click', function(e) {
      e.preventDefault();
      dropdown.classList.remove('open');
      if (item.id === 'copy')     fetchAndCopy();
      if (item.id === 'markdown') viewAsMarkdown();
    });

    dropdown.appendChild(el);
  });

  container.appendChild(trigger);
  container.appendChild(dropdown);

  function getText(callback) {
    // Primary: read from the raw markdown embedded by a MkDocs hook (if present)
    var embedded = document.getElementById('nx-page-source');
    if (embedded) {
      var src = embedded.textContent || embedded.innerHTML || '';
      if (src.trim()) {
        callback(src);
        return;
      }
    }

    // Fallback: fetch from GitHub raw source or local docs
    if (!rawUrl) {
      var article = document.querySelector('article') || document.querySelector('.md-content');
      callback(article ? article.innerText : '(no content)');
      return;
    }

    var candidates = [rawUrl];
    if (window.location.hostname === 'localhost' && window._nxMdPathBase) {
      candidates.push('/docs' + window._nxMdPathBase + '/index.md');
    }

    function tryNext(urls) {
      if (!urls.length) {
        var article = document.querySelector('article') || document.querySelector('.md-content');
        callback(article ? article.innerText : '(no content)');
        return;
      }
      var url = urls.shift();
      fetch(url).then(function(r) {
        if (!r.ok) throw new Error('not ok');
        return r.text();
      }).then(function(text) {
        if (text.trimStart().startsWith('<!') || text.trimStart().startsWith('<html')) {
          tryNext(urls);
        } else {
          callback(text);
        }
      }).catch(function() { tryNext(urls); });
    }

    tryNext(candidates);
  }

  function fetchAndCopy() {
    triggerMain.innerHTML = '<span style="color:var(--nx-brand)">' + icons.check + '</span>';
    getText(function(text) {
      navigator.clipboard.writeText(text);
      setTimeout(function() { triggerMain.innerHTML = icons.copy; }, 2000);
    });
  }

  function viewAsMarkdown() {
    getText(function(text) {
      var win = window.open('', '_blank');
      if (win) {
        win.document.write('<pre style="white-space:pre-wrap;word-wrap:break-word;font-family:monospace;padding:20px">' + text.replace(/</g, '&lt;').replace(/>/g, '&gt;') + '</pre>');
        win.document.close();
      }
    });
  }

  // Inject widget — prepend to content area
  var wrapper = document.createElement('div');
  wrapper.className = 'md-content__button';
  wrapper.style.cssText = 'position:relative;z-index:1;';
  wrapper.appendChild(container);
  contentInner.insertBefore(wrapper, contentInner.firstChild);

  // Split-button event handlers
  triggerChev.addEventListener('click', function(e) {
    e.stopPropagation();
    dropdown.classList.toggle('open');
  });

  triggerMain.addEventListener('click', function(e) {
    e.stopPropagation();
    dropdown.classList.remove('open');
    fetchAndCopy();
  });

  document.addEventListener('click', function(e) {
    if (!container.contains(e.target)) dropdown.classList.remove('open');
  });
}

// Run on first load
document.addEventListener('DOMContentLoaded', initLLMWidget);

// Re-run on every instant navigation (Material for MkDocs SPA mode)
if (typeof document$ !== 'undefined') {
  document$.subscribe(initLLMWidget);
}
