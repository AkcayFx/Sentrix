import { createContext, useContext, useEffect, useState, useCallback, type ReactNode } from 'react';
import { type UserDTO, getMe, login as apiLogin, register as apiRegister, logout as apiLogout, getStoredToken, clearStoredToken } from './api';

interface AuthState {
  user: UserDTO | null;
  loading: boolean;
  error: string | null;
  isGuest: boolean;
}

interface AuthContextValue extends AuthState {
  login: (email: string, password: string) => Promise<void>;
  register: (email: string, password: string, displayName?: string) => Promise<void>;
  loginAsGuest: () => Promise<void>;
  logout: () => Promise<void>;
  clearError: () => void;
}

const AuthContext = createContext<AuthContextValue | null>(null);

function generateGuestCredentials() {
  const id = crypto.randomUUID().replace(/-/g, '').slice(0, 12);
  const rand = crypto.randomUUID().replace(/-/g, '');
  return {
    email: `guest_${id}@sentrix.guest`,
    password: `G!${rand.slice(0, 10)}xQ9#`,
    displayName: 'Guest',
  };
}

export function AuthProvider({ children }: { children: ReactNode }) {
  const [state, setState] = useState<AuthState>({ user: null, loading: true, error: null, isGuest: false });

  useEffect(() => {
    const token = getStoredToken();
    if (!token) {
      setState({ user: null, loading: false, error: null, isGuest: false });
      return;
    }
    getMe()
      .then((user) => {
        const guest = localStorage.getItem('sentrix_guest') === '1';
        setState({ user, loading: false, error: null, isGuest: guest });
      })
      .catch(() => {
        clearStoredToken();
        localStorage.removeItem('sentrix_guest');
        setState({ user: null, loading: false, error: null, isGuest: false });
      });
  }, []);

  const login = useCallback(async (email: string, password: string) => {
    setState((s) => ({ ...s, loading: true, error: null }));
    try {
      localStorage.removeItem('sentrix_guest');
      const res = await apiLogin(email, password);
      setState({ user: res.user, loading: false, error: null, isGuest: false });
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : 'Login failed';
      setState((s) => ({ ...s, loading: false, error: msg }));
      throw err;
    }
  }, []);

  const register = useCallback(async (email: string, password: string, displayName?: string) => {
    setState((s) => ({ ...s, loading: true, error: null }));
    try {
      localStorage.removeItem('sentrix_guest');
      const res = await apiRegister(email, password, displayName);
      setState({ user: res.user, loading: false, error: null, isGuest: false });
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : 'Registration failed';
      setState((s) => ({ ...s, loading: false, error: msg }));
      throw err;
    }
  }, []);

  const loginAsGuest = useCallback(async () => {
    setState((s) => ({ ...s, loading: true, error: null }));
    try {
      const { email, password, displayName } = generateGuestCredentials();
      const res = await apiRegister(email, password, displayName);
      localStorage.setItem('sentrix_guest', '1');
      setState({ user: res.user, loading: false, error: null, isGuest: true });
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : 'Guest access failed';
      setState((s) => ({ ...s, loading: false, error: msg }));
      throw err;
    }
  }, []);

  const logout = useCallback(async () => {
    localStorage.removeItem('sentrix_guest');
    await apiLogout();
    setState({ user: null, loading: false, error: null, isGuest: false });
  }, []);

  const clearError = useCallback(() => {
    setState((s) => ({ ...s, error: null }));
  }, []);

  return (
    <AuthContext.Provider value={{ ...state, login, register, loginAsGuest, logout, clearError }}>
      {children}
    </AuthContext.Provider>
  );
}

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error('useAuth must be used within AuthProvider');
  return ctx;
}
