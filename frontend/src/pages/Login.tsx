import { useState, useEffect, type FormEvent } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { useAuth } from '../lib/auth';
import BinaryRain from '../components/BinaryRain';
import GlitchText from '../components/GlitchText';

function TerminalPrompt() {
  const [text, setText] = useState('');
  const full = '> Awaiting credentials...';
  useEffect(() => {
    if (text.length >= full.length) return;
    const t = setTimeout(() => setText(full.slice(0, text.length + 1)), 60 + Math.random() * 40);
    return () => clearTimeout(t);
  }, [text]);

  return (
    <div style={{
      fontFamily: 'var(--font-mono)',
      fontSize: '0.6875rem',
      color: 'var(--matrix)',
      opacity: 0.7,
      marginBottom: 24,
      letterSpacing: '0.02em',
    }}>
      {text}
      <span style={{ animation: 'blink-cursor 0.5s step-end infinite' }}>_</span>
    </div>
  );
}

export default function LoginPage() {
  const { login, loginAsGuest, error, clearError } = useAuth();
  const navigate = useNavigate();
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [submitting, setSubmitting] = useState(false);

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault();
    clearError();
    setSubmitting(true);
    try {
      await login(email, password);
      navigate('/');
    } catch {
      // error is set in context
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="auth-layout scanlines crt-flicker crt-vignette">
      <div className="auth-left">
        <div className="auth-card">
          <TerminalPrompt />
          <h1 style={{ textShadow: '0 0 10px var(--accent-glow)' }}>Access Terminal</h1>
          <p>Authenticate to initialize secure session</p>

          {error && <div className="alert alert-error" style={{ marginBottom: 20 }}>{error}</div>}

          <form className="auth-form" onSubmit={handleSubmit}>
            <div className="input-group">
              <label htmlFor="login-email">Email</label>
              <input
                id="login-email"
                type="email"
                className="input"
                placeholder="you@example.com"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                required
                autoFocus
              />
            </div>

            <div className="input-group">
              <label htmlFor="login-password">Password</label>
              <input
                id="login-password"
                type="password"
                className="input"
                placeholder="Enter your password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                required
              />
            </div>

            <button type="submit" className="btn btn-primary btn-lg" disabled={submitting}>
              {submitting ? (
                <><span className="spinner" style={{ width: 18, height: 18, borderWidth: 2 }} /> Decrypting...</>
              ) : (
                '> Initialize Session'
              )}
            </button>
          </form>

          <div className="auth-divider">
            <span>or</span>
          </div>

          <button
            type="button"
            className="btn btn-ghost btn-lg btn-guest"
            disabled={submitting}
            onClick={async () => {
              setSubmitting(true);
              try {
                await loginAsGuest();
                navigate('/');
              } catch { /* error shown via context */ }
              finally { setSubmitting(false); }
            }}
          >
            Continue as Guest
          </button>

          <div className="auth-footer">
            Don't have an account? <Link to="/register">Create one</Link>
          </div>
        </div>
      </div>

      <div className="auth-binary-divider" />

      <div className="auth-right">
        <BinaryRain opacity={0.15} speed={0.8} density={0.5} />
        <div className="auth-brand">
          <div className="auth-brand-icon">S</div>
          <GlitchText text="Sentrix" className="auth-brand-title" />
          <p>AI-Powered Autonomous Security Testing Platform</p>
        </div>
        <div className="auth-features">
          <div className="auth-feature">
            <div className="auth-feature-icon">01</div>
            <span>Automated vulnerability assessment</span>
          </div>
          <div className="auth-feature">
            <div className="auth-feature-icon">10</div>
            <span>Multi-agent AI orchestration</span>
          </div>
          <div className="auth-feature">
            <div className="auth-feature-icon">11</div>
            <span>Real-time progress monitoring</span>
          </div>
          <div className="auth-feature">
            <div className="auth-feature-icon">00</div>
            <span>Sandboxed Docker execution</span>
          </div>
        </div>
      </div>
    </div>
  );
}
