interface ScanLineProps {
  color?: string;
  duration?: number;
  className?: string;
}

export default function ScanLine({
  color = 'var(--accent)',
  duration = 4,
  className = '',
}: ScanLineProps) {
  return (
    <div
      className={`scan-line-container ${className}`}
      style={{ '--scan-color': color, '--scan-duration': `${duration}s` } as React.CSSProperties}
    >
      <div className="scan-line-beam" />
    </div>
  );
}
