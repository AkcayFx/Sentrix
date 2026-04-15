import { useState, useEffect, useCallback } from 'react';

interface Toast {
  id: number;
  message: string;
  severity: 'success' | 'error' | 'warning' | 'info';
  duration: number;
}

let toastId = 0;
let addToastFn: ((t: Omit<Toast, 'id'>) => void) | null = null;

/** Global function to show a toast from anywhere in the app. */
export function showToast(
  message: string,
  severity: Toast['severity'] = 'info',
  duration = 4000
) {
  addToastFn?.({ message, severity, duration });
}

export default function ToastContainer() {
  const [toasts, setToasts] = useState<Toast[]>([]);

  const addToast = useCallback((t: Omit<Toast, 'id'>) => {
    const id = ++toastId;
    setToasts((prev) => [...prev, { ...t, id }]);
    setTimeout(() => {
      setToasts((prev) => prev.filter((x) => x.id !== id));
    }, t.duration);
  }, []);

  useEffect(() => {
    addToastFn = addToast;
    return () => {
      addToastFn = null;
    };
  }, [addToast]);

  if (toasts.length === 0) return null;

  return (
    <div className="toast-container">
      {toasts.map((t) => (
        <div
          key={t.id}
          className={`toast toast-${t.severity}`}
          onClick={() => setToasts((prev) => prev.filter((x) => x.id !== t.id))}
        >
          <span className="toast-icon">
            {t.severity === 'success' && '✓'}
            {t.severity === 'error' && '✕'}
            {t.severity === 'warning' && '⚠'}
            {t.severity === 'info' && 'ℹ'}
          </span>
          <span className="toast-message">{t.message}</span>
          <div
            className="toast-progress"
            style={{ animationDuration: `${t.duration}ms` }}
          />
        </div>
      ))}
    </div>
  );
}
