import { beforeEach, describe, expect, it, vi } from 'vitest';
import { clearStoredToken, getStoredToken, login } from './api';

describe('api auth helpers', () => {
  beforeEach(() => {
    localStorage.clear();
    vi.restoreAllMocks();
  });

  it('stores access token after successful login', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        access_token: 'token-123',
        refresh_token: 'refresh-123',
        expires_at: '2030-01-01T00:00:00Z',
        user: {
          id: 'u1',
          email: 'demo@test.com',
          display_name: 'Demo',
          role: 'user',
        },
      }),
    } satisfies Partial<Response>);

    vi.stubGlobal('fetch', fetchMock);

    await login('demo@test.com', 'secret');

    expect(fetchMock).toHaveBeenCalledWith(
      '/api/v1/auth/login',
      expect.objectContaining({
        method: 'POST',
      }),
    );
    expect(getStoredToken()).toBe('token-123');
  });

  it('removes stored token on clearStoredToken', () => {
    localStorage.setItem('sentrix_token', 'temporary-token');
    clearStoredToken();
    expect(getStoredToken()).toBeNull();
  });
});
