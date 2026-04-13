// API Base URL - assumes proxy in dev, or relative in prod if served by Go
const API_BASE = '/api/v1';

// Auth state
let sseSource = null;

async function checkAuth() {
    try {
        const response = await fetch(`${API_BASE}/auth/me`);
        if (response.ok) {
            const user = await response.json();
            showApp(user);
            return true;
        }
    } catch (e) {
        // Not authenticated
    }
    showAuth();
    return false;
}

function showApp(user) {
    document.getElementById('authSection').style.display = 'none';
    document.getElementById('appSection').style.display = '';
    document.getElementById('userEmail').textContent = user.email;
    // Load data
    fetchFeeds();
    fetchDestinationTypes();
    fetchDestinations();
    fetchDigests();
    fetchDeliveries();
    fetchWebhooks();
    fetchCredentials();
    populateArticleDigestSelect();
    initSSE();
}

function showAuth() {
    document.getElementById('authSection').style.display = '';
    document.getElementById('appSection').style.display = 'none';
    // Hide registration UI if the server has registration disabled.
    fetch(`${API_BASE}/auth/register`, { method: 'POST' }).then(r => {
        const disabled = r.status === 403;
        document.getElementById('showRegister').parentElement.style.display = disabled ? 'none' : '';
        if (disabled) {
            document.getElementById('registerForm').style.display = 'none';
            document.getElementById('loginForm').style.display = '';
        }
    }).catch(() => {});
}

function showAuthError(message) {
    const el = document.getElementById('authError');
    el.textContent = message;
    el.style.display = '';
    setTimeout(() => { el.style.display = 'none'; }, 5000);
}

// Login form
document.getElementById('loginFormEl').addEventListener('submit', async (e) => {
    e.preventDefault();
    const email = document.getElementById('loginEmail').value;
    const password = document.getElementById('loginPassword').value;
    try {
        const response = await fetch(`${API_BASE}/auth/login`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ email, password }),
        });
        if (!response.ok) {
            const text = await response.text();
            showAuthError(text || 'Login failed');
            return;
        }
        checkAuth();
    } catch (err) {
        showAuthError('Login failed: ' + err.message);
    }
});

// Register form
document.getElementById('registerFormEl').addEventListener('submit', async (e) => {
    e.preventDefault();
    const email = document.getElementById('regEmail').value;
    const password = document.getElementById('regPassword').value;
    const confirm = document.getElementById('regPasswordConfirm').value;
    if (password !== confirm) {
        showAuthError('Passwords do not match');
        return;
    }
    try {
        const response = await fetch(`${API_BASE}/auth/register`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ email, password }),
        });
        if (!response.ok) {
            const text = await response.text();
            showAuthError(text || 'Registration failed');
            return;
        }
        checkAuth();
    } catch (err) {
        showAuthError('Registration failed: ' + err.message);
    }
});

// Toggle login/register
document.getElementById('showRegister').addEventListener('click', (e) => {
    e.preventDefault();
    document.getElementById('loginForm').style.display = 'none';
    document.getElementById('registerForm').style.display = '';
});
document.getElementById('showLogin').addEventListener('click', (e) => {
    e.preventDefault();
    document.getElementById('registerForm').style.display = 'none';
    document.getElementById('loginForm').style.display = '';
});

// Intercept 401 responses to redirect to login
const originalFetch = window.fetch;
window.fetch = async function(...args) {
    const response = await originalFetch.apply(this, args);
    if (response.status === 401 && !args[0].toString().includes('/auth/')) {
        showAuth();
    }
    return response;
};

// DOM Elements
const feedList = document.getElementById('feedList');
const addFeedForm = document.getElementById('addFeedForm');
const pollBtn = document.getElementById('pollBtn');
const refreshBtn = document.getElementById('refreshBtn');
const toastContainer = document.getElementById('toastContainer');

// Destination UI Elements
const destinationList = document.getElementById('destinationList');
const addDestinationForm = document.getElementById('addDestinationForm');
const destTypeSelect = document.getElementById('destType');
const destFieldsContainer = document.getElementById('destFields');

// --- API Calls ---

// Fetch available destination types and render tabs dynamically.
async function fetchDestinationTypes() {
    try {
        const response = await fetch(`${API_BASE}/destination-types`);
        if (!response.ok) throw new Error('Failed to fetch destination types');
        const types = await response.json();
        const tabsContainer = document.getElementById('destTypeTabs');
        if (!tabsContainer || !types || types.length === 0) return;

        const labels = {
            remarkable: 'ReMarkable', file: 'File', email: 'Email',
            gmail: 'Gmail', gcp: 'GCP', dropbox: 'Dropbox', notion: 'Notion',
        };
        tabsContainer.innerHTML = types.map(t =>
            `<li><a href="#" role="button" class="secondary outline" onclick="switchDestTab('${t}', this)">${labels[t] || t}</a></li>`
        ).join('');

        // Select first tab
        const firstTab = tabsContainer.querySelector('a');
        if (firstTab) switchDestTab(types[0], firstTab);
    } catch (err) {
        console.warn('Could not fetch destination types:', err);
    }
}

async function fetchDestinations() {
    try {
        const response = await fetch(`${API_BASE}/destinations`);
        if (!response.ok) throw new Error('Failed to fetch destinations');
        const data = await response.json();
        window.allDestinations = data;
        renderDestinations(data);
        updateDigestDestinationSelect(data);
    } catch (err) {
        console.warn("Could not fetch destinations:", err);
        destinationList.innerHTML = `<tr><td colspan="4">Error loading destinations</td></tr>`;
    }
}

async function addDestination(destData) {
    try {
        const response = await fetch(`${API_BASE}/destinations`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(destData)
        });
        if (!response.ok) {
            const txt = await response.text();
            throw new Error(txt || 'Failed to add destination');
        }
        showToast('Destination added successfully');
        addDestinationForm.reset();
        updateDestFormFields(); // Reset dynamic fields
        fetchDestinations();
    } catch (err) {
        showToast(`Error adding destination: ${err.message}`, 'error');
    }
}

async function removeDestination(id) {
    if (!confirm('Are you sure you want to remove this destination?')) return;
    try {
        const response = await fetch(`${API_BASE}/destinations/${id}`, {
            method: 'DELETE'
        });
        if (!response.ok) throw new Error('Failed to remove destination');
        showToast('Destination removed');
        fetchDestinations();
    } catch (err) {
        showToast(`Error removing destination: ${err.message}`, 'error');
    }
}

async function setDefaultDestination(id) {
    try {
        const response = await fetch(`${API_BASE}/destinations/${id}/default`, {
            method: 'PUT'
        });
        if (!response.ok) throw new Error('Failed to set default');
        showToast('Default destination updated');
        fetchDestinations();
    } catch (err) {
        showToast(`Error setting default: ${err.message}`, 'error');
    }
}

async function testDestination(id) {
    try {
        showToast('Testing connection...', 'info');
        const response = await fetch(`${API_BASE}/destinations/${id}/test`, {
            method: 'POST'
        });
        if (!response.ok) {
            const txt = await response.text();
            throw new Error(txt || 'Connection test failed');
        }
        const data = await response.json();
        showToast(`Success: ${data.message}`, 'info'); // Using 'info' usually implies green or neutral, maybe add 'success' type if styles support it
    } catch (err) {
        showToast(`Test failed: ${err.message}`, 'error');
    }
}

async function authorizeOAuthDestination(id) {
    try {
        showToast('Getting authorization URL...', 'info');
        const response = await fetch(`${API_BASE}/destinations/${id}/auth-url`);
        if (!response.ok) {
            const txt = await response.text();
            throw new Error(txt || 'Failed to get auth URL');
        }
        const data = await response.json();
        
        // Open in new window/tab
        const authWindow = window.open(data.auth_url, '_blank', 'width=600,height=700');
        
        // Listen for OAuth completion message from popup
        window.addEventListener('message', function handler(event) {
            if (event.data && event.data.type === 'oauth_complete') {
                window.removeEventListener('message', handler);
                if (event.data.success) {
                    showToast('Authorization successful!', 'info');
                    fetchDestinations(); // Refresh to update status
                }
            }
        });
        
        showToast('Complete authorization in the new window', 'info');
    } catch (err) {
        showToast(`Authorization failed: ${err.message}`, 'error');
    }
}

async function fetchFeeds() {
    try {
        const response = await fetch(`${API_BASE}/feeds`);
        if (!response.ok) throw new Error('Failed to fetch feeds');
        const feeds = await response.json();
        allFeeds = feeds || [];
        renderFeeds(feeds);
    } catch (err) {
        showToast(`Error: ${err.message}`, 'error');
        feedList.innerHTML = `<tr><td colspan="6">Error loading feeds</td></tr>`;
    }
}

async function addFeed(feedData) {
    try {
        const response = await fetch(`${API_BASE}/feeds`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(feedData)
        });
        if (!response.ok) throw new Error('Failed to add feed');
        showToast('Feed added successfully');
        fetchFeeds();
        addFeedForm.reset();
        addFeedForm.closest('details').removeAttribute('open'); // Close details
    } catch (err) {
        showToast(`Error adding feed: ${err.message}`, 'error');
    }
}

async function removeFeed(id) {
    if (!confirm('Are you sure you want to remove this feed?')) return;
    try {
        const response = await fetch(`${API_BASE}/feeds/${id}`, {
            method: 'DELETE'
        });
        if (!response.ok) throw new Error('Failed to remove feed');
        showToast('Feed removed');
        fetchFeeds();
    } catch (err) {
        showToast(`Error removing feed: ${err.message}`, 'error');
    }
}

async function triggerPoll() {
    try {
        const response = await fetch(`${API_BASE}/poll`, {
            method: 'POST',
            body: JSON.stringify({}) // Poll all
        });
        if (!response.ok) throw new Error('Failed to start poll');
        showToast('Polling started...', 'info');
    } catch (err) {
        showToast(`Error starting poll: ${err.message}`, 'error');
    }
}


function renderDestinations(dests) {
    if (!dests || dests.length === 0) {
        destinationList.innerHTML = `<tr><td colspan="4">No destinations found. Add one!</td></tr>`;
        return;
    }

    // OAuth destination types that need authorization
    const oauthTypes = ['gmail', 'dropbox', 'notion'];

    destinationList.innerHTML = dests.map(d => {
        // Check if OAuth destination needs authorization
        let needsAuth = false;
        if (oauthTypes.includes(d.Type)) {
            try {
                const config = JSON.parse(d.Config);
                if (d.Type === 'notion') {
                    needsAuth = !config.access_token;
                } else {
                    needsAuth = !config.access_token || !config.refresh_token;
                }
            } catch (e) {
                needsAuth = true;
            }
        }
        
        return `
        <tr>
            <td>
                <strong>${escapeHtml(d.Name)}</strong>
                ${needsAuth ? '<br><small class="auth-warning">⚠ Needs Authorization</small>' : ''}
            </td>
            <td>${d.Type}</td>
            <td>${d.IsDefault ? '✅' : ''}</td>
            <td>
                ${!d.IsDefault ? `<a href="#" role="button" class="outline secondary btn-sm" data-set-default-dest="${d.ID}">Set Default</a>` : ''}
                ${needsAuth ? `<a href="#" role="button" class="outline btn-sm auth-btn" data-authorize-dest="${d.ID}">Authorize</a>` : ''}
                <a href="#" role="button" class="outline secondary btn-sm" data-edit-dest="${d.ID}">Edit</a>
                <a href="#" role="button" class="outline contrast btn-sm" data-test-dest="${d.ID}">Test</a>
                <a href="#" role="button" class="outline contrast btn-sm" data-remove-dest="${d.ID}">Remove</a>
            </td>
        </tr>
    `}).join('');
}


// UI Rendering for Feed List
function renderFeeds(feeds) {
    if (!feeds || feeds.length === 0) {
        feedList.innerHTML = `<tr><td colspan="6">No feeds found. Add one above!</td></tr>`;
        return;
    }

    feedList.innerHTML = feeds.map(feed => {
        let delivery;
        if (feed.digest_names && feed.digest_names.length > 0) {
            const digestStr = feed.digest_names.map(n => escapeHtml(n)).join(', ');
            if (feed.deliver_individually) {
                delivery = `<small>${digestStr} + Individual</small>`;
            } else {
                delivery = `<small>${digestStr}</small>`;
            }
        } else if (feed.deliver_individually) {
            delivery = '<small>Individual</small>';
        } else {
            delivery = '<small>None</small>';
        }

        return `
        <tr>
            <td>${feed.id}</td>
            <td><strong>${escapeHtml(feed.name)}</strong></td>
            <td><small><a href="${escapeHtml(feed.url)}" target="_blank" class="secondary">${escapeHtml(feed.url)}</a></small></td>
            <td>${delivery}</td>
            <td>${formatDate(feed.last_polled)}</td>
            <td>
                <a href="#" role="button" class="outline secondary btn-sm" data-edit-feed="${feed.id}">Edit</a>
                <a href="#" role="button" class="outline contrast btn-sm" data-remove-feed="${feed.id}">Remove</a>
            </td>
        </tr>`;
    }).join('');
}

// --- Deliveries ---

const deliveryList = document.getElementById('deliveryList');

async function fetchDeliveries() {
    try {
        const response = await fetch(`${API_BASE}/deliveries`);
        if (!response.ok) throw new Error('Failed to fetch deliveries');
        const deliveries = await response.json();
        renderDeliveries(deliveries);
    } catch (err) {
        console.warn('Could not fetch deliveries:', err);
        deliveryList.innerHTML = `<tr><td colspan="5">Error loading deliveries</td></tr>`;
    }
}

function renderDeliveries(deliveries) {
    if (!deliveries || deliveries.length === 0) {
        deliveryList.innerHTML = `<tr><td colspan="5">No deliveries yet</td></tr>`;
        return;
    }

    deliveryList.innerHTML = deliveries.map(d => {
        const typeLabel = d.delivery_type === 'digest' ? '📰' : '📄';
        const title = d.url
            ? `<a href="${escapeHtml(d.url)}" target="_blank" class="secondary">${escapeHtml(d.title)}</a>`
            : escapeHtml(d.title);
        const feed = d.feed_name || '—';
        const dest = d.dest_name ? `${escapeHtml(d.dest_name)} (${d.dest_type})` : '—';

        return `
        <tr>
            <td>${typeLabel}</td>
            <td>${title || '—'}</td>
            <td><small>${escapeHtml(feed)}</small></td>
            <td><small>${dest}</small></td>
            <td><small>${formatDate(d.delivered_at)}</small></td>
        </tr>`;
    }).join('');
}

// --- Send Article ---

document.getElementById('sendArticleForm').addEventListener('submit', async (e) => {
    e.preventDefault();
    const title = document.getElementById('articleTitle').value;
    const url = document.getElementById('articleUrl').value;
    const content = document.getElementById('articleContent').value;
    const directory = document.getElementById('articleDirectory').value;
    const digestId = document.getElementById('articleDigest').value;

    if (!url && !content) {
        showToast('URL or content is required', 'error');
        return;
    }

    const body = {};
    if (title) body.title = title;
    if (url) body.url = url;
    if (content) body.content = content;
    if (directory) body.directory = directory;
    if (digestId) body.digest_id = digestId;

    const btn = e.target.querySelector('button[type="submit"]');
    btn.setAttribute('aria-busy', 'true');
    btn.textContent = digestId ? 'Queuing...' : 'Sending...';

    try {
        const response = await fetch(`${API_BASE}/articles`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(body),
        });
        if (!response.ok) throw new Error(await response.text());
        const result = await response.json();
        showToast(result.status === 'queued' ? `Queued: ${result.title}` : `Delivered: ${result.title}`);
        document.getElementById('sendArticleForm').reset();
        if (digestId) fetchDigests();
        else fetchDeliveries();
    } catch (err) {
        showToast(`Error: ${err.message}`, 'error');
    } finally {
        btn.removeAttribute('aria-busy');
        btn.textContent = 'Send';
    }
});

function populateArticleDigestSelect() {
    const select = document.getElementById('articleDigest');
    try {
        fetch(`${API_BASE}/digests`).then(r => r.json()).then(digests => {
            const options = (digests || []).map(d => `<option value="${d.ID}">${escapeHtml(d.Name)}</option>`).join('');
            select.innerHTML = `<option value="">Deliver immediately</option>${options}`;
        });
    } catch (e) { /* ignore */ }
}

// --- Webhooks ---

const webhookList = document.getElementById('webhookList');

async function fetchWebhooks() {
    try {
        const response = await fetch(`${API_BASE}/webhooks`);
        if (!response.ok) throw new Error('Failed to fetch webhooks');
        const webhooks = await response.json();
        renderWebhooks(webhooks);
        populateWebhookDigestSelect();
    } catch (err) {
        console.warn('Could not fetch webhooks:', err);
        webhookList.innerHTML = `<tr><td colspan="5">Error loading webhooks</td></tr>`;
    }
}

function renderWebhooks(webhooks) {
    if (!webhooks || webhooks.length === 0) {
        webhookList.innerHTML = `<tr><td colspan="4">No webhooks configured</td></tr>`;
        return;
    }

    const baseUrl = window.location.origin;
    webhookList.innerHTML = webhooks.map(w => {
        let configSummary = '—';
        try {
            const cfg = JSON.parse(w.Config || '{}');
            const parts = [];
            if (cfg.digest_id) parts.push(`digest: ${cfg.digest_id.substring(0, 8)}…`);
            if (cfg.directory) parts.push(`dir: ${cfg.directory}`);
            configSummary = parts.length ? parts.join(', ') : 'default destination';
        } catch (e) {
            configSummary = 'default destination';
        }

        return `
        <tr>
            <td>${escapeHtml(w.Type)}</td>
            <td><small><code>${baseUrl}/api/v1/webhook/miniflux</code></small></td>
            <td><small>${configSummary}</small></td>
            <td><button class="btn-sm outline secondary" onclick="removeWebhook('${w.ID}')">Remove</button></td>
        </tr>`;
    }).join('');
}

async function populateWebhookDigestSelect() {
    const select = document.getElementById('webhookDigest');
    try {
        const response = await fetch(`${API_BASE}/digests`);
        if (!response.ok) return;
        const digests = await response.json();
        const options = digests.map(d => `<option value="${d.ID}">${escapeHtml(d.Name)}</option>`).join('');
        select.innerHTML = `<option value="">Deliver immediately</option>${options}`;
    } catch (e) { /* ignore */ }
}

document.getElementById('addWebhookForm').addEventListener('submit', async (e) => {
    e.preventDefault();
    const type = document.getElementById('webhookType').value;
    const secret = document.getElementById('webhookSecret').value;
    const digestId = document.getElementById('webhookDigest').value;
    const directory = document.getElementById('webhookDirectory').value;

    const config = {};
    if (digestId) config.digest_id = digestId;
    if (directory) config.directory = directory;

    try {
        const response = await fetch(`${API_BASE}/webhooks`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ type, secret, config: JSON.stringify(config) }),
        });
        if (!response.ok) throw new Error(await response.text());
        showToast('Webhook created');
        document.getElementById('addWebhookForm').reset();
        fetchWebhooks();
    } catch (err) {
        showToast(`Error: ${err.message}`, 'error');
    }
});

document.getElementById('addCredentialForm').addEventListener('submit', (e) => {
    e.preventDefault();
    addCredential();
});

async function removeWebhook(id) {
    try {
        await fetch(`${API_BASE}/webhooks/${id}`, { method: 'DELETE' });
        showToast('Webhook removed');
        fetchWebhooks();
    } catch (err) {
        showToast(`Error: ${err.message}`, 'error');
    }
}

// --- Credentials ---

const credentialList = document.getElementById('credentialList');
let cachedCredentials = [];

async function fetchCredentials() {
    try {
        const response = await fetch(`${API_BASE}/credentials`);
        if (!response.ok) throw new Error('Failed to fetch credentials');
        const credentials = await response.json();
        cachedCredentials = credentials || [];
        renderCredentials(cachedCredentials);
        populateCredentialSelects();
    } catch (err) {
        console.warn('Could not fetch credentials:', err);
        credentialList.innerHTML = `<tr><td colspan="4">Error loading credentials</td></tr>`;
    }
}

function renderCredentials(creds) {
    if (!creds || creds.length === 0) {
        credentialList.innerHTML = `<tr><td colspan="4">No credentials configured</td></tr>`;
        return;
    }

    credentialList.innerHTML = creds.map(c => {
        const typeLabels = { substack_cookie: 'Substack Cookie' };
        const typeLabel = typeLabels[c.type] || c.type;
        const updatedAt = formatCredentialAge(c.updated_at);

        return `
        <tr>
            <td>${escapeHtml(c.name)}</td>
            <td>${escapeHtml(typeLabel)}</td>
            <td><small>${updatedAt}</small></td>
            <td><button class="btn-sm outline secondary" onclick="removeCredential('${c.id}')">Remove</button></td>
        </tr>`;
    }).join('');
}

// Show relative age with a warning for credentials older than 90 days.
function formatCredentialAge(dateStr) {
    if (!dateStr || dateStr.startsWith('0001')) return 'Unknown';
    const updated = new Date(dateStr);
    const now = new Date();
    const diffMs = now - updated;
    const diffDays = Math.floor(diffMs / (1000 * 60 * 60 * 24));

    if (diffDays === 0) return 'Today';
    if (diffDays === 1) return '1 day ago';
    const label = `${diffDays} days ago`;
    if (diffDays > 90) return `⚠️ ${label} (expired?)`;
    return label;
}

async function addCredential() {
    const name = document.getElementById('credentialName').value;
    const type = document.getElementById('credentialType').value;
    const substackSid = document.getElementById('credentialSubstackSid').value;

    if (!name || !substackSid) {
        showToast('Name and cookie value are required', 'error');
        return;
    }

    try {
        const response = await fetch(`${API_BASE}/credentials`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ name, type, config: { substack_sid: substackSid } }),
        });
        if (!response.ok) throw new Error(await response.text());
        showToast('Credential added');
        document.getElementById('addCredentialForm').reset();
        fetchCredentials();
    } catch (err) {
        showToast(`Error: ${err.message}`, 'error');
    }
}

async function removeCredential(id) {
    if (!confirm('Remove this credential?')) return;
    try {
        await fetch(`${API_BASE}/credentials/${id}`, { method: 'DELETE' });
        showToast('Credential removed');
        fetchCredentials();
    } catch (err) {
        showToast(`Error: ${err.message}`, 'error');
    }
}

// Populate credential dropdowns in add/edit feed forms.
function populateCredentialSelects() {
    const selects = [
        document.getElementById('addFeedCredential'),
        document.getElementById('editFeedCredential'),
    ];
    const options = '<option value="">None</option>' +
        (cachedCredentials || []).map(c => `<option value="${c.id}">${escapeHtml(c.name)}</option>`).join('');
    selects.forEach(sel => { if (sel) sel.innerHTML = options; });
}

function escapeHtml(text) {
    if (!text) return '';
    return text
        .replace(/&/g, "&amp;")
        .replace(/</g, "&lt;")
        .replace(/>/g, "&gt;")
        .replace(/"/g, "&quot;")
        .replace(/'/g, "&#039;");
}

function formatDate(dateStr) {
    if (!dateStr || dateStr.startsWith("0001")) return "Never";
    return new Date(dateStr).toLocaleString();
}

function showToast(message, type = 'info') {
    const toast = document.createElement('div');
    toast.className = 'toast';
    toast.textContent = message;
    if (type === 'error') toast.style.borderColor = 'var(--form-element-invalid-border-color)';
    
    toastContainer.appendChild(toast);
    
    // Auto remove
    setTimeout(() => {
        toast.style.opacity = '0';
        setTimeout(() => toast.remove(), 300);
    }, 5000);
}


// --- Dynamic Form Fields ---

function switchDestTab(type, element) {
    // 1. Update hidden input
    document.getElementById('destType').value = type;

    // 2. Update Tab Styles
    const tabs = document.querySelectorAll('#destTypeTabs a');
    tabs.forEach(t => {
        t.classList.add('outline');
        t.removeAttribute('role'); // Reset primary role style if pico uses it, but we use classes
        // Actually pico uses role="button" and classes.
        // Let's just toggle 'outline' class. Active tab should NOT have outline (solid).
    });
    
    // Reset all to outline
    tabs.forEach(t => t.classList.add('outline'));
    // Set active to solid
    if (element) {
        element.classList.remove('outline');
    }

    // 3. Update Fields
    updateDestFormFields(type);
}

function updateDestFormFields(typeOverride) {
    const type = typeOverride || document.getElementById('destType').value;
    let html = '';

    switch (type) {
        case 'remarkable':
            html = `
                <div class="grid">
                    <label>
                        Registration Code <small>(<a href="https://my.remarkable.com/pair" target="_blank">Get Code</a>)</small>
                        <input type="text" name="config_code" placeholder="Enter code from my.remarkable.com">
                    </label>
                </div>
                <details>
                    <summary><small>Or Manual Tokens</small></summary>
                    <label>User Token <input type="text" name="config_user_token"></label>
                    <label>Device Token <input type="text" name="config_device_token"></label>
                </details>
            `;
            break;
        case 'email':
            html = `
                <div class="grid">
                    <label>SMTP Server <input type="text" name="config_server" required></label>
                    <label>Port <input type="number" name="config_port" value="587" required></label>
                </div>
                <div class="grid">
                    <label>Username <input type="text" name="config_username" required></label>
                    <label>Password <input type="password" name="config_password" required></label>
                </div>
                <div class="grid">
                    <label>To Email <input type="email" name="config_to_email" required></label>
                    <label>From Email <input type="email" name="config_from_email" required></label>
                </div>
            `;
            break;
        case 'file':
            html = `
                <label>Root Path <input type="text" name="config_path" placeholder="/absolute/path/to/folder" required></label>
            `;
            break;
        case 'gcp':
            html = `
                <label>Bucket Name <input type="text" name="config_bucket" required></label>
                <label>Credentials JSON Path <input type="text" name="config_credentials" placeholder="/path/to/key.json" required></label>
            `;
            break;
        case 'gmail':
            html = `
                <p><small>You'll need OAuth credentials from <a href="https://console.cloud.google.com/apis/credentials" target="_blank">Google Cloud Console</a>.
                Create an OAuth 2.0 Client ID (Web application) and add redirect URI: <code>http://localhost:8080/api/v1/oauth/callback</code></small></p>
                <div class="grid">
                    <label>Client ID <input type="text" name="config_client_id" required></label>
                    <label>Client Secret <input type="text" name="config_client_secret" required></label>
                </div>
                <label>Recipient Email <input type="email" name="config_to_email" placeholder="your@email.com" required></label>
                <p><small>After adding, click "Authorize" to complete the OAuth flow with Google.</small></p>
            `;
            break;
        case 'dropbox':
            html = `
                <p><small>You'll need a Dropbox App from <a href="https://www.dropbox.com/developers/apps" target="_blank">Dropbox App Console</a>.
                Create an app and add redirect URI: <code>http://localhost:8080/api/v1/oauth/callback</code></small></p>
                <div class="grid">
                    <label>App Key <input type="text" name="config_app_key" required></label>
                    <label>App Secret <input type="text" name="config_app_secret" required></label>
                </div>
                <label>Folder Path <input type="text" name="config_folder_path" value="/rss2rm" placeholder="/rss2rm"></label>
                <p><small>After adding, click "Authorize" to connect your Dropbox account.</small></p>
            `;
            break;
        case 'notion':
            html = `
                <p><small>You'll need a Notion integration from <a href="https://www.notion.so/my-integrations" target="_blank">Notion Integrations</a>.
                Create a public integration and add redirect URI: <code>http://localhost:8080/api/v1/oauth/callback</code></small></p>
                <div class="grid">
                    <label>OAuth Client ID <input type="text" name="config_client_id" required></label>
                    <label>OAuth Client Secret <input type="text" name="config_client_secret" required></label>
                </div>
                <label>Parent Page ID <small>(optional - can select during authorization)</small>
                    <input type="text" name="config_parent_page_id" placeholder="Leave empty to select during auth">
                </label>
                <p><small>After adding, click "Authorize" to connect your Notion workspace and select a page.</small></p>
            `;
            break;
    }

    destFieldsContainer.innerHTML = html;
}


// --- Digest Functions ---

const digestsContainer = document.getElementById('digestsContainer');
const addDigestForm = document.getElementById('addDigestForm');
const digestDestinationSelect = document.getElementById('digestDestination');

let allFeeds = []; // Cache feeds for digest assignment
let cachedDigests = []; // Cache digests for edit lookup

async function fetchDigests() {
    try {
        const response = await fetch(`${API_BASE}/digests`);
        if (!response.ok) throw new Error('Failed to fetch digests');
        const digests = await response.json();
        cachedDigests = digests || [];
        renderDigests(digests);
        renderFeeds(allFeeds); // Re-render feeds to update delivery column
    } catch (err) {
        console.warn("Could not fetch digests:", err);
        digestsContainer.innerHTML = '<p>Error loading digests</p>';
    }
}

async function addDigest(data) {
    try {
        const response = await fetch(`${API_BASE}/digests`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(data)
        });
        if (!response.ok) {
            const txt = await response.text();
            throw new Error(txt || 'Failed to create digest');
        }
        showToast('Digest created successfully');
        addDigestForm.reset();
        document.getElementById('digestSchedule').value = '07:00';
        fetchDigests();
    } catch (err) {
        showToast(`Error creating digest: ${err.message}`, 'error');
    }
}

async function removeDigest(id) {
    if (!confirm('Remove this digest? Feeds will be detached but not deleted.')) return;
    try {
        const response = await fetch(`${API_BASE}/digests/${id}`, { method: 'DELETE' });
        if (!response.ok) throw new Error('Failed to remove digest');
        showToast('Digest removed');
        fetchDigests();
        fetchFeeds();
    } catch (err) {
        showToast(`Error: ${err.message}`, 'error');
    }
}

async function generateDigest(id) {
    try {
        showToast('Generating digest...', 'info');
        const response = await fetch(`${API_BASE}/digests/${id}/generate`, { method: 'POST' });
        if (!response.ok) {
            const txt = await response.text();
            throw new Error(txt || 'Generation failed');
        }
        showToast('Digest generated successfully');
        fetchDigests();
    } catch (err) {
        showToast(`Error: ${err.message}`, 'error');
    }
}

async function editDigest(id) {
    const d = cachedDigests.find(d => d.ID === id);
    if (!d) return;
    document.getElementById('editDigestId').value = id;
    document.getElementById('editDigestName').value = d.Name;
    document.getElementById('editDigestSchedule').value = d.Schedule;
    document.getElementById('editDigestDirectory').value = d.Directory || '';
    document.getElementById('editDigestRetain').value = d.Retain || 0;
    
    const modal = document.getElementById('editDigestModal');
    if (modal) modal.showModal();
}

async function addFeedToDigest(digestId, feedId, alsoIndividual) {
    try {
        const response = await fetch(`${API_BASE}/digests/${digestId}/feeds`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ feed_id: feedId, also_individual: alsoIndividual })
        });
        if (!response.ok) throw new Error('Failed to add feed to digest');
        showToast('Feed added to digest');
        fetchDigests();
        fetchFeeds();
    } catch (err) {
        showToast(`Error: ${err.message}`, 'error');
    }
}

async function removeFeedFromDigest(digestId, feedId) {
    try {
        const response = await fetch(`${API_BASE}/digests/${digestId}/feeds/${feedId}`, { method: 'DELETE' });
        if (!response.ok) throw new Error('Failed to remove feed from digest');
        showToast('Feed removed from digest');
        fetchDigests();
        fetchFeeds();
    } catch (err) {
        showToast(`Error: ${err.message}`, 'error');
    }
}

function renderDigests(digests) {
    if (!digests || digests.length === 0) {
        digestsContainer.innerHTML = '<p>No digests configured. Create one above!</p>';
        return;
    }

    digestsContainer.innerHTML = digests.map(d => {
        const lastGen = (!d.LastGenerated || d.LastGenerated.startsWith('0001'))
            ? 'Never' : new Date(d.LastGenerated).toLocaleString();
        const dest = d.DestinationID ? `dest:${d.DestinationID}` : 'default';

        return `
        <article class="digest-card">
            <header>
                <div class="digest-header">
                    <strong>${escapeHtml(d.Name)}</strong>
                    <small>Schedule: ${escapeHtml(d.Schedule)} · Dir: ${escapeHtml(d.Directory || d.Name)} · Dest: ${dest} · Last: ${lastGen}</small>
                </div>
            </header>
            <div id="digest-feeds-${d.ID}">Loading feeds...</div>
            <div id="digest-pending-${d.ID}"></div>
            <div class="digest-add-controls">
                <select id="digest-add-feed-${d.ID}" class="digest-add-select">
                    <option value="">Add feed...</option>
                </select>
                <label class="digest-also-label">
                    <input type="checkbox" id="digest-also-individual-${d.ID}">
                    Also deliver individually
                </label>
                <a href="#" role="button" class="outline secondary btn-sm" data-add-feed-to-digest="${d.ID}">Add</a>
            </div>
            <footer class="digest-actions">
                <a href="#" role="button" class="outline btn-sm" data-generate-digest="${d.ID}">Generate Now</a>
                <a href="#" role="button" class="outline secondary btn-sm" data-edit-digest="${d.ID}">Edit</a>
                <a href="#" role="button" class="outline contrast btn-sm" data-remove-digest="${d.ID}">Remove</a>
            </footer>
        </article>
        `;
    }).join('');

    // Load feeds for each digest
    digests.forEach(d => {
        loadDigestFeeds(d.ID);
        loadDigestPending(d.ID);
    });
}

async function loadDigestFeeds(digestId) {
    try {
        const response = await fetch(`${API_BASE}/digests/${digestId}/feeds`);
        if (!response.ok) throw new Error('Failed');
        const feeds = await response.json();
        const container = document.getElementById(`digest-feeds-${digestId}`);
        if (!feeds || feeds.length === 0) {
            container.innerHTML = '<small>No feeds assigned</small>';
        } else {
            container.innerHTML = feeds.map(f => {
                return `<span class="feed-badge">
                    ${escapeHtml(f.Name)}
                    <a href="#" data-remove-feed-from-digest="${digestId}" data-feed-id="${f.ID}">×</a>
                </span>`;
            }).join('');
        }

        // Populate the add-feed dropdown with feeds NOT already in this digest
        const digestFeedIds = new Set(feeds ? feeds.map(f => f.ID) : []);
        const select = document.getElementById(`digest-add-feed-${digestId}`);
        if (select) {
            const availableFeeds = allFeeds.filter(f => !digestFeedIds.has(f.id));
            select.innerHTML = '<option value="">Add feed...</option>' +
                availableFeeds.map(f => `<option value="${f.id}">${escapeHtml(f.name)}</option>`).join('');
        }
    } catch (err) {
        const container = document.getElementById(`digest-feeds-${digestId}`);
        if (container) container.innerHTML = '<small>Error loading feeds</small>';
    }
}

async function loadDigestPending(digestId) {
    try {
        const response = await fetch(`${API_BASE}/digests/${digestId}/pending`);
        if (!response.ok) return;
        const entries = await response.json();
        const container = document.getElementById(`digest-pending-${digestId}`);
        if (!entries || entries.length === 0) {
            container.innerHTML = '';
            return;
        }
        container.innerHTML = `<details><summary><small>${entries.length} article${entries.length === 1 ? '' : 's'} queued for next digest</small></summary>
            <ul>${entries.map(e => {
                const title = e.url
                    ? `<a href="${escapeHtml(e.url)}" target="_blank" class="secondary">${escapeHtml(e.title)}</a>`
                    : escapeHtml(e.title);
                return `<li><small>${title} · ${formatDate(e.published)}</small></li>`;
            }).join('')}</ul></details>`;
    } catch (err) { /* ignore */ }
}

function handleAddFeedToDigest(digestId) {
    const select = document.getElementById(`digest-add-feed-${digestId}`);
    const feedId = select.value;
    if (!feedId) {
        showToast('Select a feed first', 'error');
        return;
    }
    const alsoIndividual = document.getElementById(`digest-also-individual-${digestId}`).checked;
    addFeedToDigest(digestId, feedId, alsoIndividual);
}

function updateDigestDestinationSelect(dests) {
    if (!digestDestinationSelect) return;
    let html = '<option value="">System Default</option>';
    if (dests && dests.length > 0) {
        html += dests.map(d => `<option value="${d.ID}">${escapeHtml(d.Name)} (${d.Type})</option>`).join('');
    }
    digestDestinationSelect.innerHTML = html;
}

// --- Event Listeners & Init ---

function editFeed(id, name, directory, deliverIndividually, retain, credentialId) {
    document.getElementById('editFeedId').value = id;
    document.getElementById('editFeedName').value = name || '';
    document.getElementById('editFeedDirectory').value = directory || '';
    document.getElementById('editFeedIndividual').checked = !!deliverIndividually;
    document.getElementById('editFeedRetain').value = retain || 0;
    document.getElementById('editFeedCredential').value = credentialId || '';
    
    const dirGroup = document.getElementById('editFeedDirectoryGroup');
    dirGroup.style.display = deliverIndividually ? '' : 'none';
    
    document.getElementById('editFeedIndividual').onchange = function() {
        dirGroup.style.display = this.checked ? '' : 'none';
    };
    
    document.getElementById('editFeedModal').showModal();
}

function editDestination(id) {
    const dest = (window.allDestinations || []).find(d => d.ID === id);
    if (!dest) return;
    document.getElementById('editDestId').value = id;
    document.getElementById('editDestName').value = dest.Name;
    document.getElementById('editDestinationModal').showModal();
}

// Event delegation for feed table
feedList.addEventListener('click', (e) => {
    const editBtn = e.target.closest('[data-edit-feed]');
    if (editBtn) {
        e.preventDefault();
        const id = editBtn.dataset.editFeed;
        const feed = allFeeds.find(f => f.id === id);
        if (feed) editFeed(id, feed.name, feed.directory || '', !!feed.deliver_individually, feed.retain || 0, feed.credential_id || '');
    }
    const removeBtn = e.target.closest('[data-remove-feed]');
    if (removeBtn) {
        e.preventDefault();
        removeFeed(removeBtn.dataset.removeFeed);
    }
});

// Event delegation for destination table
destinationList.addEventListener('click', (e) => {
    const btn = e.target.closest('[data-set-default-dest]');
    if (btn) { e.preventDefault(); setDefaultDestination(btn.dataset.setDefaultDest); }
    const authBtn = e.target.closest('[data-authorize-dest]');
    if (authBtn) { e.preventDefault(); authorizeOAuthDestination(authBtn.dataset.authorizeDest); }
    const editBtn = e.target.closest('[data-edit-dest]');
    if (editBtn) { e.preventDefault(); editDestination(editBtn.dataset.editDest); }
    const testBtn = e.target.closest('[data-test-dest]');
    if (testBtn) { e.preventDefault(); testDestination(testBtn.dataset.testDest); }
    const removeBtn = e.target.closest('[data-remove-dest]');
    if (removeBtn) { e.preventDefault(); removeDestination(removeBtn.dataset.removeDest); }
});

// Event delegation for digests container
digestsContainer.addEventListener('click', (e) => {
    const genBtn = e.target.closest('[data-generate-digest]');
    if (genBtn) { e.preventDefault(); generateDigest(genBtn.dataset.generateDigest); }
    const editBtn = e.target.closest('[data-edit-digest]');
    if (editBtn) { e.preventDefault(); editDigest(editBtn.dataset.editDigest); }
    const removeBtn = e.target.closest('[data-remove-digest]');
    if (removeBtn) { e.preventDefault(); removeDigest(removeBtn.dataset.removeDigest); }
    const addBtn = e.target.closest('[data-add-feed-to-digest]');
    if (addBtn) { e.preventDefault(); handleAddFeedToDigest(addBtn.dataset.addFeedToDigest); }
    const removeFeedBtn = e.target.closest('[data-remove-feed-from-digest]');
    if (removeFeedBtn) {
        e.preventDefault();
        removeFeedFromDigest(
            removeFeedBtn.dataset.removeFeedFromDigest,
            removeFeedBtn.dataset.feedId
        );
    }
});

document.addEventListener('DOMContentLoaded', () => {
    // Destination type tabs are populated dynamically in fetchDestinationTypes()

    // Edit Feed Form
    const feedForm = document.getElementById('editFeedForm');
    if (feedForm) {
        feedForm.addEventListener('submit', async (e) => {
            e.preventDefault();
            const id = document.getElementById('editFeedId').value;
            const newName = document.getElementById('editFeedName').value;
            const newDirectory = document.getElementById('editFeedDirectory').value;
            const deliverIndividually = document.getElementById('editFeedIndividual').checked;
            const retain = parseInt(document.getElementById('editFeedRetain').value) || 0;
            const credentialId = document.getElementById('editFeedCredential').value;
            
            const body = { 
                name: newName,
                directory: newDirectory,
                deliver_individually: deliverIndividually,
                retain: retain
            };
            if (credentialId) body.credential_id = credentialId;
            else body.credential_id = '';

            try {
                const response = await fetch(`${API_BASE}/feeds/${id}`, {
                    method: 'PUT',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify(body)
                });
                if (!response.ok) {
                    const txt = await response.text();
                    throw new Error(txt || 'Failed to update feed');
                }
                showToast('Feed updated');
                document.getElementById('editFeedModal').close();
                fetchFeeds();
            } catch (err) {
                showToast(`Error: ${err.message}`, 'error');
            }
        });
    }

    // Edit Digest Form
    const digestForm = document.getElementById('editDigestForm');
    if (digestForm) {
        digestForm.addEventListener('submit', async (e) => {
            e.preventDefault();
            const id = document.getElementById('editDigestId').value;
            const newName = document.getElementById('editDigestName').value;
            const newSchedule = document.getElementById('editDigestSchedule').value;
            const newDirectory = document.getElementById('editDigestDirectory').value;
            const retain = parseInt(document.getElementById('editDigestRetain').value) || 0;
            
            try {
                const response = await fetch(`${API_BASE}/digests/${id}`, {
                    method: 'PUT',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ 
                        name: newName, 
                        schedule: newSchedule,
                        directory: newDirectory,
                        retain: retain
                    })
                });
                if (!response.ok) throw new Error('Failed to update digest');
                showToast('Digest updated');
                document.getElementById('editDigestModal').close();
                fetchDigests();
            } catch (err) {
                showToast(`Error: ${err.message}`, 'error');
            }
        });
    }

    // Edit Destination Form
    const destForm = document.getElementById('editDestinationForm');
    if (destForm) {
        destForm.addEventListener('submit', async (e) => {
            e.preventDefault();
            const id = document.getElementById('editDestId').value;
            const name = document.getElementById('editDestName').value;
            
            try {
                const res = await fetch(`${API_BASE}/destinations/${id}`, {
                    method: 'PUT',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ name: name })
                });
                if (!res.ok) {
                    const txt = await res.text();
                    throw new Error(txt || 'Failed to update destination');
                }
                showToast('Destination updated');
                document.getElementById('editDestinationModal').close();
                fetchDestinations();
            } catch (err) {
                showToast(`Error: ${err.message}`, 'error');
            }
        });
    }
});

addFeedForm.addEventListener('submit', (e) => {
    e.preventDefault();
    const formData = new FormData(addFeedForm);
    const credentialId = document.getElementById('addFeedCredential').value;
    const data = {
        url: formData.get('url'),
        name: formData.get('name'),
        directory: formData.get('directory'),
        backfill: parseInt(formData.get('backfill')) || 5,
        deliver_individually: document.getElementById('addFeedIndividual').checked
    };
    if (credentialId) data.credential_id = credentialId;
    addFeed(data);
});

addDestinationForm.addEventListener('submit', (e) => {
    e.preventDefault();
    const formData = new FormData(addDestinationForm);
    const config = {};
    
    // Extract config fields (prefixed with config_)
    for (const [key, value] of formData.entries()) {
        if (key.startsWith('config_')) {
            config[key.replace('config_', '')] = value;
        }
    }
    
    const data = {
        name: formData.get('name'),
        type: formData.get('type'),
        is_default: formData.get('is_default') === 'on',
        config: config
    };
    addDestination(data);
});

addDigestForm.addEventListener('submit', (e) => {
    e.preventDefault();
    const formData = new FormData(addDigestForm);
    const destIdVal = formData.get('destination_id');
    const data = {
        name: formData.get('name'),
        schedule: formData.get('schedule'),
        directory: formData.get('directory'),
        destination_id: destIdVal || null,
        retain: parseInt(formData.get('retain')) || 0
    };
    addDigest(data);
});

pollBtn.addEventListener('click', (e) => {
    e.preventDefault();
    triggerPoll();
});

document.getElementById('logoutBtn').addEventListener('click', async (e) => {
    e.preventDefault();
    if (sseSource) sseSource.close();
    await fetch(`${API_BASE}/auth/logout`, { method: 'POST' });
    showAuth();
});

document.getElementById('changePasswordBtn').addEventListener('click', (e) => {
    e.preventDefault();
    document.getElementById('changePasswordModal').showModal();
});

document.getElementById('changePasswordForm').addEventListener('submit', async (e) => {
    e.preventDefault();
    const currentPass = document.getElementById('currentPassword').value;
    const newPass = document.getElementById('newPassword').value;
    const confirmPass = document.getElementById('confirmNewPassword').value;
    if (newPass !== confirmPass) {
        showToast('New passwords do not match', 'error');
        return;
    }
    try {
        const response = await fetch(`${API_BASE}/auth/change-password`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ current_password: currentPass, new_password: newPass }),
        });
        if (!response.ok) {
            const text = await response.text();
            showToast(text || 'Failed to change password', 'error');
            return;
        }
        showToast('Password changed successfully');
        document.getElementById('changePasswordModal').close();
        document.getElementById('changePasswordForm').reset();
    } catch (err) {
        showToast('Error: ' + err.message, 'error');
    }
});

refreshBtn.addEventListener('click', (e) => {
    e.preventDefault();
    fetchFeeds();
    fetchDigests();
    fetchDeliveries();
});

// SSE Listener
function initSSE() {
    if (sseSource) sseSource.close();
    sseSource = new EventSource(`${API_BASE}/poll/events`);

    sseSource.onmessage = function(event) {
        try {
            const data = JSON.parse(event.data);
            
            if (data.Type === 'ITEM_UPLOADED') {
                showToast(`Uploaded: ${data.ItemTitle}`);
            } else if (data.Type === 'ERROR') {
                showToast(`Error: ${data.Message}`, 'error');
            } else if (data.Type === 'FINISH') {
                showToast(`Poll Finished: ${data.Message}`);
                fetchFeeds();
                fetchDeliveries();
            }
        } catch (e) {
            console.error("SSE Parse Error", e);
        }
    };

    sseSource.onerror = function(err) {
        console.error("SSE Error", err);
    };
}

// Expose functions to window for HTML onclick attributes
window.switchDestTab = switchDestTab;
window.updateDestFormFields = updateDestFormFields;
window.removeWebhook = removeWebhook;
window.removeCredential = removeCredential;

// Init — check auth first, then load data
checkAuth();
