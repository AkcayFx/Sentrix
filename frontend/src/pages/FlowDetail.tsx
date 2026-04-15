import { useEffect, useState, useRef, useCallback, useMemo, type FormEvent } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import {
  getFlowDetail,
  getAssistantSession,
  sendAssistantMessage,
  startFlow,
  stopFlow,
  subscribeFlowEvents,
  updateAssistantSession,
  updateFlow,
  fetchArtifactBlob,
  type FlowDetailDTO,
  type FlowDTO,
  type TaskDTO,
  type TraceEntryDTO,
  type AgentLogDTO,
  type TerminalLogDTO,
  type SearchLogDTO,
  type VectorStoreLogDTO,
  type ArtifactDTO,
  type MessageChainDTO,
  type AssistantDTO,
  type AssistantLogDTO,
  type BrowserCapabilitiesDTO,
  type FindingDTO,
} from '../lib/api';
import type { ScreenshotDataEntry } from '../lib/reportHtml';

interface LogEntry {
  id: string;
  ts: number;
  time: string;
  type: string;
  agent?: string;
  message: string;
  meta?: Record<string, unknown>;
}

type FlowTab = 'live' | 'trace' | 'intel' | 'screenshots' | 'reports' | 'tasks';
type ChatTab = 'automation' | 'assistant';

type IntelEntry =
  | {
      id: string;
      createdAt: string;
      sortTime: number;
      kind: 'search';
      toolName: string;
      provider: string;
      query: string;
      target: string;
      resultCount: number;
      summary: string;
      metadata: string;
    }
  | {
      id: string;
      createdAt: string;
      sortTime: number;
      kind: 'memory';
      action: string;
      query: string;
      content: string;
      resultCount: number;
    };

const AGENT_COLORS: Record<string, string> = {
  orchestrator: 'var(--secondary)',
  primary: '#2563eb',
  assistant: '#0f766e',
  generator: 'var(--warning)',
  refiner: '#8b5cf6',
  reporter: '#14b8a6',
  adviser: '#f97316',
  reflector: '#ec4899',
  researcher: 'var(--info)',
  searcher: '#0891b2',
  enricher: '#7c3aed',
  installer: '#84cc16',
  pentester: 'var(--danger)',
  coder: 'var(--success)',
};

const AGENT_ICONS: Record<string, string> = {
  orchestrator: 'O',
  primary: 'M',
  assistant: 'U',
  generator: 'G',
  refiner: 'F',
  reporter: 'T',
  adviser: 'A',
  reflector: 'Y',
  researcher: 'R',
  searcher: 'S',
  enricher: 'E',
  installer: 'I',
  pentester: 'P',
  coder: 'C',
};

const STATUS_CLASSES: Record<string, string> = {
  idle: 'badge-pending',
  pending: 'badge-pending',
  queued: 'badge-pending',
  running: 'badge-running',
  done: 'badge-done',
  failed: 'badge-failed',
  stopped: 'badge-failed',
};

let logIdCounter = 0;

function parseMetadata(value: string): Record<string, unknown> | null {
  try {
    return JSON.parse(value) as Record<string, unknown>;
  } catch {
    return null;
  }
}

function humanizeChainType(value: string): string {
  return value.replace(/_/g, ' ');
}

function toSortTime(value: string): number {
  return new Date(value).getTime();
}

export default function FlowDetailPage() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();

  const [flow, setFlow] = useState<FlowDTO | null>(null);
  const [tasks, setTasks] = useState<TaskDTO[]>([]);
  const [logs, setLogs] = useState<LogEntry[]>([]);
  const [traceEntries, setTraceEntries] = useState<TraceEntryDTO[]>([]);
  const [showDebugTrace, setShowDebugTrace] = useState(false);
  const [expandedTraceIds, setExpandedTraceIds] = useState<Set<string>>(new Set());
  const [, setAgentLogs] = useState<AgentLogDTO[]>([]);
  const [terminalLogs, setTerminalLogs] = useState<TerminalLogDTO[]>([]);
  const [searchLogs, setSearchLogs] = useState<SearchLogDTO[]>([]);
  const [vectorStoreLogs, setVectorStoreLogs] = useState<VectorStoreLogDTO[]>([]);
  const [artifacts, setArtifacts] = useState<ArtifactDTO[]>([]);
  const [findings, setFindings] = useState<FindingDTO[]>([]);
  const [browserCapabilities, setBrowserCapabilities] = useState<BrowserCapabilitiesDTO>({ mode: 'native', screenshots_enabled: false });
  const [, setMessageChains] = useState<MessageChainDTO[]>([]);
  const [assistant, setAssistant] = useState<AssistantDTO | null>(null);
  const [assistantLogs, setAssistantLogs] = useState<AssistantLogDTO[]>([]);
  const [activeTab, setActiveTab] = useState<FlowTab>('live');
  const [activeChatTab, setActiveChatTab] = useState<ChatTab>('automation');
  const [loading, setLoading] = useState(true);
  const [starting, setStarting] = useState(false);
  const [stopping, setStopping] = useState(false);
  const [assistantLoading, setAssistantLoading] = useState(true);
  const [assistantSending, setAssistantSending] = useState(false);
  const [assistantUpdating, setAssistantUpdating] = useState(false);
  const [editingScope, setEditingScope] = useState(false);
  const [savingScope, setSavingScope] = useState(false);
  const [draftTitle, setDraftTitle] = useState('');
  const [draftTarget, setDraftTarget] = useState('');
  const [draftDescription, setDraftDescription] = useState('');
  const [assistantDraft, setAssistantDraft] = useState('');
  const [automationDraft, setAutomationDraft] = useState('');
  const [error, setError] = useState('');
  const [assistantError, setAssistantError] = useState('');
  const [screenshotUrls, setScreenshotUrls] = useState<Record<string, string>>({});

  const logEndRef = useRef<HTMLDivElement>(null);
  const assistantLogEndRef = useRef<HTMLDivElement>(null);
  const esRef = useRef<EventSource | null>(null);
  const screenshotUrlsRef = useRef<Record<string, string>>({});

  const isFlowActive = flow && (flow.status === 'running' || flow.status === 'queued');
  const scopeMissing = !flow?.target?.trim();

  const applyFlowDetail = useCallback((data: FlowDetailDTO) => {
    setFlow(data.flow);
    setTasks(data.tasks || []);
    setTraceEntries(data.trace_entries || []);
    setAgentLogs(data.agent_logs || []);
    setTerminalLogs(data.terminal_logs || []);
    setSearchLogs(data.search_logs || []);
    setVectorStoreLogs(data.vector_store_logs || []);
    setArtifacts(data.artifacts || []);
    setFindings(data.findings || []);
    setMessageChains(data.message_chains || []);
    if (data.browser_capabilities) {
      setBrowserCapabilities(data.browser_capabilities);
    }
    setDraftTitle(data.flow.title);
    setDraftTarget(data.flow.target || '');
    setDraftDescription(data.flow.description || '');
  }, []);

  const applyAssistantDetail = useCallback((data: { assistant: AssistantDTO; logs: AssistantLogDTO[] }) => {
    setAssistant(data.assistant);
    setAssistantLogs(data.logs || []);
  }, []);

  const loadFlow = useCallback(async (showSpinner = true) => {
    if (!id) return;
    if (showSpinner) {
      setLoading(true);
    }
    try {
      const data = await getFlowDetail(id, showDebugTrace);
      applyFlowDetail(data);
      setError('');
    } catch {
      setError('Failed to load flow');
    } finally {
      if (showSpinner) {
        setLoading(false);
      }
    }
  }, [id, showDebugTrace, applyFlowDetail]);

  const loadAssistant = useCallback(async () => {
    if (!id) return;
    setAssistantLoading(true);
    try {
      const data = await getAssistantSession(id);
      applyAssistantDetail(data);
      setAssistantError('');
    } catch (err) {
      setAssistantError(err instanceof Error ? err.message : 'Failed to load assistant');
    } finally {
      setAssistantLoading(false);
    }
  }, [id, applyAssistantDetail]);

  const addLog = useCallback((type: string, message: string, agent?: string, meta?: Record<string, unknown>) => {
    const entry: LogEntry = {
      id: String(++logIdCounter),
      ts: Date.now(),
      time: new Date().toLocaleTimeString(),
      type,
      agent,
      message,
      meta,
    };
    setLogs(prev => [...prev, entry]);
  }, []);

  useEffect(() => {
    loadFlow();
  }, [loadFlow]);

  useEffect(() => {
    loadAssistant();
  }, [loadAssistant]);

  // Lazy-load screenshot blobs when the Reports tab is active.
  useEffect(() => {
    screenshotUrlsRef.current = screenshotUrls;
  }, [screenshotUrls]);

  useEffect(() => {
    if (activeTab !== 'screenshots' || !id) return;
    const screenshotArtifacts = artifacts.filter(a => a.kind === 'browser_screenshot');
    if (screenshotArtifacts.length === 0) return;

    let cancelled = false;
    const newUrls: Record<string, string> = {};

    (async () => {
      for (const art of screenshotArtifacts) {
        if (cancelled) break;
        if (screenshotUrls[art.id]) continue;
        try {
          const blob = await fetchArtifactBlob(id, art.id);
          if (cancelled) break;
          newUrls[art.id] = URL.createObjectURL(blob);
        } catch {
          // Skip failed fetches.
        }
      }
      if (!cancelled && Object.keys(newUrls).length > 0) {
        setScreenshotUrls(prev => ({ ...prev, ...newUrls }));
      }
    })();

    return () => {
      cancelled = true;
    };
  }, [activeTab, id, artifacts, screenshotUrls]);

  useEffect(() => {
    return () => {
      Object.values(screenshotUrlsRef.current).forEach(url => URL.revokeObjectURL(url));
    };
  }, []);

  useEffect(() => {
    if (!isFlowActive || !id) return;

    const interval = window.setInterval(() => {
      getFlowDetail(id, showDebugTrace)
        .then(data => {
          applyFlowDetail(data);
        })
        .catch(() => undefined);
    }, 4000);

    return () => window.clearInterval(interval);
  }, [id, isFlowActive, showDebugTrace, applyFlowDetail]);

  useEffect(() => {
    if (!id || !isFlowActive) return;

    const es = subscribeFlowEvents(
      id,
      (type, data) => {
        switch (type) {
          case 'connected':
            addLog('system', 'Connected to event stream');
            break;
          case 'flow_started':
            addLog('system', `Flow started: ${(data as Record<string, string>).title || ''}`);
            setFlow(prev => prev ? { ...prev, status: 'running' } : prev);
            break;
          case 'task_created': {
            const d = data as Record<string, string>;
            addLog('task', `Task created: ${d.title}`, d.agent_role);
            setTasks(prev => [
              ...prev,
              {
                id: d.task_id,
                title: d.title,
                description: '',
                status: 'pending',
                sort_order: Number(d.sort_order) || prev.length,
              },
            ]);
            break;
          }
          case 'subtask_started': {
            const d = data as Record<string, string>;
            addLog('agent', `${d.agent_role} agent started: ${d.title}`, d.agent_role);
            setTasks(prev =>
              prev.map(t =>
                t.id === d.task_id ? { ...t, status: 'running' } : t,
              ),
            );
            break;
          }
          case 'action_completed': {
            const d = data as Record<string, string>;
            const content = d.content || '';
            const preview = content.length > 120 ? content.slice(0, 120) + '...' : content;
            addLog(
              d.has_tools === 'true' || (d as Record<string, unknown>).has_tools === true ? 'tool' : 'agent',
              preview || 'Agent reasoning...',
              d.agent,
            );
            break;
          }
          case 'tool_executed': {
            const d = data as Record<string, string>;
            addLog('tool', `Tool: ${d.tool}(${d.args || ''})`, d.agent);
            break;
          }
          case 'subtask_completed': {
            const d = data as Record<string, string>;
            addLog('agent', `${d.agent_role} agent completed (${d.tokens_used || '?'} tokens)`, d.agent_role);
            break;
          }
          case 'task_completed': {
            const d = data as Record<string, string>;
            const title = d.title || d.task_id || 'task';
            if ((d.status || '') === 'failed') {
              addLog('error', `Task failed: ${title}${d.error ? ` - ${d.error}` : ''}`);
            } else {
              addLog('task', `Task ${d.status}: ${title}`);
            }
            setTasks(prev =>
              prev.map(t =>
                t.id === d.task_id ? { ...t, status: d.status || 'done' } : t,
              ),
            );
            break;
          }
          case 'flow_completed':
            addLog('system', 'Flow execution completed');
            setFlow(prev => prev ? { ...prev, status: 'done' } : prev);
            loadFlow(false);
            break;
          case 'flow_failed': {
            const d = data as Record<string, string>;
            addLog('error', `Flow failed: ${d.error || 'Unknown error'}`);
            setFlow(prev => prev ? { ...prev, status: 'failed' } : prev);
            loadFlow(false);
            break;
          }
          case 'flow_stopped':
            addLog('system', 'Flow execution stopped');
            setFlow(prev => prev ? { ...prev, status: 'stopped' } : prev);
            loadFlow(false);
            break;
          case 'finding_created': {
            const d = data as Record<string, string>;
            setFindings(prev => [...prev, {
              id: String(Date.now()),
              severity: d.severity || 'info',
              title: d.title || '',
              description: d.description || '',
              evidence: d.evidence || '',
              task_title: d.task_title || '',
              created_at: new Date().toISOString(),
            }]);
            break;
          }
        }
      },
      () => {
        if (esRef.current?.readyState === EventSource.CLOSED) {
          addLog('system', 'Event stream closed');
        } else if (esRef.current?.readyState === EventSource.CONNECTING) {
          addLog('system', 'Event stream disconnected');
        }
      },
    );

    esRef.current = es;
    return () => {
      es.close();
      esRef.current = null;
    };
  }, [id, isFlowActive, addLog, loadFlow]);

  useEffect(() => {
    logEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [logs]);

  useEffect(() => {
    assistantLogEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [assistantLogs]);

  const openScopeEditor = useCallback(() => {
    if (!flow) return;
    setDraftTitle(flow.title);
    setDraftTarget(flow.target || '');
    setDraftDescription(flow.description || '');
    setError('');
    setEditingScope(true);
  }, [flow]);

  const handleSaveScope = async (e: FormEvent) => {
    e.preventDefault();
    if (!id) return;

    const nextTitle = draftTitle.trim();
    const nextTarget = draftTarget.trim();
    const nextDescription = draftDescription.trim();
    if (!nextTitle || !nextTarget) {
      setError('Add both a title and a target before saving.');
      return;
    }

    setSavingScope(true);
    setError('');
    try {
      const updated = await updateFlow(id, {
        title: nextTitle,
        target: nextTarget,
        description: nextDescription,
      });
      setFlow(updated);
      setDraftTitle(updated.title);
      setDraftTarget(updated.target || '');
      setDraftDescription(updated.description || '');
      setEditingScope(false);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to save flow scope');
    } finally {
      setSavingScope(false);
    }
  };

  const handleSendAutomation = async (e: FormEvent) => {
    e.preventDefault();
    if (!id || !flow) return;

    const command = automationDraft.trim();
    if (!command) return;

    addLog('user', command);
    setAutomationDraft('');

    setStarting(true);
    setError('');
    try {
      await updateFlow(id, { description: command });
      setFlow(prev => prev ? { ...prev, description: command } : prev);
      setDraftDescription(command);
      await startFlow(id);
      setFlow(prev => prev ? { ...prev, status: 'queued' } : prev);
      addLog('system', 'Flow queued for execution...');
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to start flow');
    } finally {
      setStarting(false);
    }
  };

  const handleStop = async () => {
    if (!id) return;
    setStopping(true);
    try {
      await stopFlow(id);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to stop flow');
    } finally {
      setStopping(false);
    }
  };

  const handleToggleAssistantAgents = async () => {
    if (!id || !assistant) return;
    setAssistantUpdating(true);
    setAssistantError('');
    try {
      const data = await updateAssistantSession(id, { use_agents: !assistant.use_agents });
      applyAssistantDetail(data);
      const detail = await getFlowDetail(id);
      applyFlowDetail(detail);
    } catch (err) {
      setAssistantError(err instanceof Error ? err.message : 'Failed to update assistant mode');
    } finally {
      setAssistantUpdating(false);
    }
  };

  const handleSendAssistantMessage = async (e: FormEvent) => {
    e.preventDefault();
    if (!id) return;

    const nextMessage = assistantDraft.trim();
    if (!nextMessage) {
      return;
    }

    setAssistantSending(true);
    setAssistantError('');
    try {
      const data = await sendAssistantMessage(id, {
        content: nextMessage,
        use_agents: assistant?.use_agents ?? true,
      });
      applyAssistantDetail(data);
      setAssistantDraft('');
      const detail = await getFlowDetail(id);
      applyFlowDetail(detail);
    } catch (err) {
      setAssistantError(err instanceof Error ? err.message : 'Failed to send assistant message');
    } finally {
      setAssistantSending(false);
    }
  };

  const handleOpenHtmlReport = useCallback(async () => {
    if (!flow || !id) return;

    // Fetch screenshot blobs and convert to data URLs for self-contained report.
    const ssArtifacts = artifacts.filter(a => a.kind === 'browser_screenshot');
    const screenshots: ScreenshotDataEntry[] = await Promise.all(
      ssArtifacts.map(async (art) => {
        const meta = parseMetadata(art.metadata);
        const entry: ScreenshotDataEntry = {
          artifactId: art.id,
          targetUrl: typeof meta?.target_url === 'string' ? meta.target_url : '',
          action: typeof meta?.action === 'string' ? meta.action : '',
          createdAt: art.created_at,
          dataUrl: '',
        };
        try {
          const blob = await fetchArtifactBlob(id, art.id);
          const buf = await blob.arrayBuffer();
          const bytes = new Uint8Array(buf);
          let binary = '';
          for (let i = 0; i < bytes.length; i++) {
            binary += String.fromCharCode(bytes[i]);
          }
          entry.dataUrl = `data:image/png;base64,${btoa(binary)}`;
        } catch {
          // Keep empty dataUrl — report shows placeholder.
        }
        return entry;
      }),
    );

    const { buildFlowHtmlReport } = await import('../lib/reportHtml');
    const html = buildFlowHtmlReport({
      flow,
      tasks,
      artifacts,
      searchLogs,
      terminalLogs,
      screenshots,
    });

    const blob = new Blob([html], { type: 'text/html;charset=utf-8' });
    const reportUrl = URL.createObjectURL(blob);
    const win = window.open(reportUrl, '_blank', 'noopener,noreferrer');

    // If popup is blocked, fall back to direct download.
    if (!win) {
      const link = document.createElement('a');
      link.href = reportUrl;
      link.download = `${flow.title.toLowerCase().replace(/[^a-z0-9]+/g, '_')}_report.html`;
      document.body.appendChild(link);
      link.click();
      document.body.removeChild(link);
      URL.revokeObjectURL(reportUrl);
      return;
    }

    window.setTimeout(() => URL.revokeObjectURL(reportUrl), 5 * 60_000);
  }, [flow, id, artifacts, tasks, searchLogs, terminalLogs]);

  const toggleTraceExpand = useCallback((entryId: string) => {
    setExpandedTraceIds(prev => {
      const next = new Set(prev);
      if (next.has(entryId)) {
        next.delete(entryId);
      } else {
        next.add(entryId);
      }
      return next;
    });
  }, []);

  const handleToggleDebugTrace = useCallback(async () => {
    if (!id) return;
    const nextDebug = !showDebugTrace;
    setShowDebugTrace(nextDebug);
    try {
      const data = await getFlowDetail(id, nextDebug);
      applyFlowDetail(data);
    } catch {
      // Revert on failure.
      setShowDebugTrace(!nextDebug);
    }
  }, [id, showDebugTrace, applyFlowDetail]);

  const completedTasks = useMemo(
    () => tasks.filter(t => t.status === 'done').length,
    [tasks],
  );
  const progress = useMemo(
    () => (tasks.length > 0 ? Math.round((completedTasks / tasks.length) * 100) : 0),
    [completedTasks, tasks.length],
  );
  const findingArtifacts = useMemo(
    () => artifacts.filter(entry => entry.kind === 'finding'),
    [artifacts],
  );
  const reportArtifacts = useMemo(
    () => artifacts.filter(entry => entry.kind === 'task_report_markdown'),
    [artifacts],
  );
  const screenshotArtifacts = useMemo(
    () => artifacts.filter(entry => entry.kind === 'browser_screenshot'),
    [artifacts],
  );
  const intelEntries = useMemo<IntelEntry[]>(
    () =>
      [
        ...searchLogs.map(entry => ({
          id: `search-${entry.id}`,
          createdAt: entry.created_at,
          sortTime: toSortTime(entry.created_at),
          kind: 'search' as const,
          toolName: entry.tool_name,
          provider: entry.provider,
          query: entry.query,
          target: entry.target,
          resultCount: entry.result_count,
          summary: entry.summary,
          metadata: entry.metadata,
        })),
        ...vectorStoreLogs.map(entry => ({
          id: `memory-${entry.id}`,
          createdAt: entry.created_at,
          sortTime: toSortTime(entry.created_at),
          kind: 'memory' as const,
          action: entry.action,
          query: entry.query,
          content: entry.content,
          resultCount: entry.result_count,
        })),
      ].sort((a, b) => a.sortTime - b.sortTime),
    [searchLogs, vectorStoreLogs],
  );
  const tabCounts = useMemo(
    () => ({
      live: logs.length,
      trace: traceEntries.length,
      intel: intelEntries.length,
      screenshots: screenshotArtifacts.length,
      reports: findingArtifacts.length + reportArtifacts.length,
      tasks: tasks.length,
    }),
    [logs.length, traceEntries.length, intelEntries.length, screenshotArtifacts.length, findingArtifacts.length, reportArtifacts.length, tasks.length],
  );


  if (loading) {
    return (
      <>
        <div className="page-header">
          <div className="page-title">Flow Details</div>
        </div>
        <div className="page-body" style={{ display: 'flex', justifyContent: 'center', padding: 80 }}>
          <div className="spinner spinner-lg" />
        </div>
      </>
    );
  }

  if (!flow) {
    return (
      <>
        <div className="page-header">
          <div className="page-title">Flow Not Found</div>
        </div>
        <div className="page-body">
          <p style={{ color: 'var(--text-muted)' }}>This flow does not exist or you do not have access.</p>
        </div>
      </>
    );
  }

  const renderEmptyState = (message: string) => (
    <div style={{ color: 'var(--text-muted)', textAlign: 'center', padding: '40px 0', fontFamily: 'var(--font-mono)', fontSize: '0.8125rem' }}>
      {message}
    </div>
  );

  return (
    <>
      <div className="page-header">
        <div style={{ display: 'flex', alignItems: 'center', gap: 16 }}>
          <button className="btn btn-ghost btn-icon" onClick={() => navigate('/flows')} title="Back to flows">
            {'<'}
          </button>
          <div>
            <div className="page-title">{flow.title}</div>
            {flow.target ? (
              <div style={{ color: 'var(--text-secondary)', fontSize: '0.8125rem', marginTop: 2, fontFamily: 'var(--font-mono)' }}>
                Target: {flow.target}
              </div>
            ) : (
              <div style={{ color: 'var(--warning)', fontSize: '0.8125rem', marginTop: 2 }}>
                No target defined yet.
              </div>
            )}
          </div>
        </div>
        <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
          <span className={`badge ${STATUS_CLASSES[flow.status] || 'badge-pending'}`}>
            <span className="badge-dot" />
            {flow.status}
          </span>
          {!isFlowActive && (
            <button className="btn btn-secondary" onClick={openScopeEditor} disabled={editingScope}>
              Edit Flow
            </button>
          )}
        </div>
      </div>

      {editingScope && (
        <div className="modal-overlay" onClick={() => !savingScope && setEditingScope(false)}>
          <div className="modal" onClick={(e) => e.stopPropagation()}>
            <div className="modal-header">
              <h2>{scopeMissing ? 'Add Flow Scope' : 'Edit Flow Scope'}</h2>
              <button className="btn btn-ghost btn-icon" onClick={() => setEditingScope(false)} disabled={savingScope}>
                x
              </button>
            </div>
            <form onSubmit={handleSaveScope}>
              <div className="modal-body" style={{ display: 'flex', flexDirection: 'column', gap: 20 }}>
                <div className="input-group">
                  <label htmlFor="flow-title-edit">Title</label>
                  <input
                    id="flow-title-edit"
                    className="input"
                    value={draftTitle}
                    onChange={(e) => setDraftTitle(e.target.value)}
                    placeholder="e.g. Acme Web App Assessment"
                    required
                    autoFocus
                  />
                </div>
                <div className="input-group">
                  <label htmlFor="flow-target-edit">Target</label>
                  <input
                    id="flow-target-edit"
                    className="input"
                    value={draftTarget}
                    onChange={(e) => setDraftTarget(e.target.value)}
                    placeholder="e.g. https://scanme.nmap.org or 192.168.1.1"
                    required
                  />
                  <div style={{ fontSize: '0.75rem', color: 'var(--text-muted)' }}>
                    The URL, hostname, or IP to test. All agents will always use this as the authorized target.
                  </div>
                </div>
                <div className="input-group">
                  <label htmlFor="flow-scope-edit">Scope &amp; Instructions</label>
                  <textarea
                    id="flow-scope-edit"
                    className="input"
                    value={draftDescription}
                    onChange={(e) => setDraftDescription(e.target.value)}
                    rows={5}
                    placeholder={`In scope: web app only
Out of scope: brute force, DoS, third-party domains
Goal: enumerate and validate web vulnerabilities`}
                    style={{ resize: 'vertical' }}
                  />
                  <div style={{ fontSize: '0.75rem', color: 'var(--text-muted)' }}>
                    What to test and how. The agents use this along with the target.
                  </div>
                </div>
              </div>
              <div className="modal-footer">
                <button type="button" className="btn btn-secondary" onClick={() => setEditingScope(false)} disabled={savingScope}>
                  Cancel
                </button>
                <button type="submit" className="btn btn-primary" disabled={savingScope}>
                  {savingScope ? 'Saving...' : 'Save Scope'}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}

      <div className="page-body">
        {error && <div className="alert alert-error" style={{ marginBottom: 20 }}>! {error}</div>}

        <div className="flow-detail-grid flow-detail-layout-v2" data-layout="chat-split">
          {/* ── Left: Chat Panel (Automation / Assistant) ── */}
          <div className="flow-chat-panel">
            <div className="flow-chat-container">
              <div className="flow-chat-header">
                <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                  {[
                    { key: 'automation', label: 'Chat' },
                    { key: 'assistant', label: 'Assistant' },
                  ].map(tab => (
                    <button
                      key={tab.key}
                      className={activeChatTab === tab.key ? 'btn btn-primary btn-sm' : 'btn btn-ghost btn-sm'}
                      onClick={() => setActiveChatTab(tab.key as ChatTab)}
                    >
                      {tab.label}
                    </button>
                  ))}
                </div>
                <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                  {activeChatTab === 'assistant' && (
                    <button
                      className={assistant?.use_agents ? 'btn btn-primary btn-sm' : 'btn btn-secondary btn-sm'}
                      onClick={handleToggleAssistantAgents}
                      disabled={assistantLoading || assistantSending || assistantUpdating || !assistant}
                    >
                      {assistantUpdating ? 'Updating...' : assistant?.use_agents ? 'Agents: On' : 'Agents: Off'}
                    </button>
                  )}
                  {activeChatTab === 'assistant' && (
                    <span className={`badge badge-sm ${STATUS_CLASSES[assistant?.status || 'idle'] || 'badge-pending'}`}>
                      <span className="badge-dot" />
                      {assistant?.status || (assistantLoading ? 'loading' : 'idle')}
                    </span>
                  )}
                  {activeChatTab === 'automation' && (
                    <span className={`badge badge-sm ${findings.length > 0 ? 'badge-running' : 'badge-pending'}`}>
                      {findings.length} finding{findings.length !== 1 ? 's' : ''}
                    </span>
                  )}
                </div>
              </div>

              <div className="flow-chat-body">
                {activeChatTab === 'automation' && (() => {
                  const userMessages = logs.filter(l => l.type === 'user');
                  const chatItems: ({ kind: 'user'; ts: number; entry: LogEntry } | { kind: 'finding'; ts: number; entry: FindingDTO })[] = [
                    ...userMessages.map(l => ({ kind: 'user' as const, ts: l.ts, entry: l })),
                    ...findings.map(f => ({ kind: 'finding' as const, ts: new Date(f.created_at).getTime(), entry: f })),
                  ];
                  chatItems.sort((a, b) => a.ts - b.ts);

                  if (chatItems.length === 0) {
                    return (
                      <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center', height: '100%', color: 'var(--text-muted)', textAlign: 'center', padding: 40 }}>
                        <div style={{ fontSize: '2.5rem', marginBottom: 16, opacity: 0.3 }}>S</div>
                        <p style={{ fontSize: '0.9rem', fontWeight: 500, marginBottom: 8 }}>
                          {isFlowActive ? 'Scanning for vulnerabilities...' : 'No findings yet'}
                        </p>
                        <p style={{ fontSize: '0.8125rem' }}>
                          {isFlowActive
                            ? 'Findings will appear here as agents discover them.'
                            : 'Type a command below to start a scan.'}
                        </p>
                      </div>
                    );
                  }

                  const severityColors: Record<string, string> = {
                    critical: '#ef4444',
                    high: '#f97316',
                    medium: '#eab308',
                    low: '#3b82f6',
                    info: '#6b7280',
                  };

                  return (
                    <>
                      <div style={{ display: 'flex', flexDirection: 'column', gap: 12, padding: '4px 0' }}>
                        {chatItems.map(item => {
                          if (item.kind === 'user') {
                            const log = item.entry as LogEntry;
                            return (
                              <div key={`user-${log.id}`} className="flow-chat-bubble flow-chat-user">
                                <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 6 }}>
                                  <span style={{ fontSize: '0.75rem', fontWeight: 600, color: 'var(--accent)' }}>You</span>
                                  <span className="flow-log-time">{log.time}</span>
                                </div>
                                <pre style={{ margin: 0, whiteSpace: 'pre-wrap', color: 'var(--text-primary)', fontSize: '0.8125rem', fontFamily: 'var(--font-mono)' }}>
                                  {log.message}
                                </pre>
                              </div>
                            );
                          }
                          const finding = item.entry as FindingDTO;
                          const color = severityColors[finding.severity] || severityColors.info;
                          return (
                            <div key={`finding-${finding.id}`} style={{
                              border: `1px solid ${color}33`,
                              borderLeft: `3px solid ${color}`,
                              borderRadius: 6,
                              padding: '12px 14px',
                              background: `${color}08`,
                            }}>
                              <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 6 }}>
                                <span style={{
                                  fontSize: '0.6875rem',
                                  fontWeight: 700,
                                  textTransform: 'uppercase',
                                  letterSpacing: '0.05em',
                                  color,
                                  background: `${color}18`,
                                  padding: '2px 6px',
                                  borderRadius: 3,
                                }}>
                                  {finding.severity}
                                </span>
                                <span style={{ fontSize: '0.75rem', color: 'var(--text-muted)' }}>
                                  {finding.task_title}
                                </span>
                              </div>
                              <div style={{ fontWeight: 500, fontSize: '0.875rem', marginBottom: 4 }}>
                                {finding.title}
                              </div>
                              <div style={{ fontSize: '0.8125rem', color: 'var(--text-secondary)', lineHeight: 1.5 }}>
                                {finding.description}
                              </div>
                              {finding.evidence && (
                                <pre style={{
                                  marginTop: 8,
                                  padding: '8px 10px',
                                  background: 'var(--bg-tertiary)',
                                  borderRadius: 4,
                                  fontSize: '0.75rem',
                                  fontFamily: 'var(--font-mono)',
                                  color: 'var(--text-secondary)',
                                  whiteSpace: 'pre-wrap',
                                  wordBreak: 'break-all',
                                  maxHeight: 120,
                                  overflow: 'auto',
                                }}>
                                  {finding.evidence}
                                </pre>
                              )}
                            </div>
                          );
                        })}
                      </div>
                      <div ref={logEndRef} />
                    </>
                  );
                })()}

                {activeChatTab === 'assistant' && (
                  <>
                    {assistantError && (
                      <div className="alert alert-error" style={{ margin: '0 0 12px 0' }}>
                        ! {assistantError}
                      </div>
                    )}
                    {assistantLoading ? (
                      <div style={{ display: 'flex', justifyContent: 'center', padding: 32 }}>
                        <div className="spinner" />
                      </div>
                    ) : assistantLogs.length === 0 ? (
                      <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center', height: '100%', color: 'var(--text-muted)', textAlign: 'center', padding: 40 }}>
                        <div style={{ fontSize: '2.5rem', marginBottom: 16, opacity: 0.3 }}>S</div>
                        <p style={{ fontSize: '0.9rem', fontWeight: 500, marginBottom: 8 }}>No assistant messages yet</p>
                        <p style={{ fontSize: '0.8125rem' }}>
                          Ask it to inspect the target, refine scope, explain findings, or delegate research.
                        </p>
                      </div>
                    ) : (
                      assistantLogs.map(entry => {
                        const metadata = parseMetadata(entry.metadata);
                        const toolName = typeof metadata?.tool === 'string' ? metadata.tool : '';
                        const isUser = entry.role === 'user';

                        return (
                          <div key={entry.id} className={`flow-chat-bubble ${isUser ? 'flow-chat-user' : 'flow-chat-ai'}`}>
                            <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 6, flexWrap: 'wrap' }}>
                              <span className="flow-log-agent" style={{ color: AGENT_COLORS[entry.agent_role] || AGENT_COLORS.assistant, minWidth: 'auto' }}>
                                {AGENT_ICONS[entry.agent_role] || AGENT_ICONS.assistant} {entry.agent_role}
                              </span>
                              <span className="flow-log-time">{new Date(entry.created_at).toLocaleTimeString()}</span>
                              {toolName && (
                                <span className="badge badge-sm badge-running">
                                  <span className="badge-dot" />
                                  {toolName}
                                </span>
                              )}
                            </div>
                            <pre style={{ margin: 0, whiteSpace: 'pre-wrap', color: 'var(--text-secondary)', fontSize: '0.8125rem', fontFamily: 'var(--font-mono)' }}>
                              {entry.content}
                            </pre>
                          </div>
                        );
                      })
                    )}
                    <div ref={assistantLogEndRef} />
                  </>
                )}
              </div>

              <div className="flow-chat-input">
                {activeChatTab === 'assistant' ? (
                  <form onSubmit={handleSendAssistantMessage} style={{ display: 'flex', gap: 10, alignItems: 'flex-end' }}>
                    <textarea
                      className="input"
                      rows={2}
                      value={assistantDraft}
                      onChange={(e) => setAssistantDraft(e.target.value)}
                      onKeyDown={(e) => {
                        if (e.key === 'Enter' && !e.shiftKey) {
                          e.preventDefault();
                          if (assistantDraft.trim() && !assistantSending && !assistantLoading) {
                            handleSendAssistantMessage(e as unknown as FormEvent);
                          }
                        }
                      }}
                      placeholder="Ask the assistant to inspect the target, explain findings, or delegate research."
                      disabled={assistantLoading || assistantSending}
                      style={{ resize: 'none', flex: 1 }}
                    />
                    <button className="btn btn-primary" type="submit" disabled={assistantLoading || assistantSending || !assistantDraft.trim()}>
                      {assistantSending ? (
                        <div className="spinner" style={{ width: 16, height: 16 }} />
                      ) : 'Send'}
                    </button>
                  </form>
                ) : isFlowActive ? (
                  <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
                    <div className="spinner" style={{ width: 14, height: 14 }} />
                    <span style={{ flex: 1, color: 'var(--text-muted)', fontSize: '0.8125rem' }}>
                      Sentrix is working...
                    </span>
                    <button className="btn btn-danger btn-sm" onClick={handleStop} disabled={stopping}>
                      {stopping ? 'Stopping...' : 'Stop'}
                    </button>
                  </div>
                ) : (
                  <div>
                    {flow.target && (
                      <div style={{ fontSize: '0.75rem', color: 'var(--text-muted)', marginBottom: 6, fontFamily: 'var(--font-mono)' }}>
                        Target: {flow.target}
                      </div>
                    )}
                    <form onSubmit={handleSendAutomation} style={{ display: 'flex', gap: 10, alignItems: 'flex-end' }}>
                      <textarea
                        className="input"
                        rows={2}
                        value={automationDraft}
                        onChange={(e) => setAutomationDraft(e.target.value)}
                        onKeyDown={(e) => {
                          if (e.key === 'Enter' && !e.shiftKey) {
                            e.preventDefault();
                            if (automationDraft.trim() && !starting) {
                              handleSendAutomation(e as unknown as FormEvent);
                            }
                          }
                        }}
                        placeholder={flow.target ? `e.g. "sql injection test" or "scan open ports"` : 'Set a target first via Edit Flow'}
                        disabled={starting || !flow.target}
                        style={{ resize: 'none', flex: 1 }}
                      />
                      <button className="btn btn-primary" type="submit" disabled={starting || !automationDraft.trim() || !flow.target}>
                        {starting ? (
                          <div className="spinner" style={{ width: 16, height: 16 }} />
                        ) : 'Send'}
                      </button>
                    </form>
                  </div>
                )}
              </div>
            </div>
          </div>

          {/* ── Right: Activity Tabs ── */}
          <div className="flow-log-panel">
            <div className="flow-log-container">
              <div className="flow-log-header">
                <div style={{ display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap' }}>
                  {[
                    { key: 'live', label: 'Live' },
                    { key: 'trace', label: 'Trace' },
                    { key: 'intel', label: 'Intel' },
                    { key: 'screenshots', label: 'Screenshots' },
                    { key: 'reports', label: 'Reports' },
                    { key: 'tasks', label: 'Tasks' },
                  ].map(tab => (
                    <button
                      key={tab.key}
                      className={activeTab === tab.key ? 'btn btn-primary btn-sm' : 'btn btn-ghost btn-sm'}
                      onClick={() => setActiveTab(tab.key as FlowTab)}
                      style={{ minWidth: 'fit-content' }}
                    >
                      {tab.label} ({tabCounts[tab.key as keyof typeof tabCounts]})
                    </button>
                  ))}
                </div>
                <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                  <button
                    className="btn btn-secondary btn-sm"
                    onClick={handleOpenHtmlReport}
                    title="Open a styled HTML report in a new tab"
                  >
                    HTML Report
                  </button>
                  {isFlowActive && (
                    <span className="badge badge-running badge-sm">
                      <span className="badge-dot" />
                      Live
                    </span>
                  )}
                </div>
              </div>
              <div className="flow-log-body">
                {activeTab === 'live' && (
                  <>
                    {logs.length === 0 ? (
                      renderEmptyState('Waiting for execution to begin...')
                    ) : (
                      logs.map(entry => (
                        <div key={entry.id} className={`flow-log-entry flow-log-${entry.type}`}>
                          <span className="flow-log-time">{entry.time}</span>
                          {entry.agent && (
                            <span
                              className="flow-log-agent"
                              style={{ color: AGENT_COLORS[entry.agent] || 'var(--text-secondary)' }}
                            >
                              {AGENT_ICONS[entry.agent] || 'A'} {entry.agent}
                            </span>
                          )}
                          <span className="flow-log-msg">{entry.message}</span>
                        </div>
                      ))
                    )}
                    <div ref={logEndRef} />
                  </>
                )}

                {activeTab === 'trace' && (
                  traceEntries.length === 0 ? renderEmptyState('No execution trace recorded yet.') : (
                    <>
                      <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', marginBottom: 16, alignItems: 'center' }}>
                        <span style={{ color: 'var(--text-muted)', fontSize: '0.8125rem' }}>
                          {traceEntries.length} entries
                        </span>
                        <button
                          className={showDebugTrace ? 'btn btn-primary btn-sm' : 'btn btn-ghost btn-sm'}
                          onClick={handleToggleDebugTrace}
                          style={{ marginLeft: 'auto' }}
                        >
                          {showDebugTrace ? 'Hide internal/debug' : 'Show internal/debug'}
                        </button>
                      </div>

                      {traceEntries.map(entry => {
                        const expanded = expandedTraceIds.has(entry.id);
                        const hasLongContent = (entry.content?.length ?? 0) > 220;
                        const kindClass =
                          entry.kind === 'agent_event' ? 'flow-log-agent'
                          : entry.kind === 'terminal' ? 'flow-log-tool'
                          : 'flow-log-agent';

                        const kindBadge =
                          entry.kind === 'agent_event' ? 'badge-pending'
                          : entry.kind === 'terminal' ? 'badge-running'
                          : 'badge-done';

                        const kindLabel =
                          entry.kind === 'agent_event' ? (entry.event_type || 'event')
                          : entry.kind === 'terminal' ? 'terminal'
                          : (entry.role || 'transcript');

                        return (
                          <div
                            key={entry.id}
                            className={`flow-log-entry ${kindClass}`}
                            style={{
                              display: 'block',
                              opacity: entry.debug ? 0.65 : 1,
                              borderLeft: entry.debug ? '3px solid var(--text-muted)' : undefined,
                            }}
                          >
                            <div
                              style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 6, flexWrap: 'wrap', cursor: hasLongContent ? 'pointer' : 'default' }}
                              onClick={() => hasLongContent && toggleTraceExpand(entry.id)}
                            >
                              <span className="flow-log-time">{new Date(entry.created_at).toLocaleTimeString()}</span>
                              <span className={`badge badge-sm ${kindBadge}`}>
                                <span className="badge-dot" />
                                {kindLabel}
                              </span>
                              {entry.agent_role && (
                                <span className="flow-log-agent" style={{ color: AGENT_COLORS[entry.agent_role] || 'var(--text-secondary)' }}>
                                  {AGENT_ICONS[entry.agent_role] || 'A'} {entry.agent_role}
                                </span>
                              )}
                              {entry.kind === 'transcript' && entry.chain_type && (
                                <span className="badge badge-sm badge-running">
                                  <span className="badge-dot" />
                                  {humanizeChainType(entry.chain_type)}
                                </span>
                              )}
                              {entry.task_label && (
                                <span style={{ color: 'var(--text-secondary)', fontSize: '0.75rem' }}>
                                  {entry.task_label}
                                </span>
                              )}
                              {entry.debug && (
                                <span className="badge badge-sm badge-pending">
                                  <span className="badge-dot" />
                                  debug
                                </span>
                              )}
                              {hasLongContent && (
                                <span style={{ color: 'var(--text-muted)', fontSize: '0.75rem', marginLeft: 'auto' }}>
                                  {expanded ? 'collapse' : 'expand'}
                                </span>
                              )}
                            </div>

                            {entry.kind === 'terminal' ? (
                              <>
                                {entry.command && (
                                  <code style={{ color: 'var(--text-primary)', fontSize: '0.8125rem', display: 'block', marginBottom: 6 }}>
                                    $ {entry.command}
                                  </code>
                                )}
                                {(expanded || !hasLongContent) ? (
                                  <>
                                    {entry.stdout && (
                                      <pre style={{ margin: 0, whiteSpace: 'pre-wrap', color: 'var(--text-secondary)', fontSize: '0.8125rem', fontFamily: 'var(--font-mono)' }}>
                                        {entry.stdout}
                                      </pre>
                                    )}
                                    {entry.stderr && (
                                      <pre style={{ margin: 0, whiteSpace: 'pre-wrap', color: 'var(--danger, #ef4444)', fontSize: '0.8125rem', fontFamily: 'var(--font-mono)' }}>
                                        {entry.stderr}
                                      </pre>
                                    )}
                                  </>
                                ) : (
                                  <pre style={{ margin: 0, whiteSpace: 'pre-wrap', color: 'var(--text-secondary)', fontSize: '0.8125rem', fontFamily: 'var(--font-mono)' }}>
                                    {entry.summary}
                                  </pre>
                                )}
                              </>
                            ) : (
                              <pre style={{ margin: 0, whiteSpace: 'pre-wrap', color: 'var(--text-secondary)', fontSize: '0.8125rem', fontFamily: 'var(--font-mono)' }}>
                                {expanded || !hasLongContent ? entry.content : entry.summary}
                              </pre>
                            )}
                          </div>
                        );
                      })}
                    </>
                  )
                )}

                {activeTab === 'intel' && (
                  intelEntries.length === 0 ? renderEmptyState('No search or memory activity recorded yet.') : (
                    <>
                      <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', marginBottom: 16 }}>
                        <span className="badge badge-sm badge-done">
                          <span className="badge-dot" />
                          Searches {searchLogs.length}
                        </span>
                        <span className="badge badge-sm badge-running">
                          <span className="badge-dot" />
                          Memory {vectorStoreLogs.length}
                        </span>
                      </div>

                      {intelEntries.map(entry => {
                        if (entry.kind === 'search') {
                          return (
                            <div key={entry.id} className="flow-log-entry flow-log-task" style={{ display: 'block' }}>
                              <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 6, flexWrap: 'wrap' }}>
                                <span className="flow-log-time">{new Date(entry.createdAt).toLocaleTimeString()}</span>
                                <span className="badge badge-sm badge-done">
                                  <span className="badge-dot" />
                                  search
                                </span>
                                <span className="badge badge-sm badge-done">
                                  <span className="badge-dot" />
                                  {entry.toolName}
                                </span>
                                {entry.provider && (
                                  <span style={{ color: 'var(--text-secondary)', fontSize: '0.75rem' }}>
                                    via {entry.provider}
                                  </span>
                                )}
                                <span style={{ color: 'var(--text-muted)', fontSize: '0.75rem' }}>
                                  {entry.resultCount} result{entry.resultCount === 1 ? '' : 's'}
                                </span>
                                {(() => {
                                  const meta = entry.metadata ? parseMetadata(entry.metadata) : null;
                                  if (typeof meta?.screenshot_artifact_id === 'string' && meta.screenshot_artifact_id) {
                                    return (
                                      <span className="badge badge-sm badge-running">
                                        <span className="badge-dot" />
                                        screenshot
                                      </span>
                                    );
                                  }
                                  return null;
                                })()}
                              </div>
                              {entry.query && (
                                <div style={{ color: 'var(--text-primary)', marginBottom: 6, fontSize: '0.8125rem' }}>
                                  Query: <code>{entry.query}</code>
                                </div>
                              )}
                              {entry.target && (
                                <div style={{ color: 'var(--text-primary)', marginBottom: 6, fontSize: '0.8125rem' }}>
                                  Target: <code>{entry.target}</code>
                                </div>
                              )}
                              <div className="flow-log-msg" style={{ display: 'block', whiteSpace: 'pre-wrap' }}>{entry.summary}</div>
                              {(() => {
                                const meta = entry.metadata ? parseMetadata(entry.metadata) : null;
                                const screenshotId = typeof meta?.screenshot_artifact_id === 'string' ? meta.screenshot_artifact_id : '';
                                if (!screenshotId || !id) return null;
                                return (
                                  <button
                                    className="btn btn-ghost btn-sm"
                                    style={{ marginTop: 8 }}
                                    onClick={async () => {
                                      try {
                                        const blob = await fetchArtifactBlob(id, screenshotId);
                                        const objUrl = URL.createObjectURL(blob);
                                        window.open(objUrl, '_blank');
                                        setTimeout(() => URL.revokeObjectURL(objUrl), 5 * 60_000);
                                      } catch { /* ignore */ }
                                    }}
                                  >
                                    Open screenshot
                                  </button>
                                );
                              })()}
                            </div>
                          );
                        }

                        return (
                          <div key={entry.id} className="flow-log-entry flow-log-system" style={{ display: 'block' }}>
                            <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 6, flexWrap: 'wrap' }}>
                              <span className="flow-log-time">{new Date(entry.createdAt).toLocaleTimeString()}</span>
                              <span className="badge badge-sm badge-running">
                                <span className="badge-dot" />
                                memory
                              </span>
                              <span className="badge badge-sm badge-running">
                                <span className="badge-dot" />
                                {entry.action}
                              </span>
                              <span style={{ color: 'var(--text-muted)', fontSize: '0.75rem' }}>
                                {entry.resultCount} result{entry.resultCount === 1 ? '' : 's'}
                              </span>
                            </div>
                            {entry.query && (
                              <div style={{ color: 'var(--text-primary)', marginBottom: 6, fontSize: '0.8125rem' }}>
                                Query: <code>{entry.query}</code>
                              </div>
                            )}
                            {entry.content && (
                              <div className="flow-log-msg" style={{ display: 'block', whiteSpace: 'pre-wrap' }}>{entry.content}</div>
                            )}
                          </div>
                        );
                      })}
                    </>
                  )
                )}

                {activeTab === 'screenshots' && (() => {
                  if (!browserCapabilities.screenshots_enabled) {
                    return renderEmptyState(
                      'Screenshots are unavailable in this environment. Configure SCRAPER_PUBLIC_URL or SCRAPER_PRIVATE_URL to enable browser screenshots.',
                    );
                  }
                  if (screenshotArtifacts.length === 0) {
                    const hasBrowserActivity = searchLogs.some(l => l.tool_name === 'browser');
                    if (hasBrowserActivity) {
                      return renderEmptyState(
                        'Browser activity was recorded in this flow, but no screenshot was captured.',
                      );
                    }
                    return renderEmptyState('This flow has not used the browser tool yet.');
                  }
                  return (
                    <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fill, minmax(280px, 1fr))', gap: 12 }}>
                      {screenshotArtifacts.map(entry => {
                        const metadata = parseMetadata(entry.metadata);
                        const targetUrl = typeof metadata?.target_url === 'string' ? metadata.target_url : '';
                        const action = typeof metadata?.action === 'string' ? metadata.action : '';
                        const objUrl = screenshotUrls[entry.id];

                        return (
                          <div
                            key={entry.id}
                            className="flow-log-entry flow-log-task"
                            style={{
                              display: 'block',
                              borderRadius: 10,
                              overflow: 'hidden',
                            }}
                          >
                            {objUrl ? (
                              <img
                                src={objUrl}
                                alt={`Screenshot of ${targetUrl}`}
                                style={{
                                  width: '100%',
                                  maxHeight: 200,
                                  objectFit: 'cover',
                                  objectPosition: 'top',
                                  borderRadius: 6,
                                  marginBottom: 10,
                                  background: 'var(--surface-0)',
                                }}
                              />
                            ) : (
                              <div style={{ height: 120, display: 'flex', alignItems: 'center', justifyContent: 'center', color: 'var(--text-muted)', fontSize: '0.75rem', background: 'var(--surface-0)', borderRadius: 6, marginBottom: 10 }}>
                                Loading...
                              </div>
                            )}
                            <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 6, flexWrap: 'wrap' }}>
                              <span className="flow-log-time">{new Date(entry.created_at).toLocaleTimeString()}</span>
                              <span className="badge badge-sm badge-running">
                                <span className="badge-dot" />
                                {action}
                              </span>
                            </div>
                            {targetUrl && (
                              <div style={{ color: 'var(--text-secondary)', fontSize: '0.75rem', marginBottom: 8, wordBreak: 'break-all' }}>
                                {targetUrl}
                              </div>
                            )}
                            <div style={{ display: 'flex', gap: 8 }}>
                              {objUrl && (
                                <>
                                  <button
                                    className="btn btn-ghost btn-sm"
                                    onClick={() => window.open(objUrl, '_blank')}
                                  >
                                    Open
                                  </button>
                                  <a
                                    className="btn btn-ghost btn-sm"
                                    href={objUrl}
                                    download={`screenshot_${entry.id}.png`}
                                  >
                                    Download
                                  </a>
                                </>
                              )}
                            </div>
                          </div>
                        );
                      })}
                    </div>
                  );
                })()}

                {activeTab === 'reports' && (
                  (findingArtifacts.length + reportArtifacts.length) === 0 ? renderEmptyState('No reports or findings have been persisted yet.') : (
                    <>
                      <div
                        style={{
                          display: 'grid',
                          gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))',
                          gap: 12,
                          marginBottom: 16,
                        }}
                      >
                        <div className="flow-log-entry flow-log-task" style={{ display: 'block' }}>
                          <div style={{ color: 'var(--text-muted)', fontSize: '0.75rem', marginBottom: 6 }}>Findings</div>
                          <div style={{ fontSize: '1.4rem', fontWeight: 700 }}>{findingArtifacts.length}</div>
                        </div>
                        <div className="flow-log-entry flow-log-system" style={{ display: 'block' }}>
                          <div style={{ color: 'var(--text-muted)', fontSize: '0.75rem', marginBottom: 6 }}>Task Reports</div>
                          <div style={{ fontSize: '1.4rem', fontWeight: 700 }}>{reportArtifacts.length}</div>
                        </div>
                      </div>

                      {[...findingArtifacts, ...reportArtifacts].sort((a, b) => a.created_at.localeCompare(b.created_at)).map(entry => {
                        const metadata = parseMetadata(entry.metadata);
                        const severity = typeof metadata?.severity === 'string' ? metadata.severity : '';
                        const title = typeof metadata?.title === 'string' ? metadata.title : entry.task_title;
                        const note = typeof metadata?.message === 'string' ? metadata.message : '';

                        return (
                          <div key={entry.id} className="flow-log-entry flow-log-task" style={{ display: 'block' }}>
                            <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 6, flexWrap: 'wrap' }}>
                              <span className="flow-log-time">{new Date(entry.created_at).toLocaleTimeString()}</span>
                              <span className={`badge badge-sm ${entry.kind === 'finding' ? 'badge-failed' : 'badge-done'}`}>
                                <span className="badge-dot" />
                                {entry.kind === 'finding' ? 'finding' : 'task report'}
                              </span>
                              {severity && (
                                <span className="badge badge-sm badge-running">
                                  <span className="badge-dot" />
                                  {severity}
                                </span>
                              )}
                              <span style={{ color: 'var(--text-secondary)', fontSize: '0.75rem' }}>{entry.task_title}</span>
                            </div>
                            <div style={{ color: 'var(--text-primary)', marginBottom: 6, fontSize: '0.9rem', fontWeight: 600 }}>
                              {title}
                            </div>
                            {note && (
                              <div style={{ color: 'var(--text-secondary)', marginBottom: 8, fontSize: '0.8125rem' }}>
                                {note}
                              </div>
                            )}
                            {entry.content && (
                              <pre style={{ margin: 0, whiteSpace: 'pre-wrap', color: 'var(--text-secondary)', fontSize: '0.8125rem', fontFamily: 'var(--font-mono)' }}>
                                {entry.content}
                              </pre>
                            )}
                            {!entry.content && entry.file_path && (
                              <div style={{ color: 'var(--text-secondary)', fontSize: '0.8125rem' }}>
                                File: <code>{entry.file_path}</code>
                              </div>
                            )}
                          </div>
                        );
                      })}
                    </>
                  )
                )}

                {activeTab === 'tasks' && (
                  tasks.length === 0 ? renderEmptyState('No tasks yet. Start the flow to begin execution.') : (
                    <>
                      <div
                        style={{
                          display: 'grid',
                          gridTemplateColumns: 'repeat(auto-fit, minmax(140px, 1fr))',
                          gap: 12,
                          marginBottom: 16,
                        }}
                      >
                        <div className="flow-log-entry flow-log-system" style={{ display: 'block' }}>
                          <div style={{ color: 'var(--text-muted)', fontSize: '0.75rem', marginBottom: 6 }}>Total</div>
                          <div style={{ fontSize: '1.4rem', fontWeight: 700 }}>{tasks.length}</div>
                        </div>
                        <div className="flow-log-entry flow-log-task" style={{ display: 'block' }}>
                          <div style={{ color: 'var(--text-muted)', fontSize: '0.75rem', marginBottom: 6 }}>Completed</div>
                          <div style={{ fontSize: '1.4rem', fontWeight: 700, color: 'var(--success)' }}>{completedTasks}</div>
                        </div>
                        <div className="flow-log-entry flow-log-task" style={{ display: 'block' }}>
                          <div style={{ color: 'var(--text-muted)', fontSize: '0.75rem', marginBottom: 6 }}>Running</div>
                          <div style={{ fontSize: '1.4rem', fontWeight: 700, color: 'var(--info)' }}>
                            {tasks.filter(t => t.status === 'running').length}
                          </div>
                        </div>
                        <div className="flow-log-entry flow-log-task" style={{ display: 'block' }}>
                          <div style={{ color: 'var(--text-muted)', fontSize: '0.75rem', marginBottom: 6 }}>Failed</div>
                          <div style={{ fontSize: '1.4rem', fontWeight: 700, color: 'var(--danger)' }}>
                            {tasks.filter(t => t.status === 'failed').length}
                          </div>
                        </div>
                      </div>

                      {tasks.length > 0 && (
                        <div style={{ marginBottom: 16 }}>
                          <div className="flow-progress-bar">
                            <div className="flow-progress-fill" style={{ width: `${progress}%` }} />
                          </div>
                          <div style={{ display: 'flex', justifyContent: 'space-between', marginTop: 6 }}>
                            <span style={{ fontSize: '0.75rem', color: 'var(--text-muted)' }}>
                              {completedTasks} of {tasks.length} tasks complete
                            </span>
                            <span style={{ fontSize: '0.75rem', color: 'var(--text-muted)' }}>
                              {progress}%
                            </span>
                          </div>
                        </div>
                      )}

                      {tasks.map((task, idx) => (
                        <div
                          key={task.id}
                          className="flow-log-entry flow-log-task"
                          style={{
                            display: 'block',
                            borderLeft: `3px solid ${
                              task.status === 'done' ? 'var(--success)'
                              : task.status === 'running' ? 'var(--info)'
                              : task.status === 'failed' ? 'var(--danger)'
                              : 'var(--border)'
                            }`,
                          }}
                        >
                          <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 8, flexWrap: 'wrap' }}>
                            <span style={{
                              width: 24,
                              height: 24,
                              borderRadius: '50%',
                              display: 'inline-flex',
                              alignItems: 'center',
                              justifyContent: 'center',
                              fontSize: '0.75rem',
                              fontWeight: 700,
                              flexShrink: 0,
                              background: task.status === 'done' ? 'var(--success)'
                                : task.status === 'running' ? 'var(--info)'
                                : task.status === 'failed' ? 'var(--danger)'
                                : 'var(--surface-2)',
                              color: task.status === 'pending' ? 'var(--text-muted)' : '#fff',
                            }}>
                              {task.status === 'done' ? '\u2713' : task.status === 'running' ? '\u25B6' : task.status === 'failed' ? '\u2717' : String(idx + 1)}
                            </span>
                            <span style={{ color: 'var(--text-primary)', fontWeight: 600, fontSize: '0.875rem', flex: 1 }}>
                              {task.title}
                            </span>
                            <span className={`badge badge-sm ${STATUS_CLASSES[task.status] || 'badge-pending'}`}>
                              <span className="badge-dot" />
                              {task.status}
                            </span>
                          </div>

                          {task.description && (
                            <div style={{ color: 'var(--text-secondary)', fontSize: '0.8125rem', marginBottom: 8, paddingLeft: 34 }}>
                              {task.description}
                            </div>
                          )}

                          {task.subtasks && task.subtasks.length > 0 && (
                            <div style={{ paddingLeft: 34, display: 'flex', flexDirection: 'column', gap: 8, marginTop: 4 }}>
                              <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}>
                                {task.subtasks.map(st => (
                                  <span
                                    key={st.id}
                                    className="flow-agent-chip"
                                    style={{ borderColor: AGENT_COLORS[st.agent_role] || 'var(--border)' }}
                                  >
                                    {AGENT_ICONS[st.agent_role] || 'A'} {st.agent_role}
                                  </span>
                                ))}
                              </div>
                              {task.subtasks.map((st, stIdx) => (
                                <div
                                  key={st.id}
                                  style={{
                                    border: '1px solid var(--border)',
                                    borderRadius: 8,
                                    padding: '8px 12px',
                                    background: 'var(--surface-1)',
                                  }}
                                >
                                  <div style={{ display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap', marginBottom: 4 }}>
                                    <span style={{ color: 'var(--text-muted)', fontSize: '0.75rem', fontWeight: 600 }}>
                                      {idx + 1}.{stIdx + 1}
                                    </span>
                                    <span
                                      className="flow-log-agent"
                                      style={{ color: AGENT_COLORS[st.agent_role] || 'var(--text-secondary)' }}
                                    >
                                      {AGENT_ICONS[st.agent_role] || 'A'} {st.agent_role}
                                    </span>
                                    <span className={`badge badge-sm ${STATUS_CLASSES[st.status] || 'badge-pending'}`}>
                                      <span className="badge-dot" />
                                      {st.status}
                                    </span>
                                  </div>
                                  <div style={{ color: 'var(--text-primary)', fontWeight: 600, fontSize: '0.8125rem' }}>
                                    {st.title}
                                  </div>
                                  {st.description && (
                                    <div style={{ color: 'var(--text-muted)', fontSize: '0.75rem', marginTop: 4 }}>
                                      {st.description}
                                    </div>
                                  )}
                                </div>
                              ))}
                            </div>
                          )}
                        </div>
                      ))}
                    </>
                  )
                )}
              </div>
            </div>
          </div>
        </div>
      </div>
    </>
  );
}
