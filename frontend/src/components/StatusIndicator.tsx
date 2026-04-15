interface StatusIndicatorProps {
  status: 'pending' | 'running' | 'done' | 'failed' | 'stopped';
  size?: 'sm' | 'md' | 'lg';
  label?: string;
}

export default function StatusIndicator({ status, size = 'md', label }: StatusIndicatorProps) {
  const sizeClass = `status-indicator-${size}`;

  return (
    <span className={`status-indicator ${sizeClass} status-${status}`}>
      <span className="status-dot" />
      {label && <span className="status-label">{label}</span>}
    </span>
  );
}
