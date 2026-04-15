import { lazy, Suspense } from 'react';
import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom';
import { useAuth } from './lib/auth';
import Layout from './components/Layout';
import ToastContainer from './components/Toast';

const LoginPage = lazy(() => import('./pages/Login'));
const RegisterPage = lazy(() => import('./pages/Register'));
const Dashboard = lazy(() => import('./pages/Dashboard'));
const FlowsPage = lazy(() => import('./pages/Flows'));
const FlowDetailPage = lazy(() => import('./pages/FlowDetail'));
const ProvidersPage = lazy(() => import('./pages/Providers'));
const TokensPage = lazy(() => import('./pages/Tokens'));
const MemoryPage = lazy(() => import('./pages/Memory'));
const ObservabilityPage = lazy(() => import('./pages/Observability'));

function RouteLoader() {
  return (
    <div className="loading-screen">
      <div className="spinner spinner-lg" />
      <p>Loading Sentrix...</p>
    </div>
  );
}

function ProtectedRoute({ children }: { children: React.ReactNode }) {
  const { user, loading } = useAuth();

  if (loading) {
    return (
      <div className="loading-screen">
        <div className="spinner spinner-lg" />
        <p>Loading Sentrix...</p>
      </div>
    );
  }

  if (!user) {
    return <Navigate to="/login" replace />;
  }

  return <>{children}</>;
}

function PublicRoute({ children }: { children: React.ReactNode }) {
  const { user, loading } = useAuth();

  if (loading) {
    return (
      <div className="loading-screen">
        <div className="spinner spinner-lg" />
        <p>Loading Sentrix...</p>
      </div>
    );
  }

  if (user) {
    return <Navigate to="/" replace />;
  }

  return <>{children}</>;
}

export default function App() {
  return (
    <BrowserRouter>
      <ToastContainer />
      <Suspense fallback={<RouteLoader />}>
        <Routes>
          {/* Public routes */}
          <Route path="/login" element={<PublicRoute><LoginPage /></PublicRoute>} />
          <Route path="/register" element={<PublicRoute><RegisterPage /></PublicRoute>} />

          {/* Protected routes */}
          <Route
            element={
              <ProtectedRoute>
                <Layout />
              </ProtectedRoute>
            }
          >
            <Route path="/" element={<Dashboard />} />
            <Route path="/flows" element={<FlowsPage />} />
            <Route path="/flows/:id" element={<FlowDetailPage />} />
            <Route path="/providers" element={<ProvidersPage />} />
            <Route path="/tokens" element={<TokensPage />} />
            <Route path="/memory" element={<MemoryPage />} />
            <Route path="/observability" element={<ObservabilityPage />} />
          </Route>

          {/* Fallback */}
          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      </Suspense>
    </BrowserRouter>
  );
}
