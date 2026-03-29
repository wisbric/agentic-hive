(function() {
  'use strict';

  let currentUser = null;
  let servers = [];
  let templates = [];
  let refreshTimer = null;
  const expandedServers = new Set();

  function getCookie(name) {
    const match = document.cookie.match(new RegExp('(?:^|; )' + name + '=([^;]*)'));
    return match ? decodeURIComponent(match[1]) : '';
  }

  async function api(method, path, body) {
    const opts = { method, credentials: 'same-origin', headers: {} };
    if (body) {
      opts.headers['Content-Type'] = 'application/json';
      opts.body = JSON.stringify(body);
    }
    if (method !== 'GET' && method !== 'HEAD') {
      opts.headers['X-CSRF-Token'] = getCookie('csrf');
    }
    const res = await fetch(path, opts);
    if (res.status === 401) {
      showView('login');
      throw new Error('unauthorized');
    }
    return res;
  }

  function showView(name) {
    document.querySelectorAll('.view').forEach(v => v.style.display = 'none');
    const view = document.getElementById(name + '-view');
    if (view) view.style.display = '';
    if (refreshTimer) { clearInterval(refreshTimer); refreshTimer = null; }
  }

  // --- Setup ---
  document.getElementById('setup-form').addEventListener('submit', async (e) => {
    e.preventDefault();
    const username = document.getElementById('setup-username').value;
    const password = document.getElementById('setup-password').value;
    try {
      const res = await api('POST', '/api/auth/setup', { username, password });
      if (res.ok) {
        const data = await res.json();
        currentUser = data.username;
        showDashboard();
      } else {
        document.getElementById('setup-error').textContent = 'Setup failed';
      }
    } catch (e) { document.getElementById('setup-error').textContent = e.message; }
  });

  // --- Login ---
  document.getElementById('login-form').addEventListener('submit', async (e) => {
    e.preventDefault();
    const username = document.getElementById('login-username').value;
    const password = document.getElementById('login-password').value;
    try {
      const res = await api('POST', '/api/auth/login', { username, password });
      if (res.ok) {
        const data = await res.json();
        currentUser = data.username;
        showDashboard();
      } else {
        document.getElementById('login-error').textContent = 'Invalid credentials';
      }
    } catch (e) { document.getElementById('login-error').textContent = e.message; }
  });

  // --- Logout ---
  window.doLogout = async function() { await api('POST', '/api/auth/logout'); currentUser = null; showView('login'); };

  // --- Add Server ---
  document.getElementById('show-add-server').addEventListener('click', () => {
    const section = document.getElementById('add-server-section');
    section.style.display = section.style.display === 'none' ? '' : 'none';
  });

  document.getElementById('add-server-form').addEventListener('submit', async (e) => {
    e.preventDefault();
    const name = document.getElementById('server-name').value;
    const host = document.getElementById('server-host').value;
    const port = parseInt(document.getElementById('server-port').value) || 22;
    const sshUser = document.getElementById('server-user').value;
    const key = document.getElementById('server-key').value;

    const btn = e.submitter || e.target.querySelector('button[type="submit"]');
    const origText = btn.textContent;
    btn.disabled = true;
    btn.textContent = 'Adding server...';

    try {
      const res = await api('POST', '/api/servers', { name, host, port, sshUser });
      if (!res.ok) throw new Error('Failed to create server');
      const srv = await res.json();

      btn.textContent = 'Uploading key...';

      // Upload key
      await fetch('/api/servers/' + srv.ID + '/key', {
        method: 'PUT',
        credentials: 'same-origin',
        headers: { 'X-CSRF-Token': getCookie('csrf') },
        body: key,
      });

      btn.textContent = 'Added!';
      btn.classList.add('btn-success');
      document.getElementById('add-server-form').reset();
      await loadServers();
      setTimeout(() => {
        document.getElementById('add-server-section').style.display = 'none';
        btn.textContent = origText;
        btn.disabled = false;
        btn.classList.remove('btn-success');
      }, 1500);
    } catch (e) {
      btn.textContent = 'Failed: ' + e.message;
      btn.classList.add('btn-error');
      setTimeout(() => { btn.textContent = origText; btn.disabled = false; btn.classList.remove('btn-error'); }, 3000);
    }
  });

  // --- Dashboard ---
  async function loadTemplates() {
    try {
      const res = await api('GET', '/api/templates');
      if (res.ok) templates = await res.json();
    } catch (e) { /* fallback to empty */ }
    if (!templates || templates.length === 0) {
      templates = [
        {ID: 'claude', Name: 'Claude Code', Command: 'claude', Workdir: '~/'},
        {ID: 'shell', Name: 'Shell', Command: 'bash', Workdir: '~/'},
      ];
    }
  }

  async function showDashboard() {
    showView('dashboard');
    document.getElementById('current-user').textContent = currentUser || '';
    await loadTemplates();
    await loadServers();
    // Show admin section if user has admin role
    loadAdminSection();
  }

  async function loadServers() {
    try {
      const res = await api('GET', '/api/servers');
      if (!res.ok) return;
      servers = await res.json();
      renderServers();
    } catch (e) { /* handled by api() */ }
  }

  function renderServers() {
    const container = document.getElementById('servers-list');
    if (!servers || servers.length === 0) {
      container.innerHTML = '<p class="no-sessions">No servers registered yet. Add one to get started.</p>';
      return;
    }

    container.innerHTML = servers.map(srv => `
      <div class="server-card" data-id="${srv.ID}">
        <div class="server-header" onclick="toggleServer('${srv.ID}')">
          <div class="server-name">
            <span class="status-dot ${srv.Status}"></span>
            ${esc(srv.Name)}
          </div>
          <span class="server-host">${esc(srv.SSHUser)}@${esc(srv.Host)}:${srv.Port}</span>
        </div>
        <div class="server-body" id="body-${srv.ID}">
          <div id="sessions-${srv.ID}">Loading sessions...</div>
          <div class="new-session-form">
            <select id="template-${srv.ID}" onchange="onTemplateChange('${srv.ID}')">
              ${templates.map(t => `<option value="${esc(t.ID)}" data-cmd="${esc(t.Command)}" data-workdir="${esc(t.Workdir)}">${esc(t.Name)}</option>`).join('')}
            </select>
            <input type="text" id="label-${srv.ID}" placeholder="Session label" value="claude">
            <input type="text" id="workdir-${srv.ID}" placeholder="Working dir" value="~/" class="workdir-input">
            <button onclick="createSession('${srv.ID}')">New Session</button>
          </div>
          <button class="btn-small btn-danger" style="margin-top:12px" onclick="deleteServer('${srv.ID}')">Remove Server</button>
        </div>
      </div>
    `).join('');

    // Restore expanded state and re-fetch sessions for expanded servers
    expandedServers.forEach(id => {
      const body = document.getElementById('body-' + id);
      if (body) {
        body.classList.add('open');
        loadSessions(id);
      } else {
        // Server no longer exists, remove from expanded set
        expandedServers.delete(id);
      }
    });
  }

  window.toggleSettings = function(name) {
    const body = document.getElementById('settings-body-' + name);
    const chevron = document.getElementById('chevron-' + name);
    if (body.classList.contains('open')) {
      body.classList.remove('open');
      if (chevron) chevron.classList.remove('open');
    } else {
      body.classList.add('open');
      if (chevron) chevron.classList.add('open');
    }
  };

  window.toggleServer = async function(id) {
    const body = document.getElementById('body-' + id);
    if (body.classList.contains('open')) {
      body.classList.remove('open');
      expandedServers.delete(id);
      return;
    }
    body.classList.add('open');
    expandedServers.add(id);
    await loadSessions(id);
  };

  async function loadSessions(serverID, live) {
    const container = document.getElementById('sessions-' + serverID);
    try {
      const url = '/api/servers/' + serverID + '/sessions' + (live ? '?live=true' : '');
      const res = await api('GET', url);
      const sessions = await res.json();
      if (!sessions || sessions.length === 0) {
        container.innerHTML = '<p class="no-sessions">No active sessions</p>';
        return;
      }
      container.innerHTML = sessions.map(s => `
        <div class="session-row">
          <div>
            <div class="session-name">${esc(s.name)}</div>
            <div class="session-meta">${s.windows} window(s) · idle ${s.idle} · ${s.attached > 0 ? 'attached' : 'detached'}</div>
          </div>
          <div class="session-actions">
            <button onclick="copySSH('${esc(s.sshCommand)}')">SSH</button>
            <button onclick="openTerminal('${serverID}', '${esc(s.name)}')">Terminal</button>
            <button class="btn-danger" onclick="killSession('${serverID}', '${esc(s.name)}')">Kill</button>
          </div>
        </div>
      `).join('');
    } catch (e) {
      container.innerHTML = '<p class="error">Failed to load sessions</p>';
    }
  }

  window.onTemplateChange = function(serverID) {
    const sel = document.getElementById('template-' + serverID);
    const opt = sel.options[sel.selectedIndex];
    const workdirInput = document.getElementById('workdir-' + serverID);
    if (workdirInput && opt.dataset.workdir) {
      workdirInput.value = opt.dataset.workdir;
    }
    // Update label to match template name
    const labelInput = document.getElementById('label-' + serverID);
    const name = opt.textContent.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/-+$/, '');
    if (labelInput) labelInput.value = name;
  };

  window.createSession = async function(serverID) {
    const sel = document.getElementById('template-' + serverID);
    const opt = sel.options[sel.selectedIndex];
    const label = document.getElementById('label-' + serverID).value || 'session';
    const workdir = document.getElementById('workdir-' + serverID).value || '~/';
    const command = opt.dataset.cmd || 'bash';
    // Find the button and show loading
    const btn = event.target;
    const origText = btn.textContent;
    btn.disabled = true;
    btn.textContent = 'Creating...';
    try {
      await api('POST', '/api/servers/' + serverID + '/sessions', {
        label: label,
        command: command,
        workdir: workdir
      });
      btn.textContent = 'Created! Loading...';
      btn.classList.add('btn-success');
      await loadSessions(serverID, true);
      setTimeout(() => { btn.textContent = origText; btn.disabled = false; btn.classList.remove('btn-success'); }, 1500);
    } catch (e) {
      btn.textContent = 'Failed';
      btn.classList.add('btn-error');
      setTimeout(() => { btn.textContent = origText; btn.disabled = false; btn.classList.remove('btn-error'); }, 2000);
    }
  };

  window.killSession = async function(serverID, name) {
    if (!confirm('Kill session ' + name + '?')) return;
    // Find the clicked button
    const btn = event.target;
    const origText = btn.textContent;
    btn.disabled = true;
    btn.textContent = 'Killing...';
    try {
      await api('DELETE', '/api/servers/' + serverID + '/sessions/' + name);
      btn.textContent = 'Done';
      await loadSessions(serverID, true);
    } catch (e) {
      btn.textContent = 'Failed';
      btn.classList.add('btn-error');
      setTimeout(() => { btn.textContent = origText; btn.disabled = false; btn.classList.remove('btn-error'); }, 2000);
    }
  };

  window.copySSH = function(cmd) {
    const btn = event.target;
    navigator.clipboard.writeText(cmd).then(() => {
      const orig = btn.textContent;
      btn.textContent = 'Copied!';
      btn.classList.add('btn-success');
      setTimeout(() => { btn.textContent = orig; btn.classList.remove('btn-success'); }, 1500);
    });
  };

  window.openTerminal = function(serverID, sessionName) {
    window.open('/static/terminal.html?server=' + serverID + '&session=' + sessionName, '_blank');
  };

  window.refreshDashboard = async function() {
    const btn = document.getElementById('refresh-btn');
    if (btn) { btn.disabled = true; btn.textContent = '↻ Refreshing...'; }
    await loadServers();
    // Also live-refresh sessions for any expanded servers
    for (const id of expandedServers) {
      await loadSessions(id, true);
    }
    if (btn) { btn.textContent = '↻ Done!'; setTimeout(() => { btn.textContent = '↻ Refresh'; btn.disabled = false; }, 1000); }
  };

  window.deleteServer = async function(id) {
    if (!confirm('Remove this server?')) return;
    try {
      await api('DELETE', '/api/servers/' + id);
      expandedServers.delete(id);
      loadServers();
    } catch (e) { alert('Failed to delete server'); }
  };

  // --- Admin Section ---
  async function loadAdminSection() {
    const adminSection = document.getElementById('admin-section');
    if (!adminSection) return;

    // Try to fetch config — if 403, user is not admin
    try {
      const res = await api('GET', '/api/admin/config');
      if (!res.ok) {
        adminSection.style.display = 'none';
        return;
      }
      const cfg = await res.json();
      adminSection.style.display = '';

      document.getElementById('admin-config-auth').textContent = cfg.authMode || '—';
      document.getElementById('admin-config-keystore').textContent = cfg.keyStoreBackend || '—';
      document.getElementById('admin-config-poll').textContent = (cfg.pollInterval || 30) + 's';

      await refreshUserList();
      await loadSettings();
    } catch (e) {
      adminSection.style.display = 'none';
    }
  }

  // --- Settings ---

  function setFieldFromSetting(inputId, labelId, setting) {
    if (!setting) return;
    const input = document.getElementById(inputId);
    const label = document.getElementById(labelId);
    if (!input) return;

    if (setting.source === 'env') {
      input.readOnly = true;
      input.value = setting.value || '';
      // Add (env) badge to label if not already present
      if (label && !label.querySelector('.env-badge')) {
        const badge = document.createElement('span');
        badge.className = 'env-badge';
        badge.textContent = 'env';
        label.appendChild(badge);
      }
    } else {
      input.readOnly = false;
      input.value = setting.value || '';
    }
  }

  function setSecretFieldFromSetting(inputId, labelId, setting) {
    if (!setting) return;
    const input = document.getElementById(inputId);
    const label = document.getElementById(labelId);
    if (!input) return;

    if (setting.source === 'env') {
      input.readOnly = true;
      input.placeholder = setting.is_set ? '****' : 'Not set';
      if (label && !label.querySelector('.env-badge')) {
        const badge = document.createElement('span');
        badge.className = 'env-badge';
        badge.textContent = 'env';
        label.appendChild(badge);
      }
    } else {
      input.readOnly = false;
      input.placeholder = setting.is_set ? '****' : 'Leave blank to keep current';
    }
    // Never populate secret values — leave empty so user must type to change
    input.value = '';
  }

  async function loadSettings() {
    try {
      const res = await api('GET', '/api/admin/settings');
      if (!res.ok) return;
      const s = await res.json();

      const oidc = s.oidc || {};
      const vault = s.vault || {};

      // OIDC fields
      setFieldFromSetting('oidc-issuer-url',   'lbl-oidc-issuer-url',   oidc.issuer_url);
      setFieldFromSetting('oidc-client-id',    'lbl-oidc-client-id',    oidc.client_id);
      setSecretFieldFromSetting('oidc-client-secret', 'lbl-oidc-client-secret', oidc.client_secret);
      setFieldFromSetting('oidc-redirect-url', 'lbl-oidc-redirect-url', oidc.redirect_url);
      setFieldFromSetting('oidc-roles-claim',  'lbl-oidc-roles-claim',  oidc.roles_claim);
      setFieldFromSetting('oidc-admin-group',  'lbl-oidc-admin-group',  oidc.admin_group);

      // Vault fields
      setFieldFromSetting('vault-address',     'lbl-vault-address',     vault.address);
      setSecretFieldFromSetting('vault-token', 'lbl-vault-token',       vault.token);
      setFieldFromSetting('vault-secret-path', 'lbl-vault-secret-path', vault.secret_path);

    } catch (e) {
      // Settings endpoint may not exist yet — fail silently
    }
  }

  function showStatus(elementId, message, isError) {
    const el = document.getElementById(elementId);
    if (!el) return;
    el.textContent = message;
    el.className = 'settings-status ' + (isError ? 'error' : 'success');
    setTimeout(() => { el.textContent = ''; el.className = 'settings-status'; }, 5000);
  }

  function collectFormValues(fields) {
    const values = {};
    fields.forEach(({ key, inputId, isSecret }) => {
      const input = document.getElementById(inputId);
      if (!input || input.readOnly) return;
      const val = input.value.trim();
      if (isSecret && val === '') return; // Don't send blank secret
      values[key] = val;
    });
    return values;
  }

  document.getElementById('oidc-settings-form').addEventListener('submit', async (e) => {
    e.preventDefault();
    const btn = e.submitter || e.target.querySelector('button[type="submit"]');
    await saveSettingsWithFeedback(btn, saveOIDCSettings);
  });

  document.getElementById('vault-settings-form').addEventListener('submit', async (e) => {
    e.preventDefault();
    const btn = e.submitter || e.target.querySelector('button[type="submit"]');
    await saveSettingsWithFeedback(btn, saveVaultSettings);
  });

  async function saveSettingsWithFeedback(btn, saveFn) {
    if (!btn) { await saveFn(); return; }
    const orig = btn.textContent;
    btn.disabled = true;
    btn.textContent = 'Saving...';
    await saveFn();
    btn.textContent = 'Saved!';
    btn.classList.add('btn-success');
    await loadSettings(); // reload to show current values
    setTimeout(() => { btn.textContent = orig; btn.disabled = false; btn.classList.remove('btn-success'); }, 1500);
  }

  async function saveOIDCSettings() {
    const values = collectFormValues([
      { key: 'oidc.issuer_url',    inputId: 'oidc-issuer-url' },
      { key: 'oidc.client_id',     inputId: 'oidc-client-id' },
      { key: 'oidc.client_secret', inputId: 'oidc-client-secret', isSecret: true },
      { key: 'oidc.redirect_url',  inputId: 'oidc-redirect-url' },
      { key: 'oidc.roles_claim',   inputId: 'oidc-roles-claim' },
      { key: 'oidc.admin_group',   inputId: 'oidc-admin-group' },
    ]);
    try {
      const res = await api('PUT', '/api/admin/settings', values);
      if (res.ok) {
        showStatus('oidc-status', 'OIDC settings saved.', false);
      } else {
        const data = await res.json().catch(() => ({}));
        showStatus('oidc-status', 'Save failed: ' + (data.error || res.status), true);
      }
    } catch (e) {
      showStatus('oidc-status', 'Save failed: ' + e.message, true);
    }
  }

  async function saveVaultSettings() {
    const values = collectFormValues([
      { key: 'vault.address',     inputId: 'vault-address' },
      { key: 'vault.token',       inputId: 'vault-token', isSecret: true },
      { key: 'vault.secret_path', inputId: 'vault-secret-path' },
    ]);
    try {
      const res = await api('PUT', '/api/admin/settings', values);
      if (res.ok) {
        showStatus('vault-status', 'Vault settings saved.', false);
      } else {
        const data = await res.json().catch(() => ({}));
        showStatus('vault-status', 'Save failed: ' + (data.error || res.status), true);
      }
    } catch (e) {
      showStatus('vault-status', 'Save failed: ' + e.message, true);
    }
  }

  window.testOIDC = async function() {
    showStatus('oidc-status', 'Testing OIDC connection...', false);
    try {
      const body = {
        issuer_url: document.getElementById('oidc-issuer-url').value,
        client_id: document.getElementById('oidc-client-id').value,
      };
      const res = await api('POST', '/api/admin/settings/test-oidc', body);
      const data = await res.json().catch(() => ({}));
      if (res.ok) {
        showStatus('oidc-status', data.message || 'OIDC connection OK.', false);
      } else {
        showStatus('oidc-status', 'Test failed: ' + (data.error || res.status), true);
      }
    } catch (e) {
      showStatus('oidc-status', 'Test failed: ' + e.message, true);
    }
  };

  window.testVault = async function() {
    showStatus('vault-status', 'Testing Vault connection...', false);
    try {
      const body = {
        address: document.getElementById('vault-address').value,
        token: document.getElementById('vault-token').value,
      };
      const res = await api('POST', '/api/admin/settings/test-vault', body);
      const data = await res.json().catch(() => ({}));
      if (res.ok) {
        showStatus('vault-status', data.message || 'Vault connection OK.', false);
      } else {
        showStatus('vault-status', 'Test failed: ' + (data.error || res.status), true);
      }
    } catch (e) {
      showStatus('vault-status', 'Test failed: ' + e.message, true);
    }
  };

  async function refreshUserList() {
    const container = document.getElementById('admin-users-list');
    if (!container) return;
    try {
      const res = await api('GET', '/api/users');
      if (!res.ok) return;
      const users = await res.json();
      if (!users || users.length === 0) {
        container.innerHTML = '<p class="no-sessions">No users found.</p>';
        return;
      }
      container.innerHTML = users.map(u => `
        <div class="user-row">
          <div>
            <span class="user-name">${esc(u.username)}</span>
            <span class="user-role">${esc(u.role)}</span>
            <span class="user-meta">${esc(u.createdAt)}</span>
          </div>
          <div>
            ${u.username !== currentUser ? `<button class="btn-small btn-danger" onclick="deleteUser('${esc(u.id)}', '${esc(u.username)}')">Delete</button>` : '<span class="user-meta">(you)</span>'}
          </div>
        </div>
      `).join('');
    } catch (e) {
      container.innerHTML = '<p class="error">Failed to load users</p>';
    }
  }

  window.deleteUser = async function(id, username) {
    if (!confirm('Delete user ' + username + '? This cannot be undone.')) return;
    try {
      const res = await api('DELETE', '/api/users/' + id);
      if (!res.ok) {
        const data = await res.json();
        alert('Failed: ' + (data.error || 'unknown error'));
        return;
      }
      await refreshUserList();
    } catch (e) { alert('Failed to delete user'); }
  };

  function esc(s) {
    const d = document.createElement('div');
    d.textContent = s;
    return d.innerHTML;
  }

  // --- User menu ---
  window.toggleUserMenu = function() {
    document.getElementById('user-menu-dropdown').classList.toggle('open');
  };
  document.addEventListener('click', function(e) {
    const menu = document.getElementById('user-menu-dropdown');
    const trigger = document.getElementById('user-menu-trigger');
    if (menu && !menu.contains(e.target) && !trigger.contains(e.target)) {
      menu.classList.remove('open');
    }
  });

  // --- Theme toggle ---
  window.toggleTheme = function() {
    const html = document.documentElement;
    const current = html.getAttribute('data-theme') || 'dark';
    const next = current === 'dark' ? 'light' : 'dark';
    html.setAttribute('data-theme', next);
    localStorage.setItem('agentic-hive-theme', next);
    updateThemeUI(next);
  };
  function updateThemeUI(theme) {
    document.getElementById('theme-icon').textContent = theme === 'dark' ? '\u263E' : '\u2600';
    document.getElementById('theme-label').textContent = theme === 'dark' ? 'Light mode' : 'Dark mode';
  }
  (function() {
    const saved = localStorage.getItem('agentic-hive-theme') || 'dark';
    document.documentElement.setAttribute('data-theme', saved);
    // Update UI after DOM is ready (elements may not exist yet at this point; safe to call, will be no-op if elements missing)
    document.addEventListener('DOMContentLoaded', function() { updateThemeUI(saved); }, { once: true });
    // Also try immediately in case DOMContentLoaded already fired
    if (document.readyState !== 'loading') { updateThemeUI(saved); }
  })();

  // --- About modal ---
  window.showAbout = async function() {
    document.getElementById('about-modal').style.display = '';
    document.getElementById('user-menu-dropdown').classList.remove('open');
    try {
      const res = await fetch('/api/about');
      if (res.ok) {
        const info = await res.json();
        document.getElementById('about-version').textContent = info.version || 'dev';
        document.getElementById('about-commit').textContent = info.commit || 'unknown';
        document.getElementById('about-uptime').textContent = info.uptime || '-';
      }
    } catch(e) {}
  };
  window.closeAbout = function() { document.getElementById('about-modal').style.display = 'none'; };

  // --- Init ---
  async function init() {
    try {
      const setupRes = await fetch('/api/auth/setup/status');
      const setupData = await setupRes.json();
      if (setupData.needed) {
        showView('setup');
        return;
      }

      // Show SSO button if OIDC is configured
      if (setupData.oidc_available) {
        const ssoDiv = document.getElementById('login-sso');
        if (ssoDiv) ssoDiv.style.display = '';
      }

      // Check if already logged in via /api/me
      const meRes = await fetch('/api/me', { credentials: 'same-origin' });
      if (meRes.ok) {
        const me = await meRes.json();
        currentUser = me.username;
        showDashboard();
      } else {
        showView('login');
      }
    } catch (e) {
      showView('login');
    }
  }

  init();
})();
