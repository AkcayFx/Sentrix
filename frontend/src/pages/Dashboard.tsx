import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useAuth } from '../lib/auth';
import { listFlows, listProviders, type FlowDTO, type ProviderDTO } from '../lib/api';
import ScanLine from '../components/ScanLine';

const BINARY_LABELS = ['0x01', '0x02', '0x03', '0x04'];

const HEX_STREAM = Array.from({ length: 400 }, () =>
  Math.floor(Math.random() * 256).toString(16).padStart(2, '0')
).join(' ');

export default function Dashboard() {
  const { user } = useAuth();
  const navigate = useNavigate();
  const [flows, setFlows] = useState<FlowDTO[]>([]);
  const [providers, setProviders] = useState<ProviderDTO[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    Promise.all([
      listFlows().catch(() => [] as FlowDTO[]),
      listProviders().catch(() => [] as ProviderDTO[]),
    ]).then(([f, p]) => {
      setFlows(f);
      setProviders(p);
      setLoading(false);
    });
  }, []);

  const totalFlows = flows.length;
  const runningFlows = flows.filter((f) => f.status === 'running').length;
  const completedFlows = flows.filter((f) => f.status === 'done' || f.status === 'completed').length;
  const totalProviders = providers.length;
  const recentFlows = flows.slice(0, 5);

  const greeting = () => {
    const h = new Date().getHours();
    if (h < 12) return 'Good morning';
    if (h < 18) return 'Good afternoon';
    return 'Good evening';
  };

  const stats = [
    { label: 'Total Flows', value: totalFlows, color: 'accent', binary: BINARY_LABELS[0] },
    { label: 'Running', value: runningFlows, color: 'blue', binary: BINARY_LABELS[1] },
    { label: 'Completed', value: completedFlows, color: 'green', binary: BINARY_LABELS[2] },
    { label: 'LLM Providers', value: totalProviders, color: 'violet', binary: BINARY_LABELS[3] },
  ];

  return (
    <>
      <div className="page-header">
        <div>
          <div className="page-title page-title-hacker">{greeting()}, {user?.display_name || 'Analyst'}</div>
          <p style={{ color: 'var(--matrix)', fontSize: '0.75rem', marginTop: 4, fontFamily: 'var(--font-mono)', letterSpacing: '0.04em', opacity: 0.7 }}>
            root@sentrix:~$ status --overview
          </p>
        </div>
        <button className="btn btn-primary" onClick={() => navigate('/flows')}>
          + New Flow
        </button>
      </div>

      <div className="page-body">
        {/* Stats */}
        <div className="stats-grid">
          {stats.map((s) => (
            <div className="stat-card hud-corners" key={s.label}>
              <ScanLine duration={5 + Math.random() * 3} />
              <div className="hex-overlay" aria-hidden="true">{HEX_STREAM}</div>
              <div className="stat-card-header" style={{ position: 'relative', zIndex: 1 }}>
                <div className={`stat-card-icon ${s.color}`}>
                  <span style={{ fontFamily: 'var(--font-mono)', fontSize: '0.6875rem', fontWeight: 700 }}>{s.binary}</span>
                </div>
              </div>
              <div className="stat-card-value" style={{ position: 'relative', zIndex: 1 }}>{loading ? '--' : s.value}</div>
              <div className="stat-card-label" style={{ position: 'relative', zIndex: 1 }}>{s.label}</div>
            </div>
          ))}
        </div>

        {/* Recent Flows Table */}
        <div className="table-container">
          <div className="table-header">
            <div className="table-title">Recent Flows</div>
            {flows.length > 0 && (
              <button className="btn btn-ghost btn-sm" onClick={() => navigate('/flows')}>
                View all &gt;&gt;
              </button>
            )}
          </div>

          {loading ? (
            <div className="table-empty">
              <div className="spinner spinner-lg" style={{ margin: '0 auto' }} />
            </div>
          ) : recentFlows.length === 0 ? (
            <div className="table-empty">
              <pre style={{
                fontFamily: 'var(--font-mono)',
                fontSize: '0.6875rem',
                marginBottom: 16,
                color: 'var(--matrix)',
                opacity: 0.5,
                lineHeight: 1.4,
              }}>
{`  _____
 |     |
 | 0x0 |  NO TARGETS ACQUIRED
 |_____|
`}
              </pre>
              <p style={{ color: 'var(--text-muted)' }}>No active engagements. Initialize a flow to begin reconnaissance.</p>
              <button className="btn btn-primary btn-sm" onClick={() => navigate('/flows')} style={{ marginTop: 16 }}>
                {'> Initialize Flow'}
              </button>
            </div>
          ) : (
            <table>
              <thead>
                <tr>
                  <th>Title</th>
                  <th>Status</th>
                  <th>Created</th>
                </tr>
              </thead>
              <tbody>
                {recentFlows.map((f) => (
                  <tr key={f.id} style={{ cursor: 'pointer' }} onClick={() => navigate(`/flows/${f.id}`)}>
                    <td style={{ fontWeight: 500 }}>{f.title}</td>
                    <td>
                      <span className={`badge badge-${f.status}`}>
                        <span className="badge-dot" />
                        {f.status}
                      </span>
                    </td>
                    <td style={{ color: 'var(--text-secondary)', fontFamily: 'var(--font-mono)', fontSize: '0.75rem' }}>
                      {new Date(f.created_at).toLocaleDateString()}
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
