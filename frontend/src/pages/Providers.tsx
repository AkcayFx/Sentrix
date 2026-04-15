import { useEffect, useState, type FormEvent } from 'react';
import {
  listProviders,
  createProvider,
  deleteProvider,
  testProvider,
  type ProviderDTO,
  type TestProviderResult,
} from '../lib/api';

const PROVIDER_TYPES = [
  { value: 'openai', label: 'OpenAI', icon: '🧠' },
  { value: 'openrouter', label: 'OpenRouter', icon: '🛣️' },
  { value: 'anthropic', label: 'Anthropic', icon: '🤖' },
  { value: 'gemini', label: 'Google Gemini', icon: '✨' },
  { value: 'ollama', label: 'Ollama (Local)', icon: '🏠' },
  { value: 'deepseek', label: 'DeepSeek', icon: '🔎' },
  { value: 'custom', label: 'Custom HTTP', icon: '🔧' },
];

type TestStatus = 'idle' | 'testing' | 'success' | 'error';

export default function ProvidersPage() {
  const [providers, setProviders] = useState<ProviderDTO[]>([]);
  const [loading, setLoading] = useState(true);
  const [showCreate, setShowCreate] = useState(false);
  const [providerType, setProviderType] = useState('openai');
  const [modelName, setModelName] = useState('');
  const [apiKey, setApiKey] = useState('');
  const [baseUrl, setBaseUrl] = useState('');
  const [isDefault, setIsDefault] = useState(false);
  const [creating, setCreating] = useState(false);
  const [error, setError] = useState('');

  // Test connection state
  const [testStatuses, setTestStatuses] = useState<Record<string, TestStatus>>({});
  const [testErrors, setTestErrors] = useState<Record<string, string>>({});
  const [modalTestStatus, setModalTestStatus] = useState<TestStatus>('idle');
  const [modalTestError, setModalTestError] = useState('');

  const load = () => {
    setLoading(true);
    listProviders()
      .then((userProvs) => {
        setProviders(userProvs);
      })
      .catch(() => setError('Failed to load providers'))
      .finally(() => setLoading(false));
  };

  useEffect(load, []);

  const handleCreate = async (e: FormEvent) => {
    e.preventDefault();
    setCreating(true);
    setError('');
    try {
      await createProvider({
        provider_type: providerType,
        model_name: modelName || undefined,
        api_key: apiKey || undefined,
        base_url: baseUrl || undefined,
        is_default: isDefault,
      });
      setProviderType('openai');
      setModelName('');
      setApiKey('');
      setBaseUrl('');
      setIsDefault(false);
      setShowCreate(false);
      setModalTestStatus('idle');
      setModalTestError('');
      load();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create provider');
    } finally {
      setCreating(false);
    }
  };

  const handleDelete = async (id: string) => {
    if (!confirm('Remove this provider configuration?')) return;
    try {
      await deleteProvider(id);
      load();
    } catch {
      setError('Failed to delete provider');
    }
  };

  const handleTestCard = async (p: ProviderDTO) => {
    setTestStatuses((s) => ({ ...s, [p.id]: 'testing' }));
    setTestErrors((s) => ({ ...s, [p.id]: '' }));
    try {
      const result: TestProviderResult = await testProvider({
        provider_type: p.provider_type,
        provider_id: p.id,
        model_name: p.model_name || undefined,
        base_url: p.base_url || undefined,
      });
      setTestStatuses((s) => ({ ...s, [p.id]: result.success ? 'success' : 'error' }));
      if (!result.success) {
        setTestErrors((s) => ({ ...s, [p.id]: result.error || 'Test failed' }));
      }
    } catch (err) {
      setTestStatuses((s) => ({ ...s, [p.id]: 'error' }));
      setTestErrors((s) => ({ ...s, [p.id]: err instanceof Error ? err.message : 'Test failed' }));
    }
  };

  const handleTestModal = async () => {
    setModalTestStatus('testing');
    setModalTestError('');
    try {
      const result = await testProvider({
        provider_type: providerType,
        api_key: apiKey || undefined,
        base_url: baseUrl || undefined,
        model_name: modelName || undefined,
      });
      setModalTestStatus(result.success ? 'success' : 'error');
      if (!result.success) setModalTestError(result.error || 'Test failed');
    } catch (err) {
      setModalTestStatus('error');
      setModalTestError(err instanceof Error ? err.message : 'Connection failed');
    }
  };

  const getProviderInfo = (type: string) =>
    PROVIDER_TYPES.find((p) => p.value === type) || { label: type, icon: '❓' };

  const getModelPlaceholder = (type: string) => {
    switch (type) {
      case 'openrouter':
        return 'e.g. openrouter/auto, openai/gpt-5.4-mini';
      case 'anthropic':
        return 'e.g. claude-sonnet-4-20250514';
      case 'gemini':
        return 'e.g. gemini-2.5-flash';
      case 'deepseek':
        return 'e.g. deepseek-chat';
      case 'ollama':
        return 'e.g. llama3.1:8b';
      case 'custom':
        return 'e.g. default';
      default:
        return 'e.g. gpt-4o, gpt-5.4-mini';
    }
  };

  const statusDot = (status: TestStatus) => {
    const colors: Record<TestStatus, string> = {
      idle: 'var(--text-muted)',
      testing: 'var(--warning)',
      success: 'var(--success)',
      error: 'var(--danger)',
    };
    return (
      <span
        style={{
          display: 'inline-block',
          width: 8,
          height: 8,
          borderRadius: '50%',
          backgroundColor: colors[status],
          animation: status === 'testing' ? 'pulse 1s infinite' : undefined,
        }}
      />
    );
  };

  return (
    <>
      <div className="page-header">
        <div className="page-title">LLM Providers</div>
        <button className="btn btn-primary" onClick={() => setShowCreate(true)}>
          + Add Provider
        </button>
      </div>

      <div className="page-body">
        {error && <div className="alert alert-error" style={{ marginBottom: 20 }}>⚠ {error}</div>}

        {/* Create Modal */}
        {showCreate && (
          <div className="modal-overlay" onClick={() => setShowCreate(false)}>
            <div className="modal" onClick={(e) => e.stopPropagation()}>
              <div className="modal-header">
                <h2>Add Provider</h2>
                <button className="btn btn-ghost btn-icon" onClick={() => setShowCreate(false)}>✕</button>
              </div>
              <form onSubmit={handleCreate}>
                <div className="modal-body" style={{ display: 'flex', flexDirection: 'column', gap: 20 }}>
                  <div className="input-group">
                    <label htmlFor="prov-type">Provider</label>
                    <select
                      id="prov-type"
                      className="input"
                      value={providerType}
                      onChange={(e) => {
                        setProviderType(e.target.value);
                        setModalTestStatus('idle');
                        setModalTestError('');
                      }}
                    >
                      {PROVIDER_TYPES.map((p) => (
                        <option key={p.value} value={p.value}>{p.icon} {p.label}</option>
                      ))}
                    </select>
                  </div>
                  <div className="input-group">
                    <label htmlFor="prov-model">Model name</label>
                    <input
                      id="prov-model"
                      className="input"
                      placeholder={getModelPlaceholder(providerType)}
                      value={modelName}
                      onChange={(e) => setModelName(e.target.value)}
                    />
                  </div>
                  <div className="input-group">
                    <label htmlFor="prov-key">API Key</label>
                    <input
                      id="prov-key"
                      type="password"
                      className="input"
                      placeholder="sk-..."
                      value={apiKey}
                      onChange={(e) => setApiKey(e.target.value)}
                    />
                  </div>
                  {(providerType === 'ollama' || providerType === 'custom') && (
                    <div className="input-group">
                      <label htmlFor="prov-url">Base URL</label>
                      <input
                        id="prov-url"
                        className="input"
                        placeholder={providerType === 'ollama' ? 'http://localhost:11434' : 'http://localhost:8000/v1'}
                        value={baseUrl}
                        onChange={(e) => setBaseUrl(e.target.value)}
                      />
                    </div>
                  )}
                  <label style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: '0.875rem', cursor: 'pointer' }}>
                    <input type="checkbox" checked={isDefault} onChange={(e) => setIsDefault(e.target.checked)} />
                    Set as default provider
                  </label>

                  {/* Test Connection */}
                  <div style={{
                    display: 'flex', alignItems: 'center', gap: 12,
                    padding: '12px 16px', borderRadius: 8,
                    background: 'var(--surface-hover)',
                  }}>
                    <button
                      type="button"
                      className="btn btn-secondary btn-sm"
                      onClick={handleTestModal}
                      disabled={modalTestStatus === 'testing'}
                      style={{ whiteSpace: 'nowrap' }}
                    >
                      {modalTestStatus === 'testing' ? '⏳ Testing...' : '🔌 Test Connection'}
                    </button>
                    {modalTestStatus === 'success' && (
                      <span style={{ color: 'var(--success)', fontSize: '0.8125rem', fontWeight: 500 }}>
                        ✅ Connection OK
                      </span>
                    )}
                    {modalTestStatus === 'error' && (
                      <span style={{ color: 'var(--danger)', fontSize: '0.8125rem' }}>
                        ❌ {modalTestError}
                      </span>
                    )}
                  </div>
                </div>
                <div className="modal-footer">
                  <button type="button" className="btn btn-secondary" onClick={() => setShowCreate(false)}>Cancel</button>
                  <button type="submit" className="btn btn-primary" disabled={creating}>
                    {creating ? 'Saving...' : 'Add Provider'}
                  </button>
                </div>
              </form>
            </div>
          </div>
        )}

        {loading ? (
          <div style={{ display: 'flex', justifyContent: 'center', padding: 60 }}>
            <div className="spinner spinner-lg" />
          </div>
        ) : (
          <>
            {/* User Providers Section */}
            <div style={{
              fontSize: '0.75rem', fontWeight: 600, textTransform: 'uppercase',
              letterSpacing: '0.1em', color: 'var(--text-muted)', marginBottom: 12,
            }}>
              {providers.length > 0 ? 'Your Providers' : ''}
            </div>

            {providers.length === 0 ? (
              <div className="card" style={{ textAlign: 'center', padding: 60 }}>
                <div style={{ fontSize: '2.5rem', marginBottom: 12 }}>🤖</div>
                <p style={{ color: 'var(--text-secondary)' }}>No LLM providers configured yet.</p>
                <p style={{ color: 'var(--text-muted)', fontSize: '0.8125rem', marginTop: 4 }}>
                  Add a provider to power AI agents.
                </p>
                <button className="btn btn-primary btn-sm" onClick={() => setShowCreate(true)} style={{ marginTop: 16 }}>
                  Add Provider
                </button>
              </div>
            ) : (
              <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(320px, 1fr))', gap: 20 }}>
                {providers.map((p) => {
                  const info = getProviderInfo(p.provider_type);
                  const tStatus = testStatuses[p.id] || 'idle';
                  const tError = testErrors[p.id] || '';
                  return (
                    <div key={p.id} className="card card-glow">
                      <div style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', marginBottom: 16 }}>
                        <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
                          <div className="stat-card-icon violet" style={{ fontSize: '1.5rem' }}>{info.icon}</div>
                          <div>
                            <div style={{ fontWeight: 600, display: 'flex', alignItems: 'center', gap: 8 }}>
                              {info.label}
                              {statusDot(tStatus)}
                            </div>
                            <div style={{ fontSize: '0.8125rem', color: 'var(--text-muted)' }}>
                              {p.model_name || 'Default model'}
                            </div>
                          </div>
                        </div>
                        {p.is_default && (
                          <span className="badge badge-done">Default</span>
                        )}
                      </div>

                      <div style={{ display: 'flex', gap: 12, fontSize: '0.8125rem', color: 'var(--text-secondary)', marginBottom: 12 }}>
                        <span>API Key: {p.has_api_key ? '••••••••' : 'Not set'}</span>
                        {p.base_url && <span>URL: {p.base_url}</span>}
                      </div>

                      {tStatus === 'error' && tError && (
                        <div style={{
                          fontSize: '0.75rem', color: 'var(--danger)',
                          padding: '8px 12px', borderRadius: 6,
                          background: 'rgba(255, 82, 82, 0.1)',
                          marginBottom: 12,
                        }}>
                          {tError}
                        </div>
                      )}

                      <div style={{ display: 'flex', justifyContent: 'space-between', gap: 8 }}>
                        <button
                          className="btn btn-secondary btn-sm"
                          onClick={() => handleTestCard(p)}
                          disabled={tStatus === 'testing'}
                        >
                          {tStatus === 'testing' ? '⏳ Testing...' : '🔌 Test'}
                        </button>
                        <button className="btn btn-danger btn-sm" onClick={() => handleDelete(p.id)}>Remove</button>
                      </div>
                    </div>
                  );
                })}
              </div>
            )}
          </>
        )}
      </div>

      <style>{`
        @keyframes pulse {
          0%, 100% { opacity: 1; }
          50% { opacity: 0.3; }
        }
      `}</style>
    </>
  );
}
