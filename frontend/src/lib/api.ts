const BASE = '/api/v1';

export interface AuthResponse {
  access_token: string;
  refresh_token: string;
  expires_at: string;
  user: UserDTO;
}

export interface UserDTO {
  id: string;
  email: string;
  display_name: string;
  role: string;
}

export interface FlowDTO {
  id: string;
  title: string;
  description: string;
  target: string;
  status: string;
  config: string;
  created_at: string;
  updated_at: string;
}

export interface APITokenDTO {
  id: string;
  label: string;
  last_used_at: string | null;
  expires_at: string | null;
  created_at: string;
}

export interface ProviderDTO {
  id: string;
  provider_type: string;
  model_name: string;
  base_url: string | null;
  is_default: boolean;
  has_api_key: boolean;
  created_at: string;
  updated_at: string;
}

class APIError extends Error {
  status: number;
  constructor(message: string, status: number) {
    super(message);
    this.status = status;
    this.name = 'APIError';
  }
}

function getStoredToken(): string | null {
  return localStorage.getItem('sentrix_token');
}

function setStoredToken(token: string) {
  localStorage.setItem('sentrix_token', token);
}

function clearStoredToken() {
  localStorage.removeItem('sentrix_token');
}

async function request<T>(path: string, options: RequestInit = {}): Promise<T> {
  const token = getStoredToken();
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    ...(options.headers as Record<string, string> || {}),
  };
  if (token) {
    headers['Authorization'] = `Bearer ${token}`;
  }

  const res = await fetch(`${BASE}${path}`, {
    ...options,
    headers,
  });

  if (!res.ok) {
    const body = await res.json().catch(() => ({ error: res.statusText }));
    throw new APIError(body.error || 'Request failed', res.status);
  }

  return res.json();
}

// ── Auth ────────────────────────────────────────────────────────────

export async function login(email: string, password: string): Promise<AuthResponse> {
  const data = await request<AuthResponse>('/auth/login', {
    method: 'POST',
    body: JSON.stringify({ email, password }),
  });
  setStoredToken(data.access_token);
  return data;
}

export async function register(email: string, password: string, display_name?: string): Promise<AuthResponse> {
  const data = await request<AuthResponse>('/auth/register', {
    method: 'POST',
    body: JSON.stringify({ email, password, display_name }),
  });
  setStoredToken(data.access_token);
  return data;
}

export async function logout(): Promise<void> {
  try {
    await request('/auth/logout', { method: 'POST' });
  } finally {
    clearStoredToken();
  }
}

export async function getMe(): Promise<UserDTO> {
  return request<UserDTO>('/auth/me');
}

// ── Flows ───────────────────────────────────────────────────────────

export async function listFlows(): Promise<FlowDTO[]> {
  return request<FlowDTO[]>('/flows');
}

export async function getFlow(id: string): Promise<FlowDTO> {
  return request<FlowDTO>(`/flows/${id}`);
}

export async function createFlow(data: { title: string; description?: string; target?: string; config?: string }): Promise<FlowDTO> {
  return request<FlowDTO>('/flows', {
    method: 'POST',
    body: JSON.stringify(data),
  });
}

export async function updateFlow(id: string, data: Partial<{ title: string; description: string; target: string; status: string; config: string }>): Promise<FlowDTO> {
  return request<FlowDTO>(`/flows/${id}`, {
    method: 'PUT',
    body: JSON.stringify(data),
  });
}

export async function deleteFlow(id: string): Promise<void> {
  await request(`/flows/${id}`, { method: 'DELETE' });
}

// ── API Tokens ──────────────────────────────────────────────────────

export async function listAPITokens(): Promise<APITokenDTO[]> {
  return request<APITokenDTO[]>('/api-tokens');
}

export async function createAPIToken(label: string): Promise<{ token: string; data: APITokenDTO }> {
  return request('/api-tokens', {
    method: 'POST',
    body: JSON.stringify({ label }),
  });
}

export async function deleteAPIToken(id: string): Promise<void> {
  await request(`/api-tokens/${id}`, { method: 'DELETE' });
}

// ── Providers ───────────────────────────────────────────────────────

export async function listProviders(): Promise<ProviderDTO[]> {
  return request<ProviderDTO[]>('/providers');
}

export async function createProvider(data: {
  provider_type: string;
  model_name?: string;
  api_key?: string;
  base_url?: string;
  is_default?: boolean;
}): Promise<ProviderDTO> {
  return request<ProviderDTO>('/providers', {
    method: 'POST',
    body: JSON.stringify(data),
  });
}

export async function updateProvider(id: string, data: Partial<{
  model_name: string;
  api_key: string;
  base_url: string;
  is_default: boolean;
}>): Promise<ProviderDTO> {
  return request<ProviderDTO>(`/providers/${id}`, {
    method: 'PUT',
    body: JSON.stringify(data),
  });
}

export async function deleteProvider(id: string): Promise<void> {
  await request(`/providers/${id}`, { method: 'DELETE' });
}

export interface ProviderInfoDTO {
  type: string;
  label: string;
  model: string;
  source: string;
  available: boolean;
}

export interface TestProviderResult {
  success: boolean;
  message?: string;
  error?: string;
}

export async function testProvider(data: {
  provider_type: string;
  provider_id?: string;
  api_key?: string;
  base_url?: string;
  model_name?: string;
}): Promise<TestProviderResult> {
  return request<TestProviderResult>('/providers/test', {
    method: 'POST',
    body: JSON.stringify(data),
  });
}

export async function getAvailableProviders(): Promise<ProviderInfoDTO[]> {
  return request<ProviderInfoDTO[]>('/providers/available');
}

// ── Flow Execution ──────────────────────────────────────────────────

export interface ActionDTO {
  id: string;
  action_type: string;
  status: string;
  input: string;
  output?: string;
  duration_ms?: number;
}

export interface SubtaskDTO {
  id: string;
  title: string;
  description: string;
  agent_role: string;
  sort_order: number;
  status: string;
  result?: string;
  actions?: ActionDTO[];
}

export interface TaskDTO {
  id: string;
  title: string;
  description: string;
  status: string;
  result?: string;
  sort_order: number;
  subtasks?: SubtaskDTO[];
}

export interface TraceEntryDTO {
  id: string;
  created_at: string;
  kind: 'agent_event' | 'transcript' | 'terminal';
  debug: boolean;
  agent_role?: string;
  task_label?: string;
  summary: string;
  content?: string;
  event_type?: string;
  role?: string;
  chain_type?: string;
  command?: string;
  stdout?: string;
  stderr?: string;
}

export interface BrowserCapabilitiesDTO {
  mode: 'scraper' | 'native';
  screenshots_enabled: boolean;
}

export interface FindingDTO {
  id: string;
  severity: string;
  title: string;
  description: string;
  evidence: string;
  task_title: string;
  created_at: string;
}

export interface FlowDetailDTO {
  flow: FlowDTO;
  tasks: TaskDTO[];
  trace_entries: TraceEntryDTO[];
  agent_logs: AgentLogDTO[];
  terminal_logs: TerminalLogDTO[];
  search_logs: SearchLogDTO[];
  vector_store_logs: VectorStoreLogDTO[];
  artifacts: ArtifactDTO[];
  findings: FindingDTO[];
  message_chains: MessageChainDTO[];
  browser_capabilities: BrowserCapabilitiesDTO;
}

export interface AgentLogDTO {
  id: string;
  agent_role: string;
  event_type: string;
  message: string;
  metadata: string;
  created_at: string;
}

export interface TerminalLogDTO {
  id: string;
  stream_type: string;
  command?: string;
  content: string;
  created_at: string;
}

export interface SearchLogDTO {
  id: string;
  tool_name: string;
  provider: string;
  query: string;
  target: string;
  result_count: number;
  summary: string;
  metadata: string;
  created_at: string;
}

export interface VectorStoreLogDTO {
  id: string;
  action: string;
  query: string;
  content: string;
  result_count: number;
  metadata: string;
  created_at: string;
}

export interface ArtifactDTO {
  id: string;
  action_id: string;
  action_type: string;
  task_id: string;
  task_title: string;
  subtask_id: string;
  kind: string;
  file_path?: string;
  content?: string;
  metadata: string;
  created_at: string;
}

export interface MessageChainDTO {
  id: string;
  flow_id: string;
  task_id: string;
  task_title: string;
  subtask_id: string;
  subtask_title: string;
  role: string;
  agent_role: string;
  chain_type: string;
  content: string;
  token_count: number;
  metadata: string;
  created_at: string;
}

export interface AssistantDTO {
  id: string;
  flow_id: string;
  title: string;
  status: string;
  use_agents: boolean;
  created_at: string;
  updated_at: string;
}

export interface AssistantLogDTO {
  id: string;
  role: string;
  agent_role: string;
  content: string;
  metadata: string;
  created_at: string;
}

export interface AssistantDetailDTO {
  assistant: AssistantDTO;
  logs: AssistantLogDTO[];
}

export async function getFlowDetail(id: string, includeDebugTrace = false): Promise<FlowDetailDTO> {
  const qs = includeDebugTrace ? '?include_debug_trace=true' : '';
  return request<FlowDetailDTO>(`/flows/${id}${qs}`);
}

export async function startFlow(id: string): Promise<{ message: string; status: string }> {
  return request(`/flows/${id}/start`, { method: 'POST' });
}

export async function stopFlow(id: string): Promise<{ message: string }> {
  return request(`/flows/${id}/stop`, { method: 'POST' });
}

export async function getAssistantSession(id: string): Promise<AssistantDetailDTO> {
  return request<AssistantDetailDTO>(`/flows/${id}/assistant`);
}

export async function updateAssistantSession(
  id: string,
  data: { use_agents: boolean },
): Promise<AssistantDetailDTO> {
  return request<AssistantDetailDTO>(`/flows/${id}/assistant`, {
    method: 'PUT',
    body: JSON.stringify(data),
  });
}

export async function sendAssistantMessage(
  id: string,
  data: { content: string; use_agents?: boolean },
): Promise<AssistantDetailDTO> {
  return request<AssistantDetailDTO>(`/flows/${id}/assistant/messages`, {
    method: 'POST',
    body: JSON.stringify(data),
  });
}

export function subscribeFlowEvents(
  id: string,
  onEvent: (type: string, data: Record<string, unknown>) => void,
  onError?: (err: Event) => void,
): EventSource {
  const token = getStoredToken();
  // EventSource doesn't support custom headers, so we pass the token via query param.
  // The backend auth middleware should also accept ?token=...
  const url = `${BASE}/flows/${id}/events${token ? `?token=${encodeURIComponent(token)}` : ''}`;
  const es = new EventSource(url);

  // Listen for specific event types.
  const eventTypes = [
    'connected', 'flow_started', 'task_created', 'subtask_started',
    'action_completed', 'tool_executed', 'subtask_completed',
    'task_completed', 'flow_completed', 'flow_failed', 'flow_stopped',
    'finding_created',
  ];

  for (const et of eventTypes) {
    es.addEventListener(et, (e: MessageEvent) => {
      try {
        const data = JSON.parse(e.data);
        onEvent(et, data);
      } catch {
        onEvent(et, {});
      }
    });
  }

  if (onError) {
    es.onerror = onError;
  }

  return es;
}

// ── Artifact Files ─────────────────────────────────────────────────

/**
 * Fetch a file-backed artifact as a Blob via authenticated request.
 * Used for binary artifacts like browser screenshots.
 */
export async function fetchArtifactBlob(flowId: string, artifactId: string): Promise<Blob> {
  const token = getStoredToken();
  const headers: Record<string, string> = {};
  if (token) {
    headers['Authorization'] = `Bearer ${token}`;
  }

  const res = await fetch(`${BASE}/flows/${flowId}/artifacts/${artifactId}/file`, { headers });

  if (!res.ok) {
    throw new APIError(`Failed to fetch artifact file: ${res.statusText}`, res.status);
  }

  return res.blob();
}

export { APIError, getStoredToken, clearStoredToken };
