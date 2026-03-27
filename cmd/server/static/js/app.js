(function() {
  'use strict';

  let currentUser = null;
  let servers = [];
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
    if (name === 'dashboard') {
      refreshTimer = setInterval(loadServers, 30000);
    }
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
  document.getElementById('logout-btn').addEventListener('click', async () => {
    await api('POST', '/api/auth/logout');
    currentUser = null;
    showView('login');
  });

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

    try {
      const res = await api('POST', '/api/servers', { name, host, port, sshUser });
      if (!res.ok) throw new Error('Failed to create server');
      const srv = await res.json();

      // Upload key
      await fetch('/api/servers/' + srv.ID + '/key', {
        method: 'PUT',
        credentials: 'same-origin',
        headers: { 'X-CSRF-Token': getCookie('csrf') },
        body: key,
      });

      document.getElementById('add-server-form').reset();
      document.getElementById('add-server-section').style.display = 'none';
      loadServers();
    } catch (e) { alert('Error: ' + e.message); }
  });

  // --- Dashboard ---
  async function showDashboard() {
    showView('dashboard');
    document.getElementById('current-user').textContent = currentUser || '';
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
            <select id="template-${srv.ID}">
              <option value="claude">Claude Code</option>
              <option value="claude-resume">Claude Code (resume)</option>
              <option value="shell">Shell</option>
            </select>
            <input type="text" id="label-${srv.ID}" placeholder="Session label" value="claude">
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

  async function loadSessions(serverID) {
    const container = document.getElementById('sessions-' + serverID);
    try {
      const res = await api('GET', '/api/servers/' + serverID + '/sessions');
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

  window.createSession = async function(serverID) {
    const template = document.getElementById('template-' + serverID).value;
    const label = document.getElementById('label-' + serverID).value || 'session';
    const commands = { 'claude': 'claude', 'claude-resume': 'claude --resume', 'shell': 'bash' };
    try {
      await api('POST', '/api/servers/' + serverID + '/sessions', {
        label: label,
        command: commands[template] || 'bash',
        workdir: '~/'
      });
      await loadSessions(serverID);
    } catch (e) { alert('Failed to create session'); }
  };

  window.killSession = async function(serverID, name) {
    if (!confirm('Kill session ' + name + '?')) return;
    try {
      await api('DELETE', '/api/servers/' + serverID + '/sessions/' + name);
      await loadSessions(serverID);
    } catch (e) { alert('Failed to kill session'); }
  };

  window.copySSH = function(cmd) {
    navigator.clipboard.writeText(cmd).then(() => {
      // Brief visual feedback would be nice but keeping it simple
    });
  };

  window.openTerminal = function(serverID, sessionName) {
    window.open('/static/terminal.html?server=' + serverID + '&session=' + sessionName, '_blank');
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
    } catch (e) {
      adminSection.style.display = 'none';
    }
  }

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

  // --- Init ---
  async function init() {
    try {
      const setupRes = await fetch('/api/auth/setup/status');
      const setupData = await setupRes.json();
      if (setupData.needed) {
        showView('setup');
        return;
      }

      // Try to access a protected route to check if already logged in
      const res = await fetch('/api/servers', { credentials: 'same-origin' });
      if (res.ok) {
        // Fetch current user info from session cookie — read username from cookie or session
        // We don't have a /api/me endpoint, so check if we can parse the session
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
