// Sonus: the two-step verification and app password routes (server/sonus_twofactor.go), under /auth. Signed in, the
// session token goes along; during the first sign-in, the setup token the server handed out instead.
import { baseUrl } from '../utils'

export const authCall = async (method, path, body, token) => {
  const bearer = token ?? localStorage.getItem('token')
  const response = await fetch(baseUrl(`/auth${path}`), {
    method,
    headers: {
      'Content-Type': 'application/json',
      ...(bearer ? { 'X-ND-Authorization': `Bearer ${bearer}` } : {}),
    },
    body: body ? JSON.stringify(body) : undefined,
  })
  let data = null
  try {
    data = await response.json()
  } catch {
    // 204 No Content and the like
  }
  if (!response.ok) {
    const error = new Error((data && data.error) || response.statusText)
    error.status = response.status
    error.twoFactor = data && data.twoFactor
    throw error
  }
  return data
}

export const getTwoFactorStatus = () => authCall('GET', '/2fa')
export const beginSetup = (token, code) =>
  authCall('POST', '/2fa/setup', { token, code }, token)
export const confirmSetup = (token, code) =>
  authCall('POST', '/2fa/confirm', { token, code }, token)
export const newRecoveryCodes = (code) =>
  authCall('POST', '/2fa/recovery-codes', { code })
export const disableTwoFactor = (code) =>
  authCall('POST', '/2fa/disable', { code })
export const listAppPasswords = () => authCall('GET', '/app-passwords')
export const createAppPassword = (name) =>
  authCall('POST', '/app-passwords', { name })
export const deleteAppPassword = (id) =>
  authCall('DELETE', `/app-passwords/${encodeURIComponent(id)}`)
export const getUserTwoFactor = (userId) =>
  authCall('GET', `/2fa/users/${encodeURIComponent(userId)}`)
export const resetUserTwoFactor = (userId) =>
  authCall('POST', `/2fa/users/${encodeURIComponent(userId)}/reset`)
