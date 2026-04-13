package api

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"rss2rm/internal/db"
)

// NewAdminServer creates an HTTP handler for the admin API.
// If token is non-empty, all API endpoints require an
// Authorization: Bearer header matching the token.
func NewAdminServer(database *db.DB, token string) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /admin/", adminIndex(token != ""))
	mux.HandleFunc("GET /admin/users", adminListUsers(database))
	mux.HandleFunc("POST /admin/users", adminCreateUser(database))
	mux.HandleFunc("DELETE /admin/users/{id}", adminDeleteUser(database))
	mux.HandleFunc("POST /admin/users/{id}/verify", adminVerifyUser(database))

	// Wrap with token auth (if configured) and request logging
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		defer func() {
			slog.Info("request", "component", "admin", "method", r.Method, "path", r.URL.Path, "duration", time.Since(start).Round(time.Millisecond))
		}()

		// Allow the admin page itself without auth
		if token != "" && r.URL.Path != "/admin/" {
			auth := r.Header.Get("Authorization")
			if auth == "" || !strings.HasPrefix(auth, "Bearer ") || strings.TrimPrefix(auth, "Bearer ") != token {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}

		w.Header().Set("Content-Type", "application/json")
		mux.ServeHTTP(w, r)
	})
}

// adminIndex returns a handler that serves the admin page with
// AUTH_REQUIRED injected based on whether a token is configured.
func adminIndex(authRequired bool) http.HandlerFunc {
	authRequiredJS := "false"
	tokenDisplay := "none"
	adminDisplay := ""
	if authRequired {
		authRequiredJS = "true"
		tokenDisplay = ""
		adminDisplay = "none"
	}
	page := adminPageHTML
	page = strings.Replace(page, "%%AUTH_REQUIRED%%", authRequiredJS, 1)
	page = strings.Replace(page, "%%TOKEN_DISPLAY%%", tokenDisplay, 1)
	page = strings.Replace(page, "%%ADMIN_DISPLAY%%", adminDisplay, 1)
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, page)
	}
}

const adminPageHTML = `<!DOCTYPE html>
<html lang="en" data-theme="dark">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>rss2rm Admin</title>
<link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/@picocss/pico@1.5.13/css/pico.min.css">
<style>
body { padding-top: 2rem; }
.btn-sm { padding: 2px 8px; font-size: 0.8em; }
#tokenSection { max-width: 500px; margin: 4rem auto; }
.status-msg { margin-top: 1rem; }
</style>
</head>
<body>

<section id="tokenSection" class="container" style="display:%%TOKEN_DISPLAY%%;">
    <h2>rss2rm Admin</h2>
    <form id="tokenForm">
        <label for="adminToken">Admin Token
            <input type="password" id="adminToken" placeholder="Enter admin token" required>
        </label>
        <button type="submit">Connect</button>
    </form>
    <div id="tokenError" style="display:none; color:var(--form-element-invalid-border-color); margin-top:1rem;"></div>
</section>

<main id="adminSection" class="container" style="display:%%ADMIN_DISPLAY%%">
    <nav>
        <ul><li><strong>rss2rm Admin</strong></li></ul>
        <ul><li><a href="#" id="disconnectBtn">Disconnect</a></li></ul>
    </nav>

    <section>
        <h3>Create User</h3>
        <form id="createUserForm">
            <div class="grid">
                <label>Email <input type="email" id="newEmail" required></label>
                <label>Password <input type="password" id="newPassword" minlength="8" required></label>
            </div>
            <button type="submit">Create User</button>
        </form>
        <p id="createStatus" class="status-msg" style="display:none;"></p>
    </section>

    <section>
        <h3>Users</h3>
        <table role="grid">
            <thead>
                <tr>
                    <th>Email</th>
                    <th>Verified</th>
                    <th>Created</th>
                    <th>Actions</th>
                </tr>
            </thead>
            <tbody id="userList">
                <tr><td colspan="4" aria-busy="true">Loading...</td></tr>
            </tbody>
        </table>
    </section>
</main>

<script>
const API = '/admin';
const AUTH_REQUIRED = %%AUTH_REQUIRED%%;
let token = '';

function authHeaders() {
    return { 'Authorization': 'Bearer ' + token, 'Content-Type': 'application/json' };
}

function showError(msg) {
    const el = document.getElementById('tokenError');
    el.textContent = msg;
    el.style.display = '';
    setTimeout(() => { el.style.display = 'none'; }, 5000);
}

function showStatus(msg) {
    const el = document.getElementById('createStatus');
    el.textContent = msg;
    el.style.display = '';
    setTimeout(() => { el.style.display = 'none'; }, 5000);
}

document.getElementById('tokenForm').addEventListener('submit', async (e) => {
    e.preventDefault();
    token = document.getElementById('adminToken').value;
    try {
        const resp = await fetch(API + '/users', { headers: authHeaders() });
        if (resp.status === 401) {
            showError('Invalid token');
            token = '';
            return;
        }
        if (!resp.ok) throw new Error('Server error');
        sessionStorage.setItem('adminToken', token);
        showAdmin();
    } catch (err) {
        showError('Connection failed: ' + err.message);
        token = '';
    }
});

document.getElementById('disconnectBtn').addEventListener('click', (e) => {
    e.preventDefault();
    token = '';
    sessionStorage.removeItem('adminToken');
    document.getElementById('tokenSection').style.display = '';
    document.getElementById('adminSection').style.display = 'none';
});

function showAdmin() {
    document.getElementById('tokenSection').style.display = 'none';
    document.getElementById('adminSection').style.display = '';
    fetchUsers();
}

async function fetchUsers() {
    try {
        const resp = await fetch(API + '/users', { headers: authHeaders() });
        if (!resp.ok) throw new Error('Failed to fetch users');
        const users = await resp.json();
        renderUsers(users);
    } catch (err) {
        document.getElementById('userList').innerHTML =
            '<tr><td colspan="4">Error: ' + err.message + '</td></tr>';
    }
}

function renderUsers(users) {
    const tbody = document.getElementById('userList');
    if (!users || users.length === 0) {
        tbody.innerHTML = '<tr><td colspan="4">No users</td></tr>';
        return;
    }
    tbody.innerHTML = users.map(u => {
        const created = new Date(u.created_at).toLocaleString();
        const verified = u.verified ? '✅' : '❌';
        return '<tr>' +
            '<td>' + escapeHtml(u.email) + '</td>' +
            '<td>' + verified + '</td>' +
            '<td><small>' + created + '</small></td>' +
            '<td>' +
                (!u.verified ? '<a href="#" role="button" class="outline secondary btn-sm" data-verify="' + u.id + '">Verify</a> ' : '') +
                '<a href="#" role="button" class="outline contrast btn-sm" data-delete="' + u.id + '">Delete</a>' +
            '</td>' +
        '</tr>';
    }).join('');
}

document.getElementById('userList').addEventListener('click', async (e) => {
    const verifyBtn = e.target.closest('[data-verify]');
    if (verifyBtn) {
        e.preventDefault();
        const id = verifyBtn.dataset.verify;
        await fetch(API + '/users/' + id + '/verify', { method: 'POST', headers: authHeaders() });
        fetchUsers();
    }
    const deleteBtn = e.target.closest('[data-delete]');
    if (deleteBtn) {
        e.preventDefault();
        if (!confirm('Delete this user and all their data?')) return;
        const id = deleteBtn.dataset.delete;
        await fetch(API + '/users/' + id, { method: 'DELETE', headers: authHeaders() });
        fetchUsers();
    }
});

document.getElementById('createUserForm').addEventListener('submit', async (e) => {
    e.preventDefault();
    const email = document.getElementById('newEmail').value;
    const password = document.getElementById('newPassword').value;
    try {
        const resp = await fetch(API + '/users', {
            method: 'POST',
            headers: authHeaders(),
            body: JSON.stringify({ email, password })
        });
        if (!resp.ok) {
            const txt = await resp.text();
            throw new Error(txt || 'Failed to create user');
        }
        showStatus('User created: ' + email);
        document.getElementById('createUserForm').reset();
        fetchUsers();
    } catch (err) {
        showStatus('Error: ' + err.message);
    }
});

function escapeHtml(text) {
    if (!text) return '';
    const d = document.createElement('div');
    d.textContent = text;
    return d.innerHTML;
}

// Init
if (!AUTH_REQUIRED) {
    fetchUsers();
} else {
    const saved = sessionStorage.getItem('adminToken');
    if (saved) {
        token = saved;
        document.getElementById('adminToken').value = saved;
        showAdmin();
    }
}
</script>
</body>
</html>`

func adminListUsers(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		users, err := database.GetAllUsers(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		// Return users without password hashes
		type userResponse struct {
			ID        string    `json:"id"`
			Email     string    `json:"email"`
			Verified  bool      `json:"verified"`
			CreatedAt time.Time `json:"created_at"`
		}
		var resp []userResponse
		for _, u := range users {
			resp = append(resp, userResponse{
				ID: u.ID, Email: u.Email,
				Verified: u.Verified, CreatedAt: u.CreatedAt,
			})
		}
		json.NewEncoder(w).Encode(resp)
	}
}

func adminCreateUser(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		if req.Email == "" || req.Password == "" {
			http.Error(w, "email and password required", http.StatusBadRequest)
			return
		}
		id, err := database.CreateUser(r.Context(), req.Email, req.Password)
		if err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		slog.Info("user created", "component", "admin", "email", req.Email, "id", id)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{"id": id, "email": req.Email})
	}
}

func adminDeleteUser(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" {
			http.Error(w, "user ID required", http.StatusBadRequest)
			return
		}
		user, err := database.GetUserByID(r.Context(), id)
		if err != nil || user == nil {
			http.Error(w, "user not found", http.StatusNotFound)
			return
		}
		if err := database.DeleteUser(r.Context(), id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		slog.Info("user deleted", "component", "admin", "email", user.Email, "id", id)
		w.WriteHeader(http.StatusNoContent)
	}
}

func adminVerifyUser(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" {
			http.Error(w, "user ID required", http.StatusBadRequest)
			return
		}
		if err := database.SetUserVerified(r.Context(), id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		slog.Info("user verified", "component", "admin", "id", id)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "verified"})
	}
}
