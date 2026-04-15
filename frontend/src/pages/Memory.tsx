import { useState, useEffect, useCallback } from 'react';
import { getStoredToken } from '../lib/api';
import { showToast } from '../components/Toast';

interface MemoryItem {
  id: string;
  tier: string;
  category: string;
  content: string;
  score: number;
  metadata: Record<string, unknown>;
  created_at: string;
}

const API = '/api/v1/memories';

const TIER_LABELS: Record<string, string> = {
  long_term: 'Long-term',
  working: 'Working',
  episodic: 'Episodic',
};

const CATEGORY_OPTIONS = [
  'all',
  'observation',
  'conclusion',
  'tool_output',
  'vulnerability',
  'technique',
  'general',
];

export default function MemoryPage() {
  const [memories, setMemories] = useState<MemoryItem[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(true);
  const [searchMode, setSearchMode] = useState(false);
  const [query, setQuery] = useState('');
  const [searching, setSearching] = useState(false);
  const [filterCategory, setFilterCategory] = useState('all');
  const [expandedId, setExpandedId] = useState<string | null>(null);
  const [stats, setStats] = useState<{ enabled: boolean } | null>(null);

  const headers = useCallback(
    () => {
      const token = getStoredToken();
      return {
        'Content-Type': 'application/json',
        ...(token ? { Authorization: `Bearer ${token}` } : {}),
      };
    },
    []
  );

  // Fetch memories list
  const fetchMemories = useCallback(async () => {
    setLoading(true);
    try {
      const res = await fetch(`${API}?limit=50`, { headers: headers() });
      if (!res.ok) throw new Error('Failed to load');
      const data = await res.json();
      setMemories(data.memories || []);
      setTotal(data.total || 0);
      setSearchMode(false);
    } catch {
      showToast('Failed to load memories', 'error');
    } finally {
      setLoading(false);
    }
  }, [headers]);

  // Fetch stats
  const fetchStats = useCallback(async () => {
    try {
      const res = await fetch(`${API}/stats`, { headers: headers() });
      if (res.ok) setStats(await res.json());
    } catch {
      /* ignore */
    }
  }, [headers]);

  useEffect(() => {
    fetchMemories();
    fetchStats();
  }, [fetchMemories, fetchStats]);

  // Semantic search
  const handleSearch = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!query.trim()) return;

    setSearching(true);
    try {
      const res = await fetch(`${API}/search`, {
        method: 'POST',
        headers: headers(),
        body: JSON.stringify({
          query: query.trim(),
          limit: 20,
          category: filterCategory === 'all' ? '' : filterCategory,
        }),
      });
      if (!res.ok) throw new Error('Search failed');
      const data = await res.json();
      setMemories(data.results || []);
      setTotal(data.count || 0);
      setSearchMode(true);
    } catch {
      showToast('Semantic search failed', 'error');
    } finally {
      setSearching(false);
    }
  };

  // Delete memory
  const handleDelete = async (id: string) => {
    try {
      const res = await fetch(`${API}/${id}`, {
        method: 'DELETE',
        headers: headers(),
      });
      if (!res.ok) throw new Error('Delete failed');
      setMemories((prev) => prev.filter((m) => m.id !== id));
      setTotal((prev) => prev - 1);
      showToast('Memory deleted', 'success');
    } catch {
      showToast('Failed to delete memory', 'error');
    }
  };

  const filtered =
    filterCategory === 'all'
      ? memories
      : memories.filter((m) => m.category === filterCategory);

  const formatDate = (iso: string) => {
    const d = new Date(iso);
    return d.toLocaleDateString('en-US', {
      month: 'short',
      day: 'numeric',
      hour: '2-digit',
      minute: '2-digit',
    });
  };

  return (
    <>
      <div className="page-header">
        <h1 className="page-title">🧠 Memory Store</h1>
        <div style={{ display: 'flex', gap: '8px', alignItems: 'center' }}>
          {stats && (
            <span
              className={`badge ${stats.enabled ? 'badge-done' : 'badge-failed'}`}
            >
              <span className="badge-dot" />
              {stats.enabled ? 'Embeddings Active' : 'Embeddings Off'}
            </span>
          )}
          {searchMode && (
            <button className="btn btn-secondary btn-sm" onClick={fetchMemories}>
              ← All Memories
            </button>
          )}
        </div>
      </div>

      <div className="page-body">
        {/* Search bar */}
        <form className="memory-search-bar" onSubmit={handleSearch}>
          <input
            className="input memory-search-input"
            type="text"
            placeholder="Semantic search across all memories..."
            value={query}
            onChange={(e) => setQuery(e.target.value)}
          />
          <select
            className="input memory-category-select"
            value={filterCategory}
            onChange={(e) => setFilterCategory(e.target.value)}
          >
            {CATEGORY_OPTIONS.map((c) => (
              <option key={c} value={c}>
                {c === 'all' ? 'All Categories' : c.replace(/_/g, ' ')}
              </option>
            ))}
          </select>
          <button
            className="btn btn-primary"
            type="submit"
            disabled={searching || !query.trim()}
          >
            {searching ? 'Searching…' : '🔍 Search'}
          </button>
        </form>

        {/* Results header */}
        <div className="memory-results-header">
          <span className="memory-results-count">
            {searchMode ? `${total} results` : `${total} memories total`}
            {filterCategory !== 'all' && ` · filtered by "${filterCategory}"`}
          </span>
        </div>

        {/* Memory list */}
        {loading ? (
          <div className="table-empty">
            <div className="spinner spinner-lg" />
            <p style={{ marginTop: '12px' }}>Loading memories…</p>
          </div>
        ) : filtered.length === 0 ? (
          <div className="table-empty">
            <div className="table-empty-icon">🧠</div>
            <p>
              {searchMode
                ? 'No memories match your search query.'
                : 'No memories stored yet. Memories are created automatically during flow execution.'}
            </p>
          </div>
        ) : (
          <div className="memory-grid">
            {filtered.map((mem) => (
              <div
                key={mem.id}
                className={`memory-card card ${expandedId === mem.id ? 'memory-card-expanded' : ''}`}
                onClick={() =>
                  setExpandedId(expandedId === mem.id ? null : mem.id)
                }
              >
                <div className="memory-card-top">
                  <div className="memory-card-badges">
                    <span className={`badge badge-tier-${mem.tier}`}>
                      {TIER_LABELS[mem.tier] || mem.tier}
                    </span>
                    <span className="badge badge-category">
                      {mem.category.replace(/_/g, ' ')}
                    </span>
                    {searchMode && mem.score < 1 && (
                      <span className="badge badge-score">
                        {Math.round(mem.score * 100)}% match
                      </span>
                    )}
                  </div>
                  <span className="memory-card-date">
                    {formatDate(mem.created_at)}
                  </span>
                </div>

                <p className="memory-card-content">
                  {expandedId === mem.id
                    ? mem.content
                    : mem.content.length > 200
                      ? mem.content.slice(0, 200) + '…'
                      : mem.content}
                </p>

                {expandedId === mem.id && (
                  <div className="memory-card-actions">
                    <button
                      className="btn btn-danger btn-sm"
                      onClick={(e) => {
                        e.stopPropagation();
                        handleDelete(mem.id);
                      }}
                    >
                      Delete
                    </button>
                    <span className="memory-card-id">
                      ID: {mem.id.slice(0, 8)}
                    </span>
                  </div>
                )}
              </div>
            ))}
          </div>
        )}
      </div>
    </>
  );
}
