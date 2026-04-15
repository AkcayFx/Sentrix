import type { ArtifactDTO, FlowDTO, SearchLogDTO, TaskDTO, TerminalLogDTO } from './api';

interface ParsedFinding {
  severity: string;
  title: string;
  message: string;
  remediation: string;
  evidence: string;
  createdAt: string;
}

const SEVERITY_ORDER = ['critical', 'high', 'medium', 'low', 'info'] as const;

function escapeHtml(value: string): string {
  return value
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

function formatTime(value: string): string {
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
}

function parseMetadata(value: string): Record<string, unknown> {
  try {
    return JSON.parse(value) as Record<string, unknown>;
  } catch {
    return {};
  }
}

function normalizeSeverity(value: string): string {
  const normalized = value.toLowerCase().trim();
  return SEVERITY_ORDER.includes(normalized as (typeof SEVERITY_ORDER)[number]) ? normalized : 'info';
}

function severityClass(severity: string): string {
  return `sev-${normalizeSeverity(severity)}`;
}

function markdownToHtml(markdown: string): string {
  const lines = markdown.split(/\r?\n/);
  const html: string[] = [];
  let inCode = false;

  for (const raw of lines) {
    const line = raw.trimEnd();
    if (line.trim().startsWith('```')) {
      inCode = !inCode;
      html.push(inCode ? '<pre><code>' : '</code></pre>');
      continue;
    }
    if (inCode) {
      html.push(`${escapeHtml(line)}\n`);
      continue;
    }

    const heading = line.match(/^(#{1,6})\s+(.+)$/);
    if (heading) {
      const lvl = heading[1].length;
      html.push(`<h${lvl}>${escapeHtml(heading[2])}</h${lvl}>`);
      continue;
    }

    const bullet = line.match(/^[-*]\s+(.+)$/);
    if (bullet) {
      html.push(`<li>${escapeHtml(bullet[1])}</li>`);
      continue;
    }

    if (!line.trim()) {
      html.push('');
      continue;
    }

    html.push(`<p>${escapeHtml(line)}</p>`);
  }

  const joined = html.join('\n');
  return joined
    .replace(/(?:^|\n)<li>[\s\S]*?<\/li>(?=\n(?!<li>)|$)/g, (block) => {
      const items = block.split('\n').filter(x => x.startsWith('<li>')).join('\n');
      return `\n<ul>\n${items}\n</ul>`;
    })
    .trim();
}

function collectFindings(artifacts: ArtifactDTO[]): ParsedFinding[] {
  return artifacts
    .filter(a => a.kind === 'finding')
    .map((artifact) => {
      const meta = parseMetadata(artifact.metadata);
      return {
        severity: String(meta.severity || 'info'),
        title: String(meta.title || artifact.task_title || 'Security finding'),
        message: String(meta.message || ''),
        remediation: String(meta.remediation || ''),
        evidence: artifact.content || '',
        createdAt: artifact.created_at,
      };
    })
    .sort((a, b) => SEVERITY_ORDER.indexOf(normalizeSeverity(a.severity) as (typeof SEVERITY_ORDER)[number]) - SEVERITY_ORDER.indexOf(normalizeSeverity(b.severity) as (typeof SEVERITY_ORDER)[number]));
}

function buildTaskRows(tasks: TaskDTO[]): string {
  if (!tasks.length) return '<tr><td colspan="3">No tasks available</td></tr>';
  return tasks
    .slice()
    .sort((a, b) => a.sort_order - b.sort_order)
    .map(task => `<tr><td>${escapeHtml(task.title)}</td><td><span class="pill status-${escapeHtml(task.status.toLowerCase())}">${escapeHtml(task.status)}</span></td><td>${task.subtasks?.length || 0}</td></tr>`)
    .join('\n');
}

function buildSeveritySummary(findings: ParsedFinding[]): string {
  const counts = new Map<string, number>();
  for (const sev of SEVERITY_ORDER) counts.set(sev, 0);
  for (const finding of findings) {
    const sev = normalizeSeverity(finding.severity);
    counts.set(sev, (counts.get(sev) || 0) + 1);
  }
  return SEVERITY_ORDER.map((sev) => (
    `<div class="metric-card ${severityClass(sev)}"><div class="metric-label">${sev.toUpperCase()}</div><div class="metric-value">${counts.get(sev) || 0}</div></div>`
  )).join('\n');
}

function buildFindingCards(findings: ParsedFinding[]): string {
  if (!findings.length) return '<div class="empty">No findings were persisted for this flow.</div>';
  return findings.map((finding) => {
    const severity = normalizeSeverity(finding.severity);
    return `<article class="finding-card ${severityClass(severity)}">
      <header><span class="pill ${severityClass(severity)}">${escapeHtml(severity.toUpperCase())}</span><span class="time">${escapeHtml(formatTime(finding.createdAt))}</span></header>
      <h3>${escapeHtml(finding.title)}</h3>
      ${finding.message ? `<p>${escapeHtml(finding.message)}</p>` : ''}
      ${finding.remediation ? `<p><strong>Remediation:</strong> ${escapeHtml(finding.remediation)}</p>` : ''}
      ${finding.evidence ? `<pre><code>${escapeHtml(finding.evidence)}</code></pre>` : ''}
    </article>`;
  }).join('\n');
}

function buildSearchRows(searchLogs: SearchLogDTO[]): string {
  if (!searchLogs.length) return '<tr><td colspan="4">No search logs available</td></tr>';
  return searchLogs.map(log => `<tr><td>${escapeHtml(formatTime(log.created_at))}</td><td>${escapeHtml(log.tool_name)}</td><td>${escapeHtml(log.provider || '-')}</td><td>${escapeHtml(log.query || '-')}</td></tr>`).join('\n');
}

function buildTerminalRows(terminalLogs: TerminalLogDTO[]): string {
  if (!terminalLogs.length) return '<tr><td colspan="3">No terminal logs available</td></tr>';
  return terminalLogs.map(log => `<tr><td>${escapeHtml(formatTime(log.created_at))}</td><td>${escapeHtml(log.stream_type)}</td><td>${escapeHtml(log.command || log.content.slice(0, 80))}</td></tr>`).join('\n');
}

/** Screenshot data URL entry for embedding in the self-contained report. */
export interface ScreenshotDataEntry {
  artifactId: string;
  targetUrl: string;
  action: string;
  createdAt: string;
  /** data:image/png;base64,... or empty on fetch failure */
  dataUrl: string;
}

function buildScreenshotSection(screenshots: ScreenshotDataEntry[]): string {
  if (!screenshots.length) return '';
  const cards = screenshots.map((ss) => {
    const imgTag = ss.dataUrl
      ? `<img src="${ss.dataUrl}" alt="Screenshot of ${escapeHtml(ss.targetUrl)}" style="width:100%;max-height:400px;object-fit:contain;border-radius:8px;border:1px solid rgba(148,163,184,.2);background:#0f172a" />`
      : '<div class="empty" style="height:80px">Screenshot could not be loaded</div>';
    return `<article class="finding-card" style="border-color:rgba(56,189,248,.3)">
      <header><span class="pill">${escapeHtml(ss.action)}</span><span class="time">${escapeHtml(formatTime(ss.createdAt))}</span></header>
      ${imgTag}
      <p style="word-break:break-all;font-size:.8rem;margin-top:8px">${escapeHtml(ss.targetUrl)}</p>
    </article>`;
  }).join('\n');
  return `<section class="panel"><h2>Browser Screenshots</h2><div class="finding-list">${cards}</div></section>`;
}

export function buildFlowHtmlReport(params: {
  flow: FlowDTO;
  tasks: TaskDTO[];
  artifacts: ArtifactDTO[];
  searchLogs: SearchLogDTO[];
  terminalLogs: TerminalLogDTO[];
  screenshots?: ScreenshotDataEntry[];
}): string {
  const { flow, tasks, artifacts, searchLogs, terminalLogs, screenshots = [] } = params;
  const findings = collectFindings(artifacts);
  const reports = artifacts
    .filter(a => a.kind === 'task_report_markdown' && a.content)
    .sort((a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime());

  const reportSections = reports.length
    ? reports.map((report, idx) => `<section class="panel"><h2>Task Report ${idx + 1}</h2><div class="subtle">${escapeHtml(report.task_title)} - ${escapeHtml(formatTime(report.created_at))}</div><div class="markdown">${markdownToHtml(report.content || '')}</div></section>`).join('\n')
    : '<section class="panel"><h2>Task Reports</h2><div class="empty">No markdown task reports found.</div></section>';

  const screenshotSection = buildScreenshotSection(screenshots);

  return `<!doctype html>
<html lang="en"><head><meta charset="utf-8" /><meta name="viewport" content="width=device-width, initial-scale=1" />
<title>${escapeHtml(flow.title)} - Sentrix Security Report</title>
<style>
:root{color-scheme:dark}*{box-sizing:border-box}body{margin:0;font-family:Inter,Segoe UI,Roboto,Helvetica,Arial,sans-serif;background:radial-gradient(1200px 500px at 10% 0%, #11203d, #05080f 60%);color:#e5e7eb;line-height:1.5}
.container{max-width:1100px;margin:0 auto;padding:32px 20px 64px}.hero{border:1px solid rgba(148,163,184,.2);border-radius:16px;padding:24px;background:linear-gradient(180deg,rgba(30,41,59,.65),rgba(15,23,42,.5));box-shadow:0 16px 48px rgba(0,0,0,.35);margin-bottom:20px}
h1{margin:0 0 8px;font-size:1.7rem}h2{margin-top:0;font-size:1.2rem}h3{margin:0 0 8px;font-size:1rem}p{margin:8px 0;color:#cbd5e1}.subtle{color:#94a3b8;font-size:.85rem}
.grid{display:grid;gap:12px;grid-template-columns:repeat(auto-fit,minmax(140px,1fr));margin-top:16px}.metric-card{border-radius:12px;border:1px solid rgba(148,163,184,.2);background:rgba(15,23,42,.65);padding:12px}.metric-label{font-size:.75rem;color:#94a3b8;margin-bottom:4px}.metric-value{font-size:1.4rem;font-weight:700}
.panel{border:1px solid rgba(148,163,184,.2);border-radius:14px;background:rgba(2,6,23,.62);padding:18px;margin-top:14px}table{width:100%;border-collapse:collapse;font-size:.88rem}th,td{text-align:left;padding:10px 8px;border-bottom:1px solid rgba(148,163,184,.15);vertical-align:top}th{color:#93c5fd;font-weight:600;font-size:.8rem;text-transform:uppercase;letter-spacing:.04em}
.pill{display:inline-block;border:1px solid rgba(148,163,184,.3);border-radius:999px;padding:2px 9px;font-size:.74rem;color:#e2e8f0}.status-done{border-color:rgba(34,197,94,.45);color:#86efac}.status-running,.status-queued{border-color:rgba(56,189,248,.45);color:#7dd3fc}.status-failed,.status-stopped{border-color:rgba(248,113,113,.5);color:#fda4af}.status-pending{border-color:rgba(148,163,184,.4);color:#cbd5e1}
.finding-list{display:grid;gap:12px}.finding-card{border:1px solid rgba(148,163,184,.2);border-radius:12px;padding:14px;background:rgba(15,23,42,.5)}.finding-card header{display:flex;justify-content:space-between;align-items:center;gap:8px;margin-bottom:8px}.time{color:#94a3b8;font-size:.78rem}
.sev-critical{border-color:rgba(239,68,68,.55)!important;color:#fca5a5}.sev-high{border-color:rgba(251,146,60,.55)!important;color:#fdba74}.sev-medium{border-color:rgba(234,179,8,.55)!important;color:#fde047}.sev-low{border-color:rgba(56,189,248,.55)!important;color:#7dd3fc}.sev-info{border-color:rgba(148,163,184,.45)!important;color:#cbd5e1}
pre{margin:10px 0 0;padding:12px;border-radius:10px;background:rgba(15,23,42,.78);color:#cbd5e1;overflow-x:auto;border:1px solid rgba(148,163,184,.2);font-size:.8rem;white-space:pre-wrap}.markdown h1,.markdown h2,.markdown h3,.markdown h4{margin-top:16px;margin-bottom:8px}.markdown ul{margin:8px 0;padding-left:18px;color:#d1d5db}
.empty{border:1px dashed rgba(148,163,184,.3);border-radius:12px;padding:16px;color:#94a3b8;text-align:center}
</style></head>
<body><main class="container">
<section class="hero"><h1>${escapeHtml(flow.title)}</h1><p>${escapeHtml(flow.description || 'No scope provided')}</p><div class="subtle">Generated by Sentrix on ${escapeHtml(new Date().toLocaleString())}</div>
<div class="grid"><div class="metric-card"><div class="metric-label">Flow Status</div><div class="metric-value">${escapeHtml(flow.status.toUpperCase())}</div></div><div class="metric-card"><div class="metric-label">Tasks</div><div class="metric-value">${tasks.length}</div></div><div class="metric-card"><div class="metric-label">Findings</div><div class="metric-value">${findings.length}</div></div><div class="metric-card"><div class="metric-label">Task Reports</div><div class="metric-value">${reports.length}</div></div></div></section>
<section class="panel"><h2>Severity Distribution</h2><div class="grid">${buildSeveritySummary(findings)}</div></section>
<section class="panel"><h2>Execution Overview</h2><table><thead><tr><th>Task</th><th>Status</th><th>Subtasks</th></tr></thead><tbody>${buildTaskRows(tasks)}</tbody></table></section>
<section class="panel"><h2>Security Findings</h2><div class="finding-list">${buildFindingCards(findings)}</div></section>
${reportSections}
${screenshotSection}
<section class="panel"><h2>Search Activity</h2><table><thead><tr><th>Time</th><th>Tool</th><th>Provider</th><th>Query</th></tr></thead><tbody>${buildSearchRows(searchLogs)}</tbody></table></section>
<section class="panel"><h2>Terminal Activity</h2><table><thead><tr><th>Time</th><th>Stream</th><th>Command / Preview</th></tr></thead><tbody>${buildTerminalRows(terminalLogs)}</tbody></table></section>
</main></body></html>`;
}
