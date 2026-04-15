import { useState, type FormEvent } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { useAuth } from '../lib/auth';
import BinaryRain from '../components/BinaryRain';
import GlitchText from '../components/GlitchText';

export default function RegisterPage() {
  const { register, error, clearError } = useAuth();
  const navigate = useNavigate();
  const [displayName, setDisplayName] = useState('');
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [confirm, setConfirm] = useState('');
  const [localError, setLocalError] = useState('');
  const [submitting, setSubmitting] = useState(false);

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault();
    clearError();
    setLocalError('');

    if (password !== confirm) {
      setLocalError('Passwords do not match');
      return;
    }
    if (password.length < 12) {
      setLocalError('Password must be at least 12 characters');
      return;
    }

    setSubmitting(true);
    try {
      await register(email, password, displayName);
      navigate('/');
    } catch {
      // error set in context
    } finally {
      setSubmitting(false);
    }
  };

  const displayError = localError || error;

  return (
    <div className="auth-layout scanlines crt-flicker crt-vignette">
      <div className="auth-left">
        <div className="auth-card">
          <h1 style={{ textShadow: '0 0 10px var(--accent-glow)' }}>Register Operator</h1>
          <p>Provision new credentials for Sentrix access</p>

          {displayError && <div className="alert alert-error" style={{ marginBottom: 20 }}>{displayError}</div>}

          <form className="auth-form" onSubmit={handleSubmit}>
            <div className="input-group">
              <label htmlFor="reg-name">Callsign</label>
              <input
                id="reg-name"
                type="text"
                className="input"
                placeholder="Your display name"
                value={displayName}
                onChange={(e) => setDisplayName(e.target.value)}
                autoFocus
              />
            </div>

            <div className="input-group">
              <label htmlFor="reg-email">Email</label>
              <input
                id="reg-email"
                type="email"
                className="input"
                placeholder="you@example.com"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                required
              />
            </div>

            <div className="input-group">
              <label htmlFor="reg-password">Password</label>
              <input
                id="reg-password"
                type="password"
                className="input"
                placeholder="Min 12 chars, upper/lower/digit/special"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                required
              />
            </div>

            <div className="input-group">
              <label htmlFor="reg-confirm">Confirm Password</label>
              <input
                id="reg-confirm"
                type="password"
                className="input"
                placeholder="Re-enter your password"
                value={confirm}
                onChange={(e) => setConfirm(e.target.value)}
                required
              />
            </div>

            <button type="submit" className="btn btn-primary btn-lg" disabled={submitting}>
              {submitting ? (
                <><span className="spinner" style={{ width: 18, height: 18, borderWidth: 2 }} /> Provisioning...</>
              ) : (
                '> Create Account'
              )}
            </button>
          </form>

          <div className="auth-footer">
            Already have an account? <Link to="/login">Sign in</Link>
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
            <span>Deploy in minutes with Docker</span>
          </div>
          <div className="auth-feature">
            <div className="auth-feature-icon">10</div>
            <span>Multi-provider LLM support</span>
          </div>
          <div className="auth-feature">
            <div className="auth-feature-icon">11</div>
            <span>20+ security tools integrated</span>
          </div>
          <div className="auth-feature">
            <div className="auth-feature-icon">00</div>
            <span>Vector memory for smart findings</span>
          </div>
        </div>
      </div>
    </div>
  );
}
