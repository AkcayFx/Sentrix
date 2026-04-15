import { NavLink, Outlet, useNavigate } from 'react-router-dom';
import { useAuth } from '../lib/auth';
import { useState, useEffect } from 'react';
import BootSequence from './BootSequence';

const NAV_ITEMS = [
  {
    section: 'Operations',
    items: [
      { to: '/', label: 'Dashboard', icon: '>', end: true },
      { to: '/flows', label: 'Flows', icon: '$' },
      { to: '/memory', label: 'Memory', icon: '#' },
    ],
  },
  {
    section: 'Configuration',
    items: [
      { to: '/providers', label: 'LLM Providers', icon: '@' },
      { to: '/tokens', label: 'API Tokens', icon: '*' },
      { to: '/observability', label: 'Observability', icon: '~' },
    ],
  },
];

export default function Layout() {
  const { user, logout } = useAuth();
  const navigate = useNavigate();
  const [collapsed, setCollapsed] = useState(false);
  const [booted, setBooted] = useState(() => sessionStorage.getItem('sentrix-booted') === '1');
  const [showClock, setShowClock] = useState('');

  useEffect(() => {
    const tick = () => setShowClock(new Date().toLocaleTimeString('en-US', { hour12: false }));
    tick();
    const id = setInterval(tick, 1000);
    return () => clearInterval(id);
  }, []);

  const handleBootComplete = () => {
    sessionStorage.setItem('sentrix-booted', '1');
    setBooted(true);
  };

  const handleLogout = async () => {
    await logout();
    navigate('/login');
  };

  const initials = user?.display_name
    ? user.display_name.split(' ').map((w: string) => w[0]).join('').toUpperCase().slice(0, 2)
    : '?';

  if (!booted) {
    return <BootSequence onComplete={handleBootComplete} />;
  }

  return (
    <div className={`app-layout app-scanlines crt-flicker crt-vignette grid-bg threat-border ${collapsed ? 'sidebar-collapsed' : ''}`}>
      {/* Sidebar */}
      <aside className={`sidebar ${collapsed ? 'collapsed' : ''}`}>
        <div className="sidebar-header">
          <div className="sidebar-logo">
            <div className="sidebar-logo-icon">S</div>
            {!collapsed && <span className="sidebar-logo-text">Sentrix</span>}
          </div>
          <button
            className="btn btn-ghost btn-icon sidebar-toggle"
            onClick={() => setCollapsed(!collapsed)}
            title={collapsed ? 'Expand sidebar' : 'Collapse sidebar'}
          >
            {collapsed ? '>>' : '<<'}
          </button>
        </div>
        {!collapsed && (
          <div style={{
            padding: '0 20px 8px',
            fontFamily: 'var(--font-mono)',
            fontSize: '0.5625rem',
            color: 'var(--matrix)',
            opacity: 0.6,
            display: 'flex',
            justifyContent: 'space-between',
            letterSpacing: '0.04em',
          }}>
            <span>{showClock}</span>
            <span style={{ color: 'var(--accent)' }}>SYS:OK</span>
          </div>
        )}

        <nav className="sidebar-nav">
          {NAV_ITEMS.map((section) => (
            <div className="nav-section" key={section.section}>
              {!collapsed && <div className="nav-section-title">{section.section}</div>}
              {section.items.map((item) => (
                <NavLink
                  key={item.to}
                  to={item.to}
                  end={item.end}
                  className={({ isActive }) => `nav-link ${isActive ? 'active' : ''}`}
                >
                  <span className="nav-link-icon" style={{ fontFamily: 'var(--font-mono)', fontWeight: 700 }}>
                    {item.icon}
                  </span>
                  {!collapsed && item.label}
                </NavLink>
              ))}
            </div>
          ))}
        </nav>

        <div className="sidebar-footer">
          {!collapsed && (
            <div style={{
              fontFamily: 'var(--font-mono)',
              fontSize: '0.5625rem',
              color: 'var(--text-muted)',
              opacity: 0.4,
              marginBottom: 12,
              letterSpacing: '0.05em',
              lineHeight: 1.6,
              wordBreak: 'break-all',
            }}>
              01010011 01000101 01001110 01010100 01010010 01001001 01011000
            </div>
          )}
          <div className="sidebar-user" onClick={handleLogout} title="Click to log out">
            <div className="sidebar-user-avatar">{initials}</div>
            {!collapsed && (
              <div className="sidebar-user-info">
                <div className="sidebar-user-name">{user?.display_name || 'User'}</div>
                <div className="sidebar-user-email">{user?.email}</div>
              </div>
            )}
            {!collapsed && (
              <span style={{ color: 'var(--text-muted)', fontSize: '0.875rem', fontFamily: 'var(--font-mono)' }}>[x]</span>
            )}
          </div>
        </div>
      </aside>

      {/* Mobile overlay */}
      {!collapsed && (
        <div
          className="sidebar-overlay"
          onClick={() => setCollapsed(true)}
        />
      )}

      {/* Main */}
      <main className={`main-content ${collapsed ? 'main-content-expanded' : ''}`}>
        <Outlet />
      </main>
    </div>
  );
}
