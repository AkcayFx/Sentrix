import { useEffect, useState, type FormEvent } from 'react';
import { useNavigate } from 'react-router-dom';
import { listFlows, createFlow, deleteFlow, type FlowDTO } from '../lib/api';


export default function FlowsPage() {
  const [flows, setFlows] = useState<FlowDTO[]>([]);
  const [loading, setLoading] = useState(true);
  const [showCreate, setShowCreate] = useState(false);
  const [title, setTitle] = useState('');
  const [target, setTarget] = useState('');

  const [creating, setCreating] = useState(false);
  const [error, setError] = useState('');
  const navigate = useNavigate();


  const load = () => {
    setLoading(true);
    listFlows()
      .then(setFlows)
      .catch(() => setError('Failed to load flows'))
      .finally(() => setLoading(false));
  };

  useEffect(load, []);

  const handleCreate = async (e: FormEvent) => {
    e.preventDefault();
    if (!title.trim() || !target.trim()) return;
    setCreating(true);
    setError('');
    try {
      await createFlow({ title: title.trim(), target: target.trim(), description: '' });
      setTitle('');
      setTarget('');

      setShowCreate(false);
      load();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create flow');
    } finally {
      setCreating(false);
    }
  };

  const handleDelete = async (id: string) => {
    if (!confirm('Delete this flow? This cannot be undone.')) return;
    try {
      await deleteFlow(id);
      load();
    } catch {
      setError('Failed to delete flow');
    }
  };

  return (
    <>
      <div className="page-header">
        <div className="page-title">Flows</div>
        <button className="btn btn-primary" onClick={() => setShowCreate(true)}>
          + New Flow
        </button>
      </div>

      <div className="page-body">
        {error && <div className="alert alert-error" style={{ marginBottom: 20 }}>⚠ {error}</div>}

        {/* Create Modal */}
        {showCreate && (
          <div className="modal-overlay" onClick={() => setShowCreate(false)}>
            <div className="modal" onClick={(e) => e.stopPropagation()}>
              <div className="modal-header">
                <h2>Create New Flow</h2>
                <button className="btn btn-ghost btn-icon" onClick={() => setShowCreate(false)}>✕</button>
              </div>
              <form onSubmit={handleCreate}>
                <div className="modal-body" style={{ display: 'flex', flexDirection: 'column', gap: 20 }}>
                  <div className="input-group">
                    <label htmlFor="flow-title">Title</label>
                    <input
                      id="flow-title"
                      className="input"
                      placeholder="e.g. Web App Security Audit"
                      value={title}
                      onChange={(e) => setTitle(e.target.value)}
                      required
                      autoFocus
                    />
                  </div>
                  <div className="input-group">
                    <label htmlFor="flow-target">Target</label>
                    <input
                      id="flow-target"
                      className="input"
                      placeholder="e.g. https://scanme.nmap.org or 192.168.1.1"
                      value={target}
                      onChange={(e) => setTarget(e.target.value)}
                      required
                    />
                    <div style={{ fontSize: '0.75rem', color: 'var(--text-muted)' }}>
                      The URL, hostname, or IP address to test. All agents will use this as the authorized target.
                    </div>
                  </div>
                </div>
                <div className="modal-footer">
                  <button type="button" className="btn btn-secondary" onClick={() => setShowCreate(false)}>Cancel</button>
                  <button type="submit" className="btn btn-primary" disabled={creating}>
                    {creating ? 'Creating...' : 'Create Flow'}
                  </button>
                </div>
              </form>
            </div>
          </div>
        )}

        {/* Flows Table */}
        <div className="table-container">
          <div className="table-header">
            <div className="table-title">All Flows</div>
            <span style={{ color: 'var(--text-muted)', fontSize: '0.8125rem' }}>
              {flows.length} flow{flows.length !== 1 ? 's' : ''}
            </span>
          </div>

          {loading ? (
            <div className="table-empty">
              <div className="spinner spinner-lg" style={{ margin: '0 auto' }} />
            </div>
          ) : flows.length === 0 ? (
            <div className="table-empty">
              <div className="table-empty-icon">🔄</div>
              <p>No flows created yet.</p>
              <p style={{ fontSize: '0.8125rem', marginTop: 4 }}>Click "New Flow" to start a security assessment.</p>
            </div>
          ) : (
            <table>
              <thead>
                <tr>
                  <th>Title</th>
                  <th>Target</th>
                  <th>Status</th>
                  <th>Created</th>
                  <th style={{ width: 100 }}>Actions</th>
                </tr>
              </thead>
              <tbody>
                {flows.map((f) => (
                  <tr key={f.id}>
                    <td style={{ fontWeight: 500 }}>{f.title}</td>
                    <td style={{ color: 'var(--text-secondary)', maxWidth: 250, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', fontFamily: 'monospace', fontSize: '0.85rem' }}>
                      {f.target || '—'}
                    </td>
                    <td>
                      <span className={`badge badge-${f.status}`}>
                        <span className="badge-dot" />
                        {f.status}
                      </span>
                    </td>
                    <td style={{ color: 'var(--text-secondary)', whiteSpace: 'nowrap' }}>
                      {new Date(f.created_at).toLocaleDateString()}
                    </td>
                    <td>
                      <div style={{ display: 'flex', gap: 8 }}>
                        <button className="btn btn-primary btn-sm" onClick={() => navigate(`/flows/${f.id}`)}>
                          View
                        </button>
                        <button className="btn btn-danger btn-sm" onClick={() => handleDelete(f.id)}>
                          Delete
                        </button>
                      </div>
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
