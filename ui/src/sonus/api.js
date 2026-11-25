// Sonus: the two-step verification and app password routes (server/sonus_twofactor.go), under /auth. Signed in, the
// session token goes along; during the first sign-in, the setup token the server handed out instead.
//
// The Sonus server (Rust), which answers /auth in front of this Navidrome, asks for more than this Navidrome did: a
// one-time enrolment code at a first set-up, and a verification code to make an app password, to allow an app, and for
// an admin's reset. The codes are sent when there is one; this Navidrome's own routes read past them.
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
export const beginSetup = (token, code, enrolmentCode) =>
  authCall('POST', '/2fa/setup', { token, code, enrolmentCode }, token)
export const confirmSetup = (token, code, enrolmentCode) =>
  authCall('POST', '/2fa/confirm', { token, code, enrolmentCode }, token)
export const newRecoveryCodes = (code) =>
  authCall('POST', '/2fa/recovery-codes', { code })
export const disableTwoFactor = (code) =>
  authCall('POST', '/2fa/disable', { code })
export const listAppPasswords = () => authCall('GET', '/app-passwords')
export const createAppPassword = (name, code) =>
  authCall('POST', '/app-passwords', { name, code })
export const deleteAppPassword = (id) =>
  authCall('DELETE', `/app-passwords/${encodeURIComponent(id)}`)
export const getUserTwoFactor = (userId) =>
  authCall('GET', `/2fa/users/${encodeURIComponent(userId)}`)
// answers { enrolmentCode, expiresAt } where the server requires two-step verification: what the user sets it up again with
export const resetUserTwoFactor = (userId, code) =>
  authCall('POST', `/2fa/users/${encodeURIComponent(userId)}/reset`, { code })
// { enrolmentCode, expiresAt }, shown once: what a user without two-step verification sets it up with
export const issueEnrolmentCode = (userId, code) =>
  authCall('POST', `/2fa/users/${encodeURIComponent(userId)}/enrolment-code`, {
    code,
  })
