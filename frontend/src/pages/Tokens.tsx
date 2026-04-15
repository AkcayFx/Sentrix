import { useEffect, useState, type FormEvent } from 'react';
import { listAPITokens, createAPIToken, deleteAPIToken, type APITokenDTO } from '../lib/api';

export default function TokensPage() {
  const [tokens, setTokens] = useState<APITokenDTO[]>([]);
  const [loading, setLoading] = useState(true);
  const [showCreate, setShowCreate] = useState(false);
  const [label, setLabel] = useState('');
  const [creating, setCreating] = useState(false);
  const [newToken, setNewToken] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);
  const [error, setError] = useState('');

  const load = () => {
    setLoading(true);
    listAPITokens()
      .then(setTokens)
      .catch(() => setError('Failed to load tokens'))
      .finally(() => setLoading(false));
  };

  useEffect(load, []);

  const handleCreate = async (e: FormEvent) => {
    e.preventDefault();
    if (!label.trim()) return;
    setCreating(true);
    setError('');
    try {
      const result = await createAPIToken(label.trim());
      setNewToken(result.token);
      setLabel('');
      load();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create token');
    } finally {
      setCreating(false);
    }
  };

  const handleCopy = () => {
    if (newToken) {
      navigator.clipboard.writeText(newToken);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    }
  };

  const handleDelete = async (id: string) => {
    if (!confirm('Revoke this API token? Any integrations using it will stop working.')) return;
    try {
      await deleteAPIToken(id);
      load();
    } catch {
      setError('Failed to delete token');
    }
  };

  const closeTokenDialog = () => {
    setNewToken(null);
    setCopied(false);
    setShowCreate(false);
  };

  return (
    <>
      <div className="page-header">
        <div className="page-title">API Tokens</div>
        <button className="btn btn-primary" onClick={() => { setShowCreate(true); setNewToken(null); }}>
          + Generate Token
        </button>
      </div>

      <div className="page-body">
        {error && <div className="alert alert-error" style={{ marginBottom: 20 }}>⚠ {error}</div>}

        {/* Create / Show Token Modal */}
        {showCreate && (
          <div className="modal-overlay" onClick={closeTokenDialog}>
            <div className="modal" onClick={(e) => e.stopPropagation()}>
              <div className="modal-header">
                <h2>{newToken ? 'Token Generated' : 'Generate API Token'}</h2>
                <button className="btn btn-ghost btn-icon" onClick={closeTokenDialog}>✕</button>
              </div>

              {newToken ? (
                <>
                  <div className="modal-body">
                    <div className="alert alert-success" style={{ marginBottom: 16 }}>
                      ✓ Token generated. Copy it now — it won't be shown again!
                    </div>
                    <div
                      style={{
                        background: 'var(--surface-1)',
                        border: '1px solid var(--border)',
                        borderRadius: 'var(--radius-md)',
                        padding: '12px 16px',
                        fontFamily: 'var(--font-mono)',
                        fontSize: '0.8125rem',
                        wordBreak: 'break-all',
                        color: 'var(--accent)',
                        cursor: 'pointer',
                      }}
                      onClick={handleCopy}
                      title="Click to copy"
                    >
                      {newToken}
                    </div>
                  </div>
                  <div className="modal-footer">
                    <button className="btn btn-secondary" onClick={handleCopy}>
                      {copied ? '✓ Copied!' : 'Copy Token'}
                    </button>
                    <button className="btn btn-primary" onClick={closeTokenDialog}>Done</button>
                  </div>
                </>
              ) : (
                <form onSubmit={handleCreate}>
                  <div className="modal-body">
                    <div className="input-group">
                      <label htmlFor="token-label">Label</label>
                      <input
                        id="token-label"
                        className="input"
                        placeholder="e.g. CI/CD Pipeline, Local Development"
                        value={label}
                        onChange={(e) => setLabel(e.target.value)}
                        required
                        autoFocus
                      />
                    </div>
                  </div>
                  <div className="modal-footer">
                    <button type="button" className="btn btn-secondary" onClick={closeTokenDialog}>Cancel</button>
                    <button type="submit" className="btn btn-primary" disabled={creating}>
                      {creating ? 'Generating...' : 'Generate'}
                    </button>
                  </div>
                </form>
              )}
            </div>
          </div>
        )}

        {/* Tokens Table */}
        <div className="table-container">
          <div className="table-header">
            <div className="table-title">Your API Tokens</div>
            <span style={{ color: 'var(--text-muted)', fontSize: '0.8125rem' }}>
              {tokens.length} token{tokens.length !== 1 ? 's' : ''}
            </span>
          </div>

          {loading ? (
            <div className="table-empty">
              <div className="spinner spinner-lg" style={{ margin: '0 auto' }} />
            </div>
          ) : tokens.length === 0 ? (
            <div className="table-empty">
              <div className="table-empty-icon">🔑</div>
              <p>No API tokens created yet.</p>
              <p style={{ fontSize: '0.8125rem', marginTop: 4, color: 'var(--text-muted)' }}>
                Generate a token for programmatic access to Sentrix APIs.
              </p>
            </div>
          ) : (
            <table>
              <thead>
                <tr>
                  <th>Label</th>
                  <th>Last Used</th>
                  <th>Created</th>
                  <th style={{ width: 100 }}>Actions</th>
                </tr>
              </thead>
              <tbody>
                {tokens.map((t) => (
                  <tr key={t.id}>
                    <td>
                      <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                        <span style={{ fontSize: '1rem' }}>🔑</span>
                        <span style={{ fontWeight: 500 }}>{t.label}</span>
                      </div>
                    </td>
                    <td style={{ color: 'var(--text-secondary)' }}>
                      {t.last_used_at ? new Date(t.last_used_at).toLocaleDateString() : 'Never'}
                    </td>
                    <td style={{ color: 'var(--text-secondary)', whiteSpace: 'nowrap' }}>
                      {new Date(t.created_at).toLocaleDateString()}
                    </td>
                    <td>
                      <button className="btn btn-danger btn-sm" onClick={() => handleDelete(t.id)}>
                        Revoke
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      </div>
    </>
  );
}
