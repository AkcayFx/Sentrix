export default function ObservabilityPage() {
  return (
    <>
      <div className="page-header">
        <h1 className="page-title">📡 Observability</h1>
      </div>

      <div className="page-body">
        <div className="observability-hero">
          <div className="observability-hero-icon">📡</div>
          <h2>Monitoring & Tracing</h2>
          <p className="observability-hero-desc">
            Full-stack observability with OpenTelemetry, Grafana dashboards, and distributed tracing.
          </p>
        </div>

        <div className="stats-grid" style={{ marginTop: '32px' }}>
          <div className="stat-card">
            <div className="stat-card-header">
              <div className="stat-card-icon accent">📊</div>
            </div>
            <div className="stat-card-value">Grafana</div>
            <div className="stat-card-label">
              Dashboards for API performance, agent metrics, and LLM usage.
              Activate with the observability docker-compose overlay.
            </div>
          </div>

          <div className="stat-card">
            <div className="stat-card-header">
              <div className="stat-card-icon violet">🔍</div>
            </div>
            <div className="stat-card-value">Jaeger</div>
            <div className="stat-card-label">
              Distributed tracing UI for tracking requests across agents,
              tools, and LLM calls.
            </div>
          </div>

          <div className="stat-card">
            <div className="stat-card-header">
              <div className="stat-card-icon blue">📝</div>
            </div>
            <div className="stat-card-value">Loki</div>
            <div className="stat-card-label">
              Centralized log aggregation. Query and correlate logs
              alongside traces and metrics.
            </div>
          </div>

          <div className="stat-card">
            <div className="stat-card-header">
              <div className="stat-card-icon green">📈</div>
            </div>
            <div className="stat-card-value">Metrics</div>
            <div className="stat-card-label">
              VictoriaMetrics time-series storage for request latency,
              token usage, and agent completion rates.
            </div>
          </div>
        </div>

        <div className="card" style={{ marginTop: '24px', textAlign: 'center' }}>
          <p style={{ color: 'var(--text-secondary)', marginBottom: '16px' }}>
            Start the observability stack with:
          </p>
          <code className="observability-cmd">
            docker compose -f docker-compose.yml -f docker-compose-observability.yml up -d
          </code>
        </div>
      </div>
    </>
  );
}
